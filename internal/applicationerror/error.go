// Package applicationerror classifies application-layer outcomes without coupling
// use cases to a transport protocol.
package applicationerror

import "errors"

// Code identifies a stable application-layer outcome.
type Code string

const (
	CodeInvalidRequest          Code = "invalid_request"
	CodeNotFound                Code = "not_found"
	CodeConflict                Code = "conflict"
	CodePlanningPending         Code = "planning_pending"
	CodePlanChanged             Code = "plan_changed"
	CodePlanDocumentUnavailable Code = "plan_document_unavailable"
	CodeUnavailable             Code = "unavailable"
)

// Error preserves the underlying error while attaching stable classification
// and retry guidance for adapters.
type Error struct {
	Code      Code
	Retryable bool
	Err       error
}

func (e *Error) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Wrap attaches application metadata while preserving errors.Is/errors.As.
func Wrap(code Code, retryable bool, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Code: code, Retryable: retryable, Err: err}
}

func CodeOf(err error) (Code, bool) {
	var classified *Error
	if !errors.As(err, &classified) || classified.Code == "" {
		return "", false
	}
	return classified.Code, true
}

func IsCode(err error, code Code) bool {
	actual, ok := CodeOf(err)
	return ok && actual == code
}

func IsRetryable(err error) bool {
	var classified *Error
	return errors.As(err, &classified) && classified.Retryable
}
