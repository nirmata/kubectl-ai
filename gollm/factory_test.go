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
	"strings"
	"testing"
)

func TestNewClient(t *testing.T) {
	_, err := NewClient(context.Background(), "gemini")
	if err == nil || err.Error() != "GEMINI_API_KEY environment variable not set" {
		t.Fatalf("Unexpected error: %v", err)
	}

	_, err = NewClient(context.Background(), "invalid")
	if err == nil || !strings.Contains(err.Error(), "provider \"invalid\" not registered") {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestWithAPIKey(t *testing.T) {
	var opts ClientOptions
	WithAPIKey("sk-test-123")(&opts)
	if opts.APIKey != "sk-test-123" {
		t.Fatalf("WithAPIKey did not set APIKey: got %q, want %q", opts.APIKey, "sk-test-123")
	}
}

func TestAPIKeyEnvVar(t *testing.T) {
	tests := map[string]string{
		"openai":    "OPENAI_API_KEY",
		"anthropic": "ANTHROPIC_API_KEY",
		"azopenai":  "AZURE_OPENAI_API_KEY",
		"gemini":    "GEMINI_API_KEY",
		"unknown":   "",
	}
	for providerID, want := range tests {
		if got := APIKeyEnvVar(providerID); got != want {
			t.Errorf("APIKeyEnvVar(%q) = %q, want %q", providerID, got, want)
		}
	}
}

func TestAnthropicFactoryPrefersWithAPIKeyOverEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "env-key-should-lose")
	c, err := NewClient(context.Background(), "anthropic", WithAPIKey("sk-ant-from-option"))
	if err != nil {
		t.Fatalf("expected client to build from WithAPIKey, got error: %v", err)
	}
	ac, ok := c.(*AnthropicClient)
	if !ok {
		t.Fatalf("expected *AnthropicClient, got %T", c)
	}
	if ac.apiKey != "sk-ant-from-option" {
		t.Fatalf("expected option key to win over env, got apiKey %q", ac.apiKey)
	}
}
