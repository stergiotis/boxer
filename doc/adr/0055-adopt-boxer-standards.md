---
type: adr
status: proposed
date: 2026-04-23
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0055: Adopt boxer coding & documentation standards via go.mod pin

## Context

pebble2impl historically had implicit conventions: `.golangci.yml` enforced
some rules, `scripts/ci/` ran lints, and `doc/adr/` accumulated ADRs in an
ad-hoc front-matter format. There was no root `CLAUDE.md`, no
`CODINGSTANDARDS.md`, no `DOCUMENTATION_STANDARD.md`, and no template
library. Three `DESIGN.md` files coexisted with three packages, despite
being a banned filename in the user's other (canonical) standards repo.

[`github.com/stergiotis/boxer`](https://github.com/stergiotis/boxer) is the
sibling open-source project owned by the same author. It carries
authoritative `CODINGSTANDARDS.md` and `doc/DOCUMENTATION_STANDARD.md`
(Diátaxis + ADR + YAML front-matter), a `doc/templates/` library, and a
`gov doclint` enforcement tool. pebble2impl already imports boxer as a Go
module and uses go.work locally to point at `../boxer`.

The user's stated objective is to align pebble2impl with the boxer
standard, executed in four steps:

1. lock the standard for new (Claude-assisted) work,
2. assess gaps,
3. migrate documentation,
4. migrate code.

This ADR captures the decision adopted in step 1 and the gap inventory
produced in step 2. Steps 3 and 4 will execute the migration backlog
recorded below.

## Design space (QOC)

**Question.** How should pebble2impl carry boxer's coding and documentation
standards: by reference (pinned via Go module mechanisms) or by some form of
local copy?

**Options.**

- **O1** — Pin via `go.mod` and resolve through `go list -m -f '{{.Dir}}'`.
  Locally `go.work` resolves to `../boxer`; in CI the GOMODCACHE path of the
  pinned module version. Tools (`gov doclint`) are invoked through `go run
  github.com/stergiotis/boxer/public/app …`.
- **O2** — Commit copies of boxer's `CODINGSTANDARDS.md`,
  `doc/DOCUMENTATION_STANDARD.md`, and `doc/templates/` to pebble2impl,
  plus a vendored or rebuilt `doclint` binary.
- **O3** — Commit symbolic links from pebble2impl to `../boxer/...` paths.

**Criteria.**

- **C1 — Drift risk.** How easily can the two repos disagree on the standard?
- **C2 — CI portability.** Does the approach work without a sibling boxer checkout?
- **C3 — Contributor friction.** What setup must a fresh contributor perform?
- **C4 — Text duplication.** How many bytes of boxer's standard live in pebble2impl?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 (go.mod pin)    | O2 (copy)        | O3 (symlinks)         |
|----|--------------------|------------------|-----------------------|
| C1 | ++ (pin)           | −− (manual sync) | + (live)              |
| C2 | ++                 | ++               | −− (dangle)           |
| C3 | + (`go.work` once) | ++ (zero)        | − (sibling clone)     |
| C4 | ++ (zero)          | −− (full)        | ++ (zero)             |

## Decision

We adopt boxer's `CODINGSTANDARDS.md` and `doc/DOCUMENTATION_STANDARD.md`
as authoritative for pebble2impl, referenced via the `go.mod` pin on
`github.com/stergiotis/boxer`. No standard text is copied into this repo.
A thin root `CLAUDE.md` indexes the canonical files and adds the
pebble2impl-local supplement (FFFI2, imzero2, WidgetHandle, etc.) that has
no boxer counterpart. Enforcement runs through `scripts/ci/doclint.sh`,
which invokes `boxer/public/app gov doclint` at the pinned version, both in
CI and via the Claude Code `PostToolUse` hook on every Markdown
write/edit.

The standard applies in full to all **new** code and documentation
authored after 2026-04-23. Existing content is grandfathered until step 3
(docs) and step 4 (code) bring it forward incrementally.

## Alternatives

- **Copy boxer's text into pebble2impl.** Rejected: drift risk is the
  failure mode this ADR exists to prevent. Bumping `go.mod` would not bump
  the standard, so the two repos would silently disagree.
- **Symlink to `../boxer/...` for human ergonomics.** Rejected as a
  committed artifact: CI checkouts have no sibling boxer and would fail.
  May be rehydrated locally (gitignored) by a developer-run script, which
  is non-blocking and out of scope for this ADR.
- **Fork the standard into a third "shared-standards" repo.** Rejected for
  now: only two consumers exist (boxer, pebble2impl), so the extraction
  cost exceeds the benefit. Reconsider when a third repo joins.

## Consequences

### Positive

- Bumping `github.com/stergiotis/boxer` in `go.mod` automatically picks up
  the latest standard text and `doclint` rules. No two-place updates.
- `scripts/ci/doclint.sh` and the `PostToolUse` hook are version-locked to
  the same boxer revision the rest of the codebase compiles against.
- `CLAUDE.md` is short (~80 lines) and reviewable in one pass; the deep
  body of rules lives in boxer where it is already maintained.
- Future repos depending on boxer can reuse the same wiring with zero
  per-repo standard text.

### Negative

- Reading the standards locally requires either `bash scripts/boxer-path.sh`
  followed by a Read of the resolved path, or a developer-run
  `rehydrate-boxer.sh` (not committed in step 1) producing absolute
  symlinks. Not a one-`cat` operation by default.
- Standard *changes* land in pebble2impl whenever boxer is bumped; this is
  the upside above, but it means a `go get -u boxer` run can introduce new
  doclint failures the contributor did not author. Mitigation: re-run
  `scripts/ci/lint.sh` immediately after any boxer bump.
- pebble2impl's `tags` file must continue to include `llm_generated_opus47`
  for `gov doclint` to compile; removing that tag silently breaks the gate.

### Neutral

- The hook fires on every `Write|Edit` of `*.md`; the first invocation in a
  session pays the `go run` build cost (~5–10 s) while later invocations
  hit the build cache. Acceptable for interactive work.

## Gap inventory (step 2 output)

Two snapshots, both captured by `scripts/ci/doclint.sh --min-severity info`
on 2026-04-23. The first is the raw walk; the second applies the
exclusions resolved below (§ "Scoping resolutions"), which is the figure
steps 3 and 4 actually need to drive down.

| Snapshot                | Lines | Notes |
|-------------------------|-------|-------|
| Raw walk (`.`)          | 3167  | Includes attic, autogen changelog, generated `*.out.*`/`*.gen.*`, and CLAUDE.md |
| Post-exclusion (default sweep) | 2251  | What `scripts/ci/lint.sh` and the hook actually evaluate |

### Markdown gaps (step 3 territory, post-exclusion)

| Rule  | Severity | Count | Notes |
|-------|----------|-------|-------|
| DL001 | error    | 30    | Missing/malformed front-matter; spans `doc/`, `doc/skills/*/`, `experiments/*/`, `src/go/public/.../README.md`, `src/rust/README.md`, `planning/`, `scripts/`, `sponsor_deps_out/` |
| DL007 | error    | 10    | Broken in-repo links; 9 in `doc/adr/0004-retire-alpha-cbor-for-jsonv2.md` (paths now under `attic/`), 1 in `doc/adr/0003-imzero2-unified-color-type.md` (refers to a `DOCUMENTATION_STANDARD.md` that does not exist locally — boxer's is the source) |
| DL011 | info     | 7     | Open `status: draft` set: ADR-0054, the three step-1 EXPLANATION migrations, VALUE_PROPOSITION.md, and the present ADR draft |
| DL005 | error    | 0     | Banned filenames cleared by step 1 |
| DL004 | error    | 0     | Banner state matches front-matter on all current docs |

### Go gaps (step 4 territory, post-exclusion)

| Rule  | Severity | Count | Notes |
|-------|----------|-------|-------|
| DL009 | info     | 2066  | Exported symbols missing doc comments (baseline cleanup) |
| DL009 | warn     | 138   | Existing comments not following form (wrong prefix or terminal punctuation) |

Worst single files (DL009 post-exclusion): `cbor/kvh/kvh.go` (102), `imzero2/egui2/components/egui2_enums.go` (100, hand-written despite the suffix; reclassify or generate), `boxerstaging/spinnaker/vdd/spinnaker_dimdata*.go` (~250 across four files), `boxerstaging/leeway/card/leeway_card_*.go` (~160 across four files), `cbor/stringencoder.go` (39).

### Convention divergences (sample-based, step 4 territory)

- **Receiver names.** ~228 functions use `s` as receiver, 45 `f`, 20 `t`, 12 `m`, plus tens of others. The boxer standard mandates `inst`. Mass rename required, mechanical but high churn.
- **Interface naming.** ~11 interfaces in `src/go/public` lack the mandatory `I` suffix (e.g. `Application`, `KeyGenerator`, `SliceEncoder`, `Style`, `Widget`). Manageable scale.
- **Error pattern `if err := f(); err != nil`.** 81 occurrences across 26 files (excluding `attic`, `*.out.go`, `*.gen.go`). Mechanical conversion to named-return + naked-return.
- **Enum `E` suffix, named return values, `eh.Errorf` adoption, `iter.Seq` iterators, sized-integer fields, SoA preference.** Not surveyed quantitatively in this pass; assume comparable scale and address opportunistically during code-migration sweeps.

### Scoping resolutions

Boxer's `gov doclint` has no built-in exclusion flag, so the following
exclusions are applied in our shell wrapper layer
(`scripts/ci/doclint.sh` for the default sweep,
`scripts/ci/doclint-hook.sh` for per-file Claude Code edits). Both
scripts share the same exclusion list so the gate and the hook agree on
scope.

1. **`doc/changelog/summaries/`** — excluded. Autogenerated by
   `summarize_gemini.sh` / `summarize_gemma4.sh`; treating their output
   as drift signal would either gate the changelog pipeline or force the
   summarizer to emit front-matter for transient artifacts.
2. **`attic/`** — excluded (matches both top-level `attic/` and any
   nested `**/attic/**`). Retired code per ADR-0053; not worth keeping
   green during steps 3–4.
3. **`CLAUDE.md`** — exempt. It is an agent-instruction file, not a
   Diátaxis artifact; conceptually the same role the standard already
   carves out for the repo-root `README.md`.
4. **Generated sources `*.out.*` and `*.gen.*`** — excluded. Mirrors the
   filter `scripts/ci/lint.sh` already applies to `go vet`,
   `staticcheck`, and `errcheck`. Note that some hand-written files
   carry suffix-shaped names (e.g. `egui2_enums.go`); these still get
   linted and may need reclassification or actual generation.

A future upstream contribution to boxer should add native exclusion
flags to `gov doclint`; our wrapper can collapse to a thin pass-through
once that lands.

### Migration order (proposed for step 3)

1. Fix the 10 DL007 broken-link errors in ADRs 0003 and 0004 — fast win,
   restores link integrity for the gate.
2. Sweep `doc/`, `doc/skills/`, `src/go/public/.../*.md`, `src/rust/`,
   `experiments/`, `planning/`, `scripts/` adding front-matter and
   classifying each file into a Diátaxis quadrant. README files become
   `type: reference`; SKILLS.md and skill-attached assets need a
   policy decision (probably `type: reference` with `audience: agent`).
3. Flip the migrated step-1 EXPLANATION drafts (and ADR-0054, ADR-0055,
   VALUE_PROPOSITION.md) to `stable` / `accepted` after a human review
   pass; remove the draft banner on each.

### Migration order (proposed for step 4)

Code migration runs per-package, lint-driven, after step 3 lands:

1. **Quick wins first.** Convert the 81 `if err := f(); err != nil`
   occurrences to the named-return pattern (mechanical, contained).
2. **Interface renames.** Append `I` to the ~11 interfaces lacking the
   suffix; fix call sites.
3. **Receiver rename pass.** Sweep non-`attic` packages converting `s` /
   `f` / `t` / `m` receivers to `inst`. Likely a single tooling-driven PR
   per top-level package.
4. **DL009 wrong-form warnings (151).** Fix existing-but-malformed doc
   comments. These are higher-value than the 2967 missing comments.
5. **DL009 missing-comment baseline (2967).** Add doc comments to exported
   symbols opportunistically; not a single-pass mass change.

`attic/` is out of scope for the rename passes; surviving callers of
attic'd packages should not be rewritten just to satisfy the standard.
Generated `*.out.go` / `*.gen.go` are out of scope unless the generator
itself produces non-conformant code.

## Status

Proposed — awaiting review by repo owner.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## References

- [ADR-0053](./0004-retire-alpha-cbor-for-jsonv2.md) — alpha/cbor retirement; the `attic/` paths created by this ADR are the source of the DL007 link breakage caught above.
- [`github.com/stergiotis/boxer`](https://github.com/stergiotis/boxer) — canonical standards.
- Step 1 commit: `2e38bf27 docs(standards): adopt boxer coding & doc standards (step 1)`.
