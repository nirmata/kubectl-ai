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
	"testing"
)

// TestNirmataDebugToolSending provides detailed debugging of the exact tool sending flow
func TestNirmataDebugToolSending(t *testing.T) {
	var capturedRequest nirmataChatRequest
	var capturedHeaders http.Header
	var capturedURL *url.URL

	// Create a detailed capturing server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		capturedURL = r.URL

		// Capture the complete request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("Failed to read request body: %v", err)
		}

		// Unmarshal and capture the request
		if err := json.Unmarshal(body, &capturedRequest); err != nil {
			t.Errorf("Failed to unmarshal request: %v", err)
		}

		// Debug output
		t.Logf("=== REQUEST DETAILS ===")
		t.Logf("URL: %s", r.URL.String())
		t.Logf("Method: %s", r.Method)
		t.Logf("Headers: %v", capturedHeaders)
		t.Logf("Request Body: %s", string(body))
		t.Logf("=== PARSED REQUEST ===")
		t.Logf("Model: %s", capturedRequest.Model)
		t.Logf("Messages Count: %d", len(capturedRequest.Messages))
		t.Logf("Tools Count: %d", len(capturedRequest.Tools))
		t.Logf("Tool Choice: %v", capturedRequest.ToolChoice)

		for i, tool := range capturedRequest.Tools {
			t.Logf("Tool %d: Type=%s, Name=%s, Description=%s", i, tool.Type, tool.Function.Name, tool.Function.Description)
			if tool.Function.Parameters != nil {
				paramsJSON, _ := json.MarshalIndent(tool.Function.Parameters, "", "  ")
				t.Logf("Tool %d Parameters: %s", i, string(paramsJSON))
			}
		}

		// Return a mock response
		response := nirmataChatResponse{
			Message: "Tools received successfully",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Parse server URL
	serverURL, _ := url.Parse(server.URL)

	// Create client with tools enabled
	client := &NirmataClient{
		baseURL:       serverURL,
		httpClient:    server.Client(),
		apiKey:        "test-debug-key",
		supportsTools: true,
	}

	chat := &nirmataChat{
		client: client,
		model:  "us.anthropic.claude-sonnet-4-20250514-v1:0",
	}

	// Setup realistic kubectl and bash tools
	tools := []*FunctionDefinition{
		{
			Name:        "kubectl",
			Description: "Execute kubectl commands to interact with Kubernetes clusters",
			Parameters: &Schema{
				Type: TypeObject,
				Properties: map[string]*Schema{
					"command": {
						Type:        TypeString,
						Description: "The complete kubectl command to execute",
					},
					"modifies_resource": {
						Type:        TypeString,
						Description: "Whether the command modifies a kubernetes resource (yes/no)",
					},
				},
				Required: []string{"command", "modifies_resource"},
			},
		},
		{
			Name:        "bash",
			Description: "Execute bash commands in the system shell",
			Parameters: &Schema{
				Type: TypeObject,
				Properties: map[string]*Schema{
					"command": {
						Type:        TypeString,
						Description: "The bash command to execute",
					},
					"modifies_resource": {
						Type:        TypeString,
						Description: "Whether the command modifies a kubernetes resource (yes/no)",
					},
				},
				Required: []string{"command", "modifies_resource"},
			},
		},
	}

	// Set function definitions
	err := chat.SetFunctionDefinitions(tools)
	if err != nil {
		t.Fatalf("Failed to set function definitions: %v", err)
	}

	t.Logf("=== CHAT STATE AFTER SETUP ===")
	t.Logf("Client supports tools: %v", client.supportsTools)
	t.Logf("Function definitions count: %d", len(chat.functionDefs))
	t.Logf("Converted tools count: %d", len(chat.tools))

	// Send a message that should trigger tool usage
	response, err := chat.Send(context.Background(), "Please get all pods in the default namespace")
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	// Verify the response
	if response == nil {
		t.Fatal("Response is nil")
	}

	candidates := response.Candidates()
	if len(candidates) == 0 {
		t.Fatal("No candidates in response")
	}

	t.Logf("=== RESPONSE ANALYSIS ===")
	t.Logf("Response text: %s", candidates[0].String())

	// Critical assertions
	if len(capturedRequest.Tools) == 0 {
		t.Error("CRITICAL: No tools found in HTTP request!")
	} else {
		t.Logf("SUCCESS: Found %d tools in HTTP request", len(capturedRequest.Tools))

		// Verify tools are properly formatted
		for i, tool := range capturedRequest.Tools {
			if tool.Type != "function" {
				t.Errorf("Tool %d has wrong type: %s, expected 'function'", i, tool.Type)
			}
			if tool.Function.Name == "" {
				t.Errorf("Tool %d has empty name", i)
			}
			if tool.Function.Description == "" {
				t.Errorf("Tool %d has empty description", i)
			}
			if tool.Function.Parameters == nil {
				t.Errorf("Tool %d has nil parameters", i)
			}
		}
	}

	if capturedRequest.ToolChoice != "auto" {
		t.Errorf("Expected tool_choice 'auto', got %v", capturedRequest.ToolChoice)
	}

	// Check HTTP headers
	authHeader := capturedHeaders.Get("Authorization")
	if authHeader != "NIRMATA-API test-debug-key" {
		t.Errorf("Expected Authorization 'NIRMATA-API test-debug-key', got %s", authHeader)
	}

	contentType := capturedHeaders.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got %s", contentType)
	}

	// Check URL parameters
	if capturedURL.Query().Get("model") != "us.anthropic.claude-sonnet-4-20250514-v1:0" {
		t.Errorf("Model parameter not set correctly: %s", capturedURL.Query().Get("model"))
	}

	if capturedURL.Query().Get("provider") != "bedrock" {
		t.Errorf("Provider parameter not set correctly: %s", capturedURL.Query().Get("provider"))
	}
}

// TestNirmataConditionsPreventingToolSending tests scenarios where tools might not be sent
func TestNirmataConditionsPreventingToolSending(t *testing.T) {
	tests := []struct {
		name             string
		supportsTools    bool
		hasTools         bool
		expectTools      bool
		expectToolChoice bool
	}{
		{
			name:             "tools supported and available",
			supportsTools:    true,
			hasTools:         true,
			expectTools:      true,
			expectToolChoice: true,
		},
		{
			name:             "tools not supported",
			supportsTools:    false, // This is ignored now - tools are always supported
			hasTools:         true,
			expectTools:      true, // Tools are always sent when defined
			expectToolChoice: true, // Tool choice is always sent with tools
		},
		{
			name:             "no tools defined",
			supportsTools:    true,
			hasTools:         false,
			expectTools:      false,
			expectToolChoice: false,
		},
		{
			name:             "tools not supported and no tools",
			supportsTools:    false, // This is ignored now - tools are always supported
			hasTools:         false,
			expectTools:      false, // No tools defined, so none sent
			expectToolChoice: false, // No tool choice when no tools
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest nirmataChatRequest

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				json.Unmarshal(body, &capturedRequest)

				response := nirmataChatResponse{Message: "Test response"}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()

			serverURL, _ := url.Parse(server.URL)

			client := &NirmataClient{
				baseURL:       serverURL,
				httpClient:    server.Client(),
				apiKey:        "test-key",
				supportsTools: true, // Always true now - tools are required
			}

			chat := &nirmataChat{
				client: client,
				model:  "test-model",
			}

			if tt.hasTools {
				tools := []*FunctionDefinition{
					{
						Name:        "test_tool",
						Description: "A test tool",
						Parameters:  &Schema{Type: TypeObject, Properties: make(map[string]*Schema)},
					},
				}
				chat.SetFunctionDefinitions(tools)
			}

			_, err := chat.Send(context.Background(), "Test message")
			if err != nil {
				t.Fatalf("Failed to send message: %v", err)
			}

			if tt.expectTools {
				if len(capturedRequest.Tools) == 0 {
					t.Error("Expected tools in request but found none")
				}
			} else {
				if len(capturedRequest.Tools) > 0 {
					t.Error("Expected no tools in request but found some")
				}
			}

			if tt.expectToolChoice {
				if capturedRequest.ToolChoice != "auto" {
					t.Errorf("Expected tool_choice 'auto', got %v", capturedRequest.ToolChoice)
				}
			} else {
				if capturedRequest.ToolChoice != nil {
					t.Errorf("Expected no tool_choice, got %v", capturedRequest.ToolChoice)
				}
			}
		})
	}
}

// TestNirmataStreamingDebug tests streaming with tools
func TestNirmataStreamingDebug(t *testing.T) {
	var capturedRequest nirmataChatRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedRequest)

		t.Logf("=== STREAMING REQUEST ===")
		t.Logf("Stream: %v", capturedRequest.Stream)
		t.Logf("Tools Count: %d", len(capturedRequest.Tools))
		t.Logf("Tool Choice: %v", capturedRequest.ToolChoice)

		// Return streaming response
		w.Header().Set("Content-Type", "application/json")

		// Send a tool call in streaming format
		streamData := []byte(`{"id":"1","type":"ToolStart","data":"{\"tool_call\":{\"id\":\"call_123\",\"type\":\"function\",\"function\":{\"name\":\"kubectl\",\"arguments\":\"{\\\"command\\\":\\\"kubectl get pods\\\",\\\"modifies_resource\\\":\\\"no\\\"}\"}}}"}` + "\n")
		w.Write(streamData)

		textData := []byte(`{"id":"2","type":"Text","data":"Here are your pods:"}` + "\n")
		w.Write(textData)
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

	// Setup tools
	tools := []*FunctionDefinition{
		{
			Name:        "kubectl",
			Description: "Execute kubectl commands",
			Parameters:  &Schema{Type: TypeObject, Properties: make(map[string]*Schema)},
		},
	}
	chat.SetFunctionDefinitions(tools)

	iterator, err := chat.SendStreaming(context.Background(), "Get pods")
	if err != nil {
		t.Fatalf("Failed to start streaming: %v", err)
	}

	var responses []ChatResponse
	for response, err := range iterator {
		if err != nil {
			t.Fatalf("Streaming error: %v", err)
		}
		if response != nil {
			responses = append(responses, response)
		}
	}

	// Verify streaming request included tools
	if !capturedRequest.Stream {
		t.Error("Expected Stream to be true")
	}

	if len(capturedRequest.Tools) == 0 {
		t.Error("Expected tools in streaming request")
	}

	if capturedRequest.ToolChoice != "auto" {
		t.Errorf("Expected tool_choice 'auto', got %v", capturedRequest.ToolChoice)
	}

	t.Logf("Received %d streaming responses", len(responses))
}
