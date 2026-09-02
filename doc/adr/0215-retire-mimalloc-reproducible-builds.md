---
type: adr
status: proposed
date: 2026-09-01
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** The change is implemented; this
> record has not been reviewed.

# ADR-0215: Retire mimalloc, and make the builds byte-reproducible

## Context

Nothing in this repo had ever checked whether a build reproduces. An audit on
2026-09-01 measured it — same machine, same tree, same flags — and found the
compilers innocent and the build recipes at fault.

**The render head could not reproduce at all.** Two release builds of
`rust/imzero2` (`--features puffin`), five minutes apart into separate target
directories, differed in **94 543 bytes**. The spread across `.rodata` (87 531),
`.rela.dyn` (3 925), `.text` (2 451), `.symtab` (606) and `.note.gnu.build-id`
(20) looks like deep codegen nondeterminism. It is one string. At `.rodata`
offset 2 400 465, between the mimalloc option names `page_max_candidates` and
`disallow_arena_alloc`, one build carries `18:22:02` and the other `18:26:56`;
both carry `Sep  1 2026`. mimalloc's own
`c_src/mimalloc/v2/src/options.c:222` (and `v3/src/options.c:236`) passes
`__DATE__, __TIME__` into a verbose message.
Eight characters shift the string pool, and every relocation after it moves.

mimalloc arrived through `fast_alloc`, which was in the desktop `default` and,
as its own manifest note said, in *every* build script. There was therefore no
shipped render head that could ever have been byte-reproducible, on any machine,
at any time.

**Everything else was path leakage.** The same binary carried 828 strings naming
the builder's `$CARGO_HOME` — panic-location file names, which survive
`debug = false`. And the headless heads, which run protobuf codegen in
`build.rs`, name their own `OUT_DIR`: two builds differing only in `--target-dir`
came out 56 157 bytes apart, the divergence confined to 134 LLVM
anonymous-symbol suffixes (`anon.d3dd7a28….llvm.<N>`) and the `.rodata` layout
that follows them. Built twice into the *same* path, wiped in between, that head
is byte-identical — so path remapping is the whole of the remaining problem.

**The one correct recipe was switched off.** `scripts/dev/build_h3_wasm.sh` has
always pinned `CONST_RANDOM_SEED`, passed `--locked`, remapped both path
prefixes and post-processed deterministically, and `scripts/ci/h3_wasm_parity.sh`
byte-compares a rebuild against the committed blob. Its header says
contributors skip and CI enforces. CI did not: the lint
workflow installs no Rust toolchain, no wasm target, and neither `wasm-strip`
nor `wasm-opt`, and the gate exits 0 with a *skipped* line when any of those is
missing. It had never run. Meanwhile `rust/h3bridge` pinned `channel = "stable"`,
which is now 1.98.0, while the committed `h3.wasm` carries `/rustc/31fca3adb…`
—
rustc 1.96.1. The gate existed to catch exactly that and could not report it.

**The Go side had no path discipline either.** No `go build` in the repo passed
`-trimpath`; `CGO_ENABLED` was left to the environment although it selects which
`net` and `os/user` implementations are compiled in; and `go 1.27.0` in go.mod is
a floor, not a pin — `GOTOOLCHAIN=auto` builds happily with a newer local
toolchain, and a different compiler is a different binary.

## Decision

**SD1 — `fast_alloc` and mimalloc are retired outright.** The feature, the
dependency, the `#[global_allocator]` in `main.rs` and the four build-script
mentions are gone; the `Cargo.lock` loses `mimalloc` and `libmimalloc-sys` and
nothing else. Do not reintroduce the feature. The manifest carries the reason
where the feature used to be, so the next person to want a faster allocator
reads the cost first.

**SD2 — Path remapping lives in one sourced file.**
[scripts/dev/rust-repro-env.sh](../../scripts/dev/rust-repro-env.sh) exports
`RUSTFLAGS` with `--remap-path-prefix` for `$CARGO_HOME` → `/cargo` and the
crate root → `/build`. Every build script sources it, and so does the h3 parity
gate — which previously carried its own copy of the same two flags with a
comment asking the reader to keep them in step. Cargo's `[profile.*] trim-paths`
would be tidier and would also cover a hand-typed `cargo build`, but it is
nightly-only as of cargo 1.98; when it stabilises, this file is the one place to
change.

**SD3 — Every cargo invocation passes `--locked`,** so a build compiles the
committed graph rather than whatever resolves that day.

**SD4 — Toolchain pins name the patch.** `rust/imzero2` moves from `1.96` to
`1.96.1`, `rust/h3bridge` from `stable` to `1.96.1` — the rustc that produced
the committed `h3.wasm`. A crate whose output is hash-checked wants the opposite
of the eframe template's advice against patch pins.

**SD5 — The h3 scripts name the channel on the command line** rather than
cd-ing into the crate. rustup resolves `rust-toolchain.toml` from the *current
directory*, and both scripts build from the repo root with `--manifest-path`, so
the new pin would otherwise have done nothing at all. cd-ing into the crate
would fix that too, but it is not equivalent: cargo then hands rustc a relative
source path instead of an absolute one, which changes the bytes the gate
compares.

**SD6 — One Go build environment.**
[scripts/dev/go-build-env.sh](../../scripts/dev/go-build-env.sh) exports the
tags, `-trimpath -buildvcs=auto`, `CGO_ENABLED=0`, and `GOTOOLCHAIN` pinned to
exactly the go.mod version *unless the caller already set one* — the airgap
environment exports `GOTOOLCHAIN=local` because it ships a single toolchain and
must not reach the network, and that setting is respected. All five build sites
(`boxer.sh`, `generate.sh`, `scripts/dev/build.sh`, `scripts/dev/generate.sh`,
`scripts/dev/lading-demo.sh`) source it. `-buildvcs` stays on as `auto` — for
one commit with a clean tree
the stamp is constant, and it is the only provenance the binary carries;
`true` would be a hard error inside a `git archive` export like the airgap
tarball.

**SD7 — CI installs what the gates need.** The lint workflow installs the pinned
rustup toolchain with the wasm target, plus `wabt` and `binaryen`. Without those
last two the h3 gate cannot reproduce the committed blob — it is the output of
`wasm-strip` + `wasm-opt -Oz` — and skips.

## Options considered

**O1 — Keep mimalloc, neutralise the macros.** `-Wno-builtin-macro-redefined
-D__DATE__=… -D__TIME__=…` on that one crate would work; mimalloc uses the
values only in a verbose message. *Against:* it keeps a C toolchain in the graph
for an allocator, leaves `libmimalloc-sys` as the last blocker on
`--target x86_64-unknown-linux-musl` (ADR-0205 M6), and makes every build depend
on a `CFLAGS` override that one forgotten script can drop. The failure mode is
silent.

**O2 — Keep mimalloc and accept unreproducible render heads.** *Against:* it
makes the whole exercise pointless for the artifact users actually run, and
leaves nobody able to tell a rebuild from a substitution.

**O3 — Retire it ✅.** The measured gain is a byte-reproducible render head and
one less C dependency; the measured cost is the allocator, worth roughly what
egui reported for it (emilk/egui#7029). A binary nobody can re-derive is worth
less.

## Consequences

### Positive

- The mimalloc-free heads reproduce: measured byte-identical across a wipe and
  rebuild in the same path, and the remaining path sensitivity is remapped away
  by SD2.
- `libmimalloc-sys` leaves the graph, which finishes ADR-0205 M6 — the appliance
  and musl shapes no longer need a cross C compiler for the allocator, and
  `scripts/ci/rust_imzero2_check.sh` loses its special allocator-free row
  because every row is now that row.
- The Go host builds byte-identically twice and embeds zero absolute paths
  (measured: `5219e143…`, twice, from `./public/app`).
- The airgap flow gets simpler: `AIRGAP_IMZERO2_FEATURES` drops to
  `headless_wgpu`, and the C-compiler line in the operator contract now has one
  cause (the wgpu/eframe wayland-sys probe) instead of two.

### Negative / residual

- **Allocation performance.** Every build now uses the system allocator. If a
  profile later says that matters, the answer is a different allocator with no
  build clock, not this one back.
- **blake3 is the last C dependence.** It compiles four assembly objects with
  the host toolchain when one is present, and falls back to pure Rust when none
  is (verified: `--features headless` checks clean with `CC=/nonexistent`). So
  the reproducible-build environment now includes "a C toolchain, or none, the
  same way as the reference build". It embeds no clock, so builds on one machine
  agree.
- **The committed `h3.wasm` is unverified against the new pin.** The parity gate
  still skips locally without wabt and binaryen. The first CI run under SD7 will
  say; if it drifts, rebuild with `scripts/dev/build_h3_wasm.sh` and commit.
- The lint workflow now installs a Rust toolchain and two apt packages, so it is
  slower and needs network on the runner.
