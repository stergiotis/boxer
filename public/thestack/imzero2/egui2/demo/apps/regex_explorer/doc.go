// Package regex_explorer is an interactive GUI for testing ClickHouse-flavoured
// regular expressions. Modelled on regexr.com, scoped to ClickHouse's function
// surface: RE2-backed single-pattern functions (match, extractAll,
// extractAllGroups, replaceRegexpAll) and VectorScan-backed multi-pattern
// (multiMatchAllIndices).
//
// Registered as demo-carousel entry a006 via [RenderLoopHandlerDemo]. Queries
// execute through `clickhouse local` subprocesses (invoked via os/exec with
// --format ArrowStream) rather than a running ClickHouse server, so the demo
// is self-contained — no server, no auth, no network. User-supplied strings
// reach the subprocess as ClickHouse SQL literals produced by boxer's
// marshalling.EscapeString. Match offsets for inline highlighting are
// computed locally via Go's regexp package (RE2-compatible with ClickHouse's
// single-pattern RE2 functions). See doc/adr/0005-regex-explorer-offset-authority.md
// for the rationale and the SD1 engine-fidelity tripwire.
package regex_explorer
