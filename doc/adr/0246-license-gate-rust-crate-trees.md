---
type: adr
status: accepted
date: 2026-09-17
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-19
---

# ADR-0246: The license gate covers the Rust crate trees

## Context

[ADR-0004](0004-license-gate-cyclonedx.md) put an inbound-license gate in front
of the Go module graph: `cyclonedx-gomod` emits an SBOM, `boxer gov
license-gate` classifies each component's SPDX identifier against the policy map
and fails CI on a forbidden or restricted category. boxer is MIT and cannot
accept copyleft inbound dependencies; the gate enforces that prospectively.

The Rust half of the tree has never been behind it. Four crates carry their own
lockfiles, and between them they resolve several hundred registry crates that no
gate has ever classified. One of those trees is not a build-time detail:
`rust/imzero2-viewer` produces a directory that gets redistributed, and
`THIRD_PARTY_NOTICES.md` §4.4 currently has to say so in as many words.
`boxer gov cargo-licenses` collects the notice files those crates oblige, but
collecting is not classifying — it would happily ship a GPL crate's COPYING file
beside an MIT binary.

The obstacle is not plumbing but semantics. A CycloneDX component carries SPDX
identifiers, one per row, and the existing gate is a row-per-identifier model. A
Cargo manifest carries an SPDX *expression*, and dual licensing is the norm
rather than the exception:

- `MIT OR Apache-2.0` — the overwhelming majority, and a row-per-identifier
  model has nothing to say about which branch binds.
- `MIT OR Apache-2.0 OR LGPL-2.1-or-later` — a real crate in the viewer's tree.
  Split into rows, the LGPL row is a restricted category and the gate goes red
  on a crate that offers MIT.
- `Apache-2.0 AND ISC`, `(MIT OR Apache-2.0) AND Unicode-3.0` — conjunctions,
  where *every* branch binds. Split into rows, a single passing row would clear
  a crate whose other conjunct is copyleft.

So the gate cannot be pointed at Cargo without first learning to evaluate an
expression, and the direction of the error differs by operator: splitting a
disjunction produces false positives, splitting a conjunction produces false
negatives. The second kind is the one a compliance gate must not make.

## Decision

We will extend `gov license-gate` to accept `cargo metadata` documents alongside
the CycloneDX SBOM, and give it an SPDX expression evaluator over the existing
policy map. A disjunction elects its most permissive known branch; a conjunction
takes its most restrictive. The policy map, the violation predicate, the CSV
inventory and the CI step stay single — this adds an ecosystem to the gate, not
a second gate.

### SD1 — Read `cargo metadata`, not a generated SBOM

The crate list comes from `cargo metadata --locked`, the same input
`gov cargo-licenses` already reads, rather than introducing a Rust SBOM
generator to produce CycloneDX. One less tool in the supply chain, one less
format whose fidelity has to be trusted, and the two Cargo-facing commands read
the same document. The cost is recorded under Consequences: `cargo metadata`
resolves manifests, so the gate needs a registry and a network, and what it
reads is the crate's own declaration.

### SD2 — One command, one policy, one inventory

`--cargo-metadata` is a repeatable flag on the existing `license-gate` command,
not a sibling `cargo-license-gate`. A second command would mean a second place
for the policy to drift, two CI steps to keep in step, and two inventories to
reconcile when a question arrives about what boxer depends on.

### SD3 — Expression semantics, and which way the errors fall

- **`OR` elects.** The most permissive branch the policy map knows wins, and
  that election is what the row records. `MIT OR Apache-2.0 OR
  LGPL-2.1-or-later` passes as MIT.
- **`AND` binds.** The most restrictive branch wins. One copyleft conjunct makes
  the crate a violation however permissive its siblings are.
- **Unknown ranks between them.** In a disjunction an unclassifiable identifier
  ranks below any branch the map knows, so it never displaces a good election.
  In a conjunction it ranks *above* forbidden and restricted, so an unknown
  conjunct can never mask a GPL one. This asymmetry is the whole point: it makes
  an unclassifiable identifier degrade to advisory, never to a pass.
- **`WITH` binds to its base.** `Apache-2.0 WITH LLVM-exception` classifies as
  the full identifier when the map knows it and as its base identifier
  otherwise; an exception narrows a license, it does not broaden it.
- The legacy `/` separator (`MIT/Apache-2.0`, still present in older crates)
  reads as `OR`.

### SD4 — The election is recorded, not implied

The CSV inventory gains the ecosystem and the full declared expression beside
the elected identifier, so every automatic election is auditable after the fact.
A gate that silently picks a branch is a gate nobody can check.

### SD5 — Scope: every Rust tree, every dependency kind

All four crate trees, with build- and dev-dependencies included, matching the
breadth `-test=true` gives the Go side. Crates local to the workspace are
skipped, as `isSelfModule` already skips boxer's own Go module: boxer's terms
are not an inbound question.

### SD6 — Four identifiers the Rust trees need

`Unicode-3.0` and `CDLA-Permissive-2.0` join the map as permissive. `OFL-1.1`
and `Ubuntu-font-1.0` join as **reciprocal**: both require derivative *fonts* to
stay under the same terms while leaving software that merely embeds the font
alone, which is what file-level reciprocity means in this taxonomy. Reciprocal
passes the gate, so this records the obligation rather than blocking it — and it
is consistent with the OFL fonts the repository already ships and documents in
`THIRD_PARTY_NOTICES.md` §2.2.

### SD7 — Unknown stays advisory

Unchanged from ADR-0004 SD5. An identifier outside the map, and a crate that
declares no license at all, land in the trailing unresolved block for manual
review and do not fail CI.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `gov license-gate` CLI | Added repeatable `--cargo-metadata`; `--sbom` becomes optional given one | `scripts/ci/license_gate.sh`, `.github/workflows/licenses.yaml` |
| Inbound-license policy map | Added four SPDX identifiers (SD6) | `policy_test.go` case table |
| Policy evaluation | Added: SPDX expression evaluation over the same categories | The Go path keeps its row-per-identifier behaviour unchanged |
| CSV inventory columns | Reshaped: gains ecosystem and declared expression | `THIRD_PARTY_NOTICES.md` §3, which documents the columns |
| License-compliance CI job | Now requires `cargo` and a reachable registry | `THIRD_PARTY_NOTICES.md` §4.4, whose "not gated" note becomes false |

## Alternatives

- **`cargo-deny`.** A capable, conventional answer, and the reason to decline it
  is not quality: its policy lives in its own TOML with its own vocabulary, so
  the one question "what may boxer depend on" would have two answers that drift
  independently. The gate's value is that the policy map is singular.
- **`cargo-cyclonedx` into the existing SBOM path.** Reuses the plumbing, but
  flattens an SPDX expression into a free-form `name` string, which is precisely
  the structure the decision needs; the gate would be back to splitting text it
  cannot evaluate.
- **Gate only `rust/imzero2-viewer`.** It is the tree that ships a
  redistributable directory, but `rust/h3bridge` produces a committed wasm
  artifact and the other two link into shipped binaries. A gate with a
  deliberate hole needs a reason better than effort.
- **A hand-curated election map per crate,** as `moduleLicenseElection` does for
  Go modules. That map works because Go elections are rare and each records a
  reading of an upstream LICENSE; across several hundred crates where
  `MIT OR Apache-2.0` is the default, it would need an entry per crate and would
  be abandoned within one dependency bump.

## Consequences

### Positive

- The largest ungated dependency surface in the repository stops being ungated,
  including the one tree whose output is redistributed.
- Expression evaluation removes a class of false negative the row-per-identifier
  model cannot see: a conjunction whose copyleft branch is cleared by a
  permissive sibling.
- `THIRD_PARTY_NOTICES.md` §4.4 loses its standing caveat.

### Negative

- The license-compliance job now needs `cargo` and a reachable registry, and
  resolving four lockfiles costs real wall-clock. The job runs on tags and
  manual dispatch, not per push, so the cost lands where it is affordable.
- The gate classifies what a crate *declares*. A mis-declared crate passes.
  `gov cargo-licenses` shipping the actual notice files is the compensating
  control, and it is the artifact a distributor reads anyway.
- Automatic election is a policy judgement made by code. SD4's recorded
  expression is what keeps it reviewable.

### Neutral

- The Go path is untouched: same SBOM, same row model, same output.
- Reciprocal remains a passing category, so SD6's font identifiers record an
  obligation without gating on it.

## Migration — Tier 1

- **Breaks.** Nothing in the tree. The CSV inventory gains columns, so a
  consumer that positionally indexes its columns would read the wrong field;
  the only documented consumer is `THIRD_PARTY_NOTICES.md` §3, which moves in
  the same commit.
- **Path.** Additive — an invocation passing only `--sbom` keeps its current
  behaviour and output.
- **Old shape.** Kept indefinitely; the Go path is not deprecated by this.

## Verification plan — Tier 1

- **Lane.** Default `go test` — a case table over the expression evaluator, and
  a `cargo metadata` fixture through the gate end to end.
- **What would fail.** A fixture crate declaring `GPL-3.0-only`, and one
  declaring `<unknown-id> AND GPL-3.0-only`, must both be reported as
  violations; one declaring `MIT OR GPL-3.0-only` must pass, electing MIT. The
  middle case is the regression that matters — it is the false negative SD3
  exists to prevent.
- **Gap.** The gate does not verify that a declaration matches the crate's
  actual license files, and it classifies no identifier it has never heard of.
  Both surface in the advisory block rather than silently.

## Status

Accepted 2026-09-19.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-09-19 — Implemented; four refinements

- **SD6 lands on Notice, not Permissive.** `Unicode-3.0` and
  `CDLA-Permissive-2.0` both require their text to travel with what they
  cover, which is the map's own definition of Notice and how it already
  files `Unicode-DFS-2016`. Both categories pass; the difference is which
  one tells the truth.
- **The CSV columns are appended rather than reshaped.** `ecosystem` and
  `declared` follow the original four, so a positional reader of
  `module,version,spdx_id,category` keeps reading the same fields and the
  Migration section's breakage does not arise.
- **An unreadable expression is unresolved, like an absent one.** SD7 named
  unknown identifiers and missing declarations; a declaration the
  evaluator cannot parse joins them in the advisory block, with its text.
- **SD3's disjunction fails closed.** Unknown ranking below *every* known
  branch includes the violating ones, so `GPL-3.0-only OR <unknown>`
  elects the GPL branch and fails, rather than passing on the branch the
  gate cannot read. This follows from SD3 as written; it is recorded here
  because it is the case a reader is most likely to take for a bug.

## References

- [ADR-0004](0004-license-gate-cyclonedx.md) — the gate this extends; its
  forbidden/restricted policy, the unknown-is-advisory stance (SD5) and the
  election mechanism are reused rather than restated.
- [ADR-0215](0215-retire-mimalloc-reproducible-builds.md) — the locked-source
  discipline `cargo metadata --locked` follows.
- `THIRD_PARTY_NOTICES.md` §3 (gate contract, CSV columns) and §4 (the Windows
  viewer's redistributables, whose crate tree this brings behind the gate).
