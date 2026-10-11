// Package cell is the declared security boundary of a deployment
// (ADR-0307 §SD1) and the exits across it (ADR-0312): what is inside the
// cell — this host, its engines, the model hosts the deployment trusts —
// and, derived from that, every edge boxer mediates that leaves it, a
// sink with a clearance in TLP terms. A payload leaves by a sink only at
// or below the sink's clearance; above it the deployment's approval
// policy says whether the person may let it out under a journaled
// approval the sink verifies, or nobody may. Decide is the guard: it
// allows, asks or denies, and never widens what a caller may do; the
// harness runs it as one guard beside its others (ADR-0310 §SD3), each a
// check that refuses or asks on its own, so the strictest answer is the
// one that stops the act first.
//
// Design note: the sinks are not a hand-kept list. They are derived from
// the declaration, so a channel the declaration omits is a declaration
// error visible in keelson('sinks'), not a hunt; channels that bypass
// boxer on a host the person administers are not here, and ADR-0307 says
// so. The clearances and the approval tiers are policy data from the
// environment (ADR-0009), with the defaults this package states, read
// failing closed. A person's approval does not lower a clearance: it is
// an entry the sink verifies against the journal — same run, the call it
// refused, the sink, the request bytes it saw — before one exit goes
// (ADR-0307 §SD7). The declaration's home is meant to be ADR-0126's
// topology; until that Update lands it is these variables, with
// BOXER_LLM_TRUSTED_HOSTS as the first fragment, taken as an input rather
// than declared twice.
package cell
