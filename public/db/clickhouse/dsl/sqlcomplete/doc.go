// Package sqlcomplete answers "what may stand where the caret is" for a
// ClickHouse buffer (ADR-0190).
//
// It is built on one commitment, §SD1: a candidate is offered only from a
// source of truth — a registry, the grammar's own vocabulary, the parsed
// statement's names, or a catalog answer for the buffer's endpoint. Where none
// exists the engine offers nothing and says why. There is no "plausible" band
// below an "exact" one, and no ranking that quietly promotes a guess.
//
// # The three inputs
//
//   - The *site* ([github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight.CaretSite]),
//     which answers every frame from the lex tier: the enclosing call, the
//     argument ordinal, the literal being typed, the member access.
//   - The *vocabulary*
//     ([github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab]), which
//     says what each argument position *is* — a component kind, a section, a
//     tuple element of a sibling argument.
//   - The *providers*, wired by the host per buffer (ADR-0147 §SD7), which turn
//     a domain into candidates. A provider that has not answered yet reports
//     "not ready", which is not the same as "empty": the engine stays silent
//     rather than claiming the domain has no members.
//
// # What the engine does not do
//
// It does not rank, it does not fuzzy-match, and it does not lex or parse. The
// match state is a case-sensitive prefix and equality test over the token the
// site reports, computed per frame; anything needing the tree arrives through
// [Request.Scope] a quiescence window later.
package sqlcomplete
