---
type: reference
audience: contributor
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-05-21
---

# Go Coding Standard
LLM statement: Refined and reviewed using Gemini3 Pro.

## Motivation
This is a very specific, opinionated standard deviating from idiomatic go in some ways.
It is tailored towards the enablement of small teams of ambitious data scientists and data engineers developing high-throughput system.
In these roles go is not the only language to master and is important to lower the cognitive load by embracing universally applicable conventions.

## Go Version
Target the most recent stable go version available.

## Packages to Use
* Use `lukechampine.com/blake3` as cryptographic hash function.
* Use `encoding/json/v2` as json package.
* Use `github.com/RoaringBitmap/roaring` for serializable uint32 and uint64 compressed bitsets.
* Use `github.com/matoous/go-nanoid/v2` in place of uuids.
* Use `github.com/rs/zerolog` for structured logging.
* Use `github.com/stretchr/testify` for unittest assertions.
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
*   **Heavy test-only dependencies must be build-tag-gated.** If a test
    introduces a non-trivial dependency chain — containers (testcontainers-go),
    browser drivers, network harnesses, large fixture or model loaders — the
    test file must carry a custom build tag (e.g. `//go:build integration`)
    and the default `go test ./...` run must not pull those modules in. The
    file [tags](./tags) lists which tags CI activates by default;
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
