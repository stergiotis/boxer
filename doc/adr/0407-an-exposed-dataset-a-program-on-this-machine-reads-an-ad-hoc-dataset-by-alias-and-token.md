---
type: adr
status: proposed
date: 2026-11-23
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted. SD1–SD8 are built (2026-11-23) in [`public/keelson/runtime/adhocdata`](../../public/keelson/runtime/adhocdata), [`public/keelson/runtime/introspect/introspecthttp`](../../public/keelson/runtime/introspect/introspecthttp), the agent runtime and play, and SD9 in play's run path. They have run against one ClickHouse 26.9 server on loopback and against stubs. One live run of a model has called `expose_dataset`; it was refused for the call's label, which the section *Decided with the owner, 2026-11-23: a task reads back its own clear work* answers.

# ADR-0407: An exposed dataset — a program on this machine reads an ad-hoc dataset by alias and token

## Context

An agent working unattended computes a result outside the server — in a
downstream app's sandboxed kernel — and publishes it as an ad-hoc dataset
([ADR-0240](./0240-adhoc-datasets-v2-sealed-store-owned-capability.md),
[ADR-0288](./0288-adhoc-bundles-datasets-published-with-the-applet-that-reads-them.md)).
Inside the host the dataset is read by SQL, and since
[ADR-0406](./0406-session-data-sets-named-reads-a-task-keeps-in-a-clickhouse-session-and-makes-again-from-their-definitions.md)
(proposed) play attaches it to a server statement as external data. Nobody
outside the host process can read it: a deliverable of a few hundred to a few
hundred thousand rows cannot be handed to a grader, a script or a dashboard.
In task trails (2026-11) the model wrote that a discriminative model fitted
in the kernel was out of reach because the result had to come out as one
self-contained SQL statement, and stayed with what SQL alone computes.

What a ClickHouse server on the same machine offers, measured on a 26.9
node (2026-11-22/23):

- `url('http://127.0.0.1:<port>/…', '<format>', '<structure>')` reads a
  loopback HTTP resource under `readonly=2` when the server's user has
  `READ ON URL` and the server's `remote_url_allow_hosts` admits the host; a
  50-row join through it works. Without the grant the server answers
  `ACCESS_DENIED`.
- `url()` sends a `HEAD` before the `GET`, as ADR-0145's 2026-07-26 update
  found for the probe. Read through the capability built here, a 6 MB Arrow
  stream took one `GET` per statement; a 50 MB one took five ranged `GET`s
  per statement.
- **A header's value is hidden in the query log; a URL is not.** With
  `url(…, headers('X-Secret-Token' = '…'))` the server's `query_log` row and
  `SHOW CREATE` print the value as `[HIDDEN]`; a token in the URL's path or
  query string is printed as written. (Measured with `clickhouse local` 26.9
  writing its own `query_log`; the measured server's user may not read
  `system.query_log`, so the server's own log was not read.)

What the host already does with sealed plaintext:
[ADR-0145](./0145-sealed-app-data.md)'s update of 2026-11-20 records that the
loopback listener serves `GET /table/<handle>` to any process on the machine
that knows a handle, and that `/query` reads `keelson('adhoc')`, whose rows
carry the handles. That path is not an act anyone took, names no one and is
not on the trail.

[ADR-0403](./0403-a-statement-a-task-wrote-leaves-only-as-a-read.md)
(proposed) and ADR-0406 are the constraints on the server side: a task's text
leaves only as a read. Nothing here writes on a server.

## Decision

We will let the person, or a task under a grant destination of its own,
**expose** one published dataset: the dataset service mints a token, and the
host's loopback listener serves the dataset's live revision under its alias
to a request presenting the token, until the exposure lapses.

### Subsidiary design decisions

- **SD1 — The unit is one dataset alias, served at its live revision.** ✓
  An exposure names an alias — a dataset published on its own, or one of a
  bundle as `<bundle>__<local>` — and each read serves the newest live
  dataset under it, so a republish is served without a new exposure. The
  revision a read served is in its `ETag` (`"<alias>-r<revision>"`) and on
  its trail row; the answer names the revision live when it was made, the
  catalog the one a read serves now. The exposure keeps the publisher, the
  bundle and the ClickHouse structure it was made for; a read that finds
  the alias held by another publisher or bundle, or a revision whose
  structure differs, ends the exposure, since the `url()` expression a
  reader was handed no longer fits. A bundle is not a unit: its datasets
  have different structures, and a reader asks for one table at a time; a
  prefix grant covers a bundle's datasets together (SD4). Each expose is an
  exposure of its own, with its own token; several may name one alias.
- **SD2 — Token and id; the token travels in a header.** ✓ An exposure has an
  id, `exp_` and 64 random bits, which names it in the catalog, a revoke and
  the trail, and a token, `bxe_` and 256 random bits, which only the answer
  that minted it carries; the service keeps its BLAKE3 digest and compares
  digests. A reader sends `Authorization: Bearer <token>` to
  `/exposed/<alias>`. The path carries no secret, so the listener, a proxy
  and the reading server may log it; ClickHouse hides a `headers()` value in
  its query log (Context). The answer gives the `url()` expression with the
  header clause and a `curl` line, ready to paste. What an operator should
  know: the token is in the statement text the reading server receives, in
  that server's `system.processes` while it runs, in the shell history of a
  `curl` line, and in the conversation record of the agent that was given
  it (the operation's answer); it is not in the host's log, its trail or
  its catalog. A token found in any of those lapses with its exposure.
- **SD3 — Lifetime and bounds.** ✓ An exposure ends at the first of: the
  end of its lifetime (`DefaultExposureLifetime` 1 h, a request may ask for
  up to `MaxExposureLifetime` 24 h); the alias holding no live dataset —
  a retract, or the publishing window closing, which retracts what it did
  not keep (ADR-0240 §SD5); a republish that changes the structure or the
  publisher (SD1); the window that made it closing; a revoke; the host
  stopping. At most `MaxExposures` (64) live at once and
  `MaxExposureReads` (16) requests open per exposure — a ClickHouse read
  fetches ranges in parallel — past which a request with a valid token is
  answered 503. The bounds are Go constants, as the package's other
  quotas are. The data is bounded by the dataset quota (256 MiB) already,
  and a read streams from the sealed file; nothing holds it in memory.
  Readable only while the host runs.
- **SD4 — Who may expose, and the grant destination.** ✓ A person's request
  — a window's, carrying no agent's call — needs nothing more: the person is
  the authority, and a window reaches the service only with the
  capability its manifest declares (`adhocdata.ExposeCap`). An agent's call
  is attested with the dispatcher, as the service's other calls are, and
  needs, in its task's grant, `expose:<prefix>` for a prefix of the alias
  (`adhocdata.DestinationExpose`; the full alias is the narrowest prefix,
  as `publish:<prefix>` is for publishing). A task without it is refused
  with a `GrantError` naming `expose:<alias>`, which play surfaces with
  `app.RefuseForDestinations`; an unattended host approves the request by
  its settings as it does any destination. `agent.ReachOf` places the class
  at the data reach: an exposed dataset is read from this machine's
  loopback, no further than a data endpoint on loopback and further than
  this host's own tables. The grant dialog words it "let programs on this
  machine read the ad-hoc datasets whose names start with …, over this
  host's loopback port, by a token that lapses", and a request for a prefix
  no published name can start with is refused at the request. A task
  exposes only a dataset **its own call published** (the attested publisher
  of the live revision, ADR-0288 §SD4), not one it may merely read under
  its grant; the person exposes the rest. Publishing does not imply
  exposing.
- **SD5 — The confinement wall (ADR-0145).** ✓ See *The wall* below. In
  short: an exposure places nothing outside the cell, so ADR-0145's
  placement wall is unchanged; it is an explicit, attested, audited act by
  the person or by a task on its own work; a task may not expose what was
  published, or asked for, under a label above TLP:CLEAR, nor what derives
  from sealed data; and on a host the organisation manages an exposure
  above the export clearance is refused.
- **SD6 — The listener's route.** ✓ `GET /exposed/{alias}` serves the
  Arrow stream from a seekable reader, so a `HEAD` answers its length and a
  `Range` its part, as `url()` needs; `GET /exposed/{alias}/csv` serves CSV
  with a header line, written batch by batch, without a length or ranges.
  Every request that opens nothing — no token, another scheme, a wrong
  token, an alias not exposed, an exposure lapsed or revoked — gets the same
  404 and body, with no `WWW-Authenticate`; the answer never confirms that
  an alias is exposed or that a token existed. The route keeps the
  listener's loopback bind and `Host` check (ADR-0145, update of
  2026-11-20). It is a route of its own: a token opens only the alias it was
  minted for and never a handle; `/table/<handle>` and `/query` never open
  an exposure, and nothing about them changes. The seam is the introspect
  registry, so the listener does not import the dataset service:
  the service sets itself as the registry's `ExposureSourceI`, and a
  started listener records the base URL it serves the registry at
  (`Registry.ServedAt`), which the service names in its answer; without a
  listener an exposure is refused, since nothing could read it.
- **SD7 — Audit and catalog.** ✓ Expose (made or refused), each read,
  revoke and lapse are audit events on the trail, domain `adhoc-expose`,
  with the exposure id, the dataset as `alias@revision`, the task, the
  window, and for a read its bytes, range, format and the remote address
  and user agent the request declared — recorded as declared, never as
  identity. An agent's expose and revoke carry the dispatcher's
  delegation and conversation. A `HEAD` is not a read. The catalog
  (`Exposures`, `runtime.adhoc.expose.list`) lists every live exposure with
  alias, revision, structure, URL, expiry, reads and bytes served, and
  whether the caller — its window, or a call of its task — made it; never
  the token, and never the dataset's handle. The service never gives a
  token again; a window keeps the answers to the exposures it made, in its
  own memory, and the person's Data sets tab shows their reader lines
  while they live.
- **SD8 — Play.** ✓ `expose_dataset {name, minutes}` (command, effect
  document, asynchronous) names the dataset by its alias or by the name
  the window binds it under, and answers the id, the URL and the CSV URL,
  the token, the `url()` expression, the `curl` line, the structure, rows,
  bytes, revision and expiry; `revoke_exposure {exposure}` ends one. Both
  write the resource `exposures` without a read of it. `list_datasets`
  reads it, marks an exposed ad-hoc dataset and lists the live exposures.
  The *Data sets* tab has Expose per ad-hoc dataset, a line per live
  exposure with Revoke, and, for one the window made, the `curl` and
  `url()` lines with Copy. The hints are on the operations; `query`'s are
  untouched, and a launch of play does not ask for `expose:` — it is asked
  for when needed. The manifest declares `runtime.adhoc.expose`,
  `.expose.revoke` and `.expose.list`.
- **SD9 — A run waits for a reach demonstration already under way.** ✓ A
  small fix beside the above, its own commit: play refused a statement
  attaching an ad-hoc dataset to a server statement while the endpoint's
  reach proof (ADR-0145 §SD5) was in flight, and the model sent the same
  statement again, one failed call per conversation in live runs. The run
  path decides on its own goroutine and, for a statement refused only for a
  demonstration under way, waits for it up to 5 s under the run's context
  and decides again; still unproven, the refusal says the run waited. A
  check at a call that starts a run (`run`, `query`, `observe_node`)
  judges the placement the run waits for, so the grant is still checked at
  the call and the wall is applied by the run; a check with no run after
  it refuses and says a run would wait. When no demonstration can be made —
  no introspection plane, no endpoint — the refusal says so and promises no
  retry. Dispatch itself still performs no I/O (ADR-0145, update of
  2026-07-26).

### The wall

ADR-0145 makes sealed data a placement property: a statement naming sealed
data is not sent to an engine that may not see plaintext, and an engine
other than this process's plane may see it only after demonstrating it
reaches this plane. ADR-0307 and
[ADR-0312](./0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md)
add the cell: an edge boxer mediates that leaves the declared cell is a sink
with a clearance, and the agent sends nothing outside the cell but the model
route. Where does an exposure stand?

1. **It places nothing outside the cell.** The listener binds loopback only
   and refuses a non-loopback `Host`; a reader is a process on this machine.
   The cell treats a loopback destination as inside it
   (`cell.Declaration.EgressClearance` is RED for a local one), and ADR-0406
   already lets a server proven to reach this plane receive sealed data as
   external data. A tunnelled server elsewhere cannot fetch the URL, the
   same accident ADR-0145 §SD5 fails safe against. So the placement wall is
   untouched and the exposure is not an ADR-0312 sink.
2. **It hands plaintext to a program boxer does not mediate further.** A
   reader may write it to a file or send it on, which is what ADR-0317
   §SD4 calls plaintext leaving boxer — file export's row of ADR-0307 §SD6.
   So the act must be the person's decision or a grant's, attested,
   audited and attributable (SD4, SD7), and it must lapse (SD3). On a host
   the organisation manages (`BOXER_CELL_MANAGED`) export above its
   clearance is prevented; an exposure carries sealed data at least at the
   confined tier (`cell.Declaration.TierOf`), and no approval path for an
   exposure is wired, so above the export clearance it is refused, the
   person's included — with the defaults, confined AMBER and export GREEN,
   a managed host exposes nothing. A host the person administers records
   rather than prevents, as ADR-0317 does for export.
3. **A task exposes only its own work, at the lowest tier.** Every ad-hoc
   dataset is sealed, so the confinement bit cannot separate what a task may
   release from what it may not. What can: who published the live revision
   (the attested call, ADR-0288 §SD5), the TLP tier the coordinator
   declared on that call and on the exposing one (ADR-0312 §SD4), and
   whether the revision derives from sealed data. A task's exposure is
   refused unless its own call published the dataset, neither call declared
   a tier above CLEAR and the revision derives from nothing sealed; a
   republish that is not that task's own at TLP:CLEAR from nothing sealed
   — declared above CLEAR, derived from sealed data, or made by the person
   or another task — ends a task's exposure. A dataset the task may read under
   `keelson:<alias>` but did not publish — another task's, or data a person
   published — is not the task's to release; the person exposes it. The
   same facts about a revision decide what a task reads back without a
   label: a dataset is sealed to every reader but the task whose own call
   published it at TLP:CLEAR from nothing sealed (*Decided with the owner,
   2026-11-23: a task reads back its own clear work*).
4. **The label of a reader's result is the reader's business.** A
   same-machine server that reads an exposure through `url()` holds an
   ordinary table as far as boxer can tell; boxer does not follow it.
5. **What it does not claim.** As ADR-0145 §SD6 says of the plane: a
   loopback port has no user boundary, so any account on the machine that
   obtains the token reads the dataset while the exposure lives; the token
   is what limits that, not the port. The pre-existing `/table/<handle>`
   path is unchanged and still readable by any local process that learns a
   handle; this ADR neither widens nor closes it.

The default is refusal wherever the rules above do not clearly admit the
act: a task's exposure of what it did not publish, a labelled call, a
revision derived from sealed data, a managed host above clearance, no
listener, no grant.

### What it does not cover

- **Another machine.** Non-loopback binds stay refused; ADR-0082's
  deferral of remote exposure with a token and TLS stands.
- **Writing on a server.** No exposure needs a write privilege anywhere.
- **The reading server's grants.** The server that reads needs
  `READ ON URL` and a `remote_url_allow_hosts` that admits `127.0.0.1`. A
  deployment that grants these to the user agent work runs as widens what
  that user may read: `url()` of any host the setting admits. Play's
  security class refuses `url()` in a task's statement, and
  `remote_url_allow_hosts` bounds the rest; a deployment that can read
  exposures with another user keeps the agent's user without the grant.
- **Who reads.** Request metadata (address, user agent) is recorded as
  declared, not authenticated; a token holder is whoever holds the token.

### Deferred

- **A persistent landing in a server database** — the dataset written to a
  table a reader finds after the host stops. A separate decision, with a
  write privilege and a retention of its own.
- **An approval path for an exposure on a managed host** (ADR-0312 §SD3),
  and an export-kind sink row for it in the cell's registry.
- **An introspection table of exposures** for the operator, beside
  `keelson('adhoc')`.
- **Tying an exposure to the task's life.** An exposure made for a task
  outlives the task while its window, its dataset and its lifetime allow;
  a harness that fetches after the task ends relies on that.
- **Formats other than ArrowStream and CSV** (Parquet, TSV with names and
  types).

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API: `adhocdata.Expose`, `RevokeExposure`, `Exposures`, `OpenExposed`, `ExposeInput`, `ExposeResult`, `ExposureInfo`, `ExposeRequest`, `RevokeExposureRequest`, `ExposuresRequest`, `ClickHouseExpression`, `CurlLine`, `DestinationExpose`, `DestinationClassExpose`, `ExposeCap`, `ExposeRevokeCap`, `ExposeListCap`, the bounds, `ErrNoListener`, `ErrNotTheTasks`, `ErrUnknownExposure`, `ErrManagedHost`, `ExposureAuditRecord` and its actions, `Config.Cell` | added | hostboot hands `Config.Cell` |
| Bus subjects `runtime.adhoc.expose`, `.expose.revoke`, `.expose.list` and their wire DTOs | added, CBOR, wire version 1 | play's manifest capabilities |
| Exported Go API: `introspect.ExposureSourceI`, `ExposedRead`, `ExposedRequest`, `ExposedStatusE`, `ExposedFormatE`, `Registry.SetExposureSource`, `ExposureSource`, `SetServedAt`, `ClearServedAt`, `ServedAt` | added | — |
| HTTP routes `GET /exposed/{alias}`, `/exposed/{alias}/csv` on the introspection listener; `introspecthttp.ExposedPathPrefix` | added | — |
| Grant destination class `expose:<prefix>` | added, `ReachData` | `agent.ReachOf`, the grant dialog's words, the request check |
| Trail audit events, domain `adhoc-expose` (`expose`, `read`, `revoke`, `lapse`) | added | trail readers |
| Play operations `expose_dataset` v1, `revoke_exposure` v1, resource `exposures`; `list_datasets` reads `exposures` and gains `Exposures`, `ExposuresError` and `Exposed` (additive, version 2 kept) | added, changed | play's help reference |
| Play manifest caps `runtime.adhoc.expose`, `.expose.revoke`, `.expose.list` | added | — |
| Play's run path: a statement waiting on a reach demonstration (SD9) | changed | — |
| Host boot: the dataset service gets the cell declaration | changed, one line | — |

## Alternatives

- **The token in the URL** (`/exposed/<token>`). One string to hand over,
  readable by any client that cannot set a header. Rejected: ClickHouse
  prints a URL in its query log and hides a header's value (Context), and a
  path is what proxies, the listener's own refusal log and shell histories
  record. The cost is that a reader must send a header; every client named
  here can.
- **Expose a bundle as the unit.** A bundle's datasets have different
  structures and a reader reads one table; a prefix destination already
  covers a bundle's members together.
- **Pin an exposure to one revision.** A reader would get what was exposed,
  not what is live; an agent that republishes its result would have to
  expose again and hand on a new token. Serving the live revision with the
  same structure, and ending on a structure change, keeps the handed-out
  line valid exactly as long as it fits.
- **Serve exposures through `/table/<name>`, keyed by token.** One route
  fewer, and a reason for the two to be confused: a token that opened a
  table name, or a handle that opened an exposure, would let either
  weaken the other.
- **A task may expose what its grant lets it read.** Wider: a task could
  release another task's or the person's data on a `keelson:` grant that
  was given for reading. Rejected as the default; the person can expose it.
- **Make `expose_dataset` consequential, under a consent class.** The
  dispatcher would ask the person for each call, or let a call through
  under `expose:<prefix>` without the service seeing the destination;
  the service could not tell which operation an attested call was. The
  grant destination, checked by the service, is the gate, as
  `clickhouse-session:` is for keeping data sets, so the operation's effect
  is document and the destination is asked for with `request_access`.
- **CSV only, or ArrowStream only.** ArrowStream is what `url()` reads with
  an explicit structure and what the dataset is; CSV is what a script in
  any language reads, and arrow-go writes it batch by batch, so it costs
  one handler. TSV with names and types was not taken: its escaping is
  ClickHouse's, and a server reads the Arrow stream.
- **Land the dataset in a server table.** Needs a write privilege and
  outlives the host; deferred by the owner's decision.

## Consequences

### Positive

- A result computed outside the server leaves the host as a URL and a
  token a person, a grader, a script or a same-machine server can use,
  without a write on any server.
- The act is named, attributed and bounded: who exposed what, every read
  with its bytes, and how it ended are on the trail.
- The first join of a sealed dataset with a server table no longer costs a
  failed call while the reach proof is in flight (SD9).

### Negative

- A token handed to the model is in the conversation record and in the
  model's reply, by design; it lapses with the exposure.
- Plaintext leaves boxer to a program that may keep it; nothing follows it
  there.
- A server user that reads exposures needs `READ ON URL`.
- A run that waits on a demonstration holds its run up to 5 s.

### Neutral

- The pre-existing loopback paths (`/table`, `/query`) are unchanged.
- On a managed host with the default tiers, exposure is unavailable.

## Migration — Tier 1

Additive. Nothing is exposed until a person or a granted task asks. A
deployment whose readers are a same-machine ClickHouse server grants that
server's reading user `READ ON URL` and admits `127.0.0.1` in
`remote_url_allow_hosts`.

## Verification plan — Tier 1

- **Lane.** Default `go test` of `adhocdata`: the token's shape and its
  absence from every record, the catalog and the log; readers without the
  token, with a wrong one, for another alias and after a revoke told the
  same 404; lapse by lifetime, by retraction and by the window closing,
  each recorded once; a republish served at its revision and a changed
  structure ending the exposure; no listener; the lifetime and count
  bounds; an agent refused unattested, without the grant (naming
  `expose:<alias>`), with a prefix grant admitted, for a dataset the task
  did not publish, above CLEAR; the revoke rule; the read bound and 503;
  the bus round trip; the service closing; a managed host. Of
  `introspecthttp`: every failure answered alike, the stream with `HEAD`
  and ranges, CSV, busy, the `Host` check and the separation from
  `/table`, the registry's served-at. Of the agent runtime: reach, wording,
  the request check. Of play: `expose_dataset` through the catalog with its
  answer and the listing, the refusal naming the destination, the
  service's refusal, the person's Expose and the tab drawn headless; the
  reach wait proven within the bound, not proven, cancelled, and with no
  demonstration possible; the call's check deferring to the run.
- **Integration lane** (`//go:build integration`): the server
  `chclient.ConfigFromEnv` names, on this machine, reads a 300,000-row
  exposure through the answer's `url()` under `readonly=2` and joins it
  with a session temporary table; a plain HTTP client fetches it; the
  reads are on the trail with the server's user agent; after a revoke the
  server's read fails.
- **What would fail.** A token in a log line, a record or the catalog; a
  404 that differs by cause; an exposure outliving its dataset, its window
  or its lifetime; a task exposing what it did not publish, or without the
  destination; a non-loopback reader; the run retrying a statement that
  should have waited.
- **Gap.** One 26.9 server on loopback. The query-log masking was measured
  with `clickhouse local` of the same version, not on that server. One
  model's live run has called `expose_dataset`, and was refused; none has
  yet had an exposure admitted. The person's Expose was drawn headless,
  not seen on a screen.

## Decided with the owner, 2026-11-23

Three decisions were the code owner's before the work began, and are
recorded here as decided; the ADR as a whole still awaits review.

- **The first way out is HTTP on the same machine.** The host serves an
  exposed dataset on its loopback listener; a server on the same machine
  reads it with `url()`, a local program with an HTTP GET. No write
  privilege on any server. It is readable only while the host runs. A
  persistent landing in a server database is a later, separate decision
  (Deferred).
- **Who may expose.** A person at the window, and an agent's task under a
  grant destination of its own for exposing, asked for with
  `request_access` like `clickhouse:<host:port>`; an unattended host
  approves it by its settings, as it does the others. Publishing does not
  imply exposing.
- **Loopback only, a token per exposure, ending with the dataset.**

## Decided with the owner, 2026-11-23: a task reads back its own clear work

A fourth decision of the same day, made after a live run met the wall
where it was not meant to stand. A model working as a task computed a
table in another app's kernel; the app published it as a bundle on the
task's behalf; the task read it in play with `keelson('<bundle>__result')`
to check and cast it, published the play result and called
`expose_dataset`. The exposure was refused: the publish was labelled
AMBER. Every ad-hoc dataset is sealed, so the read had come back confined,
the coordinator had labelled the conversation, and every later call of the
task declared the confined tier. Nothing takes a label off again, and
every byte involved was the task's own work at TLP:CLEAR.

- **The rule.** A dataset that the same task's own call published at
  TLP:CLEAR does not label that task's conversation when it reads it back.
  Everything else stays sealed: a dataset a person or another task
  published, anything published under a label above CLEAR, and the rule
  that a server must have shown it may see sealed data before a dataset
  is attached to a statement sent to it.
- **Why it reveals nothing new.** The label marks what a conversation
  holds that it may not send on. What the rule leaves unlabelled is what
  item 3 of *The wall* already lets that task release by an exposure: its
  own publication at the lowest tier, from nothing sealed. The predicate
  is one (`standingOf` in `adhocdata`), asked by the exposure rule and by
  a read.
- **Who decides, and where.** The dataset service, against the revision
  it hands out. A read (`Service.Read`, `runtime.adhoc.read`) answers
  `OwnClear` when the reading call is an attested call of a task and the
  revision in the stream was published by an attested call of that task
  at TLP:CLEAR from nothing sealed; the stream and its publication are
  taken under one lock, so no republish fits between the judgement and
  the bytes. A reader cannot assert it: the session data service passes
  the answer on (`Binding.OwnClear`), and play takes it from the binding.
  A run on this process's plane does not receive the bytes from the
  service; it names the dataset by its handle and asks about the handle
  (`Service.ReadLabel`, a read with `LabelOnly`). A bundle's dataset keeps
  its handle, bytes and publication for as long as it is registered, and a
  republish registers new handles, so the answer for a handle holds for
  every read of it. False is the zero value everywhere: a reader that does
  not look, an older one, an app's own read, the person's, another task's
  all stay sealed.
- **The wall does not move.** The rule changes a label and nothing else.
  A statement that names or attaches an ad-hoc dataset is placed, refused
  and sent as before, whoever published the dataset: the dispatch
  decision is confined, an endpoint other than this process's plane needs
  the reach proof, the session data service refuses an endpoint not
  declared for sealed data before it reads the dataset, the binding and
  the request are confined and the engine's own refusal applies. Play
  keeps the two apart: *sealed* (the run read sealed data) is what the
  wall, play's own writes and the person's history go by; *confined* (the
  result is labelled) is what an operation's outcome carries.
- **Mixed statements.** One sealed input that is not the task's own clear
  publication — another dataset, a confined data set — labels the
  statement.
- **What a publication derives from.** The tier a call declares is its
  conversation's when the model asked for it. A model that asks for a
  query over sealed data and for the publish of its result in one round
  has both declared at the tier before either outcome arrived, so the
  publish was recorded at TLP:CLEAR. This weakened item 3 as it stood: an
  exposure asked for in the same round was admitted, and released rows
  derived from a dataset a person published. A revision now records
  whether it derives from sealed data. The publisher says so from the
  label of the result it publishes (`BundleProvenance.DerivedFromSealed`;
  play's `publish_result` sets it from the main result's label, which
  carries a kept data set's confinement too), and the service adds what
  it sees itself: an input handle that is not the publishing task's own
  clear publication, or that it no longer holds, makes the revision
  derived whatever the publisher said. A derived revision is neither read
  back unlabelled nor exposed by the task, and a republish that derives
  ends a task's exposure. A publisher that states nothing and names no
  inputs is still judged by its call's tier; an app that computes from
  sealed data outside play has to set the field.
- **Kept data sets.** A session data set a task keeps from a statement
  whose only sealed inputs are its own clear publications is held behind
  the wall like any data set derived from sealed data, and reads back
  unlabelled for that task (`DatasetInfo.OwnClear`). Its table is a cache
  of its definition, and a replay reads the dataset again: each build
  judges on what it reads then. A replay that finds the dataset
  republished by someone else, or above CLEAR, or that the person rather
  than the task causes, leaves the data set confined for every reader,
  and only a later replay that reads the task's own clear publication
  again lifts that. Between replays the label is that of the rows the
  table holds.
- **A run that fails.** Its error is read as its result would be, so it is
  judged the same way: a wrong column name over the task's own table does
  not label the conversation. A statement refused before it is placed —
  no reach proof — keeps the statement's label, as before.
- **The trail.** The dataset service's row of a read taken as the task's
  own says so and names the call that published the revision; an answer by
  handle is a row of its own (`read-label`), written only when it is
  own-clear. A publish's row says when the revision derives from sealed
  data. The session data service's attach, define and replay rows name
  the attachments read as the task's own (`own-clear`) and the label that
  followed (`label`).
- **Chat needs no rule.** It labels from what an outcome carries: the
  dispatcher's confined flag and `check`'s `confined`.

Surfaces, Tier 1, all additive: `adhocdata.ReadResult.OwnClear`,
`LabelResult`, `Service.ReadLabel`, `ReadLabelRequest`, `AuditReadLabel`,
`BundleProvenance.DerivedFromSealed`, `BundleResult.DerivedFromSealed`;
the wire fields `adhocDerivedFromSealed`, `adhocOwnClear` and
`adhocLabelOnly` (memberships 281 to 283) on the ad-hoc request and reply;
`playground.BundleSpec.DerivedFromSealed`; `sessiondata.Binding.OwnClear`,
`DatasetInfo.OwnClear`, `AuditRecord.OwnClear` and `Label`; the result
stream's `Sealed` beside `Confined`, which now reports the label; a
reason on applied `adhocDataset` trail rows. Changed in meaning, not in
shape: `list_datasets`' and `keep_as`' `confined`, which say whether
reading the dataset confines the task that asks.

Verified by the default lane of `adhocdata` (who reads back as its own
and who does not, a derived publication, the answer under concurrent
republishes, the answer by handle, the bus), of `sessiondata` (the
binding, mixed statements, kept data sets across replays, the wall) and
of play, through the agent dispatcher with the dispatcher attesting each
call at the tier it declared: the sequence above ends exposed and
unlabelled; over a dataset a person published it is labelled at the read
and refused at the exposure; a result published in the round it arrived
in is not exposed. `publish_result` is consequential and a test grant has
nobody to confirm it, so those tests hand it to the window's engine under
an attested call instead of through the dispatcher. Beside the stubs, the
integration lane joins a task's own clear publication with a server table
on one ClickHouse server and finds the result unlabelled, and the
person's run of it, a mixed statement and a data set kept from a person's
dataset labelled. No live run of a model is behind the change.

## Choices for review

- The unit is a dataset alias served at its live revision, ending on a
  structure or publisher change; a bundle is not a unit.
- The token in an `Authorization: Bearer` header rather than the URL;
  the path names the alias, which is therefore visible in logs.
- 256-bit tokens kept as BLAKE3 digests; 64-bit ids; the prefixes `exp_`
  and `bxe_`.
- Lifetime 1 h by default and 24 h at most; 64 exposures; 16 open reads per
  exposure; Go constants rather than environment variables.
- An exposure ends when the window that made it closes, not when its task
  ends.
- Each expose mints a new exposure; a second one for the same alias does not
  replace the first.
- Revoke: the window or task that made it, the runtime, or any window of
  the same app without an agent's call (the person).
- The catalog lists every live exposure to every caller, marked whose it
  is; it never shows a token or a handle.
- The service never gives a token again; a window keeps the answers it got
  and shows the reader lines, token included, in the person's tab.
- A task exposes only what its own call published, not what it may read.
- A task's exposure is refused when the publishing or the exposing call
  declared a tier above CLEAR; the confined tier alone does not refuse on a
  host the person administers.
- On a managed host, the exposure counts as an export (ADR-0317 §SD4) and
  is refused above the export clearance, the person's included, until an
  approval path is wired.
- `expose:<prefix>` at the data reach; its prefix rule is `publish:`'s.
- `expose_dataset` has effect document, not consequential; the service's
  grant check is the gate.
- CSV as a second format, from arrow-go's writer; no range or length for
  CSV.
- A `HEAD` is not a read and is not audited; every `GET` that received data
  is a read, so one ClickHouse statement over a large stream is several.
- Audit events in a domain of their own (`adhoc-expose`) on the generic
  audit row, rather than new fields on the `adhocDataset` trail row.
- The registry carries the seam (source and served-at), so neither the
  listener nor the dataset service imports the other, and host boot is
  unchanged but for the cell.
- The reach wait's bound (5 s); a check at a call that starts a run lets
  the run decide, against the brief's reading that the call-side check
  keeps refusing.
- Reading back (2026-11-23): a dataset read on this process's plane is
  judged by its handle rather than routed through a read of the service.
  Review, 2026-11-23: the label is asked after the run answered, so it is
  judged by every handle the statement names, registered or not; a
  handle unregistered while the run read it used to drop out of the
  question, and a statement joining it with the task's own publication
  came back unlabelled.
- The publisher states what its rows derive from, the service adds what
  the named inputs show, and a publisher that does neither is judged by
  its call's tier.
- A task's exposure ends on a republish that derives from sealed data.
  Review, 2026-11-23: it ends on any republish that is not the exposing
  task's own clear publication, the person's and another task's under
  the same owner included, since the exposure was admitted for the
  standing of the revision it named and the token would otherwise read
  rows the task never had that standing for. The person's own exposure
  follows a republish as before.
- A failed run is judged as a successful one; a statement refused before
  it is placed keeps the statement's label.
- A replay the person causes leaves a kept data set confined; the label
  lifts again only on a replay by the task that reads its own clear
  publication.
- A run of a window under the task's mark is the task's read, as play's
  agent limits take it.
- Review, 2026-11-23: play keeps, with a result's label, the task it was
  judged for. `publish_result` by the person or by another task takes a
  result of some task's own clear publications as sealed data and
  publishes it as derived, and a `check` resting on it is confined for
  them. Open: the window's label (the catalog's `Confined`, which the
  engine stamps on every outcome and the window host reports for
  captures and titles) is one value for every reader and is judged for
  the task whose run made the result, so another task that observes the
  window reads that task's publication unlabelled. A label per reader
  changes what the catalog handler, the engine and the dispatcher
  exchange; it waits for a decision.
- The operator's `keelson('session_datasets')` shows a data set made of a
  task's own clear publications as confined; `OwnClear` is shown only to
  the task that asks.
- `validate_sql`'s and `rewrite_trace`'s `confined` stay a statement's
  property — it names sealed data — since they run nothing.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0145](./0145-sealed-app-data.md) — sealed data, the wall, the reach proof, the listener's Host check.
- [ADR-0240](./0240-adhoc-datasets-v2-sealed-store-owned-capability.md), [ADR-0288](./0288-adhoc-bundles-datasets-published-with-the-applet-that-reads-them.md) — ad-hoc datasets, bundles, the attested publisher.
- [ADR-0406](./0406-session-data-sets-named-reads-a-task-keeps-in-a-clickhouse-session-and-makes-again-from-their-definitions.md) (proposed) — external data on a server, and the destination model followed here.
- [ADR-0403](./0403-a-statement-a-task-wrote-leaves-only-as-a-read.md) (proposed) — a task's text leaves only as a read.
- [ADR-0082](./0082-imzero2-remote-session-auth-tls.md) — remote exposure, deferred.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md), [ADR-0270](./0270-play-operations-catalog-and-agent-limits.md) — operations under a task grant; play's agent limits.
- [ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md), [ADR-0312](./0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md), [ADR-0317](./0317-clipboard-and-export-copy-by-reference-and-one-choke-point.md) — the cell, its sinks, export.
- [ADR-0396](./0396-the-app-contract-a-host-promises.md) — the app contract.
- [How to publish an ad-hoc dataset and query it](../howto/adhoc-datasets.md), §7.
