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
	baseURL    *url.URL
	httpClient *http.Client
	apiKey     string
}

// Ensure NirmataClient implements the Client interface
var _ Client = &NirmataClient{}

func NewNirmataClient(ctx context.Context, opts ClientOptions) (*NirmataClient, error) {
	apiKey := os.Getenv(NIRMATA_APIKEY_ENV)

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

	return &NirmataClient{
		baseURL:    baseURL,
		httpClient: httpClient,
		apiKey:     apiKey,
	}, nil
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
}

type nirmataMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type nirmataChatRequest struct {
	Messages []nirmataMessage `json:"messages"`
}

type nirmataChatResponse struct {
	Message  string `json:"message"`
	Metadata any    `json:"metadata,omitempty"`
}

type nirmataStreamData struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
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
	req := nirmataChatRequest{Messages: messages}
	var resp nirmataChatResponse
	if err := c.client.doRequestWithModel(ctx, "llm-apps/chat", c.model, req, &resp); err != nil {
		return nil, err
	}

	c.history = append(c.history, userMessage)
	c.history = append(c.history, nirmataMessage{
		Role:    "assistant",
		Content: resp.Message,
	})
	response := &nirmataResponse{
		message:  resp.Message,
		metadata: resp.Metadata,
		model:    c.model,
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
	req := nirmataChatRequest{Messages: messages}
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
	q.Set("provider", "bedrock")
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.client.apiKey != "" {
		// Detect if this is a JWT token (starts with eyJ) or API key
		if strings.HasPrefix(c.client.apiKey, "eyJ") {
			httpReq.Header.Set("Authorization", "NIRMATA-JWT "+c.client.apiKey)
		} else {
			httpReq.Header.Set("Authorization", "NIRMATA-API "+c.client.apiKey)
		}
	}

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
			case "ToolStart", "ToolComplete":
				klog.V(3).Infof("Skipping tool event: %s", streamData.Type)
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
	if c.apiKey != "" {
		// Detect if this is a JWT token (starts with eyJ) or API key
		if strings.HasPrefix(c.apiKey, "eyJ") {
			httpReq.Header.Set("Authorization", "NIRMATA-JWT "+c.apiKey)
		} else {
			httpReq.Header.Set("Authorization", "NIRMATA-API "+c.apiKey)
		}
	}

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

	return json.NewDecoder(httpResp.Body).Decode(resp)
}

func (c *nirmataChat) SetFunctionDefinitions(functions []*FunctionDefinition) error {
	c.functionDefs = functions
	return nil
}

func (c *nirmataChat) IsRetryableError(err error) bool {
	return DefaultIsRetryableError(err)
}

type nirmataResponse struct {
	message  string
	metadata any
	model    string
}

func (r *nirmataResponse) UsageMetadata() any {
	return r.metadata
}

func (r *nirmataResponse) Candidates() []Candidate {
	candidate := &nirmataCandidate{
		text:  r.message,
		model: r.model,
	}
	return []Candidate{candidate}
}

// nirmataStreamResponse implements ChatResponse for streaming responses
type nirmataStreamResponse struct {
	content  string
	metadata any
	model    string
	done     bool
}

func (r *nirmataStreamResponse) UsageMetadata() any {
	return nil // No usage metadata in streaming chunks
}

func (r *nirmataStreamResponse) Candidates() []Candidate {
	if r.content == "" {
		return []Candidate{}
	}

	candidate := &nirmataStreamCandidate{
		content: r.content,
		model:   r.model,
	}
	return []Candidate{candidate}
}

type nirmataCandidate struct {
	text  string
	model string
}

func (c *nirmataCandidate) String() string {
	return c.text
}

func (c *nirmataCandidate) Parts() []Part {
	return []Part{&nirmataTextPart{text: c.text}}
}

type nirmataStreamCandidate struct {
	content string
	model   string
}

func (c *nirmataStreamCandidate) String() string {
	return c.content
}

func (c *nirmataStreamCandidate) Parts() []Part {
	return []Part{&nirmataTextPart{text: c.content}}
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
