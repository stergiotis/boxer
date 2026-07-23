---
type: adr
status: accepted
date: 2026-07-20
reviewed-by: "@spx"
reviewed-date: 2026-07-20
---

# ADR-0134: Ad-hoc datasets — capability-mediated ephemeral data for SQL applets

## Context

A SQL applet consumes data exclusively through its endpoint
([ADR-0132](./0132-sqlapplet-sql-defined-applets.md)): the env-configured
ClickHouse, or the in-process introspection endpoint where `keelson('…')`
tables live ([ADR-0094](./0094-keelson-introspection-tables.md)). What no
seam serves today is an app that *generates* tabular data at runtime — a
computation result, a scrape, a derived table — and wants an applet
(possibly embedded in its own GUI, the ADR-0132 §SD8 embedder shape) to
render and explore it. The only current routes are real ingestion into
ClickHouse — durable by design, the
[ADR-0089](./0089-rowdml-serialization-clickhouse-native-ingestion.md)
path, wrong for data that should die with the session — or bespoke Go panels, which forfeit the
applet definition discipline entirely.

The introspection stack almost stretches to this. A `Provider` snapshots
in-memory rows as Arrow per query; the chlocal broker binds snapshots as
`TEMPORARY` tables; the `keelson('…')` macro hides the transport. But three
properties block it as the app-data channel:

- the provider registry is **append-only** — no unregister, no replace — and
  a registered name is durably public, so per-session or per-instance
  datasets leak names or collide;
- capacity is bounded by process RSS, and every query pays a full snapshot
  copy;
- nothing is confidential at rest: the broker already materialises input
  tables as **plaintext** Arrow files under its temp directory during
  execution, removed only at handler return — a crash strands plaintext on
  disk.

The design driver is an **ephemerality requirement**: ad-hoc data must not
outlive the session — neither discoverable nor readable after a restart.
Boxes are imaged and backed up; the appliance line stores on SD media under
`/perm` ([ADR-0128](./0128-imzero2-mesh-draw-stream-codec-lane.md) records
the gokrazy probe). Cleanup-on-exit alone cannot carry that guarantee,
because a crash skips it; only cryptography can make the guarantee
unconditional.

Grounding facts, verified against clickhouse-local 26.6:

- `file('<path>','ArrowStream','<structure>')` **reads from a named pipe**,
  including in the production shape
  `CREATE TEMPORARY TABLE t AS SELECT * FROM file(…)` inside a
  multi-statement script. Plaintext can therefore transit through a kernel
  pipe buffer without ever existing on any filesystem.
- Schema inference on a pipe **fails** (inference consumes the stream and
  cannot re-read), so an explicit structure string is mandatory on this
  path.
- ClickHouse offers **no read path for externally encrypted files**: the
  `encrypted` disk type encrypts ClickHouse's own table storage (keys in
  server config, unauthenticated AES-CTR), and Parquet modular encryption
  is unsupported by the reader
  ([ClickHouse#64965](https://github.com/ClickHouse/ClickHouse/issues/64965)).
  Decryption must happen on our side of the `file()` boundary.

## Design space (QOC)

**Question.** How does a running app hand ephemeral tabular data to SQL
applets — named, queryable, JOINable with other `keelson` tables — without
creating durable state, durable public names, or plaintext at rest?

**Options.**

- **O1 — In-memory live providers.** Register an ordinary
  `FreshnessLive` provider over the app's state; snapshot per query.
- **O2 — Encrypted on-disk store, capability-managed keys, ephemeral
  handles, streaming-decrypt broker.** Datasets are chunk-encrypted Arrow
  files; keys live only in memory; access is a granted handle; the chlocal
  layer decrypts in a stream at query time.
- **O3 — Per-request external data over the wire.** Extend the chhttp
  dialect ([ADR-0133](./0133-chhttp-server-dialect-and-param-binding.md))
  with ClickHouse's multipart external-data mechanism; attach tables to
  each request.
- **O4 — Ingest into the default ClickHouse.** Write the data as real
  rows; the applet queries them like any table.

**Criteria.**

- **C1 — Ephemerality**: unreadable and undiscoverable after restart,
  crash included.
- **C2 — Capacity**: datasets larger than the process can comfortably hold.
- **C3 — Naming/lifecycle hygiene**: no durable public names, no leak on
  repeated open/close.
- **C4 — Reuse**: rides the shipped ADR-0094/0133 machinery.
- **C5 — Mediation and audit**: who published, who was granted, on record.
- **C6 — Query-path cost**: copies, transmission, cacheability.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 in-memory | O2 encrypted store | O3 wire external data | O4 ingestion |
|----|--------------|--------------------|-----------------------|--------------|
| C1 | ++ (never on disk) | ++ (by construction) | + (nothing at rest) | −− |
| C2 | −−           | ++                 | − (payload per request) | ++         |
| C3 | −−           | ++ (handles)       | ++ (no names at all)  | −            |
| C4 | ++           | ++                 | − (new dialect, both halves) | ++     |
| C5 | −            | ++                 | +                     | −            |
| C6 | − (full copy per query) | + (stream; revision-keyed cache) | −− (re-transmit per query) | ++ |

## Decision

Adopt **O2**: an ad-hoc dataset store of encrypted Arrow files with
capability-managed in-memory keys, ephemeral `keelson('…')` handles, and an
encryption-aware chlocal layer that decrypts in a stream at query time.

- **SD1 — The store.** A dataset is one chunk-encrypted Arrow IPC file
  under a runtime-owned directory (on gokrazy: beneath `/perm`, the only
  writable disk). Encryption is a segmented-AEAD scheme (per-chunk tag,
  sequence-bound nonces, an explicit final-chunk marker — the STREAM
  construction shape) so decryption authenticates incrementally at constant
  memory. Each dataset gets a fresh random key held **only in process
  memory**; nothing key-shaped is ever written. Publishing an existing
  dataset **replaces** it wholesale and bumps its revision — there is no
  append in v1. The publish gate enforces quotas (per-dataset bytes, total
  bytes, dataset count) and a **bounded column type set**, so the
  Arrow-to-ClickHouse structure mapping (SD3 needs it explicitly) is total
  over what the store can contain: an unsupported type is refused at
  publish, never discovered at query time. The runtime deletes all store
  files on orderly exit and sweeps leftovers at startup; the guarantee does
  not rest on either — after a crash the files are ciphertext whose key no
  longer exists. This store is deliberately **not** `StorageI`: that is the
  sanctioned durable-state path, and this one is anti-durable by design.

- **SD2 — The capability.** A new audited service owns policy: `publish`
  (validate, encrypt, write, mint a handle, register, catalog),
  `grant` (hand a requesting app the handle for a dataset), and `retract`
  (unregister, delete the file, drop the key) — request/reply subjects in
  the [ADR-0026](./0026-app-runtime-and-capability-subjects.md) taxonomy.
  A handle is an unguessable identifier that is also a valid `keelson`
  table name (e.g. `adhoc_<random>`), minted per publish; handles are the
  only way a query names a dataset, so nothing about the namespace is
  durable. Key custody splits by role: the capability is the **policy
  owner**, the chlocal broker is the **decrypt executor** — at publish the
  capability registers `handle → key` with the broker, requests never carry
  key material, and retract deregisters both. The stance is ADR-0026's
  hygiene-not-security: the threat model of the cryptography is the
  **disk** (backups, images, removed media, crash residue), not
  intra-process isolation — any code in the runtime process can reach the
  keys, and the catalog may show handles to the operator surface. Grants
  are audited, not enforced; engine-level grant tokens are recorded as a
  deferral (trigger: the broker leaving the process, or a genuinely
  multi-principal deployment).

- **SD3 — Encryption-aware chlocal.** `ExecRequest` grows an
  encrypted-inputs map beside `InputTables`:
  `name → {path, structure, revision}` (the key resolves broker-side per
  SD2). The broker materialises these as **named pipes**: `mkfifo` in the
  per-request directory, a writer goroutine streaming chunked-AEAD
  decryption into the pipe (opened `O_RDWR` so the open never blocks;
  closing yields EOF), and a prelude line
  `CREATE TEMPORARY TABLE <name> AS SELECT * FROM
  file('<fifo>','ArrowStream','<structure>')` — the shape verified against
  clickhouse-local 26.6. Plaintext exists only in kernel pipe buffers, at
  no point as a whole copy in any process or on any filesystem. A writer
  error (authentication failure, truncation, cancellation) aborts the
  request; the honest limit: the worker consumes verified prefix chunks
  before a truncation is detectable, so plaintext prefix rows do transit —
  but the request fails as a whole and no partial result surfaces. The
  cache key folds `(handle, revision)` instead of hashing payload bytes,
  which is cheaper and makes same-revision re-queries legitimate cache
  hits — an improvement over the never-cache discipline snapshot providers
  force. v1 confines ad-hoc handles to the **in-process engine route**;
  the HTTP table source does not serve them (a clear error instead), so
  exactly one decrypt path exists and plaintext never rides HTTP.

- **SD4 — The SQL surface and the applet binding.** Buffers reference a
  stable **alias** — `keelson('items')` — never a raw handle. An applet
  document declares its dependencies in frontmatter (`datasets: [items]`),
  and the alias→handle **binding is instance state**, applied by the
  embedder between construction and mount exactly like tab bindings
  (ADR-0132 §SD4), implemented as a rewrite before the engine's
  table-reference analysis so the §SD5 security classification and static
  analysis keep seeing a plain `keelson` read (the class stays `read`;
  ad-hoc data flows *into* the engine and widens no egress). The cost,
  stated rather than worked around: for dataset applets,
  pasteable-complete weakens to **pasteable-modulo-grant** — the SQL text
  cannot carry the authority, that being the point of mediation — and a
  standalone launch without a grant fails visibly through the kept status
  bar, the ADR-0132 §SD6 stance. The corpus gate treats declared aliases
  as valid-by-declaration.

- **SD5 — Freshness.** A publish-replace bumps the dataset revision; the
  alias binding registers that revision as a **signal**, so a Live applet
  re-runs on republish through the ordinary
  [ADR-0097](./0097-play-reactive-query-graph.md) signal model — no new
  reactivity machinery, and an explicit Run always works regardless.

- **SD6 — The catalog.** A `keelson('adhoc')` introspection provider lists
  the store: handle, alias hint, publisher app id, rows, bytes, revision,
  created-at — the ADR-0094 self-observation stance applied to this
  feature, and the operator's way to explore what ad-hoc data exists right
  now.

- **SD7 — Embedded applet surfaces.** The motivating publisher is an
  embedder app hosting an applet **document** rather than a raw SQL
  string: the minted host's construction (attenuation, toolbar, bindings)
  is factored into an embedder-facing constructor, so the ADR-0132 §SD8
  graduation keeps every §SD1 invariant — the buffer stays committed,
  gated, and classified. The embedder publishes, receives a handle, and
  binds the alias pre-mount in the same window where tabs attenuate. An
  applet's declared capabilities ride the **embedder's** manifest
  (capslock sees only the host — §SD8 unchanged). Open fork: query
  attribution for embedded instances — the lean is a composed identity
  (embedder app id carrying the applet slug) so per-applet slicing
  survives embedding; the alternative is embedder-only attribution.

- **SD8 — Deployment.** The design was checked against the gokrazy
  appliance shape, where it strengthens: `/tmp` is tmpfs and the boot path
  configures **no swap**, so even the fallback file transit can never
  reach persistent media, and key pages cannot be paged out; the store
  lives under `/perm` (ext4), where ciphertext-only-at-rest is exactly
  right for SD media that outlives sessions; clickhouse-local ships like
  the other non-Go binaries (`ExtraFilePaths`), resolved through the
  extbin chokepoint ([ADR-0118](./0118-extbin-external-process-chokepoint.md)) —
  whether it rides the A/B root images or parks under `/perm` is deferred
  to the ADR-0128 M3 boot probe. The genuine constraint is RAM budgeting,
  not architecture: `/tmp` defaults to half of RAM and worker scratch
  shares it, so appliance profiles tune `MaxMemoryPerWorker` and
  `MaxConcurrent` down. On desktops, tmpfs `/tmp` covers the transit
  fallback, and the two bridges from RAM to persistent media each get a
  line: **swap** is a box-level concern (encrypted swap), recorded, not
  solved here; **core dumps** are closed at startup — the runtime
  disables dumpability (`RLIMIT_CORE=0` / `PR_SET_DUMPABLE=0`), since a
  stock systemd-coredump would otherwise write crashed-process memory —
  keys and decrypted buffers included — to disk. Plain Go panics do not
  dump core; the exposure is FFI and `unsafe` faults. On gokrazy the
  read-only root makes the default `core_pattern` a no-op regardless.

- **SD9 — Naming (open).** Proposed: concept **ad-hoc dataset**; package
  `public/keelson/runtime/adhocdata`; capability subjects
  `adhoc.publish` / `adhoc.grant` / `adhoc.retract`; handle prefix
  `adhoc_`; catalog table `keelson('adhoc')`; frontmatter key `datasets:`.
  All are open to review — the constraint is only that handles and the
  catalog name satisfy the `keelson` identifier rule.

## Alternatives

- **In-memory live providers (O1).** Not enough on its own: capacity is
  process RSS, every query pays a full snapshot copy, and the append-only
  registry with durable names is precisely the lifecycle this feature must
  avoid. Not killed either — plain providers remain the right shape for
  system introspection, and O2 *rides* the same registry, macro, and
  broker; it adds a second, encrypted-file-backed entry kind rather than a
  parallel universe.

- **Per-request external data over the wire (O3).** Deferred, not killed.
  ClickHouse's HTTP dialect supports multipart external data natively, and
  a chhttp extension would serve two things O2 does not: ad-hoc data
  JOINed server-side on the **default** endpoint, and a broker that has
  left the process. Neither has a witness today, and the per-request
  re-transmission cost is real. Trigger: either witness appearing.

- **Ingestion (O4).** The boundary, not a competitor: data that *should*
  outlive the session is ingestion (ADR-0089), and nothing here touches
  that path. An ad-hoc dataset an operator decides to keep is exported by
  running the query and ingesting its result — an explicit act, not a
  default.

- **ClickHouse-native encryption (O5).** Killed on verified facts: the
  `encrypted` disk type encrypts ClickHouse's own table storage through
  the disk abstraction — not `file()` reads of foreign files — with keys
  in worker config files and unauthenticated CTR; encrypted Parquet has no
  reader-side key provisioning
  ([ClickHouse#64965](https://github.com/ClickHouse/ClickHouse/issues/64965)).
  Both put keys or trust in the wrong place even where they apply.

- **A `StorageI`-backed store.** Rejected: `StorageI` is the sanctioned
  path for state that must survive — persistence-backed, enumeration-free,
  value-shaped. This feature's defining property is the opposite; using
  the durable facility to hold deliberately non-durable data would make
  the ephemerality guarantee depend on backend behaviour instead of on
  key destruction.

- **Fixed per-app dataset names instead of handles.** Rejected: names
  become durably public the day an applet references them, re-opens
  collide with the append-only registry, and there is no mediation moment
  at which access can be audited. Handles make the namespace ephemeral by
  construction and give the audit trail a subject.

- **Enclave key storage (memguard-style mlocked pages outside the GC
  heap).** Rejected: it genuinely protects the key's own storage —
  locked, dump-excluded, wipeable — but the protection ends at the
  stdlib-crypto boundary, where `aes.NewCipher` expands the key into
  GC-heap round-key schedules no caller can erase. Under a threat model
  that claims only the disk, the added dependency buys nothing the SD8
  dumpability and swap lines do not.

- **File-based transit on tmpfs as the primary path.** Recorded as the
  fallback, not the default: it is zero new code (the broker's temp-dir
  default already lands on tmpfs where `/tmp` is one), but it costs a full
  materialised plaintext copy in RAM per query and leaves a
  crash-window file on systems where `/tmp` is a real disk. The pipe path
  is constant-memory, filesystem-free, and now verified.

## Consequences

### Positive

- An app hands tabular data to an applet with zero durable residue: no
  rows in ClickHouse, no plaintext on disk, no name that outlives the
  session — and after a crash, ciphertext without a key.
- Capacity moves off process RSS onto disk, while the query path gets
  *cheaper*: streaming decrypt at constant memory, and revision-keyed
  cache hits where snapshot providers could never cache.
- The applet definition discipline survives embedding: committed, gated,
  classified documents plus an out-of-band grant — no second authoring
  surface, no Go-owned SQL strings.
- The appliance deployment strengthens the guarantee for free (tmpfs
  `/tmp`, no swap, `/perm` ciphertext).

### Negative

- New surface to maintain: a capability service, a broker materialisation
  strategy with real concurrency obligations (writer goroutines, abort
  paths), a second registry entry kind, and an Arrow→ClickHouse structure
  mapping.
- Cryptography enters the trust base, with honest residuals: key erasure
  is best-effort — Go can overwrite buffers it owns, but cannot enumerate
  or erase runtime-made copies (moved goroutine stacks, stdlib round-key
  schedules), which threatens the disk guarantee only through the swap
  and core-dump bridges SD8 closes — and a worker consumes verified
  plaintext prefix chunks before a truncation aborts the request.
- Pasteable-complete weakens to pasteable-modulo-grant for dataset
  applets; the escape-hatch paste runs only where a grant can be re-bound.
- The bounded type set constrains publishers; widening it is a deliberate
  act (mapping plus publish-gate change), not a drift.

### Neutral

- The hygiene-not-security stance is unchanged; the cryptography claims
  the disk, nothing more.
- The wire dialect is untouched in v1; O3 remains the recorded route if
  the default endpoint ever needs ad-hoc JOINs.
- Query observability rides the existing `log_comment` stamp
  ([ADR-0115](./0115-query-observability-data-plane-strategy.md));
  per-applet attribution of embedded instances is the one open fork there
  (SD7).

## Update 2026-07-20 — Implementation (M1–M7)

Implemented M1–M7. The store, capability service, streaming-decrypt
broker, registry entry kind, engine routing, and catalog are in place and
tested end to end: publish an encrypted dataset, then query
`keelson('<handle>')` through the in-process engine, decrypting through a
named pipe, and the rows match; a wrong key or a truncated ciphertext
fails the request; the `keelson('adhoc')` catalog lists and shrinks; a
republish invalidates the revision-keyed cache. Three things diverged
from the text above and are recorded here rather than rewritten into it.

**SD3 pipe writer — `O_WRONLY`, not `O_RDWR`.** The decision has the
writer open the fifo `O_RDWR` so the open never blocks. Verified against
clickhouse-local 26.6.1, that races: the reader blocks until pipe EOF —
it does *not* stop at the Arrow end-of-stream marker — so the writer must
close the pipe to end the read, and a bare `O_RDWR`-then-close can close
before the reader opens, discarding the payload and leaving the reader's
own open to block forever with no writer present. The shipped writer
instead polls `O_WRONLY|O_NONBLOCK` (which returns `ENXIO` until a reader
is present), so the write side opens strictly after the read side and the
ordering is guaranteed; it then streams and closes for a clean EOF. The
poll is context-bounded so a worker that never reaches the read cannot
strand the writer, and the fd stays under the Go poller so a write past
the pipe buffer parks rather than failing. Everything else in SD3 holds:
constant memory, no whole-plaintext copy, and abort-on-writer-error even
when the worker consumed the verified prefix and exited 0.

**SD5 freshness — an instance trigger, not a graph signal.** The decision
registers the dataset revision as an ADR-0097 signal. It ships instead as
an instance-level trigger: `NotifyDatasetRevision(alias, revision)` sets
the ordinary auto-run request when Live main is on, so a republish
re-runs through the same path the Run button uses. This keeps the SD4
promise — no new reactivity machinery, an explicit Run always works —
without threading an out-of-band revision bump through the signal store,
whose encodable value set and per-frame snapshot semantics fit it poorly.
The SD7 open fork is settled as it leaned: an embedded instance's queries
attribute to a composed stamp `<embedder-app-id>#<slug>`, so per-applet
slicing survives embedding; a standalone applet keeps its minted id.

**Query path from an applet — deferred.** The applet surface ships and is
unit-tested: the delivery ops, the `datasets:` frontmatter, the
`NewEmbedded` embedder constructor, and the client-side
`keelson('<alias>')`→`keelson('<handle>')` rewrite. Making an ad-hoc
dataset actually *resolve* from an `endpoint: introspection` applet is
left open deliberately. Such an applet queries over HTTP against the
introspection host's private registry via the `url()` rewrite, and that
`/table` path refuses encrypted datasets (SD3) and never sees the handles
anyway (they register into `introspect.Default`, which the host does not
serve). The only reader of an ad-hoc dataset is an in-process
`introspectengine.Engine` over `introspect.Default`, which no production
surface constructs yet. Rather than commit to a bridge — a second
engine-backed endpoint, or unifying the host's `/query` on the in-process
engine — the query path is deferred; the recorded direction is that
`keelson(...)` itself may become a native ClickHouse table function
resolving against a single live keelson instance, which would supersede
either bridge. The rewrite is pure alias→handle indirection and plugs
into any of them unchanged. The M8 dogfood (a publishing embedder with
live verification) defers with the query path, since its verification is
querying the dataset it publishes.

The `RLIMIT_CORE=0` startup hardening (SD8) ships and is wired into the
shell. `BOXER_ADHOC_DIR` is declared in the ADR-0009 env registry; its
generated `doc/env-vars.md` row awaits a clean-tree regeneration, since
regenerating mid-flight would fold in unrelated in-progress specs. Binary
placement on the appliance stays deferred to the ADR-0128 boot probe,
unchanged.

## Update 2026-07-20 — Query path (SD3 revised)

The query path deferred above is now implemented, and it does **not** take
the in-process engine route SD3 prescribed. On review, that route could
not reach the applet — an `endpoint: introspection` applet queries over
HTTP, not through an in-process engine — and it diverges from the recorded
direction that `keelson(...)` may become a native ClickHouse table
function. So SD3's *transport* is revised (the store, keys, capability,
and bounded type set are unchanged):

- **Served over the existing url() path.** The `/query` endpoint's
  `keelson('<handle>')`→`url('.../table/<handle>','ArrowStream')` rewrite
  now emits a **third argument, the explicit structure**, for an ad-hoc
  dataset, so clickhouse applies the SD1 bounded-type mapping the publish
  gate already computes rather than its own Arrow inference. The applet's
  HTTP client is unchanged; ad-hoc becomes a first-class keelson table.
- **Decrypted in-process at `/table`.** The `/table/<handle>` endpoint,
  given a decryptor, resolves the key from the broker's KeyStore **by
  handle** (the broker stays the decrypt executor, K2) and streams the
  plaintext Arrow as the HTTP response. The **key never rides the wire** —
  only the handle, already catalog-visible, does. A mid-stream
  authentication or truncation failure aborts the connection, so the query
  fails rather than accepting a truncated result.
- **`"plaintext never rides HTTP"` becomes `"plaintext rides only loopback
  HTTP, decrypted in-process by handle."`** The introspection endpoint is
  loopback-bound (non-loopback binds are refused at Start), so plaintext
  stays in-memory on the loopback socket — the same in-memory exposure as
  the pipe's kernel buffer, defensible under the disk-only threat model
  (SD2). The honest cost: a loopback socket is a marginally broader
  surface than an fd-scoped pipe (a privileged local observer could sniff
  `lo`), and there are now **two decrypt paths** — the broker pipe
  (SD3-as-written) and this handler — where the original wanted one.
- **The pipe/engine route is kept**, not deleted: it remains the decrypt
  path for a direct in-process `introspectengine.Engine` consumer, and its
  `KeyStore` and AEAD reader are reused by the handler route. If the url()
  route proves dominant, the pipe machinery is retirable later.

Rationale: this reuses the transport play already speaks (near-zero applet
change), keeps SD1's total mapping via the explicit structure argument,
and is a stepping stone to a native `keelson()` table function — which
would also resolve server-side and could supersede this handler. Verified
end to end: publish, then query `keelson('<handle>')` over `/query`
through url() and the `/table` decrypt; a mixed ad-hoc + `env` query; and
a republish seen on the next query.

## Update 2026-07-23 — Nested / leeway-encoded columnar schemas

SD1 bounds the publish gate to a **flat scalar** type set and bare
identifier column names, so the Arrow→ClickHouse structure mapping the fifo
read (SD3) and the url() rewrite (SD3 revised) require is total. That
excludes a whole legitimate class of publisher: a **leeway-encoded /
nested columnar** table, whose physical column names carry colons and whose
repeated sections are `Array`-typed (with `Struct`/`Map`/`Nullable` in the
mix). The Consequences already name this as the sanctioned lever —
*"widening it is a deliberate act (mapping plus publish-gate change), not a
drift"* — and this is that act. The store, keys, capability, quotas, and the
two decrypt paths are unchanged.

- **The structure generator is now name-quoting and recursive.** Every
  column name — top-level and nested `Tuple` field — is backtick-quoted
  (embedded backticks doubled), so a colon-laden name is carried verbatim.
  The type mapping recurses: `List`/`LargeList`/`FixedSizeList → Array(T)`,
  `Struct → Tuple(`f` T, …)` (a *named* tuple, so Arrow struct fields match
  by name), `Map → Map(K,V)`, and a nullable **scalar leaf** wraps in
  `Nullable(T)`. The scalar set also grew to cover the remaining backbone
  and payload leaf types a real columnar table carries: a **timezone-naive**
  `Timestamp` (empty zone) → a bare `DateTime64(N)` (fabricating a UTC zone
  the schema does not carry would misrepresent it — the epoch value is
  identical, only the display zone differs), and `FixedSizeBinary(N)` (a
  fixed-width hash/correlator) → `FixedString(N)` — both also inside an
  `Array`, via the same recursion.

- **Nullability lives on scalar leaves only.** ClickHouse forbids
  `Nullable(Array)`/`Nullable(Tuple)`/`Nullable(Map)`, and its ArrowStream
  reader coerces a null container to an empty/default one (verified against
  clickhouse-local 26.6), so container-level nullability is dropped rather
  than rejected — a null list reads as `[]`. The publish-time discipline is
  kept: a still-unsupported type (dictionary, union, large/view string, or a
  timestamp with a non-UTC/non-empty zone or a coarser-than-µs unit) is
  refused at publish, naming the column, never discovered at query time —
  now also when nested inside a supported container.

- **Blast radius is one function.** The fifo read
  (`file('<fifo>','ArrowStream',<structure>)`) and the url() rewrite
  (`url(...,'ArrowStream',<structure>)`) already interpolate the structure
  string as an **opaque single-quoted SQL literal**, and a query never
  interpolates a column name as a bare identifier (an ad-hoc dataset streams
  whole; projection pruning is bypassed for it). So the colon names and
  nested types live entirely inside the structure literal and survive the
  round trip without touching either transport. The bare-identifier rule
  still guards the dataset **alias** and **handle**, which must name a
  TEMPORARY table and a frontmatter binding unquoted.

Verified end to end: the structure generator over a nested schema; a live
fifo `SELECT *` round-trip; and — through the capability service — both the
in-process engine and the url()/`/table` decrypt returning the colon-named
`Array`/`Tuple`/`Nullable` columns intact, addressable by quoted identifier
through the `keelson('…')` macro rewrite.

## Status

Accepted (2026-07-20).

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.

## References

Internal:

- [ADR-0094 — keelson introspection tables](./0094-keelson-introspection-tables.md)
  — the provider registry, `keelson('…')`, InputTables, and the endpoint
  this feature extends.
- [ADR-0132 — sqlapplet](./0132-sqlapplet-sql-defined-applets.md) — the
  applet surface, §SD5 security classes, §SD8 graduation boundary this
  design refines.
- [ADR-0133 — chhttp server dialect and param binding](./0133-chhttp-server-dialect-and-param-binding.md)
  — the request/param channel the encrypted-inputs map sits beside.
- [ADR-0026 — app runtime and capability subjects](./0026-app-runtime-and-capability-subjects.md)
  — capability taxonomy, audited request/reply, hygiene-not-security.
- [ADR-0097 — play as a reactive query-graph](./0097-play-reactive-query-graph.md)
  — the signal model SD5 rides.
- [ADR-0089 — row-DML serialization vs native ingestion](./0089-rowdml-serialization-clickhouse-native-ingestion.md)
  — the durability boundary.
- [ADR-0118 — extbin external-process chokepoint](./0118-extbin-external-process-chokepoint.md)
  and [ADR-0128](./0128-imzero2-mesh-draw-stream-codec-lane.md) — binary
  resolution and the gokrazy probe SD8 defers to.

External:

- [ClickHouse — storing data / encrypted disks](https://clickhouse.com/docs/en/operations/storing-data)
  and [ClickHouse#64965 — encrypted Parquet unsupported](https://github.com/ClickHouse/ClickHouse/issues/64965)
  — the facts killing O5.
- [gokrazy — mount.go](https://github.com/gokrazy/gokrazy/blob/main/mount.go)
  and [gokrazy — permanent data](https://gokrazy.org/userguide/permanent-data/)
  — tmpfs `/tmp`, no swap, `/perm` semantics behind SD8.
- Hoang, Reyhanitabar, Rogaway, Vizár — *Online Authenticated-Encryption
  and its Nonce-Reuse Misuse-Resistance* (CRYPTO 2015) — the STREAM
  segmented-AEAD construction SD1's format follows; the
  [age file format](https://age-encryption.org/v1) is the deployed
  precedent for chunked STREAM over files.
