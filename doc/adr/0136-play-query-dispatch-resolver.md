---
type: adr
status: proposed
date: 2026-07-21
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0136: play query dispatch — classification and the endpoint resolver seam

## Context

`apps/play` targets more than one query endpoint: an external ClickHouse
(its launch URL, switchable in the toolbar) and, when a co-resident host
publishes one, the loopback keelson introspection `/query` endpoint
([ADR-0094](./0094-keelson-introspection-tables.md), 2026-06-27 update).
The user switches between them by hand. A buffer that references
`keelson('…')` sent to the external endpoint errors verbatim; a buffer
that references server tables sent to `/query` errors just as verbatim.
The routing knowledge exists only in the user's head.

The split is not an accident the `keelson()` nanopass pass could absorb.
The pass owns the macro's *syntax* completely, but its two expansions each
pin the query to an execution locality: a bare TEMPORARY-table name only
the in-process engine can see, or a `url()` against a loopback address
only a machine-local engine can reach. The providers read live in-process
state, the table source is loopback-bound as a security boundary
([ADR-0094](./0094-keelson-introspection-tables.md) §SD3/§SD7,
[ADR-0134](./0134-adhoc-datasets.md)), and play's primary target is an
arbitrary external server. Which engine executes is therefore a
*placement* decision the client must make before any rewrite is
meaningful; today that decision is manual.

This ADR introduces the seam that makes the decision automatic. It is
deliberately the smallest cut of a longer line — placement rules and
cluster balancing ([ADR-0137](./0137-query-placement-clusters-balancing.md))
and an asynchronous result/progress transport
([ADR-0138](./0138-streaming-query-transport-observability.md)) plug into
the same seam later. What must be decided now is only the seam's shape,
because two of its properties are expensive to retrofit: per-run
resolution and a stream-shaped result path.

## Decision

Play consults a resolver on every run; the resolver classifies the
outgoing SQL and returns a dispatch decision. The seam is play-internal.

- **SD1 — Per-run resolver, decision rides the request.** A resolver
  interface is consulted once per run with the *residual* SQL (the text
  `buildResidual` finalizes: alias→handle rewrite, `SET param_*` harvest,
  pre-execute passes per [ADR-0108](./0108-keelson-sql-pass-registry.md)
  §SD6). It returns `{target, alternates, reason}`. The resolved target is
  carried by the individual request; the client's sticky URL is *not*
  mutated — it is shared state read concurrently by live delivery and the
  schema provider, and it remains the manual base the toolbar edits. The
  signature carries an affinity key from day one (a constant until
  [ADR-0137](./0137-query-placement-clusters-balancing.md) supplies query-graph
  generations; [ADR-0138](./0138-streaming-query-transport-observability.md)
  promotes it to the run identity).

- **SD2 — Classification.** The residual is parsed once; `keelson(…)`
  table-function references are collected (the predicate `keelsonsql`
  already uses internally, exported as a `References` helper) alongside
  plain table references (`analysis.ExtractTables`). Classes: *none*,
  *keelson-only*, *plain-only*, *mixed*, *unparseable*. Only the macro is
  an unambiguous locality signal: `system.*` exists on every engine with
  different content, and a qualified `keelson.*` table name can
  legitimately exist server-side, so both classify as
  indifferent — never as introspection evidence.

- **SD3 — Policy.** *keelson-only* → the published
  `introspect.LocalQueryEndpoint()`, or stay put with a hint when none is
  published. *plain-only* → the manual base URL. *none* and *unparseable* →
  the current target, no opinion; a parse failure passes through because
  the same SQL will surface a clear error where it executes (the
  `RewriteAliases` philosophy). *mixed* → refuse with a message naming the
  two sides; no guessing. [ADR-0137](./0137-query-placement-clusters-balancing.md)
  §SD5 can later upgrade *mixed* where a co-located server is proven
  reachable.

- **SD4 — Stream-shaped result path.** The resolver hands back an executor
  whose result is a sequence of frames; the HTTP binding yields a single
  frame. This is the one API decision taken ahead of need: every later
  stage (progress frames, async transport, live partial results) arrives
  as a new executor behind an unchanged seam, whereas a `[]byte`-returning
  seam would reopen the client at each stage.

- **SD5 — Two-axis dispatch label.** Classification emits sensitivity and
  execution mode as independent axes. Sensitivity constrains *placement*
  (an encrypted ad-hoc dataset never leaves loopback,
  [ADR-0134](./0134-adhoc-datasets.md)) and *transport* (what may transit
  which channel) — independently of the latency-driven sync/async choice.
  "Async" must never imply "less protected".

- **SD6 — Visible automation.** "Auto" is a third preset in the existing
  Endpoint menu, beside the manual URL and the two fixed presets. The
  toolbar shows the resolved target and the reason per run. An explicit
  manual pin always wins; the resolver never silently overrides it.
  Auto-routing only ever selects among endpoints the user already
  configured.

- **SD7 — Consumers of the decision.** The diagnostics EXPLAIN probe
  shares `buildResidual` so its verdict matches a real Run byte-for-byte;
  it must consume the routing decision too, or that guarantee silently
  dies. Applet saving (`endpoint:` stamping, today inferred from where the
  client happens to point) derives the stamp from the classification
  instead. The decision's `reason` is recorded on the run's QueryRun fact
  once [ADR-0138](./0138-streaming-query-transport-observability.md) lands,
  making dispatch auditable post-hoc.

- **SD8 — Truncation is loud (sync path).** A result capped by
  `max_result_rows`/overflow-break or an enforced `LIMIT` renders as
  *capped*, never as complete. The general frame contract (truncated vs
  died as distinct terminal states) is
  [ADR-0138](./0138-streaming-query-transport-observability.md) §SD2; this
  SD commits the sync path to the same honesty with the means it has
  (summary header, `rows_before_limit_at_least` where the format carries
  it).

## Alternatives

- **Client-side `URLPass` expansion against one endpoint.** Expand
  `keelson()` in play and send everything to the external server.
  Rejected: a remote server resolves the loopback base URL to *its own*
  loopback (garbage), a `127.0.0.1` endpoint string does not prove
  machine-locality (an SSH tunnel makes a remote server look local), and
  reachability would move sensitive loopback data across the network —
  the boundary [ADR-0094](./0094-keelson-introspection-tables.md) §SD7
  exists to prevent. Revisited under a locality *proof* in
  [ADR-0137](./0137-query-placement-clusters-balancing.md) §SD5.
- **Auto-routing by mutating the sticky URL.** Rejected: races the live
  delivery path and the schema provider mid-flight, and turns a per-query
  decision into hidden mode state.
- **Routing as a nanopass pass.** Rejected: passes are SQL→SQL rewrites
  with declared properties; routing is analysis plus policy with a
  non-SQL output. The pass pipeline stays pure; the resolver *consumes*
  analysis.
- **A resolver registry / passreg stage now.** Rejected for the first cut:
  the visible consumers (applet stamping, the later placement and
  transport resolvers) live inside play. An interface suffices; a
  registry is warranted only when a second host adopts the seam.

## Consequences

### Positive

- Keelson-referencing buffers run without a manual endpoint dance, and
  the mixed case gets an explanation instead of a server error.
- One seam absorbs the roadmap (placement, balancing, async transport)
  without reopening the client; the affinity key and frame-shaped result
  path are in place before anything needs them.
- Every automatic decision is visible and attributable (reason string;
  later, the QueryRun fact).

### Negative

- A second routing brain besides the user's mental model. Mitigated by
  SD6 (visibility, manual pin wins), not eliminated.
- One more parse per run for classification. The residual is already
  parsed for the alias rewrite when bindings exist; sharing that parse is
  an implementation option, not a commitment.

### Neutral

- The classifier's conservatism (macro-only signal) means a buffer that
  reads introspection data through hand-written `url()` still routes as
  *plain-only*. That is correct: such a buffer is addressed to a specific
  engine by construction.

## Status

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0094](./0094-keelson-introspection-tables.md) — introspection tables, `/query` endpoint, play wiring
- [ADR-0134](./0134-adhoc-datasets.md) — ad-hoc datasets, alias→handle rewrite, loopback decrypt boundary
- [ADR-0108](./0108-keelson-sql-pass-registry.md) — pre-execute pass stage the residual passes through
- [ADR-0137](./0137-query-placement-clusters-balancing.md) — placement rules, balancing, locality proof
- [ADR-0138](./0138-streaming-query-transport-observability.md) — streaming transport, run identity, frame contract
