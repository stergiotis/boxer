---
type: adr
status: accepted
date: 2026-08-30
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-30
---

# ADR-0212: Split the pprof HTTP listener out, retire `boxer_enable_profiling`

## Context

`boxer_enable_profiling` was the last entry in [`./tags`](../../tags).
[ADR-0199 §SD2](./0199-adopt-go-1-27.md) kept it when the other two went, on one
ground: the enabled arm blank-imports `net/http/pprof`, whose `init` registers
`/debug/pprof/*` on `http.DefaultServeMux`, and making that unconditional "is a
different decision with a security surface". This is that decision.

Two measurements frame it.

**The runtime gate was already there.** `--pprofCpuOutputFile` and
`--pprofHttpListenAddress` were registered unconditionally; the tag only chose
whether their actions did the work or returned *compile with the build tag*.
Nothing profiled until a flag was passed, either way — so "add a runtime flag"
was not the missing piece.

**The cost is not in boxer's binaries, it is in a consumer's.** `public/app` and
the imzero2 host link `net/http` and `runtime/pprof` regardless of the tag, so
the enabled arm added exactly two stdlib packages (`net/http/pprof`,
`internal/profile`) and under 0.3% of binary. A consumer-shaped module that links
the profiling flags and nothing else pulling `net/http` pays differently:

| Shape | Binary | Packages |
| --- | --- | --- |
| tag off (`profiling_disabled.go`) | 5,565,764 B | 126 |
| tag on | 11,909,490 B | 238 |

`net/http/pprof` drags in `net/http` and behind it the crypto/TLS/FIPS tree.
Simply deleting the build constraints would hand every importer of
`public/observability/profiling` +6.3 MB and an HTTP server whether or not it
profiles — the opposite of what ADR-0199 D2 bought. The shape of the finding is
[ADR-0203](./0203-map-widget-without-the-http-stack.md)'s (superseded by
ADR-0204) on the Rust side: the transport costs more than the feature it serves.

## Decision

Split the package along the `net/http` line; the tag then has nothing left to
gate and is retired.

- **`profiling`** keeps the file-based capture — `--pprofCpuOutputFile` over
  `runtime/pprof`, and `ProfilingHandleExit`, which is where the profile is
  serialised. It imports no `net/*`.
- **`profiling/pprofhttp`** owns `--pprofHttpListenAddress`. A host that wants
  the listener imports it and adds `pprofhttp.Flags` beside
  `profiling.ProfilingFlags`; `public/app` and the imzero2 host both do, so no
  flag name, category or behaviour changes for a user of either.
- The listener serves a mux built from `net/http/pprof`'s exported handlers,
  not `http.DefaultServeMux`. `pprofhttp.NewServeMux` is exported for a host
  that already runs a listener.
- `./tags` becomes empty. `boxer_enable_profiling` moves from `OptionalTags` to
  `RetiredTags`, so a consumer still carrying it is told which ADR removed it.

### SD1 — the always-on cost moves, it does not vanish ✓

For the same consumer-shaped module, after the split:

| Shape | Binary | Packages |
| --- | --- | --- |
| `profiling` alone | 6,059,613 B | 137 |
| `profiling` + `pprofhttp` | 11,911,647 B | 239 |

Importing `profiling` and never profiling now costs +494 KB and 11 packages over
the old compiled-out arm, because `runtime/pprof` is unconditional. That is the
price of the decision and it is not zero. It buys deleting an arm whose whole
purpose was to make a flag return an error, and it is an order of magnitude
below what the same consumer was being asked to pay for the listener.

### SD2 — the private mux is not what confines the side effect ✓

Importing `net/http/pprof` runs its `init`, so `pprofhttp` registers on
`http.DefaultServeMux` whatever it does with the handlers afterwards. No import
can prevent that. What confines the registration is the **split**: a binary that
does not import `pprofhttp` does not link `net/http/pprof`, and boxer's own
other servers already pass explicit handlers rather than serving `nil`.

The private mux is there for a smaller and separate reason. Serving `nil` serves
everything on the default mux — including whatever a host or one of its
dependencies registered globally — so the listener's exposed surface was not
knowable from this package. Now it is exactly pprof's five paths, GET-only as
upstream has made them since Go 1.22.

## Alternatives

- **Keep the tag (ADR-0199 §SD2).** Rejected: it gates a compile-out arm that
  duplicates a runtime flag, and every CI script already builds and analyses the
  enabled arm — so the tag's only real effect was on consumers, who got the
  broken-by-default arm rather than protection.
- **Delete the two `//go:build` lines and nothing else.** Rejected: a two-minute
  change that ships the 6.3 MB and the `DefaultServeMux` registration to every
  consumer, and leaves ADR-0199 §SD2's objection unanswered.
- **Drop `net/http/pprof` and write the handlers over `runtime/pprof`
  directly.** Rejected: it would avoid `internal/profile`, but `net/http` comes
  with the listener anyway, so it saves a fraction of the weight while losing
  `/debug/pprof/symbol` and `?seconds=` delta profiles — both of which `go tool
  pprof` uses against a live process.

## Surfaces

| Surface | Change | Moves with it |
| --- | --- | --- |
| `public/observability/profiling` | `ProfilingFlags` loses the listener flag; the build-tag arms are gone | any downstream host that wants the listener — see Migration |
| `public/observability/profiling/pprofhttp` | new package: `Flags`, `NewServeMux` | its `package_props.go`, and the harvested `proptable.out.go` |
| [`./tags`](../../tags) | one tag → empty | every consuming repository's copied tags file |
| `public/gov/buildtags` | `OptionalTags` → empty; `RetiredTags` gains `boxer_enable_profiling` | `TestCheckAcceptsOptional`, `TestCheckReportsRetiredFamilies` |
| `public/app`, `public/thestack/cmd/imzero2` | one import, one flag slice each | nothing — the CLI surface is unchanged |

## Migration

- **Breaks.** A downstream host that composed `profiling.ProfilingFlags` and
  relied on `--pprofHttpListenAddress` appearing loses that flag, at compile
  time silently — the slice is still valid, just shorter.
- **Path.** Import `.../profiling/pprofhttp` and add `pprofhttp.Flags` to the
  same flag composition. Then drop `boxer_enable_profiling` from the copied
  `./tags`; keeping it is inert until the next pin brings the retired-set
  finding.
- **Regeneration.** `props harvest --tracked --emit go` for the new package's
  entry in `proptable.out.go`. No other generator input changed.
- **Old shape.** `profiling_enabled.go` / `profiling_disabled.go` are deleted
  outright; the tag is recorded in the append-only `RetiredTags` and does not
  come back.

## Verification plan

| Lane | What would fail | Gap |
| --- | --- | --- |
| default `go test` | `TestCpuProfileStopPath` — it ran only under the tag before, so the file path had no unconditional lane at all | — |
| default `go test` | `TestServeMuxServesPprofPathsAndNothingElse` — the hand-registered paths, and the 404 that says the default mux is not being served | it exercises the mux, not the listener goroutine |
| `gov gate`'s `buildtags` step | `TestRequiredAndOptionalMatchTagsFile` if `./tags` and the published sets disagree | a consumer is only checked when it runs the gate |
| `lint.sh` `props drift` / `props verify` | the new package missing from `proptable.out.go` | — |
| — | nothing asserts `profiling` stays free of `net/*`; the split is a convention a future import could quietly undo | **This is the gap.** A dependency assertion would close it and is not written |

## Consequences

### Positive

- boxer builds with **no build tags at all**, completing what ADR-0199 D2
  started; `$(cat ./tags)` is now an empty string every script tolerates.
- A consumer that wants a CPU profile in a file no longer links an HTTP server
  and a TLS stack to get one.
- The CPU-profile stop path is tested in the default lane rather than in a tag
  arm CI happened to build.
- What the pprof listener exposes is readable in one function instead of being
  a property of every `init` in the binary.

### Negative

- `runtime/pprof` is unconditional in every importer of `profiling` — +494 KB
  on a small binary (SD1).
- The `net/http/pprof` `init` side effect still exists for a host that imports
  `pprofhttp`; the split confines it, it does not remove it (SD2).
- One more package, and a host wanting the listener must now know to import it.

### Neutral

- No flag name, category or default changes, so nothing a user types moves.
- No module dependency changes, so the SBOM and the license gate
  ([ADR-0004](./0004-license-gate-cyclonedx.md)) are untouched.

## Status

Accepted 2026-08-30.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0199](./0199-adopt-go-1-27.md) — retired the other two tags; its §SD2
  kept this one, and its Alternatives rejected exactly this change.
- [ADR-0203](./0203-map-widget-without-the-http-stack.md) (superseded by
  ADR-0204) — the same transport-outweighs-feature finding, measured in Rust.
- [ADR-0080](./0080-packageprops-per-package-declarations.md) — the `package_props.go`
  the new package carries.
- [pprof profiles as data](../adr-background-work/pprof-profiles-as-data.md) —
  what is done with a profile once captured.
