# Tool Support Decision: Always Enabled by Default

## Problem

The original implementation made tool support conditional via `NIRMATA_TOOLS_ENABLED` environment variable, defaulting to enabled. This created inconsistency with other providers and user confusion.

## Analysis

### Other Providers Behavior
- **OpenAI**: Tools always enabled, no configuration needed
- **Bedrock**: Tools always enabled, no configuration needed  
- **Gemini**: Tools always enabled, no configuration needed
- **Grok**: Tools always enabled, no configuration needed
- **Azure OpenAI**: Tools always enabled, no configuration needed

### Problems with Conditional Support
1. **Provider inconsistency**: Only Nirmata required configuration
2. **User confusion**: Users need to know about env var
3. **False assumption**: We assumed backend might not support tools
4. **Breaking transparency**: Violates "zero code changes when switching providers"

## Decision

**Make tool calling a core requirement** (not optional).

### Implementation
- `checkToolSupport()` always returns `true` (tools required)
- Removed all conditional logic from `SetFunctionDefinitions`
- Simplified tool sending logic (no `supportsTools` checks)
- Tool calling is mandatory for Nirmata provider
- No fallback mode - tools must work or provider fails

### Benefits
1. **Provider transparency**: Seamless switching between providers
2. **Simplified implementation**: No conditional logic needed
3. **Consistent behavior**: Matches OpenAI, Bedrock, etc.
4. **Clear expectations**: Tools are required, not optional

## Files Changed

1. **gollm/nirmata.go**: Updated `checkToolSupport()` function
2. **scope.md**: Updated design requirements and status
3. **test_default_behavior.go**: Test verifying new behavior

## Migration

This is a **requirement** change:
- ✅ Existing code works when backend supports tools
- ✅ No environment variables needed
- ✅ Complete consistency with other providers
- ⚠️ Nirmata provider will fail if backend doesn't support tools (by design)

## Result

Nirmata provider now behaves identically to all other providers regarding tool support.