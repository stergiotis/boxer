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

- **M1 — `colwidth` resolver package.** Document codec, tiers, epoch,
  capture detection, eviction; pure Go, table-driven tests.
- **M2 — `persist.FactsBackend` + carousel wiring flip.** ✓ Plus
  `runtimestatus` surfacing. **Done (2026-07-30), outside this ADR.** It
  was independent of M1/M3 and also repaired the applet store's silent
  non-durability, so it landed on its own (§SD3).
- **M3 — etable wire.** Width read-back register + `applyWidths` epoch +
  first-show autofit suppression; IDL change + regen + interpreter apply
  code in the marked region.
- **M4 — play adoption on the attr-results table.** Resolve → `EtColumn`,
  capture → store; `Manifest.PersistedKeys` gains `colw`.
- **M5 — `c.Table` / `c.NewTable` reset-based apply** for override
  consumers.
- **M6 — Affordances.** Header context "clear override"; docs
  (`doc/howto` note on width behavior).
- **M7 — play adoption on the per-DB-row grid.** ✓ The second adopter, plus
  the per-view type discriminator the column tier needs once two grids
  render the same column (Update 2026-08-16).

## Update — 2026-07-30: `colw` becomes modelled facts, not a persist document

**Q2 and §SD2 are superseded by the data-centricity invariant recorded in
[ADR-0148](./0148-app-workingsets.md) (Update 2026-07-30): state lives in
`boxer.facts` and is modelled there.** Q2 rejected a dedicated fact kind
because "a new kind means vocab + DDL + codec surface for no query anyone
has asked", and chose a versioned JSON document under `runtime.persist`.
That cost was real and is unchanged; what changed is that it is no longer
the deciding consideration. An opaque document reaches the table and stays
invisible to it, which is the shape §SD6 of
[ADR-0026](./0026-app-runtime-and-capability-subjects.md) rejected. The
accompanying *Alternatives* paragraph — declining ADR-0148's typed-payload
critique because the payload is one "nobody queries" — is withdrawn for
the same reason.

This ADR is the natural first case rather than an unlucky one: it is
accepted but unimplemented, so nothing has to be migrated, only written
differently.

**The record shape follows `WorkingsetRow`.** One row per override entry —
tier, the tier's key (`tableTag`+`columnKey`, `shapeHash`+`columnKey`, or
`columnKey` alone), width in points, font size at capture, app id — with
latest-wins per key and a tombstone for "clear override". §SD1's three
tiers, §SD4's wire, and §SD5's capture detection are unaffected; they were
never storage-dependent.

Two consequences are improvements rather than costs, and are worth naming
because they retire text above:

- **The cross-window race in §SD2 disappears.** Per-entry rows make
  last-writer-wins land at entry granularity by construction, so the
  read-merge-write rule §SD2 introduced — and the same-entry race it
  admitted it could not close — are both moot.
- **The LRU cap stops being a document rewrite.** Bounding growth becomes
  a retention question over rows, the same one every other fact kind has,
  rather than an eviction pass that rewrites the whole document on save.

**Milestones change shape.** M1's resolver keeps tiers, epoch, capture
detection and fingerprinting, and loses the document codec and the
merge-on-save rule. A new milestone precedes it: the fact kind — vocabulary
terms, the row type, and both `FactsStoreI` backends, mirroring what
ADR-0148 §SD6 did for workingsets. M4 no longer adds `colw` to
`Manifest.PersistedKeys`, since no persist key is involved.

**One trap is already known.** The chstore read is a latest-per-key
collapse, the same shape `ListWorkingsets` needed. Its tombstone predicate
must be `HAVING argMax(is_tomb, sk) = 0`; a `WHERE NOT is_tomb` filter
combined with `argMax` returns the newest surviving non-tombstone row and
silently resurrects a cleared override.

**What is unchanged.** §SD3 still holds — durability comes from the facts
store and degrades to process lifetime when ClickHouse is down. The persist
facts backend that discharged M2 keeps its value for every other adopter;
it is simply no longer this ADR's storage route.

## Update — 2026-07-31: the fact kind and M1 are implemented

Both landed as described, with three details worth recording because
reading the decisions above would not predict them.

**Tier is its own stored column, not an inference.** Whether an entry is
instance-, shape-, or column-tier is recoverable from whether its scope is
empty, and storing it anyway is one redundant low-cardinality column. The
redundancy is the point: a reader that reconstructs one field from the
shape of another is correct only until a fourth tier exists.

**Per-entry rows removed more than the merge rule.** §SD2's
read-merge-write and the same-entry race it explicitly could not close are
both simply absent — last-writer-wins now lands at entry granularity by
construction rather than by a rule the save path has to run. The resolver
has no merge step at all.

**Eviction changed meaning.** §SD5 lists it as a resolver responsibility,
which it was when the store was one document that had to be rewritten
whole. It now bounds only the in-memory working set: it never deletes a
row and never touches an unflushed capture, so an evicted override returns
on the next load. Bounding the durable trail is a retention question over
the facts table, alongside every other fact kind's.

Two facts for whoever picks up M3. The f64 value column is
`` `tv:f64Array:value:val:f64h:4A:::0::data` `` — the encoding segment is
`gM`, not the `g` the other array sections use, so a name extrapolated from
the u64 block compiles and matches nothing. And "latest" means insertion
order in `InMemoryFactsStore` and `(ts, id)` in `chstore`, as it already
does for state and workingsets: the backends agree for any caller that lets
`Ts` default to now, and diverge only for one that dates a write forward or
back.

The remaining milestones are unchanged. M3's binding work is where the
apply/capture loop meets a real crate, and the echo-suppression rule the
resolver implements — a captured drag updates the applied value without
bumping the epoch — is the part to hold on to.

## Update — 2026-07-31: the fact kind inherits a recorded deviation, and M4 is blocked

Two things surfaced while attempting M4, neither of them visible from this
ADR alone.

**The fact kind is state-shaped on a substrate that cannot give it a state
view.** [ADR-0105](./0105-keelson-adopts-generated-record-stores.md)'s
2026-07-11 reconciliation measured that `boxer.facts` has no u8 lifecycle
column, so no generated state view can ever emit against it — which is why
that ADR moved persist state to its own table. A column-width override is
state: latest-wins per key, cleared by tombstone. Implementing it on
`boxer.facts` therefore committed it to hand-written leeway-encoded SQL —
`composeListColumnWidthsSql`, nested `argMax` with `HAVING`, cumulative-sum
membership lookups — which is the code class ADR-0105 exists to delete. That
ADR's Update of this date records the deviation, why the shipped code is not
being reverted today, and the exit; this kind travels with it and moves to a
generated store when D3a lands. Nothing about §SD1's tiers, §SD4's wire or
M1's resolver depends on which table the rows sit in.

**M4 is blocked on a seam that does not exist.** The milestone reads
"play adoption on the attr-results table", and when the storage was
`runtime.persist` it needed nothing new: an app reaches persist through
`MountCtx.Storage()`. Moving to a fact kind removed that access path without
replacing it — `MountContextI` exposes `Storage()` and `Bus()`, and no app in
the tree holds a `FactsStoreI`. Handing one over is not the answer either:
`recordstore.ExecutorI` is SQL text plus Arrow batches, so an app holding a
store could read and write every other app's rows, outside the capability
model ADR-0026 establishes.

ADR-0148's host-composes shape does not fit this case. `WorkingsetComposerI`
is called once at the closing edge; this resolver reads the whole override
set at Mount and writes debounced captures mid-session. So the open question
is a typed app-state seam — a subject family mirroring `runtime.persist.*`,
or something more general — and it is larger than this ADR. M4 waits on it;
M5 and M6 are unaffected and can proceed.

The options and their costs are laid out in
[persist-api-surface-recordstore](../adr-background-work/persist-api-surface-recordstore.md).

## Update — 2026-07-31: M5 and M6, and what M6 could not build

**M5 landed as specified.** `c.Table` and `c.NewTable` take
`ApplyWidths(epoch)`; because `egui_extras` keeps its widths in a private
`TableState` with no seam to seed them, the apply is `TableBuilder::reset`
and the widths ride the `Initial()` those columns already had. Two crate
behaviours the design rests on are pinned by tests against the real crate —
that stored state beats a later `initial()` (without which the reset would
be unnecessary), and that a reset before the body restores it.

**M6 landed in two steps.** §SD5 asks for the clear action on a header
*context menu*, and egui2 had no context-menu binding — so the gesture
first shipped as a documented recipe, and the binding followed the same
day (`c.ContextMenu()`, see the Update below). What M6 contains:

- `Resolver.ClearAll(tableTag, cols)` beside the existing per-column
  `Clear`, so "reset this table" is one call. It deliberately does not stop
  at the first failure: a partial clear is the worst outcome for this
  gesture, leaving some columns reset and others not with nothing to
  distinguish them.
- [doc/howto/table-column-widths.md](../howto/table-column-widths.md),
  covering the tier model, the call-site shape, and the four behaviours that
  surprise: the one-frame lag, grow-to-fit being captured like a drag, the
  epoch meaning "widths changed" rather than "frame happened", and the
  egui_extras surfaces losing an in-flight drag on an epoch change.
- The clear gesture, first as a recipe and then on the real binding.

## Update — 2026-07-31: the context-menu binding, and why it is not `Response::context_menu`

`c.ContextMenu()` closes M6's affordance gap: a `contextMenu` block that
wraps a target body and shows a menu body on secondary click, built on the
`hoverUi` two-deferred-block shape.

It deliberately does not use egui's own `Response::context_menu`. That
helper reads `secondary_clicked()` off the response it is handed, so the
response has to sense clicks — and a click-sensing widget covering the
target's whole rect wins the pointer over every interactive widget drawn
inside it. On a sortable header the menu would eat the sort click. That
failure is already recorded in this tree for `Frame.SenseClick`.

So the overlay senses hover only and the secondary click is read from the
pointer instead, with the anchor's `hovered()` as the gate. Nothing inside
the target loses a click. The overlay is still registered *after* the body,
for the reason `hoverText` documents: egui marks a non-interactive widget
hovered only when it sits above the topmost interactive one, and an overlay
created before the body would report `hovered=false` exactly where a
right-click most often lands — over an inner button.

Open/close mirrors `Popup::context_menu`: secondary click opens, primary
click on the target closes, clicks inside the popup are left to egui's own
close behaviour.

This is a general binding, not a table feature; the width how-to uses it,
and any widget that wants a context menu now can.

## Update — 2026-07-31: M4 lands on ADR-0155's capability

The seam M4 waited for arrived as
[ADR-0155](./0155-app-embed-seam.md) §SD1: an optional capability
type-asserted off the context, exactly as `WindowFocusI` is, leaving the
four-method app contract frozen. `colwidth.HostI` is that capability for
column widths; windowhost implements it on a wrapper embedding the static
frame context, and only when it has a facts store — absence of the
capability is how an app learns there is nowhere durable to put widths.

It is declared in `colwidth` rather than in `app` because its store speaks
in facts rows and `factsstore` imports `app`; declaring it there would
close that loop.

play resolves the attr grid through it, with the existing sampled
estimator demoted to the default for untouched columns. Two call-site
details were not obvious from §SD4 and are worth carrying to the next
adopter:

- **The fixed leading column must still appear in the resolver's column
  list.** The width read-back is positional, so omitting it shifts every
  later column onto the wrong identity. Being unresizable it is never
  captured — the binding reports the supplied width straight back.
- **The first report a table makes is its force-autofit frame.** Those
  widths are the crate's, not the user's; passing `firstShow` there is
  what stops the estimator's first result being frozen as an override
  nobody chose.

Column identity is the raw Arrow field name and type rather than the
rendered label, so an override does not change identity if the label
builder does. Font size is passed as 0: play has no single text size to
attribute a width to, and zero disables rescaling — the documented
meaning, not a gap.

ADR-0155 §SD3 verified its identity decision against this ADR
pre-acceptance; M4 is the other half of that, and uses the mount context's
`AppId` exactly as SD3 requires.

## Update — 2026-08-01: the first-show gate suppressed one frame, not the first show

The Update above calls `firstShow` "what stops the estimator's first result
being frozen as an override nobody chose". It did not. `Observe` returned
before recording anything, so the width it declined to capture was still
unlike the applied one on the next frame — and the read-back lags a frame,
so there was always a next frame — and it was captured then. Opening play's
attr grid on a five-column result and touching nothing wrote ten rows: every
column, on both the instance and column tiers.

The column tier is what makes this more than cosmetic. Keyed by name and
type alone, a width measured once from one query's visible rows then applies
to any later result carrying a column of that name and type, and survives
restarts. The automatic estimator §SD5 puts underneath the overrides stops
being reachable at all.

The gate now adopts the reported widths as the comparison baseline instead
of returning. Two things about that are worth recording, because §SD4 would
not predict them:

- **The per-table record had to split in two.** It held one width per
  column, serving both as "what Resolve last handed the binding" — which
  drives the epoch — and as "what a fetched width is compared against" —
  which drives capture. Adopting the crate's width into that single value
  bumps the epoch on the next Resolve, so the binding re-seeds the crate
  against its own layout on every frame, which is the per-frame
  re-assertion Q4 rejects. The two are now separate fields and only the
  first moves the epoch.
- **A capture still writes both**, which is why the split does not disturb
  §SD5's echo-suppression rule: a captured drag updates the sent width
  without bumping the epoch, exactly as before.

**A neighbouring defect is left open.** The read-back's one-frame lag means
that on the first frame after a result's column set changes, the previous
shape's widths are matched positionally against the new columns, so a column
that moved position can have another column's width captured for it. play's
side of the same seam is `attrWidthsSeen`, one bool for the app's lifetime,
which never re-arms for a new shape. Closing it needs the shape to travel
with the read-back, or the call site to declare it — a wire or API question
rather than a resolver one.

## Update — 2026-08-02: the positional mis-attribution, and where it was closed

The Update above left this open and guessed wrong about what closing it
would take: "needs the shape to travel with the read-back, or the call site
to declare it — a wire or API question rather than a resolver one." The
resolver is handed the column list on every `Resolve`, so it can see the
shape change for itself. No wire or API change was needed.

Measured before fixing, on play's attr grid: switching to the per-attribute
view and toggling the support columns once wrote **290 override rows** with
nobody dragging anything. Each column-set change is followed by one report
describing the columns as they were, and lining that up by slot hands every
column its predecessor's width — which the column tier then applies to any
later result carrying that column.

Three parts, and the middle one was a bug in its own right:

- **A report whose length differs from the column list is dropped.** The
  read-back is positional, so a differing length is proof it belongs to
  another shape, or arrived truncated. The previous code lined the overlap
  up under a `min`.
- **A reordering now bumps the epoch.** Change detection compared a map of
  column keys, so the same set in a different order looked identical — yet
  everything on this wire goes by slot, including the seed. A reorder was
  therefore leaving the crate showing the previous order's widths, quite
  apart from what it did to capture detection.
- **Every re-seed opens a two-report settle window**, during which reports
  set the baseline instead of being captured. Two because they are
  different reports: the read-back lags a frame, so the first was produced
  before the seed landed, and the second is the seeded frame's own result.

The cost is a drag that begins and ends inside those two frames after a
re-seed — about 33ms — which is not captured. A drag emits a width every
frame it moves, so anything a hand can do outruns the window.

`firstShow` keeps its meaning but is no longer load-bearing: the first
`Resolve` for a table changes its order from nothing to something, which
opens the same window. play's `attrWidthsSeen`, which the Update above
criticised for never re-arming, is belt-and-braces for the same reason.

## Update — 2026-08-16: the second grid, and the tier collision it exposed

M4 adopted the resolver on the per-attribute grid only. play's *default*
Table view is the per-DB-row grid, which never joined — so for the view
almost everyone uses, widths were preserved neither across queries, nor
across opening the app again, nor across restarts. Reported from use, and
all three symptoms are the same omission seen from three distances.

**The across-queries symptom had a second cause worth recording**, because
it would have survived adoption on its own. `tableColsChanged` gates the
one-frame re-fit on `schema != inst.tableFitSchema` — a *pointer* compare,
which a Run always fails since every result carries a fresh `*arrow.Schema`.
So a re-fit fired on every Run, even a repeat of the identical query, and
`AutoSizeThisFrame` measured the user's drag away. The `f3f48a2c` exemption
("only columns whose resolved width is the seed's are auto-sized") could not
help, because with no resolver every column's width *was* the seed. Adoption
supplies the overrides that make the exemption bite; the pointer compare is
left as it is, since re-fitting a column nobody has touched is what it is
for.

**The column tier needed a discriminator.** §SD1 keys that tier on
`(name, type)` alone and matches it across tables, which is what lets a
recurring column keep its width. Two grids over one result break that
assumption: the per-DB-row grid renders a List column packed as `[len=N]`,
the per-attribute grid explodes it to its inner scalar, so a width fitted to
one is wrong in the other — and the column tier would have carried it over.
The fix uses the escape §SD1 already provides, "an app-chosen format tag" in
place of a canonical type: each grid appends its own view tag
(`;view=row` / `;view=attr`) to the Arrow type. Appended rather than
substituted, so a genuine type change still invalidates the override, which
is the property §SD1 leans on. The two tags are spelled out as constants
rather than derived from the granularity enum, so renaming a view cannot
silently re-key every stored width.

**Adoption forced one resolver addition, `MarkReseed`.** The first live run
of the adopted grid wrote eight override rows on startup for a table nobody
had touched — the failure mode this ADR has now hit three times. The cause is
the same lag as before seen from a new angle: a call site that asks
egui_table to auto-size holds that intent for one frame, but the fit's result
arrives in the *next* report, by which time the flag it was passing to
`Observe` is false again and the fit reads as a gesture. `firstShow` could
not cover it either, since a re-fit happens mid-session.

So the call site now says so directly: `MarkReseed(tableTag)` opens the same
two-report settle window `Resolve` opens when it bumps an epoch. It is
deliberately the same field and the same constant rather than a parallel
counter — "the crate chose these widths, not the user" is one idea, and the
per-view flag both grids used to pass to `Observe` is gone. §SD5 gave the
resolver capture detection; this is the part of it a call site could not
express.

Two consequences are worth stating plainly. Tagging the attr grid too —
rather than leaving it bare and tagging only the new grid — **re-keys the
overrides that grid had already stored**; they are orphaned, not migrated.
That is the sanctioned outcome for a type change and the alternative was a
permanent asymmetry, but it is a one-time loss rather than nothing. And the
instance tier now has two scopes, `results` and `attr-results`, so a drag in
one grid no longer reaches the other at all — which is the intended
semantics here, since the two are different renderings rather than two views
of one layout.

## Status

Accepted 2026-07-30. The fact kind and M1–M6 are all implemented (Updates
2026-07-31), M4 last, on the capability ADR-0155 established. M2 was
discharged outside this ADR before acceptance (§SD3), and Q2/§SD2's
storage choice was superseded by the ADR-0148 data-centricity invariant
before any of it was built.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers. Subsequent refinements land as dated `## Update`
sections.
