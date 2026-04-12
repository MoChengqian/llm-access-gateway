package provider

import (
	"context"
	"errors"
	"time"
)

type AttemptBeginner interface {
	BeginAttempt(ctx context.Context, metadata AttemptMetadata) (AttemptHandle, error)
}

type AttemptHandle interface {
	Complete(ctx context.Context, result AttemptResult) error
}

type AttemptMetadata struct {
	Backend      string
	Model        string
	Stream       bool
	PromptTokens int
	CreatedAt    time.Time
}

type AttemptResult struct {
	Model            string
	Status           string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

type attemptAccountingError struct {
	cause error
}

func (e attemptAccountingError) Error() string {
	return e.cause.Error()
}

func (e attemptAccountingError) Unwrap() error {
	return e.cause
}

func WrapAttemptAccountingError(err error) error {
	if err == nil {
		return nil
	}
	return attemptAccountingError{cause: err}
}

func IsAttemptAccountingError(err error) bool {
	var target attemptAccountingError
	return errors.As(err, &target)
}

type attemptRecorderContextKey struct{}
type attemptBackendContextKey struct{}

func WithAttemptRecorder(ctx context.Context, beginner AttemptBeginner) context.Context {
	if beginner == nil {
		return ctx
	}
	return context.WithValue(ctx, attemptRecorderContextKey{}, beginner)
}

func AttemptRecorderFromContext(ctx context.Context) AttemptBeginner {
	beginner, _ := ctx.Value(attemptRecorderContextKey{}).(AttemptBeginner)
	return beginner
}

func WithAttemptBackend(ctx context.Context, backend string) context.Context {
	if backend == "" {
		return ctx
	}
	return context.WithValue(ctx, attemptBackendContextKey{}, backend)
}

func AttemptBackendFromContext(ctx context.Context) string {
	backend, _ := ctx.Value(attemptBackendContextKey{}).(string)
	return backend
}
