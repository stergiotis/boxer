---
type: reference
audience: contributor
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-12
---

# Go Coding Standard
LLM statement: Refined and reviewed using Gemini3 Pro.

## Motivation
This is a very specific, opinionated standard deviating from idiomatic go in some ways.
It is tailored towards the enablement of small teams of ambitious data scientists and data engineers developing high-throughput system.
In these roles go is not the only language to master and is important to lower the cognitive load by embracing universally applicable conventions.
The team works trunk-based, committing directly to `main`; see [Version Control](#version-control).

## Version Control

Development is **trunk-based**: `main` is the single long-lived branch and work is committed directly to it. There are no pull-request gates, no `CODEOWNERS`, and no branch-protection automation (see [§10 of ENGINEERING_PRACTICES.md](./doc/ENGINEERING_PRACTICES.md#10-notably-absent)). Short-lived local branches are fine for work in progress, but are expected to land on `main` quickly rather than accumulate divergence; long-lived feature branches are not part of the workflow.

This carries a few obligations:

*   **Keep `main` buildable.** CI runs on every push (see [§1 of ENGINEERING_PRACTICES.md](./doc/ENGINEERING_PRACTICES.md#1-ci-surface)), so each commit should compile and pass the default test and lint gates under the active build tags. A change that spans several files lands as one self-contained commit rather than a sequence that leaves intermediate `HEAD`s broken.
*   **State lives in front-matter, not branches.** Draft and stable documents — and `proposed` vs. `accepted` ADRs — coexist on `main`, distinguished by their front-matter `status` (and a draft banner), never by branch. See [doc/DOCUMENTATION_STANDARD.md](./doc/DOCUMENTATION_STANDARD.md).
*   **Small, focused commits.** Review is continuous rather than gated at a merge boundary, so each commit stays scoped to one concern and carries its rationale in the message.

The "no PR branches" assumption is shared with the [documentation standard](./doc/DOCUMENTATION_STANDARD.md) and suits a small, AI-assisted team where review happens as work lands rather than at a merge gate.

## Design Before Code

For a **new package or a significant architectural change**, the first deliverable is a design, not an implementation. Sketch the package layout, the key interfaces, and the trade-offs; iterate over a few short rounds; and freeze the result as an ADR under `doc/adr/` before writing the code it describes.

This holds even when the request reads as "add a package that does X" or "integrate Y" — the response is a design to review, not a finished package. Trunk-based development has no merge gate to catch a wrong direction late (see [Version Control](#version-control)), so the alignment happens up front and is recorded where the next reader will look.

*   **Iterate, then freeze.** Present concrete options with their trade-offs and let the decision settle before implementing. Capture it with the ADR template at [`doc/templates/adr/0000-template.md`](./doc/templates/adr/0000-template.md); use a QOC matrix when there are ≥3 viable options against ≥3 criteria (see [§1 of DOCUMENTATION_STANDARD.md](./doc/DOCUMENTATION_STANDARD.md#1-artifact-types--where-they-belong)).
*   **Implement against the accepted decision.** Start coding once the ADR is `accepted`; the ADR is then the contract the code follows. A later change of direction is a new or superseding ADR, not a silent divergence in the implementation.
*   **Scope.** This covers new packages, new subsystems, and changes that cross package boundaries or revise a cross-cutting contract. Bug fixes, local refactors, and additions that fit an existing package's established shape do not need an ADR first.

## Go Version
Target the most recent stable go version available.

## Packages to Use
* Use `lukechampine.com/blake3` as cryptographic hash function.
* Use `encoding/json/v2` as json package.
* Use `github.com/RoaringBitmap/roaring` for serializable uint32 and uint64 compressed bitsets.
* Use `github.com/matoous/go-nanoid/v2` in place of uuids.
* Use `github.com/rs/zerolog` for structured logging.
* Use `github.com/stretchr/testify` for unittest assertions.
* Use `pgregory.net/rapid` for property-based testing.
* Use `github.com/zeebo/xxh3` as 64-bit non-cryptographic hash function.
* Use `github.com/stergiotis/boxer/public/config/env` for environment-variable access (see [Configuration](#configuration)).
* Use `github.com/stergiotis/boxer/public/observability/eh` for error construction and wrapping.
* Use `github.com/stergiotis/boxer/public/observability/eh/eb` for structural error construction and wrapping.
* Use `github.com/stergiotis/boxer/semistructured/leeway/canonicaltypes` for defining RPC or FFI interface descriptions.
* Use `github.com/stergiotis/boxer/semistructured/leeway/naming` for conversions between naming schemes (e.g. snake_case to camelCase).
* Use `github.com/urfave/cli/v2` for cli commands and flags handling (see [Entry Points](#entry-points)).
* Use `github.com/dim13/colormap` for scientific color maps (Magma, Inferno, Plasma, Vidiris, Parula).

## Error Handling

### Error Construction
*   **Simple Wrapping:** Use `eh.Errorf` with `%w` only.
*   **Structural Context:** Use the `eb` builder pattern for adding metadata (similar to `zerolog`).

```go
if err != nil {
      err = eb.Build().
            Str("path", path).
            Int("len", length).
            Errorf("unable to capture context: %w", err)
      return err
}
```

### Message Text
*   **No package or function-name prefixes.** `eh.Errorf` and `eb.Build()` capture a stack trace at the call site (via `runtime.Callers` in `eh`), so the originating package and function are already attached to the error. Do **not** prefix the message with the package/function name (e.g. `"mypkg: ..."` or `"LoadConfig: ..."`) — it duplicates what the stack records and drifts out of sync when code is moved or renamed.
*   Write the bare semantic message; let `Str`/`Int`/… carry structured context and the stack carry location.

```go
// Good — the stack already records package + function.
err = eb.Build().Str("path", path).Errorf("open config: %w", err)

// Avoid — "config:" / "LoadConfig:" repeat what the stack already has.
err = eb.Build().Str("path", path).Errorf("config: open config: %w", err)
```

## Control Flow
*   **Conditionals:** Use `if val { ... } else { ... }` for binary conditions.
*   **Guard Clauses:** Prefer early exits/returns to reduce nesting depth.

## Memory Management

### Allocations & Layout
*   **Data-Oriented Design:** Prefer `struct-of-arrays` (SoA) over `array-of-structs` (AoS) to improve cache locality.
*   **Avoid Small Objects:** Minimize pointer chasing; embed structs where possible.
*   **Pre-allocation:** Always pre-allocate slices and maps with `make(..., capacity)` using best-effort guesses.

### Reuse
*   **Resetting:** Reuse slices by setting length to 0 (reslicing) rather than re-allocating. Make use of `clear` where appropriate.
*   **Growing:** Use `slices.Grow()` before appending if the exact size isn't known at creation but becomes known later.
*   **Scratch Buffers:** Use pre-allocated scratch buffers as struct fields to avoid stack-to-heap escapes on small writes.
*   **Sync.Pool:** Avoid `sync.Pool` unless memory profiling proves it is necessary (prefer struct-field buffers).

*Note: These rules may be relaxed in unit test code.*

## Typing

### Integers
*   **Fields:** Use sized integers (`int32`, `uint64`, etc.).
*   **Sizes:** Use `int64` for file sizes or counts that might exceed 2 billion.
*   **Indexes:** Use `int` for slice indexing.

### Nominal Typing
*   **Strict Types:** Introduce strictly typed scalars.
    ```go
    type MyEnumE uint8
    ```
*   **No Aliases:** Do not use type aliases (`type X = Y`).
*   **Interface Compliance:** Add compile-time checks for non-standard interfaces.
    ```go
    var _ InterfaceName = (*Type)(nil)
    ```
## Naming & Style

### Interface Naming
Interface names must end with a capital `I`.
```go
type ReaderI interface { ... }
```

### Enum Naming
Enum types must end with a capital `E`. Values must be prefixed with the Type name (minus the E).
```go
type WeekdayE uint8
const (
    WeekdayMonday    WeekdayE = 1<<0
    WeekdayTuesday   WeekdayE = 1<<1
    WeekdayWednesday WeekdayE = 1<<2
    ...
)
var AllWeekdays = []WeekdayE{WeekdayMonday,WeekdayTuesday,WeekdayWednesday,...}
```

When the full type-name prefix is awkwardly long (e.g. `StaticPolySubtype*` from `StaticPolySubtypeE`), a per-enum override may be declared once on the type:
```go
//codelint:enum-prefix=Subtype
type StaticPolySubtypeE uint8
const (
    SubtypeNone  StaticPolySubtypeE = iota
    SubtypeBasic
    ...
)
```
The override only affects the value-prefix rule; the type itself must still end with `E`.

### Function & Method Naming

**Suffixes.**
*   `E` — functions returning an error (e.g. `OpenE`). E = Error. Distinct from the enum type-suffix `E` above; types and functions are disambiguated by Go's identifier conventions.

**Prefixes.**
*   `Set` — only idempotent setters may use this prefix.
*   `Get` — getters.
*   `Is` — predicates (functions or methods returning a single `bool`).

### Opposite Pairs

Use these canonical verb pairs rather than invented synonyms, so that symmetrical operations are immediately legible:

*   Begin/End
*   Prepare/Apply
*   Start/Stop
*   Incl/Excl, Inclusive/Exclusive
*   Add/Remove
*   Create/Delete/Prune, Create/Destroy
*   Commit/Rollback
*   Src/Dest, Source/Destination
*   First/Last
*   Incr/Decr, Increment/Decrement
*   Lock/Unlock
*   Next/Prev
*   Old/New
*   Open/Close
*   Set/Get, Set/Clear, Set/Unset
*   Show/Hide
*   Up/Down
*   Attach/Detach
*   Compress/Decompress
*   Connect/Disconnect
*   Enable/Disable
*   Encode/Decode
*   Serialize/Deserialize
*   Marshal/Unmarshal
*   Inflate/Deflate
*   Enter/Leave
*   Freeze/Unfreeze
*   Head/Tail
*   Increase/Decrease
*   Input/Output
*   Ingress/Egress
*   Prolog/Epilog
*   Inbound/Outbound
*   Link/Unlink
*   Push/Pop, Push/Pull
*   Read/Write
*   Register/Deregister
*   Resume/Suspend
*   Select/Deselect
*   Send/Receive
*   Setup/Teardown

## Testing

*   **Assertions:** use `github.com/stretchr/testify` (see [Packages to Use](#packages-to-use)).
*   **Property-based testing:** use `pgregory.net/rapid` (see [Packages to Use](#packages-to-use)) for invariant and round-trip properties, and its `rapid.StateMachine` / `t.Repeat` support for stateful command sequences. Go's native fuzzing (`testing.F`) stays appropriate for byte- and string-shaped inputs and composes with rapid via `rapid.MakeFuzz`.
*   **Heavy test-only dependencies must be build-tag-gated.** If a test
    introduces a non-trivial dependency chain — containers (testcontainers-go),
    browser drivers, network harnesses, large fixture or model loaders — the
    test file must carry a custom build tag (e.g. `//go:build integration`)
    and the default `go test ./...` run must not pull those modules in. The
    [`./tags`](./tags) file lists which tags CI activates by default;
    `integration` is opt-in and only set by integration CI jobs. Rationale:
    keeps `go.sum`, the module cache, and developer build times bounded by
    what production code actually needs. See
    [§4 of ENGINEERING_PRACTICES.md](./doc/ENGINEERING_PRACTICES.md#4-tests)
    for the kafka/Redpanda case study.
*   **Test helpers shared across packages** live in regular (non-`_test.go`)
    files because Go does not export `_test.go` symbols across package
    boundaries. Name such files `*_testutils.go` or place them under a
    `…/testcmd/` subpackage so their intent is obvious despite the file
    extension. Be aware that they count as production code for `go list`
    purposes and pull their imports into the production dependency graph.

## Adversarial Code Review

Review-critical code is reviewed **adversarially** — the reviewer's job is to refute the change, not to bless it — and the review is recorded where the next reader, human or agent, will look. Trunk-based development has no merge gate (see [Version Control](#version-control)), so the recorded review, not a pull request, is what attests that a subsystem was examined and marks when that examination has gone stale. The decision and its rejected alternatives are [ADR-0131](./doc/adr/0131-systematic-adversarial-code-review.md); the marker mechanics extend `packageprops` ([ADR-0080](./doc/adr/0080-packageprops-per-package-declarations.md)).

*   **Scope is opt-in.** A package acquires a review obligation once it declares a `packageprops.Review` marker or is designated review-critical; the zero value asserts nothing, exactly as `Kind` does. Start with the subsystems where a latent defect is expensive — leeway's pipeline stages, the nanopass passes, the FFI boundary, identity and marshalling — not every leaf package.
*   **Review adversarially, scaled to blast radius.** Run `/code-review` at a tier matched to the change (low/medium for leaf code, high/max for the review-critical core), or `ultra` for the highest-blast-radius subsystems. Every finding must carry a concrete failure scenario (inputs → wrong output); a finding without a repro is not yet actionable. Verify a finding — attempt to refute it — before acting on it.
*   **Record the review in the package's marker.** Set `packageprops.Review` in `package_props.go`: the gofmt-normalized source digest the review covered, the reviewer (`code-review@<tier>` or `ultra`), the commit, the date, and a reference to the findings sidecar. The marker is the summary; the findings themselves — repros, dispositions — live in the sidecar, keeping `Props` a clean vocabulary ([ADR-0080](./doc/adr/0080-packageprops-per-package-declarations.md) §SD5).
*   **Re-review is triggered by drift.** A review-aware `props verify` recomputes each marked package's normalized digest and flags the packages that have changed since their recorded review — that mismatch is the re-review signal. It is advisory while its false-positive rate is being bounded, and graduates to a CI gate per the calibration lifecycle in ADR-0131. Comment- and whitespace-only churn does not trigger it (the digest is normalized).
*   **Every finding gets a disposition.** Close each finding as fixed, accepted-risk (with the reason), or wontfix (with the reason); an open finding is tracked debt, not a silent omission. A re-review confirms the fix landed — the marker's digest advances only when the reviewed source does.

## Documentation
*   **Self-documenting code:** Do not document obvious methods or fields.
*   **No Tautologies:** Do not repeat literal values in comments.
*   **Numeric Literals:** Use the most readable syntax (Hex, bitshifts, `_` separators).

```go
var OneMillion = 1_000_000
var MagicHeader = 0xdeadbeef
var OnePiB = 1*1024*1024*1024*1024*1024
var Mask = (uint32(1)<<4)-1
```
*   **Invariants:** Explicitly document assumptions about the runtime environment (e.g., "Assumes Little Endian").

## ADR References

When code implements, enforces, or is shaped by an Architecture Decision Record
([doc/adr](./doc/adr/)), it **must** cite that ADR by its `ADR-NNNN` marker in a doc comment on the package, type, or
function the decision governs. Where the decision is decomposed — phases, cuts,
milestones, sub-decisions — pin the specific part with the `§` qualifier the ADR
uses (`§SD3`, `§M2`, `§4`), so the reference states *which* part of the decision
the code realises, not merely that the ADR exists.

```go
// Package capslock is the CLI wiring for the ADR-0026 §SD10 capslock
// cap-vs-manifest cross-checker.
```

The bare `ADR-NNNN` form (zero-padded, four digits) is canonical; a prose link to
`doc/adr/NNNN-…` also counts. Cite wherever a reader needs the decision to follow
the code — typically the package doc comment and the central types/functions —
not on every line that touches the area.

Rationale: these markers are the evidence [`boxer adr`](./doc/howto/adr-overview.md)
reads to gauge each ADR's *implementation degree* — how many files cite it, across
how many packages, pinned to which sections — and crosses it against the
front-matter `status`. A decision built without a marker reads as un-built; a
marker without its `§` qualifier loses per-section fidelity. The citation is what
makes the decision↔code mapping queryable instead of tribal knowledge.

## Portability
Microsoft Windows is not a target runtime.
Nevertheless, use stdlib functions aiming at writing portable code where it helps to capture intent and semantics. For example: Use `filepath` to manipulate paths.

## Entry Points

Do not add ad-hoc `main()` functions for new utilities, linters, or compile-time code generators. Register them as subcommands under an existing entry point — in boxer this is `./public/app/main.go`, invoked via `./boxer.sh` — so that build tags, flags, the environment-variable registry, and observability wiring are shared.

`github.com/urfave/cli/v2` is mandatory for every CLI surface, including small internal tools: utilities, linters, compile-time code generators. Even one-off commands expose their flags as `cli.Command` definitions; this keeps `--help` output, flag parsing, and `Spec.AsCliFlag()` integration uniform.

## Configuration

### Environment Variables
Use of the `public/config/env` registry is **mandatory** for every environment variable consumed by the codebase. Direct access via `os.Getenv`, `os.LookupEnv`, `os.Environ`, or `syscall.Getenv` is prohibited (a lint test enforces this).

Each variable is declared once as a package-level value, which registers a `Spec` process-globally and returns a typed handle:

```go
var LogLevel = env.NewString(env.Spec{
    Name:     "BOXER_LOG_LEVEL",
    Category: env.CategoryObservability,
    Default:  "info",
    Usage:    "zerolog level (trace|debug|info|warn|error)",
})
```

Read the value through the handle (`LogLevel.Get(ctx)` / `LogLevel.Lookup(ctx)`); derive CLI flags with `Spec.AsCliFlag()`; override in tests with `SetForTest`. Rationale: a single registry yields discoverability, typed parsing, doc generation (`boxer env gen-docs`), and prevents the lowercase-name and typo defects that motivated ADR-0009.

## Concurrency Patterns
*  **Context:** usage is mandatory for all I/O bound or long-running functions. `ctx` must be the first argument.
*  **Mutexes:** `sync.Mutex` must be a value type in the struct (not a pointer to a mutex) and zero-valued usage must be valid.
*  **Atomics:** Prefer `sync/atomic` types (e.g., `atomic.Int64`) over `atomic.LoadInt64(&val)` for type safety (Go 1.19+).

## Zero Values
Structs should be usable with their zero value (`var x MyStruct`) whenever possible.
If initialization is complex, a `New()` constructor is mandatory and the struct fields should be unexported to prevent invalid states.

## Iteration
Use the `iter` package to expose collections of data. This is preferred over returning slices (which forces allocation) or exposing internal slice fields (which breaks encapsulation).

This is particularly mandatory when traversing **Struct-of-Arrays (SoA)** storage to assemble "views" of the data on the fly.

### Naming
When a type exposes a single iterator, use one of the canonical method names:
*   `All()`: Iterates over all items.
*   `Values()`: Iterates over values (if distinct from `All`).
*   `Keys()`: Iterates over keys/indices.
*   `Backward()`: Iterates in reverse order.

Types that legitimately expose multiple distinct iterators (e.g. a graph store with `LiveChildren`, `ForwardEdges`, `BackwardEdges`, `DeletedPartitionMembers`) should use domain-describing method names instead — the canonical quartet is for single-collection-per-receiver cases.

### Error Handling in Iterators
If an iteration can fail (e.g., I/O during traversal), use `iter.Seq2[V, error]`.
```go
for item, err := range inst.StreamItems() {
    if err != nil {
        // Handle error
        return
    }
    // Process item
}
```
### Implementation
Implement iterators as methods returning the sequence function. Ensure the `yield` function is called correctly to support early breaks (break in the caller's loop returns false to yield).

### Example
```go
type UserStore struct {
    ids   []uint64
    names []string
    ages  []uint8
}

// All allows iterating the SoA storage as a unified view.
func (inst *UserStore) All() iter.Seq2[uint64, UserView] {
    return func(yield func(uint64, UserView) bool) {
        for i, id := range inst.ids {
            // Construct the view on the stack
            v := UserView{
                Name: inst.names[i],
                Age:  inst.ages[i],
            }
            if !yield(id, v) {
                return
            }
        }
    }
}
```
