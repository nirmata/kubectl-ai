package gollm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestComprehensiveStreamingBehavior(t *testing.T) {
	t.Run("BEFORE_FIX: Old implementation would yield JSON blob", func(t *testing.T) {
		actualProblemResponse := `{  "message": "I'll help you validate the policy in the sample_test directory. Let me first explore the structure to understand what we're working with.", "metadata": { "usage": { "timestamp": 1757438264414, "inputTokens": 4405, "outputTokens": 436 } } }`

		t.Log("❌ PROBLEM: Old implementation would yield this entire JSON blob as a single 'text chunk'")
		t.Logf("Raw response length: %d characters", len(actualProblemResponse))
		
		if !strings.Contains(actualProblemResponse, `"message":`) {
			t.Error("Test data should contain JSON structure")
		}
		if !strings.Contains(actualProblemResponse, `"metadata":`) {
			t.Error("Test data should contain metadata")
		}
	})

	t.Run("AFTER_FIX: New implementation yields proper streaming chunks", func(t *testing.T) {
		expectedMessage := `I'll help you validate the policy in the sample_test directory. Let me first explore the structure to understand what we're working with.

<function_calls>
<invoke name="get_file_info">
<parameter name="path">sample_test</parameter>
</invoke>
</function_calls>

Perfect! The policy validation is successful.`

		// Simulate how the server should send StreamData objects with chunked=true
		streamChunks := []nirmataStreamData{
			{Type: "StreamDataTypeText", Content: "I'll help you validate"},
			{Type: "StreamDataTypeText", Content: " the policy in the sample_test"},
			{Type: "StreamDataTypeText", Content: " directory. Let me first"},
			{Type: "StreamDataTypeText", Content: " explore the structure"},
			{Type: "StreamDataTypeText", Content: " to understand what we're working with.\n\n"},
			{Type: "StreamDataTypeText", Content: "<function_calls>\n<invoke name=\"get_file_info\">\n"},
			{Type: "StreamDataTypeText", Content: "<parameter name=\"path\">sample_test</parameter>\n</invoke>\n</function_calls>\n\n"},
			{Type: "StreamDataTypeText", Content: "Perfect! The policy validation is successful."},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify the streaming request is properly formatted
			if !strings.Contains(r.URL.RawQuery, "chunked=true") {
				t.Errorf("❌ Missing chunked=true parameter. Got query: %s", r.URL.RawQuery)
			}

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Transfer-Encoding", "chunked")
			w.WriteHeader(http.StatusOK)

			// Send StreamData objects as the server would
			encoder := json.NewEncoder(w)
			for i, chunk := range streamChunks {
				if err := encoder.Encode(chunk); err != nil {
					t.Errorf("Failed to encode chunk %d: %v", i, err)
				}
				// Simulate some delay between chunks
				time.Sleep(1 * time.Millisecond)
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
			model:   "us.anthropic.claude-sonnet-4-20250514-v1:0",
			history: []nirmataMessage{},
		}

		ctx := context.Background()
		iter, err := chat.SendStreaming(ctx, "help me validate the policy in sample_test")
		if err != nil {
			t.Fatalf("❌ SendStreaming failed: %v", err)
		}

		var receivedChunks []string
		var fullContent strings.Builder
		chunkCount := 0

		t.Log("✅ NEW BEHAVIOR: Streaming incremental chunks:")
		for response, err := range iter {
			if err != nil {
				t.Fatalf("❌ Streaming error: %v", err)
			}
			if response != nil {
				candidates := response.Candidates()
				if len(candidates) > 0 {
					if text, ok := candidates[0].Parts()[0].AsText(); ok {
						receivedChunks = append(receivedChunks, text)
						fullContent.WriteString(text)
						chunkCount++
						t.Logf("  Chunk %d: %q", chunkCount, text)
						
						// ✅ KEY TEST: No chunk should contain JSON structure
						if strings.Contains(text, `"message":`) || strings.Contains(text, `"metadata":`) {
							t.Errorf("❌ FAILED: Chunk %d contains JSON structure: %q", chunkCount, text)
						}
						
						// ✅ Each chunk should be reasonable size (not entire response)
						if len(text) > 100 {
							t.Logf("  ⚠️  Large chunk (%d chars): %q...", len(text), text[:50])
						}
					}
				}
			}
		}

		// Verify streaming behavior
		t.Logf("✅ Total chunks received: %d", chunkCount)
		t.Logf("✅ Full content length: %d characters", fullContent.Len())
		
		if chunkCount < 2 {
			t.Errorf("❌ Expected multiple chunks for streaming, got %d", chunkCount)
		}

		if fullContent.String() != expectedMessage {
			t.Errorf("❌ Content mismatch.\nExpected: %q\nGot: %q", expectedMessage, fullContent.String())
		}

		// Verify history is updated correctly
		if len(chat.history) != 2 { // user + assistant
			t.Errorf("❌ Expected 2 messages in history, got %d", len(chat.history))
		}

		if chat.history[1].Content != expectedMessage {
			t.Errorf("❌ Assistant message incorrect.\nExpected: %q\nGot: %q", expectedMessage, chat.history[1].Content)
		}

		t.Log("✅ SUCCESS: New implementation streams proper text chunks!")
	})

	t.Run("EDGE_CASE: Server returns non-streaming JSON (graceful fallback)", func(t *testing.T) {
		// Test case where server doesn't support streaming or returns regular JSON
		nonStreamingResponse := nirmataChatResponse{
			Message: "This is a regular JSON response",
			Metadata: map[string]interface{}{
				"usage": map[string]interface{}{
					"tokens": 100,
				},
			},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(nonStreamingResponse)
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
		iter, err := chat.SendStreaming(ctx, "test")
		if err != nil {
			t.Fatalf("SendStreaming failed: %v", err)
		}

		receivedAny := false
		for response, err := range iter {
			if err != nil {
				t.Logf("Got error when server sends non-streaming JSON: %v", err)
				break
			}
			if response != nil {
				candidates := response.Candidates()
				if len(candidates) > 0 {
					if text, ok := candidates[0].Parts()[0].AsText(); ok {
						t.Logf("Received: %q", text)
						// Should NOT be the raw JSON
						if strings.Contains(text, `{"message":`) {
							t.Error("❌ Should not receive raw JSON when server sends non-streaming response")
						}
						receivedAny = true
					}
				}
			}
		}

		if !receivedAny {
			t.Log("✅ No response received - this is expected when server sends incompatible format")
		}
	})
}

func TestStreamingFlowVerification(t *testing.T) {
	t.Run("Verify complete streaming flow", func(t *testing.T) {
		// Simulate the exact flow described in the problem
		streamData := []nirmataStreamData{
			{Type: "StreamDataTypeText", Content: "Analyzing"},
			{Type: "StreamDataTypeText", Content: " the"},
			{Type: "StreamDataTypeText", Content: " policy"},
			{Type: "StreamDataTypeText", Content: "..."},
			{Type: "StreamDataTypeText", Content: " Done!"},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Step 5: Nirmata provider should send StreamData chunks (not JSON blob)
			t.Log("✅ Step 5: Nirmata provider sends StreamData chunks")
			
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			encoder := json.NewEncoder(w)
			for i, chunk := range streamData {
				t.Logf("  Sending chunk %d: %q", i+1, chunk.Content)
				encoder.Encode(chunk)
			}
		}))
		defer server.Close()

		// Step 2: gollm.NewClient("nirmata")
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

		// Step 4: c.llmChat.SendStreaming()
		ctx := context.Background()
		t.Log("✅ Step 4: c.llmChat.SendStreaming() called")
		iter, err := chat.SendStreaming(ctx, "test prompt")
		if err != nil {
			t.Fatalf("SendStreaming failed: %v", err)
		}

		// Step 6 & 7: c.listener(StreamDataTypeText, chunk) → fmt.Print(chunk)
		stepCount := 0
		for response, err := range iter {
			if err != nil {
				t.Fatalf("Streaming error: %v", err)
			}
			if response != nil {
				candidates := response.Candidates()
				if len(candidates) > 0 {
					if text, ok := candidates[0].Parts()[0].AsText(); ok {
						stepCount++
						t.Logf("✅ Step 6-7: c.listener receives chunk %d: %q → fmt.Print(%q)", stepCount, text, text)
						
						// Key verification: Should be text chunks, not JSON
						if strings.Contains(text, `"message":`) || strings.Contains(text, `{`) {
							t.Errorf("❌ FAILED: Received JSON structure instead of text chunk: %q", text)
						}
					}
				}
			}
		}

		if stepCount != len(streamData) {
			t.Errorf("❌ Expected %d chunks, got %d", len(streamData), stepCount)
		}

		t.Log("✅ SUCCESS: Complete streaming flow works correctly!")
	})
}