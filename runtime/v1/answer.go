package runtime_v1

import (
	"github.com/dejitarudemon/ignicula-wire/v1/body"
	"github.com/dejitarudemon/ignicula-wire/v1/fields"
	"github.com/dejitarudemon/ignicula-framework/runtime/errs"
	"github.com/dejitarudemon/ignicula-framework/runtime/v1/answer"
)

// handleAnswer accepts a ping answer and ignores every other answer.
//
// A ping answer returns nil. Any other answer returns [errs.ErrLogAndIgnore]:
// the caller logs the error, writes nothing, and keeps the connection.
func (r Runtime) handleAnswer(b body.Body) error {
	a, ok := b.(body.Answer)
	if !ok {
		return errs.LogAndIgnore(answer.NewErrorUnexpectedAnswer(b.Command()))
	}

	if a.IsResponseTo() != fields.Ping {
		return errs.LogAndIgnore(answer.NewErrorUnexpectedAnswer(a.IsResponseTo()))
	}

	return nil
}
