package persiststore

import "strings"

// Key spaces (ADR-0105 Update 2026-08-15, P2). One table has one key space
// and the state view is per key, so without a kind prefix a workingset and
// a persist key both spelled "<app>/default" would be one entity whose
// newest row wins for both. The prefix also makes a per-kind live scan a
// primary-key range read (ScanOpts.KeyPrefix), since the table sorts by
// (id, ts).
const (
	StateKeyPrefix       = "state/"
	WorkingsetKeyPrefix  = "ws/"
	ColumnWidthKeyPrefix = "cw/"
)

// escapeSegment makes s safe as a non-final key segment: "%" becomes "%25"
// and "/" becomes "%2F", so the escaped form contains no "/" and the
// mapping is injective.
//
// App ids are Go import paths ("github.com/…/apps/play") and nest — every
// applet's id sits under the sqlapplet app's ("…/apps/sqlapplet/<slug>").
// Unescaped, "state/<app>/<key>" could not tell app "a/b" with key "c" from
// app "a" with key "b/c", and a per-app prefix scan for "…/apps/sqlapplet"
// would take in every applet's rows. The final segment is not escaped: it
// is whatever remains after the others, so it needs no delimiter.
func escapeSegment(s string) string {
	if !strings.ContainsAny(s, "%/") {
		return s
	}
	s = strings.ReplaceAll(s, "%", "%25")
	return strings.ReplaceAll(s, "/", "%2F")
}

// StateKey is the entity key of one persist-state value.
func StateKey(appId string, key string) string {
	return StateAppPrefix(appId) + key
}

// StateAppPrefix selects every persist-state key of one app, and no other
// app's.
func StateAppPrefix(appId string) string {
	return StateKeyPrefix + escapeSegment(appId) + "/"
}

// WorkingsetKey is the entity key of one saved workingset.
func WorkingsetKey(appId string, name string) string {
	return WorkingsetAppPrefix(appId) + name
}

// WorkingsetAppPrefix selects every workingset of one app, and no other
// app's.
func WorkingsetAppPrefix(appId string) string {
	return WorkingsetKeyPrefix + escapeSegment(appId) + "/"
}

// ColumnWidthKey is the entity key of one column-width override. The tier
// is a fixed label and the column key a hex hash, but both are escaped all
// the same, so the key's shape does not rest on what callers pass.
func ColumnWidthKey(appId string, tier string, scope string, columnKey string) string {
	return ColumnWidthAppPrefix(appId) + escapeSegment(tier) + "/" + escapeSegment(scope) + "/" + columnKey
}

// ColumnWidthAppPrefix selects every column-width override of one app, and
// no other app's.
func ColumnWidthAppPrefix(appId string) string {
	return ColumnWidthKeyPrefix + escapeSegment(appId) + "/"
}
