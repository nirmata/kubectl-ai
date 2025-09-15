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
	"log"

	"github.com/GoogleCloudPlatform/kubectl-ai/gollm"
)

func main() {
	// Test if Bedrock provider properly returns tool calls
	ctx := context.Background()

	// Create Bedrock client
	client, err := gollm.NewClient(ctx, "bedrock")
	if err != nil {
		log.Fatalf("Failed to create Bedrock client: %v", err)
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
	chat := client.StartChat("You are a helpful assistant.", "us.anthropic.claude-sonnet-4-20250514-v1:0")
	chat.SetFunctionDefinitions(tools)

	// Send a message that should trigger tool use
	response, err := chat.Send(ctx, "What's the weather in San Francisco?")
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}

	// Check if tool calls are returned
	foundToolCall := false
	for _, candidate := range response.Candidates() {
		for _, part := range candidate.Parts() {
			if calls, ok := part.AsFunctionCalls(); ok && len(calls) > 0 {
				foundToolCall = true
				fmt.Printf("✅ Tool call found: %+v\n", calls[0])
			}
		}
	}

	if !foundToolCall {
		fmt.Println("❌ No tool calls found in response")
		// Print the response for debugging
		for _, candidate := range response.Candidates() {
			for _, part := range candidate.Parts() {
				if text, ok := part.AsText(); ok {
					fmt.Printf("Response text: %s\n", text)
				}
			}
		}
	}
}
