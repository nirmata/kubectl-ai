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
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

type headerAdder struct {
	base http.RoundTripper
	hits *atomic.Int32
}

func (h headerAdder) RoundTrip(r *http.Request) (*http.Response, error) {
	h.hits.Add(1)
	r = r.Clone(r.Context())
	r.Header.Set("X-Wrapped", "yes")
	return h.base.RoundTrip(r)
}

// TestTransportWrapper checks, for each provider that uses the shared HTTP
// client, that the wrapper sees the request and that gollm's own transport
// still runs underneath. The server is TLS with a self-signed cert, so the
// request only succeeds if the base transport's SkipVerifySSL setting applies.
func TestTransportWrapper(t *testing.T) {
	var gotHeader atomic.Value
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader.Store(r.Header.Get("X-Wrapped"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer srv.Close()

	var hits atomic.Int32
	var base http.RoundTripper
	opts := ClientOptions{
		SkipVerifySSL: true,
		WrapTransport: func(b http.RoundTripper) http.RoundTripper {
			base = b
			return headerAdder{base: b, hits: &hits}
		},
	}

	cases := map[string]func(t *testing.T){
		"openai": func(t *testing.T) {
			oldKey, oldEP := openAIAPIKey, openAIEndpoint
			t.Cleanup(func() { openAIAPIKey, openAIEndpoint = oldKey, oldEP })
			openAIAPIKey, openAIEndpoint = "x", srv.URL
			c, err := NewOpenAIClient(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = c.client.Models.List(context.Background()) // response shape is irrelevant
		},
		"grok": func(t *testing.T) {
			t.Setenv("GROK_API_KEY", "x")
			t.Setenv("GROK_ENDPOINT", srv.URL)
			c, err := NewGrokClient(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = c.client.Models.List(context.Background())
		},
		"anthropic": func(t *testing.T) {
			t.Setenv("ANTHROPIC_API_KEY", "x")
			t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
			c, err := NewAnthropicClient(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := c.httpClient.Get(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
		},
		"llamacpp": func(t *testing.T) {
			t.Setenv("LLAMACPP_HOST", srv.URL)
			c, err := NewLlamaCppClient(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := c.httpClient.Get(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			hits.Store(0)
			gotHeader.Store("")
			base = nil
			run(t)
			if hits.Load() == 0 {
				t.Fatal("wrapper was not invoked")
			}
			if gotHeader.Load() != "yes" {
				t.Fatalf("server did not see wrapper header: %q", gotHeader.Load())
			}
			// The server cert is self-signed: reaching it proves the base's
			// SkipVerifySSL TLS config ran underneath the wrapper.
			tr, ok := base.(*http.Transport)
			if !ok || tr == http.DefaultTransport || tr.Proxy == nil {
				t.Fatalf("wrapper did not receive gollm's cloned transport: %T", base)
			}
		})
	}
}

func TestWithTransportWrapper(t *testing.T) {
	var o ClientOptions
	WithTransportWrapper(func(b http.RoundTripper) http.RoundTripper { return b })(&o)
	if o.WrapTransport == nil {
		t.Fatal("option did not set WrapTransport")
	}
}
