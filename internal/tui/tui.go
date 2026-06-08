package tui

import (
	"github.com/codemelo/mewcode/internal/agent"
	"github.com/codemelo/mewcode/internal/agents"
	"github.com/codemelo/mewcode/internal/config"
	"github.com/codemelo/mewcode/internal/conversation"
	"github.com/codemelo/mewcode/internal/llm"
	"github.com/codemelo/mewcode/internal/tools"
)

type Model struct {
	registry *tools.Registry
	agent    *agent.Agent
	conv     *conversation.Manager
}

func NewModel() *Model {
	return &Model{
		registry: tools.NewRegistry(),
		conv:     conversation.NewManager(),
	}
}

func (m *Model) ConfigureProvider(p *config.Provider, systemPrompt string) error {
	client, err := llm.NewClient(p, systemPrompt)
	if err != nil {
		return err
	}
	m.agent = agent.New(client, m.registry, p.Protocol)
	return nil
}

func (m *Model) NewSubAgentTool(provider config.Provider) *agents.AgentTool {
	return agents.NewAgentTool(llm.NewModelResolver(provider))
}

func NewConversationForTUI() *conversation.Manager {
	return conversation.NewManager()
}
