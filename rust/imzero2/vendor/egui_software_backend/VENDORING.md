---
type: reference
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Vendored 2026-08-22; the decision to
> vendor rather than depend on crates.io has not been recorded in an ADR yet.
> See [the survey](../../../../doc/adr-background-work/egui-software-backend-survey.md)
> for the measurements that led here.

# Vendored: `egui_software_backend`

CPU software rasterizer for egui, used by imzero2's `headless_soft` host
(§ `src/imzero2/headless.rs`). Upstream:
<https://github.com/DGriffin91/egui_software_backend>.

- **Upstream commit**: `1ab06af49887d7c4137570dd885cddc3fa30b4aa` (2026-04-14),
  which is release `0.0.3`.
- **Licence**: MIT OR Apache-2.0. Both texts are kept verbatim beside this file
  (`LICENSE-MIT`, `LICENSE-APACHE`) and the crate is recorded in
  [THIRD_PARTY_NOTICES.md](../../../../THIRD_PARTY_NOTICES.md).

## Why vendored rather than a crates.io dependency

The crate has one primary author, three releases and roughly 1.3k downloads,
and its egui-version bumps have arrived from outside contributors. imzero2
already pins a five-crate egui ring that has to move as one commit (see the
note on `egui` in `../../Cargo.toml`); a sixth crate on that ring that can lag
a release is a scheduling risk, not a code risk. At 5.7k lines under a
permissive licence, carrying it is cheaper than being blocked by it — and the
one egui minor bump measured so far cost zero lines in the render core.

## Deltas against upstream

Everything below is a deletion or a version pin; no rasterizer logic was
touched.

| What | Why |
| --- | --- |
| `egui` `"0.34"` → `"0.35"` | match the imzero2 ring pin. The render core needed no source change; only the two surfaces removed below did. |
| removed the `winit` feature, its four optional deps (`winit`, `softbuffer`, `egui-winit`, `bytemuck`) and `src/winit.rs` | imzero2 brings its own host. This surface is also the one that does *not* compile at 0.35 (`ViewportBuilder::monitor`, `ViewportCommand::SetMonitor`). |
| removed the `test_render` feature, its optional deps (`egui_kittest`, `image`) and `src/test_render.rs` | the upstream CPU-vs-GPU comparison harness; it needs dev-dependencies this copy does not carry. |
| removed `[dev-dependencies]`, `[[example]]` × 4, `[profile.dev*]` | dev-only, and cargo ignores `[profile]` outside the workspace root. |
| the crate-level doctest is `ignore`d | it constructs `egui_demo_lib::DemoWindows`, and the dev-dependency is gone. |
| `data/`, `tests/`, `examples/`, `demo.png`, `deny.toml`, `clippy.toml` not copied | not reachable from the library build, or (the last two) only meaningful with the lint tables below. |
| removed `[workspace]` and the `[workspace.lints.*]` tables | a nested workspace root inside `rust/imzero2` is rejected outright (*"multiple workspace roots found in the same workspace"*), and both packages are named in that workspace's `exclude` instead. The lint tables went with it: they only bind through a `[lints] workspace = true` opt-in, which neither upstream manifest carries, so they were inert even upstream. |
| `#![allow(clippy::all)]` on both crate roots | `check.sh` runs `cargo clippy --workspace -- -D warnings`, and **clippy lints path dependencies** — `exclude` does not spare them, so upstream's style choices would become hard errors in our gate. `cargo fmt` reaches them too; that one is handled on the other side, by naming packages in `check.sh` instead of `--all`. |
| added `EguiSoftwareRender::blit_dirty_to_buffer` | `blit_canvas_to_buffer` paints every *occupied* tile every frame, which for a viewport-filling UI is the whole frame — measured at 60–75 % of the frame cost. The addition paints only *dirty* tiles, resetting each to a caller-supplied clear colour first so the alpha composite does not stack. Purely additive; the original is untouched. Worth offering upstream. |
| `EguiSoftwareRender::{set_textures, free_textures}` widened from private to `pub` | the host has to apply a pass's texture deltas on frames nobody consumes pixels for — deltas are incremental, so dropping them corrupts a later viewer's texture state. Both call sites are marked `// boxer delta:` in `src/lib.rs`. |

## How this copy is built

It is **excluded** from the imzero2 workspace (`exclude = ["vendor"]` in
`../../Cargo.toml`) and consumed as a path dependency. That is deliberate:
`check.sh` runs `cargo fmt --all` and `cargo clippy --workspace ... -D warnings`,
and neither should reformat or gate on third-party source. It keeps its own
`[workspace]` table so it stays self-contained.

imzero2 enables `default-features = false, features = ["std", "log", "rayon"]`
— `std` for runtime SIMD detection (AVX2 / SSE4.1 / NEON, with a scalar
fallback), `log` so the backend's own errors reach the tracing subscriber
through the `tracing-log` bridge that `main.rs` installs, and `rayon` for the
worker pool.

`EguiSoftwareRender` is constructed `.with_caching(true)`, and that is one
decision with the `rayon` feature rather than two: **all three of the crate's
parallel sections are on the caching path**, and `render_direct` has no
parallel variant. Uncached is the faster of the two single-threaded, which is
where this host started; cached with a warm pool is ~4.5× better than either
and beats the wgpu host it stands in for. `softraster.rs` sizes the global pool
to half the hardware threads — the measured curve peaks around there, because
useful parallel width is bounded by the frame's 64-pixel tile-row count.
Measurements and the full thread sweep are in the survey linked above.

## Re-syncing

```sh
git clone https://github.com/DGriffin91/egui_software_backend /tmp/esb
git -C /tmp/esb diff 1ab06af4..HEAD -- src/ Cargo.toml
```

Re-apply the table above to whatever that diff shows, bump the commit recorded
here, and re-run `scripts/dev/play-screenshot-tour.sh` — a rasterizer
regression shows up as a pixel diff in the gallery, not as a build failure.
