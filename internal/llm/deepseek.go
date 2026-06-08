package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/codemelo/mewcode/internal/config"
	"github.com/codemelo/mewcode/internal/conversation"
)

type deepSeekClient struct {
	provider     config.Provider
	systemPrompt string
	httpClient   *http.Client
}

type deepSeekMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type deepSeekRequest struct {
	Model         string            `json:"model"`
	Messages      []deepSeekMessage `json:"messages"`
	Stream        bool              `json:"stream"`
	MaxTokens     int               `json:"max_tokens,omitempty"`
	Tools         []map[string]any  `json:"tools,omitempty"`
	StreamOptions map[string]bool   `json:"stream_options,omitempty"`
}

type deepSeekChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func newDeepSeekClient(provider *config.Provider, systemPrompt string) *deepSeekClient {
	cfg := *provider
	if cfg.Model == "" {
		cfg.Model = "deepseek-chat"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.deepseek.com"
	}
	return &deepSeekClient{
		provider:     cfg,
		systemPrompt: systemPrompt,
		httpClient:   &http.Client{Timeout: 0},
	}
}

func (c *deepSeekClient) SetMaxTokens(maxTokens int) {
	c.provider.MaxTokens = maxTokens
}

func (c *deepSeekClient) Stream(ctx context.Context, conv *conversation.Manager, tools []map[string]any) (<-chan StreamEvent, <-chan error) {
	events := make(chan StreamEvent, 32)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)

		requestBody, err := c.buildRequest(conv, tools)
		if err != nil {
			errs <- err
			return
		}

		body, err := json.Marshal(requestBody)
		if err != nil {
			errs <- &LLMError{Message: "marshal deepseek request", Err: err}
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.provider.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			errs <- &LLMError{Message: "create deepseek request", Err: err}
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiKey())

		resp, err := c.httpClient.Do(req)
		if err != nil {
			errs <- &NetworkError{LLMError: LLMError{Message: "deepseek request failed", Err: err}}
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errs <- classifyDeepSeekHTTPError(resp)
			return
		}

		if err := readDeepSeekSSE(ctx, resp.Body, events); err != nil {
			errs <- err
		}
	}()

	return events, errs
}

func (c *deepSeekClient) buildRequest(conv *conversation.Manager, tools []map[string]any) (deepSeekRequest, error) {
	messages, err := buildDeepSeekMessages(conv, c.systemPrompt)
	if err != nil {
		return deepSeekRequest{}, err
	}
	req := deepSeekRequest{
		Model:         c.provider.Model,
		Messages:      messages,
		Stream:        true,
		Tools:         tools,
		StreamOptions: map[string]bool{"include_usage": true},
	}
	if c.provider.MaxTokens > 0 {
		req.MaxTokens = c.provider.MaxTokens
	}
	return req, nil
}

func (c *deepSeekClient) apiKey() string {
	if c.provider.APIKey != "" {
		return c.provider.APIKey
	}
	return os.Getenv("DEEPSEEK_API_KEY")
}

func buildDeepSeekMessages(conv *conversation.Manager, systemPrompt string) ([]deepSeekMessage, error) {
	if conv == nil {
		return nil, errors.New("conversation manager is nil")
	}
	serialized, err := conv.Serialize("openai")
	if err != nil {
		return nil, err
	}
	messages := make([]deepSeekMessage, 0, len(serialized)+1)
	if systemPrompt != "" {
		messages = append(messages, deepSeekMessage{Role: "system", Content: systemPrompt})
	}
	for _, msg := range serialized {
		content := msg.Content
		for _, result := range msg.ToolResults {
			content += result.Content
		}
		if content == "" {
			continue
		}
		messages = append(messages, deepSeekMessage{Role: msg.Role, Content: content})
	}
	return messages, nil
}

func readDeepSeekSSE(ctx context.Context, body io.Reader, events chan<- StreamEvent) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	var thinking strings.Builder
	var stopReason string
	var usage UsageInfo

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return &NetworkError{LLMError: LLMError{Message: "stream canceled", Err: ctx.Err()}}
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			if thinking.Len() > 0 {
				events <- ThinkingComplete{Text: thinking.String()}
			}
			if stopReason == "" {
				stopReason = "stop"
			}
			events <- StreamEnd{StopReason: stopReason, Usage: usage}
			return nil
		}

		var chunk deepSeekChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return &LLMError{Message: "decode deepseek stream chunk", Err: err}
		}
		if chunk.Usage != nil {
			usage = UsageInfo{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
				TotalTokens:  chunk.Usage.TotalTokens,
			}
		}
		for _, choice := range chunk.Choices {
			if choice.FinishReason != "" {
				stopReason = choice.FinishReason
			}
			if choice.Delta.ReasoningContent != "" {
				thinking.WriteString(choice.Delta.ReasoningContent)
				events <- ThinkingDelta{Text: choice.Delta.ReasoningContent}
			}
			if choice.Delta.Content != "" {
				events <- TextDelta{Text: choice.Delta.Content}
			}
			for _, toolCall := range choice.Delta.ToolCalls {
				if toolCall.ID != "" && toolCall.Function.Name != "" {
					events <- ToolCallStart{ID: toolCall.ID, Name: toolCall.Function.Name}
				}
				if toolCall.ID != "" && toolCall.Function.Arguments != "" {
					events <- ToolCallDelta{ID: toolCall.ID, Delta: toolCall.Function.Arguments}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return &NetworkError{LLMError: LLMError{Message: "read deepseek stream", Err: err}}
	}
	events <- StreamEnd{StopReason: "stop", Usage: usage}
	return nil
}

func classifyDeepSeekHTTPError(resp *http.Response) error {
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	message := strings.TrimSpace(string(payload))
	if message == "" {
		message = fmt.Sprintf("deepseek returned HTTP %d", resp.StatusCode)
	}
	return classifyOpenAIError(&httpStatusError{
		StatusCode: resp.StatusCode,
		Message:    message,
		Headers:    resp.Header,
	})
}

func (c *deepSeekClient) ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := c.buildRequest(conversation.NewManager(), nil)
	if err != nil {
		return err
	}
	req.Messages = []deepSeekMessage{{Role: "user", Content: "hello"}}
	_, _ = json.Marshal(req)
	return nil
}
