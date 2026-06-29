package conversation

import "testing"

func TestSystemReminderAndDeepCopy(t *testing.T) {
	conv := NewManager()
	conv.AddSystemReminder("remember this")

	messages := conv.GetMessages()
	if messages[0].Content != "<system-reminder>\nremember this\n</system-reminder>" {
		t.Fatalf("unexpected reminder content: %q", messages[0].Content)
	}

	messages[0].Content = "mutated"
	if got := conv.GetMessages()[0].Content; got == "mutated" {
		t.Fatal("GetMessages returned mutable history")
	}
}

func TestSerializeAnthropicMergesAdjacentText(t *testing.T) {
	conv := NewManager()
	conv.AddUser("one")
	conv.AddUser("two")
	conv.AddAssistantFull("answer", []ThinkingBlock{{Text: "think", Signature: "sig"}}, nil)
	conv.AddAssistant("more")

	messages, err := conv.Serialize("anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 {
		t.Fatalf("messages len = %d, want 3", len(messages))
	}
	if messages[0].Content != "one\n\ntwo" {
		t.Fatalf("merged content = %q", messages[0].Content)
	}
	if messages[1].ThinkingBlocks[0].Signature != "sig" {
		t.Fatal("thinking signature was not preserved")
	}
}

func TestTruncateRestoresHistoryLength(t *testing.T) {
	conv := NewManager()
	conv.AddUser("kept")
	checkpoint := conv.Len()
	conv.AddUser("failed")

	conv.Truncate(checkpoint)

	messages := conv.GetMessages()
	if len(messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(messages))
	}
	if messages[0].Content != "kept" {
		t.Fatalf("remaining content = %q, want kept", messages[0].Content)
	}
}
