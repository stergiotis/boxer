// Package inprocbus is the in-process subject-pattern router that ADR-0026
// §SD5 ships in M2 as the pre-NATS transport for app.BusI. NATS-shaped
// semantics: subjects are dotted token sequences; pattern wildcards match
// either one token ('*') or all remaining tokens ('>'). The router is
// goroutine-safe; each app receives a permissioned client wrapper from
// Inst.NewClient that gates Publish and Subscribe against the app's
// declared SubjectFilter caps.
package inprocbus

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Match returns true if subject matches pattern. Pattern syntax follows the
// NATS subject convention:
//
//   - Tokens are period-separated.
//   - '*' matches exactly one token.
//   - '>' matches one or more remaining tokens; must be the last token.
//
// Subjects must be concrete (no wildcards). Empty pattern or empty subject
// returns false.
func Match(pattern, subject string) (ok bool) {
	if pattern == "" || subject == "" {
		return
	}
	pTokens := strings.Split(pattern, ".")
	sTokens := strings.Split(subject, ".")
	for i, pt := range pTokens {
		if pt == ">" {
			ok = (i == len(pTokens)-1) && (i < len(sTokens))
			return
		}
		if i >= len(sTokens) {
			return
		}
		if pt == "*" {
			continue
		}
		if pt != sTokens[i] {
			return
		}
	}
	ok = len(pTokens) == len(sTokens)
	return
}

// ValidatePattern checks structural validity. Returns nil if the pattern is
// well-formed; otherwise an error describing the first offence. Tokens must
// be non-empty, '>' must be the last token, and tokens other than '*' / '>'
// must consist of [A-Za-z0-9_-].
func ValidatePattern(pattern string) (err error) {
	if pattern == "" {
		err = eh.Errorf("pattern: empty")
		return
	}
	pTokens := strings.Split(pattern, ".")
	for i, pt := range pTokens {
		if pt == "" {
			err = eb.Build().Str("pattern", pattern).Int("position", i).Errorf("pattern: empty token")
			return
		}
		if pt == ">" {
			if i != len(pTokens)-1 {
				err = eb.Build().Str("pattern", pattern).Errorf("pattern: '>' must be last token")
				return
			}
			continue
		}
		if pt == "*" {
			continue
		}
		for _, r := range pt {
			if !isValidTokenChar(r) {
				err = eb.Build().Str("pattern", pattern).Errorf("pattern: invalid char %q in token %q", string(r), pt)
				return
			}
		}
	}
	return
}

// ValidateSubject is ValidatePattern minus the wildcard exemptions: subjects
// must be concrete dotted token sequences.
func ValidateSubject(subject string) (err error) {
	if subject == "" {
		err = eh.Errorf("subject: empty")
		return
	}
	sTokens := strings.Split(subject, ".")
	for i, st := range sTokens {
		if st == "" {
			err = eb.Build().Str("subject", subject).Int("position", i).Errorf("subject: empty token")
			return
		}
		if st == "*" || st == ">" {
			err = eb.Build().Str("subject", subject).Errorf("subject: wildcard %q not allowed", st)
			return
		}
		for _, r := range st {
			if !isValidTokenChar(r) {
				err = eb.Build().Str("subject", subject).Errorf("subject: invalid char %q in token %q", string(r), st)
				return
			}
		}
	}
	return
}

func isValidTokenChar(r rune) (ok bool) {
	ok = (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') || r == '_' || r == '-'
	return
}
