package main

import (
	"github.com/codemelo/mewcode/internal/config"
	"github.com/codemelo/mewcode/internal/conversation"
	"github.com/codemelo/mewcode/internal/llm"
)

type teammateWorker struct {
	client llm.Client
	conv   *conversation.Manager
}

func newTeammateWorker(provider config.Provider) (*teammateWorker, error) {
	client, err := llm.NewClient(&provider, "")
	if err != nil {
		return nil, err
	}
	return &teammateWorker{
		client: client,
		conv:   conversation.NewManager(),
	}, nil
}
