package mdextract

import (
	"regexp"
	"strings"
	"time"
)

// timestampRe gates what is tried as a timestamp: a calendar date, optionally
// followed by a time with an optional fraction and an optional zone — the
// shapes YAML's timestamp type and ISO 8601 share. The gate exists so that
// arbitrary property text never reaches time.Parse; the layouts below decide
// the rest.
var timestampRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(?:[Tt ]\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?: ?(?:[Zz]|[+-]\d{2}(?::?\d{2})?))?)?$`)

// timestampLayouts are tried in order against the gated text with "T"
// normalised as the separator. A layout without a zone reads as UTC.
var timestampLayouts = []string{
	"2006-01-02",
	"2006-01-02T15:04",
	"2006-01-02T15:04Z07:00",
	"2006-01-02T15:04Z0700",
	"2006-01-02T15:04Z07",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05Z0700",
	"2006-01-02T15:04:05Z07",
	"2006-01-02T15:04:05 Z07:00",
	"2006-01-02T15:04:05 Z0700",
	"2006-01-02T15:04:05 Z07",
}

// ParseTimestamp recognises a YAML timestamp or an ISO 8601 date or
// date-time in s: `2024-03-01`, `2024-03-01T10:20:30Z`,
// `2024-03-01 10:20:30.5 +02:00`. A value without a zone is taken as UTC.
// Fractional seconds are kept to nanoseconds.
//
// It is applied to every string leaf, so a quoted `"2024-03-01"` reads as a
// date too — the decoder does not tell the two apart, and Obsidian types a
// property by its value the same way.
func ParseTimestamp(s string) (t time.Time, ok bool) {
	s = strings.TrimSpace(s)
	if !timestampRe.MatchString(s) {
		return
	}
	// One separator spelling for the layouts; the lower-case forms are
	// legal ISO 8601 and YAML alike.
	if len(s) > 10 {
		s = s[:10] + "T" + strings.ToUpper(s[11:])
	}
	for _, layout := range timestampLayouts {
		var err error
		t, err = time.Parse(layout, s)
		if err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
