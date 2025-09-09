package gollm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNirmataStreamingImplementation(t *testing.T) {
	t.Run("chunked streaming with StreamData objects", func(t *testing.T) {
		streamData := []nirmataStreamData{
			{Type: "StreamDataTypeText", Content: "Hello"},
			{Type: "StreamDataTypeText", Content: " world"},
			{Type: "StreamDataTypeText", Content: "!"},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.RawQuery, "chunked=true") {
				t.Errorf("Expected chunked=true in query, got: %s", r.URL.RawQuery)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			encoder := json.NewEncoder(w)
			for _, data := range streamData {
				encoder.Encode(data)
			}
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
		iter, err := chat.SendStreaming(ctx, "test message")
		if err != nil {
			t.Fatalf("SendStreaming failed: %v", err)
		}

		var receivedChunks []string
		var fullContent string

		for response, err := range iter {
			if err != nil {
				t.Fatalf("Streaming error: %v", err)
			}
			if response != nil {
				candidates := response.Candidates()
				if len(candidates) > 0 {
					if text, ok := candidates[0].Parts()[0].AsText(); ok {
						receivedChunks = append(receivedChunks, text)
						fullContent += text
					}
				}
			}
		}

		expectedChunks := []string{"Hello", " world", "!"}
		if len(receivedChunks) != len(expectedChunks) {
			t.Errorf("Expected %d chunks, got %d", len(expectedChunks), len(receivedChunks))
		}

		for i, expected := range expectedChunks {
			if i < len(receivedChunks) && receivedChunks[i] != expected {
				t.Errorf("Chunk %d: expected %q, got %q", i, expected, receivedChunks[i])
			}
		}

		if fullContent != "Hello world!" {
			t.Errorf("Expected full content 'Hello world!', got %q", fullContent)
		}

		if len(chat.history) != 2 {
			t.Errorf("Expected 2 messages in history, got %d", len(chat.history))
		}

		if chat.history[1].Content != "Hello world!" {
			t.Errorf("Expected assistant message 'Hello world!', got %q", chat.history[1].Content)
		}
	})

	t.Run("streaming with error", func(t *testing.T) {
		streamData := []nirmataStreamData{
			{Type: "StreamDataTypeText", Content: "Hello"},
			{Type: "StreamDataTypeError", Error: "Stream processing failed"},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			encoder := json.NewEncoder(w)
			for _, data := range streamData {
				encoder.Encode(data)
			}
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
		iter, err := chat.SendStreaming(ctx, "test message")
		if err != nil {
			t.Fatalf("SendStreaming failed: %v", err)
		}

		var gotError bool
		for response, err := range iter {
			if err != nil {
				if !strings.Contains(err.Error(), "Stream processing failed") {
					t.Errorf("Expected error containing 'Stream processing failed', got: %v", err)
				}
				gotError = true
				break
			}
			if response != nil {
				candidates := response.Candidates()
				if len(candidates) > 0 {
					if text, ok := candidates[0].Parts()[0].AsText(); ok && text == "Hello" {
						continue
					}
				}
			}
		}

		if !gotError {
			t.Error("Expected to receive streaming error but didn't")
		}
	})

	t.Run("query parameter validation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()
			
			if query.Get("chunked") != "true" {
				t.Errorf("Expected chunked=true, got: %s", query.Get("chunked"))
			}
			
			if query.Get("model") != "test-model" {
				t.Errorf("Expected model=test-model, got: %s", query.Get("model"))
			}

			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(nirmataStreamData{
				Type: "StreamDataTypeText", 
				Content: "test",
			})
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
		if err != nil {
			t.Fatalf("SendStreaming failed: %v", err)
		}
	})
}

func TestCompareOldVsNewStreamingBehavior(t *testing.T) {
	t.Run("old behavior would yield raw JSON", func(t *testing.T) {
		rawJSONResponse := `{
  "message": "I'll help you validate the policy in the sample_test directory. Let me first explore the structure to understand what we're working with.\n\n<function_calls>\n<invoke name=\"get_file_info\">\n<parameter name=\"path\">sample_test</parameter>\n</invoke>\n</function_calls>",
  "metadata": {
    "usage": {
      "timestamp": 1757438264414,
      "conversationId": "",
      "inputTokens": 4405,
      "outputTokens": 436,
      "totalTokens": 4841
    }
  }
}`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, rawJSONResponse)
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
		iter, err := chat.SendStreaming(ctx, "test message")
		if err != nil {
			t.Fatalf("SendStreaming failed: %v", err)
		}

		gotAnyResponse := false
		for response, err := range iter {
			if err != nil {
				t.Logf("Got streaming error (expected with malformed stream): %v", err)
				return
			}
			if response != nil {
				candidates := response.Candidates()
				if len(candidates) > 0 {
					if text, ok := candidates[0].Parts()[0].AsText(); ok {
						t.Logf("Received chunk: %q", text)
						
						if strings.Contains(text, `"message":`) || strings.Contains(text, `"metadata":`) {
							t.Error("New implementation should NOT yield raw JSON like old implementation")
						}
						gotAnyResponse = true
					}
				}
			}
		}

		if !gotAnyResponse {
			t.Log("No response received - this is expected when server sends non-streaming JSON to streaming endpoint")
		}
	})
}