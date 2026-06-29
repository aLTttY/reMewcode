package conversation

import "fmt"

type ToolUseBlock struct {
	ID        string
	Name      string
	Arguments map[string]any
}

type ToolResultBlock struct {
	ToolUseID string
	Content   string
	IsError   bool
}

type ThinkingBlock struct {
	Text      string
	Signature string
}

type Message struct {
	Role           string
	Content        string
	ThinkingBlocks []ThinkingBlock
	ToolUses       []ToolUseBlock
	ToolResults    []ToolResultBlock
}

type SerializedMessage struct {
	Role           string
	Content        string
	ThinkingBlocks []ThinkingBlock
	ToolUses       []ToolUseBlock
	ToolResults    []ToolResultBlock
}

type Manager struct {
	history []Message
}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) AddUser(content string) {
	m.history = append(m.history, Message{Role: "user", Content: content})
}

func (m *Manager) AddAssistant(content string) {
	m.history = append(m.history, Message{Role: "assistant", Content: content})
}

func (m *Manager) AddAssistantThinking(text, signature string) {
	m.history = append(m.history, Message{
		Role:           "assistant",
		ThinkingBlocks: []ThinkingBlock{{Text: text, Signature: signature}},
	})
}

func (m *Manager) AddAssistantToolUse(id, name string, arguments map[string]any) {
	m.history = append(m.history, Message{
		Role:     "assistant",
		ToolUses: []ToolUseBlock{{ID: id, Name: name, Arguments: cloneMap(arguments)}},
	})
}

func (m *Manager) AddAssistantFull(content string, thinkingBlocks []ThinkingBlock, toolUses []ToolUseBlock) {
	m.history = append(m.history, Message{
		Role:           "assistant",
		Content:        content,
		ThinkingBlocks: cloneThinkingBlocks(thinkingBlocks),
		ToolUses:       cloneToolUses(toolUses),
	})
}

func (m *Manager) AddToolResult(toolUseID, content string, isError bool) {
	m.history = append(m.history, Message{
		Role:        "user",
		ToolResults: []ToolResultBlock{{ToolUseID: toolUseID, Content: content, IsError: isError}},
	})
}

func (m *Manager) AddToolResults(results []ToolResultBlock) {
	m.history = append(m.history, Message{
		Role:        "user",
		ToolResults: cloneToolResults(results),
	})
}

func (m *Manager) AddMessage(msg Message) {
	m.history = append(m.history, cloneMessage(msg))
}

func (m *Manager) AddSystemReminder(content string) {
	m.AddUser(fmt.Sprintf("<system-reminder>\n%s\n</system-reminder>", content))
}

func (m *Manager) GetMessages() []Message {
	out := make([]Message, len(m.history))
	for i, msg := range m.history {
		out[i] = cloneMessage(msg)
	}
	return out
}

func (m *Manager) Len() int {
	return len(m.history)
}

func (m *Manager) Truncate(length int) {
	if length < 0 {
		length = 0
	}
	if length > len(m.history) {
		return
	}
	m.history = m.history[:length]
}

func (m *Manager) Serialize(protocol string) ([]SerializedMessage, error) {
	switch protocol {
	case "anthropic":
		return serializeAnthropic(m.history), nil
	case "openai":
		return serializeOpenAI(m.history), nil
	default:
		return nil, fmt.Errorf("unknown protocol: %s", protocol)
	}
}

func serializeAnthropic(messages []Message) []SerializedMessage {
	var out []SerializedMessage
	for _, msg := range messages {
		next := SerializedMessage{
			Role:           msg.Role,
			Content:        msg.Content,
			ThinkingBlocks: cloneThinkingBlocks(msg.ThinkingBlocks),
			ToolUses:       cloneToolUses(msg.ToolUses),
			ToolResults:    cloneToolResults(msg.ToolResults),
		}
		if len(out) > 0 {
			last := &out[len(out)-1]
			if last.Role == next.Role && canMergeTextOnly(*last) && canMergeTextOnly(next) {
				if last.Content == "" {
					last.Content = next.Content
				} else if next.Content != "" {
					last.Content += "\n\n" + next.Content
				}
				continue
			}
		}
		out = append(out, next)
	}
	return out
}

func serializeOpenAI(messages []Message) []SerializedMessage {
	out := make([]SerializedMessage, len(messages))
	for i, msg := range messages {
		out[i] = SerializedMessage{
			Role:           msg.Role,
			Content:        msg.Content,
			ThinkingBlocks: cloneThinkingBlocks(msg.ThinkingBlocks),
			ToolUses:       cloneToolUses(msg.ToolUses),
			ToolResults:    cloneToolResults(msg.ToolResults),
		}
	}
	return out
}

func canMergeTextOnly(msg SerializedMessage) bool {
	return len(msg.ThinkingBlocks) == 0 && len(msg.ToolUses) == 0 && len(msg.ToolResults) == 0
}

func cloneMessage(msg Message) Message {
	return Message{
		Role:           msg.Role,
		Content:        msg.Content,
		ThinkingBlocks: cloneThinkingBlocks(msg.ThinkingBlocks),
		ToolUses:       cloneToolUses(msg.ToolUses),
		ToolResults:    cloneToolResults(msg.ToolResults),
	}
}

func cloneThinkingBlocks(blocks []ThinkingBlock) []ThinkingBlock {
	if blocks == nil {
		return nil
	}
	out := make([]ThinkingBlock, len(blocks))
	copy(out, blocks)
	return out
}

func cloneToolUses(blocks []ToolUseBlock) []ToolUseBlock {
	if blocks == nil {
		return nil
	}
	out := make([]ToolUseBlock, len(blocks))
	for i, block := range blocks {
		out[i] = block
		out[i].Arguments = cloneMap(block.Arguments)
	}
	return out
}

func cloneToolResults(blocks []ToolResultBlock) []ToolResultBlock {
	if blocks == nil {
		return nil
	}
	out := make([]ToolResultBlock, len(blocks))
	copy(out, blocks)
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
