package compression

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/kubectl-ai/pkg/api"
	"k8s.io/klog/v2"
)

// GetSummarizationPrompt returns the standard prompt for summarizing conversation history
func GetSummarizationPrompt(conversationText string) string {
	return fmt.Sprintf(`You are summarizing a conversation with the nctl AI agent, which helps users with Kubernetes policy-as-code tasks using Kyverno. The agent can write policies, create test resources, validate configurations, and execute various policy management operations using tools.

Please provide a comprehensive summary of the preceding conversation. IMPORTANT: This summary describes the state of the conversation BEFORE the messages that follow it. The conversation may have continued after this point, so do not assume this represents the final state.

Structure your response as follows:

## What Has Been Accomplished
- Policy requirements or objectives that were identified and discussed
- Specific policies that were created, modified, or analyzed (include names and purposes)
- Test resources that were generated and their validation results
- Key technical decisions made and constraints identified
- Any errors encountered and how they were resolved
- Tools and commands that were successfully executed

## Current State and Next Steps
- What work has been completed and is ready for use
- What is currently in progress or partially complete
- What remains to be done based on the original objectives
- Any pending tasks, follow-up actions, or next logical steps
- Current configuration state and any important context

## Technical Context
- Important technical details, configurations, or parameters that were established
- Any specific requirements, constraints, or considerations that were identified
- Dependencies or relationships between different components

Focus on the semantic progress of the policy development workflow and provide enough detail to enable seamless continuation of the conversation. Omit routine tool outputs but include meaningful results and outcomes.

Conversation to summarize:
%s`, conversationText)
}

// CompressionConfig holds configuration for conversation compression
type CompressionConfig struct {
	MaxTokens        int     // e.g., 4096 for Bedrock
	TriggerThreshold float64 // 0.70 (70%)
	TargetThreshold  float64 // 0.40 (40%)
}

// SummaryGenerator is a function type for generating summaries
type SummaryGenerator func(ctx context.Context, messages []*api.Message) (string, error)

// Compressor handles conversation history compression
type Compressor struct {
	config           CompressionConfig
	summaryGenerator SummaryGenerator
}

// NewCompressor creates a new compressor instance
func NewCompressor(config CompressionConfig, summaryGenerator SummaryGenerator) *Compressor {
	return &Compressor{
		config:           config,
		summaryGenerator: summaryGenerator,
	}
}

// CheckCompressionNeeded returns true if history exceeds trigger threshold
func (c *Compressor) CheckCompressionNeeded(history []*api.Message) bool {
	if len(history) == 0 {
		return false
	}

	totalTokens := EstimateHistoryTokens(history)
	triggerThreshold := int(float64(c.config.MaxTokens) * c.config.TriggerThreshold)

	klog.V(3).Infof("Token check: %d tokens, trigger threshold: %d", totalTokens, triggerThreshold)
	return totalTokens > triggerThreshold
}

// CompressHistory summarizes oldest messages and returns new compressed history
func (c *Compressor) CompressHistory(ctx context.Context, history []*api.Message) ([]*api.Message, error) {
	if len(history) == 0 {
		return history, nil
	}

	klog.V(0).Infof("Starting compression: %d messages, %d tokens", len(history), EstimateHistoryTokens(history))

	// Find the boundary for summarization
	boundary, err := c.findSummarizationBoundary(history)
	if err != nil {
		return nil, fmt.Errorf("failed to find summarization boundary: %w", err)
	}

	if boundary == 0 {
		klog.V(0).Infof("No messages to summarize, returning original history")
		return history, nil
	}

	// Generate summary for messages [0:boundary]
	summary, err := c.summaryGenerator(ctx, history[:boundary])
	if err != nil {
		return nil, fmt.Errorf("failed to generate summary: %w", err)
	}

	// Create summary message
	summaryMessage := &api.Message{
		ID:        fmt.Sprintf("summary-%d", time.Now().Unix()),
		Source:    api.MessageSourceAgent,
		Type:      api.MessageTypeText,
		Payload:   fmt.Sprintf("## Previous Conversation Summary\n\n%s", summary),
		Timestamp: time.Now(),
	}

	// Combine summary with remaining messages
	compressedHistory := append([]*api.Message{summaryMessage}, history[boundary:]...)

	klog.V(0).Infof("Compression complete: %d messages -> %d messages, %d tokens -> %d tokens",
		len(history), len(compressedHistory),
		EstimateHistoryTokens(history), EstimateHistoryTokens(compressedHistory))

	return compressedHistory, nil
}

// findSummarizationBoundary determines the index k where messages [0:k] should be summarized
func (c *Compressor) findSummarizationBoundary(history []*api.Message) (int, error) {
	if len(history) <= 2 {
		return 0, nil // Not enough messages to compress
	}

	// Calculate the halfway index of the current message history
	halfwayIndex := len(history) / 2
	klog.V(0).Infof("Finding boundary: history length=%d, halfway index=%d", len(history), halfwayIndex)

	// Look for the closest tool result message at or before the halfway point
	boundary := halfwayIndex
	for i := halfwayIndex; i >= 0; i-- {
		msg := history[i]
		// Check if this is a tool call response message
		if msg.Type == api.MessageTypeToolCallResponse {
			boundary = i + 1 // Include this message in the summary
			klog.V(0).Infof("Found tool result message at index %d, setting boundary to %d", i, boundary)
			break
		}
	}

	// Ensure we don't break tool call pairs
	boundary = c.ensureToolCallPairsIntact(history, boundary)

	klog.V(0).Infof("Selected boundary: %d (will summarize %d messages)", boundary, boundary)
	return boundary, nil
}

// ensureToolCallPairsIntact ensures that tool call request/response pairs are not separated
func (c *Compressor) ensureToolCallPairsIntact(history []*api.Message, boundary int) int {
	if boundary == 0 || boundary >= len(history) {
		return boundary
	}

	// Check if the message at boundary-1 is a tool call request
	// If so, we need to include its corresponding tool result
	if boundary > 0 && history[boundary-1].Type == api.MessageTypeToolCallRequest {
		// Look for the corresponding tool result after the boundary
		for i := boundary; i < len(history); i++ {
			if history[i].Type == api.MessageTypeToolCallResponse {
				// Found the tool result, adjust boundary to include it
				newBoundary := i + 1
				klog.V(0).Infof("Adjusting boundary from %d to %d to include tool call pair", boundary, newBoundary)
				return newBoundary
			}
		}
		// If no tool result found, we can't safely compress at this boundary
		// Move boundary back to avoid breaking the tool call
		newBoundary := boundary - 1
		klog.V(0).Infof("No tool result found for tool call at %d, adjusting boundary to %d", boundary-1, newBoundary)
		return newBoundary
	}

	return boundary
}

// ConvertMessagesToText converts a slice of messages to readable text format
// This is a utility function that can be used by summary generators
func ConvertMessagesToText(messages []*api.Message) string {
	var parts []string

	for _, msg := range messages {
		role := "User"
		if IsAssistantMessage(msg) {
			role = "Assistant"
		}

		content := GetMessageTextContent(msg)
		if content == "" {
			content = fmt.Sprintf("[%s message]", msg.Type)
		}

		// Truncate very long content for summarization
		if len(content) > 1000 {
			content = content[:1000] + "..."
		}

		parts = append(parts, fmt.Sprintf("%s: %s", role, content))
	}

	return strings.Join(parts, "\n\n")
}
