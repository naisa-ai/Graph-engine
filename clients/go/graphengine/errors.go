package graphengine

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Common errors returned by the client.
var (
	ErrNotFound          = errors.New("resource not found")
	ErrTimeout           = errors.New("operation timed out")
	ErrCanceled          = errors.New("operation canceled")
	ErrInvalidArgument   = errors.New("invalid argument")
	ErrResourceExhausted = errors.New("resource exhausted")
	ErrUnavailable       = errors.New("service unavailable")
	ErrInternal          = errors.New("internal error")
	ErrNotConnected      = errors.New("client not connected")
	ErrJobFailed         = errors.New("job failed")
)

// GraphEngineError wraps errors with additional context.
type GraphEngineError struct {
	Code    codes.Code
	Message string
	Cause   error
}

func (e *GraphEngineError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (cause: %v)", e.Code.String(), e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code.String(), e.Message)
}

func (e *GraphEngineError) Unwrap() error {
	return e.Cause
}

// Is implements errors.Is for GraphEngineError.
func (e *GraphEngineError) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.Code == codes.NotFound
	case ErrTimeout:
		return e.Code == codes.DeadlineExceeded
	case ErrCanceled:
		return e.Code == codes.Canceled
	case ErrInvalidArgument:
		return e.Code == codes.InvalidArgument
	case ErrResourceExhausted:
		return e.Code == codes.ResourceExhausted
	case ErrUnavailable:
		return e.Code == codes.Unavailable
	case ErrInternal:
		return e.Code == codes.Internal
	}
	return false
}

// wrapError wraps a gRPC error with additional context.
func wrapError(err error, operation string) error {
	if err == nil {
		return nil
	}

	st, ok := status.FromError(err)
	if !ok {
		return &GraphEngineError{
			Code:    codes.Unknown,
			Message: fmt.Sprintf("%s failed", operation),
			Cause:   err,
		}
	}

	return &GraphEngineError{
		Code:    st.Code(),
		Message: fmt.Sprintf("%s: %s", operation, st.Message()),
		Cause:   err,
	}
}

// IsRetryable returns true if the error is likely transient and can be retried.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	var ge *GraphEngineError
	if errors.As(err, &ge) {
		switch ge.Code {
		case codes.Unavailable, codes.ResourceExhausted, codes.Aborted, codes.DeadlineExceeded:
			return true
		}
		return false
	}

	st, ok := status.FromError(err)
	if ok {
		switch st.Code() {
		case codes.Unavailable, codes.ResourceExhausted, codes.Aborted, codes.DeadlineExceeded:
			return true
		}
	}

	return false
}

// IsNotFound returns true if the error indicates a resource was not found.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// IsTimeout returns true if the error indicates a timeout.
func IsTimeout(err error) bool {
	return errors.Is(err, ErrTimeout)
}

// IsCanceled returns true if the error indicates cancellation.
func IsCanceled(err error) bool {
	return errors.Is(err, ErrCanceled)
}

// IsResourceExhausted returns true if the error indicates resource exhaustion (quota).
func IsResourceExhausted(err error) bool {
	return errors.Is(err, ErrResourceExhausted)
}
