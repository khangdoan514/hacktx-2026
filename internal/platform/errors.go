package platform

import (
	"errors"
	"net/http"
	"strings"
)

type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func Fail(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func As(err error) *Error {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	if err == nil {
		return nil
	}
	text := err.Error()
	switch {
	case strings.Contains(text, "nmc_cross_scope"):
		return Fail(http.StatusConflict, "cross_scope", "The approval target is outside this scope")
	case strings.Contains(text, "nmc_self_approval"):
		return Fail(http.StatusForbidden, "self_approval", "You cannot approve your own submission")
	case strings.Contains(text, "nmc_conflict"):
		return Fail(http.StatusConflict, "conflict", "The record changed. Reload and try again.")
	case strings.Contains(text, "nmc_forbidden"):
		return Fail(http.StatusForbidden, "forbidden", "You cannot do that")
	case strings.Contains(text, "nmc_not_found"):
		return Fail(http.StatusNotFound, "not_found", "Record not found")
	case strings.Contains(text, "nmc_invalid_state"):
		return Fail(http.StatusConflict, "invalid_state", "That transition is not available")
	case strings.Contains(text, "nmc_invalid_target"):
		return Fail(http.StatusConflict, "invalid_target", "This approval target is not supported")
	default:
		return nil
	}
}
