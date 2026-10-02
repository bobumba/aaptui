package aap

import (
	"fmt"
	"time"
)

type ErrorKind string

const (
	Connection     ErrorKind = "connection"
	Authentication ErrorKind = "authentication"
	Permission     ErrorKind = "permission"
	Malformed      ErrorKind = "malformed response"
	Missing        ErrorKind = "missing resource"
	Unsupported    ErrorKind = "unsupported operation"
	Temporary      ErrorKind = "temporary server failure"
	History        ErrorKind = "output history unavailable"
	Storage        ErrorKind = "output storage failure"
)

type APIError struct {
	Kind       ErrorKind
	Operation  string
	RetryAfter time.Duration
}

func (e *APIError) Error() string              { return fmt.Sprintf("%s: %s", e.Operation, e.Kind) }
func apiError(kind ErrorKind, op string) error { return &APIError{Kind: kind, Operation: op} }
