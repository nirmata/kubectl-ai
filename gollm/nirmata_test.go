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
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
)

// TestNirmataToolSupport tests the tool support detection and configuration
func TestNirmataToolSupport(t *testing.T) {
	tests := []struct {
		name            string
		envVar          string
		expectedSupport bool
	}{
		{
			name:            "tools enabled by default",
			envVar:          "",
			expectedSupport: true,
		},
		{
			name:            "tools explicitly disabled",
			envVar:          "false",
			expectedSupport: false,
		},
		{
			name:            "tools enabled explicitly",
			envVar:          "true",
			expectedSupport: true,
		},
		{
			name:            "tools enabled with other value",
			envVar:          "yes",
			expectedSupport: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment
			if tt.envVar != "" {
				os.Setenv("NIRMATA_TOOLS_ENABLED", tt.envVar)
				defer os.Unsetenv("NIRMATA_TOOLS_ENABLED")
			}

			// Create a mock client for testing
			client := &NirmataClient{}

			// Test the tool support detection
			support := checkToolSupport(context.Background(), client)

			if support != tt.expectedSupport {
				t.Errorf("expected tool support %v, got %v", tt.expectedSupport, support)
			}
		})
	}
}

// TestNirmataSetFunctionDefinitions tests the function definition setting and conversion
func TestNirmataSetFunctionDefinitions(t *testing.T) {
	tests := []struct {
		name             string
		functionDefs     []*FunctionDefinition
		supportsTools    bool
		expectedToolsLen int
		expectError      bool
		validateTools    func(t *testing.T, tools []nirmataToolDef)
	}{
		{
			name:             "empty function definitions",
			functionDefs:     []*FunctionDefinition{},
			supportsTools:    true,
			expectedToolsLen: 0,
			expectError:      false,
		},
		{
			name:             "function definitions with tools disabled",
			functionDefs:     []*FunctionDefinition{{Name: "test", Description: "test"}},
			supportsTools:    false,
			expectedToolsLen: 0,
			expectError:      false,
		},
		{
			name: "single function definition with parameters",
			functionDefs: []*FunctionDefinition{
				{
					Name:        "kubectl",
					Description: "Execute kubectl commands",
					Parameters: &Schema{
						Type: TypeObject,
						Properties: map[string]*Schema{
							"command": {
								Type:        TypeString,
								Description: "The kubectl command to execute",
							},
							"modifies_resource": {
								Type:        TypeString,
								Description: "Whether the command modifies a kubernetes resource",
							},
						},
						Required: []string{"command"},
					},
				},
			},
			supportsTools:    true,
			expectedToolsLen: 1,
			expectError:      false,
			validateTools: func(t *testing.T, tools []nirmataToolDef) {
				if len(tools) != 1 {
					t.Fatalf("expected 1 tool, got %d", len(tools))
				}
				tool := tools[0]
				if tool.Type != "function" {
					t.Errorf("expected type 'function', got %s", tool.Type)
				}
				if tool.Function.Name != "kubectl" {
					t.Errorf("expected name 'kubectl', got %s", tool.Function.Name)
				}
				if tool.Function.Description != "Execute kubectl commands" {
					t.Errorf("expected description 'Execute kubectl commands', got %s", tool.Function.Description)
				}
				if tool.Function.Parameters == nil {
					t.Fatal("expected parameters to be set")
				}

				// Check parameters structure
				if tool.Function.Parameters["type"] != "object" {
					t.Errorf("expected type 'object', got %v", tool.Function.Parameters["type"])
				}

				properties, ok := tool.Function.Parameters["properties"].(map[string]interface{})
				if !ok {
					t.Fatal("expected properties to be a map")
				}

				if len(properties) != 2 {
					t.Errorf("expected 2 properties, got %d", len(properties))
				}

				if _, exists := properties["command"]; !exists {
					t.Error("expected 'command' property to exist")
				}

				if _, exists := properties["modifies_resource"]; !exists {
					t.Error("expected 'modifies_resource' property to exist")
				}
			},
		},
		{
			name: "function definition without parameters",
			functionDefs: []*FunctionDefinition{
				{
					Name:        "simple_tool",
					Description: "A simple tool without parameters",
					Parameters:  nil,
				},
			},
			supportsTools:    true,
			expectedToolsLen: 1,
			expectError:      false,
			validateTools: func(t *testing.T, tools []nirmataToolDef) {
				if len(tools) != 1 {
					t.Fatalf("expected 1 tool, got %d", len(tools))
				}
				tool := tools[0]
				if tool.Function.Name != "simple_tool" {
					t.Errorf("expected name 'simple_tool', got %s", tool.Function.Name)
				}

				// Should have minimal schema
				if tool.Function.Parameters["type"] != "object" {
					t.Errorf("expected type 'object', got %v", tool.Function.Parameters["type"])
				}

				properties, ok := tool.Function.Parameters["properties"].(map[string]interface{})
				if !ok {
					t.Fatal("expected properties to be a map")
				}

				if len(properties) != 0 {
					t.Errorf("expected empty properties for tool without parameters, got %d", len(properties))
				}
			},
		},
		{
			name: "multiple function definitions",
			functionDefs: []*FunctionDefinition{
				{
					Name:        "kubectl",
					Description: "Execute kubectl commands",
					Parameters: &Schema{
						Type: TypeObject,
						Properties: map[string]*Schema{
							"command": {Type: TypeString, Description: "The command"},
						},
					},
				},
				{
					Name:        "bash",
					Description: "Execute bash commands",
					Parameters: &Schema{
						Type: TypeObject,
						Properties: map[string]*Schema{
							"command": {Type: TypeString, Description: "The bash command"},
						},
					},
				},
			},
			supportsTools:    true,
			expectedToolsLen: 2,
			expectError:      false,
			validateTools: func(t *testing.T, tools []nirmataToolDef) {
				if len(tools) != 2 {
					t.Fatalf("expected 2 tools, got %d", len(tools))
				}

				toolNames := make(map[string]bool)
				for _, tool := range tools {
					toolNames[tool.Function.Name] = true
				}

				if !toolNames["kubectl"] {
					t.Error("expected 'kubectl' tool to exist")
				}
				if !toolNames["bash"] {
					t.Error("expected 'bash' tool to exist")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock client with tool support configuration
			client := &NirmataClient{
				supportsTools: tt.supportsTools,
			}

			chat := &nirmataChat{
				client: client,
			}

			err := chat.SetFunctionDefinitions(tt.functionDefs)

			if tt.expectError && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if len(chat.tools) != tt.expectedToolsLen {
				t.Errorf("expected %d tools, got %d", tt.expectedToolsLen, len(chat.tools))
			}

			// Validate stored function definitions
			if len(chat.functionDefs) != len(tt.functionDefs) {
				t.Errorf("expected %d function definitions stored, got %d", len(tt.functionDefs), len(chat.functionDefs))
			}

			if tt.validateTools != nil && len(chat.tools) > 0 {
				tt.validateTools(t, chat.tools)
			}
		})
	}
}

// TestNirmataSendWithTools tests the Send method with tools included in requests
func TestNirmataSendWithTools(t *testing.T) {
	tests := []struct {
		name                 string
		setupTools           bool
		supportsTools        bool
		expectToolsInRequest bool
		validateRequest      func(t *testing.T, req nirmataChatRequest)
	}{
		{
			name:                 "send without tools",
			setupTools:           false,
			supportsTools:        true,
			expectToolsInRequest: false,
		},
		{
			name:                 "send with tools but support disabled",
			setupTools:           true,
			supportsTools:        false,
			expectToolsInRequest: false,
		},
		{
			name:                 "send with tools and support enabled",
			setupTools:           true,
			supportsTools:        true,
			expectToolsInRequest: true,
			validateRequest: func(t *testing.T, req nirmataChatRequest) {
				if len(req.Tools) == 0 {
					t.Error("expected tools to be present in request")
				}
				if req.ToolChoice != "auto" {
					t.Errorf("expected tool_choice to be 'auto', got %v", req.ToolChoice)
				}

				// Validate tool structure
				for _, tool := range req.Tools {
					if tool.Function.Name == "" {
						t.Error("expected tool name to be set")
					}
					if tool.Function.Description == "" {
						t.Error("expected tool description to be set")
					}
					if tool.Function.Parameters == nil {
						t.Error("expected tool parameters to be set")
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test server to capture requests
			var capturedRequest nirmataChatRequest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Capture the request body
				body, _ := io.ReadAll(r.Body)
				json.Unmarshal(body, &capturedRequest)

				// Return a mock response
				response := nirmataChatResponse{
					Message: "Test response",
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()

			// Parse server URL
			serverURL, _ := url.Parse(server.URL)

			// Create client with test server
			client := &NirmataClient{
				baseURL:       serverURL,
				httpClient:    server.Client(),
				apiKey:        "test-key",
				supportsTools: tt.supportsTools,
			}

			chat := &nirmataChat{
				client: client,
				model:  "test-model",
			}

			// Setup tools if required
			if tt.setupTools {
				tools := []*FunctionDefinition{
					{
						Name:        "test_tool",
						Description: "A test tool",
						Parameters: &Schema{
							Type: TypeObject,
							Properties: map[string]*Schema{
								"param1": {Type: TypeString, Description: "Parameter 1"},
							},
						},
					},
				}
				err := chat.SetFunctionDefinitions(tools)
				if err != nil {
					t.Fatalf("failed to set function definitions: %v", err)
				}
			}

			// Send a message
			_, err := chat.Send(context.Background(), "Test message")
			if err != nil {
				t.Fatalf("failed to send message: %v", err)
			}

			// Validate the captured request
			if tt.expectToolsInRequest {
				if len(capturedRequest.Tools) == 0 {
					t.Error("expected tools to be present in request, but none found")
				}
				if capturedRequest.ToolChoice != "auto" {
					t.Errorf("expected tool_choice to be 'auto', got %v", capturedRequest.ToolChoice)
				}
			} else {
				if len(capturedRequest.Tools) > 0 {
					t.Error("expected no tools in request, but found some")
				}
				if capturedRequest.ToolChoice != nil {
					t.Errorf("expected tool_choice to be nil, got %v", capturedRequest.ToolChoice)
				}
			}

			if tt.validateRequest != nil {
				tt.validateRequest(t, capturedRequest)
			}
		})
	}
}

// TestNirmataSendStreamingWithTools tests the SendStreaming method with tools
func TestNirmataSendStreamingWithTools(t *testing.T) {
	// Create a test server that returns streaming response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the request body
		var capturedRequest nirmataChatRequest
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedRequest)

		// Validate that tools are included when expected
		if len(capturedRequest.Tools) == 0 {
			t.Error("expected tools to be present in streaming request")
		}
		if capturedRequest.ToolChoice != "auto" {
			t.Errorf("expected tool_choice to be 'auto', got %v", capturedRequest.ToolChoice)
		}
		if !capturedRequest.Stream {
			t.Error("expected stream to be true")
		}

		// Return a streaming response
		w.Header().Set("Content-Type", "application/json")

		// Send some stream data
		streamData := []nirmataStreamData{
			{ID: "1", Type: "Text", Data: "Hello"},
			{ID: "2", Type: "Text", Data: " world"},
		}

		for _, data := range streamData {
			dataBytes, _ := json.Marshal(data)
			w.Write(dataBytes)
			w.Write([]byte("\n"))
		}
	}))
	defer server.Close()

	// Parse server URL
	serverURL, _ := url.Parse(server.URL)

	// Create client with test server
	client := &NirmataClient{
		baseURL:       serverURL,
		httpClient:    server.Client(),
		apiKey:        "test-key",
		supportsTools: true,
	}

	chat := &nirmataChat{
		client: client,
		model:  "test-model",
	}

	// Setup tools
	tools := []*FunctionDefinition{
		{
			Name:        "test_tool",
			Description: "A test tool",
			Parameters: &Schema{
				Type: TypeObject,
				Properties: map[string]*Schema{
					"param1": {Type: TypeString, Description: "Parameter 1"},
				},
			},
		},
	}
	err := chat.SetFunctionDefinitions(tools)
	if err != nil {
		t.Fatalf("failed to set function definitions: %v", err)
	}

	// Send a streaming message
	iterator, err := chat.SendStreaming(context.Background(), "Test message")
	if err != nil {
		t.Fatalf("failed to send streaming message: %v", err)
	}

	// Consume the iterator to trigger the request
	var responses []string
	for response, err := range iterator {
		if err != nil {
			t.Fatalf("streaming error: %v", err)
		}
		if response != nil {
			candidates := response.Candidates()
			for _, candidate := range candidates {
				responses = append(responses, candidate.String())
			}
		}
	}

	// Verify we got some responses
	if len(responses) == 0 {
		t.Error("expected some responses from streaming")
	}
}

// TestNirmataToolResponseParsing tests parsing of tool calls from responses
func TestNirmataToolResponseParsing(t *testing.T) {
	tests := []struct {
		name              string
		response          nirmataChatResponse
		expectedToolCalls int
		validateToolCalls func(t *testing.T, toolCalls []nirmataToolCall)
	}{
		{
			name: "response with tool calls",
			response: nirmataChatResponse{
				Message: "I'll help you with that.",
				ToolCalls: []nirmataToolCall{
					{
						ID:   "call_123",
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{
							Name:      "kubectl",
							Arguments: `{"command":"kubectl get pods","modifies_resource":"no"}`,
						},
					},
				},
			},
			expectedToolCalls: 1,
			validateToolCalls: func(t *testing.T, toolCalls []nirmataToolCall) {
				if len(toolCalls) != 1 {
					t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
				}

				toolCall := toolCalls[0]
				if toolCall.ID != "call_123" {
					t.Errorf("expected ID 'call_123', got %s", toolCall.ID)
				}
				if toolCall.Function.Name != "kubectl" {
					t.Errorf("expected function name 'kubectl', got %s", toolCall.Function.Name)
				}

				// Parse arguments
				var args map[string]interface{}
				err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args)
				if err != nil {
					t.Fatalf("failed to parse arguments: %v", err)
				}

				if args["command"] != "kubectl get pods" {
					t.Errorf("expected command 'kubectl get pods', got %v", args["command"])
				}
			},
		},
		{
			name: "response without tool calls",
			response: nirmataChatResponse{
				Message:   "Just a regular response",
				ToolCalls: []nirmataToolCall{},
			},
			expectedToolCalls: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.response.ToolCalls) != tt.expectedToolCalls {
				t.Errorf("expected %d tool calls, got %d", tt.expectedToolCalls, len(tt.response.ToolCalls))
			}

			if tt.validateToolCalls != nil && len(tt.response.ToolCalls) > 0 {
				tt.validateToolCalls(t, tt.response.ToolCalls)
			}
		})
	}
}

// TestNirmataToolPartConversion tests the conversion of tool calls to function calls
func TestNirmataToolPartConversion(t *testing.T) {
	tests := []struct {
		name                 string
		toolCall             *nirmataToolCall
		expectedFunctionCall bool
		validateFunctionCall func(t *testing.T, fc FunctionCall)
	}{
		{
			name: "valid tool call conversion",
			toolCall: &nirmataToolCall{
				ID:   "call_456",
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      "bash",
					Arguments: `{"command":"echo hello","modifies_resource":"no"}`,
				},
			},
			expectedFunctionCall: true,
			validateFunctionCall: func(t *testing.T, fc FunctionCall) {
				if fc.ID != "call_456" {
					t.Errorf("expected ID 'call_456', got %s", fc.ID)
				}
				if fc.Name != "bash" {
					t.Errorf("expected name 'bash', got %s", fc.Name)
				}
				if fc.Arguments["command"] != "echo hello" {
					t.Errorf("expected command 'echo hello', got %v", fc.Arguments["command"])
				}
				if fc.Arguments["modifies_resource"] != "no" {
					t.Errorf("expected modifies_resource 'no', got %v", fc.Arguments["modifies_resource"])
				}
			},
		},
		{
			name: "tool call with invalid JSON arguments",
			toolCall: &nirmataToolCall{
				ID:   "call_789",
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      "kubectl",
					Arguments: `{"command":"kubectl get pods", invalid}`,
				},
			},
			expectedFunctionCall: true,
			validateFunctionCall: func(t *testing.T, fc FunctionCall) {
				if fc.ID != "call_789" {
					t.Errorf("expected ID 'call_789', got %s", fc.ID)
				}
				if fc.Name != "kubectl" {
					t.Errorf("expected name 'kubectl', got %s", fc.Name)
				}
				// Arguments should be empty due to parse error
				if len(fc.Arguments) != 0 {
					t.Errorf("expected empty arguments due to parse error, got %v", fc.Arguments)
				}
			},
		},
		{
			name: "tool call with empty arguments",
			toolCall: &nirmataToolCall{
				ID:   "call_empty",
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      "simple_tool",
					Arguments: "",
				},
			},
			expectedFunctionCall: true,
			validateFunctionCall: func(t *testing.T, fc FunctionCall) {
				if fc.ID != "call_empty" {
					t.Errorf("expected ID 'call_empty', got %s", fc.ID)
				}
				if fc.Name != "simple_tool" {
					t.Errorf("expected name 'simple_tool', got %s", fc.Name)
				}
				// Arguments should be non-nil but empty
				if fc.Arguments == nil {
					t.Error("expected non-nil arguments map")
				}
				if len(fc.Arguments) != 0 {
					t.Errorf("expected empty arguments, got %v", fc.Arguments)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolPart := &nirmataToolPart{toolCall: tt.toolCall}

			// Test AsText - should return false
			text, isText := toolPart.AsText()
			if isText {
				t.Error("expected AsText to return false for tool part")
			}
			if text != "" {
				t.Errorf("expected empty text, got %s", text)
			}

			// Test AsFunctionCalls
			functionCalls, isFunctionCall := toolPart.AsFunctionCalls()

			if isFunctionCall != tt.expectedFunctionCall {
				t.Errorf("expected AsFunctionCalls to return %v, got %v", tt.expectedFunctionCall, isFunctionCall)
			}

			if tt.expectedFunctionCall {
				if len(functionCalls) != 1 {
					t.Fatalf("expected 1 function call, got %d", len(functionCalls))
				}

				if tt.validateFunctionCall != nil {
					tt.validateFunctionCall(t, functionCalls[0])
				}
			}
		})
	}
}

// TestNirmataRequestFormatting tests the HTTP request formatting
func TestNirmataRequestFormatting(t *testing.T) {
	// Create a test server to capture and validate requests
	var capturedRequests []nirmataChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture request details
		if r.Header.Get("Authorization") != "NIRMATA-API test-key" {
			t.Errorf("expected Authorization header 'NIRMATA-API test-key', got %s", r.Header.Get("Authorization"))
		}

		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type 'application/json', got %s", r.Header.Get("Content-Type"))
		}

		// Parse query parameters
		query := r.URL.Query()
		if query.Get("model") != "test-model" {
			t.Errorf("expected model 'test-model', got %s", query.Get("model"))
		}
		if query.Get("provider") != "bedrock" {
			t.Errorf("expected provider 'bedrock', got %s", query.Get("provider"))
		}

		// Capture the request body
		var req nirmataChatRequest
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)
		capturedRequests = append(capturedRequests, req)

		// Return mock response
		response := nirmataChatResponse{Message: "Test response"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Parse server URL
	serverURL, _ := url.Parse(server.URL)

	// Create client
	client := &NirmataClient{
		baseURL:       serverURL,
		httpClient:    server.Client(),
		apiKey:        "test-key",
		supportsTools: true,
	}

	chat := &nirmataChat{
		client: client,
		model:  "test-model",
	}

	// Setup tools
	tools := []*FunctionDefinition{
		{
			Name:        "kubectl",
			Description: "Execute kubectl commands",
			Parameters: &Schema{
				Type: TypeObject,
				Properties: map[string]*Schema{
					"command": {Type: TypeString, Description: "The command"},
				},
				Required: []string{"command"},
			},
		},
	}
	err := chat.SetFunctionDefinitions(tools)
	if err != nil {
		t.Fatalf("failed to set function definitions: %v", err)
	}

	// Send message
	_, err = chat.Send(context.Background(), "Test message")
	if err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Validate captured request
	if len(capturedRequests) != 1 {
		t.Fatalf("expected 1 captured request, got %d", len(capturedRequests))
	}

	req := capturedRequests[0]

	// Validate request structure
	if req.Model != "test-model" {
		t.Errorf("expected model 'test-model', got %s", req.Model)
	}

	if len(req.Messages) == 0 {
		t.Error("expected messages to be present")
	}

	if len(req.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(req.Tools))
	}

	if req.ToolChoice != "auto" {
		t.Errorf("expected tool_choice 'auto', got %v", req.ToolChoice)
	}

	// Validate tool structure
	tool := req.Tools[0]
	if tool.Function.Name != "kubectl" {
		t.Errorf("expected tool name 'kubectl', got %s", tool.Function.Name)
	}

	if tool.Function.Description != "Execute kubectl commands" {
		t.Errorf("expected tool description 'Execute kubectl commands', got %s", tool.Function.Description)
	}

	if tool.Function.Parameters == nil {
		t.Fatal("expected tool parameters to be set")
	}
}

// TestNirmataConvertContentsToMessage tests message conversion
func TestNirmataConvertContentsToMessage(t *testing.T) {
	chat := &nirmataChat{}

	tests := []struct {
		name               string
		contents           []any
		expectedRole       string
		expectedContent    string
		expectedToolCallID string
	}{
		{
			name:            "simple string content",
			contents:        []any{"Hello world"},
			expectedRole:    "user",
			expectedContent: "Hello world",
		},
		{
			name:            "multiple string contents",
			contents:        []any{"Hello", "world"},
			expectedRole:    "user",
			expectedContent: "Hello world",
		},
		{
			name: "function call result",
			contents: []any{FunctionCallResult{
				ID:   "call_123",
				Name: "kubectl",
				Result: map[string]any{
					"output": "pod1 Running\npod2 Pending",
					"status": "success",
				},
			}},
			expectedRole:       "tool",
			expectedToolCallID: "call_123",
			expectedContent:    `{"output":"pod1 Running\npod2 Pending","status":"success"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := chat.convertContentsToMessage(tt.contents)

			if msg.Role != tt.expectedRole {
				t.Errorf("expected role %s, got %s", tt.expectedRole, msg.Role)
			}

			if msg.Content != tt.expectedContent {
				t.Errorf("expected content %s, got %s", tt.expectedContent, msg.Content)
			}

			if msg.ToolCallID != tt.expectedToolCallID {
				t.Errorf("expected tool call ID %s, got %s", tt.expectedToolCallID, msg.ToolCallID)
			}
		})
	}
}

// Benchmark tests for performance
func BenchmarkNirmataSetFunctionDefinitions(b *testing.B) {
	client := &NirmataClient{supportsTools: true}
	chat := &nirmataChat{client: client}

	// Create a set of realistic function definitions
	functionDefs := []*FunctionDefinition{
		{
			Name:        "kubectl",
			Description: "Execute kubectl commands",
			Parameters: &Schema{
				Type: TypeObject,
				Properties: map[string]*Schema{
					"command":           {Type: TypeString, Description: "The kubectl command to execute"},
					"modifies_resource": {Type: TypeString, Description: "Whether the command modifies a kubernetes resource"},
				},
				Required: []string{"command"},
			},
		},
		{
			Name:        "bash",
			Description: "Execute bash commands",
			Parameters: &Schema{
				Type: TypeObject,
				Properties: map[string]*Schema{
					"command":           {Type: TypeString, Description: "The bash command to execute"},
					"modifies_resource": {Type: TypeString, Description: "Whether the command modifies a kubernetes resource"},
				},
				Required: []string{"command"},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := chat.SetFunctionDefinitions(functionDefs)
		if err != nil {
			b.Fatalf("SetFunctionDefinitions failed: %v", err)
		}
	}
}

func BenchmarkNirmataToolConversion(b *testing.B) {
	toolCall := &nirmataToolCall{
		ID:   "call_123",
		Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{
			Name:      "kubectl",
			Arguments: `{"command":"kubectl get pods --namespace=app-dev01","modifies_resource":"no"}`,
		},
	}

	toolPart := &nirmataToolPart{toolCall: toolCall}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = toolPart.AsFunctionCalls()
	}
}
