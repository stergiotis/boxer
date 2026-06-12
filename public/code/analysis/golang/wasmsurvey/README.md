---
type: reference
audience: code-analysis tooling user
status: draft
---

> **Status: draft — pre-human-review.** Not yet verified against the current documentation standard. Do not cite as authoritative.

# wasmsurvey

## 1. Purpose

**`wasmsurvey`** classifies which of the module's own Go packages can compile to
WebAssembly under **TinyGo**, and explains why the rest cannot (ADR-0078).

It is the inverse of ADR-0003 (`doc/adr/0003-h3-wasm-bridge.md`): that work runs
a *foreign* (Rust) wasm module *inside* boxer via wazero; this asks which of
boxer's *own* pure-Go packages could themselves *become* wasm guests. Because the
repo is already cgo-free and consumer-toolchain-neutral (`CODINGSTANDARDS.md`),
the only blockers are unsupported stdlib imports, the reflect subset TinyGo
implements, `unsafe`, the json/v2 experiment, and external modules — which is
exactly what this tool measures.

A verdict is **package-level** and means "the exported API compiles and links"
for the target. That is a necessary, not fully-sufficient, condition — a package
can compile yet fail at runtime (see §5).

## 2. How it works

The pipeline runs **once per wasm target** (`wasi`, `js`, `wasm-unknown`),
because file selection — and therefore imports and verdict — depends on `GOOS`:

1. **Collect** the transitive import closure under the target's
   `GOOS`/`GOARCH=wasm`, reusing `godepcollect` (the ADR-0064 collector).
2. **Static triage:** seed each package from a curated TinyGo-support set
   (`support.go`), then propagate the worst verdict up the import DAG. Blame is
   the shortest import path to the offending leaf.
3. **Empirical confirm:** for each package the triage did not rule out, wrap it
   in a synthetic `main` referencing its exported functions and run
   `tinygo build`. This stage needs `tinygo` on `PATH`; without it the survey
   reports the static verdicts and says so.

Verdicts are green / yellow / red; `--mode both` (the default) does triage then
confirms, collapsing yellow to a real verdict. A `--json` form is emitted
alongside the text report.

## 3. Running it

The app is built and run through the repo's `boxer.sh` wrapper, which compiles
`./public/app` with the repo build tags. The command nests
`code → analysis → golang → wasmsurvey`:

```bash
# default: mode=both, all three targets, human report to stdout
./boxer.sh code analysis golang wasmsurvey

# full flag list
./boxer.sh code analysis golang wasmsurvey --help
```

**Empirical mode needs a TinyGo that supports the repo's Go version (1.26).**
TinyGo **0.41.1** is validated; Fedora's `/usr/bin/tinygo` 0.39 is too old, so
the survey preflights it and falls back to static. Put the right one first:

```bash
PATH=~/tinygo-dl/tinygo/bin:$PATH ./boxer.sh code analysis golang wasmsurvey
```

Useful variants:

```bash
./boxer.sh code analysis golang wasmsurvey --mode static                  # fast, no toolchain
./boxer.sh code analysis golang wasmsurvey --json -                       # machine-readable to stdout
./boxer.sh code analysis golang wasmsurvey --target wasi --show-green     # one target, list greens
./boxer.sh code analysis golang wasmsurvey --assume-clean <import/prefix> # counterfactual hypothesis
```

Build tags resolve automatically (`--tags` → `<root>/tags` → `GOFLAGS`), so you
normally omit `--tags`. Other knobs: `--patterns`, `--include-external`,
`--jobs`, `--timeout` (per-package probe, default 180s).

## 4. Materializing verdicts (`props`, ADR-0080)

The `props` subcommand group writes the verdict into a co-located
`package_props.go` in each package, so the result is committed and greppable:

```bash
# run the survey and (re)seed package_props.go in each in-scope package
./boxer.sh code analysis golang wasmsurvey props generate --overwrite

# read the committed declarations into a table / Go literal (no survey, no TinyGo)
./boxer.sh code analysis golang wasmsurvey props harvest --emit go --out -

# reconcile declared props against a fresh verdict; non-zero exit on a regression
./boxer.sh code analysis golang wasmsurvey props verify
```

`props verify` re-runs the survey, so with the 0.41.1 on `PATH` it doubles as the
empirical regression gate. `props harvest` does **not** run the survey or need
TinyGo — it only reads what is already committed.

## 5. Caveats

- **Compile-and-link, not runtime.** Green means the package *builds* for the
  target. TinyGo stubs or overlays things like `os/exec` and `net`, so they
  compile but fail at *runtime* on wasm (no process model, no host sockets).
  Read green as "portable to build," not "works."
- **The static Red seed is load-bearing.** Entries in `support.go`'s `redStdlib`
  / `unsupportedExternalPrefix` are pruned *before* the empirical probe, so a
  wrong Red entry is never overturned — only the Yellow seed gets probed. Keep
  the Red seed minimal and empirically justified. A 2026-06-12 audit found most
  of the original stdlib seed was false-Red (`net`, `os/exec`, `net/http`, …
  all actually compile); see the ADR-0078 Update. The external seed (Arrow,
  `golang.org/x/tools`) is still unprobed.
- **It is a snapshot.** A verdict set is for one TinyGo version + the repo's
  current tags; re-run as either moves. ADR-0078 carries the latest numbers.

## 6. See also

- `doc.go` — the package-level overview.
- `doc/adr/0078-tinygo-wasm-amenability-survey.md` — design, decision, and the corrected empirical results.
- ADR-0080 — the `packageprops` records that `props` reads and writes.
- ADR-0064 — `godepcollect`, the build-tag-aware import-graph collector this reuses.
- ADR-0003 — the foreign-wasm-via-wazero bridge this is the inverse of.
