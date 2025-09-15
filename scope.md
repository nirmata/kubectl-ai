# Nirmata Provider Tool Calling Implementation Guide

## Executive Summary

The Nirmata provider in the kubectl-ai/gollm library currently does not support native tool/function calling, while the Bedrock provider does. This causes the PAC (Policy as Code) agent to behave differently - Bedrock executes tools properly while Nirmata falls back to prompt engineering, leading to inconsistent behavior and degraded functionality.

**KEY CONSTRAINT**: The go-llm-apps backend CANNOT execute tools directly - it can only identify when tools should be called and return that information to the client for execution.

## Implementation Status

✅ **Client-side implementation complete** (kubectl-ai/gollm) - All tool calling features implemented
⏳ **Backend API implementation pending** (go-llm-apps) - Must return tool calls, not execute them
⏳ **Integration pending** (go-nctl)
🔄 **Architecture decision pending**: Keep provider in kubectl-ai or move to go-llm-apps

## Design Requirements

### Provider Transparency 🔑
The Nirmata provider MUST behave identically to Bedrock from the client's perspective:
- Same API response format
- Same tool calling behavior (required, like other providers)
- Same streaming format  
- Zero code changes when switching providers
- Test suite should pass with both providers
- Tool calling is a core requirement, not optional

This enables customers to seamlessly switch between Nirmata's managed service and their own Bedrock credentials.

## Problem Analysis

### Current Flow Comparison

#### Bedrock Provider (Working)
1. **Agent Setup** → Loads function definitions into Chat
2. **Chat Send** → Includes `toolConfig` in API request
3. **API Response** → Returns structured tool use blocks
4. **Response Parsing** → `bedrockToolPart.AsFunctionCalls()` extracts function calls
5. **Tool Execution** → Agent executes tools via `executeFunctionCalls()`
6. **Result Handling** → Tool results sent back to LLM as `FunctionCallResult`
7. **Continuation** → LLM incorporates results and continues

#### Nirmata Provider (Client Ready, Backend Issue)
1. **Agent Setup** → Loads function definitions into Chat ✅
2. **Chat Send** → DOES include tools in API request (lines 286-290) ✅
3. **API Response** → Backend returns only text content (no tool_calls) ⚠️
4. **Response Parsing** → `AsFunctionCalls()` ready to parse (lines 750-773) ✅
5. **No Tool Execution** → No tool calls from backend to execute ⚠️
6. **Fallback** → System prompt instructs LLM to generate policy directly
7. **Result** → Different behavior due to backend limitation

### Root Cause Analysis

Based on code examination:

1. **Client Implementation Complete** (`nirmata.go`)
   - `SetFunctionDefinitions` properly stores tools (lines 611-653) ✅
   - Tools are sent in requests (always enabled like other providers) (lines 287-290) ✅
   - `AsFunctionCalls()` correctly parses tool calls (lines 750-773) ✅
   - FunctionCallResult handling implemented (lines 500-515) ✅
   - Streaming tool events now handled (lines 448-474) ✅
   - Provider transparency maintained (no special env vars required) ✅

2. **Backend Issue** (`go-llm-apps`)
   - Backend doesn't parse tool_calls from Bedrock responses ⚠️
   - Needs to return tool_calls field in chat responses ⚠️
   - Must NOT execute tools (security requirement) ✅

3. **Design Decision** ✅
   - Tool calling is required (matches other providers exactly)
   - No fallback to non-tool mode - tools must work
   - Complete provider transparency achieved

3. **Agent Flow Impact** (`conversation.go:174-192`)
   - Agent checks `part.AsFunctionCalls()` for each response part
   - When this returns `nil, false`, agent assumes no tools to execute
   - Falls through to "task completed" logic

## Architecture Overview

### System Components & Tool Execution Boundary

```
┌─────────────────────────────────────────────────────────────────────┐
│                            CLIENT SIDE                              │
│  ┌──────────┐     ┌──────────────────┐     ┌──────────────────┐   │
│  │ go-nctl  │────▶│   kubectl-ai     │────▶│  Tool Executor   │   │
│  │   (CLI)  │     │     (gollm)       │     │   (Local Only)   │   │
│  └──────────┘     └──────────────────┘     └──────────────────┘   │
│                            │ ▲                                      │
│                            │ │ Tool Results                        │
│                            │ │                                      │
│                            ▼ │                                      │
└────────────────────────────│─│──────────────────────────────────────┘
                             │ │ Tool Call Requests
┌────────────────────────────│─│──────────────────────────────────────┐
│                            ▼ │                                      │
│                     ┌──────────────────┐                           │
│                     │   go-llm-apps    │                           │
│                     │  (Backend API)   │                           │
│                     │                  │                           │
│                     │ - Receives tools │                           │
│                     │ - Returns calls  │                           │
│                     │ - Never executes │                           │
│                     └──────────────────┘                           │
│                            │                                        │
│                            ▼                                        │
│                     ┌──────────────────┐                           │
│                     │     Bedrock      │                           │
│                     │    (LLM API)     │                           │
│                     └──────────────────┘                           │
│                         BACKEND SIDE                               │
└─────────────────────────────────────────────────────────────────────┘
```

### Implementation Strategy

1. **Phase 1 (COMPLETED)**: Client-side tool support in kubectl-ai
   - The Nirmata provider now sends tools in requests
   - Parses tool calls from responses
   - Returns function calls via `AsFunctionCalls()`
   - Handles `FunctionCallResult` for tool responses

2. **Phase 2 (PENDING)**: Backend API enhancement in go-llm-apps
   - Add translation layer between Nirmata API format and Bedrock
   - Handle tool definitions in requests
   - Return tool calls in responses
   - Maintain backward compatibility

3. **Phase 3 (PENDING)**: Integration in go-nctl
   - Update to use enhanced Nirmata provider
   - Add fallback prompts for non-tool scenarios
   - Test end-to-end flows

## Architectural Principles

### Critical Architecture Constraint: Tool Execution Boundary 🚨

The go-llm-apps backend is a **stateless, multi-tenant service** that:
- ❌ **CANNOT** execute tools (security/isolation requirement)
- ✅ **CAN** identify when tools should be called
- ✅ **CAN** return tool call requests to the client
- ✅ **CAN** process tool results from the client

The client (kubectl-ai/gollm or go-nctl) is responsible for:
- ✅ **Executing** tools locally
- ✅ **Sending** tool results back to the backend
- ✅ **Maintaining** conversation state

### Stateless Request Design 🔑
Every request to the `/chat` endpoint must be completely self-contained:
- Full conversation history included in each request
- No server-side session state
- Tool call IDs tracked client-side across requests
- Enables horizontal scaling and multi-tenant isolation

This means each request contains:
```json
{
  "messages": [
    {"role": "system", "content": "..."},
    {"role": "user", "content": "..."},
    {"role": "assistant", "content": "...", "tool_calls": [...]},
    {"role": "tool", "tool_call_id": "call_123", "content": "..."},
    {"role": "user", "content": "current question"}
  ],
  "tools": [...]
}
```

The backend must be able to handle any request at any point without prior context.

## Critical Gaps That Need Backend Implementation

### 1. **No Tool Call Reception from Backend** ⚠️ CRITICAL

**Problem**: The backend currently never returns tool calls in responses. The `tool_calls` field is always empty even when Bedrock returns tool use requests.

**What's Missing**: The backend needs to parse Bedrock responses and extract `toolUse` blocks, then convert them to Nirmata's `tool_calls` format.

### 2. **Streaming with HTTP Chunked Transfer Encoding** 🔑

**Problem**: Streaming responses only include text content. Tool calls in streaming are completely ignored.

**Solution**: Use HTTP chunked transfer encoding for real-time streaming:
- Set `Transfer-Encoding: chunked` header
- Stream JSONL formatted events
- Flush after each chunk for immediate delivery
- Include tool events in the stream:

```http
HTTP/1.1 200 OK
Transfer-Encoding: chunked
Content-Type: application/x-ndjson

{"type": "content", "delta": "I'll help you "}
{"type": "content", "delta": "create a policy."}
{"type": "tool_call", "data": {"id": "call_123", "function": {...}}}
```

**What's Missing**: The backend needs to handle Bedrock's streaming tool events and forward them properly.

### 3. **No Tool Discovery/Capability Negotiation**

**Problem**: The client has no way to know if the backend actually supports tools. It just optimistically sends them.

**What's Missing**: A capabilities endpoint that advertises what features the backend supports.

### 4. **Tool Results Not Properly Forwarded**

**Problem**: When the client sends tool results back (with `role: "tool"`), the backend doesn't properly convert them to Bedrock's expected format.

**What's Missing**: Proper translation of tool result messages to Bedrock's `toolResult` format.

## Implementation Requirements for go-llm-apps Backend

### Implementation Approach Options

#### Option 1: Direct Response Pattern (RECOMMENDED) ✅

The backend directly parses Bedrock's response and returns tool calls without using the agent framework. This is the simplest and most aligned with the constraint that the backend cannot execute tools.

#### Option 2: Proxy Tool Pattern

If we must use the agent framework, create "proxy tools" that don't execute anything - they just return the call info for the client to execute.

#### Option 3: Rewrite Conversation Loop (Most Complex)

Create a new conversation handler in kubectl-ai that manages the full flow, with the backend acting as a pure pass-through.

### 1. Enhanced Chat App Implementation (Direct Response Pattern)

#### 1.1 Update Chat Types (pkg/apps/chat/types.go)
```go
package chat

import (
	"github.com/nirmata/go-llm-apps/pkg/apps"
)

type ChatRequest struct {
	Messages   []ChatMessage `json:"messages" yaml:"messages"`
	Tools      []Tool        `json:"tools,omitempty" yaml:"tools,omitempty"`
	ToolChoice interface{}   `json:"tool_choice,omitempty" yaml:"tool_choice,omitempty"`
	Model      string        `json:"model,omitempty" yaml:"model,omitempty"`
	Stream     bool          `json:"stream,omitempty" yaml:"stream,omitempty"`
}

type ChatMessage struct {
	Role       string     `json:"role" yaml:"role"`
	Content    string     `json:"content" yaml:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty" yaml:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty" yaml:"tool_call_id,omitempty"`
}

type Tool struct {
	Type     string       `json:"type" yaml:"type"`
	Function ToolFunction `json:"function" yaml:"function"`
}

type ToolFunction struct {
	Name        string                 `json:"name" yaml:"name"`
	Description string                 `json:"description" yaml:"description"`
	Parameters  map[string]interface{} `json:"parameters" yaml:"parameters"`
}

type ToolCall struct {
	ID       string       `json:"id" yaml:"id"`
	Type     string       `json:"type" yaml:"type"`
	Function FunctionCall `json:"function" yaml:"function"`
}

type FunctionCall struct {
	Name      string `json:"name" yaml:"name"`
	Arguments string `json:"arguments" yaml:"arguments"` // JSON string
}

type Response struct {
	Message   string                `json:"message" yaml:"message"`
	ToolCalls []ToolCall            `json:"tool_calls,omitempty" yaml:"tool_calls,omitempty"`
	Usage     *UsageInfo            `json:"usage,omitempty" yaml:"usage,omitempty"`
	Metadata  apps.ResponseMetadata `json:"metadata" yaml:"metadata"`
}

type UsageInfo struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
```

#### 1.2 Enhanced Chat Processor (pkg/apps/chat/chat.go)
```go
package chat

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/template"

	"github.com/GoogleCloudPlatform/kubectl-ai/gollm"
	"github.com/google/uuid"
	"github.com/nirmata/go-llm-apps/pkg/apps"
	"github.com/nirmata/go-llm-apps/pkg/datastore"
	"github.com/nirmata/go-llm-apps/pkg/datastore/conversation"
	"github.com/nirmata/go-llm-apps/pkg/templates"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
)

//go:embed templates
var templatesFS embed.FS

type chat struct {
	templatesDir *template.Template
	appRunner    *apps.AppRunner
	debug        bool
}

type chatProcessor struct {
	debug bool
}

func (p *chatProcessor) GetAppName() string {
	return "chat"
}

func (p *chatProcessor) SystemPromptFile() string {
	return "system.md"
}

func (p *chatProcessor) UserPromptFile() string {
	return "user.md"
}

type PreparedChatRequest struct {
	ChatRequest
	Prompt            string
	FunctionDefs      []*gollm.FunctionDefinition
	HasToolCapability bool
}

func (p *chatProcessor) PrepareRequest(ctx context.Context, request any) (any, error) {
	chatReq, ok := request.(ChatRequest)
	if !ok {
		return nil, errors.New("invalid request type")
	}

	if len(chatReq.Messages) == 0 {
		return nil, errors.New("messages array cannot be empty")
	}

	// Convert messages to prompt (for template processing)
	prompt := convertMessagesToPrompt(chatReq.Messages)
	klog.V(2).Infof("Converted %d messages to prompt: %s", len(chatReq.Messages), prompt)

	// Convert tools to gollm function definitions if present
	var functionDefs []*gollm.FunctionDefinition
	if len(chatReq.Tools) > 0 {
		functionDefs = convertToolsToFunctionDefinitions(chatReq.Tools)
		klog.V(2).Infof("Converted %d tools to function definitions", len(functionDefs))
	}

	return PreparedChatRequest{
		ChatRequest:       chatReq,
		Prompt:            prompt,
		FunctionDefs:      functionDefs,
		HasToolCapability: len(functionDefs) > 0,
	}, nil
}

// Rest of processor methods remain the same...

func convertMessagesToPrompt(messages []ChatMessage) string {
	var prompt strings.Builder
	for _, msg := range messages {
		// Handle different message types
		switch msg.Role {
		case "user":
			prompt.WriteString(fmt.Sprintf("User: %s\n", msg.Content))
		case "assistant":
			prompt.WriteString(fmt.Sprintf("Assistant: %s\n", msg.Content))
			// Include tool calls if present
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					prompt.WriteString(fmt.Sprintf("Tool Call: %s(%s)\n", tc.Function.Name, tc.Function.Arguments))
				}
			}
		case "tool":
			prompt.WriteString(fmt.Sprintf("Tool Result [%s]: %s\n", msg.ToolCallID, msg.Content))
		case "system":
			prompt.WriteString(fmt.Sprintf("System: %s\n", msg.Content))
		}
	}
	return strings.TrimSpace(prompt.String())
}

func convertToolsToFunctionDefinitions(tools []Tool) []*gollm.FunctionDefinition {
	functionDefs := make([]*gollm.FunctionDefinition, len(tools))
	for i, tool := range tools {
		functionDefs[i] = &gollm.FunctionDefinition{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  tool.Function.Parameters,
		}
	}
	return functionDefs
}
```

#### 1.3 Direct Implementation Without Agent Framework

```go
// In pkg/apps/chat/chat.go - enhanced Run method (Direct Response Pattern)
func (c *chat) Run(ctx context.Context, reader apps.RequestReader, llmClient gollm.Client, model string, opts *apps.ApplicationOpts) (any, error) {
	chatReq := reader.(*ChatRequest)
	
	// If tools are present, handle them directly without agent framework
	if len(chatReq.Tools) > 0 {
		// Set up the chat with tools
		chat := llmClient.StartChat("", model)
		
		// Convert tools to function definitions
		funcDefs := convertToFunctionDefs(chatReq.Tools)
		chat.SetFunctionDefinitions(funcDefs)
		
		// Send messages - Bedrock will return tool calls
		response, err := chat.Send(ctx, buildMessages(chatReq.Messages))
		if err != nil {
			return nil, err
		}
		
		// Extract tool calls from Bedrock response WITHOUT executing them
		toolCalls := []ToolCall{}
		for _, part := range response.Parts() {
			if calls, ok := part.AsFunctionCalls(); ok {
				for _, call := range calls {
					toolCalls = append(toolCalls, ToolCall{
						ID:   call.ID,
						Type: "function",
						Function: FunctionCall{
							Name:      call.Name,
							Arguments: call.Arguments,
						},
					})
				}
			}
		}
		
		// Return response with tool calls (NOT executed)
		return &Response{
			Message:   response.Text(),
			ToolCalls: toolCalls,
		}, nil
	}
	
	// Non-tool path remains the same
	return c.appRunner.Run(ctx, reader, llmClient, model, opts)
}

// Alternative: Proxy Tool Pattern (if agent framework is required)
func (c *chat) runWithProxyTools(ctx context.Context, preparedRequest *PreparedChatRequest, llmClient gollm.Client, model string, opts *apps.ApplicationOpts) (any, error) {
	// Create agent with tool support
	agentInstance, err := agent.NewAgent("You are a helpful assistant that can use tools to help users.", llmClient, model)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create agent")
	}

	// Register PROXY tools that don't execute anything
	for _, funcDef := range preparedRequest.FunctionDefs {
		tool := &agent.InMemoryTool{
			ToolName:        funcDef.Name,
			ToolDescription: funcDef.Description,
			Handler: func(ctx context.Context, arguments string) (string, error) {
				// DON'T EXECUTE - just return the call info for the client
				return fmt.Sprintf(`{"action": "call_tool", "name": "%s", "args": %s}`, 
					funcDef.Name, arguments), nil
			},
		}
		agentInstance.RegisterTool(tool)
	}

	// Convert chat messages to agent conversation
	conversation := agentInstance.NewConversation(ctx)

	// Process each message in the chat history
	for _, msg := range preparedRequest.Messages {
		switch msg.Role {
		case "user":
			response, err := conversation.Send(ctx, msg.Content)
			if err != nil {
				return nil, err
			}
			// Convert agent response to chat response format
			return convertAgentResponseToChatResponse(response), nil
		case "tool":
			// Handle tool results by continuing conversation
			err := conversation.SubmitToolResult(ctx, msg.ToolCallID, msg.Content)
			if err != nil {
				return nil, err
			}
		}
	}

	return nil, errors.New("no user message found to process")
}

func convertAgentResponseToChatResponse(agentResponse string) *Response {
	// Parse agent response for tool calls
	// This would need to be implemented based on agent response format
	// For now, return simple text response
	return &Response{
		Message: agentResponse,
	}
}
```

#### 1.4 Tool Call Response Processing

The chat processor should handle tool call responses properly:

```go
func (p *chatProcessor) ValidateResponse(ctx context.Context, rawResponse string, request any) (any, error) {
	preparedReq, ok := request.(PreparedChatRequest)
	if !ok {
		return nil, errors.New("invalid request type")
	}

	// If no tool capability, return simple response
	if !preparedReq.HasToolCapability {
		return &Response{
			Message: rawResponse,
		}, nil
	}

	// Parse response for tool calls using existing agent patterns
	// This would integrate with the gollm response parsing
	response := &Response{
		Message: rawResponse,
	}

	// TODO: Parse tool calls from rawResponse
	// This should use the same logic as the agent framework
	// to extract function calls from LLM responses

	return response, nil
}

func (p *chatProcessor) PrepareResponse(ctx context.Context, request any, response any, conversationId uuid.UUID, metadata apps.ResponseMetadata) (any, error) {
	typedResponse, ok := response.(*Response)
	if !ok {
		return nil, errors.New("invalid response type")
	}

	// Inject metadata
	typedResponse.Metadata = metadata

	// Convert usage metadata if present
	if metadata.Usage.TotalTokens > 0 {
		typedResponse.Usage = &UsageInfo{
			PromptTokens:     metadata.Usage.InputTokens,
			CompletionTokens: metadata.Usage.OutputTokens,
			TotalTokens:      metadata.Usage.TotalTokens,
		}
	}

	return typedResponse, nil
}
```

#### 1.5 Streaming Support Enhancement

The streaming support should integrate with existing agent streaming patterns:

```go
// In handlers.go - modify the streaming logic to handle tool calls
func (h *handler) Handle(w http.ResponseWriter, req *http.Request) {
	// ... existing setup code ...

	var listener agent.StreamListener
	isChunked := isChunkedRequest(req)
	if isChunked {
		listener = func(ctx context.Context, contentType agent.StreamDataType, id string, message string) error {
			// Convert agent stream events to chat API format
			var jsonBytes []byte
			var err error
			
			switch contentType {
			case agent.StreamDataTypeToolStart:
				// Parse tool call from message and convert to chat format
				jsonBytes, err = json.Marshal(map[string]interface{}{
					"type": "tool_call",
					"data": message, // This should contain the tool call data
				})
			case agent.StreamDataTypeText:
				jsonBytes, err = json.Marshal(map[string]interface{}{
					"type": "content",
					"delta": message,
				})
			default:
				jsonBytes, err = convertToJSON(contentType, id, message)
			}
			
			if err != nil {
				return err
			}
			
			if _, err := w.Write(jsonBytes); err != nil {
				return err
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			return nil
		}
	}

	// ... rest of handler logic ...
}
```

#### 1.6 Remove Bedrock-Only Restriction

Update handlers.go to allow other providers for the chat endpoint:

```go
// In cmd/webserver/handlers.go - remove or modify this restriction:
// REMOVE THESE LINES:
// if strings.Contains(req.URL.Path, "/chat") && providerName != "bedrock" {
//     klog.Errorf("Chat endpoint only supports bedrock provider, got: %s", providerName)
//     httpError(nil, w, "chat endpoint only supports bedrock provider", http.StatusBadRequest)
//     return
// }

// REPLACE WITH:
if strings.Contains(req.URL.Path, "/chat") {
    klog.V(2).Infof("Chat endpoint processing request with provider: %s", providerName)
    // Chat endpoint now supports multiple providers
}
```

### 2. Architecture Benefits of This Approach

✅ **Follows Existing Patterns**: Uses the same AppProcessor interface as copilot
✅ **Reuses Agent Framework**: Leverages existing tool execution patterns
✅ **Maintains Compatibility**: Doesn't break existing chat functionality
✅ **Clean Separation**: Tool-capable vs. simple chat requests handled differently
✅ **Extensible**: Easy to add more tool capabilities later

### 3. Add Capabilities Endpoint (RECOMMENDED)

```http
GET /llm-apps/capabilities
```

**Response:**
```json
{
  "tools_supported": true,
  "tool_types": ["function"],
  "streaming_tools": true,
  "max_tools": 128,
  "providers": ["bedrock"],
  "models": [
    "us.anthropic.claude-sonnet-4-20250514-v1:0",
    "us.anthropic.claude-3-7-sonnet-20250219-v1:0"
  ]
}
```

This allows clients to detect if tools are supported before attempting to use them.

### 3. Client Implementation - kubectl-ai/gollm (ALREADY COMPLETED)

The client-side implementation in kubectl-ai is **already complete** and ready. It:

✅ **Sends tools** in requests when `SetFunctionDefinitions()` is called  
✅ **Includes tool results** with `role: "tool"` and `tool_call_id`  
✅ **Parses tool calls** from responses via `AsFunctionCalls()`  
✅ **Handles streaming** (ready to process tool events if backend sends them)

**What the client expects from the backend

**1. The client sends tools in this format:**
```json
{
  "tools": [
    {
      "name": "save_policy",
      "description": "Save a policy to a file",
      "parameters": {
        "type": "object",
        "properties": {...},
        "required": [...]
      }
    }
  ],
  "tool_choice": "auto"
}
```

**2. The client expects tool calls back in this format:**
```json
{
  "tool_calls": [
    {
      "id": "call_abc123",
      "type": "function",
      "function": {
        "name": "save_policy",
        "arguments": "{\"filename\": \"policy.yaml\", \"content\": \"...\"}"
      }
    }
  ]
}
```

**3. The client sends tool results like this:**
```json
{
  "role": "tool",
  "tool_call_id": "call_abc123",
  "content": "{\"status\": \"success\", \"message\": \"Policy saved\"}"
}
```

**4. For streaming, the client expects JSONL with tool events:**
```jsonl
{"type": "tool_call", "data": {"id": "call_xyz", "type": "function", "function": {"name": "save_policy", "arguments": "{...}"}}}
```

### 4. Testing the Implementation

#### Quick Test to Verify Backend Support

```bash
# Test 1: Send a request with tools and verify response includes tool calls
curl -X POST $NIRMATA_ENDPOINT/llm-apps/chat \
  -H "Authorization: NIRMATA-API $KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [{"role": "user", "content": "Save a policy that requires pod labels"}],
    "tools": [{
      "name": "save_policy",
      "description": "Save a Kyverno policy to a file",
      "parameters": {
        "type": "object",
        "properties": {
          "filename": {"type": "string", "description": "Name of the file"},
          "content": {"type": "string", "description": "Policy YAML content"}
        },
        "required": ["filename", "content"]
      }
    }],
    "model": "us.anthropic.claude-sonnet-4-20250514-v1:0"
  }'

# Expected response should include tool_calls:
{
  "message": "I'll save a policy that requires pod labels.",
  "tool_calls": [{
    "id": "call_abc123",
    "type": "function",
    "function": {
      "name": "save_policy",
      "arguments": "{\"filename\":\"require-pod-labels.yaml\",\"content\":\"apiVersion: kyverno.io/v1...\"}"
    }
  }]
}
```

#### Test 2: Verify Tool Result Handling

```bash
# Send back tool result
curl -X POST $NIRMATA_ENDPOINT/llm-apps/chat \
  -H "Authorization: NIRMATA-API $KEY" \
  -d '{
    "messages": [
      {"role": "user", "content": "Save a policy"},
      {"role": "assistant", "content": "I'll save the policy.", "tool_calls": [{...}]},
      {"role": "tool", "tool_call_id": "call_abc123", "content": "{\"status\":\"success\"}"}
    ]
  }'
```

### 5. go-nctl Repository Integration

#### 3.1 File: `cmd/ai/providersetup.go`

```go
package ai

import (
    "fmt"
    "os"
    
    "github.com/nirmata/go-nctl/pkg/nch/login"
    "k8s.io/klog/v2"
)

func setNirmataProviderEnvs() error {
    // Get credentials from NCH
    token, err := login.GetToken()
    if err != nil {
        return fmt.Errorf("failed to get NCH token: %w", err)
    }
    
    endpoint, err := login.GetEndpoint()
    if err != nil {
        return fmt.Errorf("failed to get NCH endpoint: %w", err)
    }
    
    // Set environment variables
    os.Setenv("NIRMATA_APIKEY", token)
    os.Setenv("NIRMATA_ENDPOINT", endpoint)
    
    // Tool calling is required for Nirmata provider
    // No fallback mode - tools must work
    if !checkNirmataToolSupport() {
        return fmt.Errorf("Nirmata backend does not support required tool calling functionality. " +
            "Please ensure your Nirmata backend is updated to support tools.")
    }
    
    return nil
}

func checkNirmataToolSupport() bool {
    // Tool calling is required - verify backend supports it
    // Could check via:
    // - API version endpoint
    // - Capabilities endpoint  
    // - Test request with tools
    
    // For now, assume backend supports tools (required)
    return true
}
```

#### 3.2 File: `cmd/ai/agent.go` (Modifications)

```go
func (c *command) runLLM(ctx context.Context, stdout io.Writer, stderr io.Writer) error {
    // ... existing code ...
    
    // Tool calling is required for all providers
    if c.provider == "nirmata" {
        // Verify tool support is available
        if !hasToolSupport() {
            return fmt.Errorf("Nirmata provider requires tool calling support, but backend does not support it")
        }
        klog.V(1).Info("Using Nirmata provider with required tool support")
    }
    
    // Use standard tool-enabled prompt for all providers
    systemPromptTemplate := systemPromptTemplateWithTools
    
    // ... rest of implementation
}
```

## Summary for go-llm-apps Team

### What Needs to Be Done

1. **HIGH PRIORITY**: Enhanced Chat App Implementation
   - Update `pkg/apps/chat/types.go` with tool-capable message structures
   - Enhance `pkg/apps/chat/chat.go` with agent integration for tool requests
   - Follow existing copilot patterns for tool handling

2. **HIGH PRIORITY**: Handler Modifications
   - Remove Bedrock-only restriction from `cmd/webserver/handlers.go`
   - Enhance streaming support to handle tool call events
   - Maintain backward compatibility with existing chat API

3. **MEDIUM PRIORITY**: Agent Framework Integration
   - Ensure chat app properly integrates with existing agent framework
   - Use in-memory tools that return function calls (not execute them)
   - Leverage existing tool registration patterns

4. **RECOMMENDED**: Add capabilities endpoint
   - Let clients know you support tools before they try to use them
   - Simple GET endpoint returning feature flags

### Expected Timeline

- **Week 1**: Get basic tool calls working using Direct Response Pattern
- **Timeline reduced from 3 weeks to 1 week** due to simplified approach
- **~100 lines of code changes** vs original ~500 lines

The kubectl-ai client is ready and waiting. Once the backend starts returning tool calls, everything will work end-to-end.

## Alternative Architecture: Move Provider to go-llm-apps

Instead of having the provider in kubectl-ai, consider moving it to go-llm-apps:

**Benefits**:
- Centralized provider management
- Easier to maintain consistency
- Better for multi-tenant scenarios
- Single source of truth for provider logic

**Implementation**:
1. Move `gollm/nirmata.go` to `go-llm-apps/pkg/providers/nirmata`
2. kubectl-ai would just reference the provider
3. All provider logic lives with the backend service

**Trade-offs**:
- Requires refactoring current architecture
- May impact other consumers of gollm
- Needs careful dependency management

## Custom conversation.go for Stateless Flow

The custom conversation.go isn't just for testing - it's to ensure proper stateless behavior:

```go
// conversation.go - Modified for stateless operation
package agent

import (
    "context"
    "github.com/GoogleCloudPlatform/kubectl-ai/gollm"
)

type StatelessConversation struct {
    chat     gollm.Chat
    messages []ChatMessage  // Full history maintained client-side
}

func (sc *StatelessConversation) Send(ctx context.Context, message string) (*gollm.ChatResponse, error) {
    // Always send complete conversation history
    sc.messages = append(sc.messages, ChatMessage{Role: "user", Content: message})
    
    // Send full history to backend
    response, err := sc.chat.Send(ctx, sc.messages)
    if err != nil {
        return nil, err
    }
    
    // Process tool calls if present
    if toolCalls := extractToolCalls(response); len(toolCalls) > 0 {
        // Add assistant message with tool calls to history
        sc.messages = append(sc.messages, ChatMessage{
            Role: "assistant",
            Content: response.Text(),
            ToolCalls: toolCalls,
        })
        
        // Tool execution happens client-side
        for _, toolCall := range toolCalls {
            result := executeToolLocally(toolCall)
            // Add tool result to history
            sc.messages = append(sc.messages, ChatMessage{
                Role: "tool",
                ToolCallID: toolCall.ID,
                Content: result,
            })
        }
        
        // Send updated history back for continuation
        return sc.chat.Send(ctx, sc.messages)
    }
    
    // Add assistant response to history
    sc.messages = append(sc.messages, ChatMessage{
        Role: "assistant",
        Content: response.Text(),
    })
    
    return response, nil
}
```

This ensures:
- Every request includes full conversation context
- Tool call IDs are preserved across requests
- Backend remains completely stateless
- Client maintains all conversation state

## Testing Scripts

### End-to-End Test Script

```bash
#!/bin/bash
# test-tool-calling.sh

echo "Testing Nirmata Tool Calling Support"

# Test 1: Check if tools are registered
echo "Test 1: Tool Registration"
./nctl ai --provider nirmata --prompt "list available tools" --max-tool-calls 0

# Test 2: Generate policy with tool
echo "Test 2: Policy Generation via Tool"
./nctl ai --provider nirmata --prompt "generate a policy that requires pod labels"

# Test 3: Multiple tool calls
echo "Test 3: Multiple Tool Calls"
./nctl ai --provider nirmata --prompt "generate a policy and create tests for it"

# Test 4: Tool error handling
echo "Test 4: Error Handling"
./nctl ai --provider nirmata --prompt "call non-existent tool"

# Compare with Bedrock
echo "Comparison: Bedrock Provider"
./nctl ai --provider bedrock --prompt "generate a policy that requires pod labels"
```

### How to Verify It's Working

1. **Check Bedrock Logs**: Verify Bedrock is actually returning `toolUse` blocks
2. **Check Translation**: Ensure `toolUse` → `tool_calls` conversion is happening
3. **Check Response**: Confirm `tool_calls` array is populated in HTTP response
4. **Check Streaming**: Verify tool events appear in JSONL stream

## Success Criteria

You'll know the implementation is working when:

1. ✅ Sending a request with tools returns `tool_calls` in the response
2. ✅ Tool results are properly forwarded to Bedrock and get responses
3. ✅ Streaming includes tool call events, not just text
4. ✅ The PAC agent in go-nctl successfully executes tools instead of falling back to prompts

## Engineering Considerations

### Design Trade-offs

1. **Optimistic vs Pessimistic Tool Support**
   - **Chosen**: Optimistic (assume support, handle failures)
   - **Rationale**: Allows faster adoption as backends add support
   - **Trade-off**: May send unnecessary data to non-supporting backends

2. **Streaming Format: HTTP Chunked Transfer with JSONL**
   - **Chosen**: HTTP chunked transfer encoding with JSONL payloads
   - **Rationale**: Real-time streaming with proper HTTP semantics
   - **Benefits**: 
     - Immediate delivery via flush after each chunk
     - Works with standard HTTP infrastructure
     - Simple line-based parsing for JSONL content
   - **Implementation**: Set `Transfer-Encoding: chunked` header and flush after each line
   - **Trade-off**: Requires proper handling of chunked encoding

3. **Endpoint Strategy**
   - **Chosen**: Single endpoint with query parameters
   - **Originally Proposed**: Separate `/chat-stream` endpoint
   - **Rationale**: Simpler implementation, fewer breaking changes
   - **Trade-off**: Less RESTful but more pragmatic

4. **Tool Support Detection**
   - **Chosen**: Environment variable with optimistic default
   - **Originally Proposed**: API probing with test requests
   - **Rationale**: Avoids extra API calls, simpler configuration
   - **Trade-off**: Requires manual configuration in some cases

### Code Quality Improvements

1. **Removed Debug Output**: Cleaned up debug `fmt.Printf` statements that were cluttering production code
2. **Consistent Error Handling**: Aligned with Bedrock provider patterns
3. **Proper Type Safety**: All tool-related types properly typed and validated
4. **Memory Efficiency**: Uses `json.NewDecoder` where appropriate

## Risks and Mitigations

### Risk 1: Backend Incompatibility
**Mitigation**: Graceful degradation when backend doesn't support tools
**Implementation**: `supportsTools` flag with environment variable override

### Risk 2: Breaking Existing Workflows
**Mitigation**: Backward compatible - tools only sent when available
**Implementation**: Conditional tool inclusion in requests

### Risk 3: Performance Degradation
**Mitigation**: No additional API calls for detection
**Implementation**: Single-pass request/response flow maintained

### Risk 4: Complex Tool Interactions
**Mitigation**: Follows exact same pattern as Bedrock provider
**Implementation**: Reused existing `FunctionCall` and `FunctionCallResult` types

## Contact Information

For questions about:
- **Client implementation** (kubectl-ai/gollm): Already complete, see code in this repo
- **Backend requirements** (go-llm-apps): This document outlines what's needed
- **Integration testing** (go-nctl): Will work automatically once backend is ready

## Key Takeaway

**The client (kubectl-ai) is ready.** The backend (go-llm-apps) just needs to:
1. Accept tool definitions in requests
2. Return tool calls (WITHOUT executing them) 
3. Accept tool results from client
4. Continue conversation after tool results
5. Remove provider restrictions that block Nirmata

**Critical Insight**: The backend should act as a **stateless proxy for tool calls, not an executor**. This dramatically simplifies the implementation:
- Backend identifies when tools should be called (via Bedrock)
- Backend returns tool call requests to the client
- Client executes tools locally
- Client sends results back
- Backend continues the conversation

This maintains security boundaries, keeps the backend stateless, and aligns with the fundamental constraint that tool execution must happen client-side.