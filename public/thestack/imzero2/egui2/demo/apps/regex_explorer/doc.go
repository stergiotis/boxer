// Package regex_explorer is an interactive GUI for testing ClickHouse-flavoured
// regular expressions. Modelled on regexr.com, scoped to ClickHouse's function
// surface: RE2-backed single-pattern functions (match, extractAll,
// extractAllGroups, replaceRegexpAll) and VectorScan-backed multi-pattern
// (multiMatchAllIndices).
//
// Registered with the app runtime as a windowed app, and with the demo
// registry as two gallery scenes (regex_explorer_tour.go). Queries execute
// against a pooled `clickhouse-local` worker reached over the chlocalbroker
// capability subject `ch.local.exec.regex_explorer` — no server, no auth, no
// network, and no subprocess management in this package. User-supplied strings
// reach ClickHouse as SQL literals produced by boxer's marshalling.EscapeString.
//
// Match offsets for inline highlighting and capture-group breakdown are
// computed locally via Go's regexp package, which targets the same RE2
// specification as ClickHouse's single-pattern functions. See
// doc/adr/0054-regex-explorer-offset-authority.md for the rationale, the
// SD1 engine-fidelity tripwire, and the ledger of known differences between
// the two engines — of which the load-bearing one is that ClickHouse's
// extractAll returns capture group 1, not the full match, whenever the
// pattern captures.
package regex_explorer
