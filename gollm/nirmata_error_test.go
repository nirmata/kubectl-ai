package gollm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestNirmataErrorMessageExtraction(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		responseBody   string
		expectedError  string
	}{
		{
			name:       "JSON with error field",
			statusCode: 400,
			responseBody: `{"error": "Invalid API key provided"}`,
			expectedError: "Invalid API key provided",
		},
		{
			name:       "JSON with message field",
			statusCode: 401,
			responseBody: `{"message": "Authentication failed"}`,
			expectedError: "Authentication failed",
		},
		{
			name:       "JSON with detail field",
			statusCode: 403,
			responseBody: `{"detail": "Access denied to this resource"}`,
			expectedError: "Access denied to this resource",
		},
		{
			name:       "JSON with multiple fields (error takes precedence)",
			statusCode: 400,
			responseBody: `{"error": "Primary error", "message": "Secondary message", "detail": "Tertiary detail"}`,
			expectedError: "Primary error",
		},
		{
			name:       "JSON with message when error is empty",
			statusCode: 400,
			responseBody: `{"error": "", "message": "Fallback message", "detail": "Detail info"}`,
			expectedError: "Fallback message",
		},
		{
			name:       "JSON with no recognized error fields",
			statusCode: 500,
			responseBody: `{"status": "failed", "reason": "unknown"}`,
			expectedError: `{"status": "failed", "reason": "unknown"}`,
		},
		{
			name:       "Non-JSON response",
			statusCode: 502,
			responseBody: `Bad Gateway Error`,
			expectedError: `Bad Gateway Error`,
		},
		{
			name:       "Empty response",
			statusCode: 503,
			responseBody: ``,
			expectedError: ``,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			parsedURL, _ := url.Parse(server.URL)
			client := &NirmataClient{
				baseURL:    parsedURL,
				httpClient: &http.Client{},
				apiKey:     "test-key",
			}

			ctx := context.Background()
			req := nirmataChatRequest{
				Messages: []nirmataMessage{
					{Role: "user", Content: "test"},
				},
			}
			
			var resp nirmataChatResponse
			err := client.doRequestWithModel(ctx, "chat", "test-model", req, &resp)
			
			if err == nil {
				t.Fatalf("Expected error but got none")
			}
			
			apiErr, ok := err.(*APIError)
			if !ok {
				t.Fatalf("Expected APIError but got %T", err)
			}
			
			if apiErr.StatusCode != tt.statusCode {
				t.Errorf("Expected status code %d, got %d", tt.statusCode, apiErr.StatusCode)
			}
			
			actualError := apiErr.Err.Error()
			if actualError != tt.expectedError {
				t.Errorf("Expected error message:\n%q\nGot:\n%q", tt.expectedError, actualError)
			}
		})
	}
}

func TestNirmataStreamingErrorMessageExtraction(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		responseBody   string
		expectedError  string
	}{
		{
			name:       "Streaming with JSON error field",
			statusCode: 429,
			responseBody: `{"error": "Rate limit exceeded"}`,
			expectedError: "Rate limit exceeded",
		},
		{
			name:       "Streaming with message field",
			statusCode: 400,
			responseBody: `{"message": "Invalid request format"}`,
			expectedError: "Invalid request format",
		},
		{
			name:       "Streaming with plain text error",
			statusCode: 500,
			responseBody: `Internal server error occurred`,
			expectedError: `Internal server error occurred`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			parsedURL, _ := url.Parse(server.URL + "/llm-apps")
			client := &NirmataClient{
				baseURL:    parsedURL,
				httpClient: &http.Client{},
				apiKey:     "test-key",
			}

			chat := &nirmataChat{
				client:  client,
				model:   "test-model",
				history: []nirmataMessage{},
			}

			ctx := context.Background()
			_, err := chat.SendStreaming(ctx, "test message")
			
			if err == nil {
				t.Fatalf("Expected error but got none")
			}
			
			apiErr, ok := err.(*APIError)
			if !ok {
				t.Fatalf("Expected APIError but got %T", err)
			}
			
			if apiErr.StatusCode != tt.statusCode {
				t.Errorf("Expected status code %d, got %d", tt.statusCode, apiErr.StatusCode)
			}
			
			actualError := apiErr.Err.Error()
			if actualError != tt.expectedError {
				t.Errorf("Expected error message:\n%q\nGot:\n%q", tt.expectedError, actualError)
			}
		})
	}
}

func TestNirmataErrorParsing(t *testing.T) {
	testCases := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "Complex nested JSON",
			input:    []byte(`{"error": {"code": "AUTH001", "description": "Authentication failed"}, "timestamp": "2024-01-01T00:00:00Z"}`),
			expected: `{"error": {"code": "AUTH001", "description": "Authentication failed"}, "timestamp": "2024-01-01T00:00:00Z"}`,
		},
		{
			name:     "HTML error page",
			input:    []byte(`<!DOCTYPE html><html><body><h1>404 Not Found</h1></body></html>`),
			expected: `<!DOCTYPE html><html><body><h1>404 Not Found</h1></body></html>`,
		},
		{
			name:     "JSON array response",
			input:    []byte(`[{"error": "Error 1"}, {"error": "Error 2"}]`),
			expected: `[{"error": "Error 1"}, {"error": "Error 2"}]`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var errorMsg string
			var jsonErr struct {
				Error   string `json:"error"`
				Message string `json:"message"`
				Detail  string `json:"detail"`
			}
			
			if err := json.Unmarshal(tc.input, &jsonErr); err == nil {
				if jsonErr.Error != "" {
					errorMsg = jsonErr.Error
				} else if jsonErr.Message != "" {
					errorMsg = jsonErr.Message
				} else if jsonErr.Detail != "" {
					errorMsg = jsonErr.Detail
				} else {
					errorMsg = string(tc.input)
				}
			} else {
				errorMsg = string(tc.input)
			}
			
			if errorMsg != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, errorMsg)
			}
		})
	}
}