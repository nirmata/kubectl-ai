package compression

import (
	"context"
	"fmt"

	"github.com/GoogleCloudPlatform/kubectl-ai/pkg/api"
	"k8s.io/klog/v2"
)

// CompressionService provides shared compression functionality for all components
type CompressionService struct {
	compressor *Compressor
}

// NewCompressionService creates a new compression service with the given summary generator
func NewCompressionService(summaryGenerator SummaryGenerator) *CompressionService {

	compressor := NewCompressor(
		CompressionConfig{
			MaxTokens:        8192, // Updated MAX_TOKENS
			TriggerThreshold: 0.70, // 70%
			TargetThreshold:  0.50, // 50% (first half only)
		},
		summaryGenerator,
	)

	return &CompressionService{
		compressor: compressor,
	}
}

// CompressIfNeeded checks if compression is needed and applies it if so
func (cs *CompressionService) CompressIfNeeded(ctx context.Context, messages []*api.Message) ([]*api.Message, error) {
	if len(messages) <= 2 {
		return messages, nil // Not enough messages to compress
	}

	if !cs.compressor.CheckCompressionNeeded(messages) {
		return messages, nil // No compression needed
	}

	klog.V(0).Infof("Compression triggered: %d tokens exceeds threshold",
		EstimateHistoryTokens(messages))

	// Compress the history
	compressed, err := cs.compressor.CompressHistory(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("compression failed: %w", err)
	}

	klog.V(0).Infof("Compression complete: %d messages -> %d messages",
		len(messages), len(compressed))

	return compressed, nil
}

// CompressIfNeededWithFeedback checks if compression is needed and applies it with user feedback
func (cs *CompressionService) CompressIfNeededWithFeedback(ctx context.Context, messages []*api.Message) ([]*api.Message, error) {
	if len(messages) <= 2 {
		return messages, nil // Not enough messages to compress
	}

	if !cs.compressor.CheckCompressionNeeded(messages) {
		return messages, nil // No compression needed
	}

	fmt.Printf("\n🔄 Compressing conversation history (%d tokens exceeds threshold)...\n",
		EstimateHistoryTokens(messages))

	// Compress the history
	compressed, err := cs.compressor.CompressHistory(ctx, messages)
	if err != nil {
		fmt.Printf("⚠️ Compression failed: %v\n", err)
		return nil, fmt.Errorf("compression failed: %w", err)
	}

	fmt.Printf("✅ Conversation compressed: %d messages -> %d messages\n",
		len(messages), len(compressed))

	return compressed, nil
}
