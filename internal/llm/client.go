package llm

import (
	"context"
	"fmt"

	"github.com/codemelo/mewcode/internal/config"
	"github.com/codemelo/mewcode/internal/conversation"
)

type Client interface {
	Stream(ctx context.Context, conv *conversation.Manager, tools []map[string]any) (<-chan StreamEvent, <-chan error)
}

type MaxTokensSetter interface {
	SetMaxTokens(maxTokens int)
}

func NewClient(provider *config.Provider, systemPrompt string) (Client, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider is nil")
	}
	switch provider.Protocol {
	case "anthropic":
		return newAnthropicClient(provider, systemPrompt), nil
	case "openai":
		return newOpenAIClient(provider, systemPrompt), nil
	case "deepseek":
		return newDeepSeekClient(provider, systemPrompt), nil
	default:
		return nil, fmt.Errorf("unknown protocol: %s", provider.Protocol)
	}
}
