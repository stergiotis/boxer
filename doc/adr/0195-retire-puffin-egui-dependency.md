---
type: adr
status: accepted
date: 2026-08-17
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-17
---

# ADR-0195: retire the `puffin_egui` dependency and the `showPuffinProfiler` opcode

## Context

`puffin_egui` is the in-app frame-profiler *window* for egui. It entered the
tree on 2026-05-29 with the renderer-workspace import (`a39b677f`) as a
**non-optional** dependency, alongside an IDL node `showPuffinProfiler`
(`22ca6c13`, the same day) whose Rust body was, in the imported form:

```
//#[cfg(feature = "puffin")]
//puffin_egui::profiler_window({{EguiContext}}); // FIXME problem with egui version in puffin_egui crate
```

**The body arrived commented out and was never uncommented.** The opcode has
therefore never done anything in this repo: the Go binding wrote a frame, the
generated interpreter arm consumed its arguments, and the arm's apply block was
empty. The FIXME names the reason correctly — `puffin_egui` 0.30 builds against
egui 0.33, while this tree tracks egui 0.35, so `profiler_window` will not
typecheck across the gap.

Because the dependency was not optional, the whole egui 0.33 stack was resolved
and compiled into **every** build to satisfy a call that does not exist.
Measured on the working tree at the date of this ADR, removing the one manifest
line drops 12 packages from `Cargo.lock` (674 → 662):

| Removed | Why it was there |
| --- | --- |
| `puffin_egui` | the dependency itself |
| `egui` 0.33.3, `egui_extras` 0.33.3, `epaint`, `emath`, `ecolor` | a second, complete egui stack |
| `accesskit` 0.21.1 | egui 0.33's accessibility tree types |
| `ab_glyph`, `ab_glyph_rasterizer`, `owned_ttf_parser` | egui 0.33's font rasterisation |
| `log-once`, `natord` | `puffin_egui`'s own helpers |

After the change `cargo tree -d` reports no duplicate egui-family crate; the
graph carries one egui version instead of two. Unrelated duplicates (`base64`
via `walkers`, among others) are untouched.

An in-flight crate-count analysis of the Rust graph, unpublished at the date of
this ADR, independently flagged both halves of this: that two egui versions
were linked (0.35.0, and 0.33.3 via `puffin_egui` 0.30), and that dropping
`puffin_egui` was available as a subtraction rung. That analysis priced the
rung as giving up "the in-app profiler UI". This ADR records that the rung is
**free**, not a trade: there was no in-app profiler UI to give up. Its measured
crate counts predate this change and should be re-derived, not quoted, if it is
published later.

### What this does *not* touch

`puffin` and `puffin_http` — the frame-profiler **server**, gated behind
`--features puffin`, built by `build_rust.sh`, driven by `profile.sh
rust-puffin`, started in `main.rs`, and emitting `profile_scope!` markers
throughout the generated interpreter — are a different mechanism and stay
exactly as they are. They are the profiling path that actually works: an
external `puffin_viewer` connects to `127.0.0.1:8585`. `puffin_egui` would have
drawn the same data *inside* the app; only that in-app window is retired.

## Decision

We remove the `puffin_egui` dependency and, with it, the `showPuffinProfiler`
IDL node and every generated surface it produces on both sides of the FFFI2
boundary. imzero2 keeps the puffin *server* lane unchanged.

### SD1 — the opcode goes with the crate, rather than remaining a no-op

The alternative was to drop only the manifest line and leave the opcode as a
documented no-op, which avoids the wire change below. Rejected: the opcode's
entire reason to exist was to call `puffin_egui`. Keeping an exported
`ShowPuffinProfiler()` in the public Go API after removing the crate leaves a
function that is named for a capability the tree no longer has any route to,
and which never worked — a stub that lies to its caller, and to any agent
reading the generated API reference.

### SD2 — opcode ids renumber; both sides regenerate together

The IDL registry is name-sorted before ids are assigned, so removing
`showPuffinProfiler` (`FuncProcIdOffset + 150`) shifts every `FuncProcId` that
sorts after it down by one; the highest id moves 184 → 183. This is a wire
change across the FFFI2 frame boundary: the Go bindings and the Rust
interpreter must be regenerated and rebuilt **together**, and a Go binary from
before the change will not interoperate with a Rust host from after it. Nothing
persists these ids across a build, so no stored data is affected. This is the
same mechanic [ADR-0194 SD2](./0194-retire-egui-snarl-binding.md) recorded for
the snarl removal — a rebuild obligation, not a migration.

### SD3 — the FIXME is not carried forward

No `// deferred:` note replaces the removed FIXME. A deferral marker records
work that is still wanted; restoring an in-app profiler window is not. If it is
ever wanted, the requirement is a `puffin_egui` release that tracks this tree's
egui version, at which point re-adding an IDL node is a small change against
whatever egui version is current then — the removed node's shape (procedural,
zero arguments) carries no design worth preserving.

### SD4 — background work that measures this graph is not rewritten

Per [ADR-0194 SD4](./0194-retire-egui-snarl-binding.md), background work is a
dated snapshot rather than a document maintained against the tree. Any analysis
that counted this crate graph keeps its numbers as written; this ADR is the
record that tells a later reader the `puffin_egui` rung has since been taken,
that it cost nothing, and that counts taken before this date are stale by 12
packages.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| egui2 IDL (`definition/egui2_definition_d_specials.go`) | removed — the `showPuffinProfiler` node | the generated Go and Rust surfaces |
| `FuncProcId` enum (FFFI2 wire) | `ShowPuffinProfiler` removed; **all later ids shift** (SD2) | both sides of the FFI boundary, rebuilt together |
| `bindings` (exported Go API under `public/`) | removed — `ShowPuffinProfiler()`, `FuncProcIdShowPuffinProfiler` | any out-of-tree caller (none known) |
| `rust/imzero2/src/imzero2/interpreter.rs` | regenerated — the dispatch arm drops out | nothing |
| `rust/imzero2/Cargo.toml` | removed — `puffin_egui = "0.30.0"` | `Cargo.lock` (12 packages) |
| gallery demo `debug_tools` | the call removed; description no longer claims a frame profiler | nothing — the call was inert, so the rendered frame is unchanged |
| `doc/skills/imzero2/assets/egui2_api_reference.md` | regenerated — the entry drops out | nothing |
| `doc/skills/imzero2/SKILL.md`, `assets/bindings.md` | hand-edited — the removed symbol drops out of both | nothing |
| `puffin` / `puffin_http`, `--features puffin`, `profile.sh` | **unchanged** — different mechanism | nothing |
| `profiling` crate and its `profile-with-puffin` feature | **unchanged** | nothing |

## Alternatives

- **O1 — fix it: bump `puffin_egui` to an egui-0.35 release.** Rejected as not
  available on demand and not wanted regardless: it reinstates a second crate
  tracking egui version bumps to duplicate what the puffin server already
  provides through `puffin_viewer`.
- **O2 — make the dependency optional, under `--features puffin`.** This is the
  smallest change that stops non-optional compilation, and would be the right
  answer if the call site worked. It does not, so the feature would gate dead
  code. Rejected.
- **O3 — drop the crate, keep the opcode as a no-op.** Rejected per SD1.
- **O4 — drop both (chosen).** Takes the carrying cost to zero and removes a
  public API symbol that never had an implementation.

## Consequences

### Positive

- 12 packages leave the build graph, including a complete duplicate egui stack,
  for every build configuration — the dependency was not feature-gated.
- One egui version is linked instead of two, which removes a standing source of
  confusion when reading `Cargo.lock` and one obstacle to egui bumps.
- One fewer crate tracking egui releases, and one fewer to carry in the
  airgapped bundle.
- The generated API reference stops advertising a function that does nothing.

### Negative

- imzero2 has no in-app profiler window. It never did in this repo, so nothing
  regresses in practice, but the affordance is now also absent from the IDL.
- The opcode renumbering (SD2) invalidates any FFFI2 stream recorded before the
  change. Recordings are debugging artefacts rather than persisted data, so the
  cost is that an old capture cannot be replayed against a new host.

### Neutral

- Profiling capability is unchanged: `--features puffin` plus `profile.sh
  rust-puffin` and an external `puffin_viewer` remain the frame-profiling path,
  and the `profile_scope!` markers in the generated interpreter are untouched.

## Migration — Tier 1

- **Breaks.** The exported `ShowPuffinProfiler()` under `public/` disappears,
  and every `FuncProcId` after the removed one changes value. No in-tree caller
  remains; an out-of-tree caller would not compile.
- **Path.** No staged path and no shim — the function had no behaviour to
  preserve. A caller deletes the call; nothing replaces it.
- **Regeneration.** Yes, and both sides of the FFI boundary need rebuilding
  together (SD2). Order matters: `egui2gen` type-checks the bindings package
  before writing, so the hand-written Go call site must be removed *before* the
  regen. Sequence used: hand-written Go → IDL → the three `egui2gen generate`
  steps of `./generate.sh` → `Cargo.toml` → build both. There was no
  hand-maintained Rust to remove — the dispatch arm is generated.
- **Old shape.** Nothing replaces it. An in-app profiler window would need a
  `puffin_egui` release matching this tree's egui version (SD3).

## Verification plan — Tier 1

- **Lane.** Default `go build` / `go test` with the repo tags for the Go side;
  `cargo build --release --features puffin` for the Rust host, which also
  refreshes `Cargo.lock`.
- **What would fail.**
  - **The regen is incomplete.** `egui2gen` type-checks the bindings package
    before writing, so a leftover hand-written reference to the removed symbol
    fails the generator rather than the compiler — the intended first tripwire.
  - **The two sides disagree on ids.** A Go binary and a Rust host built across
    the change would mis-dispatch every opcode after the removed one. Nothing
    detects this automatically; it is why SD2 states the rebuild obligation.
    The check is that both artefacts come from one build.
  - **Something else needed egui 0.33.** Guarded by reading the `Cargo.lock`
    diff: the removed packages must be exactly the `puffin_egui` subtree. In
    particular `accesskit` must survive at the version eframe uses, since
    [ADR-0154](./0154-headless-carrier-tree-and-driver.md)'s driver and
    egui-mcp both read the AccessKit tree.
  - **`--features puffin` regressed.** The server lane shares a name with the
    removed crate but no code; building with the feature is the check.
- **Gap.** No test asserts the absence of the binding, and none is added — a
  removal is verified by the build. The `debug_tools` demo's rendered output is
  unchanged because the removed call was inert, so no screenshot in the tour
  should move; that is reviewed by eye, not asserted.

## Status

Accepted 2026-08-17. All four milestones landed in one commit the same day
(`599cb9dc`), which carries this ADR alongside the removal it describes.

- **M0 — hand-written Go.** ✓ The `debug_tools` demo's call and its description
  claim.
- **M1 — IDL and regen.** ✓ The `showPuffinProfiler` node; regenerated via the
  three `egui2gen generate` steps of `./generate.sh` rather than the whole
  script, to leave unrelated in-flight codegen untouched; both sides rebuilt.
- **M2 — dependency.** ✓ Crate dropped; `Cargo.lock` refreshed by the build.
- **M3 — docs.** ✓ This ADR; the imzero2 skill and its `bindings.md` asset.

Measured on execution: `go build` with the repo tags clean; `cargo build
--release --features puffin` clean, with the warning count unchanged at 86
before and after — the orphan check ADR-0194 recorded, confirming no import or
helper was left dangling. `Cargo.lock` 674 → 662 packages; `cargo tree -d`
reports no duplicate egui-family crate.

Two things worth recording from the execution:

- **The Context's crate-count analysis is described but not cited, deliberately.**
  It was never committed and was deleted from the working tree the same day. A
  markdown link to it would be a dangling local link, which doclint treats as an
  error, so its findings are restated here in prose instead. A later reader
  should not go looking for the document.
- **Verification ran in a throwaway `git worktree add --detach` at `HEAD`, not
  in the working tree.** A concurrent session was editing `apps/play`, whose
  compile errors changed between two consecutive runs, so the tree could not
  distinguish this change's breakage from that one's. Copying only this change's
  files into a pristine checkout gave a clean signal, and is the cheaper habit
  whenever the tree is shared.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0194](./0194-retire-egui-snarl-binding.md) — the adjacent binding
  removal whose SD2 renumbering mechanic and regeneration order this follows.
- [doc/howto/imzero2-render-troubleshooting.md](../howto/imzero2-render-troubleshooting.md)
  — the profiling path that remains, via the puffin server.
