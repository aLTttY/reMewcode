package agents

import "github.com/codemelo/mewcode/internal/llm"

type AgentTool struct {
	ResolveModel func(shortName string) (llm.Client, error)
}

func NewAgentTool(resolveModel func(shortName string) (llm.Client, error)) *AgentTool {
	return &AgentTool{ResolveModel: resolveModel}
}
