package errs

import "errors"

// ErrLogAndIgnore is returned by a runtime Handle when the frame was fully
// read and the connection must stay open. The caller writes nothing, logs the
// error, and keeps reading frames. Unwrap the error for the cause.
var ErrLogAndIgnore = errors.New("log and ignore")

// logAndIgnoreError marks cause as a failure that must be logged without
// closing the connection or writing an answer.
type logAndIgnoreError struct {
	cause error
}

// LogAndIgnore wraps cause so errors.Is(err, ErrLogAndIgnore) reports that
// the caller must log the error and keep the connection.
func LogAndIgnore(cause error) error {
	return logAndIgnoreError{cause: cause}
}

func (e logAndIgnoreError) Error() string {
	if e.cause == nil {
		return ErrLogAndIgnore.Error()
	}

	return ErrLogAndIgnore.Error() + ": " + e.cause.Error()
}

func (e logAndIgnoreError) Unwrap() error {
	return e.cause
}

func (e logAndIgnoreError) Is(target error) bool {
	return target == ErrLogAndIgnore
}
