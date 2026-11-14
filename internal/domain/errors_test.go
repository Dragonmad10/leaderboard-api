package domain

import (
	"errors"
	"fmt"
	"testing"
)

// The handlers use GetErrorCode to decide which HTTP status to send,
// so each kind of error must map to the right code.
func TestGetErrorCode(t *testing.T) {
	cause := errors.New("something low level")

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"not found", NewNotFoundError("nope", cause), 404},
		{"invalid input", NewInvalidInputError("bad", cause), 400},
		{"conflict", NewConflictError("dup", cause), 409},
		{"internal", NewInternalError("boom", cause), 500},
		{"plain error that is not an AppError", cause, 500},
		// Code higher up may add context with fmt.Errorf("...: %w", err).
		// The status code must survive that wrapping.
		{"AppError wrapped by another error", fmt.Errorf("context: %w", NewNotFoundError("nope", cause)), 404},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := GetErrorCode(tc.err); got != tc.want {
				t.Errorf("GetErrorCode() = %d; want %d", got, tc.want)
			}
		})
	}
}

func TestGetErrorMessage(t *testing.T) {
	t.Run("AppError returns its user-facing message", func(t *testing.T) {
		err := NewNotFoundError("user not found in leaderboard", errors.New("sql: no rows"))
		if got := GetErrorMessage(err); got != "user not found in leaderboard" {
			t.Errorf("GetErrorMessage() = %q; want the user-facing message", got)
		}
	})

	t.Run("unknown error does not leak its details", func(t *testing.T) {
		err := errors.New("dial tcp 10.0.0.5:5432: connection refused")
		if got := GetErrorMessage(err); got != "Internal server error" {
			t.Errorf("GetErrorMessage() = %q; want the generic message", got)
		}
	})
}

// Error() is what ends up in the logs, so it should include the cause.
func TestAppError_Error(t *testing.T) {
	withCause := NewInternalError("database error", errors.New("connection refused"))
	if got, want := withCause.Error(), "database error: connection refused"; got != want {
		t.Errorf("Error() = %q; want %q", got, want)
	}

	withoutCause := NewInvalidInputError("score must be positive", nil)
	if got, want := withoutCause.Error(), "score must be positive"; got != want {
		t.Errorf("Error() = %q; want %q", got, want)
	}
}

// Unwrap lets errors.Is / errors.As see the original error inside an AppError.
func TestAppError_Unwrap(t *testing.T) {
	cause := errors.New("original")
	err := NewInternalError("wrapped", cause)

	if !errors.Is(err, cause) {
		t.Error("errors.Is could not find the original error inside the AppError")
	}
}
