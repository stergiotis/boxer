---
type: explanation
audience: prospective consumers and integrators evaluating adoption
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

> Where this page and an ADR disagree, the ADR is the record. The
> product-level statement is [positioning-statement](../positioning-statement.md);
> this page applies the same template to one subsystem. The set and its
> review checklist are in the [folder README](./README.md).

# gov — positioning

Positions the governance toolkit: the license gate over a software bill of
materials for Go and Rust, the lints whose rule ids point at the written
standards, the composite gate and the emitted skeleton a downstream
repository runs, and the capability and design-system checks that ride in
the same gate. The premise it enacts is that one architect can carry the
work only because correctness is machine-checked
([why-boxer](../why-boxer.md) P6).

## Short form

For the maintainer of boxer, or of a repository that consumes it, who must
answer "what may this tree depend on, and does it follow its own standards"
without a review team, gov is a governance toolkit: a license gate reading
one in-source policy for Go and Rust from a bill of materials[^sbom], lints
whose rule ids cite the standard they enforce, a pinned list of gate steps,
and an emitted skeleton that fails when it drifts.

Unlike a gate assembled from separately configured tools — a license scanner
per language, a lint meta-runner, a configuration language — each with a
policy of its own that drifts from the others, gov keeps one policy in Go
and breaks boxer first when it changes. The cost is an owned list of license
identifiers, a dependency cone in the gate binary, and fewer linters than a
meta-runner offers.

## Full form

**For** the maintainer of boxer, or of a repository that consumes it, who
must answer "what may this tree depend on" and "does it follow its own
written standards" mechanically, because there is no review team to ask,

**gov is** a governance toolkit: a license gate reading a bill of
materials[^sbom] for Go and a cargo tree for Rust against one in-source
policy, code and doc lints whose rule ids point at the coding and
documentation standards, a composite gate whose step list is pinned in Go,
and an emitted skeleton that fails on drift,

**that** gives "what may boxer depend on" one answer across two languages
with license elections recorded, ties every finding to a numbered rule the
reader can open, and makes a downstream repository's gate the same artifact
as boxer's,

**unlike** a gate assembled from separately configured tools — a license
scanner per language, a lint meta-runner, a configuration language — each a
second place where policy can drift,

**gov** keeps one policy, in Go, in the tree it governs. The cost is an
owned list of license identifiers tied to one detector, a dependency cone in
the gate binary, a hand-curated baseline, and fewer linters than a meta-
runner offers.

## What each clause rests on

| Slot | Clause | Rests on |
| --- | --- | --- |
| For | the maintainer, without a review team | [why-boxer](../why-boxer.md) P6 |
| For | a downstream repository | [ADR-0179](../../adr/0179-downstream-consumption-gate-and-skeleton.md) Context (two consumers drifted); [adopting-boxer-downstream](../../howto/adopting-boxer-downstream.md) |
| is a | license gate over a bill of materials, one SPDX policy, two languages | [ADR-0004](../../adr/0004-license-gate-cyclonedx.md), [ADR-0246](../../adr/0246-license-gate-rust-crate-trees.md) |
| is a | rule ids that cite the standard; reasoned disables | [ADR-0011](../../adr/0011-codelint.md); [DOCUMENTATION_STANDARD §8](../../DOCUMENTATION_STANDARD.md#8-enforcement) |
| is a | pinned step list; skeleton that fails on drift | [ADR-0179](../../adr/0179-downstream-consumption-gate-and-skeleton.md) |
| is a | manifest caps against the call graph | [ADR-0026 §SD10](../../adr/0026-app-runtime-and-capability-subjects.md) and its Update (in-process, compare mode) |
| is a | design-system lint | [ADR-0029 §SD8](../../adr/0029-imzero2-design-system-and-policy-as-code.md) |
| is a | governance corpora as tables | [ADR-0092](../../adr/0092-adr-overview-tool.md), [ADR-0168](../../adr/0168-capmap-business-capability-corpus.md) |
| that | one answer across two languages; elections recorded | [ADR-0246 §SD2, §SD4](../../adr/0246-license-gate-rust-crate-trees.md); [ADR-0004 §SD4](../../adr/0004-license-gate-cyclonedx.md) |
| that | boxer breaks first | [ADR-0179](../../adr/0179-downstream-consumption-gate-and-skeleton.md) |
| that | provenance in trailers | [ADR-0083](../../adr/0083-retire-llm-generated-build-tags.md) |
| unlike | a license scanner per language with its own policy | [ADR-0004](../../adr/0004-license-gate-cyclonedx.md) Alternatives (the prior scanner and its second detector stack); [ADR-0246](../../adr/0246-license-gate-rust-crate-trees.md) Alternatives (a Rust-side deny tool: "two answers that drift") |
| unlike | a lint meta-runner with its own format | [ADR-0011](../../adr/0011-codelint.md) Alternatives (the stock multi-checker: different CLI and diagnostic format) |
| unlike | a configuration language of its own | [ADR-0179](../../adr/0179-downstream-consumption-gate-and-skeleton.md) Alternatives (a typed config language: a second pinning system) |
| gov | one policy, in Go, in the tree | [ADR-0179](../../adr/0179-downstream-consumption-gate-and-skeleton.md) (`gate.Config` as a Go literal); [ADR-0004 §SD4](../../adr/0004-license-gate-cyclonedx.md) |
| trade | owned identifier list, detector coupling, dependency cone, baseline, fewer linters | [ADR-0004](../../adr/0004-license-gate-cyclonedx.md), [ADR-0179](../../adr/0179-downstream-consumption-gate-and-skeleton.md), [ADR-0048](../../adr/0048-go-file-package-naming.md) Consequences; [ENGINEERING_PRACTICES](../../ENGINEERING_PRACTICES.md) on the meta-runner trade |

## Boundary

- The capability check runs in compare mode; hard-fail is ahead
  ([ADR-0026](../../adr/0026-app-runtime-and-capability-subjects.md)
  Update). Its verdicts are a lower bound — hygiene, not security.
- The capability check and the design-system lint live under keelson and
  the design system, not under the `gov` command; they are clauses here
  because the gate runs them.
- Deferred or proposed, absent from the clauses: the design lint's config
  file and strict annotation, the second phase of code lint rules, the
  adversarial review protocol ([ADR-0131](../../adr/0131-systematic-adversarial-code-review.md)).
- The retired source-marker provenance is kept dormant; the statement
  claims trailers only.

## Further reading

- [why-boxer](../why-boxer.md) P1 and P6 — the premises this toolkit enacts.
- [positioning-statement](../positioning-statement.md) — the product-level statement.
- [ENGINEERING_PRACTICES](../../ENGINEERING_PRACTICES.md); [adopting-boxer-downstream](../../howto/adopting-boxer-downstream.md).
- Decisions: [ADR-0004](../../adr/0004-license-gate-cyclonedx.md),
  [ADR-0011](../../adr/0011-codelint.md),
  [ADR-0048](../../adr/0048-go-file-package-naming.md),
  [ADR-0083](../../adr/0083-retire-llm-generated-build-tags.md),
  [ADR-0179](../../adr/0179-downstream-consumption-gate-and-skeleton.md),
  [ADR-0246](../../adr/0246-license-gate-rust-crate-trees.md).
- Reference: https://pkg.go.dev/github.com/stergiotis/boxer/public/gov

[^sbom]: A software bill of materials: the machine-readable list of every dependency in a build, here in CycloneDX form.
