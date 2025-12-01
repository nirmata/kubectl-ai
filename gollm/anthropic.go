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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/GoogleCloudPlatform/kubectl-ai/pkg/api"
	"k8s.io/klog/v2"
)

const (
	envAnthropicAPIKey  = "ANTHROPIC_API_KEY"
	envAnthropicBaseURL = "ANTHROPIC_BASE_URL"
	envAnthropicModel   = "ANTHROPIC_MODEL"

	defaultAnthropicBaseURL = "https://api.anthropic.com"
	defaultAnthropicModel   = "claude-sonnet-4-20250514"
	anthropicAPIVersion     = "2023-06-01"
)

// Registers the Anthropic provider factory on package initialization
func init() {
	if err := RegisterProvider("anthropic", newAnthropicClientFactory); err != nil {
		klog.Fatalf("Failed to register anthropic provider: %v", err)
	}
}

// Creates a new Anthropic client with the given options
func newAnthropicClientFactory(ctx context.Context, opts ClientOptions) (Client, error) {
	return NewAnthropicClient(ctx, opts)
}

// Implements the gollm.Client interface for Anthropic Claude models via HTTP
type AnthropicClient struct {
	baseURL    *url.URL
	httpClient *http.Client
	apiKey     string
}

var _ Client = &AnthropicClient{}

// Creates a new client for interacting with Anthropic Claude models
func NewAnthropicClient(ctx context.Context, opts ClientOptions) (*AnthropicClient, error) {
	apiKey := os.Getenv(envAnthropicAPIKey)
	if apiKey == "" {
		klog.Errorf("ANTHROPIC_API_KEY environment variable not set")
		return nil, fmt.Errorf("%s environment variable not set", envAnthropicAPIKey)
	}

	// Get base URL
	baseURLStr := os.Getenv(envAnthropicBaseURL)
	if baseURLStr == "" {
		if opts.URL != nil && opts.URL.Host != "" {
			baseURLStr = opts.URL.Scheme + "://" + opts.URL.Host
			klog.V(1).Infof("Using base URL from ClientOptions: %s", baseURLStr)
		} else {
			baseURLStr = defaultAnthropicBaseURL
			klog.V(1).Infof("Using default Anthropic endpoint: %s", baseURLStr)
		}
	} else {
		klog.V(1).Infof("Using Anthropic base URL from environment: %s", baseURLStr)
	}

	baseURL, err := url.Parse(baseURLStr)
	if err != nil {
		return nil, fmt.Errorf("parsing base URL: %w", err)
	}

	httpClient := createCustomHTTPClient(opts.SkipVerifySSL)

	client := &AnthropicClient{
		baseURL:    baseURL,
		httpClient: httpClient,
		apiKey:     apiKey,
	}

	return client, nil
}

func (c *AnthropicClient) Close() error {
	return nil
}

func (c *AnthropicClient) StartChat(systemPrompt, model string) Chat {
	selectedModel := getAnthropicModel(model)

	chat := &anthropicChat{
		client:       c,
		systemPrompt: systemPrompt,
		model:        selectedModel,
		messages:     []anthropicMessage{},
	}

	return chat
}

// Generates a single completion for the given request
func (c *AnthropicClient) GenerateCompletion(ctx context.Context, req *CompletionRequest) (CompletionResponse, error) {
	chat := c.StartChat("", req.Model)
	chatResponse, err := chat.Send(ctx, req.Prompt)
	if err != nil {
		return nil, err
	}

	return &anthropicCompletionResponse{
		chatResponse: chatResponse,
	}, nil
}

func (c *AnthropicClient) SetResponseSchema(schema *Schema) error {
	return fmt.Errorf("response schema not supported by Anthropic")
}

var anthropicHardcodedModels = []string{
	"claude-sonnet-4-20250514",
	"claude-3-7-sonnet-20250219",
	"claude-3-5-sonnet-20241022",
	"claude-3-opus-20240229",
	"claude-3-haiku-20240307",
}

// Retrieves list of supported Anthropic models by querying the API
func (c *AnthropicClient) ListModels(ctx context.Context) ([]string, error) {
	// Build URL for models endpoint
	u := c.baseURL.JoinPath("v1", "models")
	reqURL := u.String()

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)

	// Execute request
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		klog.V(1).Infof("Failed to fetch models from API, falling back to hardcoded list: %v", err)
		return anthropicHardcodedModels, nil
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		klog.V(1).Infof("API returned error status %d, falling back to hardcoded list: %s", httpResp.StatusCode, string(body))
		return anthropicHardcodedModels, nil
	}

	// Parse response
	var modelsResponse struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name,omitempty"`
		} `json:"data"`
	}

	if err := json.NewDecoder(httpResp.Body).Decode(&modelsResponse); err != nil {
		klog.V(1).Infof("Failed to parse models response, falling back to hardcoded list: %v", err)
		return anthropicHardcodedModels, nil
	}

	// Extract model IDs
	modelIDs := make([]string, 0, len(modelsResponse.Data))
	for _, model := range modelsResponse.Data {
		modelIDs = append(modelIDs, model.ID)
	}

	klog.V(1).Infof("Successfully fetched %d models from Anthropic API", len(modelIDs))
	return modelIDs, nil
}

// Returns the model to use
func getAnthropicModel(model string) string {
	if model != "" {
		klog.V(1).Infof("Using explicitly provided model: %s", model)
		return model
	}

	if envModel := os.Getenv(envAnthropicModel); envModel != "" {
		klog.V(1).Infof("Using model from environment variable: %s", envModel)
		return envModel
	}

	klog.V(1).Infof("Using default model: %s", defaultAnthropicModel)
	return defaultAnthropicModel
}

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicContentBlock struct {
	Type        string                 `json:"type"`
	Text        string                 `json:"text,omitempty"`
	PartialJSON string                 `json:"partial_json,omitempty"`
	ID          string                 `json:"id,omitempty"`
	ToolUseID   string                 `json:"tool_use_id,omitempty"`
	Name        string                 `json:"name,omitempty"`
	Input       map[string]interface{} `json:"input,omitempty"`
	Content     interface{}            `json:"content,omitempty"`
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Messages    []anthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
	Temperature *float64           `json:"temperature,omitempty"`
}

type anthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type anthropicResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Content      []anthropicContentBlock `json:"content"`
	Model        string                  `json:"model"`
	StopReason   string                  `json:"stop_reason"`
	StopSequence string                  `json:"stop_sequence,omitempty"`
	Usage        anthropicUsage          `json:"usage"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicStreamEvent struct {
	Type         string                 `json:"type"` // "message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"
	Message      *anthropicResponse     `json:"message,omitempty"`
	ContentBlock *anthropicContentBlock `json:"content_block,omitempty"`
	Delta        *anthropicContentBlock `json:"delta,omitempty"`
	Usage        *anthropicUsage        `json:"usage,omitempty"`
	Index        int                    `json:"index,omitempty"`
}

type anthropicErrorResponse struct {
	Error anthropicError `json:"error"`
}

type anthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type anthropicChat struct {
	client       *AnthropicClient
	systemPrompt string
	model        string
	messages     []anthropicMessage
	tools        []anthropicTool
	functionDefs []*FunctionDefinition
}

func (c *anthropicChat) Initialize(history []*api.Message) error {
	klog.V(1).Infof("Initializing Anthropic chat from resumed session: %d messages to process", len(history))
	c.messages = make([]anthropicMessage, 0, len(history))

	for i, msg := range history {
		// Convert api.Message to anthropicMessage
		var role string
		switch msg.Source {
		case api.MessageSourceUser:
			role = "user"
		case api.MessageSourceModel, api.MessageSourceAgent:
			role = "assistant"
		default:
			klog.V(1).Infof("  Skipping message %d: unknown source %s", i+1, msg.Source)
			continue
		}

		// Process the message based on its type
		contentBlocks, err := c.processAPIMessage(msg)
		if err != nil {
			klog.V(1).Infof("  Failed to process message %d (%s): %v", i+1, msg.ID, err)
			continue
		}

		if len(contentBlocks) == 0 {
			klog.V(1).Infof("  Skipping message %d: no content blocks generated", i+1)
			continue
		}

		anthropicMsg := anthropicMessage{
			Role:    role,
			Content: contentBlocks,
		}

		c.messages = append(c.messages, anthropicMsg)
	}

	klog.V(1).Infof("Anthropic chat initialized: %d messages in conversation history", len(c.messages))
	return nil
}

// Converts an api.Message to Anthropic content blocks
func (c *anthropicChat) processAPIMessage(msg *api.Message) ([]anthropicContentBlock, error) {
	var contentBlocks []anthropicContentBlock

	switch msg.Type {
	case api.MessageTypeText:
		if msg.Payload != nil {
			if msg.Payload != "" {
				contentBlocks = append(contentBlocks, anthropicContentBlock{
					Type: "text",
					Text: msg.Payload.(string),
				})
			} else {
				klog.V(2).Infof("processAPIMessage: textPayload is empty")
			}
		} else {
			klog.V(2).Infof("processAPIMessage: payload is nil")
		}

	case api.MessageTypeToolCallRequest:
		if msg.Payload != nil {
			var toolCallData map[string]any

			toolUse, err := c.createToolUseFromPayload(toolCallData)
			if err != nil {
				return nil, fmt.Errorf("failed to create tool use from payload: %w", err)
			}
			if toolUse != nil {
				klog.V(2).Infof("processAPIMessage: successfully created tool_use block")
				contentBlocks = append(contentBlocks, *toolUse)
			}
		} else {
			klog.V(2).Infof("processAPIMessage: payload is nil")
		}

	case api.MessageTypeToolCallResponse:
		if msg.Payload != nil {
			var toolResultData map[string]any

			toolResult, err := c.createToolResultFromPayload(toolResultData)
			if err != nil {
				return nil, fmt.Errorf("failed to create tool result from payload: %w", err)
			}
			if toolResult != nil {
				klog.V(2).Infof("processAPIMessage: successfully created tool_result block")
				contentBlocks = append(contentBlocks, *toolResult)
			}
		}

	default:
		klog.V(2).Infof("processAPIMessage: unknown message type: %s", msg.Type)
		return nil, fmt.Errorf("unknown message type: %s", msg.Type)
	}

	return contentBlocks, nil
}

func (c *anthropicChat) createToolUseFromPayload(payload map[string]any) (*anthropicContentBlock, error) {
	// Extract required fields
	toolUseID, hasID := payload["id"].(string)
	if !hasID || toolUseID == "" {
		return nil, fmt.Errorf("missing or invalid tool use ID")
	}

	name, hasName := payload["name"].(string)
	if !hasName || name == "" {
		return nil, fmt.Errorf("missing or invalid tool name")
	}

	// Extract arguments
	var args map[string]any
	if argsData, hasArgs := payload["arguments"]; hasArgs {
		if argsMap, ok := argsData.(map[string]any); ok {
			args = argsMap
		} else if argsStr, ok := argsData.(string); ok {
			// Try to parse JSON string
			if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
				args = make(map[string]any)
			}
		}
	}

	if args == nil {
		args = make(map[string]any)
	}

	return &anthropicContentBlock{
		Type:  "tool_use",
		ID:    toolUseID,
		Name:  name,
		Input: args,
	}, nil
}

func (c *anthropicChat) createToolResultFromPayload(payload map[string]any) (*anthropicContentBlock, error) {
	// Extract required fields
	toolUseID, hasID := payload["id"].(string)
	if !hasID || toolUseID == "" {
		if id, ok := payload["tool_call_id"].(string); ok && id != "" {
			toolUseID = id
		} else {
			return nil, fmt.Errorf("missing or invalid tool use ID")
		}
	}

	// Extract result content - Anthropic requires tool_result.content to be a string
	var content string
	if resultData, hasResult := payload["result"]; hasResult {
		if str, ok := resultData.(string); ok {
			content = str
		} else {
			jsonData, err := json.Marshal(resultData)
			if err != nil {
				content = fmt.Sprintf("%v", resultData)
			} else {
				content = string(jsonData)
			}
		}
	} else if output, hasOutput := payload["output"]; hasOutput {
		if str, ok := output.(string); ok {
			content = str
		} else {
			jsonData, err := json.Marshal(output)
			if err != nil {
				content = fmt.Sprintf("%v", output)
			} else {
				content = string(jsonData)
			}
		}
	} else {
		content = ""
	}

	return &anthropicContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUseID,
		Content:   content,
	}, nil
}

// Send sends a message to the chat and returns the response
func (c *anthropicChat) Send(ctx context.Context, contents ...any) (ChatResponse, error) {
	klog.V(1).Infof("Anthropic Send called with %d content items", len(contents))
	if len(contents) == 0 {
		return nil, errors.New("no content provided")
	}

	// Process contents to conversation history but don't commit yet
	var contentBlocks []anthropicContentBlock
	if err := c.processContents(contents, &contentBlocks); err != nil {
		klog.Errorf("Failed to process contents: %v", err)
		return nil, err
	}

	// Create a temporary message list that includes the new contents
	tempMessages := make([]anthropicMessage, len(c.messages))
	copy(tempMessages, c.messages)

	if len(contentBlocks) > 0 {
		tempMessages = append(tempMessages, anthropicMessage{
			Role:    "user",
			Content: contentBlocks,
		})
	}

	// Prepare the request
	reqBody := anthropicRequest{
		Model:     c.model,
		MaxTokens: 4096,
		Messages:  tempMessages,
	}

	if c.systemPrompt != "" {
		reqBody.System = c.systemPrompt
	}

	if len(c.tools) > 0 {
		reqBody.Tools = c.tools
		klog.V(1).Infof("Request includes %d tools", len(c.tools))
	}

	// Marshal request
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		klog.Errorf("Failed to marshal request: %v", err)
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	// Build URL
	u := c.client.baseURL.JoinPath("v1", "messages")
	reqURL := u.String()

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		klog.Errorf("Failed to create HTTP request: %v", err)
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.client.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)

	// Execute request
	httpResp, err := c.client.httpClient.Do(httpReq)
	if err != nil {
		klog.Errorf("HTTP request failed: %v", err)
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer httpResp.Body.Close()

	// Handle non-200 responses
	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		klog.Errorf("Anthropic API returned error: status=%d, body=%s", httpResp.StatusCode, string(body))
		var errorResp anthropicErrorResponse
		if err := json.Unmarshal(body, &errorResp); err == nil {
			return nil, NewAPIError(httpResp.StatusCode, errorResp.Error.Message, fmt.Errorf("%s: %s", errorResp.Error.Type, errorResp.Error.Message))
		}
		return nil, NewAPIError(httpResp.StatusCode, string(body), fmt.Errorf("HTTP %d: %s", httpResp.StatusCode, string(body)))
	}

	// Parse response
	klog.V(1).Info("Parsing Anthropic API response")
	var resp anthropicResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		klog.Errorf("Failed to unmarshal response: %v", err)
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}
	klog.V(1).Infof("Response parsed: ID=%s, Role=%s, ContentBlocks=%d, StopReason=%s, Usage={Input:%d, Output:%d}",
		resp.ID, resp.Role, len(resp.Content), resp.StopReason, resp.Usage.InputTokens, resp.Usage.OutputTokens)

	// Only commit to conversation history if the request succeeded
	if len(contentBlocks) > 0 {
		c.messages = append(c.messages, anthropicMessage{
			Role:    "user",
			Content: contentBlocks,
		})
	}

	// Update conversation history with assistant's response
	assistantMsg := anthropicMessage{
		Role:    "assistant",
		Content: resp.Content,
	}
	c.messages = append(c.messages, assistantMsg)

	// Return response
	klog.V(1).Info("Returning Anthropic response wrapper")
	return &anthropicResponseWrapper{
		response: resp,
		model:    c.model,
	}, nil
}

// SendStreaming sends a message and returns a streaming response
func (c *anthropicChat) SendStreaming(ctx context.Context, contents ...any) (ChatResponseIterator, error) {
	klog.V(1).Infof("Anthropic SendStreaming called with %d content items", len(contents))
	if len(contents) == 0 {
		return nil, errors.New("no content provided")
	}

	// Process contents to conversation history but don't commit yet
	var contentBlocks []anthropicContentBlock
	if err := c.processContents(contents, &contentBlocks); err != nil {
		klog.Errorf("Failed to process contents for streaming: %v", err)
		return nil, err
	}

	// Create a temporary message list that includes the new contents
	tempMessages := make([]anthropicMessage, len(c.messages))
	copy(tempMessages, c.messages)

	if len(contentBlocks) > 0 {
		tempMessages = append(tempMessages, anthropicMessage{
			Role:    "user",
			Content: contentBlocks,
		})
	}

	// Prepare the streaming request
	reqBody := anthropicRequest{
		Model:     c.model,
		MaxTokens: 4096,
		Messages:  tempMessages,
		Stream:    true,
	}

	if c.systemPrompt != "" {
		reqBody.System = c.systemPrompt
		klog.V(1).Infof("Streaming request includes system prompt (len=%d)", len(c.systemPrompt))
	}

	if len(c.tools) > 0 {
		reqBody.Tools = c.tools
		klog.V(1).Infof("Streaming request includes %d tools", len(c.tools))
	}

	// Marshal request
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		klog.Errorf("Failed to marshal streaming request: %v", err)
		return nil, fmt.Errorf("marshaling request: %w", err)
	}
	klog.V(1).Infof("Streaming request body size: %d bytes, messages: %d", len(bodyBytes), len(reqBody.Messages))

	// Build URL
	u := c.client.baseURL.JoinPath("v1", "messages")
	reqURL := u.String()
	klog.V(1).Infof("Sending streaming POST request to: %s", reqURL)

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		klog.Errorf("Failed to create streaming HTTP request: %v", err)
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.client.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
	klog.V(2).Infof("Streaming request headers set")

	// Execute request
	klog.V(1).Info("Executing streaming HTTP request to Anthropic API")
	httpResp, err := c.client.httpClient.Do(httpReq)
	if err != nil {
		klog.Errorf("Streaming HTTP request failed: %v", err)
		return nil, fmt.Errorf("executing request: %w", err)
	}
	klog.V(1).Infof("Streaming HTTP response received: status=%d", httpResp.StatusCode)

	// Handle non-200 responses
	if httpResp.StatusCode != http.StatusOK {
		defer httpResp.Body.Close()
		body, _ := io.ReadAll(httpResp.Body)
		klog.Errorf("Anthropic streaming API returned error: status=%d, body=%s", httpResp.StatusCode, string(body))
		var errorResp anthropicErrorResponse
		if err := json.Unmarshal(body, &errorResp); err == nil {
			return nil, NewAPIError(httpResp.StatusCode, errorResp.Error.Message, fmt.Errorf("%s: %s", errorResp.Error.Type, errorResp.Error.Message))
		}
		return nil, NewAPIError(httpResp.StatusCode, string(body), fmt.Errorf("HTTP %d: %s", httpResp.StatusCode, string(body)))
	}

	// Return streaming iterator
	klog.V(1).Info("Creating streaming iterator")
	return func(yield func(ChatResponse, error) bool) {
		defer httpResp.Body.Close()

		var fullContent strings.Builder
		var toolUses []anthropicContentBlock
		var usage *anthropicUsage
		// Track partial tool inputs by tool ID for accumulating input_json_delta events
		partialToolInputs := make(map[string]*strings.Builder)
		// Map content block index to tool ID (since event.Index is content block index, not toolUses index)
		contentBlockIndexToToolID := make(map[int]string)
		scanner := bufio.NewScanner(httpResp.Body)
		eventCount := 0

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			if !strings.HasPrefix(line, "data: ") {
				klog.V(2).Infof("Skipping non-SSE line: %q", line)
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				klog.V(2).Info("Received [DONE] marker, ending stream")
				break
			}

			// Parse JSON event
			eventCount++
			var event anthropicStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				klog.V(2).Infof("Failed to parse SSE event #%d: %v, data=%q", eventCount, err, data)
				continue
			}

			switch event.Type {
			case "content_block_delta":
				if event.Delta != nil {
					// Anthropic uses "text_delta" for streaming text deltas, not "text"
					if (event.Delta.Type == "text" || event.Delta.Type == "text_delta") && event.Delta.Text != "" {
						klog.V(2).Infof("Received text delta: len=%d, preview=%q", len(event.Delta.Text), event.Delta.Text)
						fullContent.WriteString(event.Delta.Text)
						response := &anthropicStreamResponse{
							content: event.Delta.Text,
							model:   c.model,
							done:    false,
						}
						klog.V(1).Infof("Yielding text response: len=%d", len(event.Delta.Text))
						if !yield(response, nil) {
							klog.V(2).Info("Yield function returned false, stopping stream")
							return
						}
					} else if event.Delta.Type == "input_json_delta" {
						// Handle streaming tool input JSON deltas
						// event.Index is the content block index, not the toolUses index
						var toolID string
						if event.Index >= 0 {
							// Look up the tool ID by content block index
							if id, ok := contentBlockIndexToToolID[event.Index]; ok {
								toolID = id
							} else {
								// Fallback: try to find by matching the most recently started tool use
								if len(toolUses) > 0 {
									toolID = toolUses[len(toolUses)-1].ID
									klog.V(2).Infof("Using most recent tool ID %s for input_json_delta (content block index %d not in map)", toolID, event.Index)
								}
							}
						}

						if toolID != "" {
							// Accumulate the JSON fragment
							if partialToolInputs[toolID] == nil {
								partialToolInputs[toolID] = &strings.Builder{}
							}
							// For input_json_delta, the JSON fragment is in the partial_json field
							jsonFragment := event.Delta.PartialJSON
							if jsonFragment != "" {
								partialToolInputs[toolID].WriteString(jsonFragment)
							}
						} else {
							klog.V(2).Infof("input_json_delta event but no tool ID found (content block index=%d, toolUses len=%d)", event.Index, len(toolUses))
						}
					}
				}

			case "content_block_start":
				if event.ContentBlock != nil {
					if event.ContentBlock.Type == "tool_use" {
						// Start of tool use - initialize
						toolUses = append(toolUses, *event.ContentBlock)
						// Map content block index to tool ID for later lookups
						if event.Index >= 0 && event.ContentBlock.ID != "" {
							contentBlockIndexToToolID[event.Index] = event.ContentBlock.ID
							klog.V(2).Infof("Mapped content block index %d to tool ID %s", event.Index, event.ContentBlock.ID)
						}
						// Initialize the partial input accumulator for this tool
						if event.ContentBlock.ID != "" {
							partialToolInputs[event.ContentBlock.ID] = &strings.Builder{}
						}
					}
				}

			case "content_block_stop":
				// content_block_stop may not have ContentBlock populated, but we can use the index
				// to find the corresponding tool use. event.Index is the content block index, not toolUses index.
				var toolID string
				var toolUseIndex int = -1

				if event.Index >= 0 {
					// Look up the tool ID by content block index
					if id, ok := contentBlockIndexToToolID[event.Index]; ok {
						toolID = id
						// Find the tool use in our slice by ID
						for i, toolUse := range toolUses {
							if toolUse.ID == toolID {
								toolUseIndex = i
								break
							}
						}
					}
				}

				if toolUseIndex >= 0 && toolUseIndex < len(toolUses) {
					// Parse the accumulated input JSON if we have it
					if accumulatedInput := partialToolInputs[toolID]; accumulatedInput != nil && accumulatedInput.Len() > 0 {
						// Parse the accumulated JSON and set it as Input
						accumulatedJSON := accumulatedInput.String()
						var inputMap map[string]interface{}
						if err := json.Unmarshal([]byte(accumulatedJSON), &inputMap); err == nil {
							toolUses[toolUseIndex].Input = inputMap
							klog.V(2).Infof("Set tool input for %s from accumulated JSON: %d bytes", toolID, len(accumulatedJSON))
						} else {
							klog.Errorf("Failed to parse accumulated tool input JSON for %s: %v, raw=%q", toolID, err, accumulatedJSON)
							// Try to use Input from ContentBlock if available
							if event.ContentBlock != nil && event.ContentBlock.Input != nil && len(event.ContentBlock.Input) > 0 {
								toolUses[toolUseIndex].Input = event.ContentBlock.Input
							}
						}
					} else if event.ContentBlock != nil && event.ContentBlock.Input != nil && len(event.ContentBlock.Input) > 0 {
						// Use the Input from content_block_stop if no accumulated input
						toolUses[toolUseIndex].Input = event.ContentBlock.Input
						klog.V(2).Infof("Set tool input for %s from ContentBlock", toolID)
					} else {
						klog.V(2).Infof("No input found for tool %s (no accumulated input and ContentBlock.Input is nil/empty)", toolID)
					}

					// Update name from ContentBlock if available
					if event.ContentBlock != nil && event.ContentBlock.Name != "" {
						toolUses[toolUseIndex].Name = event.ContentBlock.Name
					}

					// Ensure Input is set (required by API)
					if toolUses[toolUseIndex].Input == nil {
						toolUses[toolUseIndex].Input = make(map[string]interface{})
						klog.V(2).Infof("Initialized empty Input map for tool %s", toolID)
					}

					response := &anthropicStreamResponse{
						content:  "",
						model:    c.model,
						done:     false,
						toolUses: []anthropicContentBlock{toolUses[toolUseIndex]},
					}
					if !yield(response, nil) {
						klog.V(2).Info("Yield function returned false after tool use, stopping stream")
						return
					}
					delete(partialToolInputs, toolID)
					delete(contentBlockIndexToToolID, event.Index)
				} else {
					klog.V(2).Infof("content_block_stop event with invalid content block index %d (toolID=%s, toolUseIndex=%d, toolUses len=%d)", event.Index, toolID, toolUseIndex, len(toolUses))
				}

			case "message_delta":
				klog.V(1).Infof("message_delta event: Usage=%+v", event.Usage)
				// Message-level updates (e.g., stop reason)
				// Usage is provided in message_delta, not message_stop
				if event.Usage != nil {
					usage = event.Usage
					klog.V(2).Infof("Received usage in message_delta: Input=%d, Output=%d", usage.InputTokens, usage.OutputTokens)
				}

			case "message_stop":
				// Final message - yield final response with usage if available
				finalResponse := &anthropicStreamResponse{
					content: "",
					usage:   usage, // Use usage from message_delta if available
					model:   c.model,
					done:    true,
				}
				if usage != nil {
					klog.V(2).Infof("Stream completed: Usage={Input:%d, Output:%d}, TotalEvents=%d", usage.InputTokens, usage.OutputTokens, eventCount)
				} else {
					klog.V(2).Info("Stream completed but no usage metadata available")
				}
				yield(finalResponse, nil)
			default:
				klog.V(2).Infof("Unhandled event type: %s", event.Type)
			}
		}

		if err := scanner.Err(); err != nil {
			klog.Errorf("Scanner error during streaming: %v", err)
			yield(nil, fmt.Errorf("scanner error: %w", err))
			return
		}

		klog.V(1).Infof("Streaming completed: fullContentLen=%d, toolUses=%d, totalEvents=%d", fullContent.Len(), len(toolUses), eventCount)

		// Only commit to conversation history if the streaming succeeded
		if len(contentBlocks) > 0 {
			c.messages = append(c.messages, anthropicMessage{
				Role:    "user",
				Content: contentBlocks,
			})
			klog.V(1).Infof("Added user message to history: %d blocks", len(contentBlocks))
		}

		// Update conversation history with the full response
		assistantContent := []anthropicContentBlock{}
		if fullContent.Len() > 0 {
			assistantContent = append(assistantContent, anthropicContentBlock{
				Type: "text",
				Text: fullContent.String(),
			})
			klog.V(1).Infof("Added text content to assistant message: len=%d", fullContent.Len())
		}
		assistantContent = append(assistantContent, toolUses...)
		if len(toolUses) > 0 {
			klog.V(1).Infof("Added %d tool uses to assistant message", len(toolUses))
		}

		if len(assistantContent) > 0 {
			c.messages = append(c.messages, anthropicMessage{
				Role:    "assistant",
				Content: assistantContent,
			})
			klog.V(1).Infof("Added assistant message to history: %d content blocks", len(assistantContent))
		}
		klog.V(2).Infof("Streaming iterator completed: totalMessages=%d", len(c.messages))
	}, nil
}

// processContents processes contents into content blocks without modifying conversation history
func (c *anthropicChat) processContents(contents []any, contentBlocks *[]anthropicContentBlock) error {
	for _, content := range contents {
		switch v := content.(type) {
		case string:
			*contentBlocks = append(*contentBlocks, anthropicContentBlock{
				Type: "text",
				Text: v,
			})
		case FunctionCallResult:
			// Convert to tool result content block
			// Anthropic requires tool_result.content to be a string, not an object
			var resultContent string
			if v.Result != nil {
				// v.Result is already map[string]any, so marshal it to JSON string
				jsonData, err := json.Marshal(v.Result)
				if err != nil {
					resultContent = fmt.Sprintf("%v", v.Result)
				} else {
					resultContent = string(jsonData)
				}
			} else {
				resultContent = ""
			}

			*contentBlocks = append(*contentBlocks, anthropicContentBlock{
				Type:      "tool_result",
				ToolUseID: v.ID,
				Content:   resultContent,
			})
		case []interface{}:
			for j, item := range v {
				if err := c.processContents([]any{item}, contentBlocks); err != nil {
					return fmt.Errorf("failed to process array item %d: %w", j, err)
				}
			}
		default:
			klog.Errorf("Unhandled content type: %T", content)
			return fmt.Errorf("unhandled content type: %T", content)
		}
	}

	return nil
}

func (c *anthropicChat) SetFunctionDefinitions(functions []*FunctionDefinition) error {
	klog.V(1).Infof("Setting %d function definitions for Anthropic chat", len(functions))
	c.functionDefs = functions

	if len(functions) == 0 {
		c.tools = nil
		klog.V(1).Info("No functions provided, tools cleared")
		return nil
	}

	c.tools = make([]anthropicTool, 0, len(functions))
	for _, fn := range functions {
		// Convert gollm function definition to Anthropic tool format
		inputSchema := make(map[string]interface{})
		if fn.Parameters != nil {
			// Convert Schema to map[string]interface{}
			jsonData, err := json.Marshal(fn.Parameters)
			if err != nil {
				return fmt.Errorf("failed to marshal function parameters: %w", err)
			}
			if err := json.Unmarshal(jsonData, &inputSchema); err != nil {
				return fmt.Errorf("failed to unmarshal function parameters: %w", err)
			}
		} else {
			// Provide minimal schema if none specified
			inputSchema = map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			}
		}

		tool := anthropicTool{
			Name:        fn.Name,
			Description: fn.Description,
			InputSchema: inputSchema,
		}

		c.tools = append(c.tools, tool)
		klog.V(1).Infof("Converted function %s to Anthropic tool", fn.Name)
	}

	klog.V(1).Infof("Set %d function definitions for Anthropic chat", len(c.tools))
	return nil
}

func (c *anthropicChat) IsRetryableError(err error) bool {
	return DefaultIsRetryableError(err)
}

type anthropicResponseWrapper struct {
	response anthropicResponse
	model    string
}

func (r *anthropicResponseWrapper) UsageMetadata() any {
	return r.response.Usage
}

func (r *anthropicResponseWrapper) Candidates() []Candidate {
	candidate := &anthropicCandidate{
		response: r.response,
		model:    r.model,
	}
	return []Candidate{candidate}
}

type anthropicStreamResponse struct {
	content  string
	usage    *anthropicUsage
	model    string
	done     bool
	toolUses []anthropicContentBlock
}

func (r *anthropicStreamResponse) UsageMetadata() any {
	return r.usage
}

func (r *anthropicStreamResponse) Candidates() []Candidate {
	if r.content == "" && r.usage == nil && len(r.toolUses) == 0 {
		return []Candidate{}
	}

	candidate := &anthropicStreamCandidate{
		content:  r.content,
		model:    r.model,
		toolUses: r.toolUses,
	}
	return []Candidate{candidate}
}

type anthropicCandidate struct {
	response anthropicResponse
	model    string
}

func (c *anthropicCandidate) String() string {
	var content strings.Builder
	for _, block := range c.response.Content {
		if block.Type == "text" {
			content.WriteString(block.Text)
		}
	}
	return content.String()
}

func (c *anthropicCandidate) Parts() []Part {
	var parts []Part
	for _, block := range c.response.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				parts = append(parts, &anthropicTextPart{text: block.Text})
			}
		case "tool_use":
			parts = append(parts, &anthropicToolPart{block: block})
		}
	}
	return parts
}

type anthropicStreamCandidate struct {
	content  string
	model    string
	toolUses []anthropicContentBlock
}

func (c *anthropicStreamCandidate) String() string {
	return c.content
}

func (c *anthropicStreamCandidate) Parts() []Part {
	var parts []Part

	if c.content != "" {
		parts = append(parts, &anthropicTextPart{text: c.content})
	}

	for _, toolUse := range c.toolUses {
		parts = append(parts, &anthropicToolPart{block: toolUse})
	}

	return parts
}

type anthropicTextPart struct {
	text string
}

func (p *anthropicTextPart) AsText() (string, bool) {
	return p.text, true
}

func (p *anthropicTextPart) AsFunctionCalls() ([]FunctionCall, bool) {
	return nil, false
}

type anthropicToolPart struct {
	block anthropicContentBlock
}

func (p *anthropicToolPart) AsText() (string, bool) {
	return "", false
}

func (p *anthropicToolPart) AsFunctionCalls() ([]FunctionCall, bool) {
	if p.block.Type != "tool_use" {
		return nil, false
	}

	funcCall := FunctionCall{
		ID:        p.block.ID, // Anthropic uses "id" field for tool_use
		Name:      p.block.Name,
		Arguments: p.block.Input,
	}

	return []FunctionCall{funcCall}, true
}

type anthropicCompletionResponse struct {
	chatResponse ChatResponse
}

var _ CompletionResponse = (*anthropicCompletionResponse)(nil)

func (r *anthropicCompletionResponse) Response() string {
	if r.chatResponse == nil {
		return ""
	}
	candidates := r.chatResponse.Candidates()
	if len(candidates) == 0 {
		return ""
	}
	parts := candidates[0].Parts()
	for _, part := range parts {
		if text, ok := part.AsText(); ok {
			return text
		}
	}
	return ""
}

func (r *anthropicCompletionResponse) UsageMetadata() any {
	if r.chatResponse == nil {
		return nil
	}
	return r.chatResponse.UsageMetadata()
}
