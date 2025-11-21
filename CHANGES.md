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

---
