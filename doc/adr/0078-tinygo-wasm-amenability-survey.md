---
type: adr
status: proposed
date: 2026-06-12
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0078: TinyGo/wasm package-amenability survey

## Context

A recurring question has no answer today: **which of boxer's own Go packages
could compile to WebAssembly via TinyGo, and what blocks the rest?** This is the
*inverse* of [ADR-0003](./0003-h3-wasm-bridge.md), which runs a *foreign* (Rust)
wasm module *inside* boxer through wazero. Here we ask which of boxer's *own*
pure-Go packages could themselves *become* wasm guests.

The repo is unusually well-positioned to ask. [ADR-0003](./0003-h3-wasm-bridge.md)
and `CODINGSTANDARDS.md` commit it to **pure Go, no cgo, consumer-toolchain
neutrality**; a sweep confirms it holds (`import "C"` = 0, no `.s`, no
`//go:linkname`). So the blockers are not cgo — they are unsupported stdlib
imports, the reflect subset TinyGo implements, `unsafe`, the
`goexperiment.jsonv2` experiment the repo builds under, and external-module
dependencies. That is a tractable, classifiable surface.

**Relationship to [ADR-0077](./0077-keelson-browser-wasm-execution.md).** That
ADR decides how to run keelson *in the browser* (a dual-wasm Rust-host bridge)
and, along the way, rules out a **TinyGo guest** because the keelson stack —
`marshallreflect`, the json/v2 paths, gofakeit-driven tests — is reflection-
heavy. This ADR does **not** reopen that decision. It is the *instrument* that
turns ADR-0077's qualitative TinyGo rejection into a measured, per-package map,
and answers the broader portability question for code outside the keelson guest
path. (The number adjacency is incidental: ADR-0077 was committed by a parallel
session while this work was in flight, so this survey took 0078.)

Forces:

- **The import closure is GOOS-dependent.** A package's selected files — and
  therefore its imports and verdict — differ under `GOOS=wasip1` vs `GOOS=js`.
  Any survey must re-collect per target, not reuse a host-GOOS graph.
- **A graph already exists.** [ADR-0064](./0064-godepview-go-dependency-explorer.md)'s
  `godep`/`godepcollect` loads the transitive closure (build-tag aware) into a
  `Manifest` with a BFS-capable `Index`. Re-walking the import graph would
  duplicate it.
- **Three targets, three answers.** `wasi` (GOOS=wasip1), `js` (GOOS=js, browser),
  and `wasm-unknown` (freestanding, no host) differ in both file selection and
  the supported-stdlib surface; the survey must judge all three.
- **Heuristics drift; compilers do not.** A hand-curated "TinyGo supports X" list
  is an approximation of a moving target (TinyGo 0.39 today). The real compiler
  is ground truth — but needs the toolchain installed and is slow per package.

## Design space (QOC)

**Question.** How should a package's TinyGo/wasm amenability be determined?

**Options.**

- **O1** — Static import-graph classifier: seed each package from a curated
  TinyGo-support set, propagate the worst verdict up the import DAG.
- **O2** — Empirical compile-probe: wrap each package in a synthetic `main` and
  run `tinygo build` for the target, classify the outcome.
- **O3** — Both: static triage prunes the obviously-Red subtrees, the empirical
  probe confirms the survivors *(chosen)*.

**Criteria.**

- **C1 — Accuracy:** does the verdict match what TinyGo actually does?
- **C2 — Cost:** toolchain-free? fast over ~hundreds of packages?
- **C3 — Explainability:** can it name *why* and *where* a package is blocked?
- **C4 — Maintenance:** how exposed is it to TinyGo-version drift?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | −  | ++ | ++ |
| C2 | ++ | −  | +  |
| C3 | ++ | +  | ++ |
| C4 | −  | ++ | +  |

O3 dominates on accuracy and explainability and is only behind O1 on cost — and
that cost is recovered by using O1 *as the pruning stage of* O3: the static pass
removes the Red subtrees so the slow compiler only runs on plausible survivors.

## Decision

We add a `wasmsurvey` analysis command (`app code analysis golang wasmsurvey`,
sibling of `llmuse`/`stubber` under `public/code/analysis/golang/`) that, **once
per wasm target**, collects the closure under that target's GOOS, statically
triages it, and — when TinyGo is available — empirically confirms the survivors.
The report is a per-package, per-target verdict (green/yellow/red) with the
**transitive blame**: the shortest import path to the offending leaf. A
machine-readable JSON form is emitted alongside.

### Subsidiary design decisions

- **SD1 — Per-target re-collection (GOOS-aware).** Each target re-runs the
  collector under its `GOOS`/`GOARCH=wasm` so build-constraint file selection
  matches TinyGo. `wasm-unknown` reuses wasip1 file selection plus a stricter
  support set (it has no upstream GOOS of its own).
- **SD2 — Reuse `godepcollect` (ADR-0064).** The collector gains one field,
  `Config.Env`, forwarded to `packages.Config.Env`; nothing else about the
  collection path changes. The survey re-uses `godep.Index` BFS for blame.
- **SD3 — Static set is approximate; empirical overrides.** The curated support
  set (`support.go`) is a conservative seed sourced from TinyGo's stdlib-support
  matrix plus the structural facts (no sockets / no process model / no host).
  When the probe runs, its verdict stands; a `disagree` count surfaces every row
  where the real compiler refuted the guess (in either direction).
- **SD4 — Probe contract: exported API, package granularity.** The synthetic
  `main` references each exported function as a value (blank-import fallback when
  a package exports none), forcing TinyGo to compile the exported surface rather
  than just `init`. Dead-code elimination can still drop unexported code an
  exported function never reaches, so the verdict is "the exported API compiles
  and links," a necessary not fully-sufficient condition — and **package-level,
  not per-function**.
- **SD5 — `GOEXPERIMENT=jsonv2` carried into the probe.** The repo's json/v2
  experiment is set in the build env. Whether TinyGo honors it is the survey's
  open empirical question; a rejection is captured as the `goexperiment-jsonv2`
  reason, not a tool error.
- **SD6 — TinyGo is a dev/analysis-only dependency.** It is never required for a
  consumer `go build`; absent it, the survey reports static verdicts and says so.
  This preserves the ADR-0003 / CODINGSTANDARDS toolchain-neutrality the survey
  exists to measure.

## Consequences

- A new optional dev-tool dependency. **It must be a TinyGo that supports the
  repo's Go version**: Fedora's `tinygo` 0.39 caps at Go ≤1.25 and refuses Go
  1.26 outright (the survey preflights this and falls back to static with a clear
  message); upstream **0.41.1** supports Go 1.26 and produced the results below.
  The static path needs no toolchain at all.
- **[Recanted 2026-06-12 — see the Update below.]** The first pass reported the
  dominant wasm chokepoint as two pervasively-imported leaves (`os/exec` via
  `public/observability/eh`, `net` via `github.com/rs/zerolog`/`eh/eb`). That was
  wrong: both compile under TinyGo — the verdict came from a false-Red static
  seed the tool pruned before probing. The survey is still the instrument that
  makes real chokepoints visible and measurable; this one was a seed bug, not a
  chokepoint.
- The verdict set is a snapshot for a given TinyGo + the repo's current tags; it
  is reproducible (`--json`) and cheap to re-run as either moves.

### First empirical pass (TinyGo 0.41.1, 2026-06-12) — SUPERSEDED

> **⚠ Superseded 2026-06-12** (same day). The green/red counts below are wrong:
> the static `redStdlib` seed marked `net`/`os/exec`/etc. Red and the tool pruned
> them *before* the empirical probe, so those false-Reds were never compiled.
> Corrected counts and root cause are in the Update immediately after this
> subsection. Kept here as the record of what the buggy seed reported.

`--mode both` over 362 importable library packages (package main, test-only, and
internal/ packages excluded as un-probeable):

| target         | green  | red | notes                                                              |
|----------------|--------|-----|--------------------------------------------------------------------|
| wasi           | **73** | 289 | every probed candidate compiled — 0 genuine failures               |
| js             | **73** | 289 | identical to wasi                                                  |
| wasm-unknown   | 72     | 290 | only `hmi/progressbar` fails — `x/sys/unix` syscalls, no host      |

- **The static Yellow band was pure over-pessimism.** All 64 probed Yellows
  compiled Green; TinyGo's reflect subset covers boxer's actual usage. `both`
  mode collapses Yellow to zero — every uncertain package earns a real verdict.
- **`GOEXPERIMENT=jsonv2` compiles under TinyGo 0.41.1** (SD5 resolved): many
  Green packages reach `encoding/json`→json/v2 and build fine.
- **The Red wall is two leaves.** Of 289 Red: 108 reach `os/exec` (via `eh`), 92
  reach `net` (via `zerolog`/`eb`); the remainder are Arrow (~55) and
  `golang.org/x/tools` (~19). `--assume-clean eh,zerolog` lifts ~110 packages out
  of Red.
- Probe discipline (learned the hard way, encoded in the tool): the synthetic
  main lives at the module root (deeper placement makes `go` resolve the module
  from VCS), never imports test-only or internal/ packages, and scores
  infrastructure failures *inconclusive* rather than Red.

### Update 2026-06-12 — empirical pass recanted (false-Red static seed)

The first pass is wrong, and the bug is instructive. SD3 claims an imperfect
seed "costs at most a probe, never a wrong final verdict (in `both` mode)." That
holds for the **Yellow** seed (which gets probed) but **not the Red seed**: a
package matching `redStdlib`/`unsupportedExternalPrefix` is pruned as Red and
never compiled, so a false entry there is never overturned. The Red seed was
load-bearing and unvalidated.

Direct probing (TinyGo 0.41.1 / Go 1.26.4, wasi + wasm-unknown) shows the
flagged leaves all **compile and link**: `net` (TinyGo bundles `src/net` with
`IP`/`IPNet`/`HardwareAddr`), `os/exec` (stubbed), and `github.com/rs/zerolog`
including the `binary_log`/CBOR encoder and the `IPAddr`/`IPPrefix`/`MACAddr`
methods. They fail at *runtime* on wasm (no process model / no host sockets), but
the survey's verdict is compile+link (SD4) — so they are Green. Of the 13
`redStdlib` entries, **11 were false-Reds**; only `net/smtp` (references
`tls.Conn`, absent from TinyGo's `crypto/tls`) and `net/http/httputil` actually
fail to compile on wasi. The seed was corrected to those two (commit `18d46743`).

So the "portable core starts at eh+zerolog" framing was moot: a build-tag seam
that cut `os/exec`+`net` out of `eh`/`eb`/`cbor-builder` (commit `9eff543`) was
found unnecessary and **reverted** (`0ae7dd33`) — the unmodified packages compile
under TinyGo as-is (`eh`/`eb`/`cbor-builder` verdict: Green on all three targets).

**Corrected counts** — from the re-run materialized into the per-package
`packageprops` records (ADR-0080) after the seed fix:

| target              | compiles | blocked | (was)  |
|---------------------|----------|---------|--------|
| wasi (wasip1)       | **213**  | 148     | 73/289 |
| js                  | **211**  | 150     | 73/289 |
| wasm-unknown        | **216**  | 145     | 72/290 |

Green roughly tripled — ~140 packages left the Red wall. Two caveats remain:
the **external seed** (`unsupportedExternalPrefix`: Arrow, `golang.org/x/tools`,
…) is still static and unprobed, so it carries the same blind spot and the ~148
"blocked" may itself be inflated; and the prune-before-probe logic is unchanged —
currently harmless (the two surviving `redStdlib` entries are genuine) but it
will re-bite any future wrong seed. The clean fix is to probe seeded-Red packages
in `both` mode rather than prune them (see open question 6).

## Alternatives considered

- **Static only (O1).** Rejected as the *final* answer: it cannot tell a
  reflect-using package that happens to work from one that does not. Kept as the
  triage stage of O3.
- **Empirical only (O2).** Rejected: ~hundreds of packages × 3 targets of cold
  `tinygo build` with no pruning, and it still wants the graph to compute blame.
- **Per-function granularity.** Rejected for now: would require building each
  exported symbol in isolation and attributing failures — large cost for a first
  cut. Package granularity answers the asked question.
- **Wiring the verdict into the `godepview` GUI (ADR-0064).** Deferred: a column
  on the existing graph view is a clean follow-on, not a blocker for the report.

## Status — open questions

1. ~~**`GOEXPERIMENT=jsonv2` under TinyGo** (SD5)~~ — **resolved 2026-06-12**:
   TinyGo 0.41.1 honors it; json/v2-using packages compile Green.
2. ~~**Counterfactual ("what-if") view.**~~ — **shipped** as `--assume-clean
   <prefix,…>`, which treats matching packages as Green sinks (`eh,zerolog` ⇒
   ~110 packages leave the Red wall). Note (2026-06-12): for `eh,zerolog` this
   was never hypothetical — they are genuinely Green; see the Update.
3. **Persisting results as leeway/runtime.facts** (mirroring ADR-0064 SD7) — the
   report is markdown + JSON for now.
4. **`godepview` verdict column** — deferred (see Alternatives).
5. **External allow/deny list curation.** `support.go` seeds a short
   high-confidence list and defaults unknown externals to Yellow; the empirical
   pass is what resolves them. Worth growing the list as the probe teaches us.
6. **Red seed is pruned, not probed (2026-06-12).** A false `redStdlib` /
   `unsupportedExternalPrefix` entry becomes a wrong final verdict because
   seeded-Red packages are never compiled (see Update). `redStdlib` was
   corrected; `unsupportedExternalPrefix` (Arrow, `x/tools`) is still
   unvalidated. Fix: in `both` mode, probe seeded-Reds instead of pruning them.
