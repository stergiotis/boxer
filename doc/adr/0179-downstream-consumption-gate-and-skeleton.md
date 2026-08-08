---
type: adr
status: accepted
date: 2026-08-08
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-08
---

# ADR-0179: A gate and a reconciled skeleton for repositories that consume boxer

## Context

Repositories that depend on boxer adopt its standards *by reference* — the
`go.mod` pin carries the standards text, the document templates, the `doclint`
and `codelint` rules, and the library conventions, so none of those can go
stale. One layer is exempt: the repository mechanics each consumer copies by
hand — `./tags`, the CI lint script, the launcher, the path helper, the editor
hook.

That layer drifted. Two consumer repositories were observed carrying build-tag
families boxer retired in [ADR-0083](0083-retire-llm-generated-build-tags.md)
and [ADR-0106](0106-identity-fibonacci-tags-build-tag-retirement.md); one of
them mandates the retired marker in its own `accepted` ADR, so an accepted
decision downstream contradicts an accepted decision here. Separately, boxer's
[lint.sh](../../scripts/ci/lint.sh) is 428 lines and a consumer's independent
reimplementation is 317 — what diverged is not the checking logic, which is
already Go and already repo-agnostic, but the step list.

The build-tag half is closed: `gov buildtags` publishes required, optional and
retired sets through the module pin and is gated in boxer's own lint run. This
ADR decides the remaining two questions — how the *step list* reaches a consumer,
and how the copied files stay reconciled. The evidence, the measurements, and
the options that were rejected on mechanics are in
[doc/adr-background-work/downstream-adoption-skeleton.md](../adr-background-work/downstream-adoption-skeleton.md);
this record carries only the decision.

The choice is largely one of taste and practicality rather than of forced
constraint. The one question with a genuine trade-off — what reconciliation does
about deliberate local divergence — is the QOC below.

## Design space (QOC)

**Question.** When `gov skeleton --check` finds a consumer's copy of an emitted
file differing from what this boxer would emit, what happens?

**Options.**

- **O1** — **Fail hard, one seam.** Emitted files are `DO NOT EDIT`; the emitted
  `scripts/ci/lint.sh` unconditionally sources an optional, never-generated
  `scripts/ci/lint-local.sh` for the consumer's own steps. Any other divergence
  fails. (chosen)
- **O2** — **Fail hard, no seam.** Divergence always fails; a consumer needing a
  local step must upstream it into boxer.
- **O3** — **Suppression list.** A tracked file names paths exempt from
  reconciliation.

**Criteria.**

- **C1 — Drift resistance.** Can a consumer's copy silently fall behind?
- **C2 — Local needs.** Can a consumer add a Rust crate, a capture pipeline, or
  a hardware gate without fighting the tool?
- **C3 — Upstream pressure.** Does a generally-useful local step tend to reach
  boxer, or accumulate downstream?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 (one seam) | O2 (no seam) | O3 (suppressions) |
|----|---------------|--------------|-------------------|
| C1 | ++            | ++           | −− (a suppression is drift with permission) |
| C2 | ++            | −−           | ++                |
| C3 | +             | ++           | −                 |

O2 is rejected on C2 alone: a consumer with a Rust crate needs a local step on
day one, and a tool that forbids it will simply be abandoned. O3 reintroduces
the failure this ADR exists to remove — an exemption is a copy nobody rechecks.
O1 concedes exactly one seam, in one known place, at a small cost to C3.

## Decision

We will publish the universal lint step list as **`gov gate`**, a boxer
subcommand a consumer composes into its own entry point, and reconcile the
copied repository mechanics with **`gov skeleton --check | --write`**, which
emits those files carrying a `DO NOT EDIT` header and fails on drift.

`gofmt` and `go vet` stay outside `gov gate`, in the consumer's thin wrapper:
they are the two checks that must still run on a tree too broken to build the
gate itself.

Emitted by boxer, reconciled: `scripts/boxer-path.sh`, `scripts/new-doc.sh`,
`scripts/ci/lint.sh`, the launcher, `CLAUDE.md`. Owned by the consumer, checked
but never generated: `AGENTS.md`, `./tags`, `public/app/main.go`. The split
follows from which files carry repository-specific content.

Per-repository lint configuration — exclusion sets in particular — is a
`gate.Config` struct literal in the consumer's `main.go`, not a configuration
file. The consumer already links boxer, so the config language is Go: typed,
compiler-checked, and hash-pinned through `go.sum`.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API under `public/` | added: `gov/gate`, `gov/skeleton` | consumers composing them into their entry point |
| Build and toolchain gates (`./tags`, the lint gate) | reshaped: the step list becomes a pinned artifact | boxer's own `scripts/ci/lint.sh`, which must call `gov gate` for the shared steps |
| `gov` subcommand registry | added: `gate`, `skeleton` | `public/gov/gov.go` |

## Alternatives

- **Environment variables as the source of the tag set.** Reintroduces the
  ambient-configuration failure [ADR-0009](0009-environment-variable-registry.md)
  exists to prevent, for the setting whose failure mode is a misleading
  "undefined" compile error. The file stays the source; the environment form is
  derived (`gov buildtags --print-env`).
- **Shipping the CLI as a `go tool` directive.** Blocked on mechanics, not
  taste: `go tool` accepts no `-tags` and ignores `-tags` in `GOFLAGS`, so a
  tag-requiring CLI cannot be delivered that way. Measured on go1.26.5. It
  unblocks itself when a consumer's tag set reaches zero.
- **Wrapping the `go` command.** `gopls`, `go mod tidy`, IDE run configurations
  and `go generate` subprocesses all invoke `go` directly; a wrapper only
  sometimes on the path gives inconsistent tag application, which is worse than
  consistently manual.
- **A configuration language (Dhall or similar).** Adds a second pinning system
  beside `go.mod`, a second toolchain to pin for airgapped builds, and a config
  layer [§2 of ENGINEERING_PRACTICES](../ENGINEERING_PRACTICES.md#2-static-analysis)
  declined — to configure a tag list, eight step names and a glob list.
- **Documenting the skeleton instead of generating it.** This is what exists
  today, and it is what drifted.

## Consequences

### Positive

- The step list stops being copied, so boxer breaks first when it changes.
- A boxer bump surfaces new rules and retired tags at the consumer's next lint
  run rather than at review.
- A new consumer repository is roughly 130 hand-written lines, none of it
  policy-bearing.

### Negative

- Composing boxer's governance commands into a consumer's binary pulls their
  dependency cones; a demo linking `gov` + `adr` + `dev` + `env` came to 28 MB.
  Whether an *app* binary should carry a dev-tool cone is unresolved, and the
  clean answer — a separate `tool`-directive entry point — is blocked until a
  consumer's tag set reaches zero.
- The split between universal and repository-specific steps is a judgement
  call. Every boxer-specific step that leaks into `gov gate` is one a consumer
  must then suppress; if the universal set cannot be held near the eight steps
  named here, the extraction is not paying for itself.
- One more `gov` surface to keep working.

### Neutral

- `gov buildtags` shipped ahead of this ADR. It changes no interface anyone
  builds against and fixes a live contradiction, so it did not wait; `gate` and
  `skeleton` do set contracts and do.

## Migration — Tier 1

- **Breaks.** Nothing in boxer. Downstream, a consumer's hand-written
  `scripts/ci/lint.sh`, launcher, `boxer-path.sh` and `new-doc.sh` are replaced
  by emitted equivalents; local steps move into `scripts/ci/lint-local.sh`.
- **Path.** Run `gov buildtags` and clear the findings; run
  `gov skeleton --write`; move any local lint steps into `lint-local.sh`; add
  `gov gate` to the wrapper. Per repository, not coordinated.
- **Regeneration.** None — no generator input changes and no FFI boundary moves.
- **Old shape.** Hand-written skeleton files keep working; nothing forces
  adoption. A consumer that never runs `gov skeleton` is exactly as it is today.

## Verification plan — Tier 1

- **Lane.** Default `go test`. `gov buildtags` has unit tests including one
  asserting its published sets against boxer's own `./tags`; `gate` and
  `skeleton` follow the same shape, with `skeleton --check` run against boxer's
  own tree so the emitter is exercised on a real repository.
- **What would fail.** A retired tag in any checked `./tags`; an emitted file
  edited in place; boxer's `./tags` carrying a tag the contract does not declare.
- **Gap.** Nothing verifies a *consumer's* tree from here — the gate runs where
  the consumer runs it. That is inherent to a pinned dependency and is why the
  checks must be cheap enough to leave switched on.

## Status

Accepted 2026-08-08.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [doc/adr-background-work/downstream-adoption-skeleton.md](../adr-background-work/downstream-adoption-skeleton.md) — evidence, measurements, rejected options.
- [ADR-0009](0009-environment-variable-registry.md) — the environment-variable registry this declines to route build tags through.
- [ADR-0083](0083-retire-llm-generated-build-tags.md), [ADR-0106](0106-identity-fibonacci-tags-build-tag-retirement.md) — the retirements the observed drift missed.
- [CODINGSTANDARDS § Entry Points](../../CODINGSTANDARDS.md#entry-points) — why a consumer composes into one binary rather than adding a second main.
