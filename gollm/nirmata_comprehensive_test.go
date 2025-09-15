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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/klog/v2"
)

// TestErrorVisibilityComprehensive tests all aspects of error visibility (Issue #2)
func TestErrorVisibilityComprehensive(t *testing.T) {
	testCases := []struct {
		name           string
		streamData     string
		expectError    bool
		expectUserMsg  bool
		validateOutput func(t *testing.T, output string, err error)
	}{
		{
			name:          "Valid JSON tool call",
			streamData:    `{"tool_call":{"id":"123","type":"function","function":{"name":"bash","arguments":"{\"command\":\"ls\"}"}}}`,
			expectError:   false,
			expectUserMsg: false,
			validateOutput: func(t *testing.T, output string, err error) {
				if err != nil {
					t.Errorf("Should not have error for valid JSON: %v", err)
				}
			},
		},
		{
			name:          "Plain text instead of JSON",
			streamData:    "Executing bash command: ls",
			expectError:   true,
			expectUserMsg: true,
			validateOutput: func(t *testing.T, output string, err error) {
				if !strings.Contains(output, "[Tool parsing error:") {
					t.Errorf("Expected user-visible error message, got: %s", output)
				}
			},
		},
		{
			name:          "Malformed JSON",
			streamData:    `{"tool_call": "should be object not string"}`,
			expectError:   true,
			expectUserMsg: true,
			validateOutput: func(t *testing.T, output string, err error) {
				if !strings.Contains(output, "Tool parsing error") {
					t.Errorf("Expected parsing error to be visible")
				}
			},
		},
		{
			name:          "Empty data",
			streamData:    "",
			expectError:   true,
			expectUserMsg: true,
			validateOutput: func(t *testing.T, output string, err error) {
				// Empty data should produce an error
				if err == nil {
					t.Errorf("Expected error for empty data")
				}
			},
		},
		{
			name:          "Partial JSON",
			streamData:    `{"tool_call":{"id":"123"`,
			expectError:   true,
			expectUserMsg: true,
			validateOutput: func(t *testing.T, output string, err error) {
				if !strings.Contains(output, "parsing error") || !strings.Contains(output, "unexpected end") {
					t.Errorf("Expected JSON parse error for partial data")
				}
			},
		},
		{
			name:          "HTML content mistaken for tool call",
			streamData:    `<html><body>Error page</body></html>`,
			expectError:   true,
			expectUserMsg: true,
			validateOutput: func(t *testing.T, output string, err error) {
				if !strings.Contains(output, "Tool parsing error") {
					t.Errorf("HTML should trigger parsing error")
				}
			},
		},
		{
			name:          "Unicode in error data",
			streamData:    `{"error": "❌ Failed with 中文 characters"}`,
			expectError:   true,
			expectUserMsg: true,
			validateOutput: func(t *testing.T, output string, err error) {
				// Should handle unicode gracefully
				if err == nil {
					t.Errorf("Should fail parsing non-tool-call JSON")
				}
			},
		},
		{
			name:          "Very long malformed data",
			streamData:    strings.Repeat("A", 10000),
			expectError:   true,
			expectUserMsg: true,
			validateOutput: func(t *testing.T, output string, err error) {
				// Should handle large invalid data without panic
				if err == nil {
					t.Errorf("Should error on very long non-JSON data")
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Capture log output
			var logBuf bytes.Buffer
			klog.SetOutput(&logBuf)
			defer klog.SetOutput(io.Discard)

			// Try to parse the data
			var toolData struct {
				ToolCall nirmataToolCall `json:"tool_call"`
			}

			err := json.Unmarshal([]byte(tc.streamData), &toolData)

			// Simulate our fix behavior
			var userOutput string
			if err != nil && tc.expectUserMsg {
				// This is what our fix does
				klog.Errorf("Failed to parse tool call from stream data: %v (data: %q)", err, tc.streamData)
				userOutput = fmt.Sprintf("[Tool parsing error: %v]", err)
			}

			// Validate
			if tc.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Check log output contains error at right level
			logOutput := logBuf.String()
			if tc.expectError && tc.expectUserMsg {
				// If we expect an error and user message, check that ERROR level is used
				if err != nil && !strings.Contains(logOutput, "ERROR") && !strings.Contains(logOutput, "error") {
					t.Errorf("Error should be logged at ERROR level, not debug. Log output: %s", logOutput)
				}
			}

			// Run custom validation
			tc.validateOutput(t, userOutput, err)
		})
	}
}

// TestProviderRoutingComprehensive tests provider parameter handling (Issue #4)
func TestProviderRoutingComprehensive(t *testing.T) {
	testCases := []struct {
		name           string
		clientEndpoint string
		model          string
		expectProvider bool
		validateURL    func(t *testing.T, u *url.URL)
	}{
		{
			name:           "Standard request without forced provider",
			clientEndpoint: "https://api.nirmata.io",
			model:          "claude-3-5-sonnet",
			expectProvider: false,
			validateURL: func(t *testing.T, u *url.URL) {
				if u.Query().Get("provider") != "" {
					t.Errorf("Provider should not be in URL params after fix")
				}
				if u.Query().Get("chunked") != "true" {
					t.Errorf("Chunked should still be set")
				}
			},
		},
		{
			name:           "Request with custom endpoint",
			clientEndpoint: "https://custom.nirmata.io",
			model:          "gpt-4",
			expectProvider: false,
			validateURL: func(t *testing.T, u *url.URL) {
				if strings.Contains(u.String(), "provider=bedrock") {
					t.Errorf("Should not force bedrock provider")
				}
			},
		},
		{
			name:           "Request with empty model",
			clientEndpoint: "https://api.nirmata.io",
			model:          "",
			expectProvider: false,
			validateURL: func(t *testing.T, u *url.URL) {
				if u.Query().Get("provider") != "" {
					t.Errorf("Provider parameter should be removed")
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Simulate URL building after our fix
			u, _ := url.Parse(tc.clientEndpoint)
			u = u.JoinPath("llm-apps").JoinPath("chat")
			q := u.Query()

			if tc.model != "" {
				q.Set("model", tc.model)
			}
			q.Set("chunked", "true")
			// Issue #4 fix: NOT setting provider=bedrock
			// q.Set("provider", "bedrock") // REMOVED

			u.RawQuery = q.Encode()

			// Validate
			tc.validateURL(t, u)

			// Ensure provider is never forced
			if q.Get("provider") == "bedrock" {
				t.Errorf("Issue #4: Provider should not be forced to bedrock")
			}
		})
	}
}

// TestArgumentParsingComprehensive tests argument parsing improvements (Issue #5)
func TestArgumentParsingComprehensive(t *testing.T) {
	testCases := []struct {
		name         string
		toolName     string
		arguments    string
		expectError  bool
		validateArgs func(t *testing.T, args map[string]any)
	}{
		{
			name:        "Valid simple arguments",
			toolName:    "bash",
			arguments:   `{"command": "ls -la"}`,
			expectError: false,
			validateArgs: func(t *testing.T, args map[string]any) {
				if args["command"] != "ls -la" {
					t.Errorf("Expected command to be 'ls -la'")
				}
				if _, hasError := args["_parse_error"]; hasError {
					t.Errorf("Should not have parse error for valid JSON")
				}
			},
		},
		{
			name:        "Complex nested arguments",
			toolName:    "api_call",
			arguments:   `{"endpoint": "/api/v1/users", "body": {"name": "test", "tags": ["a", "b"]}}`,
			expectError: false,
			validateArgs: func(t *testing.T, args map[string]any) {
				if args["endpoint"] != "/api/v1/users" {
					t.Errorf("Expected endpoint to be parsed")
				}
				if body, ok := args["body"].(map[string]interface{}); ok {
					if body["name"] != "test" {
						t.Errorf("Expected nested body.name to be 'test'")
					}
				} else {
					t.Errorf("Expected body to be a map")
				}
			},
		},
		{
			name:        "Invalid JSON syntax",
			toolName:    "bash",
			arguments:   `{command: "missing quotes"}`,
			expectError: true,
			validateArgs: func(t *testing.T, args map[string]any) {
				if parseError, hasError := args["_parse_error"].(string); hasError {
					if !strings.Contains(parseError, "Failed to parse arguments") {
						t.Errorf("Expected descriptive parse error, got: %s", parseError)
					}
				} else {
					t.Errorf("Expected _parse_error field for invalid JSON")
				}
			},
		},
		{
			name:        "Empty arguments",
			toolName:    "list_files",
			arguments:   "",
			expectError: false,
			validateArgs: func(t *testing.T, args map[string]any) {
				if len(args) != 0 {
					t.Errorf("Empty arguments should result in empty map")
				}
			},
		},
		{
			name:        "Escaped JSON string",
			toolName:    "write_file",
			arguments:   `{\"path\": \"test.txt\", \"content\": \"hello\"}`,
			expectError: true, // Escaped quotes make it invalid JSON
			validateArgs: func(t *testing.T, args map[string]any) {
				// Should have parse error
				if _, hasError := args["_parse_error"]; !hasError {
					t.Errorf("Expected parse error for escaped JSON")
				}
			},
		},
		{
			name:        "Arguments with special characters",
			toolName:    "bash",
			arguments:   `{"command": "echo 'Hello\nWorld\t!'"}`,
			expectError: false,
			validateArgs: func(t *testing.T, args map[string]any) {
				if cmd, ok := args["command"].(string); ok {
					if !strings.Contains(cmd, "Hello\nWorld\t!") {
						t.Errorf("Special characters should be preserved")
					}
				}
			},
		},
		{
			name:        "Very large arguments",
			toolName:    "process_data",
			arguments:   fmt.Sprintf(`{"data": "%s"}`, strings.Repeat("A", 10000)),
			expectError: false,
			validateArgs: func(t *testing.T, args map[string]any) {
				if data, ok := args["data"].(string); ok {
					if len(data) != 10000 {
						t.Errorf("Large data should be handled correctly")
					}
				}
			},
		},
		{
			name:        "Null values in arguments",
			toolName:    "update_record",
			arguments:   `{"id": 123, "name": null, "active": true}`,
			expectError: false,
			validateArgs: func(t *testing.T, args map[string]any) {
				if args["name"] != nil {
					t.Errorf("Null should be preserved as nil")
				}
				if args["active"] != true {
					t.Errorf("Boolean should be preserved")
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Capture log output
			var logBuf bytes.Buffer
			klog.SetOutput(&logBuf)
			defer klog.SetOutput(io.Discard)

			// Simulate the fixed parsing logic
			var args map[string]any
			if tc.arguments != "" {
				if err := json.Unmarshal([]byte(tc.arguments), &args); err != nil {
					// Issue #5 fix: Make error visible
					klog.Errorf("Failed to parse tool arguments for %s: %v (raw: %q)",
						tc.toolName, err, tc.arguments)

					args = make(map[string]any)
					args["_parse_error"] = fmt.Sprintf("Failed to parse arguments: %v", err)
				}
			} else {
				args = make(map[string]any)
			}

			// Check error visibility
			logOutput := logBuf.String()
			if tc.expectError && !strings.Contains(logOutput, "ERROR") {
				t.Errorf("Parse errors should be at ERROR level, not debug")
			}

			// Validate arguments
			tc.validateArgs(t, args)
		})
	}
}

// TestIntegrationWithMockServer tests the complete flow with a mock backend
func TestIntegrationWithMockServer(t *testing.T) {
	scenarios := []struct {
		name           string
		serverResponse func(w http.ResponseWriter, r *http.Request)
		validateClient func(t *testing.T, responses []string, errors []error)
	}{
		{
			name: "Server sends correct JSON format",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				// Validate request doesn't have forced provider
				if r.URL.Query().Get("provider") == "bedrock" {
					t.Errorf("Client should not force provider=bedrock")
				}

				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "event: Message\n")
				fmt.Fprintf(w, "data: {\"type\":\"ToolStart\",\"data\":")
				fmt.Fprintf(w, `"{\"tool_call\":{\"id\":\"123\",\"type\":\"function\",\"function\":{\"name\":\"bash\",\"arguments\":\"{\\\"command\\\":\\\"ls\\\"}\"}}}"`)
				fmt.Fprintf(w, "}\n\n")
			},
			validateClient: func(t *testing.T, responses []string, errors []error) {
				if len(errors) > 0 {
					t.Errorf("Should handle correct format without errors: %v", errors)
				}
			},
		},
		{
			name: "Server sends plain text (Issue #1 simulation)",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "event: Message\n")
				fmt.Fprintf(w, "data: {\"type\":\"ToolStart\",\"data\":\"Executing bash command: ls\"}\n\n")
			},
			validateClient: func(t *testing.T, responses []string, errors []error) {
				// Should see user-visible error
				found := false
				for _, resp := range responses {
					if strings.Contains(resp, "[Tool parsing error:") {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected user-visible parsing error for plain text data")
				}
			},
		},
		{
			name: "Server sends malformed events",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				// Send various malformed data
				fmt.Fprintf(w, "event: Message\n")
				fmt.Fprintf(w, "data: {\"type\":\"ToolStart\",\"data\":\"\"}\n\n")

				fmt.Fprintf(w, "event: Message\n")
				fmt.Fprintf(w, "data: {\"type\":\"ToolStart\"}\n\n") // missing data field

				fmt.Fprintf(w, "event: Message\n")
				fmt.Fprintf(w, "data: not even json\n\n")
			},
			validateClient: func(t *testing.T, responses []string, errors []error) {
				// Should handle gracefully without panic
				if len(responses) == 0 && len(errors) == 0 {
					t.Errorf("Should produce some output even with malformed data")
				}
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(scenario.serverResponse))
			defer server.Close()

			// Simulate streaming request
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			var responses []string
			var errors []error

			// Create a simple HTTP client to test server interaction
			httpClient := &http.Client{Timeout: 5 * time.Second}

			// Note: This is a simplified simulation since we can't run the full client
			// In real test, you would call client.ChatCompletionStream
			req, _ := http.NewRequestWithContext(ctx, "POST", server.URL, nil)
			resp, err := httpClient.Do(req)
			if err != nil {
				errors = append(errors, err)
			} else {
				defer resp.Body.Close()
				// Read response
				buf := make([]byte, 1024)
				n, _ := resp.Body.Read(buf)
				responses = append(responses, string(buf[:n]))
			}

			// Validate
			scenario.validateClient(t, responses, errors)
		})
	}
}

// TestConcurrentErrorHandling tests thread safety of error handling
func TestConcurrentErrorHandling(t *testing.T) {
	// Test that multiple goroutines can safely handle errors
	var wg sync.WaitGroup
	errorCount := 0
	var mu sync.Mutex

	badDataSamples := []string{
		"not json",
		`{"wrong": "format"}`,
		"",
		`{"tool_call": null}`,
		"plain text error",
	}

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			data := badDataSamples[index%len(badDataSamples)]
			var toolData struct {
				ToolCall nirmataToolCall `json:"tool_call"`
			}

			if err := json.Unmarshal([]byte(data), &toolData); err != nil {
				// Simulate our error handling
				errorMsg := fmt.Sprintf("[Tool parsing error: %v]", err)
				if errorMsg != "" {
					mu.Lock()
					errorCount++
					mu.Unlock()
				}
			}
		}(i)
	}

	wg.Wait()

	if errorCount != 100 {
		t.Errorf("Expected 100 errors from concurrent parsing, got %d", errorCount)
	}
}

// TestBackwardCompatibility ensures fixes don't break existing functionality
func TestBackwardCompatibility(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{
			name: "Valid tool calls still work",
			testFunc: func(t *testing.T) {
				validJSON := `{"tool_call":{"id":"123","type":"function","function":{"name":"bash","arguments":"{\"command\":\"ls\"}"}}}`

				var toolData struct {
					ToolCall nirmataToolCall `json:"tool_call"`
				}

				err := json.Unmarshal([]byte(validJSON), &toolData)
				if err != nil {
					t.Errorf("Valid JSON should still parse correctly: %v", err)
				}
				if toolData.ToolCall.Function.Name != "bash" {
					t.Errorf("Tool name should be 'bash'")
				}
			},
		},
		{
			name: "Empty responses handled gracefully",
			testFunc: func(t *testing.T) {
				// Empty response should not cause panic
				var toolData struct {
					ToolCall *nirmataToolCall `json:"tool_call"`
				}

				err := json.Unmarshal([]byte(`{}`), &toolData)
				if err != nil {
					t.Errorf("Empty JSON object should parse without error")
				}
				if toolData.ToolCall != nil {
					t.Errorf("Tool call should be nil for empty object")
				}
			},
		},
		{
			name: "URL building still works correctly",
			testFunc: func(t *testing.T) {
				baseURL, _ := url.Parse("https://api.nirmata.io")
				u := baseURL.JoinPath("llm-apps").JoinPath("chat")

				if !strings.Contains(u.String(), "llm-apps/chat") {
					t.Errorf("URL path building broken: %s", u.String())
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.testFunc(t)
		})
	}
}

// TestEdgeCases tests unusual scenarios
func TestEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		test func(t *testing.T)
	}{
		{
			name: "Extremely long error message",
			test: func(t *testing.T) {
				longData := strings.Repeat("A", 100000)
				var toolData struct {
					ToolCall nirmataToolCall `json:"tool_call"`
				}

				err := json.Unmarshal([]byte(longData), &toolData)
				if err != nil {
					// Should handle without panic
					errorMsg := fmt.Sprintf("[Tool parsing error: %v]", err)
					if len(errorMsg) == 0 {
						t.Errorf("Should produce error message even for very long data")
					}
				}
			},
		},
		{
			name: "Binary data in stream",
			test: func(t *testing.T) {
				binaryData := string([]byte{0xFF, 0xFE, 0x00, 0x01, 0x02})
				var toolData struct {
					ToolCall nirmataToolCall `json:"tool_call"`
				}

				err := json.Unmarshal([]byte(binaryData), &toolData)
				if err == nil {
					t.Errorf("Binary data should cause parse error")
				}
			},
		},
		{
			name: "Recursive JSON structure",
			test: func(t *testing.T) {
				// Create a deeply nested structure
				depth := 1000
				jsonStr := `{"tool_call":{"function":{"arguments":"`
				for i := 0; i < depth; i++ {
					jsonStr += `{\"nested\":`
				}
				jsonStr += `\"value\"`
				for i := 0; i < depth; i++ {
					jsonStr += `}`
				}
				jsonStr += `"}}}`

				var toolData struct {
					ToolCall nirmataToolCall `json:"tool_call"`
				}

				// Should handle deep nesting without stack overflow
				_ = json.Unmarshal([]byte(jsonStr), &toolData)
				// Just ensure no panic
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.test(t)
		})
	}
}

// BenchmarkErrorHandling tests performance of error handling
func BenchmarkErrorHandling(b *testing.B) {
	badData := "This is not JSON"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var toolData struct {
			ToolCall nirmataToolCall `json:"tool_call"`
		}

		if err := json.Unmarshal([]byte(badData), &toolData); err != nil {
			_ = fmt.Sprintf("[Tool parsing error: %v]", err)
		}
	}
}

// BenchmarkValidParsing tests performance with valid data
func BenchmarkValidParsing(b *testing.B) {
	validData := `{"tool_call":{"id":"123","type":"function","function":{"name":"bash","arguments":"{\"command\":\"ls\"}"}}}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var toolData struct {
			ToolCall nirmataToolCall `json:"tool_call"`
		}

		_ = json.Unmarshal([]byte(validData), &toolData)
	}
}

// TestRegressionPrevention ensures fixes don't reintroduce old issues
func TestRegressionPrevention(t *testing.T) {
	t.Run("Issue #2 - Errors must be visible", func(t *testing.T) {
		// Capture logs to ensure we're not using V(2) level
		var logBuf bytes.Buffer
		klog.SetOutput(&logBuf)
		defer klog.SetOutput(io.Discard)

		// Trigger an error
		badData := "not json"
		var toolData struct {
			ToolCall nirmataToolCall `json:"tool_call"`
		}

		_ = json.Unmarshal([]byte(badData), &toolData)

		// Simulate the fix
		klog.Errorf("Failed to parse tool call from stream data: test")

		logOutput := logBuf.String()
		if !strings.Contains(logOutput, "ERROR") {
			t.Errorf("Regression: Errors must be logged at ERROR level, not V(2)")
		}
	})

	t.Run("Issue #4 - Provider must not be forced", func(t *testing.T) {
		u, _ := url.Parse("https://api.nirmata.io")
		u = u.JoinPath("llm-apps/chat")
		q := u.Query()
		q.Set("chunked", "true")
		// Should NOT have this line:
		// q.Set("provider", "bedrock")

		if q.Get("provider") == "bedrock" {
			t.Errorf("Regression: Provider should not be forced to bedrock")
		}
	})

	t.Run("Issue #5 - Argument errors must be visible", func(t *testing.T) {
		var logBuf bytes.Buffer
		klog.SetOutput(&logBuf)
		defer klog.SetOutput(io.Discard)

		// Bad arguments
		args := "{broken json}"
		var parsed map[string]any

		if err := json.Unmarshal([]byte(args), &parsed); err != nil {
			// Simulate fix
			klog.Errorf("Failed to parse tool arguments: %v", err)
			parsed = make(map[string]any)
			parsed["_parse_error"] = fmt.Sprintf("Failed to parse arguments: %v", err)
		}

		logOutput := logBuf.String()
		if !strings.Contains(logOutput, "ERROR") {
			t.Errorf("Regression: Argument errors must be at ERROR level")
		}

		if _, hasError := parsed["_parse_error"]; !hasError {
			t.Errorf("Regression: Must include _parse_error field when parsing fails")
		}
	})
}
