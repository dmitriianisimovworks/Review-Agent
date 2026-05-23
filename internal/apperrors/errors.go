package apperrors

import (
	"errors"
	"fmt"
)

type Kind string

const (
	KindInvalidArgument Kind = "invalid_argument"
	KindNotFound        Kind = "not_found"
	KindConflict        Kind = "conflict"
	KindDependency      Kind = "dependency_failure"
	KindInternal        Kind = "internal"
)

type Error struct {
	Kind    Kind
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return e.Message
	}
	if e.Message == "" {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Err)
}

func (e *Error) Unwrap() error {
	return e.Err
}

func New(kind Kind, message string) error {
	return &Error{
		Kind:    kind,
		Message: message,
	}
}

func Wrap(kind Kind, message string, err error) error {
	return &Error{
		Kind:    kind,
		Message: message,
		Err:     err,
	}
}

func KindOf(err error) Kind {
	var target *Error
	if errors.As(err, &target) {
		return target.Kind
	}
	return KindInternal
}

func PublicMessage(err error) string {
	var target *Error
	if errors.As(err, &target) && target.Message != "" {
		return target.Message
	}
	return "internal server error"
}
