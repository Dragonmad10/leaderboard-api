package domain

import (
	"errors"
	"fmt"
)

var (
	// ErrNotFound indicates a resource was not found (user, leaderboard, score)
	ErrNotFound = errors.New("resource not found")

	// ErrInvalidInput indicates invalid input data
	ErrInvalidInput = errors.New("invalid input")

	// ErrConflict indicates a conflict (e.g., duplicate resource)
	ErrConflict = errors.New("conflict")

	// ErrInternal indicates an internal server error
	ErrInternal = errors.New("internal server error")
)

// AppError wraps an error with an associated HTTP status code.
type AppError struct {
	Code    int    // HTTP status code
	Message string // User-facing error message
	Err     error  // Underlying error for debugging
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *AppError) Unwrap() error {
	return e.Err
}

// NewNotFoundError creates a 404 Not Found error.
func NewNotFoundError(message string, err error) *AppError {
	return &AppError{Code: 404, Message: message, Err: err}
}

// NewInvalidInputError creates a 400 Bad Request error.
func NewInvalidInputError(message string, err error) *AppError {
	return &AppError{Code: 400, Message: message, Err: err}
}

// NewConflictError creates a 409 Conflict error.
func NewConflictError(message string, err error) *AppError {
	return &AppError{Code: 409, Message: message, Err: err}
}

// NewInternalError creates a 500 Internal Server Error.
func NewInternalError(message string, err error) *AppError {
	return &AppError{Code: 500, Message: message, Err: err}
}

// GetErrorCode extracts the HTTP status code from an error.
// Returns 500 if the error is not an AppError.
func GetErrorCode(err error) int {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return 500
}

// GetErrorMessage extracts the user-facing message from an error.
// Returns "Internal server error" if the error is not an AppError.
func GetErrorMessage(err error) string {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Message
	}
	return "Internal server error"
}