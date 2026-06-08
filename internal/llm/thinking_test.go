package llm

import (
	"errors"
	"net/http"
	"testing"

	"github.com/codemelo/mewcode/internal/config"
	"github.com/codemelo/mewcode/internal/conversation"
)

func TestSupportsAdaptiveThinking(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"claude-sonnet-4-6", true},
		{"claude-opus-4-7-20260601", true},
		{"claude-sonnet-4-5", false},
		{"gpt-4.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			if got := supportsAdaptiveThinking(tt.model); got != tt.want {
				t.Fatalf("supportsAdaptiveThinking(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestAnthropicThinkingAdaptive(t *testing.T) {
	t.Log("Official model -> adaptive: claude-sonnet-4-6")
	client := newAnthropicClient(&config.Provider{
		Protocol:  "anthropic",
		Model:     "claude-sonnet-4-6",
		MaxTokens: 2048,
		Thinking:  true,
	}, "")

	request, err := client.buildRequest(conversation.NewManager(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if request.Thinking == nil || request.Thinking.Type != "adaptive" {
		t.Fatalf("thinking = %#v, want adaptive", request.Thinking)
	}
	if request.Thinking.BudgetTokens != 0 {
		t.Fatalf("adaptive thinking must not set budget tokens, got %d", request.Thinking.BudgetTokens)
	}
}

func TestAnthropicThinkingEnabled(t *testing.T) {
	t.Log("Unofficial model -> enabled with fixed budget")
	client := newAnthropicClient(&config.Provider{
		Protocol:  "anthropic",
		Model:     "claude-3-5-sonnet-latest",
		MaxTokens: 2048,
		Thinking:  true,
	}, "")

	request, err := client.buildRequest(conversation.NewManager(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if request.Thinking == nil || request.Thinking.Type != "enabled" {
		t.Fatalf("thinking = %#v, want enabled", request.Thinking)
	}
	if request.Thinking.BudgetTokens != 2047 {
		t.Fatalf("budget tokens = %d, want 2047", request.Thinking.BudgetTokens)
	}
}

func TestAnthropicThinkingDisabled(t *testing.T) {
	client := newAnthropicClient(&config.Provider{
		Protocol:  "anthropic",
		Model:     "claude-sonnet-4-6",
		MaxTokens: 2048,
		Thinking:  false,
	}, "")

	request, err := client.buildRequest(conversation.NewManager(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if request.Thinking != nil {
		t.Fatalf("thinking = %#v, want nil", request.Thinking)
	}
}

func TestAnthropicThinkingBlocksInConversation(t *testing.T) {
	conv := conversation.NewManager()
	conv.AddAssistantFull("answer", []conversation.ThinkingBlock{
		{Text: "private reasoning", Signature: "sig-123"},
	}, []conversation.ToolUseBlock{
		{ID: "toolu_1", Name: "lookup", Arguments: map[string]any{"q": "hello"}},
	})

	messages, err := buildAnthropicMessages(conv)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(messages))
	}
	if got := messages[0].ThinkingBlocks[0].Signature; got != "sig-123" {
		t.Fatalf("signature = %q, want sig-123", got)
	}
	if got := messages[0].ToolUses[0].Arguments["q"]; got != "hello" {
		t.Fatalf("tool argument q = %v, want hello", got)
	}
}

func TestOpenAIReasoningEnabled(t *testing.T) {
	client := newOpenAIClient(&config.Provider{
		Protocol: "openai",
		Model:    "gpt-5",
		Thinking: true,
	}, "")

	request, err := client.buildRequest(conversation.NewManager(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if request.Reasoning == nil {
		t.Fatal("reasoning is nil, want enabled")
	}
	if request.Reasoning.Effort != "high" || request.Reasoning.Summary != "detailed" {
		t.Fatalf("reasoning = %#v, want high/detailed", request.Reasoning)
	}
	if len(request.Include) != 1 || request.Include[0] != "reasoning.encrypted_content" {
		t.Fatalf("include = %#v, want reasoning.encrypted_content", request.Include)
	}
}

func TestOpenAIReasoningDisabled(t *testing.T) {
	client := newOpenAIClient(&config.Provider{
		Protocol: "openai",
		Model:    "gpt-5",
		Thinking: false,
	}, "")

	request, err := client.buildRequest(conversation.NewManager(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if request.Reasoning != nil {
		t.Fatalf("reasoning = %#v, want nil", request.Reasoning)
	}
	if len(request.Include) != 0 {
		t.Fatalf("include = %#v, want empty", request.Include)
	}
}

func TestClassifyProviderErrors(t *testing.T) {
	var contextErr *ContextTooLongError
	if !errors.As(classifyOpenAIError(&httpStatusError{
		StatusCode: http.StatusBadRequest,
		Message:    "context_length_exceeded",
		Headers:    http.Header{},
	}), &contextErr) {
		t.Fatal("OpenAI context error was not classified")
	}

	var rateErr *RateLimitError
	if !errors.As(classifyAnthropicError(&httpStatusError{
		StatusCode: http.StatusTooManyRequests,
		Message:    "rate limited",
		Headers:    http.Header{"Retry-After": []string{"3"}},
	}), &rateErr) {
		t.Fatal("Anthropic rate limit error was not classified")
	}
	if rateErr.RetryAfter.Seconds() != 3 {
		t.Fatalf("RetryAfter = %s, want 3s", rateErr.RetryAfter)
	}
}
