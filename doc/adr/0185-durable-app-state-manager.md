---
type: adr
status: proposed
date: 2026-08-15
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0185: A manager for durable app state — browse it, and clear it

## Context

Keelson apps now keep three kinds of durable state, and nothing shows a user
what is stored or lets them remove it. Names below (`runtime.appstate`,
`apps/appstate`) are open to review at acceptance.

| kind | decided in | substrate today | live enumeration today |
| --- | --- | --- | --- |
| persist state | [ADR-0026](./0026-app-runtime-and-capability-subjects.md) §SD3 | `boxer.persiststate` | `ScanState` — the raw trail, not latest-wins |
| workingsets | [ADR-0148](./0148-app-workingsets.md) §SD6 | `boxer.facts` | `ListWorkingsets()` — global, live |
| column-width overrides | [ADR-0151](./0151-table-column-width-overrides.md) | `boxer.facts` | `ListColumnWidths(appId)` — needs the id |

**The substrate split is one day old.** [ADR-0105](./0105-keelson-adopts-generated-record-stores.md)'s
Update of 2026-08-14 landed D3a: persist state left `boxer.facts` for a
generated record store on `boxer.persiststate`, and the `FactsStoreI` state
verbs lost their last production caller. Any manager therefore spans two
substrates rather than one table. The costed-options page this work started
from,
[persist-api-surface-recordstore](../adr-background-work/persist-api-surface-recordstore.md),
predates that by two weeks and reads the old world; §2 and §5 of it are
overtaken, §3's surface comparison and §4's app-boundary question are not.

**The motivating case, verified against the tree on 2026-08-15.** play builds a
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
trail. The live read is `HAVING argMax(is_tomb, sk) = 0` on the winning row,
never `WHERE NOT is_tomb` on the candidates — the latter combines with `argMax`
to return the newest surviving non-tombstone row and silently resurrects a
cleared entry. That rule is already shipped and commented in
[`chstore/columnwidths.go`](../../public/keelson/runtime/factsstore/chstore/columnwidths.go);
anything new copies it rather than re-deriving it.

**Two pieces already exist and shape the design.** `keelson('workingsets')`
([`providers/workingsets.go`](../../public/keelson/runtime/introspect/providers/workingsets.go),
ADR-0148 §SD7) is a read-only introspection table over the facts store,
registered in `introspecthost` and reachable by any app through `ch.query.*` —
a browse path that needs no store in an app. And the persist store's DTO
denormalises `AppId` and `Key` into their own filterable sections, its schema
saying why: so that "every key this app owns" is a `WHERE` on a column rather
than a prefix match on the entity id.

Audit / log / lifecycle / grant rows are a *trail*, not state, and are out of
scope. Dock-layout persistence stays the recorded ADR-0026 follow-up it is.

## Design space (QOC)

**Q1 — Where does the read surface come from?**

- *Raw `boxer.facts` SQL in the manager.* Killed: hand-written membership
  arithmetic is the code class ADR-0105 exists to delete, the trap in
  ADR-0171's measured trial, and it cannot see persist state at all now that
  D3a has moved it.
- *A store handed to the app.* Killed by the constraint above.
- *Introspection providers, one table per kind.* Chosen — SD1. The pattern is
  shipped, read-only by contract, and costs no new capability.

**Q2 — What carries deletion?**

- *A privileged capability type-asserted off the frame context*, the
  `colwidth.HostI` shape widened, with the host implementing it only for the
  manager's app id. Cheapest by far. Killed: an allowlist-by-app-id is a gating
  concept ADR-0155 §SD1 does not have, it asks the user for nothing, and it
  produces no audit record — it grants silently exactly what ADR-0026 §SD7
  exists to make explicit.
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

### SD1 — Read surface: one introspection table per kind

Three `keelson()` tables, one per kind, registered in `introspecthost`
alongside the existing one ([ADR-0094](./0094-keelson-introspection-tables.md)
for the provider contract):

- `keelson('workingsets')` — exists, unchanged.
- `keelson('column_widths')` — new, over the global list SD2 adds.
- `keelson('app_state')` — new, over the persist reader SD2 adds.

Each answers *live* rows: the set a restore would find, not the write trail.
The trail stays a query against the underlying table, which is ADR-0148 §SD7's
stance and is unchanged here.

Registration is unconditional and a nil source yields an empty table rather
than an absent one — the `keelson('windows')` precedent the workingsets
provider already follows, so the set of table names does not depend on what a
host happened to wire.

### SD2 — What the store surface grows, and what it does not

Precisely one new interface method, one new reader, and nothing for
workingsets:

- **`FactsStoreI.ListAllColumnWidths()`** — the app-id-less sibling of
  `ListColumnWidths(appId)`. A separate method rather than an empty-string
  sentinel on the existing one: a stringly-typed "" meaning "all apps" is the
  kind of overload a caller gets wrong once and silently. The `chstore`
  implementation is `composeListColumnWidthsSql` with the app predicate
  dropped and the app id read back as a column instead of being supplied;
  the `HAVING argMax(is_tomb, sk) = 0` collapse is unchanged and is the part
  that must not be rewritten.
- **A live-state reader on the persist side.** `ScanState` returns the raw
  trail ordered by `Order ASC`, and only per-key `GetLive` interprets the
  tombstone, so there is no global live read. The fold is a Go pass over that
  ascending sequence — last row per key wins, a tombstone means absent — behind
  a small `persist` interface implemented by both `StoreBackend` and
  `MemoryBackend`, which `introspecthost` takes as a nillable dep exactly as
  it takes `Facts`.

  A generated `ScanLive<Kind>` verb in `recordstore/gen` would serve every
  future store and push the collapse into SQL. It is deliberately **not**
  taken here: one consumer is not evidence for a generator feature, and the Go
  fold is correct against the same ordering guarantee the generated verb would
  rely on. Recorded as the exit if a second consumer appears.
- **Workingsets: nothing.** `ListWorkingsets()` is already the global live list.

### SD3 — Deletion rides a subject family, requested not published

A new family under the ADR-0026 §SD3 taxonomy, **mutation-only**:

```
runtime.appstate.delete    one entry: kind + app id + that kind's key tuple
runtime.appstate.forget    one app: every kind at once
```

There is no `list` verb. Reading is SD1's tables, and putting the same read on
two surfaces buys nothing but a second thing to keep true.

A host-side service owns the collaborators (the facts store, the persist
backend) and dispatches; the manager declares the family in `Manifest.Caps`
with a Reason, so the ADR-0026 §SD7 broker prompts on Mount. The grant is
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

It is **not atomic and does not claim to be**. The kinds are tombstone appends
across two substrates; there is no transaction spanning them, and the reply
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

- **A sqlapplet book** over SD1's tables, on the ADR-0170 §SD7 precedent: one
  chapter per kind plus a per-app rollup. It needs only SD1 and SD2, so
  browse and inspect work before the delete seam is built.
- **`apps/appstate`** afterwards, on the [`apps/capinspector`](../../apps/capinspector)
  precedent — a registered app package, not a new `main()`
  ([CODINGSTANDARDS § Entry Points](../../CODINGSTANDARDS.md#entry-points)) —
  adding per-row delete and per-app forget over SD3.

### SD7 — Payloads are described, not served

The tables report a payload's size and kind, never its bytes. Workingset config
is facts-CBOR decodable only by the owning app's codec and runs to tens of
kilobytes; persist values are opaque bytes an app chose. The existing
workingsets provider already takes this cut for `config_bytes` and says why,
and a cross-app reader is the wrong place to widen it. Column-width rows are
the exception that needs no rule: every field of one is metadata, so they
render whole.

A "reveal payload" verb on SD3's family — audited, per-request, never a table
column — is **deferred**, with the trigger being a concrete case where the
size and kind are not enough to decide whether to delete something.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `factsstore.FactsStoreI` (exported Go API under `public/`) | +1 method, `ListAllColumnWidths` (SD2) | `chstore.Store`, `InMemoryFactsStore`, one test fake in `logbridge` |
| `persist` (exported Go API under `public/`) | new live-state reader interface + impls on `StoreBackend` / `MemoryBackend` (SD2) | `introspecthost.Deps` |
| `introspecthost.Deps` | +1 nillable dep (the persist reader) | the carousel's wiring call |
| `keelson()` table set | +`app_state`, +`column_widths` (SD1) | the introspection table docs; any reader enumerating table names |
| `runtime.appstate.>` subject family | new, mutation-only (SD3) | ADR-0026 §SD3 taxonomy; the capability registry and its capinspector description |
| `Manifest.Caps` of the new app | declares `runtime.appstate.>`, non-sticky | the §SD7 broker prompt copy |
| `boxer.facts` / `boxer.persiststate` DDL | **unchanged** — only tombstone appends | nothing; recorded because a manager that deletes rows would change it |

## Alternatives

Covered per question above. The standing rejections are raw facts SQL in an
app (the code class ADR-0105 deletes, and blind to `boxer.persiststate`), a
store handed to an app (outside the capability model), a privileged
type-asserted capability (silent, unaudited, and a gating concept §SD1 lacks),
and a capinspector tab (wrong subject, and it would give that app a privileged
capability it currently does without).

Two more, both declined rather than dismissed:

- **A `boxer appstate` subcommand as the sole delete path.** Rejected as the
  sole path only. It remains attractive as a *co-surface* — it needs no
  capability, works when the GUI cannot start, and is the natural home for
  bulk operations. Not scheduled here; adding it later costs a CLI wrapper
  over the same host-side collaborators.
- **A generated `ScanLive<Kind>` verb** instead of SD2's Go fold. Declined on
  evidence, not merit: one consumer. Named as the exit.

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
- `keelson('app_state')` is the first read path onto `boxer.persiststate` since
  D3a moved it there; the table was otherwise reachable only by an app's own
  `Get`.

### Negative

- A new subject family for two verbs. The cost is real and is paid for consent
  and audit; a manager that needed neither would not justify it.
- The manager depends on the introspection host being enabled. With it off,
  browse is empty even though delete would still work — a split-brain the app
  has to state rather than hide.
- SD2's Go fold reads a key's whole trail to answer "what is live". For app
  state that is small, and it is unbounded only in the sense the underlying
  retention question is.
- `forget` is a fan-out over two substrates with no transaction. SD4 makes the
  partial outcome explicit rather than removing it.
- One more `FactsStoreI` method, on an interface ADR-0105 D5 expects to hollow
  out opportunistically. It is added to the shape that is being replaced —
  accepted because the alternative is blocking a user-visible gap on a
  migration with no schedule.

### Neutral

- Tombstones mean the trail survives a clear; SD5 says so plainly rather than
  letting "delete" imply erasure.
- play's missing clear gesture is fixed independently and is not gated on any
  of this.
- Apps that store nothing are absent from every table; the manager shows what
  exists, not a roster.

## Migration — Tier 1

- **Breaks.** Three compile sites, all in-repo: `chstore.Store`,
  `InMemoryFactsStore`, and the `blockingStore` fake in `logbridge`'s tests
  must each grow `ListAllColumnWidths`. Nothing breaks at rest.
- **Path.** Additive otherwise. New providers register alongside existing ones;
  the new `introspecthost` dep is nillable and defaults to today's behaviour;
  the subject family is new, so no existing manifest changes.
- **Data.** No DDL change and no row rewrite on either table. A clear is an
  append, so rolling back this ADR leaves tombstones behind that the existing
  read paths already interpret correctly.
- **Old shape.** `ListColumnWidths(appId)` keeps its callers and its meaning;
  the resolver is untouched.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the fold, the providers and the service over
  the in-memory backends; `clickhouse-local` for the `chstore` read;
  `//go:build integration` for anything against a live server.
- **What would fail.**
  - A `ListAllColumnWidths` test that writes an override, tombstones it, and
    asserts it is **absent** — the `WHERE NOT is_tomb` regression, which is
    otherwise invisible until a user's cleared column comes back. Run against
    both backends, since "latest" is insertion order in one and `(ts, id)` in
    the other.
  - The same assertion for the persist fold: a `Set`, a `Delete`, and a global
    list that does not contain the key.
  - A cross-app assertion: two app ids write overrides, the global list returns
    both, and the per-app list still returns one — so the new method cannot be
    quietly implemented by dropping the filter in the wrong place.
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

- **M0 — read surface.** `ListAllColumnWidths` + the persist live fold + two
  providers + the `introspecthost` dep. Browse works from play and sqlapplet.
- **M1 — the book.** A `bookappstate` sqlapplet suite over the three tables.
- **M2 — the seam.** `runtime.appstate.>` service, request DTOs, capability
  registration and its capinspector description.
- **M3 — the app.** `apps/appstate` with per-row delete and per-app forget,
  including the confirmation step `forget` needs.

Not milestones here, recorded so they are not mistaken for oversights: play's
header clear gesture (ADR-0151 M6 follow-through, small and independent), the
`boxer appstate` co-surface, the payload-reveal verb (SD7), and a generated
`ScanLive` verb (SD2).

## Status

Proposed 2026-08-15. Nothing implemented. The design dialogue settled three
questions — the delete seam, the UI's home, and the delete granularity — and
this record is the result; the sequencing in Milestones is deliberate, so M0
can proceed without M2 being settled in code.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Update` entry / Tier 3 new superseding ADR).

## References

- [ADR-0026: App runtime and capability subjects](0026-app-runtime-and-capability-subjects.md) — §SD3 the subject taxonomy this family joins, §SD6 the facts table, §SD7 the broker that prompts.
- [ADR-0094: keelson introspection tables](0094-keelson-introspection-tables.md) — the provider contract SD1 uses.
- [ADR-0105: keelson adopts generated record stores](0105-keelson-adopts-generated-record-stores.md) — D3a, which moved persist state to its own table on 2026-08-14; D5, the opportunistic posture SD2 adds a method against.
- [ADR-0148: App workingsets](0148-app-workingsets.md) — §SD6 the kind, §SD7 the provider SD1 extends, and the data-centricity invariant.
- [ADR-0151: Table column-width overrides](0151-table-column-width-overrides.md) — the kind, the tombstone rule, the blocked-M4 constraint, and the unwired clear gesture.
- [ADR-0155: App embed seam](0155-app-embed-seam.md) — §SD1, the optional-capability pattern this case does not fit.
- [ADR-0170: Data catalog competence](0170-data-catalog-competence.md) — §SD6 CLI placement, §SD7 the sqlapplet-book rendering SD6 copies.
- [ADR-0025](0025-pushout-forget-architecture.md) / [ADR-0027](0027-pushout-forget-swiss-fadp.md) — where erasure vocabulary lives; SD5 stays out of it.
- [persist-api-surface-recordstore](../adr-background-work/persist-api-surface-recordstore.md) — the costed options this started from; §2 and §5 are overtaken by ADR-0105's 2026-08-14 Update.
- [doc/howto/table-column-widths.md](../howto/table-column-widths.md) — the clear-gesture recipe play has not wired.
