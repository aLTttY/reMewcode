package compact

import "github.com/codemelo/mewcode/internal/conversation"

type Compact struct {
	Conversation *conversation.Manager
}

func New() *Compact {
	return &Compact{Conversation: conversation.NewManager()}
}
