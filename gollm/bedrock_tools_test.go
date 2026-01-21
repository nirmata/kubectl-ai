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
	"encoding/json"
	"testing"

	"github.com/GoogleCloudPlatform/kubectl-ai/pkg/api"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

func TestCreateToolUseFromPayload(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		wantErr bool
		check   func(t *testing.T, result *types.ToolUseBlock)
	}{
		{
			name: "valid tool use with arguments map",
			payload: map[string]any{
				"id":   "call-123",
				"name": "kubectl_get",
				"arguments": map[string]any{
					"resource": "pods",
					"flags":    []string{"-n", "default"},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *types.ToolUseBlock) {
				if result == nil {
					t.Fatal("expected non-nil result")
				}
				if aws.ToString(result.ToolUseId) != "call-123" {
					t.Errorf("expected tool use id 'call-123', got %s", aws.ToString(result.ToolUseId))
				}
				if aws.ToString(result.Name) != "kubectl_get" {
					t.Errorf("expected name 'kubectl_get', got %s", aws.ToString(result.Name))
				}
			},
		},
		{
			name: "valid tool use with arguments as JSON string",
			payload: map[string]any{
				"id":        "call-456",
				"name":      "shell_exec",
				"arguments": `{"command":"ls -la"}`,
			},
			wantErr: false,
			check: func(t *testing.T, result *types.ToolUseBlock) {
				if result == nil {
					t.Fatal("expected non-nil result")
				}
				if aws.ToString(result.ToolUseId) != "call-456" {
					t.Errorf("expected tool use id 'call-456', got %s", aws.ToString(result.ToolUseId))
				}
			},
		},
		{
			name: "missing tool use id",
			payload: map[string]any{
				"name":      "kubectl_get",
				"arguments": map[string]any{},
			},
			wantErr: true,
		},
		{
			name: "empty tool use id",
			payload: map[string]any{
				"id":        "",
				"name":      "kubectl_get",
				"arguments": map[string]any{},
			},
			wantErr: true,
		},
		{
			name: "missing tool name",
			payload: map[string]any{
				"id":        "call-789",
				"arguments": map[string]any{},
			},
			wantErr: true,
		},
		{
			name: "empty tool name",
			payload: map[string]any{
				"id":        "call-789",
				"name":      "",
				"arguments": map[string]any{},
			},
			wantErr: true,
		},
		{
			name: "no arguments provided",
			payload: map[string]any{
				"id":   "call-000",
				"name": "simple_tool",
			},
			wantErr: false,
			check: func(t *testing.T, result *types.ToolUseBlock) {
				if result == nil {
					t.Fatal("expected non-nil result")
				}
				// Should have empty args map
			},
		},
	}

	cs := &bedrockChat{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := cs.createToolUseFromPayload(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("createToolUseFromPayload() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestCreateToolResultFromPayload(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		wantErr bool
		check   func(t *testing.T, result *types.ToolResultBlock)
	}{
		{
			name: "successful tool result with map",
			payload: map[string]any{
				"id": "call-123",
				"result": map[string]any{
					"output": "pod1\npod2\npod3",
					"status": "success",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *types.ToolResultBlock) {
				if result == nil {
					t.Fatal("expected non-nil result")
				}
				if aws.ToString(result.ToolUseId) != "call-123" {
					t.Errorf("expected tool use id 'call-123', got %s", aws.ToString(result.ToolUseId))
				}
				if result.Status != types.ToolResultStatusSuccess {
					t.Errorf("expected status Success, got %s", result.Status)
				}
				if len(result.Content) == 0 {
					t.Error("expected non-empty content")
				}
			},
		},
		{
			name: "tool result with error status",
			payload: map[string]any{
				"id":     "call-456",
				"status": "error",
				"result": map[string]any{
					"error": "command not found",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *types.ToolResultBlock) {
				if result == nil {
					t.Fatal("expected non-nil result")
				}
				if result.Status != types.ToolResultStatusError {
					t.Errorf("expected status Error, got %s", result.Status)
				}
			},
		},
		{
			name: "tool result with failed status",
			payload: map[string]any{
				"id":     "call-789",
				"status": "failed",
				"result": map[string]any{
					"message": "operation failed",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *types.ToolResultBlock) {
				if result == nil {
					t.Fatal("expected non-nil result")
				}
				if result.Status != types.ToolResultStatusError {
					t.Errorf("expected status Error, got %s", result.Status)
				}
			},
		},
		{
			name: "tool result with non-map result (wrapped)",
			payload: map[string]any{
				"id":     "call-000",
				"result": "simple string result",
			},
			wantErr: false,
			check: func(t *testing.T, result *types.ToolResultBlock) {
				if result == nil {
					t.Fatal("expected non-nil result")
				}
				// Non-map results should be wrapped
			},
		},
		{
			name: "missing tool use id",
			payload: map[string]any{
				"result": map[string]any{"output": "test"},
			},
			wantErr: true,
		},
		{
			name: "empty tool use id",
			payload: map[string]any{
				"id":     "",
				"result": map[string]any{"output": "test"},
			},
			wantErr: true,
		},
		{
			name: "no result provided",
			payload: map[string]any{
				"id": "call-999",
			},
			wantErr: false,
			check: func(t *testing.T, result *types.ToolResultBlock) {
				if result == nil {
					t.Fatal("expected non-nil result")
				}
				// Should have empty result
			},
		},
	}

	cs := &bedrockChat{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := cs.createToolResultFromPayload(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("createToolResultFromPayload() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestProcessAPIMessage(t *testing.T) {
	tests := []struct {
		name      string
		message   *api.Message
		wantErr   bool
		wantCount int
		check     func(t *testing.T, blocks []types.ContentBlock)
	}{
		{
			name: "text message",
			message: &api.Message{
				ID:      "msg-1",
				Source:  api.MessageSourceUser,
				Type:    api.MessageTypeText,
				Payload: "Hello, world!",
			},
			wantErr:   false,
			wantCount: 1,
			check: func(t *testing.T, blocks []types.ContentBlock) {
				if len(blocks) != 1 {
					t.Fatalf("expected 1 content block, got %d", len(blocks))
				}
				if _, ok := blocks[0].(*types.ContentBlockMemberText); !ok {
					t.Errorf("expected ContentBlockMemberText, got %T", blocks[0])
				}
			},
		},
		{
			name: "empty text message",
			message: &api.Message{
				ID:      "msg-2",
				Source:  api.MessageSourceUser,
				Type:    api.MessageTypeText,
				Payload: "",
			},
			wantErr:   false,
			wantCount: 0,
		},
		{
			name: "tool call request",
			message: &api.Message{
				ID:     "msg-3",
				Source: api.MessageSourceModel,
				Type:   api.MessageTypeToolCallRequest,
				Payload: map[string]any{
					"id":   "call-123",
					"name": "kubectl_get",
					"arguments": map[string]any{
						"resource": "pods",
					},
				},
			},
			wantErr:   false,
			wantCount: 1,
			check: func(t *testing.T, blocks []types.ContentBlock) {
				if len(blocks) != 1 {
					t.Fatalf("expected 1 content block, got %d", len(blocks))
				}
				if _, ok := blocks[0].(*types.ContentBlockMemberToolUse); !ok {
					t.Errorf("expected ContentBlockMemberToolUse, got %T", blocks[0])
				}
			},
		},
		{
			name: "tool call request with invalid payload",
			message: &api.Message{
				ID:      "msg-4",
				Source:  api.MessageSourceModel,
				Type:    api.MessageTypeToolCallRequest,
				Payload: "invalid-not-a-map",
			},
			wantErr:   false,
			wantCount: 0,
		},
		{
			name: "tool call response",
			message: &api.Message{
				ID:     "msg-5",
				Source: api.MessageSourceUser,
				Type:   api.MessageTypeToolCallResponse,
				Payload: map[string]any{
					"id": "call-123",
					"result": map[string]any{
						"output": "pod1\npod2",
					},
				},
			},
			wantErr:   false,
			wantCount: 1,
			check: func(t *testing.T, blocks []types.ContentBlock) {
				if len(blocks) != 1 {
					t.Fatalf("expected 1 content block, got %d", len(blocks))
				}
				if _, ok := blocks[0].(*types.ContentBlockMemberToolResult); !ok {
					t.Errorf("expected ContentBlockMemberToolResult, got %T", blocks[0])
				}
			},
		},
		{
			name: "tool call response with invalid payload",
			message: &api.Message{
				ID:      "msg-6",
				Source:  api.MessageSourceUser,
				Type:    api.MessageTypeToolCallResponse,
				Payload: "invalid-not-a-map",
			},
			wantErr:   false,
			wantCount: 0,
		},
		{
			name: "nil payload",
			message: &api.Message{
				ID:      "msg-7",
				Source:  api.MessageSourceUser,
				Type:    api.MessageTypeText,
				Payload: nil,
			},
			wantErr:   false,
			wantCount: 0,
		},
		{
			name: "unknown message type with text payload",
			message: &api.Message{
				ID:      "msg-8",
				Source:  api.MessageSourceUser,
				Type:    api.MessageTypeUserInputRequest,
				Payload: "Some text content",
			},
			wantErr:   false,
			wantCount: 1,
			check: func(t *testing.T, blocks []types.ContentBlock) {
				if len(blocks) != 1 {
					t.Fatalf("expected 1 content block, got %d", len(blocks))
				}
				// Should fall back to text for unknown types
				if _, ok := blocks[0].(*types.ContentBlockMemberText); !ok {
					t.Errorf("expected ContentBlockMemberText for unknown type, got %T", blocks[0])
				}
			},
		},
	}

	cs := &bedrockChat{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks, err := cs.processAPIMessage(tt.message)
			if (err != nil) != tt.wantErr {
				t.Errorf("processAPIMessage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(blocks) != tt.wantCount {
				t.Errorf("processAPIMessage() returned %d blocks, want %d", len(blocks), tt.wantCount)
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(t, blocks)
			}
		})
	}
}

func TestProcessAPIMessageWithMalformedToolData(t *testing.T) {
	cs := &bedrockChat{}

	tests := []struct {
		name    string
		message *api.Message
		wantErr bool
	}{
		{
			name: "tool call request missing id",
			message: &api.Message{
				Type: api.MessageTypeToolCallRequest,
				Payload: map[string]any{
					"name":      "kubectl_get",
					"arguments": map[string]any{},
				},
			},
			wantErr: true,
		},
		{
			name: "tool call request missing name",
			message: &api.Message{
				Type: api.MessageTypeToolCallRequest,
				Payload: map[string]any{
					"id":        "call-123",
					"arguments": map[string]any{},
				},
			},
			wantErr: true,
		},
		{
			name: "tool call response missing id",
			message: &api.Message{
				Type: api.MessageTypeToolCallResponse,
				Payload: map[string]any{
					"result": map[string]any{"output": "test"},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := cs.processAPIMessage(tt.message)
			if (err != nil) != tt.wantErr {
				t.Errorf("processAPIMessage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInitializeWithToolMessages(t *testing.T) {
	cs := &bedrockChat{}

	history := []*api.Message{
		{
			ID:      "msg-1",
			Source:  api.MessageSourceUser,
			Type:    api.MessageTypeText,
			Payload: "get all pods",
		},
		{
			ID:     "msg-2",
			Source: api.MessageSourceModel,
			Type:   api.MessageTypeToolCallRequest,
			Payload: map[string]any{
				"id":   "call-123",
				"name": "kubectl_get",
				"arguments": map[string]any{
					"resource": "pods",
				},
			},
		},
		{
			ID:     "msg-3",
			Source: api.MessageSourceUser,
			Type:   api.MessageTypeToolCallResponse,
			Payload: map[string]any{
				"id": "call-123",
				"result": map[string]any{
					"output": "pod1\npod2\npod3",
				},
			},
		},
		{
			ID:      "msg-4",
			Source:  api.MessageSourceModel,
			Type:    api.MessageTypeText,
			Payload: "Here are the pods in your cluster",
		},
	}

	err := cs.Initialize(history)
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	// Should have 4 messages in the conversation
	if len(cs.messages) != 4 {
		t.Errorf("expected 4 messages in conversation, got %d", len(cs.messages))
	}

	// Check message roles
	expectedRoles := []types.ConversationRole{
		types.ConversationRoleUser,
		types.ConversationRoleAssistant,
		types.ConversationRoleUser,
		types.ConversationRoleAssistant,
	}

	for i, msg := range cs.messages {
		if msg.Role != expectedRoles[i] {
			t.Errorf("message %d: expected role %s, got %s", i, expectedRoles[i], msg.Role)
		}
	}

	// Check content block types
	if len(cs.messages[0].Content) != 1 {
		t.Errorf("message 0: expected 1 content block, got %d", len(cs.messages[0].Content))
	} else if _, ok := cs.messages[0].Content[0].(*types.ContentBlockMemberText); !ok {
		t.Errorf("message 0: expected text content, got %T", cs.messages[0].Content[0])
	}

	if len(cs.messages[1].Content) != 1 {
		t.Errorf("message 1: expected 1 content block, got %d", len(cs.messages[1].Content))
	} else if _, ok := cs.messages[1].Content[0].(*types.ContentBlockMemberToolUse); !ok {
		t.Errorf("message 1: expected tool use content, got %T", cs.messages[1].Content[0])
	}

	if len(cs.messages[2].Content) != 1 {
		t.Errorf("message 2: expected 1 content block, got %d", len(cs.messages[2].Content))
	} else if _, ok := cs.messages[2].Content[0].(*types.ContentBlockMemberToolResult); !ok {
		t.Errorf("message 2: expected tool result content, got %T", cs.messages[2].Content[0])
	}

	if len(cs.messages[3].Content) != 1 {
		t.Errorf("message 3: expected 1 content block, got %d", len(cs.messages[3].Content))
	} else if _, ok := cs.messages[3].Content[0].(*types.ContentBlockMemberText); !ok {
		t.Errorf("message 3: expected text content, got %T", cs.messages[3].Content[0])
	}
}

func TestInitializeSkipsInvalidMessages(t *testing.T) {
	cs := &bedrockChat{}

	history := []*api.Message{
		{
			ID:      "msg-1",
			Source:  api.MessageSourceUser,
			Type:    api.MessageTypeText,
			Payload: "valid message",
		},
		{
			ID:      "msg-2",
			Source:  "unknown-source",
			Type:    api.MessageTypeText,
			Payload: "message with unknown source",
		},
		{
			ID:      "msg-3",
			Source:  api.MessageSourceUser,
			Type:    api.MessageTypeText,
			Payload: "", // empty text
		},
		{
			ID:     "msg-4",
			Source: api.MessageSourceModel,
			Type:   api.MessageTypeToolCallRequest,
			Payload: map[string]any{
				// missing required fields
				"arguments": map[string]any{},
			},
		},
		{
			ID:      "msg-5",
			Source:  api.MessageSourceUser,
			Type:    api.MessageTypeText,
			Payload: "another valid message",
		},
	}

	err := cs.Initialize(history)
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	// Should only have the 2 valid messages
	if len(cs.messages) != 2 {
		t.Errorf("expected 2 valid messages in conversation, got %d", len(cs.messages))
	}
}

func TestToolUseArgumentsParsing(t *testing.T) {
	cs := &bedrockChat{}

	// Test with valid JSON string arguments
	payload := map[string]any{
		"id":        "call-123",
		"name":      "test_tool",
		"arguments": `{"key1":"value1","key2":42,"key3":true}`,
	}

	result, err := cs.createToolUseFromPayload(payload)
	if err != nil {
		t.Fatalf("createToolUseFromPayload() failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Test with invalid JSON string arguments (should default to empty map)
	payloadInvalid := map[string]any{
		"id":        "call-456",
		"name":      "test_tool",
		"arguments": `{invalid json`,
	}

	result2, err := cs.createToolUseFromPayload(payloadInvalid)
	if err != nil {
		t.Fatalf("createToolUseFromPayload() with invalid JSON failed: %v", err)
	}

	if result2 == nil {
		t.Fatal("expected non-nil result for invalid JSON")
	}
}

func TestToolResultWithComplexStructure(t *testing.T) {
	cs := &bedrockChat{}

	payload := map[string]any{
		"id": "call-789",
		"result": map[string]any{
			"output": "command output here",
			"metadata": map[string]any{
				"duration": 1.5,
				"exitCode": 0,
			},
			"warnings": []string{"warning1", "warning2"},
		},
		"status": "success",
	}

	result, err := cs.createToolResultFromPayload(payload)
	if err != nil {
		t.Fatalf("createToolResultFromPayload() failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if aws.ToString(result.ToolUseId) != "call-789" {
		t.Errorf("expected tool use id 'call-789', got %s", aws.ToString(result.ToolUseId))
	}

	if result.Status != types.ToolResultStatusSuccess {
		t.Errorf("expected status Success, got %s", result.Status)
	}

	if len(result.Content) == 0 {
		t.Error("expected non-empty content")
	}

	// Verify content structure
	if jsonBlock, ok := result.Content[0].(*types.ToolResultContentBlockMemberJson); ok {
		// Try to marshal and unmarshal to verify structure
		data, err := json.Marshal(jsonBlock.Value)
		if err != nil {
			t.Errorf("failed to marshal json block: %v", err)
		}

		var unmarshaled map[string]any
		if err := json.Unmarshal(data, &unmarshaled); err != nil {
			t.Errorf("failed to unmarshal json block: %v", err)
		}
	} else {
		t.Errorf("expected ToolResultContentBlockMemberJson, got %T", result.Content[0])
	}
}
