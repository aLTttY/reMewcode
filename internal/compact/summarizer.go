package compact

import (
	"github.com/codemelo/mewcode/internal/config"
	"github.com/codemelo/mewcode/internal/conversation"
	"github.com/codemelo/mewcode/internal/llm"
)

type Summarizer struct {
	Client       llm.Client
	Conversation *conversation.Manager
}

func NewSummarizer(provider config.Provider) (*Summarizer, error) {
	client, err := llm.NewClient(&provider, "Summarize the conversation compactly.")
	if err != nil {
		return nil, err
	}
	return &Summarizer{
		Client:       client,
		Conversation: conversation.NewManager(),
	}, nil
}
