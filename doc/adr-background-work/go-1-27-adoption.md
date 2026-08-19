---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Written 2026-08-19 against the tree at
> `a7966f04`. The `go fix` census in §4 was **measured** on this tree with
> `go1.26.5`, one developer machine. Everything attributed to Go 1.27 is **read
> from the release notes, not verified locally** — no 1.27 toolchain is
> installed here (§5), so every 1.27 claim below is a claim to re-check, not a
> measurement. Feeds a not-yet-written ADR; once that exists it is
> authoritative and this is a snapshot.

# Go 1.27 — what it retires, what it unlocks, and what the adoption costs

[Go 1.27](https://go.dev/doc/go1.27) landed in August 2026. Four of its changes
touch decisions this repository already recorded, one of them a deferral
written in the source itself. This page inventories the four, measures the one
that can be measured today, and costs the work.

| Go 1.27 change | Stake here |
| --- | --- |
| `encoding/json/v2` graduates; experiment becomes opt-*out* (`GOEXPERIMENT=nojsonv2`) | Retires `goexperiment.jsonv2` — a deferral recorded in `gov/buildtags` and in [downstream-adoption-skeleton](./downstream-adoption-skeleton.md) §The tag endgame |
| Generic methods | Removes the constraint [ADR-0023](../adr/0023-leeway-lwq-go-api.md) cites twice for its free-function terminals |
| `goroutineleak` profile generally available | A profile kind the pprof-as-data path and the `imzrt` Profiles tab do not have |
| `go fix` gains four modernizers, drops one | Mostly *not* a 1.27 story — 23 fixers already ship in `go1.26.5` and are unapplied here |

Two of the four (the tag retirement, the toolchain bump itself) are Tier-1 under
[CODINGSTANDARDS § Design Before Code](../../CODINGSTANDARDS.md#design-before-code)
— "build/toolchain gates (`./tags`, entry-points baseline, lint+license gates)"
is named in the Tier-1 list — so they need an ADR before code. The `go fix` run
and the `goroutineleak` surface are not.

## 1. The tag endgame

The deferral is written twice, and both texts predict the same outcome.
`public/gov/buildtags/gov_buildtags.go` on `RequiredTags`:

> Retires when `encoding/json/v2` graduates into the standard library, at which
> point this set is expected to become empty and a consumer needs no build tags
> at all.

and [downstream-adoption-skeleton](./downstream-adoption-skeleton.md) §The tag
endgame costs it: one file carrying the constraint, and **34 files importing
`encoding/json/v2` / `jsontext` need no change**, because graduation preserves
the import path. Both hold — the 34 is confirmed on today's tree.

One correction to the framing "build without tags": that is true for a
*consumer*, not for boxer. `boxer_enable_profiling` stays, because it selects
between `profiling_enabled.go` and `profiling_disabled.go`, and the enabled arm
blank-imports `net/http/pprof` — which registers debug handlers on
`http.DefaultServeMux` as an import side effect. Making that unconditional is a
different decision with a security surface, not a tidy-up. **boxer goes 2 tags
→ 1; a consumer goes 2 → 0.**

### What moves

| Surface | Change |
| --- | --- |
| [`./tags`](../../tags) | `boxer_enable_profiling,goexperiment.jsonv2` → `boxer_enable_profiling` |
| `public/gov/buildtags` | `RequiredTags` → empty; `goexperiment.jsonv2` joins `RetiredTags` with the new ADR and `2026-08`; `TestRequiredAndOptionalMatchTagsFile` must still pass with an empty required set |
| [README.md](../../README.md) §build tags | Drops the "Go experiments" clause |
| [ENGINEERING_PRACTICES §3](../ENGINEERING_PRACTICES.md#3-build-tag-discipline) | Active-tag sentence; the retirement joins the `identifier_tag_fixed<N>` / `llm_generated_*` history paragraph |
| [ADR-0053](../adr/0053-retire-alpha-cbor-for-jsonv2.md), [ADR-0078](../adr/0078-tinygo-wasm-amenability-survey.md) §SD5, [ADR-0004](../adr/0004-license-gate-cyclonedx.md) §SD9, [ADR-0083](../adr/0083-retire-llm-generated-build-tags.md) | Accepted ADRs naming the tag — dated `## Update`, not silent rewrites |
| `public/code/analysis/golang/wasmsurvey` | `ReasonGoexperimentJSONv2`, its probe's `GOEXPERIMENT=jsonv2` child env, and the reason-string matcher — the TinyGo probe's reason for that tier disappears |
| [`.github/workflows/codeql.yaml`](../../.github/workflows/codeql.yaml) | Reads `./tags` into `GOFLAGS`; no edit needed, but re-verify it still resolves |
| [downstream-adoption-skeleton](./downstream-adoption-skeleton.md) | §The tag endgame becomes history rather than forecast |

### Two questions the release notes do not answer

1. **Is `-tags=goexperiment.jsonv2` inert or fatal under 1.27?** The repo has
   never set `GOEXPERIMENT=jsonv2`; it passes the *build tag* the experiment
   implies, and the stdlib's `//go:build goexperiment.jsonv2` constraints made
   that work. In 1.27 those constraints flip to the `nojsonv2` sense. The
   likely outcome is an inert unknown tag, but "likely" is not good enough for
   a file every consumer copies — a 1.27 build with and without the tag decides
   it, and decides whether the tag is merely *no longer required* or genuinely
   *retired* (`RetiredTags` means "must not be present at all", and a finding
   naming an ADR).
2. **Does `go tool` delivery unblock?** The skeleton measured, on `go1.26.5`,
   that `go tool github.com/stergiotis/boxer/public/app` fails with
   `encoding/json/v2: build constraints exclude all Go files`, because `go tool`
   accepts no `-tags` and ignores it in `GOFLAGS`. That measurement is dated
   with an explicit "re-run it rather than cite it". With required tags empty,
   it should pass for a consumer. This is the adoption payoff and should be
   re-measured, not assumed.

A third, softer question: `RetiredTags` fires on consumers who may still be on
Go 1.26, for whom the tag is *required*. Bumping the `go` directive to `1.27.0`
(§5) forces those consumers to 1.27 anyway, which makes "retired" coherent —
but the ordering matters, and the ADR should say it.

## 2. Generic methods — where they actually pay

Go 1.27 lets a method declare its own type parameters. Two limits shape the
answer below: an *interface* method still cannot declare type parameters, and a
type parameter still cannot be a receiver.

**`leeway/dml` gains essentially nothing.** The generic surface there is
type-parameterised over the *entity*, not over a value the entity produces:

- `WriteArrowRecords[E TransferRecordsI](ent E, …)` (`lw_dml_arrow_utils.go`) —
  `E` is the receiver. A method needs a concrete receiver type, so this stays a
  free function.
- The generated codecs — `TextDocAddSections[TextAttr, TextSec, Ent, DML]`,
  `…FillFromArrow[TextAttrs, TextMembs]`, `…ReadRow[TextAttrs, TextMembs]` — bind the DML
  and the reader views as type parameters for the same reason. Same verdict.

Where a *method* is wanted on the dml side, the generator can already emit a
plain (non-generic) method on the concrete generated type; nothing was blocking
that.

**`recordstore` gains one cosmetic constructor.** `gen/store_emit.go:1458`
emits `func NewXCache[W comparable](st *XStore, cfg XCacheConfig) *XCache[W]`
for six stores (`widget`, `device`, `ledger`, `pushout`, `provenance`,
`asset`). It could become `func (st *XStore) NewCache[W comparable](cfg …)`.
One generator line plus a regen, and it reads better — but it is ergonomics,
not capability, and it churns every call site. Worth doing only alongside
another generator change, not on its own.

**The real payers are elsewhere in leeway.**

| Site | Today | With generic methods |
| --- | --- | --- |
| `marshall/go/marshallreflect` | `*SectionReaders` is a fluent builder (`PlainColumn`, `Section` return the receiver), but every type-introducing terminal is a free function taking it as the first argument: `Unmarshal[T]`, `Detect[T]`, `DetectAll[T]`, `ReadComponent[T]`, `InspectLookup[T]` | The chain closes: `NewSectionReaders(n).PlainColumn(…).Section(…).Unmarshal(&out, lookup)`, with `T` inferred from `out` |
| `anchor/ecsdemo/stage2` | `Extract[T any](inst *FatRow) ([]T, error)` | `inst.Extract[T]()` — the shape the leeway-components skill teaches |
| [ADR-0023](../adr/0023-leeway-lwq-go-api.md) (`lwq`, unbuilt) | States the constraint twice: "type-introducing terminals (`Collect`, `Reduce`, `Sum`) are free functions **because Go does not allow generic methods**", and calls the result "less fluent than fully-chainable APIs in other languages" | The rationale is void. The package does not exist yet, so this is a design edit, not a migration |

[ADR-0014](../adr/0014-imzero2-context-typed-ui.md) §O1 also cites generic
methods, but for a different property (per-instantiation specialisation), which
1.27 does not provide. That kill-reason stands.

Note that all of these are `public/` API — Tier 1. `marshallreflect` is the
only one with live callers, so it is the only one carrying a migration cost;
keeping the free functions as thin forwarders during a window is the obvious
descope.

## 3. `goroutineleak`

Generally available in 1.27, having been the `goroutineleakprofile` experiment
in 1.26. It is a predefined profile — "stack traces of all leaked goroutines" —
served from `runtime/pprof` and, via `net/http/pprof`, from
`/debug/pprof/goroutineleak`. It finds goroutines blocked on a concurrency
primitive that can no longer unblock, by garbage-collector reachability
analysis; it cannot see leaks reachable through globals or through a runnable
goroutine's locals.

"In our repertoire" reads two ways, and both are worth having:

**As a diagnostic surface.** The HTTP endpoint arrives free — the
`boxer_enable_profiling` arm already blank-imports `net/http/pprof`, so
`--pprofHttpListenAddress` serves it the moment the toolchain provides it. The
data path needs work:

- `public/observability/profiling/pprofarrow` — `inferKind` classifies by
  sample-type signature and will almost certainly see the same
  `goroutine`/`count` signature as the existing goroutine profile, i.e. return
  `"goroutine"`. That is the case `WithKindHint` already exists for (block and
  mutex collide the same way and come back as `"contention"`). Confirm the
  signature, then pass a hint. **This must be checked, not assumed** — the
  alias `pprof_goroutineleak` and the kind hint have to agree, or the ad-hoc
  dataset lands on the wrong handle.
- `apps/imzrt/imzrt_panel_profiles.go` — one entry in `profileKinds`
  (`{key: "goroutineleak", label: "Leaked goroutines", capture: captureLookup("goroutineleak"), unit: "goroutines"}`).
  The package comment explains why block and mutex are deliberately absent —
  they are empty unless `SetBlockProfileRate` / `SetMutexProfileFraction` are
  set, and imzrt does not mutate runtime tunables ([ADR-0061](../adr/0061-imzero2-imzrt-go-runtime-dashboard.md) §SD6).
  `goroutineleak` needs no tunable, so it does not hit that rule — but it is
  not free either: it drives a GC-based analysis, which on the render thread of
  a live dashboard is a visible hitch. The existing capture path already runs
  through `bgjob` off the render thread, which is the right place for it; the
  cost belongs in an ADR-0061 update, since "observe-only" acquires a caveat.
- `apps/imzrt/imzrt_tour.go` and `apps/sqlapplet/bookpprof` — the Profiles
  demo and the pprof book are rosters that go stale silently.

**As a test-lane capability.** `public/keelson/runtime/task/spawn_leak_test.go`
guards a real past bug by counting goroutines and allowing `baseline+10` slack
— the standard shape when no better tool exists. A `goroutineleak` profile
turns that into a direct assertion (leaked stacks, named), with no slack
constant. That is the higher-value half of "repertoire", and it is a small,
self-contained change once the toolchain is in place.

## 4. `go fix` — largely available today, and unapplied

The framing that this waits for 1.27 is wrong. `go1.26.5` already registers 23
fixers, and none have been applied to this tree. The 1.27 delta is four added
(`atomictypes`, `embedlit`, `slicesbackward`, `unsafefuncs`), one **removed**
(`fmtappendf`), one renamed (`waitgroup` → `waitgroupgo`).

Measured 2026-08-19, `go fix -diff -tags "$(cat ./tags)" ./...` on `go1.26.5`:
**306 files, 12,827 diff lines, 3.7 s.** Per analyzer (each run alone; files
and analyzers overlap, so the column does not sum to the total):

| Fixer | Files | Changed lines | Note |
| --- | ---: | ---: | --- |
| `rangeint` | 173 | 3100 | 3-clause loop → `for i := range n`; the repo already writes this form by hand |
| `minmax` | 41 | 810 | if/else → `min` / `max` |
| `any` | 32 | 570 | `interface{}` → `any` |
| `stringscut` | 16 | 554 | `strings.Index` + slicing → `strings.Cut` |
| `mapsloop` | 5 | 449 | explicit loop → `maps` package |
| `waitgroup` | 10 | 274 | `Add(1)`/`go`/`Done()` → `wg.Go`; **renamed in 1.27** |
| `slicessort` | 6 | 260 | `sort.Slice` → `slices.Sort` |
| `slicescontains` | 18 | 193 | loop → `slices.Contains` |
| `fmtappendf` | 4 | 166 | `[]byte(fmt.Sprintf)` → `fmt.Appendf`; **removed in 1.27** |
| `stditerators` | 1 | 120 | Len/At → iterators |
| `stringsseq` | 34 | 88 | `Split`/`Fields` → `SplitSeq`/`FieldsSeq` |
| `stringsbuilder` | 8 | 76 | `+=` → `strings.Builder` |
| `reflecttypefor` | 4 | 50 | `reflect.TypeOf(x)` → `TypeFor[T]()` |
| `inline` | 7 | 16 | `//go:fix inline` directives |
| `stringscutprefix` | 3 | 12 | `HasPrefix`/`TrimPrefix` → `CutPrefix` |
| `newexpr` | 2 | 5 | go1.26 `new(expr)` |
| `forvar` / `plusbuild` | 1 / 1 | 3 / 3 | |
| `omitzero` | 1 | 2 | declined its own behaviour-changing alternative fix, unprompted |
| `testingcontext`, `hostport`, `buildtag` | 0 | 0 | clean |

Three findings worth acting on:

- **Two fixers are use-it-or-lose-it.** `fmtappendf` (4 files) disappears in
  1.27 on stylistic grounds; if its change is wanted, it must run before the
  bump. `waitgroup` (10 files) renames, which matters only for a scripted lane.
- **No generated file is touched.** 306 files; the three whose names matched a
  generated-file grep are handwritten helpers under
  `public/parsing/antlr4utils`. So the usual hazard — fixing a file that the
  next `boxer runtimecodegen` reverts — does not arise, and the generator
  templates need no parallel edit.
- **118 of the 306 are `_test.go`.** Splitting test-only fixes into their own
  commit makes both halves reviewable.

Sequencing that keeps review honest: one commit per fixer, largest last, each
verified with `scripts/ci/gotest.sh` (which runs `-race`). `rangeint` alone is
more than half the diff and should not ride along with anything else. Note
`gofmt -w` must **not** be used to tidy the result — it mangles doc-comment
quotes; `go fix` applies scoped edits and does not reformat whole files.

Whether `go fix -diff` then becomes a lint step (fail on non-empty diff) is a
separate call. It is cheap (3.7 s) and it is exactly the kind of universal step
`gov gate`'s `DefaultSteps()` exists to publish — but it also makes every new
fixer in every future Go release an immediate hard gate, which is a policy
choice, not a convenience.

## 5. The toolchain itself — what actually gates this

No Go 1.27 is installed. The system toolchain is `go1.26.5` (built with
`GOEXPERIMENT=nodwarf5`), with a `go1.26.1` under `/usr/local/go` and an older
per-user SDK. `GOTOOLCHAIN=local`, so `go.mod`'s `toolchain` line will not
fetch one — deliberately, and [ADR-0095](../adr/0095-airgapped-build-bundle.md)
and [howto/airgapped-build](../howto/airgapped-build.md) both call that
load-bearing. Acquiring 1.27 is therefore step zero, and it is a host change,
not a repo change.

Consequences of the `go.mod` bump itself, none of them optional:

- **`go 1.27.0` is required for generic methods.** The language version comes
  from the module directive; a per-file `//go:build go1.27` does not raise it.
  So §2 cannot land before the bump, and the bump forces every consumer to
  1.27 — which is what makes retiring the tag (§1) coherent.
- **`go mod tidy` will rewrite `go.mod`** for `go 1.27+`, merging require
  blocks into at most two. That is a large one-time diff with no semantic
  content; land it alone. Check it with `go mod tidy --diff`, never `tidy`
  followed by `git diff`.
- **`go test` starts running the `stdversion` vet check by default**, reporting
  standard-library symbols newer than the configured Go version. Expect
  findings on the first run.
- **CI needs no edit** — all seven workflows use `go-version-file: 'go.mod'`.
- The local `nodwarf5` experiment is a property of the *distribution's* build.
  An upstream 1.27 will not carry it, so DWARF5 debug info returns; harmless
  for correctness, visible in binary size and in whatever consumes debug info.
- The airgap bundle pins a toolchain version in its generated env; re-cut it
  after the bump.

## Plan

Ordered by dependency. M1 and M2 need no new toolchain and can land now.

| | Milestone | Gated on | Verification lane |
| --- | --- | --- | --- |
| **M0** | Install Go 1.27 beside the current toolchain; build and `scripts/ci/gotest.sh` under it, still carrying both tags. Answer §1's two questions (tag inert or fatal; `go tool` delivery) and §3's one (goroutineleak sample-type signature) | host | `gotest.sh`, `lint.sh` under both toolchains |
| **M1** | `go fix` sweep on `go1.26.5`, one commit per fixer, `fmtappendf` included before it disappears | — | `gotest.sh` per commit |
| **M2** | `goroutineleak` groundwork that does not need 1.27: `pprofarrow` kind hint plumbing, roster shape | — | `pprofarrow` unit tests |
| **M3** | Toolchain bump: `go 1.27.0` + `toolchain`, `go mod tidy` (own commit), `stdversion` fallout | M0 | all CI lanes |
| **M4** | Tag retirement: ADR first (Tier 1 — Surfaces / Migration / Verification), then `./tags`, `gov/buildtags` required→empty + retired entry, README, ENGINEERING_PRACTICES, dated Updates on ADR-0053/0078/0004/0083, `wasmsurvey` reason removal | M3, M0's answers | `gov gate` (its own `buildtags` step), `lint.sh`, a consumer-shaped `go tool` probe |
| **M5** | `goroutineleak` surfaces: imzrt `profileKinds` + tour + `bookpprof`, ADR-0061 dated Update for the GC-cost caveat; replace the goroutine-counting slack in `task/spawn_leak_test.go` | M3, M2 | live gate on the Profiles tab (returning rows is not evidence it draws — assert the Arrow header), `gotest.sh` |
| **M6** | Generic methods where they pay: `marshallreflect` terminals as methods on `*SectionReaders` with the free functions kept as forwarders; ADR-0023 design edit (it is `proposed`, so edit in place); `FatRow.Extract`. `recordstore`'s cache constructor only if a generator change is happening anyway | M3 | `gotest.sh`, leeway golden lanes |

M4 and M6 both change `public/` surfaces and both want an ADR. Whether that is
one ADR ("adopt Go 1.27") or two is a judgement call: they share a trigger and
nothing else, and the tag retirement has a downstream migration story the
generic-method change does not.

## Open questions

1. Is `-tags=goexperiment.jsonv2` inert under 1.27, and does the tag become
   *retired* or merely *not required*? (M0)
2. Does `go tool <consumer binary>` delivery unblock once required tags are
   empty? (M0) — the payoff the skeleton page predicted.
3. What sample-type signature does the `goroutineleak` profile carry, and does
   `inferKind` collide it with `goroutine`? (M0)
4. Does `go fix -diff` become a `gov gate` step, accepting that each future Go
   release's new fixers land as an immediate hard gate?
5. One ADR or two for M4 and M6?
6. Does anything want `GOEXPERIMENT=simd`? Out of scope here — named only so
   the next reader does not assume it was missed.
