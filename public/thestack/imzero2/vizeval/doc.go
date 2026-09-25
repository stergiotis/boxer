// Package vizeval holds what a rendering candidate is for the vizeval harness
// (ADR-0257, proposed): a sink of play's Experiments pane and the options it is
// drawn with.
//
// A sink declares its settings as an option [Space]. The declaration is the one
// source for the pane's controls, for validating a seeded candidate, and for a
// search enumerating candidates without knowing sink internals (ADR-0257 §SD2).
// [Sinks] is the catalogue the Experiments pane implements; the pane checks at
// test time that it implements exactly these.
package vizeval
