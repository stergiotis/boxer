---
type: adr
status: accepted
date: 2026-08-17
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-17
---

# ADR-0194: retire the `egui-snarl` node-editor binding

## Context

[ADR-0021](./0021-imzero2-snarl-node-editor-binding.md) (accepted 2026-05-04)
bound the `egui-snarl` crate as imzero2's node editor, choosing a crate binding
(its O1) over a ground-up port (its O4, rejected as an order of magnitude more
work). The binding was built: an IDL block, three register-drain accumulators,
a deferred-block map for node bodies, a `SnarlViewer` delegate, and an event
fetcher.

Two things have changed since.

**The binding does not work.** ADR-0021's own dated Update of 2026-07-31 records
the measurement: wires are emitted as shapes but never reach the framebuffer;
pins are missing entirely on pin-only nodes; node frames are far wider than
their content, so declared spacing overlaps; text renders at roughly 1.7× the
surrounding UI; drag pointer-delta does not match model-delta, with a different
ratio per axis; hit-tests land on the wrong rects. Two suspects are named there
and both remain open — `apphost.rs` pins `max_passes = 1` while snarl's fitup
uses `request_discard`, and `render_snarl_editor` carries a "KNOWN ISSUE" that
snarl's `Scene` + `set_sublayer` painting never reaches the framebuffer.
Diagnosis was deferred at the time and has not been picked up.

**Nothing consumes it.** The binding has zero call sites outside its own gallery
demo. This is the discriminator against the `egui_plot` precedent
([ADR-0149](./0149-implot-core-port-painter-lane.md), whose bridge had roughly
eight call sites and needed a sequenced migration): there is no migration to
sequence here, so the binding can be removed without anything moving first.

The [port analysis](../adr-background-work/snarl-port-analysis.md) written
2026-07-31 costed a Go rebuild on the painter lane at roughly 1.8–2.8k lines and
recommended, explicitly, that the binding's fate be decided *independently* of
the port — precisely because it has no users. That recommendation is what this
ADR acts on. The analysis also found M0 of any such port would be empty: the
painter lane already carries cubic bezier, clip stack, batched rects and
markers, sense regions, anchored wheel zoom, per-canvas pointer, text
measurement and `AllocateUiAtRect`, and `widgets/layeredgraph/view` already
implements most of a read-only M1.

Carrying cost as measured on the working tree at the date of this ADR: 299 lines
of IDL plus 8 lines of type constructors, 85 lines of hand-written Go bindings
plus ~30 lines of state-manager plumbing, a 353-line demo, ~520 lines of
hand-maintained Rust in `interpreter.rs` (486 in two snarl-only spans, the rest
retained-state fields, their constructor inits and a per-frame clear), the
generated Go and Rust surfaces those produce, and one crate dependency.

## Decision

We remove the `egui-snarl` binding in full: the IDL definition, the generated Go
and Rust surfaces, the hand-maintained Rust state and viewer, the gallery demo,
the Go state-manager plumbing, and the crate dependency. imzero2 has no
node-editor affordance after this change, and none is scheduled. If a consumer
appears, the answer is a Go port on the painter lane per the port analysis — not
a re-binding.

### SD1 — removal, not deprecation-in-place

No deprecation window and no shim. A deprecation window exists to give consumers
time to migrate; there are no consumers. Leaving a broken widget behind a
deprecation marker keeps every line of the carrying cost and adds a marker to
maintain.

### SD2 — opcode ids renumber; both sides regenerate together

The IDL registry is sorted by name before ids are assigned, so removing the
`snarl*` nodes shifts every `FuncProcId` that sorts after them. This is a wire
change across the FFFI2 frame boundary: the Go bindings and the Rust interpreter
must be regenerated and rebuilt **together**, and a Go binary from before the
change will not interoperate with a Rust host from after it. Nothing persists
these ids across a build, so no stored data is affected. The ids are not a
durable declared contract in the sense of the static-explicit rule — they are
derived from the registry each regen — so renumbering is a rebuild obligation,
not a migration.

### SD3 — generic machinery stays

Two pieces of Rust were introduced or shaped by snarl but are not snarl-specific
and are kept:

- `svgexport.rs`'s handling of layers carrying a non-identity `TSTransform`.
  It is written against `Context::set_transform_layer` in general, and removing
  it would silently regress SVG export for any future transform-layer caller.
  The comment loses its `egui_snarl` example; the code is untouched.
- `apphost.rs`'s `max_passes = 1` note. The pin is a property of the FFFI stream
  being consumed by pass 1, not of snarl; snarl was the caller that made it
  visible. The comment is rewritten to state the constraint without the example.

### SD4 — the port analysis is retained as background work

`doc/adr-background-work/snarl-port-analysis.md` keeps its findings as written.
Background work is a dated snapshot, not a document maintained against the tree,
so the analysis is not rewritten to match the removal — this ADR is the pointer
that tells a reader the binding described there is gone. It remains the starting
point if a node editor is needed again.

Its three links to the removed source files *are* repaired, to code spans plus a
pointer here. That is not a content edit but a CI obligation: doclint treats a
dangling local link as an error, so leaving them would fail the lane on a clean
checkout. The same repair applies to the one such link in ADR-0021.

### SD5 — the transform-layer gap is not addressed here

The port analysis identified one real substrate gap: egui2 has no
Scene/transform-layer binding, so real widgets cannot be scaled. Removing the
snarl binding neither closes nor worsens that gap. It is named here so a future
reader does not mistake this ADR for having handled it.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| egui2 IDL (`definition/egui2_definition_d_snarl.go`) | removed — the whole file, 4 type constructors in `_d_types.go`, 3 registration lines | the generated Go and Rust surfaces |
| `FuncProcId` / method-id enums (FFFI2 wire) | `SnarlConnection`/`SnarlEditor`/`SnarlNode`/`SnarlPin`/`FetchSnarlEvents` removed; **all later ids shift** (SD2) | both sides of the FFI boundary, rebuilt together |
| `bindings` (exported Go API under `public/`) | removed — `Snarl*` builders, event/enum types, `GetSnarlEvents`, the fetch-and-decode in `Sync` | any out-of-tree caller (none known) |
| `rust/imzero2` hand-maintained region | removed — accumulator structs, `SnarlState`, `SnarlPinMeta`, `FffiSnarlViewer`, `render_snarl_editor`, 5 state fields, per-frame clear | nothing |
| `rust/imzero2/Cargo.toml` | removed — `egui-snarl = "0.11.0"` | `Cargo.lock` |
| gallery demo | removed — `egui2_hl_snarl_demo.go` and its registry entry | the demo tour's screenshot set |
| `doc/skills/imzero2/assets/egui2_api_reference.md` | regenerated — snarl entries drop out | nothing |
| `svgexport.rs` transform handling | **unchanged** — comment only (SD3) | nothing |
| `apphost.rs` `max_passes` pin | **unchanged** — comment only (SD3) | nothing |
| register-drain pattern, deferred-block maps | **unchanged** — still used by table, tree, graph, dock | nothing |
| `widgets/layeredgraph`, `pipelineview`, `fsmview` | **unchanged** — independent widgets that never used the binding | nothing |

## Alternatives

- **O1 — fix the binding.** Diagnose the two open suspects, then work through
  pins, geometry scaling and hit-testing. Rejected: the defect list spans
  emission, layout and input, which is a rewrite of the viewer rather than a
  bug fix, and it buys a widget nothing asks for. The `Scene` fault in
  particular is the same substrate gap a port would have to solve anyway.
- **O2 — leave it in place, broken.** Zero effort today. Rejected: it is a
  working-looking widget in the gallery that does not work, which costs a
  contributor an afternoon to discover, and it keeps ~2100 lines and a crate
  alive across every regen and dependency bump.
- **O3 — port to Go now.** Rejected as premature, not as wrong: 1.8–2.8k lines
  for a widget with no consumer. The port analysis explicitly separates this
  question from the binding's fate, and this ADR does not decide it.
- **O4 — remove now, port if needed (chosen).** Takes the carrying cost to zero
  and leaves the port analysis as the re-entry point.

## Consequences

### Positive

- ~2100 lines and one crate leave the tree, along with the generated surfaces
  they produce on both sides of the FFI boundary.
- The gallery stops shipping a widget that renders incorrectly.
- One fewer crate tracking egui version bumps, and one fewer to carry in the
  airgapped bundle.

### Negative

- imzero2 has no node-editor affordance. Any consumer that appears pays the port
  cost with nothing partial to build on beyond `layeredgraph/view`.
- The IDL work in the removed definition — the accumulator shapes, the pin
  packing, the reconciliation modes (ADR-0021 SD6/SD7) — survives only as
  ADR-0021's prose and in git history. That is deliberate: the design record is
  the ADR, not the code.
- The opcode renumbering (SD2) invalidates any recorded FFFI2 stream captured
  before the change. Recordings are debugging artefacts, not persisted data, so
  the practical cost is that an old capture cannot be replayed against a new
  host.

### Neutral

- ADR-0021 is superseded, not deleted; its design space and kill-reasons remain
  the record of why a binding was chosen over a port in 2026-05, and the port
  analysis records why the reverse now looks right.

## Migration — Tier 1

- **Breaks.** The exported `Snarl*` Go API under `public/` disappears. No
  in-tree caller exists; an out-of-tree caller would not compile.
- **Path.** No staged path and no shim, per SD1. The removal is one change.
- **Regeneration.** Yes, and both sides of the FFI boundary need rebuilding
  together (SD2). Order matters: `egui2gen` type-checks the bindings package
  before writing, so the hand-written Go plumbing must be removed *before* the
  regen, and the hand-maintained Rust region likewise — the same ordering
  ADR-0149's 2026-07-30 (5) Update recorded for the `egui_plot` bridge.
  Sequence: hand-written Go → hand-maintained Rust → IDL → `./generate.sh` →
  `Cargo.toml`/lock → build both.
- **Old shape.** Nothing replaces it. A future node editor is a Go port on the
  painter lane, starting from the port analysis, and would be its own ADR.

## Verification plan — Tier 1

- **Lane.** Default `go build`/`go test` with the repo tags for the Go side;
  `cargo build` for the Rust host; the demo-registry screenshot tour for the
  gallery.
- **What would fail.**
  - **The regen is incomplete.** `egui2gen` type-checks the bindings package
    before writing, so a leftover hand-written reference to a removed generated
    symbol fails the generator rather than the compiler — that is the intended
    first tripwire.
  - **The two sides disagree on ids.** A Go binary and a Rust host built across
    the change would mis-dispatch every opcode after the removed range. Nothing
    detects this automatically; it is why SD2 states the rebuild obligation.
    The check is that both artefacts come from one build.
  - **A dangling demo registration.** The demo registry entry and the file are
    removed together; a leftover entry fails to compile.
  - **Documentation links.** doclint treats a dangling local link as an error,
    so a doc still linking a removed file fails CI on a clean checkout.
- **Gap.** No test asserts the *absence* of the binding, and none is added — a
  removal is verified by the build, not by a guard. The screenshot tour's set
  shrinks by one; that difference is reviewed by eye, not asserted.

## Status

Accepted 2026-08-17. All four milestones landed in one commit the same day
(`df4fdfb6`), so the decision and its execution carry the same date.

- **M0 — hand-written surfaces.** ✓ Go bindings and state-manager plumbing, the
  demo, and the hand-maintained Rust region and its state fields.
- **M1 — IDL and regen.** ✓ Definition file, type constructors and registration;
  regenerated via the three `egui2gen generate` steps of `./generate.sh` rather
  than the whole script, to leave unrelated in-flight codegen untouched; both
  sides rebuilt.
- **M2 — dependency.** ✓ Crate dropped; `Cargo.lock` refreshed by the build.
- **M3 — docs.** ✓ ADR-0021 flipped to superseded with a dated Update; the
  fetchers skill, airgap how-to and bundle script corrected; T3-013 voided;
  comment-only edits per SD3.

Two things worth recording from the execution, both of which the Verification
plan anticipated only in part:

- **The `impl` block is shared.** `render_snarl_editor` sits inside an
  `impl ImZeroFffi` block that also holds `drain_paint_cmds_to_painter` and
  `render_new_table`. Deleting banner-to-block-close takes all three; the build
  caught it as three `E0599`s. Delete the function's own line range instead.
  The Context figure above is the corrected one; the first measurement read
  ~1210 lines because it counted the whole shared block.
- **Warning count is a usable orphan check.** `cargo build` warnings went 88 →
  86 across the removal, confirming no import or helper was left dangling.

Measured result: 27 files, 569 insertions, 2313 deletions. `go build`/`go test`
with the repo tags, `cargo build`, and `doclint` (zero errors) all clean; no
module drift.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0021](./0021-imzero2-snarl-node-editor-binding.md) — the binding this
  supersedes, including the 2026-07-31 Update that measured it broken.
- [ADR-0149](./0149-implot-core-port-painter-lane.md) — the crate-to-Go-port
  precedent; its 2026-07-30 (5) Update records the removal mechanics reused here.
- [snarl port analysis](../adr-background-work/snarl-port-analysis.md) — the
  costed Go rebuild, and the recommendation this ADR acts on.
- [ADR-0012](./0012-imzero2-collapsible-retained-bodies.md),
  [ADR-0013](./0013-imzero2-stateful-widget-contract.md) — the deferred-body and
  stateful-widget contracts the binding cooperated with; both unaffected.
