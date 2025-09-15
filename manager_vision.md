# Nirmata Provider Architecture

## Overview

Nirmata's AI agents use the gollm framework from kubectl-ai:
https://github.com/nirmata/kubectl-ai/tree/main/gollm

## Customer Experience Goals

1. **Default Flow**: 
   - Sign up on nirmata.io 
   - Install nctl and authenticate
   - Run `nctl ai` to use the PaC agent
   - All inference goes through Nirmata's managed service by default

2. **BYOM (Bring Your Own Model)**:
   - Premium feature requiring special license
   - Specify custom provider via `--provider` and `--model` flags
   - Maintains exact same behavior as Nirmata provider

## Technical Design

### Core Requirements

1. **Provider Implementation**:
   - Add `nirmata` provider to gollm
   - Send all messages to `https://nirmata.io/llm/chat`
   - Support both streaming and non-streaming modes

2. **Backend Service Requirements**:
   - **Authentication**: Validate API/JWT tokens
   - **Multi-tenancy**: Apply rate limiting and licensing per tenant
   - **Proxy Layer**: Forward requests to underlying provider (currently Bedrock)
   - **Observability**: Record usage metrics and logs
   - **Future-proof**: Support switching inference providers without client changes

### Architectural Principles

1. **Stateless Design** 🔑
   - Each request must be self-contained with complete conversation history
   - No server-side session state
   - Enables horizontal scaling and multi-tenant isolation
   - Example request structure:
   ```json
   {
     "messages": [
       {"role": "system", "content": "..."},
       {"role": "user", "content": "..."},
       {"role": "assistant", "content": "...", "tool_calls": [...]},
       {"role": "tool", "tool_call_id": "...", "content": "..."},
       {"role": "user", "content": "..."}
     ],
     "tools": [...]
   }
   ```

2. **Provider Transparency** 🔑
   - Switching between `nirmata` and `bedrock` providers must not require code changes
   - Identical API responses and behavior
   - Tool calling must work identically across providers
   - Streaming format must be consistent

3. **Tool Support Requirements**:
   - Clients register MCP tools in request
   - Backend returns tool invocation requests
   - Client executes tools locally
   - Client sends tool results back in conversation
   - Tool call IDs must be preserved across stateless requests

4. **Streaming via HTTP Chunked Transfer** 🔑
   - Use `Transfer-Encoding: chunked` header
   - Stream JSONL formatted events
   - Support tool events in stream
   - Flush after each chunk for real-time updates

## Implementation Tasks

### Phase 1: Provider Setup
- [ ] Move Nirmata provider implementation to go-llm-apps
- [ ] Implement stateless request handling with full conversation history
- [ ] Add HTTP chunked transfer encoding for streaming

### Phase 2: Tool Calling
- [ ] Tool registration in request payload
- [ ] Tool call extraction from Bedrock responses
- [ ] Tool result forwarding in conversation history
- [ ] Write custom conversation.go for testing (always returns mock tool responses)

### Phase 3: Backend Services
- [ ] Authentication and authorization middleware
- [ ] Rate limiting and usage tracking
- [ ] Multi-tenant request isolation
- [ ] Metrics and logging infrastructure

### Phase 4: Testing & Documentation
- [ ] End-to-end tool calling tests
- [ ] Provider transparency verification
- [ ] Update API documentation
- [ ] Migration guide for existing users