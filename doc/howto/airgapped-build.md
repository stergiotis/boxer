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
libraries. **Rust does not quite**: its ~660 crates vendor cleanly (all from the
registry — there are no git dependencies), but the wgpu/winit stack leaves a thin
residue that no `cargo vendor` can supply — a **C compiler at build time**
(`libmimalloc-sys` compiles bundled C) and a **Vulkan loader + ICD at runtime**
(`ash` dlopens `libvulkan.so.1`). That residue, plus the toolchains themselves,
is what the bundle and this guide are mostly about.

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
`cmake`/`nasm`/a static libc simply produces a bundle without it, and the old
environment-provided behaviour returns. `--no-ffmpeg` opts out deliberately.
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

**Not bundled and the target still needs** (no language vendoring covers these):

- *Build time, `full` scope only:* a C compiler (`cc`/`gcc`/`clang`) and
  `pkg-config`. Distro packages.
- *Runtime:* a Vulkan loader + ICD for the wgpu head — a hardware driver (see
  [How to enable AMD hardware video encoding](./amd-hardware-video-encoding.md))
  or `lavapipe` for software rendering. Without an ICD the imzero2 head will not
  start, though headless pixel streaming ([ADR-0024](../adr/0024-imzero2-remote-access-browser-viewer.md))
  still drives off it.

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

- **`GOTOOLCHAIN=local` is load-bearing.** `go.mod` pins `toolchain go1.26.5`;
  without `GOTOOLCHAIN=local` the `go` command tries to *download* that
  toolchain when the running one differs. The env file sets it.
- **Vendoring here is a packaging carve-out.** The repo's standing policy is no
  vendoring ([ENGINEERING_PRACTICES §6](../ENGINEERING_PRACTICES.md)); the
  `vendor/` and `rust/vendor/` trees live only inside the bundle and are not
  committed.
- **Toolchains are dynamically linked.** The shipped `rustc` (and the `go` tool)
  expect a compatible glibc and the same CPU architecture as the build host.
  Across distro families, prefer `go-only` (Go binaries are static) or run the
  bundle script on a host matching the target. The bundled `tinygo` and `ffmpeg`
  are the exceptions — both static, so they constrain architecture only.
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
