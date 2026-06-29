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
	"strconv"
	"strings"
	"time"

	"github.com/codemelo/mewcode/internal/config"
	"github.com/codemelo/mewcode/internal/conversation"
)

const defaultIdleTimeout = 5 * time.Minute

type anthropicClient struct {
	provider     config.Provider
	systemPrompt string
	httpClient   *http.Client
	stream       bool
	idleTimeout  time.Duration
}

type anthropicThinkingParam struct {
	Type         string
	BudgetTokens int
}

type anthropicRequest struct {
	Model     string
	System    string
	MaxTokens int
	Thinking  *anthropicThinkingParam
	Messages  []conversation.SerializedMessage
	Tools     []map[string]any
}

type anthropicAPIRequest struct {
	Model     string                 `json:"model"`
	MaxTokens int                    `json:"max_tokens"`
	System    string                 `json:"system,omitempty"`
	Messages  []anthropicAPIMessage  `json:"messages"`
	Stream    bool                   `json:"stream,omitempty"`
	Thinking  *anthropicThinkingBody `json:"thinking,omitempty"`
	Tools     []map[string]any       `json:"tools,omitempty"`
}

type anthropicThinkingBody struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type anthropicAPIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicStreamPayload struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock *struct {
		Type  string         `json:"type"`
		ID    string         `json:"id"`
		Name  string         `json:"name"`
		Input map[string]any `json:"input"`
	} `json:"content_block"`
	Delta *struct {
		Type         string `json:"type"`
		Text         string `json:"text"`
		Thinking     string `json:"thinking"`
		Signature    string `json:"signature"`
		PartialJSON  string `json:"partial_json"`
		StopReason   string `json:"stop_reason"`
		StopSequence string `json:"stop_sequence"`
	} `json:"delta"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Message *struct {
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type anthropicAPIResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type httpStatusError struct {
	StatusCode int
	Message    string
	Headers    http.Header
}

func (e *httpStatusError) Error() string {
	return e.Message
}

func supportsAdaptiveThinking(model string) bool {
	for _, prefix := range []string{"claude-opus-4-", "claude-sonnet-4-"} {
		if !strings.HasPrefix(model, prefix) {
			continue
		}
		rest := strings.TrimPrefix(model, prefix)
		minor := ""
		for _, r := range rest {
			if r < '0' || r > '9' {
				break
			}
			minor += string(r)
		}
		value, err := strconv.Atoi(minor)
		return err == nil && value >= 6
	}
	return false
}

func newAnthropicClient(provider *config.Provider, systemPrompt string) *anthropicClient {
	cfg := *provider
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.anthropic.com"
	}
	return &anthropicClient{
		provider:     cfg,
		systemPrompt: systemPrompt,
		httpClient:   &http.Client{Timeout: 0},
		stream:       envBool("STONEAI_STREAM", false),
		idleTimeout:  defaultIdleTimeout,
	}
}

func (c *anthropicClient) SetMaxTokens(maxTokens int) {
	c.provider.MaxTokens = maxTokens
}

func (c *anthropicClient) Stream(ctx context.Context, conv *conversation.Manager, tools []map[string]any) (<-chan StreamEvent, <-chan error) {
	events := make(chan StreamEvent, 32)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)

		request, err := c.buildRequest(conv, tools)
		if err != nil {
			errs <- err
			return
		}

		nextCh := make(chan StreamEvent, 8)
		readErrCh := make(chan error, 1)
		go c.readNext(ctx, nextCh, readErrCh, request)

		idle := time.NewTimer(c.idleTimeout)
		defer idle.Stop()

		for {
			select {
			case <-ctx.Done():
				errs <- &NetworkError{LLMError: LLMError{Message: "stream canceled", Err: ctx.Err()}}
				return
			case <-idle.C:
				errs <- &NetworkError{LLMError: LLMError{Message: "stream idle timeout"}}
				return
			case err, ok := <-readErrCh:
				if !ok {
					readErrCh = nil
					continue
				}
				if err != nil {
					errs <- classifyAnthropicError(err)
					return
				}
			case event, ok := <-nextCh:
				if !ok {
					return
				}
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(c.idleTimeout)
				events <- event
				if _, ok := event.(StreamEnd); ok {
					return
				}
			}
		}
	}()

	return events, errs
}

func (c *anthropicClient) readNext(ctx context.Context, nextCh chan<- StreamEvent, errCh chan<- error, request anthropicRequest) {
	defer close(nextCh)
	defer close(errCh)

	body, err := json.Marshal(toAnthropicAPIRequest(request, c.stream))
	if err != nil {
		errCh <- &LLMError{Message: "marshal anthropic request", Err: err}
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.messagesURL(), bytes.NewReader(body))
	if err != nil {
		errCh <- &LLMError{Message: "create anthropic request", Err: err}
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey())
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		errCh <- &NetworkError{LLMError: LLMError{Message: "anthropic request failed", Err: err}}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errCh <- anthropicHTTPError(resp)
		return
	}
	if c.stream {
		err = readAnthropicSSE(ctx, resp.Body, nextCh)
	} else {
		err = readAnthropicJSON(ctx, resp.Body, nextCh)
	}
	if err != nil {
		errCh <- err
	}
}

func (c *anthropicClient) messagesURL() string {
	base := strings.TrimRight(c.provider.BaseURL, "/")
	if strings.HasSuffix(base, "/v1/messages") {
		return base
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/messages"
	}
	return base + "/v1/messages"
}

func (c *anthropicClient) apiKey() string {
	if c.provider.APIKey != "" {
		return c.provider.APIKey
	}
	if value := os.Getenv("STONEAI_API_KEY"); value != "" {
		return value
	}
	return os.Getenv("ANTHROPIC_API_KEY")
}

func (c *anthropicClient) buildRequest(conv *conversation.Manager, tools []map[string]any) (anthropicRequest, error) {
	messages, err := buildAnthropicMessages(conv)
	if err != nil {
		return anthropicRequest{}, err
	}
	maxTokens := c.provider.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	request := anthropicRequest{
		Model:     c.provider.Model,
		System:    c.systemPrompt,
		MaxTokens: maxTokens,
		Messages:  messages,
		Tools:     tools,
	}
	if c.provider.Thinking {
		if supportsAdaptiveThinking(c.provider.Model) {
			request.Thinking = &anthropicThinkingParam{Type: "adaptive"}
		} else {
			budget := maxTokens - 1
			if budget < 1 {
				budget = 1
			}
			request.Thinking = &anthropicThinkingParam{Type: "enabled", BudgetTokens: budget}
		}
	}
	return request, nil
}

func buildAnthropicMessages(conv *conversation.Manager) ([]conversation.SerializedMessage, error) {
	if conv == nil {
		return nil, errors.New("conversation manager is nil")
	}
	return conv.Serialize("anthropic")
}

func toAnthropicAPIRequest(request anthropicRequest, stream bool) anthropicAPIRequest {
	messages := make([]anthropicAPIMessage, 0, len(request.Messages))
	for _, message := range request.Messages {
		content := message.Content
		for _, result := range message.ToolResults {
			content += result.Content
		}
		if content == "" {
			continue
		}
		messages = append(messages, anthropicAPIMessage{
			Role:    message.Role,
			Content: content,
		})
	}

	out := anthropicAPIRequest{
		Model:     request.Model,
		MaxTokens: request.MaxTokens,
		System:    request.System,
		Messages:  messages,
		Stream:    stream,
		Tools:     request.Tools,
	}
	if request.Thinking != nil {
		out.Thinking = &anthropicThinkingBody{
			Type:         request.Thinking.Type,
			BudgetTokens: request.Thinking.BudgetTokens,
		}
	}
	return out
}

func readAnthropicJSON(ctx context.Context, body io.Reader, events chan<- StreamEvent) error {
	select {
	case <-ctx.Done():
		return &NetworkError{LLMError: LLMError{Message: "request canceled", Err: ctx.Err()}}
	default:
	}

	var response anthropicAPIResponse
	if err := json.NewDecoder(body).Decode(&response); err != nil {
		return &LLMError{Message: "decode anthropic response", Err: err}
	}
	if response.Error != nil {
		return &LLMError{Message: response.Error.Message}
	}
	for _, block := range response.Content {
		if block.Type == "text" && block.Text != "" {
			events <- TextDelta{Text: block.Text}
		}
	}
	usage := UsageInfo{}
	if response.Usage != nil {
		usage.InputTokens = response.Usage.InputTokens
		usage.OutputTokens = response.Usage.OutputTokens
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	stopReason := response.StopReason
	if stopReason == "" {
		stopReason = "end_turn"
	}
	events <- StreamEnd{StopReason: stopReason, Usage: usage}
	return nil
}

func readAnthropicSSE(ctx context.Context, body io.Reader, events chan<- StreamEvent) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	var stopReason string
	var usage UsageInfo
	toolIDs := map[int]string{}
	toolNames := map[int]string{}
	toolInputs := map[int]string{}

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
			if stopReason == "" {
				stopReason = "end_turn"
			}
			events <- StreamEnd{StopReason: stopReason, Usage: usage}
			return nil
		}

		var payload anthropicStreamPayload
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return &LLMError{Message: "decode anthropic stream chunk", Err: err}
		}
		if payload.Error != nil {
			return &LLMError{Message: payload.Error.Message}
		}
		if payload.Message != nil && payload.Message.Usage != nil {
			usage.InputTokens = payload.Message.Usage.InputTokens
			usage.OutputTokens = payload.Message.Usage.OutputTokens
			usage.TotalTokens = usage.InputTokens + usage.OutputTokens
		}
		if payload.Usage != nil {
			if payload.Usage.InputTokens > 0 {
				usage.InputTokens = payload.Usage.InputTokens
			}
			if payload.Usage.OutputTokens > 0 {
				usage.OutputTokens = payload.Usage.OutputTokens
			}
			usage.TotalTokens = usage.InputTokens + usage.OutputTokens
		}

		switch payload.Type {
		case "content_block_start":
			if payload.ContentBlock == nil || payload.ContentBlock.Type != "tool_use" {
				continue
			}
			toolIDs[payload.Index] = payload.ContentBlock.ID
			toolNames[payload.Index] = payload.ContentBlock.Name
			events <- ToolCallStart{ID: payload.ContentBlock.ID, Name: payload.ContentBlock.Name}
			if len(payload.ContentBlock.Input) > 0 {
				events <- ToolCallComplete{
					ID:        payload.ContentBlock.ID,
					Name:      payload.ContentBlock.Name,
					Arguments: payload.ContentBlock.Input,
				}
			}
		case "content_block_delta":
			if payload.Delta == nil {
				continue
			}
			switch payload.Delta.Type {
			case "text_delta":
				events <- TextDelta{Text: payload.Delta.Text}
			case "thinking_delta":
				events <- ThinkingDelta{Text: payload.Delta.Thinking}
			case "signature_delta":
				events <- ThinkingComplete{Signature: payload.Delta.Signature}
			case "input_json_delta":
				id := toolIDs[payload.Index]
				toolInputs[payload.Index] += payload.Delta.PartialJSON
				events <- ToolCallDelta{ID: id, Delta: payload.Delta.PartialJSON}
			}
		case "content_block_stop":
			id := toolIDs[payload.Index]
			if id == "" || toolInputs[payload.Index] == "" {
				continue
			}
			var args map[string]any
			if err := json.Unmarshal([]byte(toolInputs[payload.Index]), &args); err != nil {
				return &LLMError{Message: "decode anthropic tool arguments", Err: err}
			}
			events <- ToolCallComplete{ID: id, Name: toolNames[payload.Index], Arguments: args}
		case "message_delta":
			if payload.Delta != nil && payload.Delta.StopReason != "" {
				stopReason = payload.Delta.StopReason
			}
		case "message_stop":
			if stopReason == "" {
				stopReason = "end_turn"
			}
			events <- StreamEnd{StopReason: stopReason, Usage: usage}
			return nil
		}
	}

	if err := scanner.Err(); err != nil {
		return &NetworkError{LLMError: LLMError{Message: "read anthropic stream", Err: err}}
	}
	events <- StreamEnd{StopReason: "end_turn", Usage: usage}
	return nil
}

func anthropicHTTPError(resp *http.Response) error {
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	message := strings.TrimSpace(string(payload))
	if message == "" {
		message = fmt.Sprintf("anthropic returned HTTP %d", resp.StatusCode)
	}
	appendMewcodeLog("anthropic HTTP error: url=%s status=%d body=%s", resp.Request.URL.String(), resp.StatusCode, message)
	return &httpStatusError{
		StatusCode: resp.StatusCode,
		Message:    message,
		Headers:    resp.Header,
	}
}

func classifyAnthropicError(err error) error {
	if err == nil {
		return nil
	}
	var httpErr *httpStatusError
	if errors.As(err, &httpErr) {
		message := httpErr.Message
		lower := strings.ToLower(message)
		switch {
		case httpErr.StatusCode == http.StatusRequestEntityTooLarge || strings.Contains(lower, "prompt is too long"):
			return &ContextTooLongError{LLMError: LLMError{Message: message, Err: err}}
		case httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden:
			return &AuthenticationError{LLMError: LLMError{Message: "Invalid API key: " + message, Err: err}}
		case httpErr.StatusCode == http.StatusTooManyRequests:
			return &RateLimitError{
				LLMError:   LLMError{Message: message, Err: err},
				RetryAfter: parseRetryAfter(httpErr.Headers.Get("Retry-After")),
			}
		}
	}
	if httpErr != nil {
		return &LLMError{Message: fmt.Sprintf("anthropic HTTP %d: %s", httpErr.StatusCode, httpErr.Message)}
	}
	var networkErr *NetworkError
	if errors.As(err, &networkErr) {
		return err
	}
	var llmErr *LLMError
	if errors.As(err, &llmErr) {
		return err
	}
	return &LLMError{Message: err.Error(), Err: err}
}

func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	seconds, err := strconv.Atoi(value)
	if err == nil {
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	return time.Until(when)
}

func envBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func envFloat(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func appendMewcodeLog(format string, args ...any) {
	file, err := os.OpenFile(".mewcode.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintf(file, "%s %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}
