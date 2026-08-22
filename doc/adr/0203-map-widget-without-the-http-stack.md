---
type: adr
status: proposed
date: 2026-08-22
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0203: a map widget without the HTTP stack

## Context

[ADR-0056](./0056-walkers-map-h3-binding.md) binds the `walkers` crate as the
basemap widget. Since then the binding has moved steadily away from the crate:
`walkers::HttpTiles` is no longer used at all, `imzero2::walkers_tiles::BasemapTiles`
implements walkers' public `Tiles` trait and is the only tile client, it owns the
LRU and a process-wide six-worker download pool, and
[ADR-0165](./0165-imzero2-tile-transport-over-fffi2.md) proposes moving the fetch
itself into Go. What remains of the crate is the `Map` widget: projection,
pan/zoom, tile placement, and the `Position`/`Tiles`/`Projector` types.

A dependency audit measured what that widget costs. The finding that prompts this
ADR is that the cost is not the widget.

**walkers has no way to opt out of its HTTP client.** In 0.56, 0.57 and 0.58
alike, `reqwest` (with `rustls-tls` hard-coded), `reqwest-middleware`,
`http-cache-reqwest` and `tokio` are declared **non-optional**; the only feature
keys are `default`, `mvt`, `pmtiles` and `serde`, none of which gates HTTP.
Verified against the crates.io index rather than the vendored copy. This extends
the [ADR-0056 §SD12–SD15](./0056-walkers-map-h3-binding.md) finding that walkers
exposes no TLS *seam*: it has no TLS *switch* either.

What the chain costs the client today:

| Lens | Figure |
| --- | --- |
| Source closure | 40 crates, 131,854 code lines, 6,931 branch points — 3.1% of the client's third-party branch mass |
| Upstream owners | 24, of which **17 are named in no manifest in this tree** |
| Build-time code execution | 4 crates run build scripts (`anyhow`, `generic-array`, `serde_json`, `zmij`) |
| Shipped machine code | **1,057,461 bytes — 5.5% of the client's text** |

The shipped bytes break down as `rustls` 331,932, `reqwest` 112,552,
`hyper-util` 96,778, `ring` 95,973, `tokio` 91,418. **walkers' own widget code is
13,826 bytes.** The transport is 76× the size of the thing it is there to serve.

**This corrects what ADR-0165 claims to deliver.** That ADR says `HttpTransport`
is retired "along with the renderer's `reqwest` and TLS knobs". It retires the
renderer's *own* `reqwest` dependency — declared directly in imzero2's manifest
with `rustls-tls` and `blocking` — and should let the linker drop most of the
1.06 MB. It does not remove the dependency: walkers keeps pulling the chain into
the build graph, so the 40 crates, 24 owners and 4 build scripts survive the move.
ADR-0165 buys shipped bytes; it does not buy supply surface.

**What replacing the widget would cost is now measured too.** walkers is 2,858
code lines. The raster-map core this tree needs is ~1,214 of them — 337
statements, 73 branch points, 105 functions. The remainder is MVT vector tiles
(1,115 lines, feature-gated and unused), the HTTP client (712, already replaced),
PMTiles (109) and local tiles (80). Per module:

| Module | Statements | Branch points |
| --- | --- | --- |
| `tiles.rs` — pyramid assembly and fallback | 86 | 18 |
| `map.rs` — widget, input, draw | 73 | 24 |
| `projector.rs` | 44 | 1 |
| `mercator.rs` | 40 | 0 |
| `center.rs` — pan, zoom, inertia | 34 | **25** |
| `position.rs` | 27 | 0 |
| `zoom.rs` | 23 | 4 |
| remainder | 10 | 1 |

For scale against code this tree already maintains: `svgexport.rs` is 834
statements and 219 branch points, `scrolling_texture.rs` 150 and 42, `image.rs`
86 and 37. The whole map core is smaller than the SVG exporter.

The drawing primitives a replacement needs already exist in the client.
[`image.rs`](../../rust/imzero2/src/imzero2/image.rs) is a Go-driven texture
registry — `ensure(…) -> TextureId`, `release(id)`, `attach_texture_cache` — and
[`scrolling_texture.rs`](../../rust/imzero2/src/imzero2/scrolling_texture.rs)
already draws with `egui::Mesh` and explicit per-vertex UVs, and with
`painter.image` over UV sub-rects ([ADR-0058](./0058-imzero2-scrolling-texture-widget.md)).
A raster map is that primitive generalised: N textured quads with UV rects at
computed screen positions.

This is the same shape as [ADR-0202](./0202-retire-arrow-parquet.md), where three
call sites were holding a subtree worth 29.5% of the Go closure. Here one widget
holds a transport stack worth 5.5% of the client's machine code.

## Design space (QOC)

**Question.** How should the basemap widget stop dragging an HTTP and TLS stack
into the render client?

**Options.**

- **O1 — Status quo.** Keep walkers as it is; the chain stays.
- **O2 — Upstream a feature gate.** Make `reqwest`, `reqwest-middleware`,
  `http-cache-reqwest` and `tokio` optional behind a default-on `http-tiles`
  feature, so `default-features = false` yields the widget alone. `HttpTiles` is
  already a self-contained module and the `Tiles` trait this tree implements is
  already public, so the change is additive and default-preserving for every
  other user.
- **O3 — Vendor a patched walkers.** Carry O2's patch locally, as this tree
  already does for `egui_software_backend`, whose `VENDORING.md` records the
  upstream commit, the deltas and the reasoning.
- **O4 — Reimplement the map core in Rust,** inside imzero2: ~1,214 lines, no
  IDL change.
- **O5 — Reimplement with the state in Go.** Projection, visible-tile selection,
  pyramid fallback and pan/zoom live in Go; Rust keeps the texture registry and
  gains one draw op that takes a per-frame tile draw list.

**Criteria.**

- **C1 — Removes the supply surface** — the 40 crates, 24 owners and 4 build
  scripts, not just the bytes.
- **C2 — Removes shipped bytes** — the 1.06 MB, conditional on ADR-0165 for
  the renderer's own client.
- **C3 — Independent of anyone else's timeline.**
- **C4 — Code this tree owns and tests indefinitely.**
- **C5 — Risk of behaviour regression** — pan/zoom feel, tile fallback, the
  details walkers has already debugged.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | −− | ++ | ++ | ++ | ++ |
| C2 | −− | ++ | ++ | ++ | ++ |
| C3 | ++ | −− | +  | ++ | ++ |
| C4 | ++ | ++ | +  | −  | −  |
| C5 | ++ | ++ | ++ | −  | −− |

O2 is the only option that reaches C1 and C2 without paying C4 or C5 at all, and
it is worth doing for its own sake — every downstream user of walkers gets the
same relief. Its single weakness is C3, and O3 is exactly the bridge for that.
O4 is dominated by O5 on architecture fit and by O2/O3 on cost: it buys the same
independence as O5 while putting stateful UI logic in the artifact this tree
keeps deliberately thin, and it needs no IDL change only because it declines the
split that would make the logic testable from Go.

## Decision

*Proposed, not implemented.* Sequence rather than choose:

1. **Upstream the feature gate (O2)**, carrying it locally until it lands (O3).
   Two mechanisms are already present: the manifest's `[patch.crates-io]`
   section, which exists but holds only commented examples, and the
   `vendor/` + `path =` form used for `egui_software_backend`, which comes with
   a `VENDORING.md` discipline the patch should inherit. Behaviour is unchanged;
   the widget is unchanged; `BasemapTiles` continues to be the only tile
   client.
2. **Land [ADR-0165](./0165-imzero2-tile-transport-over-fffi2.md) independently.**
   O2 removes walkers' client, ADR-0165 removes the renderer's own. Neither
   alone gets the tree to zero HTTP in the render client; both together do.
3. **Hold O5 as the recorded fallback** if the upstream change stalls. It is
   costed here so that the decision to reimplement, if it comes, is made against
   numbers rather than frustration.

### Subsidiary design decisions

- **SD1 — The Go-facing surface does not change under any option.** The
  `walkersMap` opcode, the overlay drain ([ADR-0056 §SD2](./0056-walkers-map-h3-binding.md)),
  the per-id camera register, and the `.TileUrl` / `.TileAttribution` /
  `.TileMaxZoom` / `.TileSize` methods are the contract. Whichever option runs,
  Go code that draws a map is untouched. This is what makes the sequence
  reversible: O2 and O5 are indistinguishable from above.
- **SD2 — O5's wire form is one op carrying a draw list, not N draw ops.** A
  frame shows 20–60 tiles. Sixty separate opcodes per map per frame is a
  measurable tax on a stream that is already the frame's bottleneck; one op
  carrying an array of `(texture id, screen rect, uv rect)` matches the shape
  the mesh and atoms paths already use. The per-frame op budget is a thing to
  measure before committing, not to assume — see Q2.
- **SD3 — Under O5 the texture registry stays in Rust, keyed by tile id.**
  Textures are GPU resources with a lifetime the render loop owns.
  [`image.rs`](../../rust/imzero2/src/imzero2/image.rs) already provides
  `ensure`/`release` against a caller-supplied id; the map needs no second
  mechanism, and Go's byte cache (ADR-0165 Q1) sits above it rather than
  replacing it.
- **SD4 — ADR-0056's walkers-shaped decisions fall only under O4/O5.**
  Its §SD4 (the `Plugin<'p>` lifetime, pinning walkers ≥ 0.53), §SD6
  (`Box::leak`'d `&'static str` attribution, which exists because walkers'
  `Attribution` demands it) and §SD11's `HttpTiles` rebuild path are artefacts
  of the crate. Under O2/O3 they stand unchanged. Recorded so a later reader
  knows which of ADR-0056's subsidiary decisions are ours and which are
  walkers'.
- **SD5 — `center.rs` is the risk, not the projection math.** 25 branch points
  in 34 statements is the highest branch density in the crate, and it is
  pan/zoom/inertia edge cases. The projection modules — `position`, `mercator`,
  `projector` — carry 1 branch point across 111 statements and port to any
  language mechanically. A reimplementation that budgets by line count will
  mis-estimate; the work is concentrated in 43 branch points across two modules.

### Milestones

- **M0 — Verify the byte claim.** Build the client with the tile client removed
  and re-read its symbol table with the [ADR-0173 §SD8](./0173-code-volume-self-inspection.md)
  lens. The 1.06 MB is expected to fall to near zero under dead-code
  elimination; that expectation is unverified and cheap to settle.
- **M1 — Upstream patch (O2).** One manifest change plus `#[cfg(feature)]` on
  the `http_tiles` and `io` modules.
- **M2 — Carry it locally (O3)** until M1 lands, and re-measure the four figures
  in the Context table.
- **M3 — Only if M1 stalls: O5.** Prototype the draw-list op against the
  existing texture registry before moving any projection code.

### Open questions

- **Q1 — SVG export.** [`svgexport.rs`](../../rust/imzero2/src/imzero2/svgexport.rs)
  and the headless lane would need to handle textured quads for an O5 map to
  survive export. Whether they do today — for the current map, let alone a new
  one — is unchecked, and it may already be a gap.
- **Q2 — Per-frame op budget under O5.** 20–60 tiles per map per frame against a
  16 ms budget on a stream that already carries the whole widget tree. SD2
  proposes one op; the measurement has not been done.
- **Q3 — Pan/zoom parity.** There is no test today that would catch a
  regression in map feel. Whether the headless driver can assert on camera state
  after a synthetic drag decides whether O4/O5 are testable at all.
- **Q4 — Does upstream want the patch?** Unknown. The change is additive and
  default-preserving, which is the shape most maintainers accept, but the answer
  determines whether O3 is a two-week bridge or a permanent fork.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `imzero2` Cargo manifest | `walkers` gains `default-features = false` (O2/O3); direct `reqwest` removed (with ADR-0165) | `Cargo.lock`; the airgap bundle's crate set |
| egui2 IDL | unchanged under O2/O3; one draw-list op added under O5 | regenerated dispatch on both sides; `SKILL.md` |
| `walkersMap` opcode and overlay contract | unchanged under every option (§SD1) | nothing |
| `BOXER_MAP_TILE_CA_FILE` / `BOXER_MAP_TILE_INSECURE_TLS` | retired by ADR-0165, not by this ADR | `doc/env-vars.md` regeneration |

Untouched: the H3 binding, the camera fetcher, the overlay node types, and every
Go call site that draws a map.

## Alternatives

- **O1 — Leave it.** Zero cost today, and the widget works. Killed as a
  destination for the same reason ADR-0165 killed its own O1: the cost is not
  the bytes but the 17 unnamed owners and 4 build scripts sitting behind a
  13.8 KB widget, and nothing about that improves on its own.
- **O4 — Reimplement in Rust only.** Same independence as O5, same ~1,214 lines
  to own, but it keeps projection, tile selection and pan state in the render
  client instead of moving them to where the rest of this tree's state lives —
  and therefore keeps them untestable from Go.
- **Replace walkers with a larger mapping crate.** ADR-0056 §O3 already
  considered Galileo and rejected it on scope; nothing in this ADR changes that
  assessment, and a bigger crate would enlarge exactly the surface this ADR
  exists to shrink.
- **Accept the HTTP stack and use it.** Reverting to `walkers::HttpTiles` would
  make the dependency load-bearing again and undo the convergence onto one tile
  client ([ADR-0056, 2026-08-05](./0056-walkers-map-h3-binding.md)). It also
  reinstates the TLS problem those updates were written to solve.

## Consequences

### Positive

- Under O2/O3: 40 crates, 24 owners and 4 build scripts leave the client's
  dependency graph for a manifest change, with no behaviour change and no code
  this tree has to maintain.
- Together with ADR-0165, the render client reaches zero network dependencies —
  the property ADR-0165's Context claims for it, which this ADR shows is not
  reached by ADR-0165 alone.
- The measurement generalises: `default-features = false` on a widget crate is
  worth checking wherever one is bound, and this is the second subtree in a
  month (after [ADR-0202](./0202-retire-arrow-parquet.md)) whose cost sat far
  above its usefulness.

### Negative

- O2 puts this tree on someone else's release schedule; O3 is the mitigation and
  is itself a maintenance item.
- O5, if it comes, adds ~1,214 lines of widget code and an IDL op, and the
  43 branch points in `tiles.rs` and `center.rs` are where map widgets are
  historically wrong in ways users notice before tests do.
- Any reimplementation re-opens details walkers already settled — antimeridian
  handling, zoom clamping, tile wrap, high-DPI, inertia feel — none of which
  appear in a line count.

### Neutral

- The Context figures are one measurement of one build on one host. They are
  reproducible from the tree (`cargo tree`, a symbol-table read, and a line
  count) and should be re-taken rather than trusted after any dependency bump.
- walkers' own code is 13,826 bytes of the client. Whatever happens to the
  transport, the widget itself was never the weight.

## Migration — Tier 1

- **Breaks.** Nothing under O2/O3 — the change is a manifest flag and the
  widget's behaviour is identical. Under O5, nothing Go-facing (§SD1).
- **Path.** Nothing to migrate. A tree already on `BasemapTiles` — which is the
  only state since [ADR-0056, 2026-08-05](./0056-walkers-map-h3-binding.md) —
  needs no source change for O2.
- **Regeneration.** None under O2/O3. Under O5, `app egui2gen generate` for the
  new op, and `doc/env-vars.md` if ADR-0165 lands in the same window.
- **Old shape.** `walkers::HttpTiles` is already unused; O2 makes that
  structural instead of conventional.

## Verification plan — Tier 1

- **Lane.** `cargo build` for the manifest change; the existing headless map
  scene for behaviour.
- **What would fail.** After O2/O3, `cargo tree -e normal` naming `reqwest`,
  `rustls`, `hyper` or `tokio` anywhere under the client — the check is a grep
  over `cargo tree` output and belongs in the Rust lane, which today has no CI
  gate ([ADR-0173](./0173-code-volume-self-inspection.md) records the same gap
  for the Rust artifact generally). A symbol-table read that still finds
  `rustls` text after M0 would mean dead-code elimination is not doing what M0
  assumes.
- **Gap.** Map feel is not gated by anything today (Q3). Under O2/O3 that is
  acceptable because the widget does not change; under O4/O5 it is the first
  thing that needs building, and this ADR should not be flipped to accepted with
  O5 in scope until Q3 has an answer.

## Status

Proposed — 2026-08-22. No implementation. Supersedes nothing;
[ADR-0056 §SD4, §SD6, §SD11](./0056-walkers-map-h3-binding.md) would be
superseded only under O4/O5 (§SD4 above), and
[ADR-0165](./0165-imzero2-tile-transport-over-fffi2.md) is corrected rather than
superseded — its Decision claims a dependency removal it does not deliver alone.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.

## References

- [ADR-0056 — walkers map and H3 binding](./0056-walkers-map-h3-binding.md) — the binding, its TLS updates, and the convergence onto one tile client.
- [ADR-0165 — imzero2 tile transport over FFFI2](./0165-imzero2-tile-transport-over-fffi2.md) — the fetch move this ADR sequences with and corrects.
- [ADR-0058 — imzero2 scrolling texture widget](./0058-imzero2-scrolling-texture-widget.md) — the existing textured-quad drawing path.
- [ADR-0128 — imzero2 mesh draw stream codec lane](./0128-imzero2-mesh-draw-stream-codec-lane.md) — the draw-stream shape an O5 draw list would sit beside.
- [ADR-0173 — code-volume self-inspection](./0173-code-volume-self-inspection.md) — the symbol-table and owner lenses the Context figures were taken with.
- [ADR-0202 — retire arrow-go's parquet packages](./0202-retire-arrow-parquet.md) — the same pattern on the Go side.
- [`rust/imzero2/src/imzero2/walkers_tiles.rs`](../../rust/imzero2/src/imzero2/walkers_tiles.rs) — `BasemapTiles`, `TileTransport`.
- [`rust/imzero2/src/imzero2/image.rs`](../../rust/imzero2/src/imzero2/image.rs) — the texture registry §SD3 reuses.
- [`doc/skills/imzero2-fetchers/SKILL.md`](../skills/imzero2-fetchers/SKILL.md) — the `Sync()`-only fetcher rule an O5 request path must fit.
