package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/codemelo/mewcode/internal/conversation"
	"github.com/codemelo/mewcode/internal/llm"
	"github.com/codemelo/mewcode/internal/tools"
)

type Agent struct {
	Client   llm.Client
	Tools    *tools.Registry
	Protocol string
}

func New(client llm.Client, registry *tools.Registry, protocol string) *Agent {
	return &Agent{Client: client, Tools: registry, Protocol: protocol}
}

func (a *Agent) RunOnce(ctx context.Context, conv *conversation.Manager) error {
	if a.Client == nil {
		return errors.New("llm client is nil")
	}
	var toolSchemas []map[string]any
	if a.Tools != nil {
		toolSchemas = a.Tools.Schemas(a.Protocol)
	}

	events, errs := a.Client.Stream(ctx, conv, toolSchemas)
	var text string
	var thinkingBlocks []conversation.ThinkingBlock
	var toolUses []conversation.ToolUseBlock
	pendingToolArgs := map[string]string{}
	pendingToolNames := map[string]string{}

	for event := range events {
		switch e := event.(type) {
		case llm.ThinkingDelta:
			if len(thinkingBlocks) == 0 {
				thinkingBlocks = append(thinkingBlocks, conversation.ThinkingBlock{})
			}
			thinkingBlocks[len(thinkingBlocks)-1].Text += e.Text
		case llm.ThinkingComplete:
			thinkingBlocks = append(thinkingBlocks, conversation.ThinkingBlock{Text: e.Text, Signature: e.Signature})
		case llm.TextDelta:
			text += e.Text
		case llm.ToolCallStart:
			pendingToolNames[e.ID] = e.Name
		case llm.ToolCallDelta:
			pendingToolArgs[e.ID] += e.Delta
		case llm.ToolCallComplete:
			toolUses = append(toolUses, conversation.ToolUseBlock{
				ID:        e.ID,
				Name:      e.Name,
				Arguments: e.Arguments,
			})
			delete(pendingToolArgs, e.ID)
			delete(pendingToolNames, e.ID)
		case llm.StreamEnd:
			conv.AddAssistantFull(text, thinkingBlocks, toolUses)
		default:
			return fmt.Errorf("unhandled stream event %T", e)
		}
	}

	if err := <-errs; err != nil {
		return handleStreamError(err)
	}
	if len(toolUses) > 0 {
		conv.AddToolResults(a.executeToolUses(ctx, toolUses))
	}
	return nil
}

func (a *Agent) executeToolUses(ctx context.Context, toolUses []conversation.ToolUseBlock) []conversation.ToolResultBlock {
	results := make([]conversation.ToolResultBlock, 0, len(toolUses))
	for _, toolUse := range toolUses {
		result := tools.ToolResult{
			Tool:  toolUse.Name,
			OK:    false,
			Error: "tool registry is nil",
		}
		if a.Tools != nil {
			result = a.Tools.Execute(ctx, toolUse.Name, toolUse.Arguments)
		}
		results = append(results, conversation.ToolResultBlock{
			ToolUseID: toolUse.ID,
			Content:   result.JSON(),
			IsError:   !result.OK,
		})
	}
	return results
}

func handleStreamError(err error) error {
	var auth *llm.AuthenticationError
	var contextTooLong *llm.ContextTooLongError
	var rateLimit *llm.RateLimitError
	var network *llm.NetworkError
	var llmErr *llm.LLMError

	switch {
	case errors.As(err, &auth):
		return auth
	case errors.As(err, &contextTooLong):
		return contextTooLong
	case errors.As(err, &rateLimit):
		return rateLimit
	case errors.As(err, &network):
		return network
	case errors.As(err, &llmErr):
		return llmErr
	default:
		return err
	}
}
