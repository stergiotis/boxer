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
[scripts/dev/airgap-unpack.sh](../../scripts/dev/airgap-unpack.sh) provisions it
on the target. The decision and trade-offs are recorded in
[ADR-0095](../adr/0095-airgapped-build-bundle.md).

One asymmetry shapes everything below. **Go vendors to a fully self-contained
offline build** — it is CGO-free, so it needs no C compiler and no system
libraries. **Rust does not quite**: its ~660 crates (including one git
dependency, `egui-snarl`) vendor cleanly, but the wgpu/winit stack leaves a thin
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
`systemd`, `clickhouse`, `ffmpeg`, and `ollama` (the OpenAI-compatible API
endpoint). The unpacker preflights for these but does not supply them.

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
- `full` scope: `cargo`/`rustc` installed **via rustup** (so the channel-1.92
  toolchain pinned by `rust/imzero2/rust-toolchain` can be shipped as an
  isolated copy). A distro-packaged Rust under `/usr` is refused — the script
  tells you to `rustup toolchain install 1.92 --component rustfmt clippy`.
- `git`, `tar`, and `zstd` (falls back to `gzip`).
- Commit your work first: the source tree is taken from `git archive HEAD`, so
  uncommitted changes are not included (the two airgap files are copied in
  explicitly so a pre-commit bundle still works).

## Steps

```bash
# 1. Pack (on the connected host). Full scope, verifying the Rust offline
#    compile too (slow — omit --verify-rust to skip it):
scripts/dev/airgap-bundle.sh --scope full --verify-rust
#    ...or the lean path:
scripts/dev/airgap-bundle.sh --scope go-only
#    -> boxer-airgap-<scope>-<arch>-<date>.tar.zst

# 2. Transfer the single tarball across the gap (USB, etc.).

# 3. Provision on the target:
tar -I zstd -xf boxer-airgap-*.tar.zst        # or: tar -xzf ... for the .gz form
boxer/scripts/dev/airgap-unpack.sh            # provisions toolchains + builds
```

The bundle script self-checks the **Go** vendor by building `./public/app` and
the imzero2 Go host offline before packing — the step most people skip. The
unpacker writes `boxer-airgap.env` (an offline-configured `GOROOT`/`PATH`, plus
`GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`, `GOFLAGS=-mod=vendor`, and in
`full` scope the Rust toolchain and a `CARGO_HOME` with the vendored-sources
config). `source boxer-airgap.env` in any later shell to get the toolchains back.

## Verification

On the target, after `airgap-unpack.sh`:

```bash
source boxer-airgap.env
./app dev entry-points                 # the aggregate CLI runs
go build -tags "$(tr -d '\n' < tags)" -o /dev/null ./public/app   # rebuilds offline
```

`full` scope additionally produces `rust/imzero2/target/headless/release/imzero2`;
`go-only` installs the prebuilt one there. Launch the headless head with
`rust/imzero2/hmi_headless.sh` once a Vulkan ICD is present.

## Notes and limits

- **`GOTOOLCHAIN=local` is load-bearing.** `go.mod` pins `toolchain go1.26.4`;
  without `GOTOOLCHAIN=local` the `go` command tries to *download* that
  toolchain when the running one differs. The env file sets it.
- **Vendoring here is a packaging carve-out.** The repo's standing policy is no
  vendoring ([ENGINEERING_PRACTICES §6](../ENGINEERING_PRACTICES.md)); the
  `vendor/` and `rust/vendor/` trees live only inside the bundle and are not
  committed.
- **Toolchains are dynamically linked.** The shipped `rustc` (and the `go` tool)
  expect a compatible glibc and the same CPU architecture as the build host.
  Across distro families, prefer `go-only` (Go binaries are static) or run the
  bundle script on a host matching the target.
- **Build/CI tooling is excluded.** `golangci-lint`, `cyclonedx-gomod`, and the
  antlr grammar regen are not vendored — the bundle builds the product, it does
  not lint or regenerate it. Ship those separately if needed offline.
- **h3bridge wasm.** Its crate sources vendor in `full` scope, but *building* the
  `wasm32-unknown-unknown` artifact needs that target's std added to the
  toolchain. The committed `h3.wasm` is used at runtime regardless, so this only
  matters if you regenerate it offline.
- **Repeated transfers.** The bundle is a single artifact; re-bundling reships
  everything. If you stage to the same medium often, `rsync --partial` the
  tarball, or split the rarely-changing toolchains into a separate seed tarball
  from the frequently-changing source+vendor tarball.
