---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-07-31 to answer a
> question raised while wiring ADR-0151 M4. Nothing here is a decision.
> Provenance: claims about this repository were verified against the working
> tree on the compile date by reading the named files; claims about what an
> ADR decided are quotations from the ADR text; line counts come from `wc -l`;
> effort figures are estimates and are marked as such. Nothing here was
> measured at runtime.
>
> §5's findings were acted on the same day. The record was reconciled by
> dated Updates on [ADR-0105](../adr/0105-keelson-adopts-generated-record-stores.md)
> (the deviation, why it stands today, and the one-adapter exit),
> [ADR-0148](../adr/0148-app-workingsets.md) (the invariant binds the
> modelled fact substrate, not one table name) and
> [ADR-0151](../adr/0151-table-column-width-overrides.md) (the column-width
> kind travels with the deviation; M4 blocked). Those ADRs are
> authoritative; this page is the reasoning behind them.

# Would a record store beat `Storage()` as the app-facing state API?

## 1. The question, and why it came up

`app.StorageI` — `Get` / `Set` / `Delete` over `[]byte`, keyed `(app alias,
single subject token)` — is the only sanctioned way a keelson app keeps state.
The question is whether a generated record store
([ADR-0100](../adr/0100-recordstore-generated-leeway-clickhouse-store.md))
over `boxer.facts` would give apps a better surface.

It came up because ADR-0151 M4 could not be written. The ADR's column-width
overrides moved from `runtime.persist` to a `boxer.facts` fact kind, and at
that point play had no way to reach them: `MountContextI` exposes `Storage()`
and `Bus()`, and no app in the tree touches `factsstore`. So the concrete
sub-question is whether the answer to "how does an app reach typed durable
state" is a record store.

**The short answer is that most of this was decided a month ago and not
built.** §2 is that finding. §3 compares the surfaces on the merits anyway,
because the decision's gating means the comparison still has to be made. §4 is
the part that is genuinely open. §5 records two places where recent work
contradicted the existing decision.

## 2. ADR-0105 already decided this, and it is unbuilt

[ADR-0105](../adr/0105-keelson-adopts-generated-record-stores.md) —
*keelson adopts generated record stores for durable facts* — is accepted
(2026-07-05, reconciled in place 2026-07-11) and its Status says
"Implementation not started".

Its **D3a** is exactly the durable persist backend:

> **D3a — persist backend on a dedicated store-owned table** (unblocked now;
> needs no generator changes). The durable `StorageBackendI` backend binds its
> own generated table (working name `runtime.persiststate`): Key = string
> `"<appId>/<key>"` […] Order = the z64 timestamp lane, and a u8 lifecycle
> column, so the full state view emits — `Get`/`Set`/`Delete` map to the
> generated `GetFetch` / `Begin`+`Commit` / `Delete`-tombstone […]. Persist
> state thereby leaves the `boxer.facts` substrate; the `FactsStoreI` state
> verbs (`WriteState`/`DeleteState`/`LatestState`) stay on the legacy
> `chstore` facade until its callers migrate.

Three consequences matter here.

**A record store cannot carry state against `boxer.facts` at all.** The
2026-07-11 reconciliation records the reason as a measured force: there is
"no u8 `EntityLifecycle` in the facts schema, so no state view can emit
against `boxer.facts`". That is why D3a puts persist state on a *dedicated*
table rather than the facts table. The Alternatives section kills both
escapes explicitly — adding a lifecycle column to `boxer.facts` ("a live-table
migration plus a retrofit of every existing facts writer and reader"), and
keeping state facts-bound with membership tombstones (quoted in §5).

**So the question as posed — a record store *over `boxer.facts`* — is
answered no by an accepted ADR**, and the thing that was accepted instead is a
record store over its own table. The facts-bound store that ADR-0105 does
want (**D3b**, grants and audit — append-shaped, no state view) is gated on
two generator features that do not exist: a membership-id override on
`gen.Input`, and id-level disjointness under that override.

**None of the enabling pieces are built.** Verified on the working tree:

| ADR-0105 piece | Working name | Present? |
| --- | --- | --- |
| D1 executor adapter over `chclient` | `public/keelson/data/storeexec` | no |
| D2 store-gen wrapper for the facts schema | `runtime/factsschema/storegen` | no |
| D3a persist backend on a dedicated table | `runtime.persiststate` | no |

`recordstore/chexec` still ships only `chexec_local.go` — the
clickhouse-local executor — so there is no transport a keelson service could
use today without writing D1 first.

## 3. The surfaces, compared

Taking the question on its merits, independent of §2's gating.

`StorageI` is three methods over opaque bytes. A generated store — read from
`public/storage/recordstore/example/device_store.out.go`, 1202 lines for a
four-component example — exposes typed per-component DTOs plus `Begin`
(entity builder) / `Ingest<Kind>` / `Latest` / `GetLive` / `Delete` /
`Scan<Kind>` / `Replay` / `Flush`, with a batching read-through cache and an
optional state view.

| Dimension | `StorageI` | Generated record store |
| --- | --- | --- |
| Payload | opaque `[]byte`; every caller invents a codec | typed component DTOs, leeway-modelled, columnar-queryable |
| Enumeration | none — the applet store hand-rolls an `index` key | `Scan<Kind>` with predicates and limits |
| History | last-writer-wins, no trail | append-only substrate; `Replay(key, fromOrder)` |
| Identity | `(alias, key)` only — ADR-0148's recorded "no instance or name dimension" | entity id + components + Order |
| Deletion | `Delete` drops the key | lifecycle tombstone, state view reads live |
| Caching | none | batching read-through, optional versioned write-through |
| Cost per adopter | zero — three methods | a generated store per schema, regenerated on schema change |

On data-centricity the comparison is not close: the record store is the
surface the [ADR-0148 invariant](../adr/0148-app-workingsets.md) asks for, and
`StorageI` is the one it demotes. Four limitations recorded against `StorageI`
across ADRs 0026, 0132 and 0148 — opacity, no enumeration, no instance
dimension, no history — are all constitutive of the record store's shape.

**But the comparison above is between an app-facing API and a host-facing
one, and that is the flaw in it.** Every advantage in the right column
assumes the caller holds a store instance. Two properties make that a poor
fit for an app:

- **`ExecutorI` is SQL text and Arrow batches** — `Exec(ctx, sql)`,
  `QueryArrow(ctx, sql)`, `InsertArrow(ctx, table, records)`. Any app holding
  an executor can read and write every other app's rows in the table.
  `StorageI`'s bus family is namespace-scoped by capability
  (`runtime.persist.{ownAlias}.>`) and the service enforces the alias it
  parses from the subject. Handing a store to an app would move state access
  outside the capability model, which is the property ADR-0026 exists to
  establish.
- **A store instance is single-goroutine and stateful** — it holds an
  executor, a cache and pending writes, and ADR-0105 D4 says the owning
  service must confine it. Per-window app instances would each need one, or
  need to share one behind a mutex.

The natural reading is that a record store is the right implementation
*behind* the persist service, and the wrong thing to hand an app — which is
what D3a says. `StorageI`'s poverty is then a separate question from the
substrate: the backend can become typed and queryable without the app-facing
API changing at all, and D3a's `Get`/`Set`/`Delete` mapping is exactly that.

## 4. What is actually open

If the substrate question is settled by D3a, what remains is the one that
blocked M4: **an app can hold opaque bytes but cannot hold a typed record.**
D3a does not change that — it makes the *storage* typed while the *app* still
sees `[]byte`.

Three shapes, none of them decided:

1. **Leave the app boundary opaque.** Apps keep `StorageI`; anything wanting
   typed rows (workingsets, column widths) is composed by the app and written
   by the host, as ADR-0148 already does via `WorkingsetComposerI`. Cheapest,
   and consistent. It does not fit ADR-0151, whose resolver reads a whole
   override set at Mount and writes debounced captures mid-session — a
   lifecycle-edge composer cannot express that.
2. **A typed subject family per kind**, mirroring `runtime.persist.*`: a
   host-side service owning the store, a per-app client, a `MountCtx`
   accessor. Keeps the capability model intact. Costs roughly what persist
   costs — `public/keelson/runtime/persist` is 1281 lines including tests,
   plus a codec package (estimate).
3. **A generic typed-state subject family** that several kinds share, so the
   next kind after column widths does not repeat (2). More design, and no
   second consumer has asked for it yet, which is the usual argument for
   waiting.

The ADR-0151-shaped need is small — read a set at Mount, write debounced
entries, delete on clear — and (2) at persist's scale for one widget feature
is a poor ratio. That asymmetry is the thing worth deciding, and it is not
addressed by any current ADR.

## 5. Two contradictions in recent work

Recorded because they bear directly on the question, and because the
background page is the right place to write down that the tree and an
accepted ADR disagree.

**`persist.FactsBackend` (2026-07-30, `66ac54c9`) is the alternative ADR-0105
rejected.** It routes `StorageBackendI` onto
`FactsStoreI.WriteState`/`LatestState`/`DeleteState`, keeping persist state
facts-bound and tombstoned by `MembPersistTombstone`. ADR-0105's Alternatives
says:

> **Keeping persist state facts-bound, tombstoned by membership.** Rejected:
> without the state view the live read stays hand-written leeway-encoded SQL
> (`composeLatestStateSql` and its cumulative-sum membership lookups) — the
> code class this ADR exists to delete — and the persist milestone keeps
> roughly half its hand-rolled surface.

`composeLatestStateSql` is precisely the read path `FactsBackend.Get` uses.
The commit landed against an accepted decision that was never consulted; its
message asserts "only the adapter was ever missing", which was wrong — a
different adapter had been specified and left unbuilt. What the commit did
deliver is real (persist is durable, and the applet store's evaporation is
fixed, verified live), so this is a question of which substrate it should sit
on, not whether it works.

**The ADR-0151 column-width fact kind (2026-07-31, `7f5e5852`) added ~250
more lines of that same code class** — `composeListColumnWidthsSql`, nested
`argMax` with `HAVING`, `pickLcrString` / `pickLcrNumeric` cumulative-sum
lookups — for a kind that is state-shaped and therefore, by the 2026-07-11
force, cannot ever get a state view while it lives on `boxer.facts`.

**And the ADR-0148 data-centricity Update (2026-07-30) conflicts with D3a.**
That Update says runtime and app state "is stored in the runtime facts table
(`boxer.facts`) and modelled as facts there". D3a says persist state "thereby
leaves the `boxer.facts` substrate". Both are accepted. The conflict is
narrow — it is about which *table*, not about whether state is modelled — but
it is real, and one of the two has to give.

## 6. Summary

- A record store is a decisively better surface than `StorageI` on typing,
  enumeration, history and identity — but as a *backend*, not as something an
  app holds; `ExecutorI`'s SQL surface puts it outside the capability model.
- A record store **over `boxer.facts`** is ruled out for state by an accepted
  ADR and a schema fact: no u8 lifecycle column, so no state view can emit.
  ADR-0105 D3a puts persist state on a dedicated generated table instead.
- That decision is unbuilt, and all three of its enabling pieces (D1 executor
  adapter, D2 store-gen, D3a itself) are absent from the tree.
- The genuinely open question is not the substrate but the **app boundary**:
  nothing lets an app hold a typed record, and ADR-0151's mid-session
  read/write pattern does not fit the host-composes shape ADR-0148 uses.
- Two 2026-07-30/31 commits contradict ADR-0105, and the ADR-0148 invariant
  conflicts with D3a on which table state lives in.
