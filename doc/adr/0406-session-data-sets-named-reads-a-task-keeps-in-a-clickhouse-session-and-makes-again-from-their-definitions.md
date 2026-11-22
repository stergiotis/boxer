---
type: adr
status: proposed
date: 2026-11-22
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted. SD1–SD9 are built (2026-11-22) in [`public/keelson/runtime/sessiondata`](../../public/keelson/runtime/sessiondata) with the seams the sections below name; no app calls the service yet. It has run against one ClickHouse 26.9 server on loopback and against an HTTP stub; nothing has run on a cluster or behind a load balancer.

# ADR-0406: Session data sets — named reads a task keeps in a ClickHouse session, made again from their definitions

## Context

An agent doing analysis through play has no named intermediate. In one
benchmark attempt at an authorship-attribution task (2026-11, boxer's chat
agent), 42 of its 62 statements began with the same 4.8 KB CTE re-deriving a
tokenised message table, and the model wrote 189 KB of SQL to do what
`msgs = …` does once in a data-frame workflow. A result computed outside the
server — in a downstream app's sandboxed kernel, published as an ad-hoc dataset
([ADR-0240](./0240-adhoc-datasets-v2-sealed-store-owned-capability.md)) — could
not be joined with server tables: play refuses a statement naming both
`keelson('…')` and a server table because no endpoint serves both
([ADR-0141](./0141-play-endpoint-dispatch-seam.md)), so the model ruled the
kernel out for a task whose deliverable had to come from SQL.

What a ClickHouse server offers, measured on a single 26.9 node over HTTP on
loopback (2026-11-22, the agent's `readonly=2` profile):

- **Session temporary tables.** `CREATE TEMPORARY TABLE … AS SELECT` in an
  HTTP session (`session_id`) works under `readonly=2` — the `Memory` engine
  and `ENGINE = MergeTree ORDER BY …` alike — and `readonly=1` refuses it
  (code 164). Later statements of the session read the table; another
  session does not see it. `DROP TEMPORARY TABLE` works under `readonly=2`.
  `system.tables WHERE is_temporary` lists the session's tables with
  `total_rows` and `total_bytes`.
- **One statement at a time.** A second statement in a session while the
  first runs is refused, code 373 `SESSION_IS_LOCKED`.
- **Expiry is detectable.** With `session_check=1` a session the server does
  not hold is refused, code 372 `SESSION_NOT_FOUND`; without it a new empty
  session is opened silently. The server refused a `session_timeout` above
  its `max_session_timeout` of 3600 s (code 374).
- **External data.** A table attached to one statement as a multipart part
  (`<name>_format`, `<name>_structure` on the URL; the statement may travel
  in the form's `query` field) is readable by that statement under
  `readonly=2`; a `CREATE TEMPORARY TABLE … AS SELECT … FROM <name>` in a
  session keeps it. The server refuses external data without a structure;
  `ArrowStream` with an explicit structure works.
- **What is not the base mechanism.** `url()` needs `READ ON URL`, which a
  restricted server user lacks, and the host's HTTP table source is
  loopback-only by design (ADR-0145 §SD5). Through clickhouse-local,
  `remote(server table) JOIN local_table` is pushed to the server, which has
  no such table; only `GLOBAL JOIN` works, which ships the local table as
  external data — so federating through the local engine adds nothing over
  attaching it directly.

Measured with the capability built here (same server, 2026-11-22): a data
set of 79,109 messages with a token array took 0.95 s to make and 84 MB of
server `total_bytes`; one exploding it to 14.2 M rows from the first took
0.26 s; a statement reading it took 14 ms including the capability's probe;
after a forced expiry the next statement replayed both definitions in 1.14 s;
joining the first with an 11.2 MB Arrow stream attached as external data took
37 ms. The figures are one run each and are cited for order of magnitude only.

[ADR-0403](./0403-a-statement-a-task-wrote-leaves-only-as-a-read.md) is the
constraint this design is drawn against: text a task supplied leaves the
process only as a read, and a sender that must send a task's non-read has the
person adopt it first. A create is not a read.

## Decision

We will add a runtime service that owns named data sets for a data session —
an agent's task, or the windows of the person — each a defining read whose
table the service makes in a ClickHouse HTTP session it holds, makes again
from the definition when the session is lost, and hands to callers as a
binding for the statement they send.

### Subsidiary design decisions

- **SD1 — A runtime service, beside `adhocdata`.** ✓ Package `sessiondata`,
  bus identity `runtime.sessiondata`, subjects
  `runtime.sessiondata.{open,attach,close,define,drop,prepare,release,list}`,
  one `…Cap(reason)` helper per subject and free functions over `app.BusI`
  ([ADR-0396](./0396-the-app-contract-a-host-promises.md) §6, §9). Apps are
  its clients; an agent reaches it only through an app's operations, and no
  app imports another to use it. `hostboot` boots it behind
  `Services.SessionData`, hands it the ad-hoc dataset service and installs the
  agent dispatcher. The wire is a versioned CBOR DTO pair, as the agent
  service's is, not a leeway codec.
- **SD2 — The data session.** ✓ An agent's request is attested with the
  dispatcher (`CallContext`) and reaches its task's data session, made on the
  first definition, whatever ticket the request names; every window working
  for the task reaches the same one without being told. A person's data
  session is opened by a window, which holds it, and is named by a ticket,
  `sds_` and 128 random bits; any window presenting the ticket attaches and
  holds it too. Holding is what a person's request is checked against; the
  ticket is a bearer capability, unguessable, never on the trail or in the
  catalog (a digest stands in). Opening, attaching and closing are refused
  for an agent's call. A task's data session ends when the dispatcher no
  longer reports the task live (`TaskLive`, swept every 30 s); a person's
  ends when the last window holding it closes or one closes it; all end when
  the host stops. Ending drops the tables the service made.
- **SD3 — A data set is its definition; the table caches it.** ✓ A data set
  has a name (an identifier of at most 64 bytes), a defining `SELECT` of at
  most 64 KiB, the data sets it reads (`session('name')`), the ad-hoc
  datasets it reads (`keelson('alias')`), an optional sort key of column
  names, a revision, a state (`live`, `expired`, `stale`, `failed`), its
  columns, rows, bytes, label and endpoint. Its table is
  `` `__sd_<name>_r<revision>` ``, a temporary `MergeTree` table ordered by the
  sort key or `tuple()`, so its rows sit in the server's temporary data on
  disk rather than in memory. A server session is per data session, endpoint
  and identity (URL, user and a digest of the password); its first statement
  opens it and every later one carries `session_check=1`. Each use starts
  with a probe of the session's tables: a table gone marks its data set
  `expired`, a capability table no data set owns — a create whose answer
  never arrived, a drop that did not reach — is dropped, a lost session is
  reopened under a new id, and the statement's data sets and what they read
  are made again, oldest dependency first — each a `replay` on the trail. A
  step that meets a lost session runs once more. Redefinition stays on the
  data set's endpoint and identity (another is refused until it is dropped);
  it makes the new revision's table, then drops the old one
  and leaves every data set that reads it `stale`, rebuilt by the next
  statement that reads it; a definition that would read itself, directly or
  through others, is refused. Drop refuses a data set others read unless
  cascaded. A definition is made at once, so one that does not run is
  refused when it is given.
- **SD4 — Bounds per data session.** ✓ At most 64 data sets; rows and bytes
  under `BOXER_SESSIONDATA_MAX_ROWS` and `BOXER_SESSIONDATA_MAX_BYTES`; the
  ad-hoc datasets one statement attaches under
  `BOXER_SESSIONDATA_MAX_ATTACH_BYTES`. The create reads its `SELECT`
  through `LIMIT` one more than the rows the session may still hold, so a
  table that reaches the limit is over the bound; rows and the server's
  `total_bytes` are read back after the create, and a table over either bound
  is dropped and its definition refused. The count and both sums are taken
  again when a build is recorded, since builds on two server sessions of one
  data session run at once. `BOXER_SESSIONDATA_SESSION_TIMEOUT` is the
  `session_timeout` sent.
- **SD5 — The create is the capability's statement around a checked read.**
  ✓ A definition's macros are replaced by the tables and external tables it
  reads, and the result must be one query the grammar parses, without a
  `FORMAT` clause or a `SET` prelude, that both the statement-kind classifier
  the fairlead pass uses and the security class play requires of an agent's
  statement place as a read — so no `url()`, `remote()`, `file()`, no
  unknown table function. Only then is its `SELECT` taken from the parse —
  its tokens as boxer's lexer read them, every comment replaced by a space,
  and that text checked again — and wrapped:
  `CREATE TEMPORARY TABLE … ENGINE = MergeTree ORDER BY … AS SELECT * FROM (…) LIMIT n`.
  Every token outside the parentheses is the service's, and every token
  inside is one the check read. Both classifiers refuse a statement with a
  block comment that opens another inside it, which ClickHouse would end
  elsewhere than boxer's lexer (ADR-0132, update of 2026-11-22); the create
  carries no comment at all. This is the shape
  ADR-0403 names for an app's own writes — text the app composed, bounded by
  the grant's destinations — and not a task's non-read the person must
  adopt: what the task supplied is a read, judged before wrapping by a
  stricter check than the fairlead pass applies; the wrapper's one effect is
  a table in a server session the service owns, invisible to every other
  session, gone with it, and of the one kind `readonly=2` admits; and the
  grant names that effect on its own (SD6). The service's statements —
  `sessiondata.create`, `sessiondata.replay`, `sessiondata.probe`,
  `sessiondata.drop` — are led through the fairlead with the task mark
  cleared, carry `readonly=2`, and stamp `log_comment` with the service and
  the task and call whose work they are, so the server's query log
  attributes them. The journal records site and text, not the task
  (ADR-0403, deferred); the trail row records the task, the call and the
  inner read's stratum. The create's own stratum is unplaced, as any
  write's. The caller's statement is the task's text and the caller marks
  it as before; the service checks an agent's prepared statement is a read
  once its macros are replaced, by the same two classifiers. An endpoint URL that carries `query` — which the server reads as the start
  of the statement — or a parameter the service sets is refused.
- **SD6 — The right to keep session tables is a grant destination.** ✓
  `clickhouse-session:<host>` (`DestinationSession`), at the data reach, in
  words "keep named tables for this task in a session on the ClickHouse
  server at <host>". A definition and a replay need it and
  `clickhouse:<host>`; a task without either is refused with a
  `GrantError` naming the first missing, which an app surfaces with
  `app.RefuseForDestinations`. An unattended host allows it through its
  settings as it allows any destination. Reading a data set needs nothing
  beyond what the caller's own checks need of a run on the endpoint.
- **SD7 — Host datasets attach as external data, inside the wall.** ✓ A
  `keelson('alias')` call in a prepared statement or a definition is read
  through `adhocdata.Service.Read` — its attestation, its grant rule
  (`keelson:<alias>`, the bundle, or the publishing task) and its audit —
  and sent as an `ArrowStream` external table with the structure the publish
  gate's mapping gives. An ad-hoc dataset is sealed, so attaching one is a
  confined run ([ADR-0145](./0145-sealed-app-data.md) §SD3): it is refused
  unless the caller declares the endpoint allowed to see sealed data
  (`Endpoint.ServesConfined`, the declaration the server engine takes), a
  data set defined from one is labelled confined, the label is carried to
  every data set that reads it, and a statement reading a confined data set
  is refused on an undeclared endpoint and bound as confined, so the server
  engine's backstop applies too. The declaration is per call, not per
  session. A handle instead of an alias, and a call with named arguments,
  are refused.
- **SD8 — The seam is a binding, not a run.** ✓ `Prepare` returns a
  `Binding`: the statement with its macros replaced; the settings that put
  it in the server session (`session_id`, `session_timeout`,
  `session_check=1`); the external tables; the label; the data sets read and
  replayed; the ticket; and a lease. A server session admits one statement at
  a time: the lease holds it from `Prepare` until `Release` — after the
  response is drained or abandoned — or until `BOXER_SESSIONDATA_LEASE_TIMEOUT`
  lapses, and a second `Prepare` waits under its caller's context. The
  leases a window has out are released when it closes, and every lease when
  the host stops. A
  statement naming no data set holds nothing; one naming neither macro comes
  back unchanged. `Binding.Apply` adds settings, external tables and label to
  a `queryengine.Request`. The request gained `External`, which the server
  engine sends as a multipart form and the local engine refuses.
- **SD9 — The trail and the catalog show lineage and state.** ✓ Open,
  attach, define, replay, expire, stale, drop, attach of host data and close
  are audit events on the trail (domain `sessiondata`), with the task and
  call as delegation, data sets as `name@revision`, what a build read as
  references, ad-hoc datasets as `alias@handle#revision:digest`, and the
  definition's digest and bounded text. `List` gives a window or an agent its
  session's data sets with definitions, sizes, endpoint, table and state;
  `keelson('session_datasets')` gives the operator every session's, without
  definitions or tickets.

### What it does not cover

- **A cluster or a load balancer.** A session lives on one node. A balancer
  that sends a session's statements to different nodes shows as a lost
  session at every step or an unknown table; the service does not detect
  the cause. The endpoint is meant to be one server, as the server engine's
  is.
- **The server user's grants.** A user without `CREATE TEMPORARY TABLE`
  cannot hold data sets, and a deployment that grants it widens what the
  server lets any statement of that user do; the client-side barriers —
  play's security class, ADR-0403's pass, the check here — are what keep a
  task's own text from creating one. Those barriers judge the text as
  boxer's lexer reads it. One difference from the server was found while
  this was built: ClickHouse nests block comments and the lexer does not,
  so `/* /* */ SELECT 1 AS "x */ CREATE TEMPORARY TABLE t (a UInt8) ENGINE = Memory --"`
  was a read to both classifiers and a create to a 26.9 server under
  `readonly=2`. Both classifiers now refuse a statement whose block comment
  opens another (ADR-0132, update of 2026-11-22), which closes that gap for
  play's limits, the pass and this service alike. Other token-level
  differences between the two lexers are not ruled out, and with the grant
  in place the server no longer stands behind the client for this one
  effect.
- **Isolation between the windows of one person's data session.** The
  ticket is a bearer capability; whoever holds it reads and redefines.
- **A caller that does not release.** The lease lapses; until then the
  session waits.
- **The credentials.** A caller hands the endpoint's user and password to
  the service over the in-process bus, which neither logs nor records them;
  the service keeps them for the server session's life.
- **What a replay reads.** A replay evaluates the definition against the
  sources as they are then; its row count is on the trail beside the first
  build's.

### Deferred

- **Wiring the apps.** Play, chat and an app in another repository that
  runs an agent's statements call the service; none does yet.
- **Moving the gated run out of play.** Parse, security class, grant check,
  `readonly=2`, stamps and the fairlead mark stay in play's client; the
  service is called from inside them (see Alternatives).
- **Persistent scratch databases**, and definitions that outlive the host.
- **Exposing host datasets to other machines over HTTP** — ADR-0082's
  deferral stands.
- **Introspection tables other than ad-hoc datasets** as external data, and
  a caller's alias bindings (window-scoped aliases, handles).
- **TLP tier labels** on data sets (ADR-0312); only the ADR-0145 label is
  carried.
- **A time bound on a create** of the service's own; the server's apply.
- **A capability-inspector page** for the service.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API: `public/keelson/runtime/sessiondata` | added | hostboot |
| Bus subjects `runtime.sessiondata.*` and their wire DTOs | added, wire version 1 | the free functions of the package; manifest capabilities |
| Exported Go API: `queryengine.Request.External`, `queryengine.ExternalTable` | added | the server engine sends it; the local engine refuses it |
| Exported Go API: `agent.Service.TaskLive` | added | — |
| Grant destination class `clickhouse-session:<host>` | added, `ReachData` | `agent.ReachOf`, the grant dialog's words |
| Introspection table `keelson('session_datasets')` | added | — |
| Trail audit events, domain `sessiondata` | added | trail readers |
| Environment variables `BOXER_SESSIONDATA_*` | added | `doc/env-vars.md` |
| Fairlead exit sites `sessiondata.{create,replay,probe,drop}` | added | journal readers |
| Host boot: `Services.SessionData`, `Runtime.SessionData` | added, on in `AllServices` | — |
| Design lint L12 special ids | `runtime.sessiondata` added | — |

## Alternatives

- **A run entry point of the service's own.** The service would parse,
  check, send and stream a statement's result. Not taken here: play's run
  path also rewrites, places, degrades for a read-only user, stamps, observes
  progress and decodes Arrow, and moving it means moving play's agent limits
  and dispatch decision into the runtime with it, which is the larger change
  deferred above. A binding the caller applies keeps one run path per app.
- **The server pulls host data with `url()`.** Needs `READ ON URL`, which the
  restricted user the agentic layer recommends lacks, and reaches only a
  loopback server.
- **Federate through clickhouse-local.** Only `GLOBAL JOIN` works, which
  sends the local table as external data — what SD7 does directly.
- **Name data sets by bare table names.** The agent would write `msgs`; but
  ClickHouse resolves an unqualified name to a temporary table first, so a
  data set named as a server table would shadow it in every statement in the
  session, and which statements belong in the session would depend on a
  guess. `session('…')` says which tables are the session's.
- **The `Memory` engine.** Holds every data set in server memory for the
  session's life.
- **Mark the create with the task and teach the pass the shape.** The pass
  would let a task's own `CREATE TEMPORARY TABLE … AS SELECT` leave, which
  ADR-0403 forbids, and would learn one service's statement.
- **A second fairlead mark, "caused by", beside the author.** Would let a
  journal or a pass see agent-caused statements; ADR-0403 rejected the cause
  as the key and deferred the task on the journal. Not needed to send the
  create, and a change to the fairlead's statement for every exit.
- **Return the creates for the caller to send.** Every caller would then run
  the replay, the probe and the bounds itself, and the fairlead would see
  them under the caller's site.
- **Create tables lazily, at first read.** A definition that does not run
  would be found by the next statement, not by the call that wrote it.
- **One server session per endpoint, whatever the identity.** A caller
  naming the user would reach tables another made with the password.

## Consequences

### Positive

- A task defines an intermediate once and later statements read it by name;
  a result computed on the host joins server tables in one statement.
- A definition outlives its table: an expired session costs a replay, not
  the work.
- The service's own statements pass the fairlead as its own, so ADR-0403's
  pass keeps refusing every non-read a task wrote.

### Negative

- A deployment that wants session data sets for agent work grants its server
  user `CREATE TEMPORARY TABLE`.
- Statements reading one data session's tables on one endpoint run one at a
  time, and a caller that forgets to release holds the others until the
  lease lapses.
- Each prepared statement costs a probe round trip before it is sent.
- Sealed data reaches a server when the caller declares it may; the
  declaration is the caller's word, as the server engine's is.

### Neutral

- A statement naming no data set is untouched and holds no session.
- The service's statements land in the journal under four sites and in the
  server's query log under the service's stamp.

## Migration — Tier 1

Additive. Nothing calls the service until an app is wired. The agentic
layer's advice on the server user changes for a deployment that enables it:
`CREATE TEMPORARY TABLE` is granted.

## Verification plan — Tier 1

- **Lane.** Default `go test`, against an HTTP stub that holds sessions,
  enforces one statement per session, expires on demand and reads external
  data: the composed create, its site and its cleared mark (and the host's
  pass refusing the same text marked); refusals of non-reads; destinations
  refused by name; replay after expiry in dependency order in a new session;
  a lost session mid-define retried; redefinition, staleness and rebuild;
  the three bounds; external data and its label; the wall on an undeclared
  endpoint; one statement at a time and a lapsed lease; a person's session by
  ticket and its end; a task's session across windows and its end; drop and
  cascade; host stop; the catalog; the bus round trip; the create carrying
  no comment and an agent's statement nesting one refused; an endpoint URL
  carrying `query`; a redefinition on another endpoint; concurrent
  definitions on two endpoints against the count and row bounds; a define
  and a prepare meeting a closed data session; a table a failed create left
  behind; a closed window's lease. The server engine's
  multipart form and its refusals. `TaskLive` and the reach of the new
  destination.
- **Integration lane** (`//go:build integration`, the server
  `chclient.ConfigFromEnv` names): define, prepare and run through the server
  engine, a forced expiry with a 2 s `session_timeout` and its replay, an
  ad-hoc dataset joined with a data set, a data set defined from one,
  concurrent statements in one session, and a definition whose nested
  block comment hides an expression from the grammar.
- **What would fail.** A create led as the task's; a task's non-read
  wrapped; text the server reads that the check did not; a lost session not replayed or replayed out of order; a bound not
  enforced; sealed data sent to an undeclared endpoint; two statements in one
  session at once.
- **Gap.** One 26.9 server on loopback; no server user without
  `CREATE TEMPORARY TABLE`, no `readonly=1` profile, no cluster; no app wired.

## Choices for review

- The package and subject name `sessiondata`, the macro `session('…')`,
  the ticket prefix `sds_` and the table prefix `__sd_`.
- A person's data session lives while any window holds it, not only while
  its opener does; any holder may close it.
- An agent's call always reaches its task's data session; a ticket it names
  is ignored rather than refused.
- The composed create is the service's own statement (SD5) and is led with
  the task mark cleared; the task is on the trail and in `log_comment`, not
  in the journal.
- `clickhouse-session:<host>` is a class of its own at the data reach, and
  reading a data set needs no destination beyond the endpoint's.
- The server session is keyed by endpoint, user and password digest.
- `MergeTree ORDER BY tuple()` by default, the sort key limited to column
  names.
- Bounds and their defaults: 64 data sets, 50 M rows, 4 GiB, 64 MiB attached
  per statement, 15 min session timeout, 15 min lease; the byte bound is the
  server's `total_bytes`, checked after the create.
- The row bound by `LIMIT n+1` around the read.
- A definition is made at once; a failed replay leaves the definition and
  shows `failed`.
- Redefinition leaves dependants stale and rebuilds them on next read rather
  than at once.
- Drop refuses a data set others read unless cascaded.
- A probe before every prepared statement, rather than trusting the last
  known state.
- The ServesConfined declaration is the caller's, per call; a data set
  derived from sealed data is confined, and confinement is the only label
  carried.
- An ad-hoc dataset is named by alias only.
- Prepare checks an agent's statement is a read after replacing its macros,
  in addition to the caller's checks.
- Credentials cross the in-process bus in the request.
- A task's data session has a ticket, returned with every define, list and
  binding as a person's is; a window that attaches by it works in the
  task's data session as the person — defining, dropping and preparing
  without the task's grant — and stays a holder until the session ends.
- Comments in a definition are not sent; a definition or an agent's
  prepared statement nesting a block comment is refused (by the
  classifiers) rather than rewritten.
- Whether a caller sends an agent's statement that reads data sets with
  `readonly=1`, so the server itself refuses a create the task's text
  smuggles past the client, leaving `readonly=2` to the service's own
  creates; play sends `readonly=2` today for the settings it changes.
- The byte bound is the server's `total_bytes` after the create, so one
  create may take more than the bound on the server before it is dropped;
  only rows are bounded while the create runs.
- A redefinition stays on its data set's endpoint and identity.
- The probe drops capability tables no data set owns.
- A window's leases are released when it closes, though a statement it sent
  may still be running on the server, which then refuses the next one in
  the session as locked.
- An endpoint URL may not carry `query` or a parameter the service sets;
  other parameters (`database`) are the caller's.
- A `SETTINGS` clause in a definition's `SELECT` is the task's, as it is in
  play; it is sent inside the create.
- The catalog shows definition digests, not texts.
- The wire is CBOR DTOs, not a leeway codec.
- Task sessions end by a 30 s sweep of `TaskLive`, not an event.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0403](./0403-a-statement-a-task-wrote-leaves-only-as-a-read.md) (proposed) — a task's text leaves only as a read.
- [ADR-0367](./0367-sql-statements-leave-through-one-fairlead-and-each-is-placed-in-a-stratum.md) — the fairlead and its sites.
- [ADR-0240](./0240-adhoc-datasets-v2-sealed-store-owned-capability.md), [ADR-0288](./0288-adhoc-bundles-datasets-published-with-the-applet-that-reads-them.md) — ad-hoc datasets and their grant rules.
- [ADR-0145](./0145-sealed-app-data.md) — sealed data, the confined label and the wall.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) §SD6, [ADR-0270](./0270-play-operations-catalog-and-agent-limits.md) §SD2, §SD7 — task grants, play's agent limits and its own writes.
- [ADR-0141](./0141-play-endpoint-dispatch-seam.md), [ADR-0144](./0144-query-engine-adapters.md) — play's dispatch and the engine request.
- [ADR-0394](./0394-library-packages-do-not-import-the-app-platform.md), [ADR-0396](./0396-the-app-contract-a-host-promises.md) — where the service lives and how apps call it.
- [ADR-0082](./0082-imzero2-remote-session-auth-tls.md) — remote exposure, deferred.
- [How to keep named data sets for a task](../howto/session-data-sets.md).
