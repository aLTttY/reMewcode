package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/codemelo/mewcode/internal/config"
	"github.com/codemelo/mewcode/internal/conversation"
)

type openaiClient struct {
	provider     config.Provider
	systemPrompt string
	idleTimeout  time.Duration
}

type openAIReasoningParam struct {
	Effort  string
	Summary string
}

type openAIRequest struct {
	Model     string
	System    string
	MaxTokens int
	Reasoning *openAIReasoningParam
	Include   []string
	Input     []conversation.SerializedMessage
	Tools     []map[string]any
}

func newOpenAIClient(provider *config.Provider, systemPrompt string) *openaiClient {
	return &openaiClient{
		provider:     *provider,
		systemPrompt: systemPrompt,
		idleTimeout:  defaultIdleTimeout,
	}
}

func (c *openaiClient) SetMaxTokens(maxTokens int) {
	c.provider.MaxTokens = maxTokens
}

func (c *openaiClient) Stream(ctx context.Context, conv *conversation.Manager, tools []map[string]any) (<-chan StreamEvent, <-chan error) {
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
		go readOpenAINext(ctx, nextCh, readErrCh, request)

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
					errs <- classifyOpenAIError(err)
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

func readOpenAINext(ctx context.Context, nextCh chan<- StreamEvent, errCh chan<- error, request openAIRequest) {
	defer close(nextCh)
	defer close(errCh)

	select {
	case <-ctx.Done():
		errCh <- ctx.Err()
		return
	default:
	}

	if request.Reasoning != nil {
		nextCh <- ThinkingDelta{Text: "Reasoning enabled."}
		nextCh <- ThinkingComplete{Text: "Reasoning enabled.", Signature: "encrypted-local-reasoning"}
	}
	nextCh <- TextDelta{Text: ""}
	nextCh <- StreamEnd{StopReason: "completed", Usage: UsageInfo{}}
}

func (c *openaiClient) buildRequest(conv *conversation.Manager, tools []map[string]any) (openAIRequest, error) {
	input, err := buildOpenAIInput(conv)
	if err != nil {
		return openAIRequest{}, err
	}
	maxTokens := c.provider.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	request := openAIRequest{
		Model:     c.provider.Model,
		System:    c.systemPrompt,
		MaxTokens: maxTokens,
		Input:     input,
		Tools:     tools,
	}
	if c.provider.Thinking {
		request.Reasoning = &openAIReasoningParam{Effort: "high", Summary: "detailed"}
		request.Include = []string{"reasoning.encrypted_content"}
	}
	return request, nil
}

func buildOpenAIInput(conv *conversation.Manager) ([]conversation.SerializedMessage, error) {
	if conv == nil {
		return nil, errors.New("conversation manager is nil")
	}
	return conv.Serialize("openai")
}

func classifyOpenAIError(err error) error {
	if err == nil {
		return nil
	}
	var httpErr *httpStatusError
	if errors.As(err, &httpErr) {
		message := httpErr.Message
		if httpErr.StatusCode == http.StatusRequestEntityTooLarge ||
			(httpErr.StatusCode == http.StatusBadRequest && containsContextLengthError(message)) {
			return &ContextTooLongError{LLMError: LLMError{Message: message, Err: err}}
		}
		if httpErr.StatusCode == http.StatusUnauthorized {
			return &AuthenticationError{LLMError: LLMError{Message: "Invalid API key: " + message, Err: err}}
		}
		if httpErr.StatusCode == http.StatusTooManyRequests {
			return &RateLimitError{
				LLMError:   LLMError{Message: message, Err: err},
				RetryAfter: parseRetryAfter(httpErr.Headers.Get("Retry-After")),
			}
		}
		return &LLMError{Message: fmt.Sprintf("HTTP %d %s: %s", httpErr.StatusCode, http.StatusText(httpErr.StatusCode), message)}
	}
	return &LLMError{Message: err.Error(), Err: err}
}

func containsContextLengthError(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "context_length_exceeded") ||
		strings.Contains(lower, "context length") ||
		strings.Contains(lower, "maximum context")
}
