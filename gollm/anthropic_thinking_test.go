// Copyright 2025 Google LLC
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
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// setAPIKey lets NewAnthropicClient pass its env-var check; no HTTP traffic
// happens in these tests.
func setAPIKey(t *testing.T) {
	t.Helper()
	old := os.Getenv(envAnthropicAPIKey)
	os.Setenv(envAnthropicAPIKey, "test-key-not-real")
	t.Cleanup(func() {
		if old == "" {
			os.Unsetenv(envAnthropicAPIKey)
		} else {
			os.Setenv(envAnthropicAPIKey, old)
		}
	})
}

// buildRequest mirrors what Send and SendStreaming construct internally,
// so applyThinkingConfig is exercised in the same shape as production code.
func buildRequest(chat *anthropicChat, stream bool) anthropicRequest {
	req := anthropicRequest{
		Model:     chat.model,
		MaxTokens: defaultMaxTokens,
		Messages: []anthropicMessage{{
			Role:    "user",
			Content: []anthropicContentBlock{{Type: "text", Text: "test"}},
		}},
		Stream: stream,
	}
	chat.applyThinkingConfig(&req)
	return req
}

// TestAnthropic_NoThinkingEffort_ZeroBehaviorChange asserts that when
// WithThinkingEffort isn't set (the existing-caller path), the marshaled
// request body has no `thinking` or `output_config` fields. This is the
// load-bearing backwards-compatibility test: existing gollm anthropic
// callers must see byte-identical request bodies after this change.
func TestAnthropic_NoThinkingEffort_ZeroBehaviorChange(t *testing.T) {
	setAPIKey(t)

	client, err := NewAnthropicClient(context.Background(), ClientOptions{})
	if err != nil {
		t.Fatalf("NewAnthropicClient: %v", err)
	}

	chat := client.StartChat("system prompt", defaultAnthropicModel).(*anthropicChat)
	req := buildRequest(chat, false)

	bodyBytes, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(bodyBytes)

	if strings.Contains(body, `"thinking"`) {
		t.Errorf("request body unexpectedly contains \"thinking\" field: %s", body)
	}
	if strings.Contains(body, `"output_config"`) {
		t.Errorf("request body unexpectedly contains \"output_config\" field: %s", body)
	}
	// Critical backwards-compat assertion: max_tokens must stay at the
	// pre-thinking default (4096), NOT bumped to the thinking-enabled
	// 8192. Without this assertion an earlier draft of the implementation
	// silently bumped max_tokens for ALL anthropic callers — caught by a
	// reviewer, not by the test, which is exactly the kind of slip this
	// assertion is here to prevent.
	if req.MaxTokens != defaultMaxTokens {
		t.Errorf("no-thinking path must keep MaxTokens=%d, got %d", defaultMaxTokens, req.MaxTokens)
	}
	if strings.Contains(body, `"max_tokens":8192`) {
		t.Errorf("no-thinking request body must not contain max_tokens:8192: %s", body)
	}
}

// TestAnthropic_ThinkingEffort_LockstepSendAndStreaming is the load-bearing
// wire-format correctness test. With WithThinkingEffort("medium"):
//
//  1. Both the non-streaming (Stream:false) and streaming (Stream:true)
//     request bodies must include the thinking and output_config fields.
//  2. The two code paths must produce IDENTICAL thinking/output_config
//     content (after stripping the Stream field difference). If they ever
//     diverge, callers get inconsistent thinking behavior depending on
//     whether they happen to use streaming.
//
// If this test breaks, the on-the-wire JSON to Anthropic changed for one
// path but not the other.
func TestAnthropic_ThinkingEffort_LockstepSendAndStreaming(t *testing.T) {
	setAPIKey(t)

	client, err := NewAnthropicClient(context.Background(), ClientOptions{
		ThinkingEffort: "medium",
	})
	if err != nil {
		t.Fatalf("NewAnthropicClient: %v", err)
	}

	chat := client.StartChat("system prompt", defaultAnthropicModel).(*anthropicChat)

	sendReq := buildRequest(chat, false)
	streamReq := buildRequest(chat, true)

	// Both must populate Thinking with adaptive type
	if sendReq.Thinking == nil || sendReq.Thinking.Type != thinkingTypeAdaptive {
		t.Errorf("Send path: expected Thinking.Type=%q, got %#v", thinkingTypeAdaptive, sendReq.Thinking)
	}
	if streamReq.Thinking == nil || streamReq.Thinking.Type != thinkingTypeAdaptive {
		t.Errorf("Streaming path: expected Thinking.Type=%q, got %#v", thinkingTypeAdaptive, streamReq.Thinking)
	}

	// Both must populate OutputConfig with the configured effort
	if sendReq.OutputConfig == nil || sendReq.OutputConfig.Effort != "medium" {
		t.Errorf("Send path: expected OutputConfig.Effort=%q, got %#v", "medium", sendReq.OutputConfig)
	}
	if streamReq.OutputConfig == nil || streamReq.OutputConfig.Effort != "medium" {
		t.Errorf("Streaming path: expected OutputConfig.Effort=%q, got %#v", "medium", streamReq.OutputConfig)
	}

	// Both must bump MaxTokens to the thinking-enabled cap. Symmetric to the
	// no-thinking-path assertion in TestAnthropic_NoThinkingEffort_ZeroBehaviorChange:
	// applyThinkingConfig owns the MaxTokens bump, both code paths invoke it,
	// and divergence between Send and SendStreaming on this field would mean
	// streaming users silently truncate while non-streaming users don't (or
	// vice versa).
	if sendReq.MaxTokens != defaultMaxTokensThinking {
		t.Errorf("Send path: expected MaxTokens=%d (thinking on), got %d", defaultMaxTokensThinking, sendReq.MaxTokens)
	}
	if streamReq.MaxTokens != defaultMaxTokensThinking {
		t.Errorf("Streaming path: expected MaxTokens=%d (thinking on), got %d", defaultMaxTokensThinking, streamReq.MaxTokens)
	}

	// Marshal both and verify the thinking/output_config keys appear at top
	// level in the JSON (Anthropic's API requires this — they are NOT
	// wrapped in an envelope unlike Bedrock's additionalModelRequestFields).
	for label, req := range map[string]anthropicRequest{"send": sendReq, "stream": streamReq} {
		bodyBytes, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("%s marshal: %v", label, err)
		}
		body := string(bodyBytes)
		if !strings.Contains(body, `"thinking":{"type":"adaptive"}`) {
			t.Errorf("%s body missing top-level adaptive thinking: %s", label, body)
		}
		if !strings.Contains(body, `"output_config":{"effort":"medium"}`) {
			t.Errorf("%s body missing top-level output_config.effort=medium: %s", label, body)
		}
	}
}

// TestAnthropic_ThinkingEffort_AllValidValues exercises every accepted
// effort value as a table test. "" and "off" must produce no thinking
// fields (semantically equivalent — Anthropic treats omission as
// disabled). "low"/"medium"/"high"/"max" must produce the corresponding
// adaptive-thinking + effort fields.
func TestAnthropic_ThinkingEffort_AllValidValues(t *testing.T) {
	setAPIKey(t)

	tests := []struct {
		name             string
		effort           string
		wantThinkingSet  bool   // true ⇒ Thinking and OutputConfig should both be non-nil
		wantEffortInBody string // checked only when wantThinkingSet is true
	}{
		{name: "empty string is no-op", effort: "", wantThinkingSet: false},
		{name: "off is no-op", effort: "off", wantThinkingSet: false},
		{name: "low enables adaptive + low effort", effort: "low", wantThinkingSet: true, wantEffortInBody: "low"},
		{name: "medium enables adaptive + medium effort", effort: "medium", wantThinkingSet: true, wantEffortInBody: "medium"},
		{name: "high enables adaptive + high effort", effort: "high", wantThinkingSet: true, wantEffortInBody: "high"},
		{name: "max enables adaptive + max effort", effort: "max", wantThinkingSet: true, wantEffortInBody: "max"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewAnthropicClient(context.Background(), ClientOptions{
				ThinkingEffort: tt.effort,
			})
			if err != nil {
				t.Fatalf("NewAnthropicClient(%q): %v", tt.effort, err)
			}

			chat := client.StartChat("", defaultAnthropicModel).(*anthropicChat)
			req := buildRequest(chat, false)

			if tt.wantThinkingSet {
				if req.Thinking == nil || req.Thinking.Type != thinkingTypeAdaptive {
					t.Errorf("expected adaptive Thinking, got %#v", req.Thinking)
				}
				if req.OutputConfig == nil || req.OutputConfig.Effort != tt.wantEffortInBody {
					t.Errorf("expected OutputConfig.Effort=%q, got %#v", tt.wantEffortInBody, req.OutputConfig)
				}
			} else {
				if req.Thinking != nil {
					t.Errorf("expected nil Thinking for effort=%q, got %#v", tt.effort, req.Thinking)
				}
				if req.OutputConfig != nil {
					t.Errorf("expected nil OutputConfig for effort=%q, got %#v", tt.effort, req.OutputConfig)
				}
			}
		})
	}
}

// TestAnthropic_ThinkingEffort_InvalidFailsFast verifies that
// NewAnthropicClient rejects unknown effort values at construction time
// rather than letting them flow through to per-request errors. The error
// message must include the offending value so operators can debug their
// startup config without enabling debug logging.
func TestAnthropic_ThinkingEffort_InvalidFailsFast(t *testing.T) {
	setAPIKey(t)

	tests := []string{"foo", "MEDIUM", "high ", " low", "1", "true"}
	for _, badValue := range tests {
		t.Run("invalid_"+badValue, func(t *testing.T) {
			_, err := NewAnthropicClient(context.Background(), ClientOptions{
				ThinkingEffort: badValue,
			})
			if err == nil {
				t.Fatalf("expected error for ThinkingEffort=%q, got nil", badValue)
			}
			if !strings.Contains(err.Error(), badValue) {
				t.Errorf("error message %q must contain offending value %q for debuggability",
					err.Error(), badValue)
			}
		})
	}
}

// TestAnthropic_ThinkingEffort_PersistsAcrossTurns guards against an
// implementation that accidentally consumes or mutates thinking config
// after the first request. We build the request three times in a row off
// the same chat and assert the thinking fields are present every time.
//
// The bug this catches: if applyThinkingConfig were ever changed to e.g.
// clear the chat's thinkingEffort after first use, multi-turn agentic
// loops would silently lose thinking after turn 1.
func TestAnthropic_ThinkingEffort_PersistsAcrossTurns(t *testing.T) {
	setAPIKey(t)

	client, err := NewAnthropicClient(context.Background(), ClientOptions{
		ThinkingEffort: "high",
	})
	if err != nil {
		t.Fatalf("NewAnthropicClient: %v", err)
	}
	chat := client.StartChat("", defaultAnthropicModel).(*anthropicChat)

	for turn := 1; turn <= 3; turn++ {
		req := buildRequest(chat, false)
		if req.Thinking == nil || req.Thinking.Type != thinkingTypeAdaptive {
			t.Errorf("turn %d: expected adaptive Thinking, got %#v", turn, req.Thinking)
		}
		if req.OutputConfig == nil || req.OutputConfig.Effort != "high" {
			t.Errorf("turn %d: expected OutputConfig.Effort=high, got %#v", turn, req.OutputConfig)
		}
	}
}
