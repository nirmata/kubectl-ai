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

// Register the Nirmata provider factory on package initialization
func init() {
	if err := RegisterProvider("nirmata", newNirmataClientFactory); err != nil {
		klog.Fatalf("Failed to register nirmata provider: %v", err)
	}
}

const (
	NIRMATA_APIKEY_ENV   = "NIRMATA_APIKEY"
	NIRMATA_ENDPOINT_ENV = "NIRMATA_ENDPOINT"

	DEFAULT_NIRMATA_ENDPOINT = "https://nirmata.io"

	DEFAULT_NIRMATA_MODEL = "us.anthropic.claude-sonnet-4-20250514-v1:0"

	DEFAULT_NIRMATA_REGION = "us-west-2"
)

// newNirmataClientFactory creates a new Nirmata client with the given options
func newNirmataClientFactory(ctx context.Context, opts ClientOptions) (Client, error) {
	return NewNirmataClient(ctx, opts)
}

// NirmataClient implements the gollm.Client interface for Nirmata models via HTTP
type NirmataClient struct {
	baseURL       *url.URL
	httpClient    *http.Client
	apiKey        string
	supportsTools bool // Feature flag for tool support
}

// Ensure NirmataClient implements the Client interface
var _ Client = &NirmataClient{}

func NewNirmataClient(ctx context.Context, opts ClientOptions) (*NirmataClient, error) {
	apiKey := os.Getenv(NIRMATA_APIKEY_ENV)
	if apiKey == "" {
		return nil, fmt.Errorf("%s environment variable not set", NIRMATA_APIKEY_ENV)
	}

	baseURLStr := os.Getenv(NIRMATA_ENDPOINT_ENV)
	if baseURLStr == "" {
		klog.V(1).Infof("Using default endpoint: %s", DEFAULT_NIRMATA_ENDPOINT)
		baseURLStr = DEFAULT_NIRMATA_ENDPOINT
	}

	baseURL, err := url.Parse(baseURLStr)
	if err != nil {
		return nil, fmt.Errorf("parsing base URL: %w", err)
	}

	httpClient := createCustomHTTPClient(opts.SkipVerifySSL)

	client := &NirmataClient{
		baseURL:    baseURL,
		httpClient: httpClient,
		apiKey:     apiKey,
	}

	// Tool calling is required for Nirmata provider
	client.supportsTools = true

	return client, nil
}

// checkToolSupport checks if the backend supports tool calling
func checkToolSupport(ctx context.Context, client *NirmataClient) bool {
	// Tool calling is required for Nirmata provider
	// This matches the expectation that all providers must support tools
	return true
}

// setAuthHeader sets the Authorization header using NIRMATA-API format.
// Simple and clean like other providers (OpenAI, Grok, Gemini).
func (c *NirmataClient) setAuthHeader(req *http.Request) {
	req.Header.Set("Authorization", "NIRMATA-API "+c.apiKey)
}

func (c *NirmataClient) Close() error {
	return nil
}

func (c *NirmataClient) StartChat(systemPrompt, model string) Chat {
	selectedModel := getNirmataModel(model)

	chat := &nirmataChat{
		client:       c,
		systemPrompt: systemPrompt,
		model:        selectedModel,
		history:      []nirmataMessage{},
	}

	if systemPrompt != "" {
		chat.history = append(chat.history, nirmataMessage{
			Role:    "system",
			Content: systemPrompt,
		})
	}

	return chat
}

func (c *NirmataClient) GenerateCompletion(ctx context.Context, req *CompletionRequest) (CompletionResponse, error) {
	chat := c.StartChat("", req.Model)
	chatResponse, err := chat.Send(ctx, req.Prompt)
	if err != nil {
		return nil, err
	}

	return &nirmataCompletionResponse{
		chatResponse: chatResponse,
	}, nil
}

func (c *NirmataClient) SetResponseSchema(schema *Schema) error {
	return fmt.Errorf("response schema not supported by Nirmata")
}

func (c *NirmataClient) ListModels(ctx context.Context) ([]string, error) {
	return []string{
		"us.anthropic.claude-sonnet-4-20250514-v1:0",   // Claude Sonnet 4 (default)
		"us.anthropic.claude-3-7-sonnet-20250219-v1:0", // Claude 3.7 Sonnet
	}, nil
}

type nirmataChat struct {
	client       *NirmataClient
	systemPrompt string
	model        string
	history      []nirmataMessage
	functionDefs []*FunctionDefinition
	tools        []nirmataToolDef // Converted tools for API
}

type nirmataMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content,omitempty"`
	ToolCalls  []nirmataToolCall `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
}

type nirmataChatRequest struct {
	Messages   []nirmataMessage `json:"messages"`
	Tools      []nirmataToolDef `json:"tools,omitempty"`
	ToolChoice interface{}      `json:"tool_choice,omitempty"`
	Model      string           `json:"model,omitempty"`
	Stream     bool             `json:"stream,omitempty"`
}

type nirmataChatResponse struct {
	Message   string            `json:"message"`
	ToolCalls []nirmataToolCall `json:"tool_calls,omitempty"`
	Usage     *nirmataUsage     `json:"usage,omitempty"`
	Metadata  any               `json:"metadata,omitempty"`
}

type nirmataStreamData struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
}

// Tool-related structures
type nirmataToolDef struct {
	Type     string              `json:"type"`
	Function nirmataToolFunction `json:"function"`
}

type nirmataToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type nirmataToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type nirmataUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (c *nirmataChat) Initialize(history []*api.Message) error {
	c.history = make([]nirmataMessage, 0, len(history))

	if c.systemPrompt != "" {
		c.history = append(c.history, nirmataMessage{
			Role:    "system",
			Content: c.systemPrompt,
		})
	}

	for _, msg := range history {
		role := "user"
		switch msg.Source {
		case api.MessageSourceUser:
			role = "user"
		case api.MessageSourceModel, api.MessageSourceAgent:
			role = "assistant"
		default:
			continue
		}

		var content string
		if msg.Type == api.MessageTypeText && msg.Payload != nil {
			if textPayload, ok := msg.Payload.(string); ok {
				content = textPayload
			} else {
				content = fmt.Sprintf("%v", msg.Payload)
			}
		} else {
			continue
		}

		if content == "" {
			continue
		}

		nirmataMsg := nirmataMessage{
			Role:    role,
			Content: content,
		}

		c.history = append(c.history, nirmataMsg)
	}

	return nil
}

func (c *nirmataChat) Send(ctx context.Context, contents ...any) (ChatResponse, error) {
	if len(contents) == 0 {
		return nil, errors.New("no content provided")
	}

	userMessage := c.convertContentsToMessage(contents)
	messages := append(c.history, userMessage)
	req := nirmataChatRequest{
		Messages: messages,
		Model:    c.model,
	}

	// Add tools if defined (tools always supported like other providers)
	if len(c.tools) > 0 {
		req.Tools = c.tools
		req.ToolChoice = "auto"
	}

	var resp nirmataChatResponse
	if err := c.client.doRequestWithModel(ctx, "llm-apps/chat", c.model, req, &resp); err != nil {
		return nil, err
	}

	c.history = append(c.history, userMessage)

	// Create assistant message for history
	assistantMsg := nirmataMessage{
		Role:    "assistant",
		Content: resp.Message,
	}
	if len(resp.ToolCalls) > 0 {
		assistantMsg.ToolCalls = resp.ToolCalls
	}
	c.history = append(c.history, assistantMsg)

	response := &nirmataResponse{
		message:   resp.Message,
		toolCalls: resp.ToolCalls,
		usage:     resp.Usage,
		metadata:  resp.Metadata,
		model:     c.model,
	}

	return response, nil
}

func (c *nirmataChat) SendStreaming(ctx context.Context, contents ...any) (ChatResponseIterator, error) {
	if len(contents) == 0 {
		return nil, errors.New("no content provided")
	}

	// Convert contents to user message
	userMessage := c.convertContentsToMessage(contents)

	// Build complete message history
	messages := append(c.history, userMessage)

	// Create request
	req := nirmataChatRequest{
		Messages: messages,
		Model:    c.model,
		Stream:   true,
	}

	// Add tools if defined (tools always supported like other providers)
	if len(c.tools) > 0 {
		req.Tools = c.tools
		req.ToolChoice = "auto"
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	u := c.client.baseURL.JoinPath("llm-apps").JoinPath("chat")
	q := u.Query()
	if c.model != "" {
		q.Set("model", c.model)
	}
	q.Set("chunked", "true")
	// Issue #4 fix: Don't force provider - let backend decide based on its configuration
	// Removed: q.Set("provider", "bedrock")
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	c.client.setAuthHeader(httpReq)

	httpResp, err := c.client.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		defer httpResp.Body.Close()
		body, _ := io.ReadAll(httpResp.Body)

		var errorMsg string
		var jsonErr struct {
			Error   string `json:"error"`
			Message string `json:"message"`
			Detail  string `json:"detail"`
		}

		if err := json.Unmarshal(body, &jsonErr); err == nil {
			if jsonErr.Error != "" {
				errorMsg = jsonErr.Error
			} else if jsonErr.Message != "" {
				errorMsg = jsonErr.Message
			} else if jsonErr.Detail != "" {
				errorMsg = jsonErr.Detail
			} else {
				errorMsg = string(body)
			}
		} else {
			errorMsg = string(body)
		}

		return nil, &APIError{
			StatusCode: httpResp.StatusCode,
			Message:    fmt.Sprintf("HTTP %d", httpResp.StatusCode),
			Err:        fmt.Errorf("%s", errorMsg),
		}
	}

	c.history = append(c.history, userMessage)

	return func(yield func(ChatResponse, error) bool) {
		defer httpResp.Body.Close()

		var fullContent strings.Builder

		// Parse streaming JSONL response
		klog.V(1).Info("Processing streaming JSONL response")
		scanner := bufio.NewScanner(httpResp.Body)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			// Parse JSON stream data (JSONL format)
			var streamData nirmataStreamData
			if err := json.Unmarshal([]byte(line), &streamData); err != nil {
				klog.V(3).Infof("Skipping non-JSON line: %q - %v", line, err)
				continue
			}

			klog.V(3).Infof("Received stream data: ID=%s, Type=%s, Data=%q",
				streamData.ID, streamData.Type, streamData.Data)

			switch streamData.Type {
			case "Text":
				if streamData.Data != "" {
					fullContent.WriteString(streamData.Data)
					response := &nirmataStreamResponse{
						content: streamData.Data,
						model:   c.model,
						done:    false,
					}
					if !yield(response, nil) {
						return
					}
				}
			case "Error":
				if streamData.Data != "" {
					yield(nil, fmt.Errorf("stream error: %s", streamData.Data))
					return
				}
			case "ToolStart":
				// Parse tool call from stream data
				klog.V(3).Infof("Processing tool start event: %s", streamData.Data)
				if streamData.Data != "" {
					var toolData struct {
						ToolCall nirmataToolCall `json:"tool_call"`
					}
					if err := json.Unmarshal([]byte(streamData.Data), &toolData); err == nil && toolData.ToolCall.ID != "" {
						// Create a streaming response with tool call
						response := &nirmataStreamResponse{
							content:   "",
							toolCalls: []nirmataToolCall{toolData.ToolCall},
							model:     c.model,
							done:      false,
						}
						if !yield(response, nil) {
							return
						}
						// Add to history
						c.history = append(c.history, nirmataMessage{
							Role:      "assistant",
							ToolCalls: []nirmataToolCall{toolData.ToolCall},
						})
					} else {
						// Make parse errors visible to users (Issue #2 fix)
						klog.Errorf("Failed to parse tool call from stream data: %v (data: %q)", err, streamData.Data)
						// Send error to user so they can see what went wrong
						response := &nirmataStreamResponse{
							content: fmt.Sprintf("[Tool parsing error: %v]", err),
							model:   c.model,
							done:    false,
						}
						yield(response, nil)
					}
				}
			case "ToolComplete":
				// Tool completion event - log for debugging
				klog.V(3).Infof("Tool completed: %s", streamData.Data)
				continue
			case "InputText", "InputChoice":
				klog.V(3).Infof("Skipping input event: %s", streamData.Type)
				continue
			default:
				klog.V(2).Infof("Unknown stream data type: %s", streamData.Type)
			}
		}

		if err := scanner.Err(); err != nil {
			klog.V(2).Infof("Scanner error: %v", err)
		}

		if fullContent.Len() > 0 {
			c.history = append(c.history, nirmataMessage{
				Role:    "assistant",
				Content: fullContent.String(),
			})
		}
	}, nil
}

func (c *nirmataChat) convertContentsToMessage(contents []any) nirmataMessage {
	var contentStr strings.Builder

	// Handle special case of FunctionCallResult
	for _, content := range contents {
		if fcr, ok := content.(FunctionCallResult); ok {
			// Tool result message
			resultJSON, _ := json.Marshal(fcr.Result)
			return nirmataMessage{
				Role:       "tool",
				ToolCallID: fcr.ID,
				Content:    string(resultJSON),
			}
		}
	}

	// Handle regular content
	for i, content := range contents {
		if i > 0 {
			contentStr.WriteString(" ")
		}

		switch v := content.(type) {
		case string:
			contentStr.WriteString(v)
		case *api.Message:
			if v.Type == api.MessageTypeText && v.Payload != nil {
				if textPayload, ok := v.Payload.(string); ok {
					contentStr.WriteString(textPayload)
				} else {
					contentStr.WriteString(fmt.Sprintf("%v", v.Payload))
				}
			}
		default:
			contentStr.WriteString(fmt.Sprintf("%v", v))
		}
	}

	return nirmataMessage{
		Role:    "user",
		Content: contentStr.String(),
	}
}

func (c *NirmataClient) doRequestWithModel(ctx context.Context, endpoint, model string, req any, resp any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	u := c.baseURL.JoinPath(endpoint)
	q := u.Query()
	if model != "" {
		q.Set("model", model)
	}
	q.Set("provider", "bedrock")
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	c.setAuthHeader(httpReq)

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)

		var errorMsg string
		var jsonErr struct {
			Error   string `json:"error"`
			Message string `json:"message"`
			Detail  string `json:"detail"`
		}

		if err := json.Unmarshal(body, &jsonErr); err == nil {
			if jsonErr.Error != "" {
				errorMsg = jsonErr.Error
			} else if jsonErr.Message != "" {
				errorMsg = jsonErr.Message
			} else if jsonErr.Detail != "" {
				errorMsg = jsonErr.Detail
			} else {
				errorMsg = string(body)
			}
		} else {
			errorMsg = string(body)
		}

		return &APIError{
			StatusCode: httpResp.StatusCode,
			Message:    fmt.Sprintf("HTTP %d", httpResp.StatusCode),
			Err:        fmt.Errorf("%s", errorMsg),
		}
	}

	bodyBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}

	return json.Unmarshal(bodyBytes, resp)
}

func (c *nirmataChat) SetFunctionDefinitions(functions []*FunctionDefinition) error {
	c.functionDefs = functions

	// Convert to Nirmata format (tools always supported like other providers)
	c.tools = make([]nirmataToolDef, 0, len(functions))
	for _, fn := range functions {
		// Create the function definition
		functionDef := nirmataToolFunction{
			Name:        fn.Name,
			Description: fn.Description,
		}

		// Convert Schema to map
		if fn.Parameters != nil {
			jsonData, err := json.Marshal(fn.Parameters)
			if err != nil {
				return fmt.Errorf("marshal parameters for %s: %w", fn.Name, err)
			}

			var params map[string]interface{}
			if err := json.Unmarshal(jsonData, &params); err != nil {
				return fmt.Errorf("unmarshal parameters for %s: %w", fn.Name, err)
			}
			functionDef.Parameters = params
		} else {
			// Provide minimal schema if none specified
			functionDef.Parameters = map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			}
		}

		// Wrap in the tool structure with type
		tool := nirmataToolDef{
			Type:     "function",
			Function: functionDef,
		}

		c.tools = append(c.tools, tool)
	}

	klog.V(1).Infof("Set %d function definitions for Nirmata chat", len(c.tools))
	return nil
}

func (c *nirmataChat) IsRetryableError(err error) bool {
	return DefaultIsRetryableError(err)
}

type nirmataResponse struct {
	message   string
	toolCalls []nirmataToolCall
	usage     *nirmataUsage
	metadata  any
	model     string
}

func (r *nirmataResponse) UsageMetadata() any {
	return r.metadata
}

func (r *nirmataResponse) Candidates() []Candidate {
	candidate := &nirmataCandidate{
		text:      r.message,
		toolCalls: r.toolCalls,
		model:     r.model,
	}
	return []Candidate{candidate}
}

// nirmataStreamResponse implements ChatResponse for streaming responses
type nirmataStreamResponse struct {
	content   string
	toolCalls []nirmataToolCall
	usage     *nirmataUsage
	metadata  any
	model     string
	done      bool
}

func (r *nirmataStreamResponse) UsageMetadata() any {
	return nil // No usage metadata in streaming chunks
}

func (r *nirmataStreamResponse) Candidates() []Candidate {
	if r.content == "" && len(r.toolCalls) == 0 && r.usage == nil {
		return []Candidate{}
	}

	candidate := &nirmataStreamCandidate{
		content:   r.content,
		toolCalls: r.toolCalls,
		model:     r.model,
	}
	return []Candidate{candidate}
}

type nirmataCandidate struct {
	text      string
	toolCalls []nirmataToolCall
	model     string
}

func (c *nirmataCandidate) String() string {
	return c.text
}

func (c *nirmataCandidate) Parts() []Part {
	var parts []Part

	if c.text != "" {
		parts = append(parts, &nirmataTextPart{text: c.text})
	}

	for _, toolCall := range c.toolCalls {
		tc := toolCall // Create a copy to avoid pointer issues
		parts = append(parts, &nirmataToolPart{toolCall: &tc})
	}

	return parts
}

type nirmataStreamCandidate struct {
	content   string
	toolCalls []nirmataToolCall
	model     string
}

func (c *nirmataStreamCandidate) String() string {
	return c.content
}

func (c *nirmataStreamCandidate) Parts() []Part {
	var parts []Part

	if c.content != "" {
		parts = append(parts, &nirmataTextPart{text: c.content})
	}

	for _, toolCall := range c.toolCalls {
		tc := toolCall // Create a copy to avoid pointer issues
		parts = append(parts, &nirmataToolPart{toolCall: &tc})
	}

	return parts
}

type nirmataTextPart struct {
	text string
}

func (p *nirmataTextPart) AsText() (string, bool) {
	return p.text, true
}

func (p *nirmataTextPart) AsFunctionCalls() ([]FunctionCall, bool) {
	return nil, false
}

// nirmataToolPart implements Part for tool/function calls - CRITICAL FIX
type nirmataToolPart struct {
	toolCall *nirmataToolCall
}

func (p *nirmataToolPart) AsText() (string, bool) {
	return "", false
}

func (p *nirmataToolPart) AsFunctionCalls() ([]FunctionCall, bool) {
	if p.toolCall == nil {
		return nil, false
	}

	// Parse arguments from JSON string (Issue #5 fix: better error handling)
	var args map[string]any
	if p.toolCall.Function.Arguments != "" {
		// Try to unmarshal as JSON string first
		if err := json.Unmarshal([]byte(p.toolCall.Function.Arguments), &args); err != nil {
			// Make error visible to help debugging
			klog.Errorf("Failed to parse tool arguments for %s: %v (raw: %q)",
				p.toolCall.Function.Name, err, p.toolCall.Function.Arguments)

			// Use empty args but make it clear there was an issue
			args = make(map[string]any)
			args["_parse_error"] = fmt.Sprintf("Failed to parse arguments: %v", err)
		}
	} else {
		args = make(map[string]any)
	}

	funcCall := FunctionCall{
		ID:        p.toolCall.ID,
		Name:      p.toolCall.Function.Name,
		Arguments: args,
	}

	return []FunctionCall{funcCall}, true
}

func getNirmataModel(model string) string {
	if model != "" {
		klog.V(2).Infof("Using explicitly provided model: %s", model)
		return model
	}

	if envModel := os.Getenv("NIRMATA_MODEL"); envModel != "" {
		klog.V(1).Infof("Using model from environment variable: %s", envModel)
		return envModel
	}

	klog.V(1).Infof("Using default model: %s", DEFAULT_NIRMATA_MODEL)
	return DEFAULT_NIRMATA_MODEL
}

type nirmataCompletionResponse struct {
	chatResponse ChatResponse
}

var _ CompletionResponse = (*nirmataCompletionResponse)(nil)

func (r *nirmataCompletionResponse) Response() string {
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

func (r *nirmataCompletionResponse) UsageMetadata() any {
	if r.chatResponse == nil {
		return nil
	}
	return r.chatResponse.UsageMetadata()
}
