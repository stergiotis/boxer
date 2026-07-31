---
type: adr
status: accepted
date: 2026-07-25
reviewed-by: "p@stergiotis"
reviewed-date: 2026-07-26
---

# ADR-0141: The endpoint dispatch seam — one resolved decision per run

## Context

play talks to more than one engine. An external ClickHouse server over
HTTP, the loopback introspection plane
([ADR-0094](./0094-keelson-introspection-tables.md)), and — through the
`keelson()` macro — ad-hoc datasets whose decryption never leaves loopback
([ADR-0134](./0134-adhoc-datasets.md)). Which one runs a given statement was
decided by whatever the endpoint switcher last pointed at, and the user
held that decision in their head.

Mechanically, three issuers each read `Client.URL()` at request-build time
and independently arrived at an answer: the run path
(`ExecuteArrowStream`), the diagnostics EXPLAIN probe (`ProbeStatement`),
and the incremental-header progress client that rides the same URL
([ADR-0115](./0115-query-observability-data-plane-strategy.md) plane A). Nothing tied their answers
together, and nothing recorded why any of them was right.

The probe is the sharp edge. Its verdict is only worth reading because it
matches a real Run byte for byte — it deliberately shares `buildResidual`
with the run path for exactly that reason. But it ships
`EXPLAIN AST <residual>`, and that wrapper is outside Grammar1's SELECT
surface and names none of the residual's tables. Any placement rule that
looks at the statement would therefore answer differently for the probe
than for the run it claims to describe, and the probe would quietly start
attesting about a server the run never reaches.

[doc/explanation/query-system-requirements.md](../explanation/query-system-requirements.md)
sets out the requirements behind this (R2 hard locality walls, R6 the
two-axis dispatch label, R12 decisions as auditable data) and names this
extension point E2. Boxer's job is the seam; placement maps, cluster
rosters, and balancing strategy are site policy and stay out of the
repository.

## Decision

We will resolve the endpoint **once per run**, through a resolver
consulted with the finalized outgoing SQL, and pass the resulting
`dispatchDecision` — endpoint, class, and a human-readable reason — to
every issuer as a **required parameter**.

Two properties are carried by the types, not by convention:

- **The compiler enumerates the issuers.** A request path that reads an
  endpoint without being handed a decision does not compile. This is what
  makes the seam hold as the code grows.
- **The zero decision is invalid.** `dispatchDecision{}` names no endpoint,
  and every issuer routes through one accessor that rejects it. Forgetting
  to resolve fails the run loudly rather than falling back to an ambient
  default — the failure mode a nilable option would have re-introduced.

A decision may also *refuse*, carrying the reason instead of an endpoint.
The run does not happen and the user is told why.

The resolver sees the **residual** — the SQL after the client-side
rewrites, which is what the server will actually receive — so no rule ever
judges a form the server will not see. The diagnostics probe resolves from
the statement it wraps rather than from its own `EXPLAIN AST` text; a test
fails loudly if that regresses.

boxer ships two resolvers. `staticResolver` answers with the pinned
endpoint and nothing else — what play already did, written down.
`keelsonResolver`, behind the toolbar's Auto preset (default on), routes on
the one fact boxer owns: a read naming only `keelson()` tables goes to the
in-process introspection plane, a read naming plain tables stays on the
pinned endpoint, and a read naming both is refused, since no endpoint
serves both. Only a provable read moves — a mutation, or a statement whose
kind cannot be established, stays where the user pointed (R5). `system.*`
carries no placement meaning (it answers on either engine), and the
`keelson` *database* is an ordinary qualified table that merely shares a
word with the macro.

That is the whole of boxer's routing policy, and it is deliberately about
locality rather than preference: the introspection plane is the only place
its data exists.

## Alternatives

- **A nilable `*dispatchDecision`, or a field on `ExecOptions`.** Both make
  "no decision" representable and silently fall back to the ambient
  endpoint — precisely the implicit placement this ADR exists to remove.
- **Resolve inside `ExecuteArrowStream` from its own SQL argument.** Each
  issuer would resolve independently again, and the probe would resolve
  from its `EXPLAIN AST` wrapper. That is the divergence in Context, made
  permanent.
- **Resolve from the authored buffer rather than the residual.** Cheaper —
  it avoids one extra rewrite per run — but it lets a rule fire on a form
  the pre-execute passes are about to rewrite away.
- **Put the decision on `compiledNode`.** That struct is a memo key
  (`key()` is its identity); a decision in it would fragment the cache and
  make placement part of node identity, which it is not.

## Consequences

### Positive

- Where a query runs is a named value with a reason attached, so it can be
  shown in the UI and recorded on the run's durable record (R12).
- A system that needs real placement policy replaces one small interface,
  and publishes its placement data as ordinary introspection tables (E5)
  rather than as boxer schema.
- Probe and run cannot diverge, and the guard is a test rather than a
  comment.

### Negative

- One extra client-side rewrite per run, since resolution runs
  `buildResidual` to see the residual. The schema probes behind it are
  cached, so the cost is parsing, and it is paid per run rather than per
  frame.
- Every future request path must obtain a decision. That is the intended
  friction, but it is friction.

### Neutral

- `Client.URL()` survives as the *manual base*: what the switcher shows and
  what a resolver falls back to. Infrastructure requests that are not the
  user's query — the `system.columns` schema probe, the pin INSERT, the
  run-history readback — deliberately stay on it rather than taking a
  decision, since none of them is the statement being placed.
- The affinity token of R4 is carried through the resolver signature and
  judged by nobody: boxer's own endpoints have no members to choose
  between. It exists so a site resolver has somewhere to put it.
- The dispatch class is play-internal for now. Promoting it to the shared
  E9 vocabulary waits for a second consumer.

## Status

Accepted 2026-07-26, and implemented: the seam, `staticResolver`,
`keelsonResolver` behind the Auto preset (default on), and the applet
stamping derived from it.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-07-26 — The decision records its member, and the affinity token has a judge

Both were recorded above as carried-but-unjudged, and
[ADR-0144](./0144-query-engine-adapters.md) closed them while building the
engine roles.

The decision gains a `member` field: the concrete member of a placement the
run went to, empty where there is none to choose. It is what a cancellation
must be addressed to — `KILL QUERY` reaches only the member that ran the
query (R11), so a decision remembering a cluster address instead would send
the kill to whichever member happened to answer. `killTarget()` gives that
one answer rather than a convention each caller re-derives, and the toolbar
names the member only when there was a choice.

The affinity token's judge is `queryengine.SelectMember(placement,
affinity)` — the deterministic (placement, generation) function R4 asks for,
and no more than that. Boxer still supplies no roster and no balancing: the
Neutral note above holds, with the one word "nobody" now reading "no resolver
boxer ships", since a site resolver has somewhere concrete to put its answer.

The dispatch class is still play-internal. A second consumer has not
appeared, and the E9 vocabulary waits for one.

One consequence for the issuers this ADR enumerates: they now obtain an
engine from the decision rather than a URL, one per run
(`Client.engineFor`). That is not a new seam — it is the same decision,
consumed by something that also knows how to observe and cancel on the
endpoint it names.

### 2026-07-31 — wrapped wire bodies resolve from the statement they wrap

The diagnostics probe's rule — "resolve from the statement it wraps rather
than from its own `EXPLAIN AST` text" — gained a second consumer and with it
a general form: `ExecOptions.WrapStatement`, a wire-body-only transform the
transport applies to the residual between the client-side rewrites and the
FORMAT step. A lane that sets it (the Flow tab's EXPLAIN lenses, ADR-0153)
compiles, routes, rewrites and memoises the plain statement; only the bytes
on the wire differ. The placement argument is the same as the probe's, made
sharper by the lenses: index structure and schema are endpoint-local, so an
EXPLAIN answered by any endpoint other than the one the statement routes to
describes a query that will never run there. The hook lives on `ExecOptions`
— what the Alternatives rejected was a *decision* field, which would have
made "no decision" representable; a wire transform carries no placement and
leaves the required-parameter seam intact.

## References

- [doc/explanation/query-system-requirements.md](../explanation/query-system-requirements.md) — the requirements and the E-point catalog.
- [ADR-0094](./0094-keelson-introspection-tables.md) — the loopback introspection plane and its `/query` endpoint.
- [ADR-0134](./0134-adhoc-datasets.md) — ad-hoc datasets; §SD4 is the alias→handle rewrite inside the residual.
- [ADR-0115](./0115-query-observability-data-plane-strategy.md) — observability planes; the progress client that rides the dispatched URL.
- [ADR-0097](./0097-play-reactive-query-graph.md) — the reactive query graph whose lanes are the other issuers.
