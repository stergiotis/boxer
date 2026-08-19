---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Written 2026-08-19 against the tree at
> `a7966f04`, one developer machine. The `go fix` census in §4 was measured on
> `go1.26.5`; **§M0 was then run against a real `go1.27.0`** — the tag matrix,
> the `go tool` delivery probe, the `goroutineleak` shape and cost, the
> `stdversion` fallout and the `go mod tidy` churn are all measured, in a
> detached worktree at `29733c55`. Claims still taken from the release notes
> rather than measured are marked as such. Feeds a not-yet-written ADR; once
> that exists it is authoritative and this is a snapshot.

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

### The questions, answered — M0, 2026-08-19 on `go1.27.0`

**The tag is inert.** A full-repo `go build ./...` under `go1.27.0`, with
`GOFLAGS` cleared so the tag set is actually what the flag says, gives an
identical result in all three configurations — today's set, the target set, and
no tags at all: one failing package, and it fails for an unrelated reason
(below). Under `go1.26.5` the same build without the tag fails as expected, with
`imports encoding/json/v2: build constraints exclude all Go files`. So the tag
neither helps nor harms under 1.27, and the repo builds with no tags at all.

The mechanism is worth recording, because it is not what the release note
implies: the 1.27 standard library **still carries `//go:build goexperiment.jsonv2`**
on the `encoding/json/v2` and `jsontext` files (99 files reference the tag).
What changed is the baseline — `internal/buildcfg` now sets `JSONv2: true`, so
the toolchain supplies the tag itself. Nothing was un-gated; the default moved.
That also means `goexperiment.jsonv2` is a *recognised* tag, not an unknown one,
which supports classifying it as **retired** rather than merely not-required.

**`go tool` delivery unblocks.** A throwaway consumer module — `go 1.27.0`, a
`tool github.com/stergiotis/boxer/public/app` directive, a `replace` to the
worktree, no tags anywhere — runs the full boxer CLI:
`go tool github.com/stergiotis/boxer/public/app --help` prints the command tree.
This is the payoff [downstream-adoption-skeleton](./downstream-adoption-skeleton.md)
predicted and asked to be re-measured rather than cited; it now measures.

**But graduation changed the API.** This is the finding that was not in the
release notes, and it is the only thing in the repo that 1.27 actually breaks.
Diffing the exported surface of both packages between `go1.26.5` and `go1.27.0`:

| Package | 1.26.5 | 1.27.0 |
| --- | --- | --- |
| `jsontext` | `func (t Token) Float() float64` | `(float64, error)` |
| `jsontext` | `func (t Token) Int() int64` | `(int64, error)` |
| `jsontext` | `func (t Token) Uint() uint64` | `(uint64, error)` |
| `jsontext` | `func AppendFormat(dst, src []byte, …)` | `AppendFormat[Bytes ~[]byte \| ~string](dst []byte, src Bytes, …)` — source-compatible for `[]byte` callers |
| `jsontext` | — | new: `AppendFloat`, `Float32(float32) Token`, `Token.Float32()` |
| `json/v2` | `func DiscardUnknownMembers(v bool) Options` | **removed** |
| `json/v2` | struct-tag options `case format ignore **inline** omitempty omitzero strict string **unknown**` | `inline` and `unknown` replaced by **`embed`** — and an unrecognised option is **silently ignored** |

Blast radius across 26 files importing either `jsontext`: **one file, two
lines** — `public/semistructured/leeway/dml/example/cli.go:166,168`, which reads
a number token as a float and then as an int. `DiscardUnknownMembers` is unused
here. The fix is mechanical (take the error, wrap it with `eb.Build()`), and a
verified patch exists.

**The struct-tag rename is the dangerous one, because it is silent.** The last
row of that table costs no compile error and no runtime error — a field tagged
`json:",inline"` simply stops being inlined and starts emitting as an ordinary
nested member under its Go field name. Measured on both toolchains with the
same four-line program:

| | `go1.26.5` | `go1.27.0` |
| --- | --- | --- |
| `Extra map[string]any` tagged `,inline`, key colliding with an emitted member | `{"model":"a"` + error `jsontext: duplicate object member name "model"` | `{"model":"a","Extra":{"model":"b"}}`, **no error** |
| same, tagged `,embed` | — | `{"model":"a"` + the same duplicate-member error |

One site in the tree carries the old spelling —
`public/llm/openaichat/llm_openaichat.go:398`, where `Extra` exists precisely to
flatten provider-specific request members into the request object, with the
duplicate-name error as its documented collision guarantee. Under 1.27 that
guarantee silently disappears and the wire shape changes. Changing the tag to
`,embed` restores both, verified.

Nothing in the toolchain would have caught this: `go build` is clean, `go vet`
is clean, and the wrong output is valid JSON. What caught it was the package's
own `TestEncodeRequestExtraCollisionFails`, which pins the guarantee rather than
the happy path. That is the argument for M0 running the whole test suite and not
just building.

**The fix is not backward-compatible**, which fixes the sequencing: under
`go1.26.5` `Token.Float()` returns one value, so the repaired file will not
compile there. It has to land in the same commit as the toolchain bump, not
before it.

Note also that the breakage arrives through a *dependency*, not only the
stdlib. `github.com/go-json-experiment/json` gates its own `jsontext` on
`//go:build !goexperiment.jsonv2 || !go1.25` and type-aliases to the standard
library otherwise — so the ten files importing the external module get the new
stdlib API the moment the default flips. Migrating those imports to
`encoding/json/jsontext` changes nothing semantically, but it removes a
dependency whose only remaining job is to be an alias.

**One softer point stands.** `RetiredTags` would fire on a consumer still on Go
1.26, for whom the tag is *required*. The `go` directive bump (§5) forces those
consumers to 1.27 anyway, and boxer's retired-set only reaches a consumer
through its module pin — so a consumer that has not bumped the pin does not see
the finding either. The ordering is self-consistent; the ADR should still say
it.

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

- `public/observability/profiling/pprofarrow` — **no change needed.** The
  predicted hazard was that the profile would carry the goroutine profile's
  `goroutine/count` signature and be misclassified, needing a `WithKindHint`
  like block and mutex do. Measured (M0): it carries its **own** sample type,
  `goroutineleak/count`, so `inferKind`'s single-count-type branch already
  returns `"goroutineleak"` and `Convert` lands on the `pprof_goroutineleak`
  alias unaided. This is why it was worth probing rather than assuming.
- `apps/imzrt/imzrt_panel_profiles.go` — one entry in `profileKinds`
  (`{key: "goroutineleak", label: "Leaked goroutines", capture: captureLookup("goroutineleak"), unit: "goroutines"}`).
  The package comment explains why block and mutex are deliberately absent —
  they are empty unless `SetBlockProfileRate` / `SetMutexProfileFraction` are
  set, and imzrt does not mutate runtime tunables ([ADR-0061](../adr/0061-imzero2-imzrt-go-runtime-dashboard.md) §SD6).
  `goroutineleak` needs no tunable, so it does not hit that rule — but it is
  not free, and the cost is a sharper one than "slow". Measured against a
  205 MB / 400k-object live heap: the capture **forces exactly one GC cycle**
  (the `goroutine` profile forces none), costs 4.5–7.3 ms wall against
  200–300 µs, and adds ~0.1 ms of stop-the-world pause. The latency is
  irrelevant — the existing path already captures through `bgjob`, off the
  render thread. The forced GC is not: it perturbs the very heap and GC series
  the dashboard is plotting, so a capture leaves a step in imzrt's own charts.
  That belongs in an ADR-0061 dated Update, because "observe-only" acquires a
  real caveat: this button changes what the instrument reads.
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

### What the sweep cost — run 2026-08-19

**21 commits, 320 files, +901 / −1206.** Full `-race` suite green in 455 s;
`lint.sh` clean of anything the sweep introduced. Notes a second reader needs:

- **`go fix` emitted code that does not compile, twice**, both from
  `stringscut`. One was cosmetic (`after := after`, a self-assignment of the
  name `strings.Cut` had just bound). The other was not: a test held two
  independent `strings.Index` results, `idIdx` and `u8Idx`, and the fixer
  rewrote **both to a single name** `found`, leaving `if !found || !found`.
  That is a semantic corruption. It happened to be a compile error because the
  two statements were adjacent; further apart it would have compiled and
  silently checked one condition twice.
- **`go build ./...` does not catch that**, because it does not compile test
  files. `go vet ./...` does. Vet is the gate for a sweep like this, not build.
- **One fixer was declined**: `stringsbuilder` on
  `helphost.renderNavSections`, where the loop runs at most four times to build
  a twelve-byte string in a per-frame render path. A `strings.Builder` there is
  slower and reads worse.
- **The sweep is tag-blind.** `go fix -tags "$(cat ./tags)"` never sees files
  behind `integration`, `binary_log`, `bootstrap` and the rest; each family
  needs its own pass. Two integration-test packages additionally fail to
  type-check at HEAD (`chserver` — `recordingBus` missing `RequestWithTimeout`;
  `apps/play` — `len()` on a `*BinarySearchGrowingKV`), so the analyzer skips
  those packages entirely and silently. `gpu_*` was not swept: it needs vendor
  headers.

That settles whether `go fix -diff` should become a `gov gate` step: **no.** A
gate would have to be green, and one site is deliberately not taken — so it
would be permanently red or need a suppression mechanism `go fix` does not
have. More to the point, a fixer that can rewrite two variables into one is not
something to run unattended on a schedule. The right cadence is a reviewed
sweep per Go release, which is what this was.

## 5. The toolchain itself — what actually gates this

Go 1.27 was not installed when this page was first written; M0 fetched
`go1.27.0` per-invocation (`GOTOOLCHAIN=go1.27.0`, which resolves through the
module cache and leaves the machine's `go env` config untouched). The system
toolchain remains `go1.26.5`, built with `GOEXPERIMENT=nodwarf5`; the upstream
1.27 carries no such baseline, so DWARF5 debug info returns — harmless for
correctness, visible in binary size and in whatever consumes debug info.
`GOTOOLCHAIN=local` is set in the machine's `go env`, deliberately, and
[ADR-0095](../adr/0095-airgapped-build-bundle.md) and
[howto/airgapped-build](../howto/airgapped-build.md) both call that
load-bearing.

Consequences of the `go.mod` bump, measured unless marked:

- **The `go` directive bump is not optional and not deferrable.** Building the
  tree with the 1.27 *toolchain* while `go.mod` still says `go 1.26.0` produces
  **310 `stdversion` findings across 31 files** — every `encoding/json/v2` and
  `jsontext` symbol is "requires go1.27 or later (file is go1.26)", because the
  graduated package declares those symbols as new in 1.27. Bumping the
  directive to `go 1.27.0` takes that to **zero**, with no code change. Since
  1.27's `go test` runs `stdversion` by default, the toolchain and the
  directive have to move together or the test lane is red.
- **`go 1.27.0` is also what enables generic methods.** The language version
  comes from the module directive; a per-file `//go:build go1.27` does not
  raise it. So §2 cannot land before the bump, and the bump forces every
  consumer to 1.27 — which is what makes retiring the tag (§1) coherent.
- **`go mod tidy` churn is negligible, not large.** The earlier estimate of a
  big one-time require-block merge was wrong: measured, `go mod tidy --diff`
  under the 1.27 directive is **2 insertions, 5 deletions** — it folds one
  stray single-line `require … // indirect` into the indirect block, and
  **deletes the `toolchain` line** (redundant once it equals the `go`
  directive). Worth knowing that last part, because
  [howto/airgapped-build](../howto/airgapped-build.md) currently explains the
  offline env in terms of "`go.mod` pins `toolchain go1.26.5`" — that sentence
  goes stale.
- **Skew is a hard failure, by design.** With the directive at `1.27.0`, the
  1.26.5 toolchain refuses: `go: go.mod requires go >= 1.27.0 (running
  go 1.26.5; GOTOOLCHAIN=local)`. That is the correct behaviour and the whole
  airgap risk below in one line.
- **CI needs no edit** — all seven workflows use `go-version-file: 'go.mod'`.
- `go vet ./...` under 1.27 in the target state is clean apart from 236
  pre-existing `unreachable code` findings in generated ANTLR `.out.go`
  parsers, which `lint.sh` already filters.
- **`staticcheck` does not survive the bump, and there is nowhere to move to.**
  `honnef.co/go/tools v0.7.0` panics under `go1.27.0` — `unexpected expr:
  *ast.KeyValueExpr` out of its own IR builder — on **every** package: measured
  across all 33 subtrees of `./public`, not one completes. It is not narrowable
  to an exclusion, because what it chokes on is in the standard library every
  package links. The obvious remedy fails too: `v0.8.0-rc.1`, the only newer
  publication, panics on its own bug (`internal error: unhandled builtin
  recover`) — and does so **under `go1.26.5` as well**, so it is not a 1.27
  problem and not a version we could adopt early either. `@master` resolves to
  an older pseudo-version. So the step is parked: `lint.sh` now prints
  `DID NOT RUN` and the one-line panic instead of letting a stack trace land in
  a warn-only step and read like findings. `errcheck`, `nilaway` and
  `govulncheck` (which reports `Go: go1.27.0`) all run fine.
- **capslock drifts, and the reason is worth reading.** The capability gate
  (ADR-0026 §SD10) went red on `apps/writingstylescope :: CAPABILITY_NETWORK`.
  Isolated to the toolchain: identical code passes under 1.26 and fails under
  1.27. The path is two frames — the app's own closure "calling" a closure
  inside `net/http.ParseCookie`. Mechanism: 1.27 rewrote `ParseCookie` from
  `strings.Split` plus a plain loop to `for s := range strings.SplitSeq(line,
  ";")`, which lowers to a `func(string) bool` yield closure; the app's
  `sectionTexts` returns an `iter.Seq[string]`, and capslock's VTA call graph
  resolves *its* `yield` call to every same-signature closure in the program.
  A false positive, and the same one already recorded beside it as
  `net/http.containsDotDot$1` — found four months earlier, in the same app, by
  the same mechanism. It belongs in `knownNoiseSinks`, not in the baseline:
  that file's own doc says an accepted finding no app could honestly act on is
  a defect in the table, not debt in the app. **Expect more of these** as the
  standard library adopts range-over-func inside capability-branded packages.
- **A `go.work` outside the repo gates the bump locally.** The developer
  workspace at the parent directory spans nine sibling repositories and
  declares `go 1.26.1`; a workspace's `go` line must be at least each member's,
  so it refuses the module until it is raised. It is git-ignored here and
  therefore not something this repository can fix — but it is the first thing
  that breaks after the commit, for every one of those nine repositories, and
  it is not mentioned anywhere. `GOWORK=off` is the escape hatch for a single
  command.

### What the lanes said

`scripts/ci/gotest.sh` (`-race -short -cover`) under `go1.27.0`, `go 1.27.0`
directive, target tag set: **248 s, two failing packages, both explained.**

- `public/llm/openaichat` — the `,inline` → `,embed` rename above. Real, fixed,
  re-verified green.
- `public/gov/buildtags` — `TestRequiredAndOptionalMatchTagsFile` failing
  because the probe had dropped `goexperiment.jsonv2` from `./tags` while
  `RequiredTags` still declared it. Not a 1.27 problem: the gate catching a
  half-done migration is the gate working. Restoring either half turns it
  green. Read it as a verification result for M4 — the `./tags` edit and the
  `RequiredTags` edit have to be one commit.

### Airgap bundles — this repository and the adopter

Two repositories build airgap bundles, and they share the core by reference:
[`scripts/dev/airgap-lib.sh`](../../scripts/dev/airgap-lib.sh) here, sourced
directly by the adopter's own bundler rather than copied (ADR-0005). A fix in
the shared lib therefore reaches both at once — but three properties of that
lib turn a toolchain bump into a coordination problem:

1. **The bundle ships the packing operator's toolchain.** `airgap_ship_goroot`
   copies `$(go env GOROOT)` verbatim. Nothing pins or checks a version. So the
   first bundle cut after the bump must be packed on a host whose *default* go
   is 1.27 — running the bundler under a `GOTOOLCHAIN=go1.27.0` override is not
   enough, because the override does not move `GOROOT`.
2. **The generated workspace carries a `go` line taken from the module.** The
   adopter's bundler computes it as `grep -m1 '^go ' <its own go.mod>` and
   passes it to `airgap_go_workspace_vendor`, which writes it into the shipped
   `go.work`. Its bundle ships **both** source trees. So once this repository
   declares `go 1.27.0` while the adopter's module still declares 1.26.x, the
   generated workspace under-declares the version its own contents require, and
   `go work vendor` fails at pack time.
3. **`GOTOOLCHAIN=local` is exported into the bundle env** (both in
   `airgap_set_go_offline_env` and in the generated env file), together with
   `GOPROXY=off`. That is deliberate and should stay — but it means version
   skew on the target is a hard stop with no download escape hatch, exactly the
   `requires go >= 1.27.0` error above.

Ordering that falls out, for the adopter (which consumes this repository by
module pin, with no `replace`, so nothing reaches it until the pin moves):

1. Its own `go.mod` `go` line reaches `1.27.0` **before or with** the pin bump.
2. Its `./tags` file — which today carries `goexperiment.jsonv2` alongside its
   own repo-local tags — drops that entry **only after** it is on 1.27. Dropping
   it earlier breaks its build; keeping it afterwards is inert (§1) until the
   pin bump brings the retired-set finding.
3. The next bundle is cut on a 1.27 packing host, and its dated predecessor
   tarballs stay buildable only under 1.26 — they are snapshots, and that is
   fine, but nobody should expect a mixed-toolchain rebuild from one.

None of this needs new machinery. It needs the bump to be one ordered
operation across two repositories rather than two independent ones, and it
needs the "packed on a 1.27 host" precondition written down where the bundler
can see it.

## Plan

Ordered by dependency. Milestone ids are stable — M2 is struck rather than
renumbered, because M0 measured its work away.

| | Milestone | Gated on | Verification lane |
| --- | --- | --- | --- |
| **M0** | ~~Acquire 1.27; answer the tag, `go tool` and `goroutineleak` questions~~ **done 2026-08-19** — results in §1, §3, §5 | — | ran in a detached worktree at `29733c55` |
| **M1** | ~~`go fix` sweep on `go1.26.5`, one commit per fixer~~ **done 2026-08-19** — 21 commits, 320 files, `fmtappendf` taken before it disappears; see §4 | — | full `-race` suite green, `lint.sh` clean of sweep-introduced findings |
| ~~**M2**~~ | ~~`pprofarrow` kind-hint plumbing~~ — **not needed**: the profile carries its own `goroutineleak/count` sample type and classifies correctly today (§3) | — | — |
| **M3** | ~~One atomic commit: `go 1.27.0`, `go mod tidy`, the `jsontext` repair, the `,inline` → `,embed` tag~~ **done 2026-08-19** as `42f1dc4b` — plus two things the plan did not predict: `net/http.ParseCookie$1` into capslock's `knownNoiseSinks`, and a `DID NOT RUN` branch for the staticcheck step, since the planned pin bump to `v0.8.0-rc.1` turned out to be unusable (§5). The `go-json-experiment/json` dependency was left in place | M0 | full `-race` suite green (386 s), `go vet` clean, `go mod tidy` clean, capslock green |
| **M3b** | Airgap: ~~refresh the `toolchain go1.26.5` sentence and state the "packed on a 1.27 host" precondition in [howto/airgapped-build](../howto/airgapped-build.md)~~ **done 2026-08-19** as `849b9b64`. Still open: coordinate the adopter's `go` line and its bundle re-cut, and raise the local `go.work` (§5) | M3 | a bundle cut + `airgap-unbundle` smoke on a clean host |
| **M4** | Tag retirement: ADR first (Tier 1 — Surfaces / Migration / Verification), then `./tags`, `gov/buildtags` required→empty + retired entry, README, ENGINEERING_PRACTICES, dated Updates on ADR-0053/0078/0004/0083, `wasmsurvey` reason removal; then adopter pin bump + its `./tags` edit | M3 | `gov gate` (its `buildtags` step), `lint.sh`, the `go tool` probe from M0 |
| **M5** | `goroutineleak` surfaces: one `profileKinds` entry, imzrt tour + `bookpprof` rosters, ADR-0061 dated Update for the forced-GC caveat; replace the goroutine-counting slack in `task/spawn_leak_test.go` | M3 | live gate on the Profiles tab (returning rows is not evidence it draws — assert the Arrow header), `gotest.sh` |
| **M6** | Generic methods where they pay: `marshallreflect` terminals as methods on `*SectionReaders` with the free functions kept as forwarders; ADR-0023 design edit (it is `proposed`, so edit in place); `FatRow.Extract`. `recordstore`'s cache constructor only if a generator change is happening anyway | M3 | `gotest.sh`, leeway golden lanes |

M4 and M6 both change `public/` surfaces and both want an ADR. Whether that is
one ADR ("adopt Go 1.27") or two is a judgement call: they share a trigger and
nothing else, and the tag retirement has a downstream migration story the
generic-method change does not.

## Open questions

1. ~~Is the tag inert under 1.27; retired or merely not-required?~~ **Answered
   (M0): inert, and *recognised* rather than unknown — which argues for
   retired.** §1.
2. ~~Does `go tool` delivery unblock?~~ **Answered (M0): yes, measured on a
   consumer-shaped module with no tags.** §1.
3. ~~Does `inferKind` collide `goroutineleak` with `goroutine`?~~ **Answered
   (M0): no — it carries its own sample type.** §3.
4. ~~Does `go fix -diff` become a `gov gate` step?~~ **Answered by running it
   (M1): no.** One site is deliberately declined, so a gate would be
   permanently red; and the fixer that miscompiled two sites is not one to run
   unattended. A reviewed sweep per Go release is the cadence. §4.
5. One ADR or two for M4 and M6?
6. Should M3 also drop `github.com/go-json-experiment/json`? Under 1.27 it is a
   type alias for the standard library, so the import is a dependency whose
   only job is indirection — but it is ten files of churn for no behaviour
   change, which may be better folded into M1's sweep or left alone.
7. Does anything want `GOEXPERIMENT=simd`? Out of scope here — named only so
   the next reader does not assume it was missed.
