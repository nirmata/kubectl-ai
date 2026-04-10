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
	"reflect"
	"testing"
)

// TestIsClaude46Family covers the exported model-gating predicate. The
// Bedrock provider and any external decorators (e.g. webserver wrappers
// that inject extended thinking) rely on this to decide whether to forward
// the `thinking` and `output_config` fields. Getting it wrong is a bug in
// either direction:
//
//   - false positive (matching a non-4.6 model) → Bedrock returns an error
//     when output_config.effort is forwarded
//   - false negative (missing a real 4.6 model) → thinking is silently never
//     enabled and the user wonders why responses don't have thinking blocks
//
// Both directions are tested here. Adding a new 4.6 family member to
// claude46ShortNames should make new "should match" cases pass without any
// other code change — that's the centralization payoff.
func TestIsClaude46Family(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  bool
	}{
		// Should match — all the forms callers might pass
		{"short alias sonnet 4.6", "claude-sonnet-4-6", true},
		{"short alias opus 4.6", "claude-opus-4-6", true},
		{"global cross-region sonnet", "global.anthropic.claude-sonnet-4-6", true},
		{"global cross-region opus", "global.anthropic.claude-opus-4-6", true},
		{"US regional CRIS sonnet", "us.anthropic.claude-sonnet-4-6", true},
		{"EU regional CRIS opus", "eu.anthropic.claude-opus-4-6", true},
		{"base regional sonnet", "anthropic.claude-sonnet-4-6", true},

		// Must NOT match — these are real Bedrock models that would error
		// on output_config.effort
		{"empty string", "", false},
		{"sonnet 4 (non-6)", "us.anthropic.claude-sonnet-4-20250514-v1:0", false},
		{"sonnet 3.7", "us.anthropic.claude-3-7-sonnet-20250219-v1:0", false},
		{"sonnet 4.5", "us.anthropic.claude-sonnet-4-5-20250929-v1:0", false},
		{"opus 4.5", "us.anthropic.claude-opus-4-5-20251101-v1:0", false},
		{"haiku 4.5", "us.anthropic.claude-haiku-4-5-20251001-v1:0", false},
		{"nova pro", "us.amazon.nova-pro-v1:0", false},
		{"completely unrelated", "llama-3-70b", false},

		// Edge cases
		{"prefix-only must not match", "claude-sonnet", false},
		{"hypothetical 4-7 must not match 4-6", "claude-sonnet-4-7", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsClaude46Family(tt.model)
			if got != tt.want {
				t.Errorf("IsClaude46Family(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

// TestGetBedrockModel_Aliases verifies that the short model aliases for the
// 4.6 family resolve to their fully-qualified Bedrock IDs, while full IDs
// continue to pass through unchanged.
func TestGetBedrockModel_Aliases(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "sonnet 4.6 alias resolves to global ID",
			in:   "claude-sonnet-4-6",
			want: "global.anthropic.claude-sonnet-4-6",
		},
		{
			name: "opus 4.6 alias resolves to global ID",
			in:   "claude-opus-4-6",
			want: "global.anthropic.claude-opus-4-6",
		},
		{
			name: "fully-qualified Sonnet 4 ID passes through",
			in:   "us.anthropic.claude-sonnet-4-20250514-v1:0",
			want: "us.anthropic.claude-sonnet-4-20250514-v1:0",
		},
		{
			name: "fully-qualified Sonnet 4.6 ID passes through unchanged",
			in:   "global.anthropic.claude-sonnet-4-6",
			want: "global.anthropic.claude-sonnet-4-6",
		},
		{
			name: "unknown short name passes through (Bedrock will reject)",
			in:   "claude-not-a-real-model",
			want: "claude-not-a-real-model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getBedrockModel(tt.in)
			if got != tt.want {
				t.Errorf("getBedrockModel(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestBedrockChat_SetThinkingConfig_BuildsExtras is the load-bearing wire-
// payload test. It exercises the SetThinkingConfig flow end-to-end:
//
//  1. Caller invokes SetThinkingConfig with the desired mode/effort
//  2. The setter populates extendedThinkingExtras as the raw map (this is
//     what we assert on, because the smithy-go document codec only
//     unmarshals into typed structs, not generic maps)
//  3. The setter caches the wrapped smithy document on c.thinkingExtras
//     for Send/SendStreaming to use on every turn without rebuilding
//
// We assert both the raw map shape AND the cached-document nil-ness so
// the contract Send/SendStreaming relies on stays locked in:
// thinkingExtras is non-nil exactly when there are extras to send.
//
// If this test breaks, the on-the-wire JSON to Bedrock changed, which means
// callers depending on adaptive thinking will silently regress.
func TestBedrockChat_SetThinkingConfig_BuildsExtras(t *testing.T) {
	tests := []struct {
		name         string
		thinkingMode string
		effort       string
		want         map[string]any // nil ⇒ no extras (default Bedrock behavior)
	}{
		{
			name:         "both unset returns nil (preserves default Bedrock behavior)",
			thinkingMode: "",
			effort:       "",
			want:         nil,
		},
		{
			name:         "adaptive thinking only",
			thinkingMode: "adaptive",
			effort:       "",
			want: map[string]any{
				"thinking": map[string]any{"type": "adaptive"},
			},
		},
		{
			name:         "effort only",
			thinkingMode: "",
			effort:       "medium",
			want: map[string]any{
				"output_config": map[string]any{"effort": "medium"},
			},
		},
		{
			name:         "adaptive thinking with medium effort (recommended Sonnet 4.6 default)",
			thinkingMode: "adaptive",
			effort:       "medium",
			want: map[string]any{
				"thinking":      map[string]any{"type": "adaptive"},
				"output_config": map[string]any{"effort": "medium"},
			},
		},
		{
			name:         "thinking disabled",
			thinkingMode: "disabled",
			effort:       "",
			want: map[string]any{
				"thinking": map[string]any{"type": "disabled"},
			},
		},
		{
			name:         "max effort with adaptive (Opus 4.6 high-stakes default)",
			thinkingMode: "adaptive",
			effort:       "max",
			want: map[string]any{
				"thinking":      map[string]any{"type": "adaptive"},
				"output_config": map[string]any{"effort": "max"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chat := &bedrockChat{}
			chat.SetThinkingConfig(tt.thinkingMode, tt.effort)

			gotExtras := chat.extendedThinkingExtras()
			if !reflect.DeepEqual(gotExtras, tt.want) {
				t.Errorf("extendedThinkingExtras mismatch\n got:  %#v\n want: %#v", gotExtras, tt.want)
			}

			// thinkingExtras is what Send/SendStreaming actually use; assert
			// the cached document is non-nil exactly when there are extras.
			if (tt.want == nil) != (chat.thinkingExtras == nil) {
				t.Errorf("thinkingExtras nil-ness mismatch: extras=%v cached doc nil? %v",
					tt.want, chat.thinkingExtras == nil)
			}
		})
	}
}

// TestBedrockChat_SetThinkingConfig_Clears verifies that calling
// SetThinkingConfig with empty strings clears a previously-cached document.
// This guards against a stale document being sent on a chat that was
// configured and then explicitly reset to default behavior.
func TestBedrockChat_SetThinkingConfig_Clears(t *testing.T) {
	chat := &bedrockChat{}

	chat.SetThinkingConfig("adaptive", "medium")
	if chat.thinkingExtras == nil {
		t.Fatal("expected non-nil thinkingExtras after configuring")
	}

	chat.SetThinkingConfig("", "")
	if chat.thinkingExtras != nil {
		t.Errorf("expected thinkingExtras to be cleared after SetThinkingConfig(\"\", \"\"), got %v", chat.thinkingExtras)
	}
	if chat.thinkingMode != "" || chat.effort != "" {
		t.Errorf("expected mode/effort cleared, got mode=%q effort=%q", chat.thinkingMode, chat.effort)
	}
}
