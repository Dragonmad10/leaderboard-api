package domain

import (
	"errors"
	"fmt"
)

var (
	// ErrNotFound indica que un recurso no existe (usuario, leaderboard, score)
	ErrNotFound = errors.New("resource not found")

	// ErrInvalidInput indica datos de entrada inválidos
	ErrInvalidInput = errors.New("invalid input")

	// ErrConflict indica un conflicto (ej. duplicado)
	ErrConflict = errors.New("conflict")

	// ErrInternal indica un error interno del servidor
	ErrInternal = errors.New("internal server error")
)

// AppError es un error con código HTTP asociado
type AppError struct {
	Code    int
	Message string
	Err     error
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

// NewNotFoundError crea un error 404
func NewNotFoundError(message string, err error) *AppError {
	return &AppError{Code: 404, Message: message, Err: err}
}

// NewInvalidInputError crea un error 400
func NewInvalidInputError(message string, err error) *AppError {
	return &AppError{Code: 400, Message: message, Err: err}
}

// NewConflictError crea un error 409
func NewConflictError(message string, err error) *AppError {
	return &AppError{Code: 409, Message: message, Err: err}
}

// NewInternalError crea un error 500
func NewInternalError(message string, err error) *AppError {
	return &AppError{Code: 500, Message: message, Err: err}
}

// GetErrorCode extrae el código HTTP de un error
func GetErrorCode(err error) int {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return 500
}

// GetErrorMessage extrae el mensaje de un error
func GetErrorMessage(err error) string {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Message
	}
	return "Internal server error"
}