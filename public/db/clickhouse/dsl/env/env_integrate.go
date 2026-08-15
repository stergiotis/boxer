package env

import (
	"sort"
	"strings"
)

// Integrate produces SQL by emitting [Environment.PreludeComments], then the
// SET-line prelude, then body. SET lines are emitted in alphabetical order.
// StatementSettings and Format are read-only views populated by [Extract] —
// they live in body and are not re-emitted here.
//
// Integrate is the inverse of [Extract] up to whitespace, SET-line ordering,
// and comment position. Round-trip is normalising, not byte-identical: a
// comment above a prelude does come back where it was, but one written
// between two SET lines floats to the top, the same way the SET lines
// themselves are re-ordered.
func (e *Environment) Integrate(body string) (sql string, err error) {
	if e == nil {
		sql = body
		return
	}

	var sb strings.Builder
	sb.WriteString(e.PreludeComments)

	keys := make([]string, 0, len(e.SessionSettings))
	for k := range e.SessionSettings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s := e.SessionSettings[k]
		sb.WriteString("SET ")
		sb.WriteString(s.Name)
		sb.WriteString(" = ")
		sb.WriteString(s.Raw)
		sb.WriteString(";\n")
	}

	pkeys := make([]string, 0, len(e.Params))
	for k, p := range e.Params {
		if p.Raw == "" {
			continue
		}
		pkeys = append(pkeys, k)
	}
	sort.Strings(pkeys)
	for _, k := range pkeys {
		p := e.Params[k]
		sb.WriteString("SET ")
		sb.WriteString(p.Name)
		sb.WriteString(" = ")
		sb.WriteString(p.Raw)
		sb.WriteString(";\n")
	}

	sb.WriteString(body)
	sql = sb.String()
	return
}
