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

### 2026-08-25 — aarch64 supported; bundles declared native-only and enforced

The bundle now packs and provisions on **aarch64** as well as x86_64, and the
"native-only" property that was always true of it is now stated and checked
rather than left to the operator.

**What actually blocked aarch64.** Not the pinned artefacts: the Go SDK, TinyGo,
Grafana, `mcp-grafana` and `rclone` all publish `linux-arm64` releases, and each
`AIRGAP_*_SHA256_arm64` constant was already in place. It was the static `ffmpeg`
build, which demanded `nasm` unconditionally. `nasm` is an x86 assembler and
nothing in that component set reaches for it on another CPU — libaom gates its
whole nasm/yasm search on `AOM_TARGET_CPU` being x86/x86_64, SVT-AV1 gates it on
`HAVE_X86_PLATFORM`, libvpx's `which nasm` probe sits inside its x86 branch (which
is also the only reason the `which` *binary* was ever a prerequisite), and ffmpeg
looks for an `x86asmexe` only on x86. So the preflight refused hosts that could
build all four perfectly well. Because `airgap_preflight_ffmpeg_build` is what
gates the ffmpeg stage, the visible cost was every aarch64 bundle silently losing
its encoder. The requirement is now architecture-conditional: `nasm` plus `which`
on x86, GNU `as` on aarch64 — the assembler libvpx's `arm64-linux-gcc` target
actually names, and which libaom and ffmpeg need for their `.S` files regardless.

**Native-only, said out loud.** Nothing in a bundle cross-compiles, so the packing
host's CPU *is* the bundle's CPU. Two consequences follow, and both are now
enforced rather than documented:

- **Pack time.** `airgap_require_arch` refuses an unsupported CPU up front. The
  pinned-artefact stagers are each best-effort by design, which means an
  unsupported host did not fail — it succeeded, and produced a bundle quietly
  missing its pinned Go SDK, tinygo and ffmpeg. A refusal is strictly better than
  that discovery happening across an air gap.
- **Unbundle time.** `airgap_require_manifest_arch` refuses an extracted bundle
  whose `MANIFEST` arch is not the host's, before anything is positioned or run.
  With one architecture in circulation this check was pointless; with two it is
  the difference between one sentence naming both arches and
  `.../toolchains/go/bin/go: cannot execute binary file: Exec format error`
  several steps into provisioning. A `MANIFEST` with no `arch=` line predates the
  record and warns rather than refuses — such a bundle is far more likely to be an
  old one on its native host than a mismatched one.

**Two spellings, both deliberate.** `airgap_arch` yields Go's (`amd64`, `arm64`),
which is what release URLs and the `AIRGAP_*_SHA256_<arch>` families are keyed by;
`airgap_arch_uname` yields `uname -m`'s (`x86_64`, `aarch64`), which is what
bundle *filenames* carry and therefore what downstream `(scope, arch)` routing is
named after. An operator reading a tarball name expects the string their own host
prints, so the conventions stay separate even though one derives from the other.

**One incidental fix.** The stagers each expanded
`eval "sha=\$AIRGAP_X_SHA256_$arch"` directly. Under the `set -u` the wrappers
run with, a case arm added *without* its hash constant is then an `unbound
variable` that aborts the entire pack, rather than skipping one best-effort
artefact — a trap set for exactly the kind of change this entry makes.
`airgap_pinned_sha_for_arch` makes it a warning and a non-zero return.

### 2026-08-25 — the target-side contract is derived from the render head, not asserted

Three preflights told an airgapped operator the wrong thing, and the errors
pointed in opposite directions at once. All three are now derived from the
imzero2 feature set the bundle records, via `airgap_preflight_render_head`.

**What was wrong.** The `## Consequences` note below says the shipped toolchains
"expect a compatible glibc and the same CPU architecture", and §Context above
attributes to Rust a residue of "a C compiler at build time (`libmimalloc-sys`
compiles bundled C) and a Vulkan loader + ICD at runtime". That residue is real
for the **wgpu/winit** stack. It is not real for the head this bundle actually
builds. Measured 2026-08-25:

| feature set | crates | wgpu | builds with no `cc` | `libvulkan` | ffmpeg |
| --- | ---: | --- | --- | --- | --- |
| `headless` (what the bundle builds) | 161 | no | **yes** | no | unused |
| `headless_soft` | 166 | no | **yes** | no | **required** |
| `headless_wgpu` | 199 | yes | — | yes | **required** |

`--no-default-features --features headless` and `headless_soft` both complete a
release build with `CC` and `CXX` pointed at a nonexistent binary, producing a
binary whose entire dynamic contract is `libc`/`libm`/`libgcc_s`/`ld.so`. So:

- the **C compiler + `pkg-config`** check ran for `--scope full` and blamed
  `libmimalloc-sys` — a crate not in the graph. mimalloc sits behind imzero2's
  `fast_alloc`, which lives only in `default`, and every airgap build passes
  `--no-default-features`. The real causes of a C dependency are the wgpu graph's
  `wayland-sys` (which probes with `pkg-config`) and `fast_alloc` — independent of
  each other, which is why `cc` is now its own capability rather than a
  consequence of `wgpu`;
- the **Vulkan** check ran unconditionally, for a head that carries no wgpu;
- **ffmpeg** was treated as merely nice to have, when a rasterizing head has the
  encoder lane compiled in and spawns it as soon as `IMZERO2_HEADLESS_LISTEN` or
  `IMZERO2_HEADLESS_H264_OUT` is set.

**Why this is worth an entry rather than a quiet fix.** Asking for a Vulkan ICD
that nothing loads, while calling the one genuinely required binary optional, is
the most expensive way for a deploy contract to be wrong: a missing ICD is exactly
the thing that gets chased for hours on a host with no network to search from.

**The mechanism.** `AIRGAP_IMZERO2_FEATURES` is the single source of truth — the
bundlers pass it to cargo, the MANIFEST publishes it as `imzero2_features`, and
the unbundler derives requirements from it. Same doctrine as `tags`: the string
that built the artefact is the string that describes it, so the two cannot drift.
`airgap_render_head_caps` maps a feature set to `raster` / `wgpu` / `cc`; an
unrecognised set resolves to all three, so a bundle predating this record asks for
too much rather than promising too little.

**One source correction alongside it.** `headless.rs`'s module doc and four
inline comments said the PNG dump, the H264 sink, the encode probe and "every
video codec" live under `headless_wgpu`. The `#[cfg]` beneath them says
`headless_raster` — they predate ADR-0205's `headless_soft` split, and they are
the difference between "this head has no encoder" and "it does". Comments only;
no gate changed.

### 2026-08-27 — boxer declares its own head; the verify now compiles what ships

Two corrections to the entry above, both about `AIRGAP_IMZERO2_FEATURES`.

**Boxer's head is `headless_wgpu,fast_alloc`, not `headless`.** The 2026-08-25
entry gave that variable a single default for boxer and its downstream, and chose
the lean head. Wrong here: `airgap-unbundle.sh` runs
`rust/imzero2/build_rust_headless.sh` in **both** scopes — full scope builds it on
the target, go-only ships what it produced — and that script builds
`headless_wgpu,fast_alloc`. So this bundle really does need a Vulkan ICD at
runtime and a C compiler at build time, and its original unconditional preflights
were right, `libmimalloc-sys` attribution included, since `fast_alloc` is on here.
The derived-requirements machinery stays; each bundler now declares its own head
rather than inheriting a shared default, and the capability mapping treats a C
toolchain as independent of wgpu precisely because `fast_alloc` can require one on
its own.

**The offline verify was testing a feature set the bundle does not ship.** It
compiled a hardcoded `headless` while the target built `headless_wgpu,fast_alloc`
— so it passed while saying nothing about the binary the target would produce, and
the wgpu/naga subtree (the largest part of the graph, and the part with the C
dependency) was never exercised offline at all. It now compiles the declared head.
That is a pre-existing gap, not one the 2026-08-25 entry introduced.

The verify also moves onto `airgap_verify_imzero2_heads`, the primitive
hackathon2026 uses to check a whole menu of heads. Boxer declares no menu — its
unbundler runs a fixed script rather than taking a head argument — so here it
verifies one head; the point is that the toolchain pinning, the graded failure and
the timing report do not exist in two versions.

### 2026-08-28 — the ffmpeg sources ship, so the target can rebuild the encoder

`_airgap/ffmpeg-src` now carries the five pinned codec tarballs (~90 MB, 58 MB of
it openh264) beside the static binary.

**It closes a claim the bundle was already making.** The 2026-07-28 entry above
put `build-static-ffmpeg.sh`, `verify-ffmpeg-lanes.sh` and
`bench-ffmpeg-lanes.sh` into the bundle so the target could "re-verify or rebuild
the bundled encoder binary without a boxer checkout". Only re-verify worked. The
source cache deliberately lives *outside* the staging tree so a re-pack does not
re-download, so only the linked binary travelled — and on the target the builder
died with `missing <src-dir>/<tarball> (pass --fetch, or stage the tarballs
first)`, where `--fetch` wants exactly the network that is absent. The bundle was
shipping a build script it could not run.

**Why it is worth the 90 MB.** ffmpeg is not optional for a rasterizing render
head: the encoder lane is compiled in under `headless_raster` and spawns it as
soon as the carrier or the H264 sink is configured (2026-08-25 entry). So the one
component the target cannot do without was also the only one it could not change
— against a bundle whose entire point is that boxer and hackathon stay editable
source. `--without-h264` drops openh264 and two thirds of the added weight for an
AV1+VP9 build.

Only the tarballs travel. The source cache doubles as the build's working
directory, so it also holds extracted trees, `_b/`, `_prefix/` and `_logs/` —
several hundred MB of host-specific output that must not leak into a bundle; CI
asserts exactly five tarballs and nothing else.

Sources are staged whether or not the binary built, which is deliberate: the
tarballs are fetched after the preflight but before the compile, so a host that
fails mid-compile still has sources to pass on, and a target with the build
toolchain can then produce what the packing host could not.

Verified by running the shipped builder against only the shipped tarballs with no
`--fetch`, i.e. touching no network at all. `--preflight-only` exits before
creating any directory, so CI can rehearse the rebuild against the bundle's own
source dir without polluting it.

### 2026-08-28 — a packed bundle can be augmented with a natively-built ffmpeg

`airgap_augment_ffmpeg` builds the static ffmpeg from the sources already inside
an extracted bundle, verifies every codec lane natively, installs it as that
bundle's binary and records where it came from. `--fetch-only` on
`build-static-ffmpeg.sh` is the pack-side half: stage the tarballs without
compiling, and without demanding a build toolchain.

**What it changes about "native-only".** Nothing in a bundle cross-compiles, so
the packing host has had to BE the target's architecture. ffmpeg is the one
component that escapes: its sources are architecture-independent, they travel
since the entry above, and building them needs **no network** — the tarballs are
right there, so `--fetch` is never passed. The native build can therefore be
deferred to a third host that is neither the packing host nor the destination:

| step | where | needs |
| --- | --- | --- |
| pack | connected, **any** arch | network, dhall, Go, Rust |
| augment | offline, **target** arch | a C/C++ toolchain, nothing else |
| provision | offline, target arch | no build toolchain at all |

The middle step needing no network is what makes it a different thing from
packing, and why it can run on a transit box, a build runner, or the target itself
before isolation.

**It verifies before it installs**, with the `verify-ffmpeg-lanes.sh` the bundle
already carries: nine checks over the four codec lanes plus the Annex-B sink. A
binary that fails is discarded and the bundle left unchanged. This is *stronger*
than the pack-time check it replaces, because it runs on the architecture that
will actually encode — the pack could only ever verify its own arch.

**Provenance is recorded, not assumed.** The `ffmpeg` line is rewritten and
`ffmpeg_augmented` appended, because a bundle whose binary did not come from its
packing host should say so — otherwise the MANIFEST implies a chain of custody
that did not happen.

**The encoder is delivered as a sidecar.** `airgap_emit_ffmpeg_sidecar` packages
it as its own small artefact and leaves the bundle byte-for-byte untouched;
`airgap_apply_ffmpeg_sidecar` installs one, needing no build toolchain, which is
what lets that half run on the airgapped target. Re-packing (`airgap_augment_ffmpeg`)
remains for handing on a single file.

Re-packing makes one signature cover work done by two parties — whoever assembled
the payload and whoever compiled the encoder — so the last signer vouches for both
and the augment host must hold a release key. A sidecar separates them: each party
signs what it produced, earlier signatures stay valid because earlier artefacts
never change, and the chain composes to any length (pack → re-sign → sidecar →
sign → verify both → apply → provision).

The binding is a digest over the bundle's codec tarballs plus the architecture,
re-computed at apply time. Binding to the *sources* rather than the bundle's bytes
is deliberate — the same encoder is correct for any bundle carrying those same
pinned sources for that arch — and the digest runs over a sorted listing so it
ignores directory order and build leftovers. It proves integrity, not
authenticity, so applying takes an optional trust anchor and checks the signature
before installing.

**Signing has an order now: pack → augment → sign.** Augmenting alters the
artefact, so a signature made at pack time stops verifying. That is the signature
working, not a defect, and the wrapper refuses rather than producing a bundle whose
own verifier rejects it. It never re-signs by itself: that needs a key belonging to
whoever owns the release, and silently re-signing would put the augment host
outside the trust boundary while acting as if it were inside.

**Scope.** ffmpeg only, and deliberately: it is the one native artefact whose
source is architecture-independent and small enough to ship. The general shape
would fit anything in the same position, but nothing else is.

## Status

Accepted (2026-06-23; updated 2026-07-31).

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [How to build boxer on an airgapped host](../howto/airgapped-build.md)
- [scripts/dev/airgap-bundle.sh](../../scripts/dev/airgap-bundle.sh), [scripts/dev/airgap-unbundle.sh](../../scripts/dev/airgap-unbundle.sh)
- [ENGINEERING_PRACTICES §6](../ENGINEERING_PRACTICES.md) — the no-vendor policy this carves out from
