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
    plus the rustup-pinned channel-1.92 toolchain sysroot; the target compiles
    offline (`CARGO_NET_OFFLINE=true`, vendored sources).
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
  cannot be relocated into the bundle and ignores the 1.92 pin. The bundle
  requires a rustup-managed toolchain.

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

## Status

Accepted (2026-06-23; updated 2026-07-14).

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [How to build boxer on an airgapped host](../howto/airgapped-build.md)
- [scripts/dev/airgap-bundle.sh](../../scripts/dev/airgap-bundle.sh), [scripts/dev/airgap-unbundle.sh](../../scripts/dev/airgap-unbundle.sh)
- [ENGINEERING_PRACTICES §6](../ENGINEERING_PRACTICES.md) — the no-vendor policy this carves out from
