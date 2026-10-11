// Package agentschema is the agent schema (ADR-0313): the relations an
// agent may read in full, declared once and compiled to SQL twice — to a
// server principal whose scope is a settings-profile constraint the query
// cannot override (§SD2), with a restrictive row policy as the backstop
// that must never fire (§SD3), and to the local worker with the scope as a
// constant the single writer ships (§SD6). Base tables are not nameable
// from the agent's principal, so validity is syntactic and a refusal names
// a relation, never a row (§SD1); content a policy withholds reappears in a
// value-less withheld section with its memberships and the policy that
// withheld it, so a reader tells "withheld" from "never recorded" (§SD5).
//
// The journal relations of §SD1 — my_run, my_conversation and
// agent_history(scope) — are the agent's view of ADR-0308 §SD10's read
// surface, declared over the plain-shaped journal tables the introspection
// registry serves; budgets on reads through them are ADR-0312 §SD5's
// (§SD7).
//
// Design note: the declaration is a Go literal, not a parsed text — four
// rule forms and no value predicates (§SD6), because a predicate on values
// is where a declaration becomes a policy engine. Memberships on the plain
// shape are a declared array column; the compilation of the same rules
// onto leeway's packed and exploded layouts (§SD4) is deferred to the
// dated Update on ADR-0171 that admits the withheld section into the view
// IR.
package agentschema
