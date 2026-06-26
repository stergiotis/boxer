---
type: adr
status: accepted
date: 2026-06-23
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-23
---

# ADR-0094: keelson runtime introspection as ClickHouse-queryable tables

## Context

keelson holds a lot of in-process runtime state, scattered across single-purpose
registries: the env-var registry ([ADR-0009](./0009-environment-variable-registry.md)),
the app manifest registry, the demo registry ([ADR-0057](./0057-demo-registry-and-drivers.md)),
the window host, build/VCS metadata, the build SBOM, sysmetrics
([ADR-0090](./0090-sysmetrics-pubsub-data-plane.md)), and more. Each is reachable
only through its own Go API. There is no uniform way to *query* this state — to
filter it, join two facets of it, or join it against other data a ClickHouse
instance already holds.

We want these facets exposed as tables a ClickHouse engine can `SELECT` and
`JOIN`, reachable two ways: by an ad-hoc `clickhouse-local`, and by a localhost
`clickhouse-server` that holds other data and wants to join keelson state into
it. Most of the interesting state exists *only inside a running process* (open
windows, live env values, sysmetrics), so the design cannot assume a separate
batch process can see it.

Precedent already exists for the mechanics, just not the abstraction: `boxer adr`
([ADR-0092](./0092-adr-overview-tool.md)) turns Go structs into Apache Arrow IPC
and queries them with `clickhouse-local`; `apps/play` speaks ClickHouse over HTTP
and renders `ArrowStream` results; the chlocal broker
([ADR-0028](./0028-chlocal-low-latency-sql-cap.md)) pools `clickhouse-local` workers
behind an audited bus capability.

## Design space (QOC)

**Question.** How should keelson expose its heterogeneous in-process runtime
state as ClickHouse-queryable tables, reachable by both a local engine and a
joining server?

**Options.**

- **O1 — Provider registry + dual transport.** A registry of table-providers,
  each emitting one Arrow table. An in-process HTTP endpoint serves each table as
  `ArrowStream` for any ClickHouse to pull via the `url()` table function; an
  in-process query path runs whole queries through the chlocal broker with
  nanopass-driven projection.
- **O2 — leeway `TableDesc` per table.** Model each facet with the data-mapping
  engine: a `TableDesc` plus generated marshalling DML per table.
- **O3 — Push / INSERT into a server.** keelson connects to a running
  `clickhouse-server` and `INSERT`s its state into real tables.
- **O4 — Replicate the `boxer adr` pattern per source.** A bespoke
  structs→Arrow→`clickhouse-local` exec, copied for each data source.

**Criteria.**

- **C1 — Reaches both a local engine and a joining server.** The two consumption
  modes we actually want.
- **C2 — No external server required.** Works on a box with only
  `clickhouse-local`.
- **C3 — Reuse of existing machinery** (chlocal broker, Arrow build, env
  redaction).
- **C4 — Per-table authoring cost.** What it takes to add the Nth table.
- **C5 — Laziness / projection for expensive tables.** Avoid materialising
  costly columns (window SVGs) or sampling side-effecting sources (`/proc`) when
  a query does not reference them.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 (registry + dual transport) | O2 (leeway TableDesc) | O3 (push to server) | O4 (per-tool exec) |
|----|--------------------------------|-----------------------|---------------------|--------------------|
| C1 | ++ (`url()` from either)        | + (heavy)             | + (server only)     | −− (no pull surface) |
| C2 | ++                             | +                     | −− (needs a server) | ++                 |
| C3 | + (broker, Arrow, redaction)   | − (gen per table)     | − (server client + insert) | + (copies adr) |
| C4 | + (one provider; reflective emitter planned) | −− (TableDesc + DML each) | − (schema + insert each) | −− (whole tool each) |
| C5 | ++ (table-prune + in-proc column-prune) | + | − | − |

## Decision

Adopt **O1**. A new package (proposed home `public/keelson/runtime/introspect`)
owns a provider registry and two transports over one shared Arrow core. The
tables live in a `keelson` namespace — **not** `system`, which ClickHouse
reserves for its own introspection tables.

- **SD1 — Provider registry + interface.** A `TableProvider` declares
  `Name() string`, `Schema() *arrow.Schema`, a freshness class (`Static` —
  process-stable; `Live` — snapshot per query), and
  `Snapshot(proj Projection) (arrow.Record, error)`, where `proj` is a column
  subset or "all". Subsystems register their provider; the registry enumerates
  them. `Schema()` doubles as the schema nanopass needs to expand `SELECT *`.

- **SD2 — Arrow materialisation core.** Per-provider schema + `RecordBuilder`
  loops, hand-rolled in the `boxer adr` idiom at first; once the shape is stable,
  extract a small reflection-driven struct→Arrow helper (struct-tag column names;
  scalars, `[]string`, nullable) reusable repo-wide. leeway is deliberately *not*
  used here — a `TableDesc` plus generated DML is too heavy for dozen-row
  introspection tables. The HTTP transport writes `ArrowStream` (stream writer);
  the in-process transport writes Arrow files for `file()` temp tables.

- **SD3 — HTTP table source.** An in-process Go `http.Server`, **loopback-bound
  by default** (the [ADR-0082](./0082-imzero2-remote-session-auth-tls.md) bind-gate
  pattern — only its loopback default is implemented here), exposing
  `GET /table/<name>` → `ArrowStream`. Any ClickHouse queries it with
  `url('http://127.0.0.1:<port>/table/<name>', 'ArrowStream')`; a server can then
  `JOIN` keelson tables against its own data. Sensitive env values are redacted
  with the existing `env.FormatValue` before serialisation.

- **SD4 — In-process projected query path.** A whole SQL query is parsed with
  nanopass (`analysis.ExtractTables` + `ExtractColumns`, with `SELECT *` expanded
  against each provider's `Schema()`), yielding the referenced tables and their
  referenced columns. Only those providers are snapshotted, and only their
  referenced columns. The projected Arrow tables are handed to the chlocal broker,
  which runs the query and returns `ArrowStream`. Projection is **best-effort and
  conservative**: any parse/analysis uncertainty falls back to all columns of the
  referenced tables. It is never a correctness dependency — `clickhouse-local`
  re-validates, and a mis-prune that dropped a needed column would be a bug, so
  "cannot prove unused → include".

- **SD5 — chlocal broker input tables.** The broker
  ([ADR-0028](./0028-chlocal-low-latency-sql-cap.md)) gains an optional
  `InputTables map[string][]byte` on `ExecRequest`: it writes each to the worker's
  tmpdir and prepends a `CREATE TEMPORARY TABLE <name> AS SELECT * FROM
  file('<name>.arrow','Arrow')` prelude. Each input table's content hash folds
  into the result-cache key, so caching stays correct when a `Live` table's bytes
  change under an unchanged SQL string. Chosen over bypassing the broker (direct
  `chlocalpool.Acquire`, which would lose the broker's cache + audit) and over
  routing the in-process path through `url()` (which would defeat projection —
  see below).

- **SD6 — Widget SQL console.** An imzero2 widget submits SQL to the SD4 path and
  renders the `ArrowStream` result, reusing `apps/play`'s ClickHouse-Arrow
  rendering.

- **SD7 — Security defaults.** Loopback bind + sensitive-value redaction are the
  v1 boundary. Non-loopback exposure (bearer token, TLS) is deferred to the
  unbuilt [ADR-0082](./0082-imzero2-remote-session-auth-tls.md) design rather than
  re-derived here.

- **SD8 — v1 table catalogue.** v1 ships the cheap, mostly-static facets and
  table-level laziness:

  | table | source | freshness |
  |-------|--------|-----------|
  | `keelson.env` | env registry `All()` + live `Get()` (redacted) | specs Static / values Live |
  | `keelson.apps` | app `AllManifests()` | Static |
  | `keelson.demos` | demo `registry.All()` | Static |
  | `keelson.build` | `runinfo` + `vcs` (one row) | Static |
  | `keelson.sbom` | licensegate CycloneDX | Static |
  | `keelson.windows` | window host `OpenWindows()` (metadata only) | Live |

  Deferred to v2: `window_screenshots` (the SVG blob — the column whose cost
  actually justifies column-level projection), `sysmetrics`/`procs`, `help_docs`,
  and the audit `facts`.

## Alternatives

- **leeway `TableDesc` per table (O2).** Rejected: leeway is the right tool for
  durable, evolving schemas with round-trip codecs, not for a dozen small,
  read-only introspection snapshots. Standing up a `TableDesc` and generated DML
  per facet is disproportionate authoring cost (C4) for data that a hand-rolled
  builder emits directly.

- **Push / INSERT into a server (O3).** Rejected: it makes a running
  `clickhouse-server` a hard dependency (fails C2), inverts the freshness model
  (state must be pushed and goes stale between pushes), and still needs a server
  client and insert path keelson does not have.

- **Replicate `boxer adr` per source (O4).** Rejected: no shared core, every
  source reinvents schema + exec, and the result is reachable only by the tool
  that built it — a joining `clickhouse-server` (C1) has nothing to pull.

- **`url()` for the in-process path too.** Tempting for uniformity (one
  transport: the widget would also query via `clickhouse-local` + `url()` against
  keelson's own endpoint). Rejected for the projected path: the `url()` table
  function performs **no projection or predicate pushdown** to an arbitrary HTTP
  source — ClickHouse fetches the whole table payload and filters locally. Routing
  the in-process path through it would discard the nanopass projection that is the
  point of SD4. `url()` remains exactly right for *external* consumers (SD3),
  where the query is ClickHouse's to own.

## Consequences

### Positive

- One mechanism reaches both consumption modes: `clickhouse-local` and a joining
  `clickhouse-server` query the same `url()` endpoint; the in-process widget gets
  a projected fast path.
- Adding a table is one `TableProvider`; expensive facets stay lazy because an
  unreferenced table is never snapshotted (and, in URL mode, never even fetched).
- Reuses the chlocal broker, the Arrow build idiom, and env redaction rather than
  new infrastructure.

### Negative

- Column-level projection only bites on the in-process path. Every *external*
  ClickHouse consumer fetches whole table payloads via `url()` — so the v2 SVG
  blob and deep sysmetrics need either an opt-in `?cols=` hint we serve, or
  acceptance that a `SELECT key FROM url('…/windows')` still renders every
  screenshot. Table-level laziness is the only automatic lever in URL mode.
- The broker grows an `InputTables` surface and a content-hashed cache key (SD5);
  small, but it widens an audited capability.
- nanopass earns little on v1's cheap tables (materialising all columns is free
  and ClickHouse prunes after load). v1 carries the projection machinery mostly to
  establish it for the v2 expensive tables.

### Neutral

- The table namespace is `keelson`, not `system` (reserved by ClickHouse).
- `Live` providers snapshot at query time; a short coalescing TTL so a burst of
  queries shares one snapshot is possible later, not in v1.
- The widget console overlaps `apps/play` (a ClickHouse SQL playground), but play
  targets an external server over HTTP whereas the console targets the in-process
  SD4 path; shared rendering, different backends.

## Status

Accepted (2026-06-23). v1 is implemented and wired into the carousel host —
§SD1–§SD5, §SD8, plus the §SD3 HTTP table source and §SD4 in-process engine;
`url()` queries (including a two-table join) are verified against
clickhouse-local 26.5. The §SD6 console widget is deferred to interactive,
GUI-verified work; per-package `package_props` seeding awaits a TinyGo 0.41.1 box.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## Update (2026-06-27) — wiring `apps/play` to the `/query` endpoint

`apps/play` (the external-over-HTTP SQL playground) could not query keelson
tables: it never expanded the `keelson('…')` macro and pointed at an external
ClickHouse, so the macro reached that server verbatim and errored. The §SD3
`/query` endpoint was already built to be play-compatible (POST SQL body →
`RewriteToURL` → broker → ArrowStream), but nothing started it where play runs,
and play was never pointed at it. This update closes that gap; it does not
change any §SD decision.

- **In-process is mandatory; no standalone daemon.** The providers read *live*
  in-process state — `keelson.windows` is the running window host, `keelson.env`
  values are this process's live `Get()`, and the registries are in-process. A
  separate OS process would report its own empty state, so a standalone
  `introspect serve` was considered and rejected (it could only serve the static
  `build`/`sbom` facets, and would mislead on the live ones). A cross-process
  variant would require exporting GUI state over NATS (the
  [ADR-0090](./0090-sysmetrics-pubsub-data-plane.md) pattern), which §SD2
  deliberately did not adopt. The table source therefore runs in the GUI host's
  own process, where `apps/play` is already co-resident.

- **Reusable host hook (`introspecthost.Start`).** The carousel's inline startup
  block is lifted into `keelson/runtime/introspect/introspecthost`, so any
  keelson GUI host stands the table source up with one call (registry build +
  optional chlocal-broker `/query` runner + bind + diagnostic). It lives above
  `introspecthttp` because it needs the bus and broker, which `introspecthttp`
  deliberately does not import. A new `KEELSON_INTROSPECT_ENABLE` env var
  (default on) gates it; `KEELSON_INTROSPECT_HTTP_LISTEN` still sets the bind
  address.

- **Endpoint discovery (`introspect.LocalQueryEndpoint`).** The hook publishes
  the bound `/query` URL via a process-global, but only when the endpoint is
  backed by a runner (an unbacked `/query` answers 503, so advertising it would
  be a foot-gun). A co-resident app reads it without a hard-coded port (the
  server binds an ephemeral port by default).

- **`apps/play` switchable destination.** The `Client` target is now mutable
  under a lock (`URL`/`SetURL`, read once per request), and the toolbar gains an
  "Endpoint" menu: a manual URL plus a "Keelson introspection" preset (shown only
  when `LocalQueryEndpoint` is non-empty) and an "External (reset)". No protocol
  change — play talks to `/query` exactly as it talks to ClickHouse.

- **`/query` HTTP-interface parity.** To make play work against `/query`
  unchanged, the handler now emits a minimal `X-ClickHouse-Summary`
  (`result_bytes`/`elapsed_ns`; the broker surfaces no read counters, so those
  stay 0) and rejects `param_*` query-string keys with a clear 400 rather than
  mis-running an unbound placeholder (parameter binding through the in-process
  runner is out of scope). The wire subset that matters here — POST body,
  `X-ClickHouse-*` headers, status codes, summary header — was cross-checked
  against a sibling ClickHouse-Arrow-to-JSON proxy that speaks the same dialect.

The §SD6 console widget remains deferred; this update gives play (a separate
backend by §Consequences) the same reach without building that widget.

## References

- [ADR-0009 — environment variable registry](./0009-environment-variable-registry.md)
- [ADR-0028 — chlocal low-latency SQL capability (broker)](./0028-chlocal-low-latency-sql-cap.md)
- [ADR-0057 — demo registry and drivers](./0057-demo-registry-and-drivers.md)
- [ADR-0082 — remote session auth/TLS (bind-gate, token, TLS)](./0082-imzero2-remote-session-auth-tls.md)
- [ADR-0084 — nanopass ANTLR DFA cache bounding](./0084-nanopass-antlr-dfa-cache-bounding.md)
- [ADR-0090 — sysmetrics pub/sub data plane](./0090-sysmetrics-pubsub-data-plane.md)
- [ADR-0092 — ADR overview tool (structs→Arrow→clickhouse-local precedent)](./0092-adr-overview-tool.md)
</content>
</invoke>
