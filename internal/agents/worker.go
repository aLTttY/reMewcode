package agents

import (
	"github.com/codemelo/mewcode/internal/config"
	"github.com/codemelo/mewcode/internal/conversation"
	"github.com/codemelo/mewcode/internal/llm"
)

type Worker struct {
	Client       llm.Client
	Conversation *conversation.Manager
}

func NewWorker(provider config.Provider, systemPrompt string) (*Worker, error) {
	client, err := llm.NewClient(&provider, systemPrompt)
	if err != nil {
		return nil, err
	}
	return &Worker{
		Client:       client,
		Conversation: conversation.NewManager(),
	}, nil
}
