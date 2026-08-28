---
type: how-to
audience: engineer packaging boxer for an offline host
status: draft
---

> **Status: draft — pre-human-review.** Not yet verified against the current documentation standard. Do not cite as authoritative.

# How to build boxer on an airgapped host

Boxer can be packaged so it builds — and runs — on a host with no network and
no Go or Rust package access, using only the two languages' native vendoring.
Two scripts implement it: [scripts/dev/airgap-bundle.sh](../../scripts/dev/airgap-bundle.sh)
packs a tarball on a connected host, and
[scripts/dev/airgap-unbundle.sh](../../scripts/dev/airgap-unbundle.sh) provisions it
on the target. The decision and trade-offs are recorded in
[ADR-0095](../adr/0095-airgapped-build-bundle.md).

One asymmetry shapes everything below. **Go vendors to a fully self-contained
offline build** — it is CGO-free, so it needs no C compiler and no system
libraries. **Rust can too, but only for some render heads**: its crates vendor
cleanly (all from the registry — there are no git dependencies), and the residue
`cargo vendor` cannot supply belongs to the **wgpu/winit** stack specifically — a
**C compiler at build time** (`wayland-sys` probes with `pkg-config`; `mimalloc`,
via `default`'s `fast_alloc`, compiles bundled C) and a **Vulkan loader + ICD at
runtime** (`ash` dlopens `libvulkan.so.1`).

The head this bundle builds by default, `--no-default-features --features
headless`, carries none of that: measured 2026-08-25, it builds *and links* with
`CC` pointed at a nonexistent binary, and its binary references no `libvulkan`.
So that residue is conditional, and the unbundler derives it from the feature set
the MANIFEST records — see the requirements table below.

This is the language-native route. A whole-system, fully hermetic alternative
(Nix — cryptographic reproducibility and near-free incremental transfer) is out
of scope here; these scripts deliberately use only Go's and Rust's own vendoring.

## When you need this

You want to build or run boxer on a host that cannot reach `proxy.golang.org`,
`crates.io`, or `static.rust-lang.org`, and you accept staging a tarball across
the gap (USB stick, one-way diode, review-gated copy).

## What the bundle carries — and what it does not

Pick a **scope**; it is the one decision that swings bundle size:

| Scope | Go | Rust (imzero2 head) | Ships | Use when |
| --- | --- | --- | --- | --- |
| `full` (default) | build from vendored source | build from vendored source | Go SDK + Rust toolchain + both vendor trees | developers must recompile Rust offline |
| `go-only` | build from vendored source | shipped **prebuilt** | Go SDK + Go vendor + one imzero2 binary | only the Go side changes offline |

`go-only` drops the Rust toolchain, the ~660 vendored crates, and the build-time
C-compiler requirement entirely — much smaller. Prefer it unless you genuinely
need to rebuild the Rust render host on the target.

**Provided by the environment, deliberately not bundled** (the deploy contract):
`systemd`, `clickhouse`, and `ollama` (the OpenAI-compatible API
endpoint). The unbundler preflights for these but does not supply them.

**`ffmpeg` is bundled** (it used to be on that list). The imzero2 headless
encoder needs a specific component set — rawvideo in, NUT out, the `lavfi` probe
path, `dump_extra`, the software encoders — and a distro build satisfying it
pulls in ~290 shared objects. `scripts/dev/build-static-ffmpeg.sh` produces one
static, self-contained binary (~20 MiB) into `_airgap/bin/ffmpeg`, and the
unbundler exports `IMZERO2_FFMPEG_BIN` pointing at it, so it never shadows the
system `ffmpeg` for anything else. It is **software-only by construction**: a
static binary cannot `dlopen`, which is how both VAAPI and NVENC load their
drivers. That costs nothing on a host without a GPU, and `CodecLane::best`
probes and falls back on its own. Packing is best-effort: a build host lacking
`cmake`/an assembler/a static libc simply produces a bundle without it, and the
old environment-provided behaviour returns. `--no-ffmpeg` opts out deliberately.
The assembler is architecture-dependent — `nasm` on x86_64, GNU `as` on aarch64,
since every codec here gates its nasm search on an x86 target;
`scripts/dev/build-static-ffmpeg.sh --preflight-only` names what is missing.
The pinned **source tarballs ship too**, at `_airgap/ffmpeg-src` (~90 MB), so the
`build-static-ffmpeg.sh` the bundle already carried can actually be run on the
target — without them a rebuild died for want of the tarballs, and only
re-verifying the shipped binary worked. `IMZERO2_FFMPEG_SRC` points at them and
the MANIFEST records `ffmpeg_src`.

Because those sources are architecture-independent and need no network to build,
the encoder can also be compiled *after* packing, on a host of the target's
architecture — which is how a bundle for a foreign arch gets an ffmpeg at all. The
library carries the pieces (`airgap_emit_ffmpeg_sidecar`,
`airgap_apply_ffmpeg_sidecar`, `airgap_augment_ffmpeg`); the downstream wrapper is
`hackathon2026`'s `scripts/dev/airgap-augment.sh`.
Verify a bundled binary with `scripts/dev/verify-ffmpeg-lanes.sh`.

**`tinygo` is bundled as a pinned upstream binary** at `_airgap/toolchains/tinygo`
— a whole `TINYGOROOT`, since TinyGo finds its root from its own executable and
nothing has to export `TINYGOROOT`. The wasm survey shells out to it, so without
it that tooling does not run offline. It cannot be produced the way the rest of
the bundle is (building TinyGo means building LLVM), so it is taken as an upstream
*prebuilt release artefact*: fetched by exact version and refused on a SHA-256
mismatch (pins in `scripts/dev/airgap-lib.sh`, downloads cached at
`.airgap-dl/`). That is weaker provenance than anything else in the bundle; the
pin and the `MANIFEST` entry are what make it auditable.

Packing compiles a wasm smoke module with the bundled tinygo *and* the bundled Go
SDK — the only check that the pair agrees — and **drops** a tinygo that fails it
rather than shipping one the target cannot use. The unbundler puts it on `PATH`
*and* points `BOXER_TINYGO` at it, so boxer's own resolution does not depend on
`PATH` order. Unlike `ffmpeg`, which is deliberately kept off `PATH` so it cannot
shadow the system one, an airgapped target has no other `tinygo` to displace.
`--no-tinygo` opts out; it is ~172 MB compressed, ~1.2 GB unpacked, which is
material against a `go-only` bundle. `linux-amd64` and `linux-arm64` are pinned;
any other architecture gets a warning and no tinygo.

**Not bundled and the target still needs** — conditional on the render head the
bundle carries, which `airgap-unbundle.sh` reads from the MANIFEST's
`imzero2_features` line rather than assuming:

| render head | C compiler + `pkg-config` (build, `full` scope) | Vulkan loader + ICD (runtime) | `ffmpeg` (runtime) |
| --- | --- | --- | --- |
| `headless` — the default here | no | no | not used (mesh lane only) |
| `headless_soft` — CPU raster | no | no | **required** |
| `headless_wgpu` — GPU raster | yes | yes | **required** |

For a wgpu head the ICD is a hardware driver (see
[How to enable AMD hardware video encoding](./amd-hardware-video-encoding.md)) or
`lavapipe` for software rendering; without one that head will not start.
`ffmpeg` turns from optional into required the moment the head can rasterize
(`headless_raster`): the encoder lane is compiled in and spawns it as soon as
`IMZERO2_HEADLESS_LISTEN` or `IMZERO2_HEADLESS_H264_OUT` is set. A bundle packed
before the MANIFEST carried `imzero2_features` is preflighted against the
strictest column.

## Prerequisites (on the connected build host)

- `go` — the signed release toolchain ([ADR-0085](../adr/0085-imzero2-demo-pull-build-atomic-deploy.md));
  its `GOROOT` is shipped verbatim.
- `full` scope: `cargo`/`rustc` installed **via rustup**, so the channel pinned
  by [rust/imzero2/rust-toolchain](../../rust/imzero2/rust-toolchain) can be
  shipped as an isolated copy. A distro-packaged Rust under `/usr` is refused —
  install the pinned channel with
  `rustup toolchain install <channel> -c rustfmt -c clippy`. Do not run that
  concurrently with a `cargo` invocation inside `rust/imzero2/`: the pin makes
  cargo auto-install the same toolchain, the two races roll each other back, and
  you are left with a half-installed sysroot.
- `git`, `tar`, and `zstd` (falls back to `gzip`).
- `curl`, to fetch the pinned `tinygo` release. Without it the bundle is packed
  without `tinygo` rather than failing.
- Network access **on this host only** — the pinned artefact is downloaded here
  and cached at `.airgap-dl/`, so a re-pack does not re-fetch it.
- Commit your work first: the source tree is taken from `git archive HEAD`, so
  uncommitted changes are not included (the two airgap files are copied in
  explicitly so a pre-commit bundle still works).

## Steps

```bash
# 1. Pack (on the connected host). Full scope compiles the vendored crates
#    offline with the toolchain the bundle ships (slow, and the only check that
#    the Rust half builds on the target); --skip-rust-verify opts out:
scripts/dev/airgap-bundle.sh --scope full
#    ...or the lean path:
scripts/dev/airgap-bundle.sh --scope go-only
#    ...dropping the bundled tinygo (~172 MB compressed):
scripts/dev/airgap-bundle.sh --scope go-only --no-tinygo
#    -> boxer-airgap-<scope>-<arch>-<date>.tar.zst

# 2. Transfer the single tarball across the gap (USB, etc.).

# 3. Provision on the target:
tar -I zstd -xf boxer-airgap-*.tar.zst        # or: tar -xzf ... for the .gz form
boxer/scripts/dev/airgap-unbundle.sh            # provisions toolchains + builds
```

The bundle script self-checks the **Go** vendor by building `./public/app` and
the imzero2 Go host offline before packing — the step most people skip. The
unbundler writes `boxer-airgap.env` (an offline-configured `GOROOT`/`PATH`, plus
`GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`, `GOFLAGS=-mod=vendor`, and in
`full` scope the Rust toolchain and a `CARGO_HOME` with the vendored-sources
config; plus `BOXER_TINYGO` and a `PATH` entry when the bundle carries a tinygo).
`source boxer-airgap.env` in any later shell to get the toolchains back.

## Verification

### Before shipping — on the build host, network truly down

You can confirm the bundle unpacks and builds offline without a target, by
running the unbundler inside a rootless network namespace (`unshare -rn`) so any
stray fetch fails loudly rather than silently succeeding:

```bash
work=$(mktemp -d)                 # if /tmp is tmpfs (RAM), use: mktemp -d -p /some/disk
tar -I zstd -xf boxer-airgap-*.tar.zst -C "$work"
cd "$work/boxer"
unshare -rn bash -c '
  # this probe should FAIL — the namespace has no connectivity:
  timeout 3 bash -c "exec 3<>/dev/tcp/1.1.1.1/443" && { echo "network up — not isolated"; exit 1; }
  ./scripts/dev/airgap-unbundle.sh          # provisions + builds, all offline
  source boxer-airgap.env
  ./app dev entry-points
'
cd - >/dev/null && rm -rf "$work"          # several GB in full scope
```

`airgap-unbundle.sh` is non-destructive: it positions the shipped toolchains inside
the extracted tree (`_airgap/toolchains/`) and points `CARGO_HOME` there too, so
it never touches your `~/.rustup` or `~/.cargo`. A same-host run proves the
offline build path but not cross-distro portability — the shipped toolchains are
dynamically linked (see Notes below).

### On the target, after `airgap-unbundle.sh`

```bash
source boxer-airgap.env
./app dev entry-points                 # the aggregate CLI runs
go build -tags "$(tr -d '\n' < tags)" -o /dev/null ./public/app   # rebuilds offline
```

`full` scope additionally produces `rust/imzero2/target/headless/release/imzero2`;
`go-only` installs the prebuilt one there. Launch the headless head with
`rust/imzero2/hmi_headless.sh` once a Vulkan ICD is present.

## Notes and limits

- **`GOTOOLCHAIN=local` is load-bearing.** `go.mod` declares `go 1.27.0`;
  without `GOTOOLCHAIN=local` the `go` command tries to *download* a matching
  toolchain when the running one differs. The env file sets it. (Until
  2026-08-19 `go.mod` also carried an explicit `toolchain` line; `go mod tidy`
  drops it once it equals the `go` directive, which changes nothing here —
  the `go` line drives selection either way.)
- **The bundle ships the packing operator's toolchain, unpinned.**
  `airgap_ship_goroot` copies `$(go env GOROOT)` verbatim and checks no
  version. Since the module requires 1.27, a bundle packed on a host whose
  default `go` is older produces a tarball that cannot build its own contents,
  and `GOTOOLCHAIN=local` makes that a hard stop on the target rather than a
  download: `go.mod requires go >= 1.27.0 (running go1.26.x; GOTOOLCHAIN=local)`.
  Pack on a host whose default `go` is 1.27 or newer. Running the bundler under
  a `GOTOOLCHAIN=` override is *not* enough — the override does not move
  `GOROOT`, which is what gets copied. Putting the toolchain's own `bin` first
  on `PATH` does move it, and is the fix when the system `go` is older:

  ```sh
  # A GOTOOLCHAIN fetch leaves a complete SDK in the module cache — but that
  # tree is read-only (dr-xr-xr-x), and airgap_ship_goroot copies with `cp -a`,
  # so pointing PATH straight at it ships a GOROOT the target cannot delete
  # without chmod. Copy it out and make it writable first.
  cache="$(go env GOMODCACHE)/golang.org/toolchain@v0.0.1-go1.27.0.$(go env GOOS)-$(go env GOARCH)"
  cp -a "$cache" ~/sdk/go1.27.0 && chmod -R u+w ~/sdk/go1.27.0
  PATH="$HOME/sdk/go1.27.0/bin:$PATH" go env GOROOT GOVERSION   # confirm before packing
  ```

  The usual route to a second SDK — `go install golang.org/dl/go1.27@latest`
  then `go1.27 download` — was not available as of 2026-08-20: the `dl` module
  had no `go1.27` wrapper yet. An unpacked release tarball from the downloads
  page works equally well and needs no chmod.

  There is a second version in play, and it is not the same one: the bundler
  writes the workspace's `go` line from the *module's* `go.mod`, not from the
  toolchain. With a workspace line below what a member requires, `go work
  vendor` fails at pack time before anything is shipped —
  `cannot load module …: go.mod requires go >= 1.27.0 (running go 1.26.5;
  GOTOOLCHAIN=local)`. Both have to move.
- **Vendoring here is a packaging carve-out.** The repo's standing policy is no
  vendoring ([ENGINEERING_PRACTICES §6](../ENGINEERING_PRACTICES.md)); the
  `vendor/` and `rust/vendor/` trees live only inside the bundle and are not
  committed.
- **Toolchains are dynamically linked.** The shipped `rustc` (and the `go` tool)
  expect a compatible glibc and the same CPU architecture as the build host.
  Across distro families, prefer `go-only` (Go binaries are static) or run the
  bundle script on a host matching the target. The bundled `tinygo` and `ffmpeg`
  are the exceptions — both static, so they constrain architecture only.
- **x86_64 and aarch64; native only.** Nothing in a bundle cross-compiles, so it
  is packed *for* the CPU it is packed *on*, and every pinned artefact publishes a
  linux release for both. Any other CPU is refused at pack time rather than
  yielding a bundle quietly missing its pinned Go SDK, `tinygo` and `ffmpeg` —
  the stagers are individually best-effort, so without that gate an unsupported
  host produces a broken bundle instead of an error. `airgap-unbundle.sh` then
  refuses a bundle whose `MANIFEST` arch is not the target's, before it positions
  or runs anything; a bundle predating that record warns instead.
- **`tinygo` is pinned, not built here.** It comes from an upstream release,
  verified against a SHA-256 in `scripts/dev/airgap-lib.sh`. Bump the version and
  its hash together; a bump with a stale hash fails closed and the bundle is
  packed without it.
- **Build/CI tooling is mostly excluded.** `golangci-lint`, `cyclonedx-gomod`,
  and the antlr grammar regen are not vendored — the bundle builds the product,
  it does not lint or regenerate it. Ship those separately if needed offline.
  `tinygo` is the one exception, bundled because the wasm survey is a *product*
  path rather than a governance one.
- **h3bridge wasm.** Its crate sources vendor in `full` scope, but *building* the
  `wasm32-unknown-unknown` artifact needs that target's std added to the
  toolchain. The committed `h3.wasm` is used at runtime regardless, so this only
  matters if you regenerate it offline.
- **Repeated transfers.** The bundle is a single artifact; re-bundling reships
  everything. If you stage to the same medium often, `rsync --partial` the
  tarball, or split the rarely-changing toolchains into a separate seed tarball
  from the frequently-changing source+vendor tarball.
