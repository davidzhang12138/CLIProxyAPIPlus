package claude

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildKiroPayloadTextifiesToolHistoryWhenNoToolsDeclared(t *testing.T) {
	input := []byte(`{
		"model": "claude-haiku-4-5",
		"max_tokens": 64000,
		"temperature": 1,
		"tools": [],
		"messages": [
			{
				"role": "assistant",
				"content": [
					{
						"type": "tool_use",
						"id": "toolu_1",
						"name": "Read",
						"input": {"file_path": "a.txt"}
					}
				]
			},
			{
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "toolu_1",
						"content": "file contents"
					}
				]
			},
			{"role": "user", "content": "summarize"}
		]
	}`)

	result, _ := BuildKiroPayload(input, "claude-haiku-4.5", "", "AI_EDITOR", false, false, nil, nil)

	var payload KiroPayload
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if payload.InferenceConfig == nil {
		t.Fatal("expected inferenceConfig")
	}
	if payload.InferenceConfig.MaxTokens != 32000 {
		t.Fatalf("maxTokens = %d, want 32000", payload.InferenceConfig.MaxTokens)
	}

	var foundToolUseText bool
	var foundToolResultText bool
	for i, history := range payload.ConversationState.History {
		if history.AssistantResponseMessage != nil {
			if len(history.AssistantResponseMessage.ToolUses) > 0 {
				t.Fatalf("history[%d] has Kiro toolUses despite no declared tools: %+v", i, history.AssistantResponseMessage.ToolUses)
			}
			if strings.Contains(history.AssistantResponseMessage.Content, "Read") &&
				strings.Contains(history.AssistantResponseMessage.Content, "a.txt") {
				foundToolUseText = true
			}
		}
		if history.UserInputMessage != nil {
			if history.UserInputMessage.UserInputMessageContext != nil {
				t.Fatalf("history[%d] has Kiro userInputMessageContext despite no declared tools: %+v", i, history.UserInputMessage.UserInputMessageContext)
			}
			if strings.Contains(history.UserInputMessage.Content, "toolu_1") &&
				strings.Contains(history.UserInputMessage.Content, "file contents") {
				foundToolResultText = true
			}
		}
	}
	currentUser := payload.ConversationState.CurrentMessage.UserInputMessage
	if currentUser.UserInputMessageContext != nil {
		t.Fatalf("currentMessage has Kiro userInputMessageContext despite no declared tools: %+v", currentUser.UserInputMessageContext)
	}
	if strings.Contains(currentUser.Content, "toolu_1") &&
		strings.Contains(currentUser.Content, "file contents") {
		foundToolResultText = true
	}

	if !foundToolUseText {
		t.Fatal("expected assistant tool_use to be preserved as plain text")
	}
	if !foundToolResultText {
		t.Fatal("expected user tool_result to be preserved as plain text")
	}
}

func TestBuildKiroPayloadKeepsToolHistoryWhenToolsDeclared(t *testing.T) {
	input := []byte(`{
		"model": "claude-haiku-4-5",
		"max_tokens": 1024,
		"tools": [
			{
				"name": "Read",
				"description": "Read a file",
				"input_schema": {
					"type": "object",
					"properties": {
						"file_path": {"type": "string"}
					}
				}
			}
		],
		"messages": [
			{"role": "user", "content": "read a file"},
			{
				"role": "assistant",
				"content": [
					{
						"type": "tool_use",
						"id": "toolu_1",
						"name": "Read",
						"input": {"file_path": "a.txt"}
					}
				]
			},
			{
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "toolu_1",
						"content": "file contents"
					}
				]
			},
			{"role": "user", "content": "summarize"}
		]
	}`)

	result, _ := BuildKiroPayload(input, "claude-haiku-4.5", "", "AI_EDITOR", false, false, nil, nil)

	var payload KiroPayload
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	var foundToolUse bool
	var foundToolResult bool
	for _, history := range payload.ConversationState.History {
		if history.AssistantResponseMessage != nil && len(history.AssistantResponseMessage.ToolUses) == 1 {
			foundToolUse = true
		}
		if history.UserInputMessage != nil &&
			history.UserInputMessage.UserInputMessageContext != nil &&
			len(history.UserInputMessage.UserInputMessageContext.ToolResults) == 1 {
			foundToolResult = true
		}
	}
	currentCtx := payload.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext
	if currentCtx != nil && len(currentCtx.ToolResults) == 1 {
		foundToolResult = true
	}

	if !foundToolUse {
		t.Fatal("expected declared-tools request to keep Kiro toolUses")
	}
	if !foundToolResult {
		t.Fatal("expected declared-tools request to keep Kiro toolResults")
	}
}
