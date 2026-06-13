package inprocbus

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// SubjectAllowed returns true if any cap in caps with a matching direction
// covers the given subject under NATS-style pattern matching. dir is the
// caller's intent (CapDirectionPub when publishing, CapDirectionSub when
// subscribing); a cap with CapDirectionBoth satisfies either.
func SubjectAllowed(caps []app.SubjectFilter, subject string, dir app.CapDirectionE) (allowed bool) {
	for _, c := range caps {
		if !directionAllows(c.Direction, dir) {
			continue
		}
		if Match(c.Pattern, subject) {
			allowed = true
			return
		}
	}
	return
}

func directionAllows(have, want app.CapDirectionE) (ok bool) {
	if have == app.CapDirectionBoth {
		ok = true
		return
	}
	ok = have == want
	return
}
