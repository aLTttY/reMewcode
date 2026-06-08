package llm

import (
	"context"
	"errors"
	"net/http"
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
	return &anthropicClient{
		provider:     *provider,
		systemPrompt: systemPrompt,
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
		go readNext(ctx, nextCh, readErrCh, request)

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

func readNext(ctx context.Context, nextCh chan<- StreamEvent, errCh chan<- error, request anthropicRequest) {
	defer close(nextCh)
	defer close(errCh)

	select {
	case <-ctx.Done():
		errCh <- ctx.Err()
		return
	default:
	}

	if request.Thinking != nil {
		nextCh <- ThinkingDelta{Text: "Thinking enabled."}
		nextCh <- ThinkingComplete{Text: "Thinking enabled.", Signature: "local-signature"}
	}
	nextCh <- TextDelta{Text: ""}
	nextCh <- StreamEnd{StopReason: "end_turn", Usage: UsageInfo{}}
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
