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
* Use `github.com/stergiotis/boxer/public/observability/eh` for error construction and wrapping.
* Use `github.com/stergiotis/boxer/public/observability/eh/eb` for structural error construction and wrapping.
* Use `github.com/stergiotis/boxer/semistructured/leeway/canonicaltypes` for defining RPC or FFI interface descriptions.
* Use `github.com/stergiotis/boxer/semistructured/leeway/naming` for conversions between naming schemes (e.g. snake_case to camelCase).
* Use `github.com/urfave/cli/v3` for cli commands and flags handling.

## Error Handling & Flow Control

### Return Signature
Named return values are **mandatory** for all functions and methods.
```go
func (inst *Type) DoWork() (n int, err error) { ... }
```

### Error Checking
*   **Naked Returns:** Use naked returns (`return`) immediately after overwriting the `err` variable.
*   **No Short Declaration:** Do not use the `if err := func(); err != nil` syntax. This prevents variable shadowing and keeps the logic linear.

Correct Pattern:
```go
err = myFunc()
if err != nil {
    // Wrap with %w only. Do not use other format specifiers.
    err = eh.Errorf("unable to capture context: %w", err)
    return
}
```

### Error Construction
*   **Simple Wrapping:** Use `eh.Errorf` with `%w` only.
*   **Structural Context:** Use the `eb` builder pattern for adding metadata (similar to `zerolog`).

```go
if err != nil {
      err = eb.Build().
            Str("path", path).
            Int("len", length).
            Errorf("unable to capture context: %w", err)
      return
}
```

## Control Flow
*   **Conditionals:** Use `if val { ... } else { ... }` for binary conditions.
*   **Guard Clauses:** Prefer early exits/returns to reduce nesting depth.

## Scoping
To mitigate scope pollution (caused by banning `if err := ...`), use explicit anonymous blocks with comments to segment logic within large functions.
These blocks are candidates to be extracted in external functions.

```go
func (inst *Worker) Process() (err error) {
    // ... setup code ...

    { // Stage: Release
        var x int
        x, err = inst.calculateRelease()
        if err != nil {
            return
        }
        inst.use(x)
    } // 'x' is now out of scope

    return
}
```

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

### Receiver Name
Always use `inst` (instance) as the receiver name.
```go
func (inst *Encoder) Method() { ... }
```

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
Follow standard Go naming conventions for iterator methods:
*   `All()`: Iterates over all items.
*   `Values()`: Iterates over values (if distinct from `All`).
*   `Keys()`: Iterates over keys/indices.
*   `Backward()`: Iterates in reverse order.

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
