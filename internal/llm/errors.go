package llm

import (
	"fmt"
	"time"
)

type LLMError struct {
	Message string
	Err     error
}

func (e *LLMError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *LLMError) Unwrap() error {
	return e.Err
}

type AuthenticationError struct {
	LLMError
}

type RateLimitError struct {
	LLMError
	RetryAfter time.Duration
}

type NetworkError struct {
	LLMError
}

type ContextTooLongError struct {
	LLMError
}
