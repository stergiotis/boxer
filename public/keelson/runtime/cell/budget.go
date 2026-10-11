package cell

// The exit budget (ADR-0312 §SD5, law L19): bytes sent to a remote route
// per task. The model service reserves the exact size of the request body
// as the quantity "bytes.sent" in ration's ledger before the request
// leaves, and settles the same figure after, so a budget rule on a task
// account bounds it in the ledger's own shape and the call that would
// cross the limit is refused rather than admitted and overshooting. There
// is no second meter: a consumer without the ledger has no exit.
//
// deferred: read budgets in bytes on every read path and in attribute
// instances on the agent schema (ADR-0313 §SD7) are metered where those
// reads are built; this note holds the exit side only.
