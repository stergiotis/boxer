---
type: adr
status: proposed
date: 2026-07-25
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0142: The result-frame contract — absence means incomplete

## Context

A query result can be short for several unrelated reasons: the run finished
and that was all the data; the run hit a row limit and the rest was never
sent; the connection dropped mid-stream; the process serving it died. Read
off a row count, all four look identical — a number of rows that arrived.

Before this, play's two result paths each drained an Arrow reader with
their own loop and their own error check. Whether a dead stream was noticed
depended on whether a particular error variable happened to be consulted at
a particular point, in each path separately. Nothing in the types carried
the distinction, so nothing carried it to the panels that render results,
and each would have had to re-derive "did this finish?" for itself.

The requirement is R9 of
[query-system-requirements](../explanation/query-system-requirements.md),
completeness honesty: a capped result, a stream that died, and a complete
result are three outcomes, and a consumer must be unable to mistake one for
another. E3 is the extension point.

## Decision

We will model a run's result as a sequence of typed, sequenced frames —
data, progress, and **exactly one terminal** frame saying how it ended
(complete, truncated, or failed) — and make absence the safe reading:

> **No terminal frame means incomplete.**

Nothing has to go right for a partial result to be recognised as partial. A
producer that stops — because it crashed, was disconnected, or simply
forgot — yields `ErrIncomplete` rather than a plausible short answer. The
alternative framing, an explicit "incomplete" marker, requires the failing
party to successfully report its own failure.

One `Collector` owns the invariants, and it rejects rather than tolerates:
a frame out of sequence, a second terminal, anything after a terminal, and
a frame whose kind or terminal state was never set. Each of those is a
producer bug that would otherwise resurface later as a wrong answer.

Zero values are `Unknown`, not `Data` and `Complete`. A frame nobody filled
in must not read as data, and a terminal that never said how the run ended
must not read as success — the same default-deny discipline the statement
classifier uses.

A synchronous HTTP response is the **degenerate case** of this stream, not
an exception to it: data frames for the batches, then one terminal. Writing
it that way is what puts the invariants in one place.

Progress frames are advisory and may be absent entirely; their absence says
nothing (R8). Only the terminal is load-bearing.

## Alternatives

- **A boolean `complete` beside the result.** Representable as false-y by
  omission, and it says nothing about *why* — truncated and failed collapse
  into one bit that consumers then guess about.
- **An explicit "incomplete" terminal.** Requires the party that failed to
  report its own failure. A killed process sends nothing at all, which is
  exactly the case the contract must handle.
- **Each panel checks the reader's error itself.** The status quo. It is
  not that panels do it wrongly, it is that each must remember to, and a
  new panel starts from zero.
- **Carry the payload as `any`.** Loses the type at the seam for no gain;
  the collector is generic in the payload instead, and the envelope's
  invariants are payload-independent.

## Consequences

### Positive

- A partial result is recognisable as partial without cooperation from
  whatever went wrong.
- Truncation has somewhere to live, so a result capped against a limit can
  be rendered as capped rather than as the answer.
- Panels render results instead of re-deriving what "finished" means.

### Negative

- Every result path must now produce a terminal frame. That is the point,
  but it is a step a new path can forget — the failure mode is a loud
  `ErrIncomplete`, not a silent wrong answer.
- The collector holds the whole result before the terminal arrives, which
  suits the synchronous binding and would need revisiting for a genuinely
  streaming consumer that renders as it goes.

### Neutral

- What boxer can honestly detect about truncation is narrow: only a cap the
  request declared on *itself* is visible client-side. A cap applied by a
  server default or a quota comes back short with nothing on the wire, and
  is not claimed. The detection is also deliberately ambiguous-loud — a
  result complete at exactly the cap is indistinguishable from one cut
  there, and is reported as *may be a prefix*.
- The contract is in-process for now. The bus binding it needs for a
  streaming reply channel is separate work.

## Status

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [doc/explanation/query-system-requirements.md](../explanation/query-system-requirements.md) — R8, R9, and the E3 extension point.
- [ADR-0115](./0115-query-observability-data-plane-strategy.md) — the observability planes progress frames come from.
- [ADR-0141](./0141-play-endpoint-dispatch-seam.md) — the dispatch seam; the same default-deny zero-value discipline.
