package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/codemelo/mewcode/internal/conversation"
	"github.com/codemelo/mewcode/internal/llm"
	"github.com/codemelo/mewcode/internal/tools"
)

type fakeClient struct {
	events []llm.StreamEvent
	err    error
}

func (c fakeClient) Stream(context.Context, *conversation.Manager, []map[string]any) (<-chan llm.StreamEvent, <-chan error) {
	events := make(chan llm.StreamEvent, len(c.events))
	errs := make(chan error, 1)
	for _, event := range c.events {
		events <- event
	}
	close(events)
	errs <- c.err
	close(errs)
	return events, errs
}

func TestRunOnceExecutesToolCall(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	conv := conversation.NewManager()
	conv.AddUser("read hello.txt")
	agent := New(fakeClient{events: []llm.StreamEvent{
		llm.ToolCallStart{ID: "call_1", Name: "read_file"},
		llm.ToolCallComplete{ID: "call_1", Name: "read_file", Arguments: map[string]any{"path": "hello.txt"}},
		llm.StreamEnd{StopReason: "tool_calls"},
	}}, tools.NewRegistryAt(root), "deepseek")

	if err := agent.RunOnce(context.Background(), conv); err != nil {
		t.Fatal(err)
	}
	messages := conv.GetMessages()
	if len(messages) != 3 {
		t.Fatalf("messages len = %d, want 3", len(messages))
	}
	if len(messages[1].ToolUses) != 1 {
		t.Fatalf("assistant tool uses = %#v", messages[1].ToolUses)
	}
	if len(messages[2].ToolResults) != 1 {
		t.Fatalf("tool results = %#v", messages[2].ToolResults)
	}
	result := messages[2].ToolResults[0]
	if result.IsError {
		t.Fatalf("tool result is error: %s", result.Content)
	}
	var payload struct {
		OK    bool `json:"ok"`
		Value struct {
			Content string `json:"content"`
		} `json:"value"`
	}
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || payload.Value.Content != "hello" {
		t.Fatalf("payload = %#v", payload)
	}
}
