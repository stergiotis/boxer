---
type: adr
status: accepted
date: 2026-04-27
reviewed-by: "p@stergiotis"
reviewed-date: 2026-04-27
---

# ADR-0004: License Compliance Gate via CycloneDX SBOM Post-Processing

## Context

The repository's license-compliance gate at `scripts/ci/golicenses.sh` (renamed to [`license_gate.sh`](../../scripts/ci/license_gate.sh) by SD11 below) is implemented on top of [`github.com/google/go-licenses`](https://github.com/google/go-licenses): the script invokes `go tool ... go-licenses check --disallowed_types=forbidden,restricted ./public/...` and a separate `go-licenses csv` invocation generates the dependency inventory referenced by [`THIRD_PARTY_NOTICES.md`](../../THIRD_PARTY_NOTICES.md) §3. The policy is unchanged: boxer is MIT-licensed and cannot accept AGPL/GPL/LGPL/SSPL inbound dependencies; the gate enforces this prospectively.

`go-licenses v1.6.0` transitively depends on `gopkg.in/src-d/go-git.v4` — the *deprecated* fork of go-git, abandoned upstream in 2019 — to fall back to a network fetch when a license cannot be resolved from `$GOMODCACHE`. This is the sole consumer of the v4 fork in `go.sum`. The other go-git stack (`github.com/go-git/go-git/v5` + `go-billy/v5` + `gcfg`) is pulled in by [`cyclonedx-gomod`](https://github.com/CycloneDX/cyclonedx-gomod), which is already declared in the [`go.mod` `tool` block](../../go.mod) for SBOM generation purposes; that stack is actively maintained and not the concern of this ADR.

Observed pressures:

- **Dependency-surface reduction.** Removing `go-licenses` removes the deprecated `src-d/go-git.v4` fork entirely. Two transitive license-tooling stacks collapse to one.
- **Tooling consolidation.** `cyclonedx-gomod` is already in the toolchain for SBOM purposes and ships its own license detector. Running two license classifiers (one in `go-licenses`, one in `cyclonedx-gomod`) over the same module graph is redundant.
- **Policy must remain inspectable.** The forbidden/restricted SPDX-ID set is the load-bearing contract of the gate; whatever replaces `go-licenses` must keep the policy obvious in source rather than buried in third-party defaults.
- **The `unknown` set is real and tolerated.** Some upstream modules ship a single `LICENSE` at the module root and the classifier cannot resolve it for every subpackage (`github.com/golang/freetype/{raster,truetype}` via `github.com/fogleman/gg` is the canonical example). The current gate surfaces these advisorially without failing CI; that posture must be preserved (see `THIRD_PARTY_NOTICES.md:172-181`).
- **License-detector parity is not free.** `cyclonedx-gomod`'s detector is distinct from `go-licenses`/`licensecheck`. First-run output *will* diverge in the SPDX-ID set and the unknown set. Cutover proceeds directly without an up-front side-by-side diff (see SD10): the post-cutover CI run is itself the validator, and any forbidden/restricted classification it surfaces is treated as a real finding rather than a detector regression.

## Design space (QOC)

**Question.** How should boxer enforce its inbound-license policy and generate the dependency inventory while removing the `go-licenses` dependency on the deprecated `src-d/go-git.v4` fork?

**Options.**

- **O1** — Status quo: keep `go-licenses` and accept the `src-d/go-git.v4` dependency.
- **O2** — DIY classifier built directly on `github.com/google/licensecheck` (the library `go-licenses` itself uses internally), walking `$GOMODCACHE` and applying the policy in our own code.
- **O3** — Reuse `cyclonedx-gomod` (already in the tool block) for license detection and SBOM emission, post-process its CycloneDX JSON output with a small in-repo Go cmd that applies the forbidden/restricted policy, emits the CSV inventory, and gates CI *(chosen)*.
- **O4** — Source-vendor `go-licenses` into `internal/` and strip the network-fallback path that drags in `src-d/go-git.v4`.

**Criteria.**

- **C1 — Dependency-surface reduction:** does the option remove `gopkg.in/src-d/go-git.v4` from `go.sum`, and does it avoid introducing new heavyweight transitive deps?
- **C2 — Tool consolidation:** does the option reduce the number of distinct license-tooling stacks the repository carries?
- **C3 — Policy expressiveness and inspectability:** is the forbidden/restricted SPDX-ID list visible and editable in repository sources rather than third-party defaults?
- **C4 — Detector coverage:** how well does the option detect licenses across the existing dependency set, including the subpackage-resolution edge cases tolerated by the current gate?
- **C5 — Maintenance burden:** how much code does the repository own and have to keep current as SPDX/license tooling evolves?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | ++ | ++ | +  |
| C2 | −  | +  | ++ | −  |
| C3 | −  | ++ | ++ | +  |
| C4 | ++ | +  | +  | +  |
| C5 | ++ | −  | +  | −− |

O3 is the Pareto-best option overall. O2 ties on C1/C3 and beats O3 on detector independence (it lets us stay on `licensecheck`, which is the upstream classifier `go-licenses` already uses) but loses on C2 (introduces a *second* license-detection path running in parallel with `cyclonedx-gomod`'s own detector) and on C5 (we own the module-walking, `$GOMODCACHE` resolution, subpackage-LICENSE-fallback logic that `go-licenses` already solved). O3 amortises license detection against work `cyclonedx-gomod` already does for SBOM purposes; the post-processor is policy + CSV emission only.

## Decision

We replace `go-licenses` with a two-step pipeline:

1. **`cyclonedx-gomod mod -licenses=true -test=true -json -output sbom.json`** — produces a CycloneDX 1.6 SBOM containing per-component license evidence as SPDX identifiers under `component.evidence.licenses[].license.id`.
2. **`go run ./internal/cmd/licensegate -sbom sbom.json -csv third_party_licenses.csv`** — a new in-repo Go cmd that parses the SBOM, applies the forbidden/restricted policy from an inlined SPDX-ID map, writes the CSV inventory, and exits non-zero on policy violations while reporting `unknown` advisorially.

`scripts/ci/golicenses.sh` is rewritten to drive this pipeline; the `.github/workflows/licenses.yaml` invocation does not change. The `go-licenses` entry is removed from the `tool` block in `go.mod` and from `scripts/ci/install.sh`. As a direct consequence, `gopkg.in/src-d/go-git.v4` and the `github.com/google/go-licenses` module disappear from `go.sum`.

### Subsidiary design decisions

Each of the following fixes a load-bearing detail of the implementation. Where the viable-option count stayed below the QOC threshold, the alternative is captured in prose instead of a matrix.

- **SD1 — Cmd location: `internal/cmd/licensegate/`.** Internal so it cannot be imported by `public/...` consumers (it is not part of the API surface); a real cmd package so it is testable as Go code with table-driven SPDX-mapping tests. Rejected `scripts/ci/*.go`: introduces a new "Go-in-scripts" pattern not yet present in the repo and forfeits package-level testability.

- **SD2 — License field consumption: read both `component.licenses[]` and `component.evidence.licenses[]`, deduplicating by SPDX ID.** `cyclonedx-gomod` defaults to evidence-only output (license is reported as evidence rather than asserted because detection is heuristic); the `-assert-licenses` flag promotes evidence to assertions. Reading both arrays makes the gate insensitive to which flag is in effect and to future CycloneDX schema evolution that may move where assertions live. Rejected reading only `evidence.licenses[]`: brittle to flag changes and to schema migrations.

- **SD3 — Scope: module-wide, not `./public/...`.** `cyclonedx-gomod mod` operates on the whole module and does not honour build tags or package-set filters. Adopted as a deliberate policy tightening: a transitively-pulled GPL dependency is a problem regardless of which subtree imports it. The previous `--ignore github.com/stergiotis/boxer` semantics translate to filtering the project's own module-purl prefix in the post-processor (see SD6). Rejected (a) running `cyclonedx-gomod app` once per public binary — multi-invocation complexity for a marginal scope-narrowing; rejected (b) constructing a synthetic module containing only `./public/...` roots — fragile and brittle across dependency graph changes.

- **SD4 — SPDX-ID → category map: inlined in `internal/cmd/licensegate/policy.go`.** The forbidden/restricted SPDX-ID lists (~80 identifiers across the categories) are written out as Go literals in a single file, with a comment block citing the SPDX source. This is the same set `go-licenses` uses internally, replicated rather than imported. Rejected (a) lifting `go-licenses`' `licenses` package as a vendored copy — inherits its abstractions and update cadence; rejected (b) declaring the policy in YAML/TOML and parsing at runtime — adds a parser and a config schema for data that changes monthly at the fastest. The inlined map is the documentation.

- **SD5 — `unknown` handling: advisory, non-fatal.** Components without a detected license are reported on stderr as a trailing block (matching `golicenses.sh`'s current "unresolved licenses" output) but do not fail the gate. Preserves the current posture documented in `THIRD_PARTY_NOTICES.md:172-181`. Rejected fail-on-unknown because it would introduce CI flakiness on dependency bumps where a new module ships a non-canonical license file the detector cannot classify; the existing gate already documents this as a deliberate tolerance.

- **SD6 — Self-module exclusion: `pkg:golang/github.com/stergiotis/boxer` purl prefix.** The post-processor filters components whose purl begins with the boxer module path. This matches the current `--ignore github.com/stergiotis/boxer` flag. Sibling repositories owned by the same author (`capmap`, `pushout-algo`, `stylometrics`) are *not* filtered: they are independent modules and the gate evaluates them like any other dependency. Their LICENSE-file absence surfaces in the `unknown` advisory and is tracked there.

- **SD7 — CSV schema redefined: `module,version,spdx_id,category`.** The CSV is a derived artifact, not committed to the repository, and has no machine consumers; the column rename costs one diff in `THIRD_PARTY_NOTICES.md:155`. Rejected preserving `go-licenses`' `module,license_url,license_type` schema: `license_url` is a `go-licenses`-specific synthesis (a guessed VCS URL pointing at the LICENSE file in the upstream repo) that cyclonedx-gomod does not produce, and `license_type` collapses SPDX-ID and category into one field. The new schema is strictly more informative.

- **SD8 — Test-deps inclusion: `-test=true`.** Includes test-only transitive dependencies in the gate. Strictly broader than the current scope and consistent with SD3's tightening posture: a GPL test dependency is still in `go.sum` and still flagged by other compliance scanners. Rejected `-test=false`: would create an asymmetric scope where production deps are gated but test deps are not, harder to defend.

- **SD9 — Build-tag handling: not honoured (cyclonedx-gomod limitation).** The current gate uses `GOFLAGS="-tags=$tags"`. `cyclonedx-gomod mod` does not evaluate build constraints. Tag-gated dependencies (`identifier_tag_fixed16`, `goexperiment.jsonv2`, `llm_generated_*`) therefore enter the gate unconditionally. Acceptable consequence of SD3: build tags do not filter what is in `go.sum`, only what is compiled, and a `go.sum` entry under a forbidden license is a problem regardless of tag selection.

- **SD10 — Cutover validation: deferred to the first post-cutover CI run; no up-front side-by-side diff.** A side-by-side `go-licenses` vs. new-pipeline diff was considered and explicitly rejected as unnecessary work. Rationale: any forbidden/restricted classification produced by the new gate that `go-licenses` did not flag is a *real* finding (the dependency's license actually falls in that category) rather than a detector regression to triage; the right response is to address the dependency, not to second-guess the classifier. The `unknown` set will shift on cutover and that shift is informational, not actionable. If the post-cutover CI run produces an unexpected violation, the documented fallback is O2 (DIY on `licensecheck`).

- **SD11 — Script renaming: `golicenses.sh` → `license_gate.sh`.** The script name encodes the implementation rather than the function; renaming aligns with the cmd name and removes the misleading `go-licenses` reference. The single workflow caller in `.github/workflows/licenses.yaml:20` is updated in lock-step. Rejected keeping the old name: future readers grep for `go-licenses` and find a script that no longer uses it.

## Alternatives

Rejection rationale for the top-level options is in the QOC matrix; these notes capture nuance not visible in the ratings.

- **O1 — Status quo.** Eliminated by C1: keeps the deprecated `src-d/go-git.v4` fork in `go.sum` indefinitely.
- **O2 — DIY on `licensecheck`.** A clean second-best. Loses to O3 only because `cyclonedx-gomod` already does the module walking, license-file resolution, and classification work that O2 would have to replicate; the post-processor in O3 is strictly smaller (~200 LoC vs. ~400 LoC). If `cyclonedx-gomod`'s license-detection coverage proves materially worse than `licensecheck`'s in the SD10 cutover validation, O2 is the documented fallback.
- **O4 — Source-vendor `go-licenses`.** Inherits go-licenses' internals (CSV writer, GitHub URL resolution, ignore-flag plumbing) while still requiring removal of the `src-d/go-git.v4`-using fallback path. Higher net code ownership than O2 with no compensating benefit.

## Consequences

### Positive

- **Removes `gopkg.in/src-d/go-git.v4`** (the deprecated 2019-abandoned fork) and `github.com/google/go-licenses` from the dependency graph. One full transitive stack disappears from `go.sum`.
- **One license-detection stack instead of two.** `cyclonedx-gomod` was already running its detector for SBOM purposes; that work now does double duty.
- **Policy is inspectable in repository source.** The forbidden/restricted SPDX-ID set lives in `internal/cmd/licensegate/policy.go` and changes go through code review rather than third-party defaults.
- **CSV schema is informative.** `(module, version, spdx_id, category)` is strictly more useful than `(module, license_url, license_type)` for both audit and inventory purposes.
- **Scope tightening is principled.** SD3+SD8+SD9 align the gate with the contents of `go.sum` rather than a build-tag-conditioned subset, eliminating silent gaps where a forbidden dep was reachable only from `internal/...` or only under a non-default build tag.

### Negative

- **First-run output diverges from the current gate.** Different classifier; the SPDX-ID set and especially the `unknown` set will not be identical. Per SD10, the divergence is accepted without an up-front diff: the post-cutover CI run is the validator, and any newly-surfaced forbidden/restricted classification is treated as a real finding.
- **Policy ownership.** ~80 SPDX IDs across categories must be kept current as SPDX evolves. The set is small and changes slowly; a comment block in `policy.go` cites the SPDX source so future maintainers know where to look.
- **Build tags no longer narrow the gate.** Acceptable per SD9; documented here so it is not a surprise on the next dependency bump.
- **Coupling to `cyclonedx-gomod`.** The gate now depends on `cyclonedx-gomod`'s license-detection behaviour. A regression in its detector becomes a boxer CI issue. The fallback documented in O2 mitigates this.

### Neutral

- **`THIRD_PARTY_NOTICES.md` §3 needs three textual updates** (the `go-licenses csv` invocation, the gate description, the `go-licenses CSV output` reference). These are mechanical and tracked in the implementation PR, not as separate ADRs.
- **`.github/workflows/licenses.yaml` is unchanged** beyond the script rename in SD11.
- The `unknown` advisory list will likely shift on cutover: some modules currently flagged as unknown by `go-licenses` may resolve under cyclonedx-gomod, and vice versa. This is expected and fine.

### Derived practices

- **SBOM regeneration is automatic.** `cyclonedx-gomod` runs unconditionally at the start of `license_gate.sh`; the SBOM is treated as a transient artifact, not committed.
- **Policy edits are single-file.** Adding or removing a SPDX ID in a category is a one-line edit in `internal/cmd/licensegate/policy.go` with a table-driven test update; ADR amendment via the inline Updates pattern records the rationale.
- **Cutover deltas surface naturally.** Per SD10, no up-front diff is required; the first post-cutover CI run is the validator, and subsequent dependency bumps surface their own deltas through normal CI.

## Status

Accepted — 2026-04-27. Design frozen; implementation begins on `internal/cmd/licensegate/` and the `scripts/ci/license_gate.sh` rewrite.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## Updates

### 2026-05-02 — Per-module license election (SD12)

The post-cutover SBOM run surfaced one policy violation: `github.com/golang/freetype` classified as `GPL-2.0-or-later` (restricted). The upstream `LICENSE` file is dual: "Use of the Freetype-Go software is subject to your choice of exactly one of the following two licenses: \* The FreeType License \[FTL\] \[…\] or \* The GNU General Public License (GPL), version 2 or later." `cyclonedx-gomod`'s detector emits the GPL branch only; the prior `go-licenses` gate had this module in the unresolved-advisory set rather than failing on it.

Per SD10 this is a real finding, not a detector regression. The resolution is to formally elect the permissive FTL branch in repository sources rather than rely on detector behaviour either way:

- **SD12 — Per-module license election: `moduleLicenseElection` map in `policy.go`.** Each entry replaces the SBOM-detected license set for one module with a single asserted SPDX ID, gated by a comment citing the upstream LICENSE wording that authorises the election. Evaluated before policy classification in `main.go` so the elected SPDX ID flows into both the CSV inventory and the violation check. Initial entry: `github.com/golang/freetype → FTL`. `FTL` added to `spdxCategory` as `Notice`. Rejected (a) treating the upstream's choice-of-license as detector noise to ignore — bad-faith reading of the policy and unauthored in code; rejected (b) module-level allow-listing that bypasses categorisation entirely — election preserves SPDX-ID provenance in the CSV and re-runs the test suite's category lookup as a sanity check.

The election mechanism is the only addition; the cutover otherwise lands as designed.

### 2026-05-21 — Migrated to `boxer gov license-gate` subcommand

The standalone `internal/cmd/licensegate/` main is removed per the new CODINGSTANDARDS.md "Entry Points" rule (no ad-hoc `main()` for utilities or compile-time tools). Its three source files move to `public/gov/licensegate/` with `package licensegate`; the test file moves with them. The new entry surface is `boxer gov license-gate --sbom … [--csv …]`, registered alongside the other `gov` subcommands. `scripts/ci/license_gate.sh` switches from `go run … ./internal/cmd/licensegate` to `go run … ./public/app gov license-gate`. Pre-migration exit codes 1 (policy violation) and 2 (invocation error) collapse to a single non-zero exit driven by the returned error, which is behaviour-equivalent for `set -e` callers and the only externally observable change. The policy table, election map, and SBOM parser are byte-identical to the pre-migration files — the core SD1–SD12 decisions stand unchanged.

## References

- `scripts/ci/golicenses.sh` — gate implementation at the time of writing; replaced and renamed to [`license_gate.sh`](../../scripts/ci/license_gate.sh) by this ADR (SD11).
- [`scripts/ci/install.sh`](../../scripts/ci/install.sh) — tool-installation script; `go-licenses` line removed by this ADR.
- [`.github/workflows/licenses.yaml`](../../.github/workflows/licenses.yaml) — CI workflow invoking the gate; updated for the script rename in SD11.
- [`THIRD_PARTY_NOTICES.md`](../../THIRD_PARTY_NOTICES.md) §3 — current policy documentation; updated by this ADR's implementation PR.
- [`go.mod`](../../go.mod) — `tool` block; `go-licenses` entry removed.
- [`doc/adr/0001-adopt-diataxis.md`](0001-adopt-diataxis.md) — framework ADR referenced by `status` / `reviewed-by` fields.
- [`doc/adr/0003-h3-wasm-bridge.md`](0003-h3-wasm-bridge.md) — prior ADR; QOC + SD-numbered subsidiary-decisions pattern followed here.
- CycloneDX 1.6 JSON spec, `components[*].evidence.licenses`: https://cyclonedx.org/docs/1.6/json/#components_items_evidence_licenses
- SPDX License List: https://spdx.org/licenses/
- `cyclonedx-gomod`: https://github.com/CycloneDX/cyclonedx-gomod
- `google/licensecheck` (referenced by O2 fallback): https://pkg.go.dev/github.com/google/licensecheck
