---
type: adr
status: accepted
date: 2026-11-04
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-05
reversibility: local-edit
---

# ADR-0357: A model call names the journal entry written before it

## Context

ADR-0308 §SD2 says every act is written to the run journal before it leaves. The
rule held because the chat wrote its entries in that order, not because anything
checked. The journal's writer does not gate the model client, and the host's
model service accepts a call that names no run. A harness that skips the journal
therefore loses write-before-act, the only-input rule and resume without anything
refusing it, and the record cannot show what is missing, since an act never
written leaves no trace to find.

ADR-0318 §SD1 limits the problem by naming one harness and calling that a review
convention. It says what should hold, and a second holder of the `llm` grant would
still run beside the journal.

The chat itself had gaps. The summariser and the title call named no run, and the
title call wrote an intent with no request entry. The request entry's digest is
canonical over the projection and the profile fingerprint, so it cannot be
compared with what the service receives, and the summariser reuses it for a
request with one message more.

## Decision

**We will have the model service refuse a completion unless it names a `request`
entry, already in the journal, of an open run its sender owns, whose content
digest matches the call.**

### SD1 — The call carries a reference

`llm.Request` gains `Journal`, in the form of an approval key: `<run>#<ordinal>`.
The service loads the entry through the host's journal store before any wall or
admission check, and a refusal goes through the service's ordinary refusal path,
so a refused call is on the trail.

### SD2 — What the check holds

The entry exists and is of kind `request`; its run is open and owned by the
sender; and its content digest is the call's. An approved resend (ADR-0312 §SD3)
presents the same entry a second time and passes, since the digest leaves
`Approval` out. A run in the `Run` field that disagrees with the reference is
refused.

### SD3 — A second digest, on the entry's name

The existing digest stays: it is the canonical identity law L4 compares. The new
one is over what the call asks of the model — messages, tools and parameters —
computed by one function on both sides of the bus. The harness cannot digest the
encoded body, because the service fills in the model and the default token limit
before encoding.

It lives in the request entry's `Name`, which is already chained and which request
entries leave empty; `tools` entries already keep a digest there. No record-store
column is added, so existing runs keep verifying under L31, and an older entry's
empty name is a digest no new call can match.

### SD4 — No store refuses, and no switch

A service built with no journal checker refuses every completion and logs why
once, as approvals already do. A failed journal service therefore stops model
calls. That is the intended failure: the chat cannot work without its store in any
case, and allowing the call would remove the rule in the one situation it exists
for. There is no environment variable to turn this off. Tests name an explicit
stand-in.

### SD5 — An outcome settles a request

`agentrun.Fold` treats an outcome under a request's key as settling it, as a reply
does. The title and the summariser end in outcomes, and without this a request
entry from either returns as a call to remake on resume.

### SD6 — The evidence

The tagged id of the entry goes on the call's record and its row in
`keelson('llm_calls')`. Law L39, served by `keelson('evidence_checks')`: every
answered call names a request entry, and no entry is named by two calls that left.
"One entry, one call" is a law and not a check at the door, because a call
refused at a wall legitimately presents its entry again. L39 judges the calls in
this process's ring.

**Deferred.** The durable trail row (`trail.LlmCall`) does not carry the id yet. It
needs a vocabulary membership and a regeneration, and L39 reads the ring, so the
law does not wait on it. Until then a join from a stored call row to its request
entry goes through the conversation and the turn.

## What this does not establish

It shows that a request was recorded before it was sent, with what content. It
does not show the journal was the only input, because a harness can record a
request it built some other way. Nor does it ensure a reply is written.

## Alternatives

- **The service writes the entry itself.** Rejected: it breaks the one-writer rule
  (ADR-0308 §SD9), and the service would hold what only the coordinator knows.
- **Check that the run exists.** Rejected: a harness could send anything under one
  open run, so the check would catch only a harness that never opened one.
- **Check the entry without a digest.** Rejected: simpler and needs no new
  function, but it cannot catch a harness that records one request and sends
  another.
- **Digest the encoded body.** Rejected: see SD3.
- **A new `RequestMeta` field.** Rejected: it changes the record-store shape and
  risks L31 over existing runs, to no benefit over the unused name.
- **An enforcement switch, off by default.** Rejected: a rule whose default is off
  is ADR-0318's review convention again.

## Consequences

### Positive

- A harness that skips the journal fails on its first model call, in testing,
  whichever app it is.
- ADR-0318's concern, a second caller running beside the controls, no longer holds
  for the journal, whatever the number of holders.

### Negative

- One more store read per model call. The SQL store's cost is unmeasured beside a
  model call's latency.
- A host whose journal service fails to start can make no model calls.
- An integrator with its own `llm` client must journal before calling.

### Neutral

- ADR-0318's single holder stays as the rule; this record does not widen it.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `llm.Request`, its wire form | added `Journal` | additive; a caller without it is refused |
| `llm.Config` | added `Journal` | hostboot wires it; tests name a stand-in |
| `llm.CallRecord`, `keelson('llm_calls')` | added the request entry's id | readers of the table |
| `agentrun.Fold` | an outcome settles a request | fold tests, resume |
| Chat's model calls | one helper; the title gains a request entry | the journal's shape for a title |
| Evidence laws | added L39 | `keelson('evidence_checks')` |

## Migration — Tier 1

- **Breaks.** A caller that does not journal, of which the tree has none once the
  chat's three call paths conform.
- **Old shape.** Existing runs verify unchanged; their request entries cannot be
  presented by a new call.

## Verification plan — Tier 1

- **Lane.** Default `go test`: each way the check refuses; the digest across the
  bus codec, `Extra` and tool schemas included; the approved resend; the fold; the
  chat's turn, summariser and title against a real checker over one store; L39.
- **Scene.** The chat's existing scenes run on a host with the checker wired, so
  a call path that does not journal fails them.

## Status

Accepted 2026-11-05, with every decision implemented. The durable trail row stays
deferred, as SD6 records.

## References

- [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md) — write before acting, one writer.
- [ADR-0309](./0309-context-projection-as-sql-over-the-journal-in-a-local-worker.md) — the canonical request digest, law L4.
- [ADR-0312](./0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md) — approval keys, the resend.
- [ADR-0318](./0318-model-inference-is-the-harnesss-apps-expose-operations.md) — one harness, by convention.
- [ADR-0358](./0358-asynchronous-operations-a-pending-outcome-and-a-turn-a-completion-wakes.md) — the same check on a tool call's intent.
