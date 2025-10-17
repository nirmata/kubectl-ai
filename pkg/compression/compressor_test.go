package compression

import (
	"testing"

	"github.com/GoogleCloudPlatform/kubectl-ai/pkg/api"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{
			name:     "empty string",
			text:     "",
			expected: 0,
		},
		{
			name:     "short text",
			text:     "Hello world",
			expected: 2, // 11 chars / 4 = 2
		},
		{
			name:     "longer text",
			text:     "This is a longer text that should have more tokens",
			expected: 12, // 50 chars / 4 = 12
		},
		{
			name:     "text with spaces",
			text:     "Hello world with spaces",
			expected: 5, // 23 chars / 4 = 5
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EstimateTokens(tt.text)
			if result != tt.expected {
				t.Errorf("EstimateTokens(%q) = %d, expected %d", tt.text, result, tt.expected)
			}
		})
	}
}

func TestEstimateMessageTokens(t *testing.T) {
	tests := []struct {
		name     string
		message  *api.Message
		expected int
	}{
		{
			name: "text message",
			message: &api.Message{
				ID:      "test-1",
				Source:  api.MessageSourceUser,
				Type:    api.MessageTypeText,
				Payload: "Hello world",
			},
			expected: 13, // 2 (text) + 11 (overhead including ID)
		},
		{
			name: "empty message",
			message: &api.Message{
				ID:      "test-2",
				Source:  api.MessageSourceUser,
				Type:    api.MessageTypeText,
				Payload: "",
			},
			expected: 11, // 0 (text) + 11 (overhead including ID)
		},
		{
			name:     "nil message",
			message:  nil,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EstimateMessageTokens(tt.message)
			if result != tt.expected {
				t.Errorf("EstimateMessageTokens() = %d, expected %d", result, tt.expected)
			}
		})
	}
}

func TestEstimateHistoryTokens(t *testing.T) {
	messages := []*api.Message{
		{
			ID:      "msg-1",
			Source:  api.MessageSourceUser,
			Type:    api.MessageTypeText,
			Payload: "Hello",
		},
		{
			ID:      "msg-2",
			Source:  api.MessageSourceAgent,
			Type:    api.MessageTypeText,
			Payload: "Hi there",
		},
	}

	expected := 2*11 + 1 + 2 // 2 messages * 11 overhead + 1 token (Hello) + 2 tokens (Hi there)
	result := EstimateHistoryTokens(messages)

	if result != expected {
		t.Errorf("EstimateHistoryTokens() = %d, expected %d", result, expected)
	}
}

func TestIsToolCallMessage(t *testing.T) {
	tests := []struct {
		name     string
		message  *api.Message
		expected bool
	}{
		{
			name: "tool call request",
			message: &api.Message{
				Type: api.MessageTypeToolCallRequest,
			},
			expected: true,
		},
		{
			name: "tool call response",
			message: &api.Message{
				Type: api.MessageTypeToolCallResponse,
			},
			expected: true,
		},
		{
			name: "text message",
			message: &api.Message{
				Type: api.MessageTypeText,
			},
			expected: false,
		},
		{
			name:     "nil message",
			message:  nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsToolCallMessage(tt.message)
			if result != tt.expected {
				t.Errorf("IsToolCallMessage() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestIsAssistantMessage(t *testing.T) {
	tests := []struct {
		name     string
		message  *api.Message
		expected bool
	}{
		{
			name: "model source",
			message: &api.Message{
				Source: api.MessageSourceModel,
			},
			expected: true,
		},
		{
			name: "agent source",
			message: &api.Message{
				Source: api.MessageSourceAgent,
			},
			expected: true,
		},
		{
			name: "user source",
			message: &api.Message{
				Source: api.MessageSourceUser,
			},
			expected: false,
		},
		{
			name:     "nil message",
			message:  nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAssistantMessage(tt.message)
			if result != tt.expected {
				t.Errorf("IsAssistantMessage() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestCompressionConfig(t *testing.T) {
	config := CompressionConfig{
		MaxTokens:        8192,
		TriggerThreshold: 0.70,
		TargetThreshold:  0.50,
	}

	if config.MaxTokens != 8192 {
		t.Errorf("MaxTokens = %d, expected 8192", config.MaxTokens)
	}
	if config.TriggerThreshold != 0.70 {
		t.Errorf("TriggerThreshold = %f, expected 0.70", config.TriggerThreshold)
	}
	if config.TargetThreshold != 0.50 {
		t.Errorf("TargetThreshold = %f, expected 0.50", config.TargetThreshold)
	}
}

func TestCheckCompressionNeeded(t *testing.T) {
	// Create a mock compressor
	config := CompressionConfig{
		MaxTokens:        100, // Low threshold for testing
		TriggerThreshold: 0.70,
		TargetThreshold:  0.40,
	}

	compressor := &Compressor{
		config: config,
	}

	tests := []struct {
		name     string
		messages []*api.Message
		expected bool
	}{
		{
			name:     "empty history",
			messages: []*api.Message{},
			expected: false,
		},
		{
			name: "below threshold",
			messages: []*api.Message{
				{
					ID:      "msg-1",
					Source:  api.MessageSourceUser,
					Type:    api.MessageTypeText,
					Payload: "Short message",
				},
			},
			expected: false,
		},
		{
			name: "above threshold",
			messages: []*api.Message{
				{
					ID:      "msg-1",
					Source:  api.MessageSourceUser,
					Type:    api.MessageTypeText,
					Payload: "This is a very long message that should exceed the token threshold when combined with the message overhead and other factors that contribute to the total token count",
				},
				{
					ID:      "msg-2",
					Source:  api.MessageSourceAgent,
					Type:    api.MessageTypeText,
					Payload: "Another long response that adds to the token count and should push us over the 70% threshold of 100 tokens",
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compressor.CheckCompressionNeeded(tt.messages)
			if result != tt.expected {
				t.Errorf("CheckCompressionNeeded() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestFindSummarizationBoundary(t *testing.T) {
	// Create a mock compressor
	config := CompressionConfig{
		MaxTokens:        50, // Very low threshold for testing
		TriggerThreshold: 0.70,
		TargetThreshold:  0.40,
	}

	compressor := &Compressor{
		config: config,
	}

	tests := []struct {
		name     string
		messages []*api.Message
		expected int
	}{
		{
			name:     "empty history",
			messages: []*api.Message{},
			expected: 0,
		},
		{
			name: "single message",
			messages: []*api.Message{
				{
					ID:      "msg-1",
					Source:  api.MessageSourceUser,
					Type:    api.MessageTypeText,
					Payload: "Short message",
				},
			},
			expected: 0, // Should not summarize anything
		},
		{
			name: "tool_result_boundary_finding",
			messages: []*api.Message{
				{
					ID:      "msg-1",
					Source:  api.MessageSourceUser,
					Type:    api.MessageTypeText,
					Payload: "User message 1",
				},
				{
					ID:      "msg-2",
					Source:  api.MessageSourceAgent,
					Type:    api.MessageTypeToolCallRequest,
					Payload: map[string]interface{}{"name": "test_tool"},
				},
				{
					ID:      "msg-3",
					Source:  api.MessageSourceAgent,
					Type:    api.MessageTypeToolCallResponse,
					Payload: "Tool result",
				},
				{
					ID:      "msg-4",
					Source:  api.MessageSourceUser,
					Type:    api.MessageTypeText,
					Payload: "User message 2",
				},
				{
					ID:      "msg-5",
					Source:  api.MessageSourceAgent,
					Type:    api.MessageTypeText,
					Payload: "Assistant response 2",
				},
			},
			expected: 3, // Halfway is 2, closest tool result before halfway is at index 2, so boundary = 3
		},
		{
			name: "tool_call_pair_protection",
			messages: []*api.Message{
				{
					ID:      "msg-1",
					Source:  api.MessageSourceUser,
					Type:    api.MessageTypeText,
					Payload: "User message 1",
				},
				{
					ID:      "msg-2",
					Source:  api.MessageSourceAgent,
					Type:    api.MessageTypeToolCallRequest,
					Payload: map[string]interface{}{"name": "test_tool"},
				},
				{
					ID:      "msg-3",
					Source:  api.MessageSourceUser,
					Type:    api.MessageTypeText,
					Payload: "User message 2",
				},
				{
					ID:      "msg-4",
					Source:  api.MessageSourceAgent,
					Type:    api.MessageTypeToolCallResponse,
					Payload: "Tool result",
				},
				{
					ID:      "msg-5",
					Source:  api.MessageSourceAgent,
					Type:    api.MessageTypeText,
					Payload: "Assistant response",
				},
			},
			expected: 4, // Halfway is 2, but msg-2 is a tool call request, so we need to include msg-4 (tool result)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			totalTokens := EstimateHistoryTokens(tt.messages)
			targetTokens := int(float64(config.MaxTokens) * config.TargetThreshold)
			t.Logf("Total tokens: %d, target: %d", totalTokens, targetTokens)

			result, err := compressor.findSummarizationBoundary(tt.messages)
			if err != nil {
				t.Errorf("findSummarizationBoundary() error = %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("findSummarizationBoundary() = %d, expected %d", result, tt.expected)
			}
		})
	}
}

func TestConvertMessagesToText(t *testing.T) {
	messages := []*api.Message{
		{
			ID:      "msg-1",
			Source:  api.MessageSourceUser,
			Type:    api.MessageTypeText,
			Payload: "Hello",
		},
		{
			ID:      "msg-2",
			Source:  api.MessageSourceAgent,
			Type:    api.MessageTypeText,
			Payload: "Hi there",
		},
	}

	result := ConvertMessagesToText(messages)
	expected := "User: Hello\n\nAssistant: Hi there"

	if result != expected {
		t.Errorf("convertMessagesToText() = %q, expected %q", result, expected)
	}
}

func TestGetMessageTextContent(t *testing.T) {
	tests := []struct {
		name     string
		message  *api.Message
		expected string
	}{
		{
			name: "text message",
			message: &api.Message{
				Type:    api.MessageTypeText,
				Payload: "Hello world",
			},
			expected: "Hello world",
		},
		{
			name: "error message",
			message: &api.Message{
				Type:    api.MessageTypeError,
				Payload: "Error occurred",
			},
			expected: "Error occurred",
		},
		{
			name: "tool call message",
			message: &api.Message{
				Type:    api.MessageTypeToolCallRequest,
				Payload: map[string]interface{}{"name": "test_tool"},
			},
			expected: "map[name:test_tool]",
		},
		{
			name:     "nil message",
			message:  nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetMessageTextContent(tt.message)
			if result != tt.expected {
				t.Errorf("GetMessageTextContent() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

// Note: TestExtractTextFromResponse is skipped due to import cycle issues
// This would require a mock implementation of gollm.ChatResponse
// which would create circular dependencies
