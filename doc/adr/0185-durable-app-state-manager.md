---
type: adr
status: accepted
date: 2026-08-15
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-22
---

# ADR-0185: A manager for durable app state — browse it, and clear it

## Context

Keelson apps now keep three kinds of durable state, and nothing shows a user
what is stored or lets them remove it. Names below (`runtime.appstate`,
`apps/appstate`) are open to review at acceptance.

| kind | decided in | substrate | live enumeration |
| --- | --- | --- | --- |
| persist state | [ADR-0026](./0026-app-runtime-and-capability-subjects.md) §SD3 | `boxer.persiststate` | `ScanLiveState` over the `state/` prefix |
| workingsets | [ADR-0148](./0148-app-workingsets.md) §SD6 | `boxer.persiststate` | `ScanLiveWorkingset` over `ws/`; `ListWorkingsets()` |
| column-width overrides | [ADR-0151](./0151-table-column-width-overrides.md) | `boxer.persiststate` | `ScanLiveColumnWidth` over `cw/`; `ListColumnWidths(appId)` per app |

**One substrate.** All three kinds live on one generated store over
`boxer.persiststate` (ADR-0105 D3a, and its Update of 2026-08-15, built
2026-09-22): each kind is a component, every live row also carries an `Owner`
component naming the app, and keys are kind-prefixed. The generated state view
answers "what is live" per kind in one statement — `ScanLive<Kind>`, optionally
narrowed by key prefix (ADR-0100's Update of 2026-09-22). This ADR was drafted
against two substrates — persist state on its own table, the other two on
`boxer.facts` — and was one of the reasons ADR-0105 unified them. The
costed-options page this work started from,
[persist-api-surface-recordstore](../adr-background-work/persist-api-surface-recordstore.md),
predates both moves; §2 and §5 of it are overtaken, §3's surface comparison
and §4's app-boundary question are not.

**The motivating case, re-verified against the tree on 2026-09-22.** play builds a
`colwidth.Resolver` and captures widths on drag-end
([`play_table_attr.go`](../../apps/play/play_table_attr.go)), but no call site
anywhere invokes `Resolver.Clear` or `ClearAll`, and play contains no
`c.ContextMenu` at all. ADR-0151's M6 shipped the clear gesture as a recipe in
[the width how-to](../howto/table-column-widths.md) and then shipped the
binding it needs; play never wired either. One drag writes two rows — the
instance tier and the column tier — which survive restart and then apply to
every later result carrying a column of that name and type. A user who drags a
column has no way back.

That case splits, and only the second half needs this ADR. Wiring play's header
context menu to the existing `Clear` / `ClearAll` is small, local, and ADR-0151's
own unfinished business; it should land on its own and is not a milestone here.
What is left is the general problem: state accumulates across apps with no
viewer.

**Two constraints bound the answer.**

*An app must not hold a `FactsStoreI`.* ADR-0151's blocked-M4 Update records
why: `recordstore.ExecutorI` is SQL text plus Arrow batches, so a store-holding
app reads and writes every other app's rows outside the capability model
ADR-0026 exists to establish. colwidth's answer was a *narrow* capability
(`colwidth.HostI`, three verbs, type-asserted off the frame context per
[ADR-0155](./0155-app-embed-seam.md) §SD1). A cross-app manager is the case
that pattern does not cover: it must see every app's state by definition, and
§SD1 capabilities go to whoever type-asserts them.

*Deleting is tombstoning.* All three kinds are latest-wins over an append-only
trail. The live read collapses each key to its newest row *before* testing
what that row is — testing first skips a tombstone, which carries no
component, and resurrects the row it cleared. The generated `ScanLive<Kind>`
does exactly that and has a test that fails if the order is reversed, so
nothing here writes the collapse by hand.

**Two pieces already exist and shape the design.** `keelson('workingsets')`
([`providers/workingsets.go`](../../public/keelson/runtime/introspect/providers/workingsets.go),
ADR-0148 §SD7) is a read-only introspection table over the state store,
registered in `introspecthost` and reachable by any app through `ch.query.*` —
a browse path that needs no store in an app. And every state row's `Owner`
component carries the app id in plain form, so "everything this app owns" is
one scan of one component, whichever kinds the rows are.

Audit / log / lifecycle / grant rows are a *trail*, not state, and are out of
scope. Dock-layout persistence stays the recorded ADR-0026 follow-up it is.

## Design space (QOC)

**Q1 — Where does the read surface come from?**

- *Raw SQL over the state table in the manager.* Killed: hand-written
  membership arithmetic is the code class ADR-0105 exists to delete and the
  trap in ADR-0171's measured trial, and the live collapse is exactly the part
  a hand-written query gets wrong.
- *A store handed to the app.* Killed by the constraint above.
- *Introspection providers, one table per kind.* Workable, but each table
  carries columns only its kind has, and "everything this app stores" becomes
  a union the reader writes. Superseded by the next option once every kind
  shared one store.
- *One introspection table over every kind.* Chosen — SD1. The pattern is
  shipped, read-only by contract, and costs no new capability.

**Q2 — What carries deletion?**

- *A privileged capability type-asserted off the frame context*, the
  `colwidth.HostI` shape widened, with the host implementing it only for the
  manager's app id. Cheapest by far. Killed: an allowlist-by-app-id is a gating
  concept ADR-0155 §SD1 does not have, it asks the user for nothing, and it
  produces no audit record — it grants silently exactly what ADR-0026 §SD7
  exists to make explicit.
- *Reuse `runtime.persist.{alias}.{key}.delete` for the persist third.* It
  exists, it is already request/reply and therefore audited, and the service
  admits a client holding a broader `runtime.persist.>` cap — so a third of the
  job needs no new subject at all. Killed: that cap is one `Pub` filter over
  the whole family, so obtaining `delete` on a key also grants `get` and `set`
  on every app's state — the manager would hold the power to read and overwrite
  precisely what §SD7 refuses even to render. It also shrinks the new family
  rather than removing it, since workingsets and column widths have no
  subject to reuse.
- *No app write path; deletion only from a `boxer appstate` subcommand*, on the
  [ADR-0170](./0170-data-catalog-competence.md) §SD6 precedent. Zero new
  capability surface, and the escalation question never arises. Killed as the
  sole answer: the user this started from dragged a column in a GUI and is not
  served by a terminal. Kept as a co-surface — see Alternatives.
- *A subject family with a declared, broker-prompted capability.* Chosen —
  SD3.

**Q3 — Where does the browse surface live?**

- *A tab in [`apps/capinspector`](../../apps/capinspector).* Killed: it
  conflates "what may this app do" with "what has this app stored", and
  capinspector declares no `Caps` today — this would hand the whole app a
  privileged one.
- *A new app, and nothing before it.* Killed on sequencing, not on merit:
  nothing is usable until the delete seam is designed and built.
- *A sqlapplet book first, the app after.* Chosen — SD6.

## Decision

### SD1 — Read surface: one introspection table over every kind

One new `keelson()` table, `keelson('app_state')`, registered in
`introspecthost` ([ADR-0094](./0094-keelson-introspection-tables.md) for the
provider contract): one row per live entry, whatever its kind.

| column | meaning |
| --- | --- |
| `kind` | `persist`, `workingset`, `column_width` — or `unknown` for a row carrying no kind this provider knows |
| `app_id` | the owning app |
| `key` | the entry's identity within its app and kind: the persist key, the workingset name, or `tier/scope/column_key` |
| `entity_id` | the store's own key for the entry |
| `payload_bytes` | size of a persist value or a workingset config; 0 for a column width |
| `detail` | the kind's metadata: a workingset's launch kind and close reason, a width's points and captured font size |
| `written_at` | when the live version was written |
| `run_id`, `instance_key` | provenance: the run and the window that wrote it |

Its rows are *live*: the set a restore would find, not the write trail. The
trail stays a query against the underlying table, which is ADR-0148 §SD7's
stance and is unchanged here. `keelson('workingsets')` stays as it is; its
rows are this table's `workingset` rows with that kind's own columns.

An `unknown` row is shown rather than skipped. A kind added to the store
before this provider learns it is still stored state, and a manager that hid
it would be the one place it could not be seen or cleared.

Registration is unconditional and a nil source yields an empty table rather
than an absent one — the `keelson('windows')` precedent the workingsets
provider already follows, so the set of table names does not depend on what a
host happened to wire.

### SD2 — What the store surface grows: nothing

The state store already answers every read the manager needs. The provider
opens its own read-only store on `introspecthost.Deps.PersistExec` — the path
`keelson('runtime_events')` already takes (ADR-0191 §SD7), chosen there so a
scan never contends with the writer's pending buffer — and calls the
generated `ScanLiveOwner`: every live row carries `Owner`, so one live scan
of that component is every live entry of every kind, tombstones already
excluded. A host with no durable backend gets an empty table, the
degradation every other nillable source already has.

No `FactsStoreI.ListAllColumnWidths` and no Go fold over the persist trail:
both were the cost of the two substrates, and went with them. A narrower
read, should one be needed, is `ScanLive<Kind>` over the kind's key prefix,
with the escaped app id appended for one app — the
escaped app id (`persiststore.ColumnWidthAppPrefix` and siblings), which is
exact even for app ids that nest.

### SD3 — Deletion rides a subject family, requested not published

A new family under the ADR-0026 §SD3 taxonomy, **mutation-only**:

```
runtime.appstate.delete    one entry: kind + app id + that kind's key tuple
runtime.appstate.forget    one app: every kind at once
```

There is no `list` verb. Reading is SD1's table, and putting the same read on
two surfaces buys nothing but a second thing to keep true.

A host-side service owns the collaborator (the state backend,
`persist.StoreBackend`) and dispatches; the manager declares the family in `Manifest.Caps`
with a Reason, so the ADR-0026 §SD7 broker prompts on Mount. That arrangement
has a worked precedent to copy rather than invent — watchbill
([ADR-0234](./0234-watchbill-client-protocol-and-worker-presence.md),
[ADR-0236](./0236-watchbill-management-app.md)) exports a `ClientCaps()` helper
beside its subject constants, and its management app declares the result in
`Manifest.Caps`. The grant is
**not sticky**: a remembered, silent grant to delete every app's data is
precisely what should not exist, and one prompt per session is proportionate
for an app a user opens deliberately.

**The verbs are request/reply, not publish, and that is load-bearing.** The
in-proc bus records an `AuditRecord` in `RequestWithTimeout` — including
permission denials — and the carousel wires that sink to the facts store.
`Publish` is not audited. A published delete would be an unrecorded one.

### SD4 — `forget` fans out and never stops early

`forget` attempts every kind and reports a per-kind outcome. It does not
abandon the rest on the first failure, for the reason ADR-0151 §M6 records for
`ClearAll`: a partial clear is the worst outcome of the gesture, leaving some
entries cleared and others not with nothing to tell them apart.

It is **not atomic and does not claim to be**. The kinds share one store, so
their tombstones can be appended together and flushed as one insert, but this
record does not rest a guarantee on ClickHouse's insert semantics; the reply
says what happened per kind rather than pretending a single verdict.

### SD5 — "Forget" clears, it does not erase

Every delete is a tombstone append. The superseded rows stay, which is what
keeps the trail readable and is the property ADR-0148 chose deliberately.

So this surface is a UI-level clear, in the sense a browser's cookie manager
is: it makes an entry stop applying. It is **not** erasure, and it must not be
described as satisfying an erasure obligation — the pushout work
([ADR-0025](./0025-pushout-forget-architecture.md),
[ADR-0027](./0027-pushout-forget-swiss-fadp.md)) is where that vocabulary
lives, and nothing here is wired to it. Bounding the trail is the retention
question every fact kind already has.

### SD6 — Browse ships as a book first, then as an app

- **A sqlapplet book** over SD1's table, on the ADR-0170 §SD7 precedent: one
  chapter per kind plus a per-app rollup. It needs only SD1 and SD2, so
  browse and inspect work before the delete seam is built.
- **`apps/appstate`** afterwards, on the [`apps/watchbill`](../../apps/watchbill)
  precedent (ADR-0236) — a registered app package, not a new `main()`
  ([CODINGSTANDARDS § Entry Points](../../CODINGSTANDARDS.md#entry-points)),
  browsing through introspection and mutating over the bus with its client's
  caps declared in `Manifest.Caps` — adding per-row delete and per-app forget
  over SD3.

### SD7 — Payloads are described, not served

The table reports a payload's size and kind, never its bytes. Workingset config
is facts-CBOR decodable only by the owning app's codec and runs to tens of
kilobytes; persist values are opaque bytes an app chose. The existing
workingsets provider already takes this cut for `config_bytes` and says why,
and a cross-app reader is the wrong place to widen it. Column-width rows are
the exception that needs no rule: every field of one is metadata, so `detail`
carries it whole.

A "reveal payload" verb on SD3's family — audited, per-request, never a table
column — is **deferred**, with the trigger being a concrete case where the
size and kind are not enough to decide whether to delete something.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `introspecthost` | +1 provider registration over `Deps.PersistExec`, which ADR-0191 §SD7 already put there | nothing — the host's wiring call is unchanged |
| `keelson()` table set | +`app_state` (SD1) | the introspection table docs; any reader enumerating table names |
| `runtime.appstate.>` subject family | new, mutation-only (SD3) | ADR-0026 §SD3 taxonomy; the capability registry and its capinspector description |
| `Manifest.Caps` of the new app | declares `runtime.appstate.>`, non-sticky | the §SD7 broker prompt copy |
| `boxer.persiststate` DDL | **unchanged** — only tombstone appends | nothing; recorded because a manager that deletes rows would change it |

## Alternatives

Covered per question above. The standing rejections are raw SQL over the state
table in an app (the code class ADR-0105 deletes), a
store handed to an app (outside the capability model), a privileged
type-asserted capability (silent, unaudited, and a gating concept §SD1 lacks),
and a capinspector tab (wrong subject, and it would give that app a privileged
capability it currently does without).

One more, declined rather than dismissed:

- **A `boxer appstate` subcommand as the sole delete path.** Rejected as the
  sole path only. It remains attractive as a *co-surface* — it needs no
  capability, works when the GUI cannot start, and is the natural home for
  bulk operations. Not scheduled here; adding it later costs a CLI wrapper
  over the same host-side collaborator.

A Go fold over the persist trail and a `ListAllColumnWidths` facts verb were
in an earlier draft of SD2. They answered two substrates, and ADR-0105's
unification removed the reason for both.

## Consequences

### Positive

- Browse and inspect arrive without any new capability surface, and without an
  app ever holding a store — the constraint that blocked ADR-0151 M4 is
  satisfied by construction for the read half rather than worked around.
- The read half ships before the write half is built, so the "no viewer"
  problem is fixed on its own schedule.
- Deleting another app's data is visible three ways: the user is prompted, the
  grant is recorded, and each call lands an audit row through a path that
  already exists.
- `keelson('app_state')` is the first *live, cross-app* view of
  `boxer.persiststate`. `keelson('runtime_events')` shows that store's trail
  for the current run and an app can `Get` its own keys; neither answers what
  is stored, for whom, right now.

### Negative

- A new subject family for two verbs. The cost is real and is paid for consent
  and audit; a manager that needed neither would not justify it.
- The manager depends on the introspection host being enabled. With it off,
  browse is empty even though delete would still work — a split-brain the app
  has to state rather than hide.
- `forget` is several tombstones, not a transaction. SD4 makes a partial
  outcome explicit rather than removing it.

### Neutral

- Tombstones mean the trail survives a clear; SD5 says so plainly rather than
  letting "delete" imply erasure.
- play's missing clear gesture is fixed independently and is not gated on any
  of this.
- Apps that store nothing are absent from every table; the manager shows what
  exists, not a roster.

## Migration — Tier 1

- **Breaks.** Nothing: SD2 adds no method to any interface.
- **Path.** Additive. The new provider registers alongside existing ones
  and read deps `introspecthost` already takes, so its `Deps` struct and every
  caller of it are untouched; the subject family is new, so no existing
  manifest changes.
- **Data.** No DDL change and no row rewrite. A clear is an append, so rolling
  back this ADR leaves tombstones behind that the existing read paths already
  interpret correctly.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the provider and the service over the
  in-memory backends; `clickhouse-local` for the provider's scan over a real
  executor, guarded so a box without the binary skips rather than fails;
  `//go:build integration` for anything against a live server.
- **What would fail.**
  - A provider test that writes an entry, deletes it, and asserts it is
    **absent** from the table — the resurrection the generated collapse
    prevents, checked at the surface a user sees.
  - A cross-app assertion: two app ids write entries of different kinds, and
    the table returns every one, each under its own app and kind.
  - A superseded version is not a row: two writes to one key give one row
    carrying the newer one.
  - A service test asserting the delete verbs are refused without the
    capability, and that the refusal is itself audited (the bus records denials
    in `RequestWithTimeout`).
  - A `forget` test where one kind's delete fails and the others still run —
    SD4's rule, which is a convention nothing otherwise checks.
  - A provider shape test pinning the table columns, so SD7's "size not bytes"
    cut cannot be widened by accident.
- **Gap.** That the broker actually prompts, and that the prompt says something
  a user can act on, is not covered by an automated lane — it is a manual check
  at the milestone. SD5's claim that a clear is not erasure is a scoping
  statement, not a testable one.

## Milestones

- **M0 — read surface.** One provider over the generated live scan. Browse
  works from play and sqlapplet.
- **M1 — the book.** A `bookappstate` sqlapplet suite over the table.
- **M2 — the seam.** `runtime.appstate.>` service, request DTOs, capability
  registration and its capinspector description.
- **M3 — the app.** `apps/appstate` with per-row delete and per-app forget,
  including the confirmation step `forget` needs.

Not milestones here, recorded so they are not mistaken for oversights: play's
header clear gesture (ADR-0151 M6 follow-through, small and independent), the
`boxer appstate` co-surface, and the payload-reveal verb (SD7).

## Status

Accepted 2026-09-22. Nothing implemented. The design dialogue settled three
questions — the delete seam, the UI's home, and the delete granularity — and
this record is the result; the sequencing in Milestones is deliberate, so M0
can proceed without M2 being settled in code.

Revised in place on 2026-09-22, before acceptance, after ADR-0105's
unification of the state kinds was built: one substrate, the generated
`ScanLive<Kind>` in place of the fold, no new `FactsStoreI` method, SD4's
fan-out on one store, and SD1 as one table over every kind rather than one
per kind. The same pass added Q2's `runtime.persist.>` option
and moved SD6's app precedent to `apps/watchbill`. The motivating case was
re-checked on that date and still holds.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Update` entry / Tier 3 new superseding ADR).

## Updates

### 2026-09-22 — M0 built: `keelson('app_state')`

The read surface is SD1 as accepted: one provider,
[`providers/appstate.go`](../../public/keelson/runtime/introspect/providers/appstate.go),
registered unconditionally in `introspecthost` over `Deps.PersistExec`, one
live scan of `Owner` per query. No interface grew, as SD2 said.

Verified over `clickhouse-local` through the runtime's own state backend: an
entry of each kind for two apps reads back once each, under its own app and
kind; a superseded version is not a row; a deleted entry is absent. A shape
test pins the column set, so SD7's cut cannot be widened by a column. The
live-server lane was not added for M0: the provider's only statement is the
generated live scan, which ADR-0105's work already runs through the HTTP
executor against a live server.

### 2026-09-22 — M1 built: the `appstate` book

Four chapters in [`apps/sqlapplet/bookappstate`](../../apps/sqlapplet/bookappstate),
as SD6 describes: a per-app rollup and one chapter per kind. The workingsets
chapter reads `keelson('workingsets')`, which already carries that kind's
own columns; the others read `keelson('app_state')`. The column-width
chapter splits the `tier/scope/column_key` key taking the middle as the
scope, since a table tag may itself contain `/`. Every chapter is executed
in the default lane against entries written through the runtime's state
backend.

### 2026-09-22 — M2 built: `runtime.appstate.{delete,forget}`

The seam is SD3 and SD4 as accepted:
[`runtime/appstate`](../../public/keelson/runtime/appstate) holds the
subjects, `ClientCaps()` (not sticky), a `Client`, and the `Service` the host
starts under `runtime.appstate` over `persist.StoreBackend`; the wire forms
are the `appstaterequest` / `appstatereply` codecs over a new `asReq…` /
`asOutcome…` vocabulary cohort. Four decisions SD3 left to the build:

- **A delete names an entry as `keelson('app_state')` shows it** — kind and
  key, or the store key for an `unknown` row — and `persiststore` owns the
  one mapping both sides use (`KindOf`, `EntryKeyOf`, `EntityIdOf`), so the
  table and the verb cannot drift apart.
- **A delete must address a live entry of the named app and kind**, else it
  is refused. Tombstoning a key that was never there, or another app's
  entry, would record a clear that did not happen.
- **Without a durable store the service still answers**, refusing with the
  reason, rather than letting a request time out.
- **The family is served whenever the host enables it**
  (`hostboot.Services.AppState`), and capinspector describes it as its own
  capability.

Verified in the default lane over `clickhouse-local` through a bus client:
a delete clears one entry and the owning app's own read sees it gone; a
delete of nothing, of another app's entry, or of a malformed key is refused
and lands nothing; an `unknown` row is cleared by its store key; forget
clears every kind of one app and nothing of another; a forget whose first
delete fails still attempts the rest and reports the failure per kind; a
request from an app without the capability is refused and that refusal is
an audit row. The broker's Mount prompt and its copy are not covered by a
lane — the manual check the verification plan names, due with M3, which is
the first app to declare the capability.

### 2026-09-22 — M3 built: the App state window

[`apps/appstate`](../../apps/appstate) is SD6's window: the apps that keep
state on the left with their entry count and size, the selected app's
entries in the middle, a delete per entry and a forget per app. Forget's
confirmation is an armed banner naming the app and the entry count, and only
its own button clears. The window declares `appstate.ClientCaps()` and
nothing else; it reads `keelson('app_state')` from the process's local query
endpoint on an off-frame poller, and runs each verb in its own goroutine.

Two things the record said that the build found otherwise:

- **There is no `ch.query.*` subject.** The Context calls the introspection
  tables "reachable by any app through `ch.query.*`"; apps reach them over
  HTTP on the local endpoint, as the watchbill window does, and this one
  does the same.
- **That read is a network capability the manifest does not justify.** The
  capslock gate reports `apps/appstate :: CAPABILITY_NETWORK`, the same entry
  `apps/watchbill` carries for the same read. Accepting it, or giving the
  endpoint read a declared cap, is a reviewer's decision and is left open.

Verified in the default lane — the window mounted beside the service over
the runtime's own backend on one bus: forget clears nothing while armed or
after a cancel, and a confirmed forget clears exactly that app; a delete
clears its entry; a refusal shows the service's reason. And in a headless
host (`apps/appstate/scenes`, plus a one-off run over three seeded entries of
a probe app): the window reads the live store without error, a delete
lowers the count by one, and a confirmed forget clears the rest. Still
unverified: that the broker's Mount prompt appears and reads well — the
headless run granted the capability without showing one, so the manual
check the verification plan names remains to be done on the desktop host.

### 2026-09-22 — the read is a declared capability (ADR-0253)

The open item of the M3 note is closed the second way: the manager reads
`keelson('app_state')` as a request on `keelson.query.app_state`, one sticky
grant declared beside `ClientCaps()`, served by the host over the ADR-0094
§SD4 engine ([ADR-0253](0253-introspection-table-reads-as-a-bus-capability.md),
proposed). SD1's "costs no new capability" no longer holds and was never
quite true — the read was off the capability system rather than free of
it. The capslock finding for `apps/appstate` is gone, and the window says
"no bus to read through" only where the host minted none.

The same day's live check also settles the M3 note's other open item, the
other way round: the Mount prompt did not appear because there is none —
the window host mints the app's bus client from `Manifest.Caps` as
declared, and the broker prompts only for a `runtime.cap.request` made
later. SD3's "the broker asks on every Mount" describes ADR-0026 §SD7's
intent, not a built behaviour; the non-sticky flag stays on the grant for
the day that prompt exists.

## References

- [ADR-0026: App runtime and capability subjects](0026-app-runtime-and-capability-subjects.md) — §SD3 the subject taxonomy this family joins, §SD6 the facts table, §SD7 the broker that prompts.
- [ADR-0094: keelson introspection tables](0094-keelson-introspection-tables.md) — the provider contract SD1 uses.
- [ADR-0105: keelson adopts generated record stores](0105-keelson-adopts-generated-record-stores.md) — D3a, which moved persist state to its own table on 2026-08-14, and its Update of 2026-08-15 (built 2026-09-22), which moved workingsets and column widths after it.
- [ADR-0148: App workingsets](0148-app-workingsets.md) — §SD6 the kind, §SD7 the provider SD1 extends, and the data-centricity invariant.
- [ADR-0151: Table column-width overrides](0151-table-column-width-overrides.md) — the kind, the tombstone rule, the blocked-M4 constraint, and the unwired clear gesture.
- [ADR-0191: Runtime instance attribution](0191-runtime-instance-attribution.md) — §SD7, the read path over an executor that SD2 reuses.
- [ADR-0100: Recordstore](0100-recordstore-generated-leeway-clickhouse-store.md) — Update 2026-09-22, `ScanLive<Kind>` and `ScanOpts.KeyPrefix`.
- [ADR-0234](0234-watchbill-client-protocol-and-worker-presence.md) / [ADR-0236](0236-watchbill-management-app.md) — the client-caps helper and management-app shape SD3 and SD6 copy.
- [ADR-0155: App embed seam](0155-app-embed-seam.md) — §SD1, the optional-capability pattern this case does not fit.
- [ADR-0170: Data catalog competence](0170-data-catalog-competence.md) — §SD6 CLI placement, §SD7 the sqlapplet-book rendering SD6 copies.
- [ADR-0025](0025-pushout-forget-architecture.md) / [ADR-0027](0027-pushout-forget-swiss-fadp.md) — where erasure vocabulary lives; SD5 stays out of it.
- [persist-api-surface-recordstore](../adr-background-work/persist-api-surface-recordstore.md) — the costed options this started from; §2 and §5 are overtaken by ADR-0105's 2026-08-14 Update.
- [doc/howto/table-column-widths.md](../howto/table-column-widths.md) — the clear-gesture recipe play has not wired.
