package llm

type StreamEvent interface {
	streamEvent()
}

type UsageInfo struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

type TextDelta struct {
	Text string
}

type ThinkingDelta struct {
	Text string
}

type ThinkingComplete struct {
	Text      string
	Signature string
}

type ToolCallStart struct {
	ID   string
	Name string
}

type ToolCallDelta struct {
	ID    string
	Delta string
}

type ToolCallComplete struct {
	ID        string
	Name      string
	Arguments map[string]any
}

type StreamEnd struct {
	StopReason string
	Usage      UsageInfo
}

func (TextDelta) streamEvent()        {}
func (ThinkingDelta) streamEvent()    {}
func (ThinkingComplete) streamEvent() {}
func (ToolCallStart) streamEvent()    {}
func (ToolCallDelta) streamEvent()    {}
func (ToolCallComplete) streamEvent() {}
func (StreamEnd) streamEvent()        {}
