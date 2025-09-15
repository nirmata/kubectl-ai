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

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/GoogleCloudPlatform/kubectl-ai/gollm"
)

func testProvider(providerName string, modelID string) {
	fmt.Printf("\n=== Testing %s provider ===\n", providerName)

	ctx := context.Background()

	// Create client
	client, err := gollm.NewClient(ctx, providerName)
	if err != nil {
		fmt.Printf("❌ Failed to create %s client: %v\n", providerName, err)
		return
	}

	// Define a test tool
	tools := []*gollm.FunctionDefinition{
		{
			Name:        "get_weather",
			Description: "Get the current weather for a location",
			Parameters: &gollm.Schema{
				Type: "object",
				Properties: map[string]*gollm.Schema{
					"location": {
						Type:        "string",
						Description: "The city and state, e.g. San Francisco, CA",
					},
				},
				Required: []string{"location"},
			},
		},
	}

	// Create a chat with tools
	chat := client.StartChat("You are a helpful assistant that can check weather.", modelID)
	chat.SetFunctionDefinitions(tools)

	// Send a message that should trigger tool use
	response, err := chat.Send(ctx, "What's the weather in San Francisco?")
	if err != nil {
		fmt.Printf("❌ Failed to send message: %v\n", err)
		return
	}

	// Check if tool calls are returned
	foundToolCall := false
	for _, candidate := range response.Candidates() {
		for _, part := range candidate.Parts() {
			if calls, ok := part.AsFunctionCalls(); ok && len(calls) > 0 {
				foundToolCall = true
				fmt.Printf("✅ Tool call found!\n")
				for _, call := range calls {
					fmt.Printf("   - Function: %s\n", call.Name)
					fmt.Printf("   - Arguments: %v\n", call.Arguments)
				}
			}

			// Also check for text response
			if text, ok := part.AsText(); ok && text != "" {
				fmt.Printf("📝 Text response: %s\n", text)
			}
		}
	}

	if !foundToolCall {
		fmt.Printf("❌ No tool calls found in response\n")
	}
}

func main() {
	// Test different providers
	providers := []struct {
		name  string
		model string
	}{
		{"bedrock", "us.anthropic.claude-3-5-sonnet-20240620-v1:0"},
		{"nirmata", "llama-3.1-70b"},
		{"openai", "gpt-4"},
	}

	for _, p := range providers {
		// Skip if no API key configured
		switch p.name {
		case "bedrock":
			if os.Getenv("AWS_REGION") == "" {
				fmt.Printf("\nSkipping Bedrock (AWS_REGION not set)\n")
				continue
			}
		case "nirmata":
			if os.Getenv("NIRMATA_API_KEY") == "" {
				fmt.Printf("\nSkipping Nirmata (NIRMATA_API_KEY not set)\n")
				continue
			}
		case "openai":
			if os.Getenv("OPENAI_API_KEY") == "" {
				fmt.Printf("\nSkipping OpenAI (OPENAI_API_KEY not set)\n")
				continue
			}
		}

		testProvider(p.name, p.model)
	}
}
