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
	"testing"
)

func TestAnthropicThinkingOptions(t *testing.T) {
	for _, tc := range []struct {
		name, thinking, effort string
		wantThinking           any
		wantEffort             any
	}{
		{name: "unset"},
		{name: "disabled", thinking: "disabled", wantThinking: "disabled"},
		{name: "between_tools", thinking: "between_tools", wantThinking: "between_tools"},
		{name: "effort only", effort: "xhigh", wantEffort: "xhigh"},
		{name: "both", thinking: "adaptive", effort: "low", wantThinking: "adaptive", wantEffort: "low"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ANTHROPIC_MAX_TOKENS", "")
			// exercise sends one Send, then one SendStreaming.
			f := newFakeAnthropic(t, reply("end_turn", `{"type":"text","text":"ok"}`), sse(msgDelta("end_turn"), evMsgStop))
			c, err := NewAnthropicClient(context.Background(), ClientOptions{
				AnthropicThinkingType: tc.thinking,
				AnthropicEffort:       tc.effort,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := exercise(context.Background(), c); err != nil {
				t.Fatal(err)
			}
			for i, body := range f.bodies { // 0: Send, 1: SendStreaming
				var gotThinking, gotEffort any
				if m, ok := body["thinking"].(map[string]any); ok {
					gotThinking = m["type"]
				} else if _, present := body["thinking"]; present {
					t.Fatalf("request %d: thinking is not an object: %v", i, body["thinking"])
				}
				if m, ok := body["output_config"].(map[string]any); ok {
					gotEffort = m["effort"]
				} else if _, present := body["output_config"]; present {
					t.Fatalf("request %d: output_config is not an object: %v", i, body["output_config"])
				}
				if gotThinking != tc.wantThinking || gotEffort != tc.wantEffort {
					t.Fatalf("request %d: thinking=%v effort=%v, want %v and %v", i, gotThinking, gotEffort, tc.wantThinking, tc.wantEffort)
				}
				if got := body["max_tokens"]; got != float64(defaultAnthropicMaxTokens) {
					t.Fatalf("request %d: max_tokens=%v, want %d", i, got, defaultAnthropicMaxTokens)
				}
			}
		})
	}
}

func TestAnthropicThinkingOptionsRejectBadValues(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "env-key")
	for _, opts := range []ClientOptions{
		{AnthropicThinkingType: "enabled"},
		{AnthropicThinkingType: "off"},
		{AnthropicEffort: "none"},
		{AnthropicEffort: "HIGH"},
	} {
		if _, err := NewAnthropicClient(context.Background(), opts); err == nil {
			t.Errorf("%+v: want an error", opts)
		}
	}
}

func TestAnthropicThinkingOptionsViaNewClient(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "env-key")
	for _, opt := range []Option{WithAnthropicThinkingType("enabled"), WithAnthropicEffort("HIGH")} {
		if _, err := NewClient(context.Background(), "anthropic", opt); err == nil {
			t.Error("NewClient accepted an invalid anthropic thinking option")
		}
	}
	if _, err := NewClient(context.Background(), "anthropic", WithAnthropicThinkingType("disabled"), WithAnthropicEffort("max")); err != nil {
		t.Fatal(err)
	}
}

// A 400 (a thinking value the model rejects) is not retried.
func TestAnthropicThinking400NotRetried(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"thinking.type.disabled is not supported"}}`))
	}))
	t.Cleanup(srv.Close)
	populateEnv(t, "anthropic", srv.URL)
	c, err := NewAnthropicClient(context.Background(), ClientOptions{AnthropicThinkingType: "disabled"})
	if err != nil {
		t.Fatal(err)
	}
	chat := c.StartChat("", "claude-test")
	_, err = chat.Send(context.Background(), "hi")
	if err == nil {
		t.Fatal("want an error")
	}
	if chat.IsRetryableError(err) {
		t.Fatalf("400 must not be retryable: %v", err)
	}
	if n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}
}
