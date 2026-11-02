// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gollm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// fakeProvider is an httptest server that speaks just enough of one provider's
// wire protocol for gollm to complete a request. It records the credential
// header of every request so tests can assert which key reached which server.
type fakeProvider struct {
	srv  *httptest.Server
	hdr  string // credential header name
	mu   sync.Mutex
	keys []string
}

func (f *fakeProvider) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.keys...)
}

const (
	anthropicMessageJSON = `{"id":"m","type":"message","role":"assistant","model":"x",` +
		`"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
	anthropicStreamBody = `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}` + "\n\n" +
		`data: {"type":"message_stop"}` + "\n\n"
	openAICompletionJSON = `{"id":"c","object":"chat.completion","created":0,"model":"m",` +
		`"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`
	openAIStreamBody = `data: {"id":"c","object":"chat.completion.chunk","created":0,"model":"m",` +
		`"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}]}` + "\n\n" +
		"data: [DONE]\n\n"
	azureCompletionJSON = `{"id":"c","created":0,"model":"m",` +
		`"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`
)

// newFakeProvider starts a fake server for provider ("anthropic", "openai" or
// "azopenai"). azopenai uses TLS because the Azure SDK refuses to send key
// credentials over plain http.
func newFakeProvider(t *testing.T, provider string) *fakeProvider {
	t.Helper()
	f := &fakeProvider{}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.keys = append(f.keys, r.Header.Get(f.hdr))
		f.mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		stream := strings.Contains(string(body), `"stream":true`)
		switch provider {
		case "anthropic":
			if stream {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(anthropicStreamBody))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(anthropicMessageJSON))
		case "openai":
			if stream {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(openAIStreamBody))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(openAICompletionJSON))
		case "azopenai":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(azureCompletionJSON))
		}
	})
	switch provider {
	case "anthropic":
		f.hdr = "x-api-key"
		f.srv = httptest.NewServer(h)
	case "openai":
		f.hdr = "Authorization"
		f.srv = httptest.NewServer(h)
	case "azopenai":
		f.hdr = "api-key"
		f.srv = httptest.NewTLSServer(h)
	default:
		t.Fatalf("unknown provider %q", provider)
	}
	t.Cleanup(f.srv.Close)
	return f
}

// wantHeader is the credential header value a provider sends for key.
func wantHeader(provider, key string) string {
	if provider == "openai" {
		return "Bearer " + key
	}
	return key
}

// setOpenAIEnvEndpoint overrides the OPENAI_ENDPOINT value cached by init().
func setOpenAIEnvEndpoint(t *testing.T, endpoint string) {
	t.Helper()
	old := openAIEndpoint
	t.Cleanup(func() { openAIEndpoint = old })
	openAIEndpoint = endpoint
}

// populateEnv points the provider's env vars at a decoy server and key so
// tests can prove options win over env.
func populateEnv(t *testing.T, provider, decoyURL string) {
	t.Helper()
	switch provider {
	case "anthropic":
		t.Setenv("ANTHROPIC_API_KEY", "env-key")
		t.Setenv("ANTHROPIC_BASE_URL", decoyURL)
	case "openai":
		t.Setenv("OPENAI_API_KEY", "env-key")
		setOpenAIEnvEndpoint(t, decoyURL)
	case "azopenai":
		t.Setenv("AZURE_OPENAI_API_KEY", "env-key")
		t.Setenv("AZURE_OPENAI_ENDPOINT", decoyURL)
	}
}

// clientArgs returns the provider ID and options for a per-client
// configuration pointing at srv with key. azopenai's per-client endpoint is
// carried by the provider ID rather than WithBaseURL.
func clientArgs(provider, key string, srv *httptest.Server, extra ...Option) (string, []Option) {
	opts := append([]Option{WithAPIKey(key)}, extra...)
	if provider == "azopenai" {
		u, _ := url.Parse(srv.URL)
		return "azopenai://" + u.Host, append(opts, WithSkipVerifySSL())
	}
	return provider, append(opts, WithBaseURL(srv.URL))
}

// exercise sends one non-streaming and one streaming request through c.
func exercise(ctx context.Context, c Client) error {
	chat := c.StartChat("sys", "test-model")
	if _, err := chat.Send(ctx, "hi"); err != nil {
		return fmt.Errorf("Send: %w", err)
	}
	it, err := chat.SendStreaming(ctx, "hi again")
	if err != nil {
		return fmt.Errorf("SendStreaming: %w", err)
	}
	for _, err := range it {
		if err != nil {
			return fmt.Errorf("stream: %w", err)
		}
	}
	return nil
}

func assertKeys(t *testing.T, f *fakeProvider, want string) {
	t.Helper()
	got := f.seen()
	if len(got) == 0 {
		t.Fatalf("server %s received no requests", f.srv.URL)
	}
	for _, k := range got {
		if k != want {
			t.Fatalf("server %s got credential %q, want %q (all: %q)", f.srv.URL, k, want, got)
		}
	}
}

func TestWithBaseURLValidation(t *testing.T) {
	tests := []struct {
		baseURL string
		wantErr bool
	}{
		{"", false},
		{"http://127.0.0.1:8080", false},
		{"https://gateway.example.com/anthropic", false},
		{"ftp://example.com", true},
		{"example.com", true},
		{"/v1", true},
		{"https://", true},
		{"http://bad host", true},
		{"http://:8080", true},
		{"https://:443", true},
	}
	for _, tt := range tests {
		t.Run(tt.baseURL, func(t *testing.T) {
			t.Setenv("ANTHROPIC_API_KEY", "k")
			_, err := NewClient(context.Background(), "anthropic", WithBaseURL(tt.baseURL))
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewClient(WithBaseURL(%q)) err = %v, wantErr %v", tt.baseURL, err, tt.wantErr)
			}
			if tt.wantErr && !strings.Contains(err.Error(), "invalid base URL") {
				t.Fatalf("expected a clear invalid base URL error, got %v", err)
			}
			// Direct factory calls validate too.
			if _, err := NewOpenAIClient(context.Background(), ClientOptions{APIKey: "k", BaseURL: tt.baseURL}); (err != nil) != tt.wantErr {
				t.Fatalf("NewOpenAIClient(BaseURL %q) err = %v, wantErr %v", tt.baseURL, err, tt.wantErr)
			}
		})
	}
}

// TestOptionsWinOverEnv checks that per-client key and endpoint beat a fully
// populated env pointing at a decoy server, on both send paths.
func TestOptionsWinOverEnv(t *testing.T) {
	for _, provider := range []string{"anthropic", "openai", "azopenai"} {
		t.Run(provider, func(t *testing.T) {
			decoy := newFakeProvider(t, provider)
			target := newFakeProvider(t, provider)
			populateEnv(t, provider, decoy.srv.URL)

			id, opts := clientArgs(provider, "opt-key", target.srv)
			c, err := NewClient(context.Background(), id, opts...)
			if err != nil {
				t.Fatal(err)
			}
			if err := exercise(context.Background(), c); err != nil {
				t.Fatal(err)
			}
			assertKeys(t, target, wantHeader(provider, "opt-key"))
			if n := len(decoy.seen()); n != 0 {
				t.Fatalf("env-configured decoy server received %d requests", n)
			}
		})
	}
}

func TestGeminiWithAPIKeyWinsOverEnv(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "env-key")
	c, err := NewClient(context.Background(), "gemini", WithAPIKey("opt-key"))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.(*GoogleAIClient).client.ClientConfig().APIKey; got != "opt-key" {
		t.Fatalf("gemini API key = %q, want option key", got)
	}
}

// TestConcurrentClientsAreIsolated builds two clients at once with different
// keys and endpoints; each must reach only its own server with its own key.
// Run with -race.
func TestConcurrentClientsAreIsolated(t *testing.T) {
	for _, provider := range []string{"anthropic", "openai", "azopenai"} {
		t.Run(provider, func(t *testing.T) {
			decoy := newFakeProvider(t, provider)
			populateEnv(t, provider, decoy.srv.URL)
			servers := map[string]*fakeProvider{
				"key-a": newFakeProvider(t, provider),
				"key-b": newFakeProvider(t, provider),
			}

			var wg sync.WaitGroup
			errs := make(chan error, len(servers))
			for key, f := range servers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					id, opts := clientArgs(provider, key, f.srv)
					c, err := NewClient(context.Background(), id, opts...)
					if err != nil {
						errs <- err
						return
					}
					for i := 0; i < 3; i++ {
						if err := exercise(context.Background(), c); err != nil {
							errs <- fmt.Errorf("%s: %w", key, err)
							return
						}
					}
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Fatal(err)
			}
			for key, f := range servers {
				assertKeys(t, f, wantHeader(provider, key))
			}
			if n := len(decoy.seen()); n != 0 {
				t.Fatalf("env-configured decoy server received %d requests", n)
			}
		})
	}
}

// TestLegacyEnvOnlyPath checks that without per-client options the env-driven
// configuration is used exactly as before.
func TestLegacyEnvOnlyPath(t *testing.T) {
	for _, provider := range []string{"anthropic", "openai", "azopenai"} {
		t.Run(provider, func(t *testing.T) {
			envSrv := newFakeProvider(t, provider)
			populateEnv(t, provider, envSrv.srv.URL)
			var opts []Option
			if provider == "azopenai" {
				opts = append(opts, WithSkipVerifySSL())
			}
			c, err := NewClient(context.Background(), provider, opts...)
			if err != nil {
				t.Fatal(err)
			}
			if err := exercise(context.Background(), c); err != nil {
				t.Fatal(err)
			}
			assertKeys(t, envSrv, wantHeader(provider, "env-key"))
		})
	}

	t.Run("anthropic default base URL", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", "k")
		t.Setenv("ANTHROPIC_BASE_URL", "")
		c, err := NewAnthropicClient(context.Background(), ClientOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got := c.baseURL.String(); got != defaultAnthropicBaseURL {
			t.Fatalf("baseURL = %q, want %q", got, defaultAnthropicBaseURL)
		}
	})
}
