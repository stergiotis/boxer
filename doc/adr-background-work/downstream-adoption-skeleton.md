---
type: explanation
audience: contributor
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Consuming boxer downstream — notes toward a decision

Compiled 2026-08-08 from this repository plus two private consumer repositories
that pin boxer as a Go module, referred to as **C1** and **C2** (this repo is
public). The decision itself is largely taste; what follows is the part that
is not — the measurements, and the reasons the rejected options were rejected.

## The problem

"boxer's standards" is five layers, and only one of them drifts:

| Layer | Reaches a consumer via | Drifts? |
| --- | --- | --- |
| Normative text (CODINGSTANDARDS, DOCUMENTATION_STANDARD, ENGINEERING_PRACTICES) | the `go.mod` pin, read at `go list -m -f '{{.Dir}}'` | no |
| Templates under [doc/templates/](../templates/) | re-read from the module dir per document | no |
| Enforcement (`gov doclint`, `gov codelint`, `dev entry-points`, `adr`) | Go library packages | no |
| Library conventions (`env` registry, `eh`/`eb`, `packageprops`) | Go import | no |
| **Repo mechanics** (`./tags`, `scripts/ci/*.sh`, launcher, hooks) | **copied by hand** | **yes** |

Adoption-by-reference is already settled — C1 and C2 each carry an accepted ADR
choosing the module pin over a copy. It solved the first four layers and left
the fifth untouched, because nothing verifies a copy.

## What the drift cost

`./tags`, observed 2026-08-08:

| | |
| --- | --- |
| boxer | `boxer_enable_profiling,goexperiment.jsonv2` |
| C1 | those two **+ `identifier_tag_fixed16` + four `llm_generated_*`** |
| C2 | those two + five repo-local **+ `identifier_tag_fixed16` + three `llm_generated_*`** |

Both carry tag families boxer retired —
`identifier_tag_fixed<N>` by [ADR-0106](../adr/0106-identity-fibonacci-tags-build-tag-retirement.md)
(2026-07), `llm_generated_*` by [ADR-0083](../adr/0083-retire-llm-generated-build-tags.md)
(2026-06). C1's adoption ADR is `accepted` and *mandates* the
`//go:build llm_generated_<model>` marker that ADR-0083 exists to abolish and
that [AGENTS.md](../../AGENTS.md) now tells agents never to reintroduce. An
accepted decision in a consumer contradicts an accepted decision in the repo it
declares authoritative.

Same shape elsewhere: boxer's [lint.sh](../../scripts/ci/lint.sh) is 428 lines
and C2's independent reimplementation is 317; what diverged is not the checking
logic (`doclint`, `codelint`, `entry-points` are already Go and already
repo-agnostic) but the *step list* — which checks run, in what order, at what
severity.

## Two measurements

**Composition works.** A consumer's own `public/app/main.go`, 53 lines,
importing `gov.NewCliCommand()`, `adr`, `dev` and `env`, builds and runs; its
`gov doclint` / `gov codelint` report DL001, CS001 and CS005 against the
consumer's own tree. No shell-out, no second binary. The demo binary was 28 MB —
a dev-tool cone in an app binary is a real cost, unresolved.

**`go tool` delivery is blocked.** `go tool` accepts only `-n`, `-modfile`,
`-C`, `-overlay`, `-modcacherw` — no `-tags` — and ignores `-tags` in `GOFLAGS`.
Measured on go1.26.5: a `tool github.com/stergiotis/boxer/public/app` directive
fails with `encoding/json/v2: build constraints exclude all Go files`. Dated
deliberately; this is a Go limitation that may be lifted, so re-run it rather
than cite it.

## Options not taken

- **Env vars as the source of the tag set.** Reintroduces the ambient-config
  failure [ADR-0009](../adr/0009-environment-variable-registry.md) exists to
  prevent, for the setting whose failure mode is a misleading "undefined". File
  is source; derive the env (`gov gate --print-env`). No separate defaults file
  is needed — `env.Spec.Default` plus `env gen-docs` is one.
- **Wrapping `go` itself** (`boxer go build …`). `gopls`, `go mod tidy`, IDE run
  configurations, `go generate` subprocesses and CI actions all invoke `go`
  directly; a wrapper only sometimes on the path gives *inconsistent* tag
  application, worse than consistently manual.
- **A config language (Dhall or similar).** The consumer already links boxer, so
  Go structs are the config language — typed, compiler-checked, hash-pinned via
  `go.sum`. Dhall would add a second pinning system beside `go.mod`, a second
  toolchain to pin for airgapped builds, and the meta-runner-ish layer
  [§2 of ENGINEERING_PRACTICES](../ENGINEERING_PRACTICES.md#2-static-analysis)
  declined. Revisit for a multi-repo × multi-environment deployment matrix; not
  for a tag list, eight step names and a glob list.
- **Dropping `./tags`.** Nothing better exists: `go.mod` has no tags directive,
  `go env -w` is per-user, `GOFLAGS` is ambient.

## The tag endgame

`goexperiment.jsonv2` retires when `encoding/json/v2` graduates (go1.27). Cost
is one file carrying the constraint; the 34 files importing the package need no
change, as graduation preserves the import path. `boxer_enable_profiling` stays
— it gates a deliberate `profiling_enabled.go` / `profiling_disabled.go`
compile-out.

So boxer goes 2 tags → 1, but **a consumer goes 2 → 0**, since
`!boxer_enable_profiling` is the default arm. At that point `go tool` delivery
opens up for consumers — launcher and `GOFLAGS` export become optional for them,
though not for boxer.

## Candidate cuts

1. **`tags-retired` + a check.** A tracked file of retired tags with their ADR;
   the check asserts a consumer's set contains everything boxer requires and
   nothing boxer retired. Catches both live drifts on the first lint after a
   bump.
2. **`gov gate`.** The universal half of `lint.sh` as one command, called by
   boxer's own `lint.sh` so the shared path has one implementation. `gofmt` and
   `go vet` stay outside it deliberately — they must work on a tree too broken
   to build the gate.
3. **`gov skeleton --check | --write`.** Reconciliation, not scaffold-once:
   boxer emits the mechanical files with a `DO NOT EDIT` header and `--check`
   fails on drift, the gate boxer already applies to generated code pointed at
   layer five. boxer emits `scripts/boxer-path.sh`, `scripts/new-doc.sh`,
   `scripts/ci/lint.sh`, the launcher, `CLAUDE.md`; the consumer owns
   `AGENTS.md`, `tags`, `public/app/main.go`. This is what actually stops the
   drift.
4. **Lint config as a `gate.Config` field**, retiring the duplicated `grep -Ev`
   exclusion sets in consumers' lint scripts and editor hooks.
5. **An adoption template** — a how-to plus an ADR-0001 skeleton, so a
   consumer's adoption ADR starts from boxer's wording.

The resulting consumer skeleton is roughly 130 hand-written lines, none of it
policy-bearing.

## Open

**What `--check` does about deliberate divergence.** A consumer with a Rust
crate or a capture pipeline wants its own lint steps on day one. Fail hard and
push the variant upstream, or allow a suppression that becomes its own drift
surface? A middle option: the emitted `lint.sh` unconditionally sources an
optional, never-generated `scripts/ci/lint-local.sh` — one seam in one known
place. Unsettled, and the main thing an ADR would decide.

## Further reading

- [CODINGSTANDARDS.md](../../CODINGSTANDARDS.md),
  [doc/DOCUMENTATION_STANDARD.md](../DOCUMENTATION_STANDARD.md),
  [doc/ENGINEERING_PRACTICES.md](../ENGINEERING_PRACTICES.md) — the referenced text.
- [ADR-0083](../adr/0083-retire-llm-generated-build-tags.md),
  [ADR-0106](../adr/0106-identity-fibonacci-tags-build-tag-retirement.md) — the
  two retirements the consumers have not picked up.
