package compression

import (
	"encoding/json"
	"fmt"

	"github.com/GoogleCloudPlatform/kubectl-ai/pkg/api"
)

// EstimateTokens provides rough token count for text using the standard 4-character-per-token heuristic
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	// Rough approximation: 4 characters ≈ 1 token for English text
	return len(text) / 4
}

// EstimateMessageTokens calculates tokens for a single api.Message
func EstimateMessageTokens(msg *api.Message) int {
	if msg == nil {
		return 0
	}

	totalTokens := 0

	// Estimate tokens for the message ID (small overhead)
	totalTokens += EstimateTokens(msg.ID)

	// Estimate tokens for the payload based on type
	switch msg.Type {
	case api.MessageTypeText:
		if textPayload, ok := msg.Payload.(string); ok {
			totalTokens += EstimateTokens(textPayload)
		}

	case api.MessageTypeToolCallRequest, api.MessageTypeToolCallResponse:
		// For tool calls, estimate tokens in the payload structure
		if msg.Payload != nil {
			// Convert payload to JSON string for estimation
			if jsonBytes, err := json.Marshal(msg.Payload); err == nil {
				totalTokens += EstimateTokens(string(jsonBytes))
			} else {
				// Fallback: estimate based on string representation
				totalTokens += EstimateTokens(fmt.Sprintf("%v", msg.Payload))
			}
		}

	case api.MessageTypeError:
		if errorPayload, ok := msg.Payload.(string); ok {
			totalTokens += EstimateTokens(errorPayload)
		}

	case api.MessageTypeUserInputRequest, api.MessageTypeUserInputResponse:
		if inputPayload, ok := msg.Payload.(string); ok {
			totalTokens += EstimateTokens(inputPayload)
		}

	case api.MessageTypeUserChoiceRequest, api.MessageTypeUserChoiceResponse:
		// For choice messages, estimate based on JSON representation
		if jsonBytes, err := json.Marshal(msg.Payload); err == nil {
			totalTokens += EstimateTokens(string(jsonBytes))
		} else {
			totalTokens += EstimateTokens(fmt.Sprintf("%v", msg.Payload))
		}

	default:
		// For unknown types, estimate based on string representation
		totalTokens += EstimateTokens(fmt.Sprintf("%v", msg.Payload))
	}

	// Add small overhead for message metadata (Source, Type, Timestamp)
	totalTokens += 10

	return totalTokens
}

// EstimateHistoryTokens calculates total tokens for an array of messages
func EstimateHistoryTokens(messages []*api.Message) int {
	if len(messages) == 0 {
		return 0
	}

	totalTokens := 0
	for _, msg := range messages {
		totalTokens += EstimateMessageTokens(msg)
	}

	return totalTokens
}

// EstimateTokensFromText is a helper function for estimating tokens from plain text
// This is useful for system prompts, user messages, etc.
func EstimateTokensFromText(text string) int {
	return EstimateTokens(text)
}

// EstimateTokensFromMessages is a helper function that takes a slice of messages
// and returns the total estimated token count
func EstimateTokensFromMessages(messages []*api.Message) int {
	return EstimateHistoryTokens(messages)
}

// GetMessageTextContent extracts text content from a message for token estimation
func GetMessageTextContent(msg *api.Message) string {
	if msg == nil {
		return ""
	}

	switch msg.Type {
	case api.MessageTypeText:
		if text, ok := msg.Payload.(string); ok {
			return text
		}
	case api.MessageTypeError:
		if text, ok := msg.Payload.(string); ok {
			return text
		}
	case api.MessageTypeUserInputRequest, api.MessageTypeUserInputResponse:
		if text, ok := msg.Payload.(string); ok {
			return text
		}
	}

	// For other types, return string representation
	return fmt.Sprintf("%v", msg.Payload)
}

// IsToolCallMessage checks if a message is part of a tool call sequence
func IsToolCallMessage(msg *api.Message) bool {
	if msg == nil {
		return false
	}
	return msg.Type == api.MessageTypeToolCallRequest || msg.Type == api.MessageTypeToolCallResponse
}

// IsAssistantMessage checks if a message is from the assistant/model
func IsAssistantMessage(msg *api.Message) bool {
	if msg == nil {
		return false
	}
	return msg.Source == api.MessageSourceModel || msg.Source == api.MessageSourceAgent
}

// IsUserMessage checks if a message is from the user
func IsUserMessage(msg *api.Message) bool {
	if msg == nil {
		return false
	}
	return msg.Source == api.MessageSourceUser
}
