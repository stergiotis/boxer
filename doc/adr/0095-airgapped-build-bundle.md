---
type: adr
status: accepted
date: 2026-06-23
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-23
---

# ADR-0095: Airgapped build bundle via Go and Rust native vendoring

## Context

Boxer must build — and run — on hosts with no network and no Go or Rust package
access, with transfer over a staged medium (USB, one-way copy). We want a route
that uses only the two languages' own vendoring, kept distinct from the
whole-system hermetic option (Nix), which is heavier and out of scope here.

The deploy environment provides `systemd`, `clickhouse`, `ffmpeg`, and `ollama`
(the OpenAI-compatible endpoint); those are not bundled. Boxer is multi-language:
Go (the `app` CLI and the imzero2 Go host) and Rust (the imzero2 wgpu render
head), so a Go-only answer is insufficient.

## Decision

Two scripts produce and consume a self-contained tarball:
[scripts/dev/airgap-bundle.sh](../../scripts/dev/airgap-bundle.sh) packs on a
connected host; [scripts/dev/airgap-unbundle.sh](../../scripts/dev/airgap-unbundle.sh)
provisions and builds on the target. The recipe is
[doc/howto/airgapped-build.md](../howto/airgapped-build.md).

- **Go** — `go mod vendor` plus the shipped `GOROOT`; the target builds with
  `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`, `GOFLAGS=-mod=vendor`,
  `CGO_ENABLED=0`. Boxer's Go build is CGO-free, so it needs no C toolchain and
  no system libraries — fully self-contained. `GOTOOLCHAIN=local` is
  load-bearing: without it `go` tries to fetch the `toolchain go1.26.4` pin.
- **Rust (imzero2)** — two scopes:
  - `full`: `cargo vendor` (the workspace + h3bridge, including the `egui-snarl`
    git dependency, whose source-replacement stanza the generated config keeps)
    plus the rustup-pinned toolchain sysroot named by
    [rust/imzero2/rust-toolchain](../../rust/imzero2/rust-toolchain); the target
    compiles offline (`CARGO_NET_OFFLINE=true`, vendored sources).
  - `go-only`: ship imzero2 **prebuilt**, dropping the Rust toolchain, the
    vendored crates, and the build-time C-compiler requirement.
- **Non-vendorable residue** the environment must still supply (the unbundler
  preflights both): a C compiler + `pkg-config` at build time (`libmimalloc-sys`
  compiles bundled C; `full` scope only), and a Vulkan loader + ICD at runtime
  for wgpu (hardware driver, or lavapipe for software rendering).

## Alternatives

- **Whole-system hermetic (Nix).** Strongest for reproducibility and near-free
  incremental transfer, but requires Nix on the target and its system-lifecycle
  wins are NixOS-only. Evaluated separately; deliberately out of scope for this
  language-native route.
- **Packed module cache instead of `vendor/`.** Honours the repo's no-vendor
  policy ([ENGINEERING_PRACTICES §6](../ENGINEERING_PRACTICES.md)) but needs
  `GOMODCACHE`/`GOPROXY` wrangling on the target. Vendoring chosen for the
  simpler target experience; it is a packaging-only carve-out, never committed
  to `main`.
- **Distro packages (`.deb`/`.rpm`) or a container image.** Better native
  integration / single portable artifact respectively, but multiple packaging
  pipelines or GPU passthrough; out of scope.
- **Distro-packaged Rust.** Refused for `full` scope: a sysroot under `/usr`
  cannot be relocated into the bundle and ignores the `rust-toolchain` pin. The
  bundle requires a rustup-managed toolchain.

## Consequences

### Positive

- One tarball; the Go half builds offline with zero system dependencies.
- Validated end-to-end on a fresh tree: both `go-only` and `full` bundles packed,
  provisioned, and built offline with the shipped toolchains (the `full` path
  compiled the whole Rust crate set from vendored sources with the network off).

### Negative

- Vendoring conflicts with the standing no-vendor policy — confined to the
  bundle, never committed.
- `full` ships the toolchain plus all crate sources (multiple GB). Shipped
  toolchains are dynamically linked, so the target must match CPU architecture
  and carry a compatible libc; across distro families prefer `go-only` (static
  Go binaries) or run the bundle on a host matching the target.
- Build/CI tooling (`golangci-lint`, `cyclonedx-gomod`, antlr) is not bundled —
  the bundle builds the product, it does not lint or regenerate it.

### Neutral

- Environment contract: `systemd`, `clickhouse`, `ffmpeg`, and `ollama` are
  provided, not bundled; the OpenAI-compatible client points at the
  environment's ollama.

## Updates

### 2026-07-14 — core extracted to `airgap-lib.sh` (now consumed downstream)

The repo-agnostic primitives behind these scripts were extracted into
[scripts/dev/airgap-lib.sh](../../scripts/dev/airgap-lib.sh) (compressor
selection, `git archive` export, toolchain shipping, rust-sysroot resolution,
cargo vendoring, offline env-file/preflight helpers, and the Go vendoring
modes). `airgap-bundle.sh` and `airgap-unbundle.sh` are now thin wrappers over it.
Behaviour is unchanged — the generated `boxer-airgap.env` is byte-identical
pre/post refactor for both scopes (verified by diff).

The library gained a second Go dependency mode, **`workspace`** (a pruned
`go.work` + `go work vendor`, where the `use`d modules stay editable source and
only their external deps are vendored), alongside boxer's original
single-module `go mod vendor`; **boxer itself keeps single-module** and is
unaffected. The `workspace` mode exists for a **downstream consumer** that ships
boxer as a dependency and tracks an *unreleased* boxer ahead of its module pin:
such a consumer can now build its own airgap bundle by sourcing this library
(reference, don't copy) and shipping boxer + itself as one co-developed
workspace, rather than forking these scripts. This is a mechanism/packaging
change only; the ADR-0095 decision stands.

### 2026-07-28 — `ffmpeg` moves from environment-provided to bundled

The context above lists `ffmpeg` with `systemd`/`clickhouse`/`ollama` as
environment-provided. It does not belong with them: those are services the
deployment already runs, whereas `ffmpeg` is a *build-configuration* dependency
of the imzero2 headless encoder. The encoder needs a specific component set —
rawvideo in, NUT out, the `lavfi` probe path, `dump_extra`, the software
encoders — so "the target has ffmpeg" was never a sufficient contract; it had to
be an ffmpeg built the right way, and a distro build satisfying it pulls in ~290
shared objects.

`airgap-bundle.sh` now builds a static, software-only ffmpeg carrying exactly
that set ([scripts/dev/build-static-ffmpeg.sh](../../scripts/dev/build-static-ffmpeg.sh),
~20 MiB, no runtime library dependencies) into `_airgap/bin/ffmpeg`, and
`airgap-unbundle.sh` exports `IMZERO2_FFMPEG_BIN` at it. Pinning by env rather
than `PATH` is deliberate: the bundled build must not shadow the system `ffmpeg`
for every other tool on the target.

**Software-only by construction.** A static binary cannot `dlopen`, which is
precisely how libva and NVENC load their drivers, so no static ffmpeg can do
hardware encode. On a host without a GPU that costs nothing, and
`CodecLane::best` probes and falls back to the software lane on its own, so the
absent hardware lanes degrade silently and correctly. A deployment that needs
hardware encode wants a dynamically linked ffmpeg — a different artifact, out of
scope for a self-contained bundle.

Best-effort, matching the shipped-`syft` precedent: a packing host without
`cmake`/`nasm`/a static libc warns and produces a bundle that falls back to the
environment's `ffmpeg` exactly as before; `--no-ffmpeg` opts out deliberately;
the MANIFEST records which happened. Verify a bundled binary with
[scripts/dev/verify-ffmpeg-lanes.sh](../../scripts/dev/verify-ffmpeg-lanes.sh),
which replays the encoder's real argv rather than trusting `ffmpeg -encoders`.

### 2026-07-28 — the Rust offline compile becomes the default, not an opt-in

A `full` bundle reached a target and failed to build there:

```text
error: `std::f64::<impl f64>::mul_add` is not yet stable as a const fn
  --> h3o-0.10.0/src/math/functions-std.rs:58:5
```

`h3o` — a direct imzero2 dependency — calls `f64::mul_add` from a `const fn`,
which rustc const-stabilised in **1.94**. The bundle shipped and built with the
then-pinned channel 1.92. The pin is now **1.96**.

**What the original decision missed.** A pinned channel is a floor the crate
graph can silently outgrow. Cargo's MSRV-aware resolution normally guards that,
but only for crates that declare `rust-version`; `h3o` declares none, so
resolution happily picked a release the pinned toolchain cannot compile. Nothing
on the build host notices, because the host's own default toolchain is newer and
every ordinary `cargo build` uses it — the pinned sysroot is exercised on exactly
one path, and that path ran on the target, after the transfer, where there is no
network and no second toolchain to fall back to. The one place the breakage was
observable before shipping was the offline verification compile, and that was
`--verify-rust`: opt-in, off by default, skipped with a `NOTE` on stderr.

So the guarantee the `full` scope exists to provide — *this tarball builds on a
disconnected host* — was never actually checked at pack time unless someone
remembered a flag. **It is now the default.** `--skip-rust-verify` opts out and
warns that the resulting bundle may ship a Rust tree its own toolchain cannot
build.

This corrects the Positive consequence above. That end-to-end validation was
real, but it was a point-in-time observation, not an invariant: it said nothing
about a bundle packed months later against a drifted crate graph. The compile is
what turns it into a property of every `full` bundle, so the claim now holds by
construction rather than by recollection.

**Cost.** Packing `full` now takes an extra full release build of the imzero2
crate set — ~1 minute cold on a 32-thread workstation, longer on a modest CI
runner. That is the correct trade against shipping a tarball whose Rust half does
not compile, which cannot be diagnosed or repaired on the far side of an air gap.

### 2026-07-31 — `tinygo` bundled as a pinned upstream binary

The bundle now carries `tinygo` at `_airgap/toolchains/tinygo` (a whole
`TINYGOROOT`), by a route none of the decision above accounts for.

**The gap it closes.** The WASM survey
([ADR-0077](./0077-keelson-browser-wasm-execution.md),
[ADR-0078](./0078-tinygo-wasm-amenability-survey.md)) shells out to `tinygo`
through `extbin`, so on an offline target that tooling simply did not run. The
"Build/CI tooling is excluded" consequence below does not cover it: that carve-out
is about lint and regeneration, and the survey is a product path.

**Why the route is different.** Everything else in a bundle is vendored source,
compiled here from source (`ffmpeg`), or a copy of a toolchain the packing
operator already installed and trusts (the Go SDK, the Rust sysroot). TinyGo can
be had none of those ways — building it means building LLVM — so it is taken as an
upstream **prebuilt release artefact**: weaker provenance than anything else here,
and the first artefact the packing script itself downloads rather than compiles or
copies. (The shipped Go SDK is prebuilt too, but it arrives as a copy of an
install the operator chose; this arrives because this script fetched it.)

The pin is the whole of the compensating control: it is fetched from an
exact-version URL and refused unless its SHA-256 matches the constant in
`airgap-lib.sh`, downloads are cached at `.airgap-dl/`, and the `MANIFEST` records
what actually went in. A version bump means changing URL and hash together; a bump
with a stale hash fails closed. This does not make a prebuilt binary equivalent to
one built here — it makes the bundle reproducible with respect to a decision
someone made once, deliberately, and can re-audit. The fetch primitive is written
to be general, so a future tool in the same position lands on the same terms.

**Verified, not just shipped.** Packing compiles a wasm smoke module with the
bundled tinygo *and the bundled Go SDK*, which is the only check that those two
halves agree — a TinyGo release that has not caught up with the Go version in
`GOROOT` would otherwise first be discovered on the far side of the gap. Same
argument as the 2026-07-28 Rust entry above, at a much lower cost (seconds). It
**fails closed**: a tinygo that does not compile here is dropped from the bundle
rather than shipped, because the unbundler would otherwise pin `BOXER_TINYGO` at
it and mask whatever the target has.

**On `PATH` as well as env-pinned.** The 2026-07-28 entry established that a
bundled binary is reached by environment variable rather than `PATH`, so it cannot
shadow a system tool. That reasoning is specific to `ffmpeg`, a common system tool
many other things invoke, and does not generalise. TinyGo takes both hooks:
`BOXER_TINYGO` (a new `extbin` `OverrideEnv`,
[ADR-0118](./0118-extbin-external-process-chokepoint.md)) so boxer's own
resolution does not depend on `PATH` order, and `PATH` so an operator can run it
directly — an airgapped target has no other `tinygo` for that to displace.

**Costs:**

- **Size.** ~172 MB compressed, ~1.2 GB unpacked — material against a `go-only`
  bundle, whose reason to exist is being small. `--no-tinygo` opts out, mirroring
  `--no-ffmpeg`.
- **Architecture.** `linux-amd64` and `linux-arm64` are pinned; any other
  architecture gets a warning and no tinygo. The binary is statically linked, so
  unlike the shipped Go SDK and `rustc` it constrains architecture but not libc.
  Best-effort throughout, matching `ffmpeg`: what could not be obtained is warned
  about at pack time and recorded as absent in the `MANIFEST`.

## Status

Accepted (2026-06-23; updated 2026-07-31).

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [How to build boxer on an airgapped host](../howto/airgapped-build.md)
- [scripts/dev/airgap-bundle.sh](../../scripts/dev/airgap-bundle.sh), [scripts/dev/airgap-unbundle.sh](../../scripts/dev/airgap-unbundle.sh)
- [ENGINEERING_PRACTICES §6](../ENGINEERING_PRACTICES.md) — the no-vendor policy this carves out from
