---
type: adr
status: superseded
date: 2026-08-22
superseded-by: ADR-0204
superseded-date: 2026-08-27
---

> **Superseded by [ADR-0204](./0204-leaflet-map-core-port.md) (2026-08-27).** Never accepted, never implemented. The Decision below — carry a patched `walkers` to gate its HTTP client — is replaced by the port, whose M4 removed `walkers` and `reqwest` outright (ADR-0204 Q2 records what is left of O2/O3). The Context's closure and musl measurements are the analysis the port was decided on and they stand, including the re-measurement after M4 recorded there.

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
alike, and on upstream's master branch at the time of writing, `reqwest` (with
`rustls-tls` hard-coded), `reqwest-middleware`, `http-cache-reqwest` and `tokio`
are declared **non-optional** (as is `futures`, which only the same async I/O
uses); the only feature keys are `default`, `mvt`, `pmtiles` and `serde`, none
of which gates HTTP. Verified against the crates.io index and the upstream
repository rather than the vendored copy. This extends
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

**Two owners, not one — and the figures split along that line.** The chain has
two roots in the manifest: walkers' non-optional dependencies, and imzero2's own
`reqwest = { features = ["rustls-tls", "blocking"] }` line, declared for
`HttpTransport` in `walkers_tiles.rs`. A measurement of the closure with walkers'
HTTP dependencies feature-gated — a scratch copy of the crate with the five
dependencies behind a default-on feature, `cargo tree -e normal` diffed per build
profile — puts numbers on each root:

| Change | desktop (`default`) | `headless` / `headless_soft` |
| --- | --- | --- |
| walkers gated, imzero2's `reqwest` line kept (O2 alone) | −40 crates, −4 build scripts — the figures in the table above | −43 crates, −4 build scripts (`anyhow` stays via `prost-derive`; `rustix` leaves) |
| walkers gated *and* imzero2's `reqwest` line removed (O2 with ADR-0165) | −74 crates, −7 build scripts | −99 crates, −8 build scripts |

The 40 crates are the HTTP *cache* layer and the middleware — `cacache`, `ssri`,
`sha1`/`sha2`/`digest`, `miette`, `time`, `serde_json`, `bincode`, `tempfile`,
`walkdir`, `reqwest-middleware`, `futures`. `reqwest`, `hyper`, `rustls`, `ring`
and `tokio` are in neither column of the first row: imzero2's own line keeps
every one of them, and they leave only when that line goes too. Under both
changes `tokio` leaves the desktop build but stays in every headless build, where
imzero2's own WebSocket carrier enables it — so "zero network dependencies in the
render client" is a desktop statement. The shipped bytes above are live today
because `HttpTransport` uses them, not because walkers does; walkers' copy of the
same crates is what survives dead-code elimination as supply surface.

**This corrects what ADR-0165 claims to deliver.** That ADR says `HttpTransport`
is retired "along with the renderer's `reqwest` and TLS knobs". It retires the
renderer's *own* `reqwest` dependency and should let the linker drop most of the
1.06 MB. It does not remove the dependency: walkers keeps pulling the chain into
the build graph, so the 40 crates, 24 owners and 4 build scripts survive the move
— and so do `reqwest`, `rustls`, `ring`, `hyper` and `tokio` themselves.
ADR-0165 buys shipped bytes; it does not buy supply surface. Symmetrically, this
ADR's O2 alone buys the cache layer; it does not buy the TLS stack. Neither alone
reaches it; both together do.

**The chain is also half of what blocks a static musl build.**
[ADR-0128](./0128-imzero2-mesh-draw-stream-codec-lane.md) defers a musl-static
appliance target. `cargo check --target x86_64-unknown-linux-musl
--no-default-features --features headless` on a host with the musl `std` but no
musl C compiler checks 340 of the closure's 348 crates clean — `tokio`,
`tungstenite`, the egui ring, `blake3` (which falls back to intrinsics) — and
fails exactly two build scripts, both with *tool not found:
`x86_64-linux-musl-gcc`*: `ring`, reached through `rustls` from both roots
above, and `libmimalloc-sys`, imzero2's own global allocator; the eight crates
it does not reach are the ones downstream of those two. Nothing in the
closure needs `cmake`, `pkg-config` or `bindgen`; `ring` and `libmimalloc-sys`
are the only crates that compile C. Removing the HTTP chain removes `ring`;
`mimalloc` remains either way, and is a companion item under ADR-0128 — make it
optional for the appliance profile, or supply a musl C toolchain — not this
ADR's. With both gone the headless host is pure Rust and links a static musl
binary with rustc's self-contained target and no external toolchain.

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

**Re-measured 2026-08-23, after ADR-0204 M4 removed the binding.** The
figures above described a tree that no longer exists: with `walkers`,
imzero2's own `reqwest` and `h3o` out of the manifest (and `lru`, which only
`walkers_tiles.rs` used), the distinct-crate count of `cargo tree -e normal`
fell from 435 to 313 for the desktop build and from 321 to 166 for the
`headless_soft` host — 117 lock entries, no additions — and none of
`reqwest`, `rustls`, `ring`, `hyper`, `walkers`, `h3o`, `geo`, `resvg` or the
second `png` remains in either tree. The musl check passes: `cargo check
--target x86_64-unknown-linux-musl --no-default-features --features
headless_soft` with `fast_alloc` off (ADR-0205 M6) has no C-compiling crate
left but `blake3`'s build dependency, which falls back to pure Rust. O1–O3 of
the Design space below are therefore moot as remedies; what remains of this
ADR is the analysis, and its Decision is superseded by ADR-0204 on that
ADR's acceptance (its Q2 / F7).

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
- **O3 — Carry a patched walkers locally.** O2's patch on a fork, reached
  through `[patch.crates-io]` — as a git source, or as a vendored copy the way
  this tree carries `egui_software_backend`, whose `VENDORING.md` records the
  upstream commit, the deltas and the reasoning.
- **O4 — Reimplement the map core in Rust,** inside imzero2: ~1,214 lines, no
  IDL change.
- **O5 — Reimplement with the state in Go.** Projection, visible-tile selection,
  pyramid fallback and pan/zoom live in Go; Rust keeps the texture registry and
  gains one draw op that takes a per-frame tile draw list.

**Criteria.**

- **C1 — Removes the supply surface** — the crates, owners and build scripts,
  not just the bytes. Two tiers, per the Context measurement: the cache layer
  (any option alone) and the TLS stack (only together with ADR-0165).
- **C2 — Removes shipped bytes** — the 1.06 MB. This criterion belongs to
  ADR-0165, which removes the line that keeps the bytes live; no option here
  moves it on its own.
- **C3 — Independent of anyone else's timeline.**
- **C4 — Code this tree owns and tests indefinitely.**
- **C5 — Risk of behaviour regression** — pan/zoom feel, tile fallback, the
  details walkers has already debugged.
- **C6 — Unblocks a static musl appliance build** — removes `ring`, one of the
  two C-compiling crates in the headless closure. Conditional on ADR-0165 for
  every option, and `mimalloc` remains either way.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | −− | +  | +  | +  | +  |
| C2 | −− | 0  | 0  | 0  | 0  |
| C3 | ++ | −− | +  | ++ | ++ |
| C4 | ++ | ++ | +  | −  | −  |
| C5 | ++ | ++ | ++ | −  | −− |
| C6 | −− | +  | +  | +  | +  |

C1 and C6 read `+` rather than `++` for O2–O5 because each reaches its full
value only with ADR-0165 alongside; C2 reads `0` because it is ADR-0165's to
deliver. O2 is the only option that reaches C1 and C6 without paying C4 or C5 at
all, and it is worth doing for its own sake — every downstream user of walkers
gets the same relief, including from an open RUSTSEC advisory upstream already
tracks (`bincode`, unmaintained, sits in the cache layer via `cacache`). Its
single weakness is C3, and O3 is exactly the bridge for that.
O4 is dominated by O5 on architecture fit and by O2/O3 on cost: it buys the same
independence as O5 while putting stateful UI logic in the artifact this tree
keeps deliberately thin, and it needs no IDL change only because it declines the
split that would make the logic testable from Go.

## Decision

*Proposed, not implemented.* Sequence rather than choose:

1. **Upstream the feature gate (O2)**, carrying it locally until it lands (O3).
   The local carry is a `[patch.crates-io]` entry pointing at the patch branch
   of a fork (`git = …, rev = …`); the manifest's `[patch.crates-io]` section
   exists and holds only commented examples. Not a vendored copy: the
   `vendor/` + `path =` form used for `egui_software_backend` would commit
   ~4.6k lines of third-party source into a public repository to carry a
   ten-line diff, which is the objection
   [ADR-0056 §SD12](./0056-walkers-map-h3-binding.md) raised when it rejected a
   patched walkers for the TLS knob — and that vendoring has itself not been
   recorded in an ADR yet (its `VENDORING.md` says so). §SD12 is superseded
   here in its conclusion, not its reasoning: the diff is still small and the
   upgrade still manual, but what it buys is now the cache layer and, with
   ADR-0165, the TLS stack and `ring`, rather than one certificate field. The
   airgap bundle's `cargo vendor`
   ([ADR-0095](./0095-airgapped-build-bundle.md)) carries git dependencies, so
   the fork need not be reachable from a target host. Behaviour is unchanged;
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
  the mesh and atoms paths already use — and walkers' own `TilePiece { tile, uv }`
  is already that item, one per visible tile. The per-frame op budget is a
  thing to measure before committing, not to assume — see Q2.
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
  Part of the `tiles.rs` row is already paid: `walkers_tiles.rs` carries its own
  lower-zoom interpolation and cache-or-interpolate path, mirrored from
  `HttpTiles` under [ADR-0056 §SD13](./0056-walkers-map-h3-binding.md).
- **SD6 — The upstream gate covers the async I/O, not just `http_tiles`.**
  `io/fetch.rs` holds the `TileFactory` trait and the async fetch loop over
  `futures` channels; the trait's only implementation is `EguiTileFactory` in
  `tiles.rs`, and its only users are `HttpTiles` and `PmTiles`. So the whole
  `io` module, `http_tiles`, and the `EguiTileFactory` impl in `tiles.rs` go
  behind the gate, with the `HttpTiles`/`Stats`/`HttpOptions` re-exports;
  `pmtiles` implies the gate (same tokio runtime); `futures` becomes optional
  with the four HTTP crates; `LocalTiles` (which decodes through `Tile::new`
  directly) and `sources` — `TileSource`, `Attribution`, `OpenStreetMap`, all
  three used by this tree — stay ungated. Upstream
  declares the crates as workspace dependencies, so the patch is one hunk in
  the workspace manifest, one in the crate's, and roughly ten `cfg` lines. The
  PR targets master, which is on egui 0.36; the local carry is the same patch
  on the 0.56.0 tag, because this tree's egui ring is pinned at 0.35 until
  `egui_graphs` releases against 0.36.

### Milestones

- **M0 — Verify the byte claim.** Build the client with imzero2's own `reqwest`
  line removed (the ADR-0165 move, or a scratch build that stubs
  `HttpTransport`) and re-read its symbol table with the
  [ADR-0173 §SD8](./0173-code-volume-self-inspection.md) lens. The 1.06 MB is
  expected to fall to near zero under dead-code elimination while walkers' copy
  of the chain stays in the graph; that expectation is unverified and cheap to
  settle. The crate-count side is settled (Context table): the gate alone moves
  the cache layer, both changes move the stack.
- **M1 — Upstream patch (O2).** The gate of §SD6: two manifest hunks and the
  `cfg` lines, PR against master.
- **M2 — Carry it locally (O3)** as a git `[patch.crates-io]` on the 0.56.0 tag
  until M1 lands, and re-measure the Context figures — the expected result is
  the first row of the Context table, not the second.
- **M3 — A `drag` verb for the headless trace driver.** The driver's verbs
  today are `click`, `focus`, `hover`, `key`, `scroll`, `scroll_into_view`,
  `set_value`, `type`, `capture`, `resize`, `cadence`, `sleep` — zoom is
  drivable, pan is not, and Go can already read the camera back through
  `FetchR15WalkersCameras`. This is the precondition for Q3, and it is useful
  under O2/O3 too, as the first regression net the map has had.
- **M4 — Only if M1 stalls: O5.** Prototype the draw-list op against the
  existing texture registry before moving any projection code; M3 first.

### Open questions

- **Q1 — SVG export — answered: it is a gap today.**
  [`svgexport.rs`](../../rust/imzero2/src/imzero2/svgexport.rs) records it
  beside `TexturePixelCache` ("coverage gap — walkers tiles"): tiles are
  uploaded inside walkers' `Tile::new` with no hook, so the textured meshes
  fall through to the visitor's comment-skip path and an exported map is an
  empty frame with its overlays. The note's own remedy — implement `Tiles` from
  scratch and intercept the bytes before upload — is what `BasemapTiles` now
  is; its download workers decode every tile, so mirroring the decoded RGBA
  into the pixel cache closes the gap under O2/O3 without touching walkers.
  Under O5 it closes for free: §SD3's registry in `image.rs` already mirrors.
  Not a blocker for any option; a small fix that should land regardless.
- **Q2 — Per-frame op budget under O5.** 20–60 tiles per map per frame against a
  16 ms budget on a stream that already carries the whole widget tree. SD2
  proposes one op; the measurement has not been done.
- **Q3 — Pan/zoom parity — half answered.** There is no test today that would
  catch a regression in map feel. The headless trace driver can scroll (zoom)
  but cannot drag (pan), and Go can read the camera after either; egui_mcp can
  drag but is desktop-only. So zoom parity is assertable now, pan parity after
  M3; O4/O5 stay untestable until then.
- **Q4 — Does upstream want the patch? — still unknown, but the field is
  clear.** walkers has no issue or PR about an optional HTTP client, in any
  state; master still declares the five crates non-optional, as workspace
  dependencies. The change is additive and default-preserving, which is the
  shape most maintainers accept, and one of the crates it makes avoidable
  (`bincode`, via `cacache`) is the subject of an open RUSTSEC advisory in
  upstream's own tracker. The answer still determines whether O3 is a two-week
  bridge or a permanent fork.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `imzero2` Cargo manifest | `walkers` gains `default-features = false` and a git `[patch.crates-io]` entry (O2/O3); direct `reqwest` removed (with ADR-0165) | `Cargo.lock` (a git source until M1 lands); the airgap bundle's crate set |
| egui2 IDL | unchanged under O2/O3; one draw-list op added under O5 | regenerated dispatch on both sides; `SKILL.md` |
| `walkersMap` opcode and overlay contract | unchanged under every option (§SD1) | nothing |
| `BOXER_MAP_TILE_CA_FILE` / `BOXER_MAP_TILE_INSECURE_TLS` | retired by ADR-0165, not by this ADR | `doc/env-vars.md` regeneration |
| headless trace driver ([ADR-0154](./0154-headless-carrier-tree-and-driver.md)) | gains a `drag` verb (M3) | its verb list in [launch-apps-non-interactively](../howto/launch-apps-non-interactively.md) |

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
- Together with ADR-0165, the desktop client reaches zero network dependencies
  — the property ADR-0165's Context claims for it, which this ADR shows is not
  reached by ADR-0165 alone; the headless clients keep `tokio` for their own
  carrier and lose everything else.
- `ring` leaves the closure, which is half of what a static musl appliance build
  needs (Context); the other half, `mimalloc`, is ADR-0128's to decide.
- The measurement generalises: `default-features = false` on a widget crate is
  worth checking wherever one is bound, and this is the second subtree in a
  month (after [ADR-0202](./0202-retire-arrow-parquet.md)) whose cost sat far
  above its usefulness.

### Negative

- O2 puts this tree on someone else's release schedule; O3 is the mitigation and
  is itself a maintenance item — a git source in `Cargo.lock`, a fork branch to
  keep on the 0.56.0 tag until the egui ring moves, and the ADR-0056 §SD12
  objection it overrides.
- O5, if it comes, adds ~1,214 lines of widget code and an IDL op, and the
  43 branch points in `tiles.rs` and `center.rs` are where map widgets are
  historically wrong in ways users notice before tests do.
- Any reimplementation re-opens details walkers already settled — antimeridian
  handling, zoom clamping, tile wrap, high-DPI, inertia feel — none of which
  appear in a line count.

### Neutral

- The Context figures are one measurement of one build on one host. They are
  reproducible from the tree (`cargo tree`, a symbol-table read, and a line
  count; the two-tier table by gating the five dependencies in a scratch copy
  of the crate and diffing `cargo tree -e normal` per profile; the musl result
  by `cargo check` against the musl target) and should be re-taken rather than
  trusted after any dependency bump.
- ADR-0056 §SD12's kill-reason stands as written — a small diff is still a
  manual merge on every upgrade. What changed is what the patch buys.
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
- **What would fail.** Two tiers, matching the Context table. After O2/O3
  alone: `cargo tree -e normal` still naming `http-cache-reqwest`, `cacache` or
  `reqwest-middleware` under the client — `reqwest`, `rustls`, `hyper` and
  `tokio` are *expected* to remain at this tier, and a check that greps for
  them would fail for the wrong reason. After O2/O3 with ADR-0165: any of
  `reqwest`, `rustls`, `ring`, `hyper` in any profile, and `tokio` in the
  desktop profile (it stays in the headless profiles for the carrier). Both
  checks are a grep over `cargo tree` output and belong in the Rust lane, which
  today has no CI gate ([ADR-0173](./0173-code-volume-self-inspection.md)
  records the same gap for the Rust artifact generally). A symbol-table read
  that still finds `rustls` text after M0 would mean dead-code elimination is
  not doing what M0 assumes.
- **musl.** `cargo check --target x86_64-unknown-linux-musl
  --no-default-features --features headless` without a musl C compiler: today
  it fails two build scripts (`ring`, `libmimalloc-sys`); after O2/O3 with
  ADR-0165 it should fail exactly one (`libmimalloc-sys`), and none once
  `mimalloc` is optional for that profile. A third failing build script at any
  stage is a new C-compiling dependency and a regression against C6.
- **Gap.** Map feel is not gated by anything today (Q3). Under O2/O3 that is
  acceptable because the widget does not change; under O4/O5 it is the first
  thing that needs building, and this ADR should not be flipped to accepted with
  O5 in scope until Q3 has an answer.

## Status

Superseded by [ADR-0204](./0204-leaflet-map-core-port.md) — 2026-08-27, never
accepted: its Decision is replaced by the port, its Context remains the
analysis. Before that: Proposed — 2026-08-22; revised in place the same day
after the closure and musl measurements in the Context. No implementation. It
supersedes nothing itself — the [ADR-0056](./0056-walkers-map-h3-binding.md)
sub-decisions it would have touched fell with the binding at ADR-0204 M4, and
[ADR-0165](./0165-imzero2-tile-transport-over-fffi2.md), which it corrected, is
folded into the same ADR.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.

## References

- [ADR-0056 — walkers map and H3 binding](./0056-walkers-map-h3-binding.md) — the binding, its TLS updates, and the convergence onto one tile client.
- [ADR-0165 — imzero2 tile transport over FFFI2](./0165-imzero2-tile-transport-over-fffi2.md) — the fetch move this ADR sequences with and corrects.
- [ADR-0058 — imzero2 scrolling texture widget](./0058-imzero2-scrolling-texture-widget.md) — the existing textured-quad drawing path.
- [ADR-0128 — imzero2 mesh draw stream codec lane](./0128-imzero2-mesh-draw-stream-codec-lane.md) — the draw-stream shape an O5 draw list would sit beside, and the deferred musl-static appliance target the Context measurement bears on.
- [ADR-0095 — airgapped build bundle](./0095-airgapped-build-bundle.md) — `cargo vendor` carries the O3 git patch into the bundle.
- [ADR-0154 — headless carrier tree and driver](./0154-headless-carrier-tree-and-driver.md) — the trace driver M3 extends.
- [ADR-0173 — code-volume self-inspection](./0173-code-volume-self-inspection.md) — the symbol-table and owner lenses the Context figures were taken with.
- [ADR-0202 — retire arrow-go's parquet packages](./0202-retire-arrow-parquet.md) — the same pattern on the Go side.
- `rust/imzero2/src/imzero2/walkers_tiles.rs` — `BasemapTiles`, `TileTransport`.
- [`rust/imzero2/src/imzero2/image.rs`](../../rust/imzero2/src/imzero2/image.rs) — the texture registry §SD3 reuses.
- [`rust/imzero2/src/imzero2/svgexport.rs`](../../rust/imzero2/src/imzero2/svgexport.rs) — `TexturePixelCache` and the recorded tile coverage gap (Q1).
- [`doc/skills/imzero2-fetchers/SKILL.md`](../skills/imzero2-fetchers/SKILL.md) — the `Sync()`-only fetcher rule an O5 request path must fit.
