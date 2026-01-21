# Nirmata Fork Changes

This document details the differences between this Nirmata-maintained fork and the upstream [GoogleCloudPlatform/kubectl-ai](https://github.com/GoogleCloudPlatform/kubectl-ai) repository.

## Overview

This fork extends kubectl-ai with additional features and security enhancements required for the Nirmata AI agent (nctl ai).

## Changes

### Security Enhancements

#### Bash Tool Directory Access Control

**Issue**: The bash tool executed shell commands without validating file paths, allowing writes outside allowed directories via shell redirection operators (`>`, `>>`, `2>`, `| tee`, etc.).

**Root Cause**: 
- The filesystem MCP server enforces allowed directories for filesystem tools (read_file, write_file)
- The bash tool executed commands directly without path validation
- Shell redirection operators bypassed the filesystem tool sandbox entirely

**Impact**: 
- Prevents shell command redirection from bypassing sandbox restrictions
- Ensures consistent security enforcement across all file operations
- Maintains backward compatibility (no restrictions when no allowed directories specified)

### Usage Enhancements

#### Message Token Count

**Issue**: It was difficult to associate an exact token count with each message in the tracked conversation history, so adding a `TokenCount` field to `api.Message` helped make this bookkeeping simpler.

### Provider Enhancements

#### Anthropic Provider Support

**Added**: Support for the Anthropic Claude API as a new LLM provider in `gollm`.

**Features**:
- Direct integration with Anthropic's Messages API (v1)
- Configurable base URL for proxy/gateway scenarios
- Support for streaming responses with Server-Sent Events (SSE)
- Full support for tool/function calling with fine-grained streaming (`input_json_delta` events)
- Dynamic model listing via Anthropic API with fallback to hardcoded list
- Session persistence compatibility with proper handling of tool calls and tool results

**Configuration**:
- API key via `ANTHROPIC_API_KEY` environment variable
- Optional base URL via `ANTHROPIC_BASE_URL` environment variable or `ClientOptions.URL`
- Optional model selection via `ANTHROPIC_MODEL` environment variable
- Default model: `claude-sonnet-4-20250514`

**Usage**: 
```bash
export ANTHROPIC_API_KEY=sk-ant-...
kubectl-ai --provider anthropic --model claude-sonnet-4-20250514
```

**Implementation Details**:
- Provider name: `anthropic`
- Implements `gollm.Client` and `gollm.Chat` interfaces
- Handles Anthropic-specific message format, system prompts, and tool use
- Supports both streaming and non-streaming responses
- Properly converts between `api.Message` format and Anthropic's message format for session persistence

#### Gemini Provider Enhancements

**Enhanced**: Improved support for session persistence and message type handling in the Gemini provider.

**Features**:
- Enhanced `Initialize` method to properly convert `api.Message` format to Gemini's native format
- Full support for all message types: text messages, tool call requests, and tool call responses
- Proper conversion between `api.Message` (used for session persistence) and Gemini's `genai.Content` format
- Model name handling with support for explicit parameter, `GEMINI_MODEL` environment variable, or default model
- Graceful handling of unsupported message types with clear error messages

**Configuration**:
- API key via `GEMINI_API_KEY` environment variable (required)
- Optional model selection via `GEMINI_MODEL` environment variable
- Default model: `gemini-2.5-pro` (when not explicitly provided)

#### Azure OpenAI Provider Enhancements

**Enhanced**: Implemented full session persistence support and fixed function schema conversion for Azure OpenAI provider.

**Features**:
- Implemented `Initialize` method to properly convert `api.Message` format to Azure OpenAI's native message format
- Full support for all message types: text messages, tool call requests, tool call responses, and error messages
- Proper conversion between `api.Message` (used for session persistence) and Azure OpenAI's `ChatRequestMessageClassification` format
- Fixed function schema conversion to properly handle array types with required `items` field
- Recursive schema conversion supporting nested objects and arrays
- Support for both API key and Azure AD credential authentication

**Configuration**:
- Endpoint via `AZURE_OPENAI_ENDPOINT` environment variable (required)
- API key via `AZURE_OPENAI_API_KEY` environment variable (optional, can use Azure AD credentials via `az login`)
- Model (deployment name) must be specified via `--model` flag (no default)

**Implementation Details**:
- Provider name: `azopenai`
- Properly handles Azure OpenAI's message structure with system, user, and assistant messages
- Tool calls are structured as assistant messages with `ToolCalls` array
- Tool results are sent as user messages following assistant messages with tool calls
- Function schemas are recursively converted to ensure array types include `items` field (required by Azure OpenAI API)
- Preserves system prompt when initializing from session history

**Bug Fixes**:
- Fixed `invalid_function_parameters` error when functions contain array parameters by ensuring `items` field is included in array schemas
- Fixed schema conversion to handle nested structures (arrays of objects, objects with arrays, etc.)

---
