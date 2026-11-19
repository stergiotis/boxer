---
type: adr
status: proposed
date: 2026-11-19
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0384: Offline `go mod tidy` against a vendored Go workspace

## Context

A downstream project that carries boxer into an airgapped site does so as a
vendored Go workspace: the source trees of the modules it co-develops, a
generated `go.work` that `use`s them, and the `go work vendor` output beside it
(`airgap_go_workspace_vendor` in the [ADR-0095](./0095-airgapped-build-bundle.md)
airgap core). Further projects at that site join the workspace as members of
their own and build with `-mod=vendor`. Their `go.mod` files still pin boxer
and their third-party dependencies, because the same projects also build on
connected hosts with `GOWORK=off`.

Three properties of Go's vendor-consistency check decide what such a member's
`go.mod` may say, and each was confirmed against a staged workspace:

- **Every `require` must be in `vendor/modules.txt` at that exact version and
  marked `## explicit`.** The check does not look at whether the module is
  imported. A `// indirect` line and a `replace` are checked too, so a member
  cannot even name a module the workspace did not vendor.
- **A workspace `modules.txt` lists a module once for each version some
  workspace module requires.** Packages are listed only under the highest of
  those versions, which is the one built. So a lower pin can pass the check
  while a different version is compiled.
- **The site has no module cache.** `go get`, `go mod tidy` and `go work sync`
  all load the module graph and refuse to run. A member whose pins drifted,
  because the workspace was re-packed with a newer boxer, fails with
  `inconsistent vendoring` and cannot be repaired with Go's own tools.

The workspace does hold everything tidy needs to answer the question for the
packages that are actually built: each package's source, either in a `use`d
module or in `vendor/`, the package-to-module map in `modules.txt`, the
selected versions, and each module's `go` version. What it lacks is
dependency test files (vendoring drops them) and the `go.mod` files of
dependencies.

## Design space (QOC)

**Question.** How does a member of a vendored workspace bring its `go.mod` and
`go.sum` to what the workspace builds, without a network or module cache?

**Options.**

- **O1** — No `require` on workspace-provided modules: members only `use` and
  import, and drop the lines.
- **O2** — Version alignment only: rewrite each existing `require` to the
  version `modules.txt` selects, with `go mod edit`.
- **O3** — Offline tidy: recompute the requirement set from the member's
  imports, walked through the workspace sources and `vendor/`, with versions
  from `modules.txt`; merge `go.sum` from the checksums the workspace modules
  carry.
- **O4** — Ship a module cache with the bundle and run real `go mod tidy`.

**Criteria.**

- **C1** — The member builds offline against the vendor tree.
- **C2** — The member's `go.mod` stays buildable and tidy on a connected host
  (`GOWORK=off`).
- **C3** — Fidelity to `go mod tidy`: requirements added, dropped and marked
  direct or indirect as tidy would.
- **C4** — Cost to the bundle and the packing host.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | +  | +  | ++ | ++ |
| C2 | −− | −  | +  | ++ |
| C3 | −− | −  | +  | ++ |
| C4 | ++ | ++ | ++ | −− |

O1 builds offline only while the member requires nothing the workspace
changed, and leaves a `go.mod` that cannot build with `GOWORK=off`. O2 keeps
stale requirements and misses new ones that a raised version brings in, which
the consistency check then reports one at a time. O4 is exact, but the cache
is a second copy of every dependency next to `vendor/`, and the bundle becomes
a different shape.

## Decision

We will add `public/code/analysis/golang/vendortidy`, a library and the
subcommand `boxer code analysis golang vendortidy`. It computes, from a
workspace's `go.work`, its `vendor/modules.txt` and the sources it holds, the
requirements `go mod tidy` would give a member module, and writes them, or
with `--check` reports the difference.

- **§SD1 — The walk.** The roots are the member's packages and their tests,
  minus `testdata`, `_`/`.` directories, nested modules and the `go.mod`
  `ignore` list, plus its `tool` packages. Imports are read under cmd/go's
  "any tags" rule: every build constraint holds except the `ignore` tag. They
  are followed transitively without tests through `use`d module sources and
  `vendor/`. A module that provides a reached package is required, as direct
  if a member package or test imports it and indirect otherwise. Tool modules
  are indirect unless code imports them.
- **§SD2 — Versions.** A requirement takes the highest version `modules.txt`
  lists for the module, which is the version built. The `go` line is raised to
  the highest `go` version a required module declares. A `toolchain` line is
  kept or dropped by cmd/go's rule. The require blocks are laid out with the
  modfile writer cmd/go picks for the module's `go` version.
- **§SD3 — What the vendor tree cannot decide is kept, not invented.** Tidy
  also keeps a requirement when dropping it would change the version selected
  for a package that only a dependency's test imports. Those tests are not in
  `vendor/`. An existing requirement on a module that `modules.txt` marks
  explicit but that provides no vendored package is therefore kept, at the
  selected version, and reported. No such requirement is added.
- **§SD4 — `go.sum` is a superset.** Lines for a module whose version changed
  or that was added are taken from the union of the workspace modules'
  `go.sum` files and `go.work.sum`. Lines for a module no longer required are
  dropped. Every other line is kept, because whether a dependency's test needs
  it cannot be judged offline. Lines are ordered as cmd/go orders them. A
  connected `go mod tidy` can therefore only remove lines, never add them.
- **§SD5 — Refusals.** An import that neither a `use`d module nor `vendor/`
  provides fails the run with the importing package named. It has to be
  vendored on a connected host. A required module that is `use`d by the
  workspace but has no version in `modules.txt` is reported and not required.

## Alternatives

- **O1, members never require workspace modules.** The member's `go.mod` stops
  describing a buildable module outside the workspace.
- **O2, version alignment with `go mod edit`.** Leaves stale requirements and
  misses new ones, so the next build fails on whatever alignment missed.
- **O4, a shipped module cache.** Exact, but doubles the dependency payload and
  changes the bundle's shape. It remains the route for a member that needs
  dependencies the workspace does not vendor.
- **`go list -deps -test` in vendor mode instead of a source walk.** Runs
  offline, but evaluates constraints for one GOOS/GOARCH and tag set, so it
  misses requirements that tidy keeps for other platforms.

## Consequences

### Positive

- A member drifted by a re-packed workspace is repaired offline in one step,
  and its `go.mod` is the one tidy would write on a connected host.
- `--check` gives a member's CI or hooks a drift test that needs no network.
- On a workspace module that is already tidy, the result is its `go.mod` and
  `go.sum` unchanged. That makes boxer's own module a standing oracle.

### Negative

- It re-implements a slice of cmd/go: the any-tags evaluation, the file-header
  scan and the layout rules. A change in cmd/go can make the two disagree
  until this package follows. The fixpoint test is what notices.
- `go.sum` can carry lines a connected tidy removes, and a requirement kept by
  §SD3 can be stale. A connected `go mod tidy --diff` is not guaranteed empty.
- Go checks vendor consistency for the whole workspace, so while one member is
  drifted the command itself does not build there. It is built from a work file
  that leaves the drifted member out, before it runs
  ([how-to](../howto/offline-go-mod-tidy.md)).

### Neutral

- `golang.org/x/mod` becomes a direct dependency of boxer. It was already in
  the module graph.

## Verification plan

- **Lane.** Default `go test`: fixture workspaces for each rule in §SD1–§SD5.
  Integration lane: stage boxer's committed tree in a workspace, run
  `go work vendor`, and require byte-identical `go.mod` and `go.sum`.
- **What would fail.** A walk rule or layout rule that drifts from cmd/go
  shows up as a difference against boxer's own tidy `go.mod`.
- **Gap.** Agreement with tidy on a member whose versions actually change is
  shown by hand with a connected `go mod tidy --diff`, not in CI, since that
  needs the network.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [How to tidy a module inside a vendored workspace, offline](../howto/offline-go-mod-tidy.md)
- [ADR-0095: Airgapped build bundle](./0095-airgapped-build-bundle.md) — the
  airgap core whose workspace variant produces the vendored workspace
- [ADR-0294: urfave/cli v3 for every CLI surface](./0294-urfave-cli-v3-for-every-cli-surface.md)
- Go command documentation: `go help modules`, `go help work`, vendor
  consistency in `cmd/go/internal/modload`
