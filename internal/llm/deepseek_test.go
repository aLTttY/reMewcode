package llm

import (
	"context"
	"strings"
	"testing"
)

func TestReadDeepSeekSSECompletesToolArguments(t *testing.T) {
	stream := strings.NewReader(strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\""}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"hello.txt\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
		``,
	}, "\n"))
	events := make(chan StreamEvent, 8)
	if err := readDeepSeekSSE(context.Background(), stream, events); err != nil {
		t.Fatal(err)
	}
	close(events)

	var complete ToolCallComplete
	for event := range events {
		if toolComplete, ok := event.(ToolCallComplete); ok {
			complete = toolComplete
		}
	}
	if complete.ID != "call_1" || complete.Name != "read_file" {
		t.Fatalf("complete = %#v", complete)
	}
	if complete.Arguments["path"] != "hello.txt" {
		t.Fatalf("arguments = %#v", complete.Arguments)
	}
}
