package worker

import "errors"

type permanentError struct {
	cause error
}

func (e *permanentError) Error() string {
	if e == nil || e.cause == nil {
		return "permanent worker error"
	}
	return e.cause.Error()
}

func (e *permanentError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{cause: err}
}

func IsPermanent(err error) bool {
	var permanent *permanentError
	return errors.As(err, &permanent)
}
