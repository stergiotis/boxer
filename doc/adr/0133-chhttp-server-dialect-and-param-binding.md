---
type: adr
status: accepted
date: 2026-07-19
reviewed-by: "@spx"
reviewed-date: 2026-07-19
---

# ADR-0133: `chhttp` — the server-side ClickHouse HTTP dialect, and parameter binding for the in-process `/query`

## Context

The in-process introspection endpoint ([ADR-0094](./0094-keelson-introspection-tables.md))
serves `POST /query` closely enough to ClickHouse's HTTP interface that
`apps/play` talks to it unchanged — with recorded gaps, restated by
[ADR-0132](./0132-sqlapplet-sql-defined-applets.md) §SD7 as this follow-up:

- **No parameter binding.** A `param_*` query-string key is rejected up
  front with a clear 400 (better than the confusing deeper failure, but
  still a refusal). Everything that binds `{name:Type}` placeholders is cut
  off: play's param slots ([ADR-0124](./0124-play-param-editing-widgets.md)),
  the Schema tab (which queries with `param_tbl`/`param_db`), the
  Diagnostics EXPLAIN probe over a parameterized buffer, and — the ADR-0132
  restriction — any parameterized sqlapplet on `endpoint: introspection`.
- **Read counters are hardwired zero.** `X-ClickHouse-Summary` carries only
  `result_bytes`/`elapsed_ns`.
- **No progress headers** stream while a statement runs.
- **The error envelope is ad-hoc** (`http.Error` with whatever the broker's
  error string holds), while play's probe classifier parses the server
  shape (`Code: N. DB::Exception: …`).

The dialect knowledge is also starting to duplicate: `introspecthttp`
implements request parsing (POST body / `?query`), format→content-type
mapping, and the summary header inline, and the sibling
ClickHouse-Arrow-to-JSON proxy that ADR-0094's parity work was
cross-checked against implements the same shapes again. A second consumer
already exists; the surface is about to grow.

The transport facts that constrain the design
([ADR-0028](./0028-chlocal-low-latency-sql-cap.md)):

- Workers are **pre-spawned, warm, blocked on stdin, and single-use** — the
  ~8 ms Acquire path is the pool's point, and argv is fixed at spawn time.
- Multi-statement stdin injection is proven machinery: the ADR-0094 §SD5
  `InputTables` support prepends `CREATE TEMPORARY TABLE …;` statements to
  the submitted SQL, with each table's bytes folded into the result-cache
  key.
- `ExecRequest.Settings` exists but is declared reserved and ignored; the
  cache key already canonicalises a settings fold (sorted keys).

## Design space (QOC)

**Question.** How do parameterized statements reach the in-process runner
without giving up the warm worker pool, and which parts of the server-side
ClickHouse HTTP dialect are worth extracting as a reusable package?

**Parameter channel options.**

- **O1 — SET-prelude injection in the broker.** `ExecRequest` gains
  `Params map[string]string`; the broker prepends one
  `SET param_<name> = '<value>';` statement per entry (the `InputTables`
  prelude precedent, same quoting helper), after the cacheability prefix
  gate, with each pair folded into the cache key.
- **O2 — Per-request argv (`--param_<name>=…`).** clickhouse-local's native
  flag form.
- **O3 — Endpoint-side literal substitution.** Textually replace
  `{name:Type}` with a typed literal before submitting.
- **O4 — Activate the reserved `Settings` field.** Forward params through a
  `SETTINGS` clause.

**Extraction options.**

- **OA — Extract `chhttp` now**, `introspecthttp` consumes it.
- **OB — Keep the dialect inline** until a third consumer appears.
- **OC — Reverse-proxy to a real server.**

**Criteria.** C1 preserves the warm pool; C2 cache-key correctness; C3
server fidelity (typed substitution stays ClickHouse's); C4 reuse across
the existing consumers; C5 implementation effort.

**Assessment.** O2 fails C1 outright — argv is fixed when a warm worker is
spawned, so per-request params would force cold spawns and forfeit the pool.
O3 fails C3: it re-creates the client-side literal-encoding surface that
server-side binding exists to avoid (play deliberately has none,
ADR-0097 slice 5). O4 is a namespace error — parameters are not settings,
and a `SETTINGS` clause cannot bind a `{name:Type}` placeholder. O1 meets
C1/C2/C3 with the smallest new mechanism. On extraction, OB concedes
ongoing duplication against a consumer that already exists, and OC solves a
different problem — the providers read live in-process state, which is why
the endpoint is in-process at all (ADR-0094 Update 2026-06-27).

## Decision

Adopt **O1** for parameters and **OA** for the extraction.

- **SD1 — `chhttp`, the server-side dialect package.** A new package
  (proposed home `public/db/clickhouse/chhttp`) owns the reusable halves of
  "answer HTTP like a ClickHouse server":

  *Request side:* statement extraction (POST body, `?query` fallback),
  `param_*` harvest with name validation, and settings classification — a
  known-ignorable list (`log_comment`, `readonly`,
  `send_progress_in_http_headers`, …) with unknown settings ignored, never
  rejected, mimicking server tolerance for clients that stamp or harden
  their requests (play's `log_comment`; the ADR-0132 §SD5 `readonly`
  enforcement).

  *Response side:* the `X-ClickHouse-Summary` builder, format-tail →
  `Content-Type` mapping, and the exception envelope — status code plus a
  `Code: N. DB::Exception: …` body and `X-ClickHouse-Exception-Code`
  header, the shape play's probe classifier already parses.

  Deliberately **not** in scope: the client half (play's `Client` stays
  where it is — different concerns, no shared code today), auth/TLS
  (deferred to [ADR-0082](./0082-imzero2-remote-session-auth-tls.md)), and
  bind policy (each host keeps its own loopback gate).

- **SD2 — The parameter channel.** `ExecRequest.Params map[string]string`.
  The broker validates names against the identifier charset the input
  tables already enforce, injects `SET param_<name> = <quoted>;` lines
  ahead of the InputTables prelude at submit time — *after* the
  cacheability prefix gate, so a parameterized SELECT stays cacheable — and
  folds each sorted `name=value` pair into the result-cache key (the
  settings-canonicalisation precedent). Values stay strings; typed
  substitution of `{name:Type}` remains the engine's job, exactly as on the
  real HTTP interface. Quoting reuses the tested `sqlQuoteString`; a
  malformed name is a structured broker error, not a worker failure.

  **Verification item:** `SET param_<name>` inside a clickhouse-local
  multi-statement script, against the pinned server line (26.x). If it were
  refused, the recorded fallback is cold-spawn-with-argv for param-carrying
  requests only (the warm path stays untouched for param-less traffic) —
  slower, but correct, and invisible in the interface.

- **SD3 — `introspecthttp` adopts `chhttp` and lifts the refusal.**
  `handleQuery` routes `param_*` into `ExecRequest.Params` instead of
  rejecting, ignores known-ignorable settings, and emits errors through the
  chhttp exception envelope. The `/query` parity note in ADR-0094 and the
  `EndpointIntrospection` restriction comments in `apps/sqlapplet` are
  updated in the same change — parameterized introspection applets become
  legal, closing the ADR-0132 §SD7 gap.

- **SD4 — Counters and progress stay honest.** `read_rows`/`read_bytes`
  remain 0: the CLI transport surfaces no counters, and inventing them
  would be worse than omitting them. Progress headers stay absent until the
  broker's reserved streaming milestone exists — a buffered reply has no
  mid-flight to report. Both are recorded limits, not oversights; play
  degrades exactly as it does today (the status bar simply shows fewer
  numbers against this endpoint).

- **SD5 — Consumers unlocked, in order.** play's param slots and Live
  signals work against the introspection endpoint; the Schema tab's
  `param_tbl`/`param_db` probes start answering; the Diagnostics probe can
  EXPLAIN a parameterized buffer; and sqlapplet's `endpoint: introspection`
  docs may carry params — the topology suite
  ([ADR-0126](./0126-appliance-topology-as-data.md)) becomes fully
  appletizable without an external server.

## Alternatives

- **Per-request argv (O2).** Killed by the worker lifecycle: warm workers
  are spawned before the request exists. Survives only as SD2's recorded
  fallback, scoped to param-carrying requests.
- **Endpoint-side literal substitution (O3).** Killed: client-side literal
  encoding is the surface ADR-0097 deliberately refused to build; typed
  substitution belongs to the engine on both transports.
- **Forward params as settings (O4).** Killed: wrong namespace — a setting
  cannot bind a placeholder, and conflating the two would leak into the
  cache-key semantics the settings fold already defines.
- **Keep the dialect inline (OB).** Rejected: the second consumer exists,
  and every gap closed here (params, envelope, settings tolerance) would
  otherwise be closed twice.
- **Reverse-proxy to a real server (OC).** Rejected: the providers read
  live in-process state; a proxy target could not see it (the standing
  ADR-0094 finding).

## Consequences

### Positive

- Parameter binding reaches the in-process path with the warm pool intact,
  and the cache stays correct under changing param values.
- One implementation of the server-side dialect, shared by the endpoint and
  available to the sibling proxy; the error envelope becomes uniform and
  probe-classifiable.
- The ADR-0132 introspection restriction lifts: parameterized applets over
  keelson tables need no external ClickHouse.

### Negative

- `chhttp` is a new public surface whose fidelity to the real server must
  be maintained as ClickHouse's interface evolves.
- The SET-prelude bet carries an empirical verification item (SD2); its
  fallback costs cold spawns for parameterized requests.
- The result-cache key grows another fold dimension; a bug there would
  serve stale results across param changes (mitigated by mirroring the
  tested InputTables fold).

### Neutral

- The client half of the dialect deliberately stays in `apps/play`;
  unification has no consumer today.
- Counters and progress remain partial until the streaming milestone; the
  status bar against this endpoint shows fewer numbers, not wrong ones.

## Status

Accepted (2026-07-19). Implementation slices: **M1** the
`chhttp` package plus tests pinning the request/response shapes;
**M2** `ExecRequest.Params` — validation, cache fold, prelude injection,
and the SD2 verification against the pinned server; **M3** `introspecthttp`
adoption plus the play/sqlapplet ride-along comment and doc updates.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.

## Update (2026-07-19) — M1 shipped; the SD2 verification item is retired

**M1** landed: `public/db/clickhouse/chhttp` with tests pinning the request
and response shapes (statement extraction errors on oversize rather than
silently truncating — a deliberate divergence from the pre-extraction
endpoint; the FORMAT-tail mapping is carried over verbatim so the M3
adoption is a drop-in). No consumer changes yet.

**SD2's empirical check passed** against the installed clickhouse-local
(26.6): `SET param_lim = 5; SELECT {lim:UInt64} + 1` submitted as a
multi-statement stdin script substitutes correctly. The SET-prelude
injection therefore proceeds as designed and the cold-spawn argv fallback
is not needed.

## Update (2026-07-19) — M2 and M3 shipped

**M2**: `ExecRequest.Params` on the chlocal broker — names validated
against the input-table charset, the SET-prelude injected after the
cacheability prefix gate, and each sorted pair folded into the result-cache
key. Implementation note beyond the ADR text: the params fold carries a
domain tag so `Params{a:b}` and `InputTables{a:"b"}` cannot alias to one
cache key. End-to-end tests bind against a live clickhouse-local and pin
cache hit/miss across changed bindings.

**M3**: `introspecthttp` adopts chhttp for the whole wire dialect and the
`QueryRunner` seam carries the bindings to the broker; the up-front
`param_*` rejection is gone, exceptions ride the `Code: N` envelope with
`X-ClickHouse-Exception-Code`, and an end-to-end test binds a `LIKE`
pattern through the full self-referential loop (URL param → broker prelude
→ clickhouse-local → `url()` back to the same server). Ride-alongs landed
in ADR-0094 (parity note), ADR-0132 (§SD7 lifted; `runtime-env` gains a
`pattern` parameter), and the sqlapplet endpoint comments.

## References

- [ADR-0094 — keelson introspection tables](./0094-keelson-introspection-tables.md)
  — the endpoint, its `/query` parity update, and the InputTables prelude
  this design mirrors.
- [ADR-0028 — chlocal low-latency SQL capability](./0028-chlocal-low-latency-sql-cap.md)
  — the warm single-use worker pool that rules out per-request argv.
- [ADR-0132 — sqlapplet](./0132-sqlapplet-sql-defined-applets.md) — §SD7
  records this follow-up; §SD5's `readonly` enforcement lands on the
  settings-tolerance list.
- [ADR-0097 — play as a reactive query-graph](./0097-play-reactive-query-graph.md)
  — the wire contract (`param_*` on the URL, `FORMAT ArrowStream` tail) and
  the no-client-side-literals stance.
- [ADR-0124 — param editing widgets](./0124-play-param-editing-widgets.md)
  and [ADR-0126 — topology as data](./0126-appliance-topology-as-data.md)
  — the consumers §SD5 unlocks.
- `public/keelson/runtime/introspect/introspecthttp/server.go`,
  `public/keelson/data/chlocalbroker`, `public/keelson/data/chlocalpool`,
  `apps/play/play_client.go` — the seams this ADR re-reads.
