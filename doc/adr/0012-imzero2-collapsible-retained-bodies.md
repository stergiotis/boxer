---
type: adr
status: proposed
date: 2026-04-25
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0012: ImZero2 — Retained Bodies for Collapsible Block Skipping

## Context

CollapsingHeader and Window in imzero2 flicker on the closed→open transition and feel laggy under deeper nesting. The trigger is structural, not a bug in any one widget: the Go-side gate that decides whether to emit a block's body opcodes reads a one-frame-stale signal from Rust, so when egui detects a click that opens a collapsible, Go has already chosen *not* to emit body content for that frame.

### Today's mechanism

The codegen template at [`fffi2_compiletime_go_server.go:682-692`](../../src/go/public/thestack/fffi2/compiletime/goserver/fffi2_compiletime_go_server.go) emits, for every block iterator with an id, a gated yield:

```go
func (inst CollapsingHeaderFluid) KeepIter() iter.Seq[functional.NilIteratorValueType] {
    inst.r.WriteOpCode(uint32(CollapsingHeaderMethodIdBuild))
    r := inst.r.BuildRetained()
    return func(yield func(functional.NilIteratorValueType) bool) {
        defer func() { inst.idGen.PopIdFromStackChecked(inst.id) }()
        r.SyncRetained()
        defer func() { End() }()
        if !CurrentApplicationState.StateManager.getResponseByIdRaw(inst.id).HasBlockSkipped() {
            yield(functional.NilIteratorValue)
        }
    }
}
```

`HasBlockSkipped()` reads `responseFlags`, which `StateManager.Sync()` populates from the previous frame's `r7` register. Rust's CollapsingHeader handler at [`interpreter.rs:2700`](../../src/rust/src/imzero2/interpreter.rs) sets `BLOCK_SKIPPED` when egui's `CollapsingHeader::show()` returns `body_returned.is_none()`. Window has the matching pattern at [`interpreter.rs:3086`](../../src/rust/src/imzero2/interpreter.rs).

The same shape is generated for ~24 block iterators (CollapsingHeader, Window, ComboBox, MenuButton, HoverText, ScrollArea, Frame, Group, Horizontal*, Indent, MenuBar, Panel*, PushId, Scope, …); the gate is inert for layout-only blocks because their handlers never push `BLOCK_SKIPPED`, but it is load-bearing for the user-toggleable ones.

### Failure mode

For a single CollapsingHeader transitioning closed→open via user click:

| Frame | Go side | Rust side | Visible |
|---|---|---|---|
| N | reads frame N-1 = skipped → does NOT yield. Stream contains only header + `End`. | egui detects click, toggles to open. `show()` calls closure; `interpret_outer` consumes only `End`. `body_returned = Some(())`, no `BLOCK_SKIPPED`. | Open header with **empty body** — flicker. |
| N+1 | reads frame N = not skipped → yields body. | renders body. | Body "pops in" one frame late. |

For the open→close transition, egui keeps `body_returned = Some(())` while the close animation plays, so `BLOCK_SKIPPED` only fires once the animation finishes — Go keeps emitting body content the whole way and the close has no visible gap.

### Nesting compounds the lag

A block's `r7` only updates when Rust visited it last frame. If the parent was closed, the inner block was never visited and its `r7` entry is **frozen** at whatever value it had when last visible. A closed→open click that propagates through `Window > CollapsingHeader > ScrollArea > CollapsingHeader` exposes one extra frame of empty content per level — the user sees the layout grow tier by tier, with content "popping in" as each level's gate refreshes. That is the user-reported "slow response" component of the bug.

### Naive gate removal is insufficient

Dropping the gate so every `KeepIter` always yields fixes click-to-open and nesting compounding, but reintroduces the stall pattern documented in [`demo/widgets/interactive_driver.go:19-29`](../../src/go/public/thestack/imzero2/egui2/demo/apps/widgets/interactive_driver.go) (the original ~11s ADR-0057 startup delay) at every frame, because heavy demo body lambdas (walkers tile fetch, graphs force-layout, treemap2 layout) would run unconditionally for ~17 collapsed demos. App-level `IsBlockSkipped` skips reintroduce the original lag at user-code granularity. The gate trades correctness for steady-state perf; we want both.

### The Rust side is not actually stateless

The "stream is the source of truth, each frame can be rendered from its opcode stream" intuition is approximately right but incomplete. [`ImZeroFffi`](../../src/rust/src/imzero2/interpreter.rs) ([`:1469`](../../src/rust/src/imzero2/interpreter.rs)) already carries cross-frame state for five widget classes, with comments explicitly calling out why the field is **not** cleared in [`prepare_next_frame()`](../../src/rust/src/imzero2/interpreter.rs) (`:1691`):

| Field | Persists | Reason |
|---|---|---|
| `dock_states: HashMap<u64, egui_dock::DockState<u64>>` | splits, active tab per group, drag-to-reorder | egui_dock owns layout |
| `graph_states: HashMap<u64, GraphState>` | force-layout positions, drag state | egui_graphs persistent layout |
| `walkers_states: HashMap<u64, WalkersState>` | HttpTiles + MapMemory + h3 outline cache | tile cache + map camera |
| `scrolling_texture: ScrollingTextureCache` | per-id ring buffer texture (ADR-0058) | ring-buffer pixel widget |
| `code_view_cache` | tokenized highlight cache | syntax highlighting amortisation |

Plus egui's own `Memory`, which is where the open/closed bit for every `CollapsingHeader` and `Window` actually lives. The flicker exists because the gate uses `r7` as a one-frame-stale *mirror* of egui's already-persistent state. The framework's identity is "stream-driven for most widgets, id-keyed Rust-side state for widgets whose semantics demand it" — not "stateless".

### Forces the decision must respect

- **Stream-of-operations as the dominant pattern.** Most widgets must remain re-renderable from the current frame's stream. New persistent state must be id-keyed and not load-bearing for widgets that don't opt in.
- **One-frame FFI lag is intrinsic.** The Go→Rust stream is one-way per frame. Solutions that require Go to know mid-frame Rust state must explicitly model a sync round-trip; pretending the lag isn't there leads to the gate's failure mode.
- **Capture infrastructure already exists.** `Fffi2.captureStack`, `BeginCapture`/`EndCapture`, `AppendRawToCapture`, the `iter.Seq` iter-scope wrapper for deferred-block widgets ([`SKILLS.md` §5.5](../skills/imzero2/SKILLS.md)) are all in place for ETable and DockArea. Reuse beats re-invention.
- **Iter-scope ergonomics.** The block-iterator API (`for range c.X(...).KeepIter()`) is the canonical idiom for blocks throughout the codebase. New machinery should compose with it, not parallel it.
- **Heavy bodies exist in the wild.** Demos with treemap layout, walkers tile fetch, force-layout graphs are not pathological — they're the design target. Steady-state-collapsed cost matters.
- **Nested collapsibles are common** (Window > CollapsingHeader > ScrollArea > Grid > …). Whatever fixes the top-level case must compose, not just succeed at depth 1.
- **Predictability over cleverness.** Heuristic / speculative approaches (animation-aware gating, click prediction) introduce non-determinism that's hard to debug in a multi-frame UI. The boxer coding standards favour explicit, deterministic mechanisms.

### Question this ADR settles

How does imzero2 eliminate the click-to-open flicker and nesting compounding for CollapsingHeader, Window, and similar block-skipping widgets, without reintroducing per-frame heavy-body cost in steady-state-collapsed?

### Discovery during stopgap testing (2026-04-26)

The first attempt at the stopgap — drop the `HasBlockSkipped` gate from the codegen and let Rust drain — uncovered a latent stream-framing bug in the Rust-side block handlers that the gate had been silently masking. The full chain:

1. Every Go-side block emits `[Block opcode][...body messages...][End message]` as separate top-level FFI messages. `End` is a distinct opcode that, when read by Rust's `interpret_outer`, sets `r=true` and terminates the loop ([`interpreter.rs:3670`](../../src/rust/src/imzero2/interpreter.rs)).
2. Layout blocks like `Horizontal`, `ScrollArea`, `Frame`, `Indent`, `Group`, `Vertical*`, `Panel*`, `PushId`, `Scope`, `MenuBar` — the long tail under [`egui2_definition_d_blocks.go`](../../src/go/public/thestack/imzero2/egui2/definition/egui2_definition_d_blocks.go) — guard their body handling with `if u.is_some() { … }` and have **no `else`**: when culled (parent passes `u=None`) they do nothing. The body messages and the block's own `End` stay in the stream.
3. The parent's drain loop (`interpret_outer(c, &mut None)`) keeps reading messages. When it hits the inner block's `End`, it terminates **one block too early** — leaving the parent's own body remainder and its own `End` unconsumed.
4. The unconsumed messages bubble up to the next outer frame of `interpret_outer`, which itself sees an `End` it didn't intend to consume. Frame rendering corrupts from that point: collapsing a `Window` makes the entire window disappear (its `show()` already drew the title bar, but the closed-branch drain returned mid-body and the leftover messages took the top-level loop down with them).

[`SKILLS.md` §13.3](../skills/imzero2/SKILLS.md) Scenario B previously claimed this nested-cull case was safe because "registers are global, drain semantics are preserved — just at the wrong nesting depth, which doesn't matter for register operations." That note covers register correctness but **not** `End`-sentinel framing. Pre-stopgap the gate prevented the scenario entirely (collapsed parents emitted no body messages, so there were no orphaned nested `End`s to leak), so the bug never surfaced. Removing the gate exposes it immediately and severely.

The stopgap (O2) was therefore reverted. The gate is re-classified: **load-bearing for stream framing, not just a performance optimization**. Any future gate-removal must be paired with making every block's Rust apply code drain its own body when `u=None` (i.e. add `else { interpret_outer(c, &mut None)?; }` next to the existing `if u.is_some() { … }` in every block in [`egui2_definition_d_blocks.go`](../../src/go/public/thestack/imzero2/egui2/definition/egui2_definition_d_blocks.go)).

## Design space (QOC)

**Question.** Same as above.

**Options.**

- **O1 — Status quo (previous-frame skip gate).** Keep the generated `HasBlockSkipped` gate. No change.
- **O2 — Gate removal (always emit body).** Drop the gate from the codegen template; let Rust drain body opcodes when the block is collapsed. Simple, one-line template change.
- **O3 — Two-pass frame (probe → commit).** Split each frame into a probe pass (Go emits skeleton ids only, Rust returns open-state bitset) and a commit pass (Go re-emits with bodies for open blocks).
- **O4 — Iterator-based retained bodies _(chosen)_.** New IDL annotation `WithRetainedBlock`. Body is captured into a Go-side buffer once via the existing `BeginCapture`/`EndCapture` machinery and stashed on the Rust side keyed by widget id. The block's `KeepIter` becomes an `iter.Seq` that yields zero times when an invalidation key matches (cache fresh) and once when it doesn't (cache miss → re-capture → upload). On render, Rust splices cached opcodes inline whenever egui says the block is open; eviction signalled to Go via a new r7 flag.
- **O5 — Deferred toggle (Rust-side click hold).** Rust intercepts the click-to-open transition by one frame: holds the click, renders pressed feedback, signals `JUST_CLICKED_TO_OPEN` in r7. Go force-yields body the next frame; Rust then commits the open. No persistent body cache.
- **O6 — Synchronous per-block query.** Inside `KeepIter`, replace the previous-frame read with a synchronous mid-frame FFI call that flushes the pending stream and waits for Rust's current open-state. Mirrors the `cachingMeasurer` pattern used for layout sizing.

**Criteria.**

- **C1 — Click-to-open visible lag.** Frames of empty content visible after a single click on a closed collapsible.
- **C2 — Steady-state-collapsed cost.** Per-frame cost when a block stays collapsed: lambda execution, opcode emission, FFI bandwidth, Rust-side drain.
- **C3 — Nested compounding.** Whether lag adds up across nested collapsibles or stays bounded.
- **C4 — Implementation effort.** Engineer-time to ship, including IDL/codegen/runtime/doc/test changes.
- **C5 — Architectural fit.** Alignment with the existing stream-driven core, the existing five Rust-side caches, the iter-scope idiom, and the boxer coding standards' preference for explicit deterministic mechanisms.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 (status quo) | O2 (gate removal) | O3 (two-pass) | O4 (retained bodies) | O5 (deferred toggle) | O6 (sync query) |
|----|-----------------|-------------------|---------------|----------------------|----------------------|-----------------|
| C1 | −−              | ++                | ++            | ++                   | +                    | ++              |
| C2 | ++              | −−                | +             | ++                   | ++                   | +               |
| C3 | −−              | ++                | ++            | ++                   | −                    | ++              |
| C4 | ++              | ++                | −−            | −                    | +                    | +               |
| C5 | +               | +                 | −−            | ++                   | +                    | +               |

**Reasoning condensed.**

- **O1** is free but loses on every UX axis; failure mode is the bug we are filing.
- **O2** is a one-line template change but pays the steady-state-collapsed cost on every frame; would reintroduce the ADR-0057 stall pattern continuously for the demo shell unless every heavy body lambda is hand-guarded.
- **O3** is correctness-clean — eliminates lag entirely with a deterministic 2-step pipeline — but is a frame-loop architectural shift and forces the widget-tree shape to be stable across two passes; large blast radius for a localised problem.
- **O4** matches the pattern of the existing five Rust-side caches almost line for line, reuses the capture infrastructure already shipped for ETable/DockArea, and lets the iter-scope yield-zero-or-once semantics express "refresh cache only when needed". Adds one new kind of state (opaque opcode bytes), which is a real but bounded novelty.
- **O5** is the cheapest fix that actually addresses the perceptual problem at depth 1, but does not compose through nesting unless the click signal is propagated through descendants — a non-trivial extension that lives outside the elegant single-block fix.
- **O6** is the per-block analogue of O3; predictable per call but scales linearly in block count per frame, and the FFI sync is not free. Reasonable as a fallback for one-off cases (mirrors `cachingMeasurer`'s niche) but unappealing as the default.

## Decision

We will adopt **O4 — iterator-based retained bodies** as the long-term mechanism for collapsible block skipping (concrete shape unchanged from the original draft, retained below).

The original plan had **O2 — gate removal** land first as a quick stopgap. After live testing the stopgap (`hmi.sh`) it was reverted because it triggered the framing bug described in *Discovery*. The stopgap is now **only viable after a prerequisite Rust-side change**:

**Prerequisite step: drain-on-cull for every block.** Add `else { interpret_outer(c, &mut None)?; }` to every block's apply code in [`egui2_definition_d_blocks.go`](../../src/go/public/thestack/imzero2/egui2/definition/egui2_definition_d_blocks.go) so each block consumes its own body and `End` regardless of whether `u` is `Some(ui)` or `None`. Affects ~30 blocks: `Horizontal*`, `Vertical*`, `Frame`, `Group`, `Indent`, `ScrollArea`, `Panel*`, `PushId`, `Scope`, `MenuBar`, plus the existing ones (`CollapsingHeader`, `Window`, `ComboBox`, `MenuButton`, `HoverText`) which currently have an explicit drain only inside the `u.is_some()` branch and need a sibling drain in the `else`. After this change, [`SKILLS.md` §13.3](../skills/imzero2/SKILLS.md) Scenario B is no longer relied on; every block is self-contained for stream framing.

Once the drain-on-cull change is in place, **O2 (gate removal) becomes safe** and can be reapplied as the stopgap, on the same explicit understanding as before:

- the stopgap fixes click-to-open flicker and nesting compounding immediately;
- the steady-state cost is acceptable for the long tail of small-body collapsibles;
- the demo shell ([`interactive_driver.go`](../../src/go/public/thestack/imzero2/egui2/demo/apps/widgets/interactive_driver.go)) and any other heavy-body host gets an app-level `IsBlockSkipped` guard in the interim, accepting one frame of empty body on click-to-open in those specific places;
- the gate-removal stopgap is removed when O4 lands.

The long-term path to **O4 — iterator-based retained bodies** is unchanged. Concretely:

1. Add a new IDL annotation `WithRetainedBlock(name, keyType)` analogous to the existing `WithDeferredBlockMap`. The codegen emits `BeginBody`/`EndBody` capture API on the parent fluid and a `BodyKey(k)` builder method.
2. Rewrite the affected `KeepIter` template so the iterator yields **once** when the held key differs from the cached key (or no entry exists), capturing the body into the Rust-side cache, and yields **zero times** otherwise — the existing `iter.Seq` semantics carry the gate naturally.
3. Add a `retained_block_bodies: HashMap<u64, RetainedBody>` field to `ImZeroFffi`. Lifetime is per widget id; cleared when the id is no longer rendered (parented to existing id-eviction passes) and on explicit invalidation. Storage holds the captured opcode bytes plus the key Go last sent.
4. Add a `RETAIN_MISS` bit to `ResponseFlags` (and a fetcher path) so Rust can request a refresh when it has evicted a body under memory pressure or never had it. Go's iterator yields once on next frame on miss; the one-frame lag in the eviction case is acceptable because eviction is rare and bounded.
5. On the parent block's render, Rust splices the cached body opcodes inline at the body site whenever egui says the block is open (or animating). The `BLOCK_SKIPPED` signal becomes advisory only — used for app-level cost decisions outside the gated yield, not for opcode emission gating.

The drain-on-cull prerequisite also benefits O4: retained bodies put body opcodes in the stream regardless of cull state (sometimes inline, sometimes by-reference), so the same framing robustness is needed there.

We explicitly do **not** adopt O3 (two-pass) at this time. It remains a viable future refactor if a class of synchronous-state problems beyond this one accumulates and justifies the frame-loop change. O4 does not preclude O3 — the retained body cache survives a future move to two-pass framing.

### Status of this ADR's implementation

- ✅ Drain-on-cull added to every block in [`egui2_definition_d_blocks.go`](../../src/go/public/thestack/imzero2/egui2/definition/egui2_definition_d_blocks.go). Each block now carries an explicit `else { interpret_outer(c, &mut None)?; }` arm that drains its own body + `End` when `u=None`. Affected: `Frame`, `ScrollArea`, `CollapsingHeader`, `ComboBox`, `MenuButton`, `MenuBar`, `Group`, `Scope`, `Indent`, `PushId`, `EnabledUi`, `Horizontal*`, `Vertical*`, `Grid`, `HoverText`, `AllocateUiAtRect`, `UiWithLayout`, `Panel*Inside`. Window and the panel root variants don't need it (they always render via `show(ctx)`); their existing post-`show()` drains cover the closed/collapsed branches.
- ✅ Codegen template gate-removal re-applied (`fffi2_compiletime_go_server.go`). Block iterators now always yield; ~15 generated `KeepIter` functions in `methods.out.go` no longer carry `HasBlockSkipped` checks.
- ✅ `Handle()` method on user-toggleable block fluids + free `IsBlockSkipped()` helper in [`egui2_methods.go`](../../src/go/public/thestack/imzero2/egui2/bindings/egui2_methods.go).
- ✅ Demo guard in [`interactive_driver.go`](../../src/go/public/thestack/imzero2/egui2/demo/apps/widgets/interactive_driver.go) uses the new helper to short-circuit heavy demo bodies.
- ◻ Wire-level regression test was added under `egui2/widget/examples/adr0012_collapsible_no_skip_gate_test.go` and re-enabled, then retired in commit 85747b80 along with the surrounding `egui2/widget/` retained-mode harness ("low value, suggests a retained mode widget"). The drain-on-cull behaviour the test asserted is now covered indirectly by the demo guard above and the `IsBlockSkipped()` helper's runtime contract.
- ⬜ O4 retained-body machinery — full Phase 2 still future work.

### Phase 2 sub-design: cache eviction strategies for stateful widgets

The retained-body cache will be the seventh entry in `ImZeroFffi`'s id-keyed cross-frame state set. Entries store opaque opcode bytes, individually possibly hundreds of KB, with id sets that can grow dynamically over time (filterable demo lists, paginated tables, dock-area tab churn). Eviction therefore matters here in a way it does not for the existing caches. This section catalogues every eviction strategy worth considering, surveys what the codebase currently uses, and lands on a recommendation for the retained-body cache.

#### Survey of existing patterns

| Cache | Type | Eviction | Bound |
|---|---|---|---|
| `dock_states` | `HashMap<u64, egui_dock::DockState<u64>>` | None | Unbounded; relies on bounded id-string catalogue |
| `graph_states` | `HashMap<u64, GraphState>` | None | Unbounded; small per-entry |
| `walkers_states` | `HashMap<u64, WalkersState>` | None | Unbounded; per-entry includes HttpTiles cache + h3 outlines |
| `scrolling_texture` (entries) | `HashMap<u64, Entry>` | TTL by `last_touched_frame` (`MAX_AGE_FRAMES = 600` ≈ 10 s) + explicit `release(id)` opcode | Bounded by `MAX_AGE_FRAMES` |
| `code_view_cache` (entries) | `HashMap<u64, CacheEntry>` keyed by *content hash* (not widget id) | None | Unbounded; one entry per distinct (text, sections) ever seen |

Three of five caches are **immortal** — `prepare_next_frame()` explicitly does not clear them, the comments call it out. They get away with it because (a) entries are small and (b) widget-id cardinality is bounded by the app's `PrepareStr` catalogue. `ScrollingTextureCache` is the one place a real bounded-memory eviction policy lives, motivated by GPU texture cost.

#### Strategy catalogue

**E1 — No eviction (immortal entries).**
- *Mechanism.* HashMap grows monotonically. `prepare_next_frame` does not touch it.
- *Pros.* Zero overhead, no metadata, simplest implementation. Used today for `dock_states` / `graph_states` / `walkers_states` / `code_view_cache`.
- *Cons.* Memory bound is whatever id+key space the app touches over the process lifetime. Wrong for any cache where per-entry size is large *and* id cardinality is open-ended.

**E2 — Explicit release only.**
- *Mechanism.* Go emits a `Release(id)` opcode when it knows the id is gone. Rust drops the entry on receipt.
- *Pros.* Per-id precision, no metadata per entry beyond the value itself, no per-frame work.
- *Cons.* Footgun: forgetting to release leaks forever. Unfit for a Go authoring model where widget lifetime tracking is implicit (`PrepareStr` paths come and go without any explicit "destroy" event). Used today as the *fast-path* alongside TTL in `ScrollingTextureCache`, not as the sole policy.

**E3 — TTL by `last_touched_frame`.**
- *Mechanism.* Each entry stores the frame number on which it was last accessed. `tick()` runs once per real frame, increments a global counter, and `HashMap::retain`s entries within `MAX_AGE_FRAMES`. The `ScrollingTextureCache` pattern.
- *Pros.* Bounded memory regardless of access pattern. No per-id Go bookkeeping. O(n) per-frame retention scan, but n is small in practice. Predictable: TTL is human-readable in seconds.
- *Cons.* Long-collapsed widgets get evicted; reopening pays a `RETAIN_MISS` re-capture (one frame of empty body or the cost of one re-emission depending on the protocol). Per-entry metadata is one `u64`. The retention scan is O(n) every frame, even when nothing needs eviction.

**E4 — Hybrid TTL + explicit release.**
- *Mechanism.* Both E2 and E3. TTL is the safety net; explicit release is the precision tool when the app *does* know.
- *Pros.* Best of both. Already a proven pattern in the codebase (`ScrollingTextureCache.tick()` + `release(id)`).
- *Cons.* Slightly more code than either alone. Explicit release is optional, so it's tolerable to omit.

**E5 — LRU bounded by entry count or byte budget.**
- *Mechanism.* HashMap + doubly-linked list (or `IndexMap`-style ordered map). On access, move to MRU end. On insertion that exceeds the cap, evict the LRU end. Variant: per-entry byte-size accounting bounds total memory rather than entry count.
- *Pros.* Strong worst-case memory guarantee. Frequently-used entries stay cached regardless of TTL.
- *Cons.* Two data structures to keep consistent (HashMap + list/order). Per-access cost (move-to-front) instead of pure O(1). Significant implementation complexity. Byte-budget variant adds size accounting per entry. Overkill for v0 unless one specific app demonstrably blows memory under E4.

**E6 — Random replacement (RR), bounded by entry count or byte budget.**
- *Mechanism.* HashMap + a count or byte budget. On insertion that exceeds the cap, pick a random entry (e.g. `keys().nth(rng.gen_range(0..n))`) and evict it. No timestamps, no list, no metadata beyond the value.
- *Pros.* O(1) eviction without LRU's bookkeeping. No per-entry metadata. Probabilistic studies (CPU caches, ARC, …) show RR is within a small constant of LRU on most workloads. Adversarial workloads exist but UI rendering isn't one — the working set is small relative to total entry count, and retained bodies are independently re-capturable on miss.
- *Cons.* A frequently-used entry can get unlucky and be evicted. The user explicitly raised this as a viable option; the trade vs LRU is "guaranteed best vs guaranteed cheap, with similar expected behaviour."
- *Note.* Random nth-key access in `std::HashMap` is O(n) without an indexed order; can be made O(1) with `IndexMap` or a parallel `Vec<u64>` keyset.

**E7 — FIFO bounded.**
- *Mechanism.* A queue of insertion order. On overflow, evict the head.
- *Pros.* O(1), no per-access updates. Simpler than LRU.
- *Cons.* No correlation with usage — a widget that's been on screen for hours but was inserted first is the first to go. Generally inferior to RR for our workload because the typical case (long-open Window with stable body) is exactly the "insertion was a long time ago" pathology.

**E8 — Reachability mark-sweep (live-id set).**
- *Mechanism.* Each frame Go emits a `LiveIds([…])` set. Rust drops every entry whose id is not in the set (after a small grace period to avoid one-frame-blip evictions during scroll-virtualisation).
- *Pros.* Mathematically tight: cache exactly mirrors the set of widgets currently rendered. No TTL tuning.
- *Cons.* Go must enumerate every retained-body id every frame — extra wire traffic linear in id count. Loses the property that an off-screen-but-recently-visible widget reopens instantly: it's evicted the moment Go stops emitting it. Mismatched with virtualised UIs where ids cycle in and out by viewport.

**E9 — Generational eviction.**
- *Mechanism.* A monotonically-increasing generation token. Each entry stores its generation at insertion. On a "wipe" event (Rust restart proxy, font atlas rebuild, theme change, …), bump the generation; on the next access, evict any entry from a previous generation.
- *Pros.* Specific tool for "everything stale because some external invariant changed". Cheap.
- *Cons.* Doesn't bound memory under steady-state — it's an orthogonal eviction trigger, useful as an *adjunct* to E4/E5/E6, not as the sole policy.

#### Comparison table

| | E1 None | E2 Release-only | E3 TTL | E4 TTL+release | E5 LRU | E6 RR | E7 FIFO | E8 Mark-sweep |
|---|---|---|---|---|---|---|---|---|
| Bounded memory | ✗ | ✗* | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| Per-frame overhead | none | none | O(n) retain | O(n) retain | O(1) | O(1) | O(1) | O(live ids) wire |
| Per-access cost | O(1) | O(1) | O(1) | O(1) | O(1) MRU update | O(1) | O(1) | O(1) |
| Per-entry metadata | none | none | u64 frame | u64 frame | list ptrs (+u64 size) | none (+u64 size if budget) | list ptrs | none |
| Risk: evict needed entry | – | only on bug | low (TTL tuned) | low | minimal | low (probabilistic) | high | low |
| Implementation effort | trivial | trivial | small | small | substantial | small | small | medium |
| Codebase precedent | ✓ widely | partial | ✓ scrolling_texture | ✓ scrolling_texture | – | – | – | – |
| Fit with retained bodies | ✗ open-ended ids × big bodies | ✗ no Go release event | ✓ | ✓✓ | ✓ if budget really matters | ✓ when LRU's bookkeeping isn't worth it | ✗ misaligned with usage | ✓ but wire-cost linear in id count |

\* E2 is bounded *if* the app reliably emits release; in practice this is a should-not-be-required-of-callers contract.

#### Recommendation for the retained-body cache (Phase 2)

**E4 (Hybrid TTL + explicit release)** is the default. Concretely:

- Mirror `ScrollingTextureCache`'s shape exactly: per-entry `last_touched_frame: u64`; a `tick()` method called from `interpret_commands_outer` once per real frame; `MAX_AGE_FRAMES` constant (suggested **1800** ≈ 30 s, longer than the texture cache's 10 s because re-capturing a body is more expensive than rebuilding a texture).
- Expose an opcode `RetainedBodyRelease(id)` (and a Go-side `c.ReleaseRetainedBody(handle)`) for callers that *do* know when a widget is gone (e.g. the demo registry when a demo is unregistered). Optional, not required.

**E6 (Random replacement)** is the documented escape hatch if E4's TTL turns out to bite under specific workloads (e.g. an app that renders 10 000 distinct retained-body ids per session, each ≥ 100 KB). It can be added later as a byte-budget cap on top of E4 — the cap fires only when total cached bytes exceed the budget, evicts a random entry, and the TTL keeps doing its thing alongside. The user is right that RR is a serious option; it's not the v0 default purely because E4 has codebase precedent and is the lowest-risk shape for the first retained-body release.

**E1 (None)** is rejected for retained bodies despite its prevalence elsewhere — per-entry size + open-ended id cardinality push memory to the wrong region.
**E2 alone** is rejected because Go's authoring model has no reliable "widget destroyed" event.
**E5 (LRU)** is rejected for v0 — strictly better than RR only on adversarial workloads, two data structures to keep consistent, not justified before any measured pressure.
**E7 (FIFO)** is rejected — the dominant pattern in this UI (long-lived collapsed Window with retained body that becomes useful on click) is exactly FIFO's worst case.
**E8 (Mark-sweep)** is rejected for v0 — wire cost linear in cached id count is undesirable, and the off-screen-instant-reopen property of E4 is more useful than mathematical tightness.
**E9 (Generational)** is kept in reserve as a future trigger if hot-reload / theme-change / font-atlas-rebuild events emerge that need wholesale invalidation; it would compose with E4 cleanly.

#### Notes for the existing immortal caches

This survey also surfaces a question for the existing caches: should `dock_states`, `graph_states`, `walkers_states`, and `code_view_cache` adopt E3 or E4? Per-entry sizes for the first three are small but non-zero (`HashMap<u64, GraphState>` with force-layout positions can carry tens of KB per graph that's been rendered in this session); `code_view_cache` is keyed by content hash and can grow with the variety of SQL strings highlighted. None of these are urgent, but a shared `IdKeyedCache<T>` helper applying the same `tick()`/`release()` shape would let all five caches share a uniform eviction story. Out of scope for ADR-0012; flagged here for a future cleanup ticket.

## Alternatives

- **O1 — Status quo.** Rejected: the bug is user-visible and structurally ineradicable without code change.
- **O2 — Gate removal as the long-term solution.** Rejected as a permanent solution: the steady-state cost recurs every frame for heavy-bodied collapsibles. Accepted as a stopgap (see Decision).
- **O3 — Two-pass frame.** Rejected for now: large blast radius, frame-loop discipline change, requires widget-tree stability across passes; disproportionate to the single-axis problem this ADR addresses. Reconsider if multiple sync-state problems converge.
- **O5 — Deferred toggle.** Rejected: addresses only the depth-1 case; nested collapsibles still compound by N-1 frames; descendant propagation of `JUST_CLICKED_TO_OPEN` adds complexity without resolving the underlying steady-state cost question.
- **O6 — Synchronous per-block query.** Rejected as the default: per-call FFI sync scales linearly in collapsible count per frame. Retained as a possible escape hatch for one-off cases that need same-frame answers (matches the existing `cachingMeasurer` precedent).

## Consequences

### Positive

- **Click-to-open is flicker-free.** Rust always has the body bytes (cached or freshly captured this frame); the click frame splices cached opcodes inline with egui's open animation.
- **Nesting does not compound.** Each level's body is independently cached and replayed; opening an outer parent does not require N frames to reveal N levels of content.
- **Steady-state-collapsed cost approaches zero.** Go ships only header opcodes + a body-by-reference token; the user's body lambda does not run; no body opcodes traverse the FFI.
- **Architectural fit is high.** Adds one entry to the existing five-class id-keyed Rust-side cache table. Reuses `Fffi2.captureStack`, `BeginCapture`/`EndCapture`, `AppendRawToCapture`, and the `DockArea` iter-scope idiom. Fits the boxer coding standards' preference for `iter.Seq`-driven block iteration.
- **Composes with future work.** A retained body can contain another retained body, an ETable, or any deferred-block widget — same capture-stack, same nesting rules.
- **`IsBlockSkipped` semantics are clarified.** It becomes an advisory app-level signal rather than a structural gate; this removes the previous-frame-staleness footgun for application authors.

### Negative

- **First Rust-side cache holding opaque Go-emitted bytes.** Existing caches hold egui-meaningful values (positions, layouts, tile pixels). Opaque bytes are less inspectable on a debugger; corruption is harder to localise. Mitigation: integrity tagging (length + checksum) on the cached bytes, plus diagnostic counters surfaced via debug tools.
- **Lifetime discipline tightens.** Cached bytes may carry handles to other Go-allocated state (font atlas IDs, retained text holders, color holders). The retained body's lifetime must be ≤ its referenced handles' lifetimes. Mitigation: document the contract explicitly; consider reference-counting referenced handles or invalidating the cache on handle release.
- **Key derivation becomes user code.** The user must derive an invalidation key that hashes everything the body reads from. A wrong key produces stale content with no error. Same problem class as React/SwiftUI memoization keys; well-understood failure mode but a new burden for imzero2 authors.
- **Implementation effort.** New IDL annotation + codegen path + Rust-side cache + eviction protocol + r7 flag + fetcher + docs + tests. Larger than O2 or O5; smaller than O3.
- **Migration scope.** ~24 generated `KeepIter` callsites change behaviour. Existing user code that relies on the previous-frame `BLOCK_SKIPPED` value reaching `IsBlockSkipped` must be audited (current call sites: [`egui2_datepicker.go`](../../src/go/public/thestack/imzero2/egui2/bindings/egui2_datepicker.go), [`egui2_scrolling_texture.go`](../../src/go/public/thestack/imzero2/egui2/bindings/egui2_scrolling_texture.go), `egui2_badge.go` (since folded into `widgets/badge/`), [`interactive_driver.go`](../../src/go/public/thestack/imzero2/egui2/demo/apps/widgets/interactive_driver.go)).

### Neutral

- **Stream-of-operations identity weakens slightly.** A frame's stream alone no longer fully describes what is on screen for retained-body collapsibles; the cached bytes from previous frames participate. The framework was already partly in this regime (dock, graphs, walkers, scrolling-texture, code-view); this is one more class. Worth recording but not the introduction of a new paradigm.
- **`HasBlockSkipped` register stays.** Still set by Rust, still readable by Go; just no longer the structural gate on body emission.
- **Capture-stack invariants unchanged.** Retained bodies push/pop on the same stack as deferred blocks; nesting rules from [`SKILLS.md` §5.4](../skills/imzero2/SKILLS.md) carry over verbatim.
- **ETable's deferred-block mechanism is unaffected.** The two systems sit on orthogonal axes (in-frame vs cross-frame); they share infrastructure but solve different problems.

## Updates

### 2026-05-26: `.Keep()` / `SyncRetained` is the (a) protocol — slot wired, channel-layer stubbed

While investigating per-frame wire-byte growth in `imztop` (root cause traced via [ADR-0049](./0049-fffi2-deferred-block-scope-buffer-memoization.md)'s new `top_scopes` field on the slow-frame log — `TabBody=405K-428K, growing ~8 KB/sec`), we rediscovered that the **id-keyed cache+refetch protocol (a) from the two-protocols framing of this ADR was scoped into the Go-side type machinery but was never wired through the channel layer.** The slot is there; the implementation falls back to "send bytes again."

The evidence chain:

- [`fffi2_typed_impl.go:167-181`](../../src/go/public/thestack/fffi2/typed/fffi2_typed_impl.go) — `BuildRetained()` computes a content-addressed `RetainedElementId` via `xxh3.Hash(raw)` and interns the bytes through `unique.Make`. The stable id and the interned content are both ready.
- [`fffi2_typed_impl.go:206-208`](../../src/go/public/thestack/fffi2/typed/fffi2_typed_impl.go) — `(*RetainedFffiHolder).SyncRetained()` exists as a distinct method from `SendIntermediate` and threads the id all the way down to the runtime.
- [`fffi2_rt_impl.go:25-28`](../../src/go/public/thestack/fffi2/runtime/fffi2_rt_impl.go) — `Fffi2.SyncRetained(id, buf)` **comments out** the channel-level call and falls back to `SendIntermediate(buf)`:

  ```go
  func (inst *Fffi2[U]) SyncRetained(id uint64, buf []byte) (err error) {
      //return inst.channel.SyncRetained(id, buf)
      return inst.SendIntermediate(buf)
  }
  ```

So `.Keep()` today gives the framework consumer (i) marshal-once / splice-many subtrees, (ii) memory-side interning of identical content, and (iii) a stable content-hash id — but **does not give cross-frame wire-byte savings**, because the channel never tells Rust "you already have this; use it again." Every frame re-transmits the full bytes. The architectural intent — `.Send()` is the immediate-mode default, `.Keep()` carries over time — was clear from the type design but lost its load-bearing edge to a stub.

This update records that finding and scopes the implementation as Phase 3 of this ADR (orthogonal to Phase 2, which targets *collapsible* bodies keyed by widget id; this targets *content-identical* bodies keyed by hash).

#### Implementation plan — Phase 3: wire `.Keep()` to cross-frame Rust-side cache

Milestones, sized to land on the same review-per-milestone cadence as Phase 1/2:

**M3.0 — Design decisions (notes-only PR).**

- **Cache eviction policy.** LRU by access time, capped by total bytes (initial budget: 8 MiB; tunable). Rationale: a few KB per `.Keep()`'d subtree × low hundreds of distinct holders × occasional rotation. Memory bound is the failure mode that scares people; choose a number we can defend.
- **Opcode shape.** Two new IDL function opcodes:
  - `FuncProcIdRetainAndReplay(id u64, len u32, bytes [u8; len])` — first-time arrival of `id`. Rust caches the framed bytes by id, then replays them through `begin_consume_message`.
  - `FuncProcIdReplay(id u64)` — subsequent arrivals. Rust looks `id` up in the cache and replays. Misses (cache evicted or never seen) signal a refetch via the existing register-drain path (see M3.3).
- **Key-collision policy.** `xxh3` is 64-bit. Birthday at p=10⁻⁹ ≈ 1.5e5 entries. Budget under that with the byte-cap above. Document the floor; revisit if it bites.
- **Frame-loop placement.** The cache lives next to the existing `ImZeroFffi` state (see `interpreter.rs:1469`); evicted alongside other id-keyed state at `prepare_next_frame`.
- **Runtime feature gate.** The whole channel-level retention path is gated by a single `atomic.Bool` checked at the call chokepoint, so the feature can be flipped on or off mid-process at sub-nanosecond hot-path cost. The chokepoint is the existing `Fffi2.SyncRetained` stub at [`fffi2_rt_impl.go:25-28`](../../src/go/public/thestack/fffi2/runtime/fffi2_rt_impl.go); every `.Keep()`'d holder already routes through it via `(*RetainedFffiHolder).SyncRetained()`. Shape:

  ```go
  // fffi2/runtime — single package-level atomic, default false.
  var retainKeepCacheEnabled atomic.Bool

  func SetRetainKeepCacheEnabled(v bool) { retainKeepCacheEnabled.Store(v) }

  func (inst *Fffi2[U]) SyncRetained(id uint64, buf []byte) (err error) {
      if !retainKeepCacheEnabled.Load() {
          return inst.SendIntermediate(buf)             // unchanged from today
      }
      return inst.channel.SyncRetained(id, buf)         // new id-keyed path
  }
  ```

  **Cost when disabled (steady state):** one `atomic.Bool.Load` per `.Keep()` holder per frame. On amd64 this compiles to a single naturally-aligned `MOVQ` — no `LOCK` prefix, no fence — so the load is ~1-2 ns and the always-false branch is perfectly predicted after the first 2-3 frames. At 60 Hz × ~100 holders that's ~6 µs / sec, i.e. ~0.0006 % of one core. Behaviorally bit-identical to today: the false branch is exactly the current stub.

  **Cost when enabled:** same load + branch (~1-2 ns), then enters the new channel path with a `seenRetained` map lookup (~20-30 ns). The saved wire bytes (a `Replay(id)` opcode is ~12 bytes regardless of payload) pay for the lookup many times over from the first repeat onward.

  **Rust side carries no flag.** When the Go flag is off, no `RetainAndReplay` / `Replay` opcodes are emitted, so the Rust handlers never fire — zero runtime cost on the Rust side. When the Go flag is on, the Rust handlers are reached via the opcode dispatcher with no extra branch.

  **Config surface.** Per CLAUDE.md the env knob is registered via `public/config/env`, not read directly:

  ```go
  // imzero2env package:
  var RetainKeepCache = env.NewBoolVar(env.Spec{
      Name:    "IMZERO2_RETAIN_KEEP_CACHE",
      Default: false,
      Help:    "Enable cross-frame Rust-side retention for .Keep()'d FFFI2 subtrees (ADR-0012 Phase 3).",
  })

  // bootstrap (e.g. egui2_lifecycle init or the application startup path):
  func init() { runtime.SetRetainKeepCacheEnabled(imzero2env.RetainKeepCache.Get()) }
  ```

  This yields three operating modes from one boolean: off (production default during rollout), on at startup (`IMZERO2_RETAIN_KEEP_CACHE=true`), and hot-toggle via `SetRetainKeepCacheEnabled` (tests, debug shells, a future `/debug/pprof/custom/toggle` handler). The atomic store-then-load ordering is sufficient for the hot-toggle case: a flip mid-frame either takes effect this frame or next; both states are valid (the false path is a strict superset of the true path's contract).

  **Rust restart resilience.** If Rust restarts mid-session while Go has the flag on, Rust's cache is empty and Go's `seenRetained` map is stale (Go thinks Rust has ids it doesn't). M3.3's eviction-sync register (`r_retainCacheMiss`) handles this for free — Rust signals miss, Go drops the affected ids and re-sends bytes the next frame. The same path covers steady-state LRU eviction.

**M3.1 — Rust-side cache and opcode handlers.**

- Add `retained_bytes_cache: lru::LruCache<u64, bytes::Bytes>` field to `ImZeroFffi`. Sized in bytes via the value type; default cap from M3.0.
- Implement handlers for `FuncProcIdRetainAndReplay` (`cache.put(id, bytes); replay(bytes)`) and `FuncProcIdReplay` (`cache.get(id) → replay(bytes); on miss, signal MISS via response-flags register`).
- Cache survives across logical frames; cleared only by LRU pressure or explicit shutdown.

**M3.2 — Go-side channel cache tracking.**

- `InlineIoChannel` gains a `seenRetained map[uint64]uint64` (id → last-seen-frame for staleness checks) protected by the channel's existing single-goroutine contract.
- Implement `(c *InlineIoChannel).SyncRetained(id uint64, buf []byte)` that:
  - On first-or-stale `id`: emit `FuncProcIdRetainAndReplay(id, len(buf), buf)` and record `seenRetained[id] = currentFrame`.
  - On already-seen `id`: emit `FuncProcIdReplay(id)`.
- Retire the `SendIntermediate` fallback in `Fffi2.SyncRetained` once M3.2 lands.

**M3.3 — Eviction sync (Rust → Go).**

- Add a `r_retainCacheMiss` response-flag register that Rust sets when `FuncProcIdReplay` arrives for an unknown id. Go drains it during the normal `StateManager.Sync` pass and drops the affected ids from `seenRetained`, so the next use re-transmits the bytes.
- The one-frame lag during a miss is acceptable — the affected widget pays one frame of "use stale id; Rust says miss; Go re-sends next frame." Rare by design (only happens under LRU pressure or a Rust restart).

**M3.4 — Documentation + sample.**

- Update `doc/skills/imzero2/SKILLS.md` to describe the `.Keep()` cross-frame contract and the eviction-fault behaviour. (Currently `SKILLS.md` describes `.Keep()` as marshal-once-splice-many — the new behaviour is a strict superset.)
- One worked example in [`doc/skills/imzero2/`](../skills/imzero2/) showing the imztop sparkline pattern: `.Keep()`'d once when sampler data changes, `.SyncRetained()` per frame in between.
- Update the FFFI2 widget-definition rules note (memory: [[feedback_fffi2_widget_definitions]]) to flag that `Retained=true` IDL widgets now carry cross-frame semantics in addition to compositional reuse.

**M3.5 — First consumer: imztop sparklines.**

- Convert imztop's per-history sparkline rendering from `.Send()` to `.Keep()`+stored-holder+`.SyncRetained()`, with the holder rebuilt only when the sampler bumps the history (1 Hz cadence).
- Smoke-test the result via the slow-frame logger from ADR-0049: expect `TabBody` to collapse to a small per-second pulse plus a ~12-byte/frame `Replay` opcode stream.
- Acceptance criterion: post-M3.5 `sync_us` p99 < frame budget (16.6 ms) on the same imztop scene that produces 30-43 ms today.

#### Order of work and gating

M3.0 lands as docs-only and is the one place the feature-flag contract is captured. M3.1 + M3.2 must land together (Rust receiving an opcode Go does not yet emit, or vice versa, would break the wire), but the M3.0 flag default of `false` makes them safe to merge in either order in practice: with the flag off, Go emits no new opcodes and Rust's new handlers never run. M3.3 lands after M3.2 so the miss path has a real signal to drain. M3.4 + M3.5 are documentation and first-application; either can land first depending on reviewer bandwidth.

Rollout sequence using the flag:

1. Merge M3.1 (Rust handlers + opcodes registered in the IDL; flag off → no opcodes arrive).
2. Merge M3.2 (Go channel-level cache; flag default off → `SyncRetained` still routes to `SendIntermediate`).
3. Set `IMZERO2_RETAIN_KEEP_CACHE=true` on a dev workstation; verify imztop's slow-frame log (ADR-0049's `top_scopes` field) shows `TabBody` collapsing.
4. Merge M3.3 (miss-sync); confirm a deliberate `SetRetainKeepCacheEnabled(false) → true` mid-process doesn't strand orphaned ids.
5. Merge M3.4 + M3.5 (docs + imztop consumer).
6. After field experience, flip the M3.0 default from `false` to `true` in a small follow-up commit (one-line change). Operators who hit a problem can flip it back via env var without a rebuild.

#### Relationship to Phase 2 (retained collapsible bodies)

Phase 2's `WithRetainedBlock(name, keyType)` is **widget-id-keyed**; Phase 3's `.Keep()` retention is **content-hash-keyed**. They sit on the same Rust-side caching foundation but solve different problems:

- Phase 2: "this open/closed body's contents are stable as long as the open key matches" (collapsibles, dock tab bodies, scroll panes).
- Phase 3: "this subtree's *bytes* are stable; replay them by id" (sparklines, static labels, anything `.Keep()`'d).

A widget body may use both (Phase 2 for the structural gate, Phase 3 for the data inside). The implementations are independent; the LRU cache from M3.1 can serve both retainers.

## Status

Proposed — awaiting review by ImZero2 maintainers.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## References

- Current gate template: [`fffi2_compiletime_go_server.go:682-692`](../../src/go/public/thestack/fffi2/compiletime/goserver/fffi2_compiletime_go_server.go).
- Generated gate sites: [`components/methods.out.go`](../../src/go/public/thestack/imzero2/egui2/bindings/methods.out.go) (~24 `KeepIter` functions).
- Rust-side block-skip handlers: [`interpreter.rs:2700`](../../src/rust/src/imzero2/interpreter.rs) (CollapsingHeader), [`:3086`](../../src/rust/src/imzero2/interpreter.rs) (Window).
- Persistent-state catalogue: [`interpreter.rs:1469`](../../src/rust/src/imzero2/interpreter.rs) (`ImZeroFffi` fields), [`:1691`](../../src/rust/src/imzero2/interpreter.rs) (`prepare_next_frame`).
- Capture infrastructure and iter-scope idiom: [`SKILLS.md` §5](../skills/imzero2/SKILLS.md).
- Frame-table for current gate semantics: [`SKILLS.md` §13.4](../skills/imzero2/SKILLS.md).
- Demo shell stall workaround tied to current gate: [`demo/widgets/interactive_driver.go:19-29`](../../src/go/public/thestack/imzero2/egui2/demo/apps/widgets/interactive_driver.go).
- Related ADRs: [ADR-0052 (unified color, deferred-block invariants)](./0003-imzero2-unified-color-type.md), [ADR-0057 (demo registry / drivers — origin of the ~11s stall context)](./0008-demo-registry-and-drivers.md), [ADR-0058 (scrolling-texture, persistent texture cache as precedent for id-keyed Rust-side state)](./0009-imzero2-scrolling-texture-widget.md).
