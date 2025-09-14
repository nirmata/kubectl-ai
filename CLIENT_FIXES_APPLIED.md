# Client-Side Fixes Applied to Nirmata Provider

## Summary

Successfully applied 3 critical fixes to the Nirmata provider client code (`gollm/nirmata.go`) to address tool calling issues identified in the `fixes.md` analysis.

## Fixes Applied

### ✅ Issue #2: Silent Parse Failures - FIXED
**Location**: `nirmata.go:468-476`

**Before**:
```go
klog.V(2).Infof("Failed to parse tool call from stream: %v", err)
```

**After**:
```go
// Make parse errors visible to users (Issue #2 fix)
klog.Errorf("Failed to parse tool call from stream data: %v (data: %q)", err, streamData.Data)
// Send error to user so they can see what went wrong
response := &nirmataStreamResponse{
    content: fmt.Sprintf("[Tool parsing error: %v]", err),
    model:   c.model,
    done:    false,
}
yield(response, nil)
```

**Impact**: Users now see parse errors immediately instead of silent failures

---

### ✅ Issue #4: Forced Provider Routing - FIXED
**Location**: `nirmata.go:350-352`

**Before**:
```go
q.Set("chunked", "true")
q.Set("provider", "bedrock")  // Forces bedrock regardless of configuration
```

**After**:
```go
q.Set("chunked", "true")
// Issue #4 fix: Don't force provider - let backend decide based on its configuration
// Removed: q.Set("provider", "bedrock")
```

**Impact**: Backend can now use its configured provider instead of being forced to use Bedrock

---

### ✅ Issue #5: Arguments Parsing Failures - FIXED
**Location**: `nirmata.go:789-810`

**Before**:
```go
if err := json.Unmarshal([]byte(p.toolCall.Function.Arguments), &args); err != nil {
    klog.V(2).Infof("Failed to unmarshal tool arguments: %v", err)
    args = make(map[string]any)
}
```

**After**:
```go
// Parse arguments from JSON string (Issue #5 fix: better error handling)
if err := json.Unmarshal([]byte(p.toolCall.Function.Arguments), &args); err != nil {
    // Make error visible to help debugging
    klog.Errorf("Failed to parse tool arguments for %s: %v (raw: %q)",
        p.toolCall.Function.Name, err, p.toolCall.Function.Arguments)

    // Use empty args but make it clear there was an issue
    args = make(map[string]any)
    args["_parse_error"] = fmt.Sprintf("Failed to parse arguments: %v", err)
}
```

**Impact**: Argument parsing errors are now visible and tracked with `_parse_error` field

## Test Results

All fixes have been validated with unit tests:

```
=== NIRMATA CLIENT FIXES SUMMARY ===

Issue #2 ✅: Parse errors now visible at Error level
  - Changed from klog.V(2) to klog.Errorf
  - User sees [Tool parsing error: ...] message

Issue #4 ✅: Provider no longer forced to 'bedrock'
  - Removed q.Set("provider", "bedrock")
  - Backend decides provider based on configuration

Issue #5 ✅: Argument parse errors visible
  - Changed from klog.V(2) to klog.Errorf
  - Added _parse_error field when parsing fails
```

## Remaining Issues (Backend Fixes Required)

The following issues require fixes in the `go-llm-apps` backend repository:

### ❌ Issue #1: Data Contract Violation
**Location**: `go-llm-apps/pkg/agent/conversation.go:403`
- Backend sends plain text instead of JSON
- Client expects: `{"tool_call": {...}}`

### ❌ Issue #3: Missing Tool Call Forwarding
**Location**: `go-llm-apps/pkg/agent/conversation.go:174-178`
- LLM tool calls are collected but not forwarded to client
- Client never knows when LLM wants to use tools

## Next Steps

1. **Deploy these client fixes** to production
2. **Apply backend fixes** in go-llm-apps repository:
   - Fix JSON structure for tool events (Issue #1)
   - Forward LLM tool calls to client (Issue #3)
3. **Test end-to-end** once both client and backend fixes are deployed

## Testing the Fixes

To test these fixes:

```bash
# Run the fix validation tests
go test -run "TestIssue" -v ./gollm

# Manual testing (after backend fixes)
nctl ai --provider nirmata --prompt "list files in current directory"
```

## Files Modified

- `gollm/nirmata.go` - Applied 3 fixes
- `gollm/nirmata_fixes_test.go` - Added validation tests

## Workaround Until Backend Fixed

Users can bypass the Nirmata backend entirely:
```bash
nctl ai --provider bedrock --prompt "your prompt"
```

This connects directly to AWS Bedrock, avoiding the translation layer issues.