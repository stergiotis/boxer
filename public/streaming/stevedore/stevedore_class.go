package stevedore

import (
	"errors"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// ClassE says whether an error is worth retrying (ADR-0252 §SD3).
type ClassE uint8

const (
	// ClassTransient is an error a later attempt may not hit: a timeout, a
	// 5xx, a broker or store hiccup. An error nobody classified is transient.
	ClassTransient ClassE = iota
	// ClassPermanent is an error no attempt will cure: a 4xx, a body the
	// handler refuses, a request that does not parse.
	ClassPermanent
)

// AllClasses lists every class, in declaration order.
var AllClasses = []ClassE{ClassTransient, ClassPermanent}

// String is the class as a status prefix spells it.
func (inst ClassE) String() string {
	switch inst {
	case ClassTransient:
		return "transient"
	case ClassPermanent:
		return "permanent"
	default:
		return "unknown"
	}
}

// classified is the wrapper the two constructors attach.
type classified struct {
	class ClassE
	err   error
}

func (inst *classified) Error() string { return inst.err.Error() }
func (inst *classified) Unwrap() error { return inst.err }

// Permanent marks err as not worth retrying. A nil err stays nil.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &classified{class: ClassPermanent, err: err}
}

// Transient marks err as worth retrying — the default for an unclassified
// error, so this is for stating it where the reader would otherwise wonder.
// A nil err stays nil.
func Transient(err error) error {
	if err == nil {
		return nil
	}
	return &classified{class: ClassTransient, err: err}
}

// Permanentf builds a permanent error from a message; %w is the only
// directive the house allows.
func Permanentf(format string, args ...any) error {
	return Permanent(eh.Errorf(format, args...))
}

// ClassOf reads the innermost classification an error carries, or transient
// when it carries none. The innermost wins because the code nearest the
// failure knows it best; an outer wrapper restating the class is harmless.
func ClassOf(err error) (class ClassE) {
	class = ClassTransient
	for err != nil {
		var c *classified
		if errors.As(err, &c) {
			class = c.class
			err = c.err
			continue
		}
		return
	}
	return
}

// StatusText renders err as the one-line status a framework routes on:
// `<class>: <message>`, newlines folded to spaces so the line stays one
// line under every codec. Nil renders as the empty status, success.
func StatusText(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(err.Error())
	return ClassOf(err).String() + ": " + msg
}

// ParseStatus reads a status a host wrote. An empty status is success. A
// status without a known class prefix is reported as it is, transient, so a
// framework-side reader never mistakes an unknown shape for success.
func ParseStatus(status string) (class ClassE, message string, ok bool) {
	if status == "" {
		return ClassTransient, "", true
	}
	for _, c := range AllClasses {
		prefix := c.String() + ": "
		if strings.HasPrefix(status, prefix) {
			return c, status[len(prefix):], false
		}
	}
	return ClassTransient, status, false
}
