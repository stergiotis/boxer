---
type: adr
status: proposed
date: 2026-08-22
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0206: A gokrazy appliance image for the headless host

## Context

[ADR-0128](./0128-imzero2-mesh-draw-stream-codec-lane.md) named the appliance
force — an image wanting a minimal, statically-linked userland — and answered it
by *avoiding pixels*: the mesh draw-stream lane, because "mesa/llvmpipe software
rasterization plus an external ffmpeg are a large dynamically-linked C closure".
Its M3 deferred the rest to a **probe, and an appliance image ADR after it**.
This is that ADR.

[ADR-0205](./0205-imzero2-cpu-rasterized-pixel-host.md) removed the first half
of that closure: `headless_soft` rasterizes on the CPU, so a pixel-producing
build carries no Vulkan loader, no ICD and no Mesa. Its M6 pairs a
**musl-static** build with the gokrazy probe, on the assumption that an
appliance needs both.

The probe found that assumption wrong, which is what unblocked this. `ldd` on
the `headless_soft` binary lists **four files** — `libgcc_s`, `libm`, `libc`
and the loader, about 4.8 MB. An image can simply carry them. (`headless_wgpu`
prints the same four, which is misleading: it reaches Vulkan through `dlopen`,
where `ldd` cannot see it. That is the closure ADR-0128 was avoiding, and it is
invisible to the obvious measurement.)

So the appliance question is separable from musl, and answerable now.

## Design space (QOC)

**Question.** How should the GPU-less headless host be delivered as a bootable
appliance image?

**Options.**

- **O1** — status quo: no image; a systemd unit on a general-purpose distro
  (`showcase/onbox/`).
- **O2** — a NixOS image, reusing ADR-P-0001's substrate as an appliance.
- **O3** — gokrazy: a Go-first appliance, non-Go binaries via `ExtraFilePaths`.
- **O4** — a from-scratch buildroot/initramfs image.
- **O5** — an OCI container on a minimal immutable host.

**Criteria.**

- **C1 — image closure.** What the image must carry, measured as built size.
- **C2 — fit for this process tree.** A Go parent that `fork`/`exec`s a Rust
  child over stdout/stdin pipes, plus an optional ffmpeg subprocess.
- **C3 — update and rollback.** Whether an atomic update with a way back exists
  without building one.
- **C4 — build and maintenance effort.**

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | −− | −  | ++ | ++ | −  |
| C2 | ++ | ++ | +  | +  | ++ |
| C3 | −  | ++ | ++ | −− | +  |
| C4 | ++ | −  | +  | −− | +  |

O3 carries **109 MB** against O2's measured 1.8–2.8 GiB (ADR-P-0001 Phase 1) and
a whole distro for O1, and gets A/B roots plus rollback without building them.
It loses to O1/O2/O5 only on C2, because a Rust binary is not a Go package and
has to ride `ExtraFilePaths` — §SD2 is that cost, and it is small.

This does **not** supersede ADR-P-0001's choice of NixOS. That decision governs
the *showcase deployment*, a box that also runs ClickHouse and a reverse proxy;
this is the *appliance* line ADR-0128 named. Both can be true.

## Decision

We will build **two gokrazy x86-64 appliance images** from one `headless_soft`
host, under `showcase/gokrazy/`.

### SD1 — one host build, two images, ffmpeg the only difference

`boxer-soft` and `boxer-soft-video` carry byte-identical Go and Rust hosts. The
only difference is whether a static ffmpeg is present, so the pair isolates that
variable exactly.

Nothing in either image selects a codec. The no-ffmpeg image finds no encoder to
spawn and `CodecLane::best()` degrades to the ADR-0128 mesh lane on its own —
the image *demonstrates* the fallback rather than being configured into it. This
is why the pair is worth more than a single image with a flag.

The static ffmpeg is the one the airgap lane already builds
(`scripts/dev/build-static-ffmpeg.sh`), not a second build. Being fully static it
cannot `dlopen` a VA driver, so every hardware lane fails and `libopenh264` is
the only candidate left — the case `codeclane.rs` already documents.

### SD2 — the Go host is the package; everything else rides `ExtraFilePaths`

gokrazy supervises Go programs, and the Go host is the parent of this process
tree, so it is the packaged program and the Rust host is one of its files. The
glibc closure ships beside it under `/lib64`.

`build.sh` reads that closure from the binary with `ldd` rather than listing it,
so a newly-linked library fails the build instead of failing the boot. Fonts
ship explicitly too: gokrazy has no fontconfig, so the `fc-match` that
`hmi_headless.sh` relies on cannot run and the paths are passed as flags.

### SD3 — the image builds from the checkout, not from the published module

gokrazy would otherwise fetch `github.com/stergiotis/boxer` from its published
location. `packer.BuildDir` walks up from `builddir/<import path>` looking for a
`go.mod`, which is gokrazy's documented hook for exactly this; `build.sh` writes
one carrying a **relative** `replace` back to the checkout, so no build host's
filesystem layout enters the tree.

`GOWORK=off` is set for the same reason and is **not optional**: a `go.work`
above the checkout captures those builddir modules, and its module set does not
provide gokrazy's own packages. The failure names a gokrazy package
(`.../cmd/dhcp`) and so reads like a gokrazy bug. A worktree outside the
workspace builds cleanly, which hides it.

### SD4 — musl-static is not a prerequisite, and stays open

§Context measured the closure at four files. The image carries them, and
ADR-0205 M6's musl half is **not** required for anything here.

It stays open on its own terms. `cargo check --target x86_64-unknown-linux-musl
--features headless_soft` fails on exactly two build scripts, `ring` and
`libmimalloc-sys`, both only for a missing cross C compiler. `ring` arrives via
`reqwest` ← `walkers` and leaves with
[ADR-0204](./0204-leaflet-map-core-port.md) M4, which deletes walkers outright.
The allocator is handled here: a new **`fast_alloc`** feature gates mimalloc, on
by default and in all four build scripts, so no shipped build changed.

Deliberately **not** a walkers feature gate. That would build a surface ADR-0204
M4 removes, in the files that milestone edits.

### SD5 — the bind address is exposed, and that is a QEMU-only posture

Both images bind `0.0.0.0:8089`, because a port forward cannot reach a guest
listening on loopback.

The carrier has no authentication and no TLS — [ADR-0082](./0082-imzero2-remote-session-auth-tls.md)
is accepted but unimplemented — and the same WebSocket carries input, so
whoever reaches that port has full control of the app. `hmi_headless.sh` refuses
a non-loopback bind, but that gate is in the shell script: neither the Go nor
the Rust side enforces it, so setting the variable here bypasses nothing that
was protecting anything.

This is acceptable **only** under QEMU user-mode networking, which is host-local
NAT. An image on real hardware needs ADR-0082 or an authenticating TLS proxy
first. Recorded as a decision rather than a README note because it is the one
thing about these images that can hurt someone.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Cargo feature set (`rust/imzero2/Cargo.toml`) | added `fast_alloc`; `mimalloc` now optional | the four `build_rust_headless*.sh`; `main.rs` allocator cfg; `default` |
| `scripts/ci/rust_imzero2_check.sh` | four feature sets gain `,fast_alloc`; new allocator-free check | — |
| New build entry point | `showcase/gokrazy/build.sh` + two `config.json` | `.gitignore` for `_stage/`, `builddir/`, `*.img` |
| Prebuilt-binary reuse | the airgap static ffmpeg becomes an appliance input | `IMZERO2_STATIC_FFMPEG` override; no new fetch lane |

No wire format, generated-code input or registry changes; `IMZERO2_*` variables
used here are all already in the ADR-0009 registry.

## Alternatives

- **NixOS image (O2).** 1.8–2.8 GiB against 109 MB, and it remains the right
  answer for the showcase box that also runs ClickHouse. Not superseded.
- **Buildroot / from-scratch (O4).** Same closure as gokrazy with no update
  story and a second toolchain to own.
- **OCI container (O5).** Registry-coupled, and ADR-P-0001 already recorded a
  hard preference against containers for this line.
- **One image with a codec flag.** Cheaper to build and strictly weaker: it
  asserts the fallback instead of demonstrating it, and cannot show that an
  image with no ffmpeg in it still serves.
- **A walkers feature gate to reach musl now.** Duplicates ADR-0204 M4 and
  collides with it. §SD4.
- **`musl-gcc` on the build host.** Packaged in Fedora, one install away, and it
  would have made §SD4 moot — but it buys a static binary the image does not
  need, at the cost of a cross C toolchain in the build path.

## Consequences

### Positive

- A GPU-less appliance that produces **pixels**, which ADR-0128 M3 could not:
  109 MB, no Vulkan loader, no ICD, no Mesa.
- The ffmpeg half of ADR-0128's closure is now **optional rather than assumed**,
  and the two images make the difference legible.
- The airgap lane's static ffmpeg gained a second consumer without a second
  build.
- `rust/imzero2` gained a checked configuration it did not have: the
  allocator-free build a musl target will need.

### Negative

- **`ExtraFilePaths` is a manual manifest.** The glibc closure is read from the
  binary, but the fonts, the ffmpeg and the destination layout are hand-written.
  A staging mistake fails at boot, not at build.
- **The images are not booted by CI.** §Verification.
- **A second deployment shape to keep working**, beside `showcase/onbox/` and
  ADR-P-0001's flake, with no shared machinery.
- **`GOWORK=off` is load-bearing and invisible.** §SD3.
- **The CJK font fallback is dropped** (~20 MB for glyphs the widget demo never
  draws). An image whose app needs them must stage a third font.

## Migration

Nothing to migrate. Both images are new, no existing deployment changes shape,
and `fast_alloc` is on by default in every build script so no shipped binary
changes.

- **Regeneration.** None. No FFI boundary moved, no generated-code input
  changed, no new `env.Spec` — so neither side needs rebuilding and
  `doc/env-vars.md` is unaffected.
- **Old shape.** A reader deploying via `showcase/onbox/` is unaffected; these
  images do not replace that path.

## Verification plan — Tier 1

- **Lane.** `scripts/ci/rust_imzero2_check.sh` (via `lint.sh`, via `lint.yaml`)
  covers the code half: the five shipped feature sets, plus a new
  `headless_soft` **without** `fast_alloc` — the allocator-free configuration
  a musl build needs, which nothing ships yet and which would otherwise rot
  unnoticed.
- **What would fail.** A feature edit that breaks the allocator gate; wgpu
  leaking into a GPU-less set (the existing `cargo tree` assertion).
- **The image half is manual, and was run.** 2026-08-22, both variants booted
  under QEMU:

  | | hello `codec` | AUs | mesh frames |
  | --- | --- | --- | --- |
  | `boxer-soft-video` | `""` → H.264 | 30 | 0 |
  | `boxer-soft` | `"mesh"` | 0 | 30 |

  The H.264 capture decodes to distinct frames; the widget gallery renders with
  both fonts; the in-frame status bar reports `codec: H.264` and **1.1 ms**
  per frame in Rust at 1920×984, against ADR-0205's 1.22 ms at 1280×800. The
  no-ffmpeg image advertised `codec="mesh"` with nothing configuring it, and its
  first mesh frame is the 1,048,605-byte atlas bootstrap ADR-0128 M1 describes.

- **Gap: no CI boot.** Booting an image needs QEMU and ~2 GiB per variant per
  run; the lane would be slow and shared-runner flaky. A staging or font
  regression therefore reaches a human, not a red build. Accepted for a
  demonstrator; a shipped appliance should revisit it.
- **Gap: no size or per-frame assertion.** A regression that doubled either
  would merge. `IMZERO2_HEADLESS_RASTER_STATS=1` makes the second a one-command
  manual check.

## Milestones

- **M0 — the images.** ✓ (2026-08-22) `showcase/gokrazy/` — `build.sh`, two
  `config.json`, README; both variants build (109 MB / 119 MB).
- **M1 — the allocator gate.** ✓ (2026-08-22) `fast_alloc`, all four build
  scripts, and the allocator-free check in the CI lane.
- **M2 — boot and contrast.** ✓ (2026-08-22) both variants booted; the H.264 vs
  mesh table in §Verification.
- **M3 — a ClickHouse-backed variant.** Not started. Answers
  [ADR-0134](./0134-adhoc-datasets.md) SD8's question, deferred *to this probe*:
  whether `clickhouse-local` rides the A/B root images or parks under `/perm`.
  The current images come up `facts:mem` / `persist:mem`, which is the baseline
  to diverge from.
- **M4 — musl-static.** Blocked on ADR-0204 M4 (§SD4). `fast_alloc` is the half
  that could land early; `ring` is the half that cannot.

## Status

Proposed 2026-08-22. Recorded after the probe rather than before it, which is
what ADR-0128 M3 asked for — the probe existed to find out whether this was
possible at all, and §Context records the measurement that changed the answer.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.

## References

- [ADR-0128](./0128-imzero2-mesh-draw-stream-codec-lane.md) — the appliance force, the mesh lane, and the M3 deferral this ADR discharges.
- [ADR-0205](./0205-imzero2-cpu-rasterized-pixel-host.md) — the CPU rasterizer that made a pixel-producing appliance possible; M6 is the milestone this splits.
- [ADR-0204](./0204-leaflet-map-core-port.md) — M4 removes walkers, and with it the remaining musl blocker.
- [ADR-0134](./0134-adhoc-datasets.md) — SD8's `/perm` question, deferred to this probe.
- [ADR-0082](./0082-imzero2-remote-session-auth-tls.md) — the auth/TLS gap behind §SD5.
- [ADR-0009](./0009-environment-variable-registry.md) — the registry the image's `IMZERO2_*` settings come from.
- [`showcase/gokrazy/README.md`](../../showcase/gokrazy/README.md) — how to build and boot.
- [gokrazy](https://gokrazy.org/) — upstream.
