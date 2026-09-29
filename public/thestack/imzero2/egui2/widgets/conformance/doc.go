// Package conformance holds the drift guard for the Go widget API contract of
// ADR-0267: a test that parses every package under widgets/ and checks the
// mechanical half of the contract's rules — entry verbs, type names, what
// Render returns, how ids and probe seqs are derived, how options and
// interaction come back, whether a package that starts goroutines can be
// closed.
//
// The test carries an allowlist of packages not yet migrated, each with the
// rules it still violates. A package may only leave the list, and a rule may
// only leave a package's entry: a violation that is not listed fails the
// test, and a listed violation that no longer occurs fails it too, so the
// list is always exactly the remaining work. Packages that are libraries
// rather than widgets are named in a separate exemption set and are not
// checked.
//
// The checks are syntactic (go/parser, no type information) on purpose: they
// run in the default test lane in well under a second and need no build of
// the widgets. The rules they cannot express — that a model carries no UI
// state, that a callback draws host content rather than reporting a click —
// remain review rules.
package conformance
