#!/bin/bash

# Test if go-llm-apps backend returns tool calls

echo "Testing go-llm-apps backend tool calling..."

# Create a request with tools
cat > /tmp/chat_request.json << 'EOF'
{
  "messages": [
    {
      "role": "user",
      "content": "What's the weather in San Francisco?"
    }
  ],
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "get_weather",
        "description": "Get the current weather for a location",
        "parameters": {
          "type": "object",
          "properties": {
            "location": {
              "type": "string",
              "description": "The city and state, e.g. San Francisco, CA"
            }
          },
          "required": ["location"]
        }
      }
    }
  ],
  "model": "us.anthropic.claude-sonnet-4-20250514-v1:0",
  "stream": false
}
EOF

# Check if server is running
if ! curl -k -s https://localhost:8443 > /dev/null 2>&1; then
    echo "❌ Server not running on https://localhost:8443"
    echo "Please start the go-llm-apps server first"
    exit 1
fi

echo "Sending request to https://localhost:8443/chat..."

# Send request and capture response
response=$(curl -k -s -X POST https://localhost:8443/chat \
    -H "Content-Type: application/json" \
    -H "Authorization: NIRMATA-API test-api-key" \
    -d @/tmp/chat_request.json)

echo "Response:"
echo "$response" | jq '.' || echo "$response"

# Check for tool_calls in response
if echo "$response" | grep -q "tool_calls"; then
    echo "✅ Tool calls found in response!"
    echo "$response" | jq '.tool_calls'
else
    echo "❌ No tool_calls found in response"
fi

# Clean up
rm /tmp/chat_request.json