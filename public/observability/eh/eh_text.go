package eh

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/fxamacker/cbor/v2"
)

// Text renders err as one line for a reader that sees only text: its message
// followed by the facts eb.Build attached to it and to every error it wraps,
// as "message [key=value …]". An HTTP body, a refusal an agent reads or a
// status line shows Error() alone, which since the values moved onto the
// builder no longer carries them.
//
// The chain is walked outermost first, through Unwrap() error and Unwrap()
// []error, and where two levels carry one key the outer value is shown. Keys
// are sorted. A string value is quoted when it is empty or holds a space, a
// quote, '=' or ']'. A payload that does not decode is skipped. Text of an
// error without facts is its Error(), and of nil the empty string.
func Text(err error) string {
	if err == nil {
		return ""
	}
	facts := map[string]any{}
	collectFacts(err, facts, map[error]struct{}{})
	msg := err.Error()
	if len(facts) == 0 {
		return msg
	}
	keys := make([]string, 0, len(facts))
	for k := range facts {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var sova strings.Builder
	sova.WriteString(msg)
	sova.WriteString(" [")
	for i, k := range keys {
		if i > 0 {
			sova.WriteByte(' ')
		}
		sova.WriteString(k)
		sova.WriteByte('=')
		sova.WriteString(textValue(facts[k]))
	}
	sova.WriteByte(']')
	return sova.String()
}

// collectFacts adds the decoded facts of err and of the errors it wraps to
// facts, keeping a key's first, outermost value.
func collectFacts(err error, facts map[string]any, seen map[error]struct{}) {
	if err == nil {
		return
	}
	if _, dup := seen[err]; dup {
		return
	}
	seen[err] = struct{}{}
	if esd, ok := err.(ErrorWithStructuredDataI); ok {
		if data := esd.GetCBORStructuredData(); len(data) > 0 {
			var level map[string]any
			if cbor.Unmarshal(data, &level) == nil {
				for k, v := range level {
					if _, have := facts[k]; !have {
						facts[k] = v
					}
				}
			}
		}
	}
	switch u := err.(type) {
	case unwrapableMulti:
		for _, child := range u.Unwrap() {
			collectFacts(child, facts, seen)
		}
	case unwrapableSingle:
		collectFacts(u.Unwrap(), facts, seen)
	}
}

// textValue renders one fact value for Text.
func textValue(v any) string {
	switch t := v.(type) {
	case string:
		if t == "" || strings.ContainsAny(t, " \t\n\"=]") {
			return strconv.Quote(t)
		}
		return t
	case []any:
		parts := make([]string, len(t))
		for i, e := range t {
			parts[i] = textValue(e)
		}
		return "[" + strings.Join(parts, ",") + "]"
	default:
		return fmt.Sprint(t)
	}
}
