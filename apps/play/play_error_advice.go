package play

import (
	"regexp"
	"strings"
)

// play_error_advice.go — what a run's error leaves unsaid.
//
// A server's diagnostic names the rule a statement broke. For a few rules
// the statement's author did not write what the diagnostic describes: the
// server arrived there by a substitution of its own. The advice says which
// substitution, so the fix is one edit instead of a search.

// shadowingAggregateRe matches the server's complaint about an aggregate
// that carries an alias and stands where no aggregate may.
var shadowingAggregateRe = regexp.MustCompile(`Aggregate function (.+?) AS ([A-Za-z_][A-Za-z0-9_]*) is found (inside another aggregate function|in [A-Z ]+?) in query`)

// runErrorAdvice is a sentence to follow a failed run's error text, or the
// empty string when the text says all there is to say.
//
// It knows one case. ClickHouse reads a name that is both a column and an
// alias of the same SELECT as the alias, so `any(tot) AS tot` turns every
// other `tot` of that SELECT into the aggregate: `sum(tot)` becomes
// `sum(any(tot))` and `GROUP BY tot` groups by an aggregate. The server
// reports the nesting it built, which the statement does not show.
func runErrorAdvice(text string) (advice string) {
	m := shadowingAggregateRe.FindStringSubmatch(text)
	if m == nil {
		return
	}
	aggregate, alias := m[1], m[2]
	if !namesIdentifier(aggregate, alias) {
		return
	}
	return "The alias " + alias + " has the name of a column it aggregates, and ClickHouse reads every other " + alias +
		" in that SELECT as the alias, so an aggregate, GROUP BY or WHERE that names the column gets the aggregate instead. Give the alias another name"
}

// namesIdentifier reports whether expr uses name as a whole identifier,
// bare or as the last part of a qualified one.
func namesIdentifier(expr string, name string) (uses bool) {
	for rest := expr; ; {
		i := strings.Index(rest, name)
		if i < 0 {
			return false
		}
		before, after := byte(' '), byte(' ')
		if i > 0 {
			before = rest[i-1]
		}
		if i+len(name) < len(rest) {
			after = rest[i+len(name)]
		}
		if !identifierByte(before) && !identifierByte(after) && after != '(' {
			return true
		}
		rest = rest[i+len(name):]
	}
}

// identifierByte reports whether b may stand inside an identifier.
func identifierByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// withRunErrorAdvice is a failed run's error text followed by its advice.
func withRunErrorAdvice(text string) (out string) {
	advice := runErrorAdvice(text)
	if advice == "" {
		return text
	}
	return strings.TrimRight(text, " \n") + " — " + advice
}
