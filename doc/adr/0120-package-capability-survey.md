---
type: adr
status: proposed
date: 2026-07-15
---

> **Status: proposed — pre-human-review.** The decisions below were agreed in a
> design dialogue and are recorded ahead of the implementation so the shape is
> reviewed before the code lands. Feasibility numbers in *Context* come from a
> throwaway probe against the real tree, not from an estimate.

# ADR-0120: surveying package capabilities with capslock

## Status

Proposed.

## Context

ADR-0080 introduced `public/packageprops`: a co-located, curated property record
per Go package, seeded by a survey and harvested into a static table. Its `Props`
doc comment anticipated exactly this moment — *"New properties are added as
fields over time (purity, determinism, ownership, stability, …); wasm amenability
is the first."* Wasm amenability remains the only one.

Separately, [google/capslock](https://github.com/google/capslock) classifies a Go
package's *capabilities* — which privileged operations it can reach — by
following calls to classified standard-library operations. It reports each
capability as either **direct** (the package's own code performs the operation,
possibly via a standard-library call) or **transitive** (it is reachable only by
going through a non-stdlib dependency). SD5 covers the exact rule and the two
places this classification is less absolute than it sounds.

Capability data is a natural second property. It also closes a loop the
repository has recently opened: ADR-0118 made `public/extbin` the single audited
chokepoint for external-process invocation. That is a *claim* about where
privilege is concentrated, and until now nothing measured it.

### What a probe against the tree actually showed

A throwaway probe (capslock v0.3.2, library API, whole tree, repo build tags):

- **440 packages** analysed in ~31s wall, producing 4033 capability records;
  362 packages carry at least one capability.
- **~7.75 GB peak RSS.** Building SSA over the whole module is memory-hungry.
  This is a generation-time cost only; nothing is paid at runtime.
- capslock's dependencies — `x/tools`, `protobuf`, `x/mod`, `x/sync`, `x/sys`,
  `fatih/color`, `go-cmp`, `mattn/go-colorable`, `mattn/go-isatty` — are **all
  already in this module's build list**. The library adds one module.
- It builds and runs at this module's exact pins (`x/tools v0.45.0`,
  `x/mod v0.36.0`, `protobuf v1.36.11`) under Go 1.26, above capslock's own
  `x/tools v0.43.0` floor.

Two findings shaped the decisions below.

**The transitive axis saturates.** Almost every package transitively reaches
almost every capability; the direct axis is where the discrimination lives:

| capability | direct | transitive |
| --- | ---: | ---: |
| `ARBITRARY_EXECUTION` | 1 | 351 |
| `REFLECT` | 9 | 339 |
| `UNSAFE_POINTER` | 11 | 333 |
| `EXEC` | 8 | 323 |
| `NETWORK` | 69 | 261 |
| `FILES` | 70 | 264 |

As a positive claim ("this package can reach X") transitive is nearly vacuous.
Its value is in the **negative**: ~100 packages have no path to the network in
the analysed call graph, and that is an assertion worth recording — with the
caveat, developed in SD5a, that it is evidence of absence rather than proof of
it.

**capslock's scope is wider than `packageprops`'s.** capslock covers 440
packages; `packageprops` covers 380. The gap is `main` and `internal/**`, which
the wasm survey's scope filter excludes for a reason specific to *it* — the
generated probe cannot `import` a `main` or `internal` package. That constraint
has nothing to do with capabilities, and a `main` package's capability set is the
whole binary's, which is the most security-relevant set on offer.

## Decision

### SD1 — capslock as a library, not an external binary

Import `github.com/google/capslock/analyzer` directly. The repository already
hosts `x/tools`-based analyzers in-process (`public/gov/codelint`,
`public/gov/callsites`, `public/code/analysis/golang/godep`); a capability survey
is the same kind of thing and belongs beside them. The library route needs no
external toolchain installed, keeps the airgap bundle self-contained, and is
type-checked against the pinned version rather than parsed out of a JSON contract
that can drift.

This deliberately does **not** route through `extbin` (ADR-0118). `extbin`
governs *external-process invocation*; a library import is not one. The wasm
survey shells out to TinyGo because a compiler leaves no choice — capslock does.

The cost accepted: SSA links into any binary that reaches the survey package
(today, `app`).

### SD2 — the survey loads packages itself, with `Dir`

`analyzer.LoadPackages` never sets `packages.Config.Dir`; it loads from the
process working directory. Rather than `chdir` — a process-global mutation
unacceptable in a library — the survey constructs its own `packages.Config` with
an explicit `Dir`, using capslock's exported `analyzer.PackagesLoadModeNeeded`
LoadMode, and hands the loaded packages to `analyzer.GetCapabilityInfo`. This
keeps the repository's existing convention of `Dir`-driven, env-free collectors
(ADR-0064 SD3).

### SD3 — representation: two `uint32` bitmasks, bit position = proto enum number

`Props` gains two fields:

```go
CapsDirect    CapabilitySet // what the package's own code exercises
CapsReachable CapabilitySet // the closure: everything it can reach at all
```

`CapabilitySet` is a `uint32` bitmask where **bit *n* is capslock's proto enum
number *n***. Protobuf enum numbers are stable by the proto compatibility
contract, so pinning bit positions to them means a future capslock release can
add capabilities without renumbering the existing ones. This is the direct lesson
of the `AspectSet` 64-aspect ceiling, where a locally-renumbered dense index made
removals renumber the set and break golden files.

`uint32` against a current maximum enum of 14 leaves headroom without pretending
to a 64-bit budget nothing needs.

The fields are scalars, not slices or maps. `packageprops.Props` is held **by
value** in `Entry`/`Table`, and `All()` hands out snapshots under no lock — a
reference-typed field would quietly turn that into shared mutable state.
`packageprops` also remains a stdlib-only leaf (ADR-0080 SD2): a bitmask needs no
import, so no capslock type crosses into it.

### SD4 — `CAPABILITY_SAFE` (bit 1) is the surveyed marker

A bitmask has no natural counterpart to `WASMUnknown`: an empty set cannot
distinguish *"surveyed, found nothing"* (the correct answer for a pure leaf like
`public/functional/option`) from *"never surveyed"*.

Rather than add a parallel `CapsSurveyed bool`, a survey that completes and finds
nothing privileged sets bit 1 — capslock's own `CAPABILITY_SAFE`, whose meaning
in the upstream taxonomy is exactly "no privileged capability". The bit is set
*only* in that case, so no package is ever both "safe" and "exec". So:

- `CapsDirect == 0` → not surveyed; asserts nothing (the ADR-0080 zero-value rule).
- `CapsDirect == 1<<CapabilitySafe` → surveyed; exercises nothing privileged.
- any other value → surveyed; the set bits are what it reaches.

Both tests stay trivial: `Surveyed()` is `!= 0`, `Safe()` is `Has(CapabilitySafe)`.
This borrows the source taxonomy's vocabulary instead of inventing a second one.

The cost, accepted with eyes open: `CapabilitySafe` is a *marker*, not a member,
and it lives inside the same bitmask as real capabilities. It is set exactly when
no other bit is, which is precisely when set algebra across two sets goes wrong —
a package whose own code is clean but which reaches files through a dependency
has `Safe` in `CapsDirect` and not in `CapsReachable`, so a naive
`direct &^ reachable` reports a subset violation that is not one. The mask is
therefore not optional: `CapabilitySet.Privileged()` strips the marker and
`Subset` uses it, and every comparison across sets must go through them. The
alternative — a separate `CapsSurveyed bool` — keeps the algebra pure at the cost
of a third field and of leaving `CapabilitySafe` a permanently-unset constant;
the marker was preferred, but this is the sharp edge it buys.

### SD5 — store the closure, not capslock's transitive-only set

Both axes are recorded despite the saturation shown above: the eight bytes buy
the negative assertions, which are the ones with teeth ("this package cannot
reach the network").

`CapsReachable` is deliberately **not** capslock's `CAPABILITY_TYPE_TRANSITIVE`
verbatim. capslock emits one record per (package, capability) carrying its
strongest type, so `TRANSITIVE` means *"reachable only through a dependency"* — a
package that execs directly gets no transitive exec record even though it plainly
does reach exec through its dependencies too. Stored raw, `extbin` — the tree's
exec chokepoint — reports a transitive set of `safe`, which reads as a claim it
cannot reach anything, and "can this package exec at all?" becomes a two-set
question with a subtlety attached.

Storing the closure instead (`direct ∪ transitive-only`) is lossless — the
transitive-only set is `CapsReachable` minus `CapsDirect` — and yields the
invariant `CapsDirect ⊆ CapsReachable`, so the question worth asking is one
lookup. Display sites lead with direct and treat reachable as the drill-down; the
harvest table renders direct only, for the saturation reason.

Two properties of capslock's classification are worth knowing before reading a
verdict, both established by inspecting `analyzer.go` and `interesting.cm` rather
than assumed:

- **A hop through the standard library does not make a capability transitive.**
  The rule is `n != pName && !isStdLib(pName)` — only a hop through a *non-stdlib*
  package demotes a verdict. So a package calling `(*exec.Cmd).Start` directly is
  DIRECT for `exec`, because the intervening `os/exec` is stdlib. "Direct" means
  *this package's own code performs the operation*, not *this package contains a
  syscall*.
- **A verdict is scoped to what capslock's call graph reaches**, which starts
  from a package's public API. Code the analysis cannot reach from there — a
  method on an unexported type that nothing in the analysed set constructs, say —
  contributes nothing, even though it is compiled in and runs at runtime.

### SD5a — `CapsDirect` is a lower bound

Both properties above are sound in the same direction: they can *omit* a
capability a package really exercises, never invent one. `CapsDirect` is
therefore a **lower bound** — evidence that a package does something, not
evidence that it does nothing else.

This is not academic. `public/algebraicarch/pushout/pijul` calls `cmd.Run()` on
an `*exec.Cmd`, in a file with no build tag, and records `CapsDirect: safe`: its
runner is a method on an unexported type that nothing in the analysed set
constructs, so the call is outside the reachable graph. A source census finds
eleven packages spawning processes; the survey attributes direct `exec` to six.

The consequence for callers: an absent bit in `CapsDirect` does **not** license
"this package cannot do X". The claim that *is* supported is the negative one on
the closure — an absent bit in `CapsReachable` means no path exists in the
analysed graph — and even that inherits the reachability caveat. Use the survey
to find what a package does and to detect drift, not as an authority for what it
does not do; for that, a source census is the sound instrument (see ADR-0118
§Scope, which is the worked example).

The `Unknown` field on a survey covers the *other* unsoundness — a capslock
release adding a capability the vocabulary lacks — and `capsurvey generate`
refuses to write when it is non-empty, because silently dropping a bit would turn
a lower bound into a wrong answer.

### SD6 — scope is re-decided, not inherited

The scope funnel becomes a union: a package earns a `package_props.go` if **any**
survey covers it. The wasm survey keeps its `main`/`internal` exclusions for its
own fields; the capability survey has no such restriction. In packages the wasm
survey cannot reach, the `WASM*` fields stay `WASMUnknown` — asserting nothing,
which is true.

Consequence: roughly 60 new `package_props.go` files under `apps/` and
`internal/`, whose wasm fields are all `WASMUnknown`.

### SD7 — generation becomes a field-preserving overlay

Today `GenerateProps` renders a whole file from the wasm survey and hand-preserves
the single curated field (`Kind`). With two independent surveys that does not
scale: each would clobber the other's fields.

Generation is restructured so that a survey **overlays only the fields it
computes** onto whatever the existing file declares. Every other field —
another survey's verdicts, and curated fields like `Kind` — is preserved by
construction rather than by a hand-maintained special case. The existing
`Kind`-preservation block becomes one instance of the general rule.

### SD8 — the keelson table

A new introspection table `package_capabilities` (ADR-0094), one row per
registered package, sourced from the **runtime registry** (`packageprops.All()`)
rather than the whole-repo static table. Every existing keelson table describes
the live process, and the registry answers the question this table exists to
answer: *what can this binary do?* The static `proptable` answers a different
question (what does the repository contain) and remains available to tooling that
wants it.

This makes the table the first production consumer of `packageprops.All()`.

Columns: `import_path`, `surveyed`, `caps_direct`, `caps_reachable` (the latter
two as `StringList`s of stable lowercase capability tokens). `arrayJoin` gives
the long form for anyone who wants one row per package × capability, so the
table does not need to pick that shape up front.

### SD9 — opt-out build tag `boxer_disable_packagecaps`

The table is present by default and removed by a tag, mirroring the existing
`boxer_enable_profiling` naming with the opposite polarity (this is the first
`boxer_disable_*` tag). Implemented as the repository's standard paired
`//go:build boxer_disable_packagecaps` / `//go:build !boxer_disable_packagecaps`
files.

When disabled the provider stays registered and yields **zero rows with the
correct schema**, matching the established "degrade to empty, never a query
error" precedent (`sbom` without `SbomPath`, `build` without `runinfo.Init`).
Queries against a disabled build still parse and run; they simply return nothing.

The tag does not strip the `Props` fields themselves — those are generated
per-package declarations, and making the struct shape depend on a build tag would
fracture it across build configurations for no benefit.

## Alternatives

**capslock as an external binary via `extbin`.** Superficially attractive: it
honours ADR-0118, mirrors the TinyGo precedent, and keeps SSA out of `app`.
Rejected because it buys that with an external toolchain install, a JSON contract
that drifts silently across capslock releases, and a harder airgap story — to
avoid a dependency the module already transitively carries. The TinyGo precedent
does not transfer: TinyGo is shelled out because a compiler cannot be linked in,
not out of preference.

**Direct capabilities only.** Half the bytes and, given saturation, nearly all
the positive signal. Rejected because it discards the provable-absence claims,
which are the ones a reviewer can actually lean on.

**Storing capslock's transitive-only set verbatim.** The obvious mapping, and
wrong for the reasons in SD5: it makes the chokepoint package report `safe`.

**A locally renumbered dense capability index.** Would have kept the mask
narrower. Rejected on the `AspectSet` precedent: a dense local index makes
upstream removals renumber the set and break every golden file. Proto numbers are
stable by contract and are free.

**One row per package × capability (long form).** More directly filterable, but
loses the row for a package with no capabilities, and diverges from the one
row-per-registry-entry shape of `extbin` and `sbom`. `arrayJoin` recovers the
long form from the wide one; the reverse is lossy.

**Sourcing the table from the static `proptable`.** Would report the whole
repository regardless of what is linked. Rejected as answering a different
question than every other keelson table asks; also drags an 84 KB generated table
into every binary that enables introspection.

**A runtime flag instead of a build tag.** Consistent with
`KEELSON_INTROSPECT_ENABLE`, but the plausible reason to want this data gone is
that a shipped binary should not enumerate its own privilege surface to whoever
can reach the query endpoint. A runtime flag leaves the data in the binary; the
request was specifically for a compile-time opt-out.

## Consequences

- **go.mod gains one module** (`github.com/google/capslock`). No new transitive
  modules. `x/tools` stays at v0.45.0 by MVS.
- **Generation needs ~8 GB of RAM** and ~30s for the whole tree. It is a
  developer/CI codegen step, not a build or runtime step, but it is not something
  that will run comfortably on a small machine.
- **SSA links into `app`**, growing it. Accepted under SD1.
- **~60 new `package_props.go` files** under previously unsurveyed trees (SD6).
- **`proptable_gen.go` must be regenerated**, and is already 9 entries stale
  against the tree — the regeneration folds in that pre-existing drift.
- **`Props` grows by 8 bytes**, copied through the registry and the static table.
  Both hold it by value; both stay copyable.
- **Set algebra must mask the safe marker** (SD4). `CapabilitySet.Privileged()`
  and `Subset` exist for this; a raw `&^` across two sets is a bug.
- **The extbin chokepoint became measurable, and was measured.** The survey
  attributes direct `exec` to six packages besides `extbin` itself and the exempt
  `eh`, which prompted a source census: eleven packages take the `*exec.Cmd` that
  `extbin.Command` returns and spawn it themselves. That is `extbin`'s
  "resolve-not-own" design working as intended, but it means the centralised
  property is binary *resolution*, not *invocation* — and that CS012 cannot see a
  spawn, since calling a method on a handed-back value needs no import. ADR-0118
  gained a §Scope section stating this precisely (2026-07-15); its title now says
  "resolution" rather than "invocation".

  Worth noting how the two instruments divided the labour, since it generalises:
  the survey found the *anomaly* (packages holding exec that ADR-0118 implied
  should not), and a grep established the *fact* (eleven, not six). Per SD5a the
  survey could not have been the authority — it is a lower bound. An
  under-approximation is still a good detector; it is a bad census.

## References

- ADR-0064 — dependency collection (`Dir`-driven, env-free collectors).
- ADR-0078 — the wasm survey.
- ADR-0080 — `packageprops`, per-package declarations, the hybrid lifecycle.
- ADR-0094 — keelson introspection tables.
- ADR-0118 — `extbin`, the external-process chokepoint.
- [google/capslock](https://github.com/google/capslock)
