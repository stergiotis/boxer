---
type: adr
status: proposed
date: 2026-08-05
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0165: imzero2 tile transport over FFFI2

## Context

The renderer fetches basemap tiles itself. Two HTTP clients now reach the network from one application: Go's, configured through the env registry, and the renderer's, configured through the `walkersMap` opcode. [ADR-0056 §SD12–SD15](./0056-walkers-map-h3-binding.md) added the second one's TLS knobs (`BOXER_MAP_TILE_CA_FILE`, `BOXER_MAP_TILE_INSECURE_TLS`) because walkers' client trusts only its bundled roots and exposes no seam. That closed the immediate gap and made the shape of the problem visible: every policy Go already owns — proxy configuration, certificate trust, timeouts, retry, the [§SD10 capability gate](./0026-app-runtime-and-capability-subjects.md), the extbin resolution chokepoint, request logging — has to be re-expressed as IDL surface before the renderer can honour it. The knob pair is the second such re-expression after the URL template, and there is no reason to expect it to be the last.

Tile fetching is also the only thing in imzero2 that reaches the network at all. Nothing else in the render client opens a socket, so this is a single, bounded exception rather than a pattern to generalise.

Constraints:

- **The frame protocol is synchronous, tile loading is not.** FFFI2 is a register-drain opcode stream; fetchers may only run from `StateManager.Sync()` at frame end (a fetcher called inline from a widget body deadlocks the render loop). Tiles already take many frames to arrive, so added latency is affordable — but the request/response hop has to fit the drain model, not fight it.
- **Decoding must stay off the render thread.** A screenful is 20–40 PNG decodes. Wherever the bytes come from, something other than the frame loop has to turn them into textures.
- **The wire carries pixels today.** `mapRaster` ships raw RGBA buffers; compressed tiles are smaller per unit of screen area than what already crosses the boundary.

## Design space (QOC)

**Question.** Where should the bytes behind a basemap tile come from, and who configures that?

**Options.**

- **O1 — Status quo: HTTP in the renderer, policy re-expressed as IDL surface.** Every new transport concern becomes another `walkersMap` method plus an env var plus a Go plumbing line.
- **O2 — Tile transport over FFFI2 into Go (proposed).** The renderer's `Tiles` implementation emits tile requests on a register; a fetcher drains them in `Sync()`; Go fetches on a background job and ships bytes back as a registered node; the renderer decodes and caches exactly as it does now. `imzero2::walkers_tiles::TileTransport` is already the seam this drops into.
- **O3 — O2, with fetching behind a keelson HTTP facility.** As O2, but Go's tile pump does not call `net/http` directly; it requests egress from a capability-gated facility that any component can use.
- **O4 — Loopback proxy.** Go runs a `127.0.0.1` HTTP server; the renderer's tile URL points at it. Achieves one egress point with no protocol change.

**Criteria.**

- **C1 — One configured egress point.** Transport policy stated once, in Go.
- **C2 — Admits non-HTTP tile sources.** mbtiles/pmtiles on disk, tiles out of ClickHouse, a fact-store lane.
- **C3 — Blast radius.** How much unrelated code the change touches.
- **C4 — Attack surface added.**
- **C5 — Render-loop cost.** Frame-time and wire-bandwidth impact.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | ++ | ++ | +  |
| C2 | −− | ++ | ++ | −− |
| C3 | ++ | −  | −− | +  |
| C4 | +  | ++ | ++ | −− |
| C5 | ++ | +  | +  | +  |

O2 is the proposal. O4 reaches C1 cheaply but adds a listening socket to every app that shows a basemap — an unauthenticated local proxy, reachable by any process on the host — and does nothing for C2. O3 is where this should end up, but the facility's boundary should not be drawn around a basemap's needs; it is recorded here as the successor, not folded in.

## Decision

*Proposed, not implemented.* Route tile fetching through FFFI2 into Go:

- A fetcher returns pending tile requests (map id, tile id) to Go, drained in `StateManager.Sync()` like every other fetcher.
- Go resolves each request on a background job — never on the render thread — and ships the compressed bytes back through a registered node keyed by tile id.
- The renderer decodes off-thread and populates the same LRU it uses today. `HttpTransport` is retired along with the renderer's `reqwest` and TLS knobs; `BOXER_MAP_TILE_CA_FILE` and `BOXER_MAP_TILE_INSECURE_TLS` become ordinary `http.Transport` configuration and keep their names and meanings.

### Open questions

These are the reasons this is proposed rather than accepted; each needs a dialogue before implementation.

- **Q1 — Cache ownership.** The renderer's LRU holds decoded textures and must stay. Whether Go also caches compressed bytes (surviving a tile-config rebuild, shareable across maps) or stays stateless is unsettled.
- **Q2 — Backpressure.** The current bound is a 6-slot request channel plus per-frame re-request. The register drain has different timing; the equivalent needs restating rather than transliterating.
- **Q3 — Failure and retry semantics.** Today a failed fetch is negative-cached until eviction, with no retry. Go can do better, but "better" needs a policy — and a way to surface a persistently failing tile server to the operator instead of a silently grey map.
- **Q4 — Bandwidth in the topologies that matter.** ~1–4 MB on a first screenful, near zero once cached. That is inside the envelope `mapRaster` already occupies for a co-located Go and renderer, which is every topology today. It should be measured, not assumed.
- **Q5 — The keelson facility boundary (O3).** Whether the tile pump calls `net/http` or requests egress from a facility, and where the capability gate sits. Deliberately out of scope here.

## Alternatives

- **O1 — Leave HTTP in the renderer.** Zero cost today and the deployments in view are covered. Killed as a destination, not as a state: it makes every future transport concern — proxy, auth header, request logging, timeout policy — a new IDL method plus a new env var, and the TLS pair is already the second instance of that tax.
- **O3 — Fetch through a keelson HTTP facility.** Where this ought to end up, and the natural home for the capability gate. Not folded in here because its boundary touches every HTTP consumer in the repo, and a boundary drawn to fit a basemap's needs is the wrong boundary. Successor, not alternative.
- **O4 — Loopback proxy in the Go process.** One egress point with no protocol change, using Go's stdlib TLS directly. Killed on attack surface: an unauthenticated local HTTP proxy in every app that shows a basemap, reachable by any process on the host, to avoid a change to a protocol we control. It also leaves the renderer speaking HTTP, so non-HTTP tile sources stay impossible.

## Consequences

**If accepted.** One egress point, configured where every other transport policy already lives. The renderer loses its direct `reqwest` dependency and, with it, the `danger_accept_invalid_certs` call. Non-HTTP tile sources become possible without further IDL surface. The Go side gains a tile pump with a lifecycle to own.

**If not.** The status quo is workable — `BasemapTiles` covers the deployments in view, and the `TileTransport` seam costs nothing to leave in place. The cost is paid per future transport concern, one IDL method at a time.

**Either way.** The seam already exists and the Go-side surface is unchanged by the swap, so this ADR does not block anything and nothing needs to be built in anticipation of it.

## Status

Proposed — 2026-08-05. No implementation. Supersedes nothing; [ADR-0056 §SD15](./0056-walkers-map-h3-binding.md) records the deferral this ADR describes.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## References

- [`doc/adr/0056-walkers-map-h3-binding.md`](./0056-walkers-map-h3-binding.md) — the walkers binding, and §SD12–SD21 for the TLS knobs, the `TileTransport` seam, and the convergence onto one tile client.
- `rust/imzero2/src/imzero2/walkers_tiles.rs` — `TileTransport`, `HttpTransport`, `BasemapTiles`.
- [`doc/skills/imzero2-fetchers/SKILL.md`](../skills/imzero2-fetchers/SKILL.md) — the `Sync()`-only fetcher rule this design has to fit.
- [`doc/adr/0009-environment-variable-registry.md`](./0009-environment-variable-registry.md) — the registry the tile knobs live in.
