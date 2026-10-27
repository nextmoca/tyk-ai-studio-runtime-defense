package needlepath

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
)

// ErrorClass names why a selection call failed. Classes are content-free and
// safe to log in every configuration.
type ErrorClass string

const (
	ClassAuth        ErrorClass = "auth"
	ClassQuota       ErrorClass = "quota"
	ClassRateLimit   ErrorClass = "rate_limit"
	ClassOversize    ErrorClass = "oversize"
	ClassTimeout     ErrorClass = "timeout"
	ClassTransport   ErrorClass = "transport"
	ClassServer      ErrorClass = "server"
	ClassContract    ErrorClass = "contract"
	ClassRequest     ErrorClass = "request"
	ClassCircuitOpen ErrorClass = "circuit_open"
	ClassCanceled    ErrorClass = "canceled"
	ClassUnknown     ErrorClass = "unknown"
)

// SelectionError is returned by Client.Select for every failure. Its Error
// text is for the caller; log only Class, which never carries request or
// response content.
type SelectionError struct {
	Class ErrorClass
	Cause error
}

func (e *SelectionError) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("Needlepath selection failed: %s", e.Class)
	}
	return fmt.Sprintf("Needlepath selection failed: %s: %v", e.Class, e.Cause)
}

func (e *SelectionError) Unwrap() error {
	return e.Cause
}

// ClassOf returns the failure class of err, or ClassUnknown for an error that
// did not come from Client.Select.
func ClassOf(err error) ErrorClass {
	var selectionError *SelectionError
	if errors.As(err, &selectionError) {
		return selectionError.Class
	}
	return ClassUnknown
}

func newError(class ErrorClass, cause error) *SelectionError {
	return &SelectionError{Class: class, Cause: cause}
}

func classForStatus(status int) ErrorClass {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return ClassAuth
	case status == http.StatusPaymentRequired:
		return ClassQuota
	case status == http.StatusTooManyRequests:
		return ClassRateLimit
	case status == http.StatusRequestEntityTooLarge:
		return ClassOversize
	case status >= http.StatusInternalServerError:
		return ClassServer
	default:
		return ClassContract
	}
}

func classForTransport(err error) ErrorClass {
	if errors.Is(err, context.DeadlineExceeded) {
		return ClassTimeout
	}
	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return ClassTimeout
	}
	if errors.Is(err, context.Canceled) {
		return ClassCanceled
	}
	return ClassTransport
}
