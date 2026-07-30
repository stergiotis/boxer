---
type: adr
status: accepted
date: 2026-07-30
reviewed-by: "p@stergiotis"
reviewed-date: 2026-07-30
---

# ADR-0151: Table column-width overrides — semantic fingerprints over runtime.persist

## Context

Both table surfaces delegate column widths to their underlying crates:
`c.Table` / `c.NewTable` to `egui_extras::TableBuilder` (0.33.3), `c.EndETable`
to `egui_table` (0.9.0). A survey of width-determination practice (academic
optimal-layout work, CSS auto layout, Dear ImGui, egui crates, virtualized
data grids) concluded that the highest-signal width source is the one every
production grid privileges: the user's own adjustment, persisted. Automatic
estimation matters only for first contact and untouched columns.

Today user adjustments are lost three ways:

- **Not durable.** Both crates keep dragged widths in egui memory
  (`TableState`); the host never serializes egui memory, so every adjustment
  dies at app exit.
- **Invisible to Go.** No binding channel reports final column widths back;
  the only `c.EndETable` fetches are the visible-range prefetch
  (`GetEtPrefetch`). Go cannot observe what the user chose.
- **Structurally keyed.** `TableState` is keyed by egui Id (UI-tree position
  plus salt). Moving a table to another pane forgets its widths; two views of
  the same data share nothing. The binding already carries scars from this
  keying — the `push_id` state-clobbering fix and the sizing-pass-poisoning
  bail-out in the interpreter's table paths.

Two further facts shape the design. First, for `c.EndETable`, the Go-supplied
`currentWidth` is only a first-show seed: `egui_table` overwrites it from
stored `TableState.col_widths` on every later frame (table.rs:388 in 0.9.0),
and force-autofits all columns on first show. Go currently has no
post-first-frame width control. Second, the visible-range culling fast path
(rows gated on `VisibleRange()`) is only correct when widths do not depend on
off-screen content — so Go-side width authority is not a preference but the
regime the culling contract already assumes.

On the storage side, the runtime already has a sanctioned per-app state
channel: `runtime.persist.{alias}.{key}.{op}`
([ADR-0026](./0026-app-runtime-and-capability-subjects.md) §SD3), surfaced to
apps as `MountCtx.Storage()` (`app.StorageI`), keys declared in
`Manifest.PersistedKeys`. Keys are single NATS tokens (no dots); values are
opaque `[]byte`. Two of its limits still bear on this design: values are
opaque, so any structure is the caller's to encode, and there is no listing
operation, so a caller that needs enumeration maintains its own index. Its
durability limit — the wired backend was in-memory — no longer applies; the
facts-backed backend that [ADR-0148](./0148-app-workingsets.md) recorded as
a follow-up has since been built and wired (ADR-0026, Update 2026-07-30), so
persisted documents survive a restart whenever ClickHouse is up. That is
what makes `runtime.persist` a candidate for width overrides at all.

## Design space (QOC)

**Q1 — What identifies "the same column" across sessions and views?**

- *egui Id.* What the crates do today. Killed: structural, breaks on UI
  refactors, cannot share across panes or queries.
- *Instance path only (app + table tag + column index).* Survives restarts
  but not column reorder/add/remove, and shares nothing across tables.
- *Semantic column identity (name + type), in a tier hierarchy.* Chosen — see
  SD1. Keying by what the column *is* rather than where it sits survives
  reordering, and lets recurring columns (e.g. the same field appearing in
  many ad-hoc query results) keep their width everywhere.

**Q2 — Where do overrides live?**

- *Serialize egui memory.* Killed: id-keyed (inherits Q1's problems), opaque
  Rust-side state, would persist far more than widths, no app scoping.
- *Local file per app.* Killed: bypasses the capability model; per-app state
  has a sanctioned channel and file access is mediated by fsbroker for user
  documents, not app state.
- *A dedicated fact kind à la workingsets (ADR-0148 §SD6).* Killed for now:
  workingsets needed instance/name dimensions, provenance vocabulary, and
  queryability; width overrides are small opaque UI preferences. A new kind
  means vocab + DDL + codec surface for no query anyone has asked. The
  persist channel already lands in `boxer.facts` as `KindState` rows once the
  facts backend exists, so the data is not lost to introspection.
- *`runtime.persist` KV, one key per table or per column.* Killed: keys are
  subject tokens (dots are separators), so structured keys need escaping; and
  each key is a bus round-trip.
- *`runtime.persist` KV, one versioned document per app.* Chosen — see SD2.

**Q3 — Durability of the persist backend.** *Settled outside this ADR — see
SD3.* The question was whether to accept a session-scoped memory backend
(hollow: the whole point is surviving restarts) or build the thin
facts-backed `StorageBackendI`. The latter was built and wired
independently, so column widths inherit the platform's durability posture —
ClickHouse when reachable, memory otherwise, the same as grants and audit
rows — and this ADR carries no durability decision of its own.

**Q4 — How does Go apply an override to `c.EndETable` without fighting the
user's drag?**

- *Re-assert widths every frame.* Killed: this is precisely the pathology
  `TableState` inflicts on Go today, with the roles reversed — the drag
  becomes impossible.
- *Clear `TableState` whenever overrides change.* Blunt: drops scroll
  position and every column's drag, not just the changed one.
- *Authoritative-apply epoch.* Chosen — see SD4. Go bumps an epoch when its
  resolved widths change; the binding writes Go's widths into `TableState`
  exactly once per bump. Drags win at all other times.

**Q5 — When is a user adjustment captured?**

- *Explicit "pin width" gesture.* More deliberate, but no grid works this
  way and it demands UI surface before proving value.
- *Silent capture on drag-end, debounced.* Chosen — see SD5. Detection: a
  fetched width differs from the width Go last applied for that column,
  while the table is not in its first-show frame.

## Decision

### SD1 — Override model: three tiers keyed by semantic column identity

A *column key* is `blake3short(name, typeDiscriminator)` where `name` is the
rendered header/field name and `typeDiscriminator` a short tag for the
render type (leeway canonical type where available, else an app-chosen
format tag). A type change deliberately invalidates the override.

Overrides resolve most-specific-first through three tiers:

1. **Instance** — `(tableTag, columnKey)`. `tableTag` is the stable string
   the call site already passes as the table's id (e.g. the
   `ids.PrepareStr` argument). Scoped to one table in one app.
2. **Shape** — `(shapeHash, columnKey)` where `shapeHash` is
   `blake3short` of the sorted column-key set. "The same logical table",
   wherever it appears.
3. **Column** — `(columnKey)` alone. "This column, anywhere in this app."
   This tier is what lets a recurring column keep its width across
   differently-shaped ad-hoc query results.

A drag-end capture writes the instance tier and the column tier (the shape
tier is derived context, written by neither; it exists so a *read* can match
a table that has an instance-tier history under a different tag — a
deliberate small cut: shape-tier writes can be added later if reading proves
useful). All tiers are per app: persist keys are namespaced by app alias and
the capability model (`runtime.persist.{ownAlias}.>`) forbids cross-app
reads. Cross-app sharing is consciously out of scope.

Widths are stored as `(points, fontSizeAtCapture)`. On resolve, a mismatch
between stored and current font size rescales proportionally — cheap
robustness against theme/zoom changes; exact re-fit stays a user action.

### SD2 — Storage: one versioned document under `runtime.persist`

One persist key per app (`colw`), declared in `Manifest.PersistedKeys`.
The value is a small versioned JSON document holding all tiers' entries.
Loaded once when storage capabilities arrive; saved debounced (~1 s after
the last capture) and on Unmount. Entries carry a last-used timestamp and
the document is capped (LRU eviction, 512 entries) so it cannot grow
unboundedly.

Persist keys are per app alias, so two windows of one app share the
document — which is the intended semantics for width preferences (the same
column should look the same in every window), not a limitation to engineer
around: the instance/name dimension ADR-0148 records as missing from the
persist family, and added for workingsets via its own fact kind, is
deliberately not wanted here. The residual risk is document-level
last-writer-wins dropping a concurrent window's fresh entries. Each save
therefore re-reads the stored document and merges entries by last-used
timestamp before writing — entry-level last-writer-wins, absorbing most of
the cross-window race without any `StorageI` API change. Truly concurrent
captures of the *same* entry still race; accepted.

### SD3 — Durability: satisfied by the facts-backed persist backend (landed)

This ADR needs `colw` documents to survive a restart, and originally
proposed building the backend that would make that true. It has since been
built independently — `persist.FactsBackend`, wired by the carousel
whenever `chstore.NewWithFallback` reached ClickHouse, labelled in
`runtimestatus` as "facts" / "mem" (see
[ADR-0026](./0026-app-runtime-and-capability-subjects.md), Update
2026-07-30). So SD3 is no longer a decision this ADR makes; it is a
precondition this ADR now meets, and the M2 milestone is discharged.

One detail of the landed backend differs from what was sketched here and
is worth keeping, since it was a live design question: the durable app id
is threaded to the backend from the bus envelope (`StorageRef.AppId`)
rather than resolved from the subject alias through the app registry. A
registry lookup would have missed synthetic service identities such as the
applet store's `runtime.appletstore`, which is never registered as an app.

What remains true for this ADR: with ClickHouse down, `colw` documents last
exactly as long as the process, which for a UI preference is an acceptable
degradation and matches every other persist user.

### SD4 — Wire additions (`c.EndETable` first)

Two additions to the etable surface, IDL-generated per the codegen flow:

- **Width read-back.** A sibling of the `r9_et_prefetch` register family:
  per table id, the per-column current widths after `egui_table` has
  reconciled state and drags. Drained by `StateManager.Sync` into the
  existing per-table cache next to `EtPrefetchValue`; exposed as
  `EndETableFluid.ColumnWidths()`. Same one-frame lag and `ok=false`
  first-frame semantics as `VisibleRange()`.
- **Authoritative apply.** An `applyWidths(epoch u32)` method on
  `endETable`. The binding keeps the last-seen epoch in its per-table
  state; when the epoch differs, it writes the accumulated `etColumn`
  widths into `egui_table`'s `TableState.col_widths` before `Table::show`,
  and suppresses the first-show force-autofit for that table. Epoch
  unchanged → `TableState` (drags) wins, exactly as today.

`c.Table` / `c.NewTable` get the cheaper cut: overrides apply as
`initial()` widths plus a state `reset()` call when the resolved widths
change (losing concurrent drags on that table in that frame — accepted for
the first iteration), and no read-back initially. If the etable capture
loop proves the model, a matching egui_extras read-back can follow.

### SD5 — The Go-side resolver

A small package (working name `egui2/colwidth`) owning: the override
document codec, tier resolution, capture detection (fetched ≠ applied while
not first-show, debounced), the epoch counter per table, and eviction. Width
resolution order at a call site becomes: override → app-supplied default
(constant or estimator such as play's sample-based `attrColWidths`) → crate
autofit for tables that opt out entirely. Estimation improvements
(probe-calibrated advances, p95 sampling) remain available as
default-providers but are out of this ADR's scope.

Capture of a double-click autofit is deliberate: the fitted width is
recorded as an override ("fit it, keep it"). A "clear override" affordance
(context action on the header) deletes the instance- and column-tier
entries for that column and returns it to defaults.

## Alternatives

Covered per-question in the design space above; the standing rejections are
egui-memory serialization (structural keys, opaque scope), per-app files
(bypasses the capability model), a dedicated fact kind (schema surface
without a query need), and per-frame width re-assertion (fights the user).

Two `StorageI` critiques recorded in earlier ADRs were weighed and declined
rather than silently inherited. A leeway-declared, kindcheck-validated
payload (ADR-0148's answer to the persist family's "opaque and
conventional" values) buys validation and introspection for a payload
nobody queries, at the cost of forcing a tiered map into columnar shape —
the versioned JSON document stays. The missing enumeration op (worked
around with a manual `index` key by the applet store, ADR-0132) is
irrelevant to a single-document layout with no keyspace to enumerate —
which is part of why the single document was chosen over per-table keys.

## Consequences

### Positive

- User width adjustments survive restarts, pane moves, and — via the column
  tier — recur across differently-shaped query results.
- Go becomes the width authority for etables, which the visible-range
  culling contract already assumed; crate-side autofit stops being a silent
  co-author.
- The persist facts backend landed as a platform improvement independent of
  tables; existing persist adopters gained durability with no code change.
- The capture channel (read-back) also gives Go eyes on actual widths for
  diagnostics, independent of overrides.

### Negative

- Two new wire surfaces (register family + method) and a reconciliation
  state machine (default → override → drag → capture → re-apply) that must
  be right about echo suppression: a captured drag must not immediately
  re-apply as an "override change" epoch bump. The resolver owns this and
  needs focused tests.
- A versioned JSON document is a soft schema; growth is bounded by LRU but
  garbage accumulates until eviction. The merge-on-save rule narrows the
  cross-window race to same-entry concurrent captures without eliminating
  it.
- `c.Table`'s reset-based apply drops in-flight drags on that table when an
  override changes; acceptable only because overrides change on user action.

### Neutral

- Cross-app width sharing is impossible under per-alias persist caps; a
  shared tier would need its own subject and is not planned.
- The width-estimation survey stands as reference for default-providers;
  nothing here forecloses upgrading estimators later.
- Apps that never touch the resolver keep exactly today's behavior.

## Milestones

- **M1** — `colwidth` resolver package: document codec, tiers, epoch,
  capture detection, eviction; pure Go, table-driven tests.
- **M2** — ~~`persist.FactsBackend` + carousel wiring flip +
  `runtimestatus` surfacing.~~ **Done (2026-07-30), outside this ADR.** It
  was independent of M1/M3 and also repaired the applet store's silent
  non-durability, so it landed on its own (§SD3).
- **M3** — etable wire: width read-back register + `applyWidths` epoch +
  first-show autofit suppression; IDL change + regen + interpreter apply
  code in the marked region.
- **M4** — play adoption on the attr-results table: resolve → `EtColumn`,
  capture → store; `Manifest.PersistedKeys` gains `colw`.
- **M5** — `c.Table` / `c.NewTable` reset-based apply for override
  consumers.
- **M6** — affordances: header context "clear override"; docs
  (`doc/howto` note on width behavior).

## Status

Accepted 2026-07-30. M2 was discharged outside this ADR before acceptance
(§SD3); implementation of M1/M3–M6 not started.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers. Subsequent refinements land as dated `## Update`
sections.

## References

- [ADR-0026 — App runtime and capability subjects](./0026-app-runtime-and-capability-subjects.md) (§SD3 persist subjects, §SD6 facts)
- [ADR-0148 — App workingsets](./0148-app-workingsets.md) (persist limits, facts-backend follow-up, last-writer-wins)
- [ADR-0132 — SQL applets](./0132-sqlapplet-sql-defined-applets.md) (O4 store over `StorageI`; the index-key enumeration workaround)
- `egui_table` 0.9.0 `table.rs` (TableState precedence, first-show autofit), `columns.rs` (`Column::auto_size`)
- `egui_extras` 0.33.3 `table.rs` (sizing pass, grow-only ratchet, 8 px/frame shrink)
- Gange, Marriott, Stuckey — [Optimal Automatic Table Layout](https://people.eng.unimelb.edu.au/pstuckey/papers/complete-table-2011.pdf) (the optimal-layout benchmark; contextual)
- Dear ImGui [`imgui_tables.cpp`](https://github.com/ocornut/imgui/blob/master/imgui_tables.cpp) (content-seeded stretch weights; contextual)
