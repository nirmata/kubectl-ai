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

// TestNirmataToolCallFlow tests the complete tool call flow
func TestNirmataToolCallFlow(t *testing.T) {
	// Create a test server that simulates tool call flow
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		// Capture and validate request
		var req nirmataChatRequest
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)

		w.Header().Set("Content-Type", "application/json")

		if requestCount == 1 {
			// First request: expect tools and return tool call
			if len(req.Tools) == 0 {
				t.Error("Expected tools in first request")
			}
			if req.ToolChoice != "auto" {
				t.Errorf("Expected tool_choice 'auto', got %v", req.ToolChoice)
			}

			// Return response with tool call
			response := nirmataChatResponse{
				Message: "I'll help you check the pods.",
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
			}
			json.NewEncoder(w).Encode(response)
		} else if requestCount == 2 {
			// Second request: expect tool result and tools still available
			if len(req.Tools) == 0 {
				t.Error("Expected tools in second request")
			}

			// Check for tool result message
			foundToolResult := false
			for _, msg := range req.Messages {
				if msg.Role == "tool" && msg.ToolCallID == "call_123" {
					foundToolResult = true
					break
				}
			}
			if !foundToolResult {
				t.Error("Expected tool result message in second request")
			}

			// Return final response
			response := nirmataChatResponse{
				Message: "Based on the kubectl output, your pods are running correctly.",
			}
			json.NewEncoder(w).Encode(response)
		}
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
					"command":           {Type: TypeString, Description: "The kubectl command"},
					"modifies_resource": {Type: TypeString, Description: "Whether the command modifies a resource"},
				},
				Required: []string{"command"},
			},
		},
	}
	err := chat.SetFunctionDefinitions(tools)
	if err != nil {
		t.Fatalf("failed to set function definitions: %v", err)
	}

	// First request - should trigger tool call
	response1, err := chat.Send(context.Background(), "Show me the pods")
	if err != nil {
		t.Fatalf("failed to send first message: %v", err)
	}

	// Verify response has tool calls
	candidates := response1.Candidates()
	if len(candidates) == 0 {
		t.Fatal("expected candidates in response")
	}

	parts := candidates[0].Parts()
	var functionCalls []FunctionCall
	for _, part := range parts {
		if calls, ok := part.AsFunctionCalls(); ok {
			functionCalls = append(functionCalls, calls...)
		}
	}

	if len(functionCalls) == 0 {
		t.Fatal("expected function calls in response")
	}

	// Simulate tool execution and send result
	toolResult := FunctionCallResult{
		ID:   functionCalls[0].ID,
		Name: functionCalls[0].Name,
		Result: map[string]any{
			"output": "NAME    READY   STATUS    RESTARTS   AGE\npod1    1/1     Running   0          5m\npod2    1/1     Running   0          3m",
			"status": "success",
		},
	}

	// Second request - send tool result
	response2, err := chat.Send(context.Background(), toolResult)
	if err != nil {
		t.Fatalf("failed to send tool result: %v", err)
	}

	// Verify final response
	candidates2 := response2.Candidates()
	if len(candidates2) == 0 {
		t.Fatal("expected candidates in final response")
	}

	finalText := candidates2[0].String()
	if finalText == "" {
		t.Error("expected non-empty final response")
	}

	// Verify we made exactly 2 requests
	if requestCount != 2 {
		t.Errorf("expected 2 requests, got %d", requestCount)
	}
}

// TestNirmataErrorHandling tests error conditions
func TestNirmataErrorHandling(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		responseBody string
		expectError  bool
	}{
		{
			name:         "success response",
			statusCode:   200,
			responseBody: `{"message":"Success"}`,
			expectError:  false,
		},
		{
			name:         "400 bad request",
			statusCode:   400,
			responseBody: `{"error":"Bad request"}`,
			expectError:  true,
		},
		{
			name:         "401 unauthorized",
			statusCode:   401,
			responseBody: `{"message":"Unauthorized"}`,
			expectError:  true,
		},
		{
			name:         "500 server error",
			statusCode:   500,
			responseBody: `{"detail":"Internal server error"}`,
			expectError:  true,
		},
		{
			name:         "invalid JSON response",
			statusCode:   500,
			responseBody: `invalid json`,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			serverURL, _ := url.Parse(server.URL)
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

			_, err := chat.Send(context.Background(), "Test message")

			if tt.expectError && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestNirmataClientCreation tests client creation scenarios
func TestNirmataClientCreation(t *testing.T) {
	// Save original env vars
	originalAPIKey := os.Getenv(NIRMATA_APIKEY_ENV)
	originalEndpoint := os.Getenv(NIRMATA_ENDPOINT_ENV)
	originalToolsEnabled := os.Getenv("NIRMATA_TOOLS_ENABLED")

	defer func() {
		// Restore original env vars
		if originalAPIKey != "" {
			os.Setenv(NIRMATA_APIKEY_ENV, originalAPIKey)
		} else {
			os.Unsetenv(NIRMATA_APIKEY_ENV)
		}
		if originalEndpoint != "" {
			os.Setenv(NIRMATA_ENDPOINT_ENV, originalEndpoint)
		} else {
			os.Unsetenv(NIRMATA_ENDPOINT_ENV)
		}
		if originalToolsEnabled != "" {
			os.Setenv("NIRMATA_TOOLS_ENABLED", originalToolsEnabled)
		} else {
			os.Unsetenv("NIRMATA_TOOLS_ENABLED")
		}
	}()

	tests := []struct {
		name                  string
		apiKey                string
		endpoint              string
		toolsEnabled          string
		expectError           bool
		expectedSupportsTools bool
	}{
		{
			name:                  "valid configuration with tools enabled",
			apiKey:                "test-key",
			endpoint:              "https://test.nirmata.io",
			toolsEnabled:          "",
			expectError:           false,
			expectedSupportsTools: true,
		},
		{
			name:                  "valid configuration with tools disabled",
			apiKey:                "test-key",
			endpoint:              "https://test.nirmata.io",
			toolsEnabled:          "false",
			expectError:           false,
			expectedSupportsTools: true, // Tools are always enabled now
		},
		{
			name:        "missing API key",
			apiKey:      "",
			endpoint:    "https://test.nirmata.io",
			expectError: true,
		},
		{
			name:                  "default endpoint",
			apiKey:                "test-key",
			endpoint:              "",
			expectError:           false,
			expectedSupportsTools: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment
			if tt.apiKey != "" {
				os.Setenv(NIRMATA_APIKEY_ENV, tt.apiKey)
			} else {
				os.Unsetenv(NIRMATA_APIKEY_ENV)
			}

			if tt.endpoint != "" {
				os.Setenv(NIRMATA_ENDPOINT_ENV, tt.endpoint)
			} else {
				os.Unsetenv(NIRMATA_ENDPOINT_ENV)
			}

			if tt.toolsEnabled != "" {
				os.Setenv("NIRMATA_TOOLS_ENABLED", tt.toolsEnabled)
			} else {
				os.Unsetenv("NIRMATA_TOOLS_ENABLED")
			}

			// Create client
			client, err := NewNirmataClient(context.Background(), ClientOptions{})

			if tt.expectError && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if !tt.expectError && client != nil {
				if client.supportsTools != tt.expectedSupportsTools {
					t.Errorf("expected supportsTools %v, got %v", tt.expectedSupportsTools, client.supportsTools)
				}
			}
		})
	}
}

// TestNirmataComplexFunctionDefinitions tests complex function definition scenarios
func TestNirmataComplexFunctionDefinitions(t *testing.T) {
	client := &NirmataClient{supportsTools: true}
	chat := &nirmataChat{client: client}

	// Test with complex nested schema
	complexSchema := &Schema{
		Type: TypeObject,
		Properties: map[string]*Schema{
			"metadata": {
				Type: TypeObject,
				Properties: map[string]*Schema{
					"name":      {Type: TypeString, Description: "Resource name"},
					"namespace": {Type: TypeString, Description: "Resource namespace"},
					"labels": {
						Type: TypeObject,
						Properties: map[string]*Schema{
							"app":     {Type: TypeString},
							"version": {Type: TypeString},
						},
					},
				},
				Required: []string{"name"},
			},
			"spec": {
				Type: TypeObject,
				Properties: map[string]*Schema{
					"replicas": {Type: TypeInteger, Description: "Number of replicas"},
					"ports": {
						Type: TypeArray,
						Items: &Schema{
							Type: TypeObject,
							Properties: map[string]*Schema{
								"port":       {Type: TypeInteger, Description: "Port number"},
								"targetPort": {Type: TypeInteger, Description: "Target port"},
								"protocol":   {Type: TypeString, Description: "Protocol"},
							},
						},
					},
				},
			},
		},
		Required: []string{"metadata", "spec"},
	}

	functionDef := &FunctionDefinition{
		Name:        "create_kubernetes_resource",
		Description: "Create a Kubernetes resource with complex configuration",
		Parameters:  complexSchema,
	}

	err := chat.SetFunctionDefinitions([]*FunctionDefinition{functionDef})
	if err != nil {
		t.Fatalf("failed to set complex function definition: %v", err)
	}

	if len(chat.tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(chat.tools))
	}

	tool := chat.tools[0]
	if tool.Type != "function" {
		t.Errorf("expected tool type 'function', got %s", tool.Type)
	}
	if tool.Function.Name != "create_kubernetes_resource" {
		t.Errorf("expected tool name 'create_kubernetes_resource', got %s", tool.Function.Name)
	}

	// Validate complex parameters structure
	if tool.Function.Parameters == nil {
		t.Fatal("expected parameters to be set")
	}

	// Check that the nested structure is preserved
	properties, ok := tool.Function.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties to be a map")
	}

	if _, exists := properties["metadata"]; !exists {
		t.Error("expected 'metadata' property to exist")
	}

	if _, exists := properties["spec"]; !exists {
		t.Error("expected 'spec' property to exist")
	}

	// Check required fields
	requiredRaw, exists := tool.Function.Parameters["required"]
	if !exists {
		t.Fatal("expected required field to exist")
	}

	// Convert interface{} slice to count
	requiredSlice, ok := requiredRaw.([]interface{})
	if !ok {
		t.Fatalf("expected required to be a slice, got %T", requiredRaw)
	}

	if len(requiredSlice) != 2 {
		t.Errorf("expected 2 required fields, got %d", len(requiredSlice))
	}
}

// TestNirmataLargeResponseHandling tests handling of large responses
func TestNirmataLargeResponseHandling(t *testing.T) {
	// Create a large response to test buffer handling
	largeMessage := ""
	for i := 0; i < 1000; i++ {
		largeMessage += "This is a large response message. "
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := nirmataChatResponse{
			Message: largeMessage,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
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

	response, err := chat.Send(context.Background(), "Generate a large response")
	if err != nil {
		t.Fatalf("failed to handle large response: %v", err)
	}

	candidates := response.Candidates()
	if len(candidates) == 0 {
		t.Fatal("expected candidates in response")
	}

	responseText := candidates[0].String()
	if len(responseText) != len(largeMessage) {
		t.Errorf("expected response length %d, got %d", len(largeMessage), len(responseText))
	}
}

// TestNirmataModelSelection tests model selection scenarios
func TestNirmataModelSelection(t *testing.T) {
	// Save original model env var
	originalModel := os.Getenv("NIRMATA_MODEL")
	defer func() {
		if originalModel != "" {
			os.Setenv("NIRMATA_MODEL", originalModel)
		} else {
			os.Unsetenv("NIRMATA_MODEL")
		}
	}()

	tests := []struct {
		name          string
		envModel      string
		requestModel  string
		expectedModel string
	}{
		{
			name:          "use default model",
			envModel:      "",
			requestModel:  "",
			expectedModel: DEFAULT_NIRMATA_MODEL,
		},
		{
			name:          "use environment model",
			envModel:      "us.anthropic.claude-3-7-sonnet-20250219-v1:0",
			requestModel:  "",
			expectedModel: "us.anthropic.claude-3-7-sonnet-20250219-v1:0",
		},
		{
			name:          "use explicit model",
			envModel:      "us.anthropic.claude-3-7-sonnet-20250219-v1:0",
			requestModel:  "us.anthropic.claude-sonnet-4-20250514-v1:0",
			expectedModel: "us.anthropic.claude-sonnet-4-20250514-v1:0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment model
			if tt.envModel != "" {
				os.Setenv("NIRMATA_MODEL", tt.envModel)
			} else {
				os.Unsetenv("NIRMATA_MODEL")
			}

			selectedModel := getNirmataModel(tt.requestModel)
			if selectedModel != tt.expectedModel {
				t.Errorf("expected model %s, got %s", tt.expectedModel, selectedModel)
			}
		})
	}
}
