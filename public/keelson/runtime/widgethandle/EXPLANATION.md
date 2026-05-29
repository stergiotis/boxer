---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# ImZero2 WidgetHandle

This file explains *why* the WidgetHandle type exists and the properties
that make it safe to pass across frames and callers. The earlier adoption
of the opaque-handle mechanism is a design decision that should, when
time permits, be captured as a dedicated ADR; until then this file records
both the motivation and the mechanics.

## Background

### Context

ImZero2 widgets are identified by a `uint64` ID derived hierarchically from a
shared `WidgetIdStack`. IDs are used by the `StateManager` to look up response
flags (clicks, hovers, drags) and data-bindings (checkbox values, slider
positions) for each widget on each frame.

Originally, the ID was an opaque implementation detail: the caller prepared an
ID on the stack (`ids.PrepareStr("inc")`), passed the stack to a factory
(`Button(ids, atoms)`), and the factory internally called `Derive()` /
`DeriveStacked()` to consume the prepared state. The raw `uint64` never left
the factory.

### Problems with the original design

1. **Interface conflation.** `WidgetIdCreatorI` (implemented by both
   `WidgetIdStack` and `AbsoluteWidgetId`) conflated ID generation with
   hierarchical scope management. `AbsoluteWidgetId` satisfied the interface
   only by making its `PopIdFromStack*` methods no-ops, silently breaking the
   push/pop contract when passed to container widgets.

2. **Implicit push/pop obligations.** `Derive` vs `DeriveStacked` placed an
   implicit obligation on the caller (or the widget factory) to pair every
   push with a pop. The distinction was invisible at the call site.

3. **Callers could not hold IDs.** The Prepare→Derive state machine consumed
   the ID inside the factory. Callers who needed an ID for their own purposes
   (e.g., to query a sense region's response later, or to index domain objects
   by widget identity) had no clean way to obtain it. They resorted to:
   - Computing raw `uint64` IDs manually and using `GetResponseByIdRaw(id)`.
   - Copying test helpers like `DeriveWidgetId` into production code.
   - Peeking into Fluid struct private fields.

   This last point is the practical failure mode visible in
   `demo/widgets/egui2_hl_treemap*_demo.go` and
   `demo/widgets/egui2_hl_sense_region_test_demo.go`: these files manually
   compute and track raw `uint64` sense region IDs because the API gave no
   first-class way to do so.

4. **Persisted IDs silently corrupt state.** Raw `uint64` IDs are only valid
   within a single program run — they depend on the hash context and the stack
   state during derivation. If a raw ID is persisted to disk and reloaded
   after restart, a lookup will either miss or, worse, silently hit an
   unrelated widget's slot.

## How it works

### The handle

A `WidgetHandle` is an opaque, runtime-scoped `uint64` derived from a raw
widget ID by XOR-ing with a secret generated once at program startup:

```go
type WidgetHandle uint64

var secret = rand.Uint64() // new every program run

func Make(id uint64) WidgetHandle      { return WidgetHandle(id ^ secret) }
func (h WidgetHandle) Resolve() uint64 { return uint64(h) ^ secret }
```

Properties:

- **Round-trip within a program run:** `Make(id).Resolve() == id`.
- **Runtime-scoped:** a handle persisted in run A and reloaded in run B
  decodes to a different raw ID (garbage). A `StateManager` lookup with a
  stale handle silently misses rather than hitting an unrelated widget.
- **Type-safe:** `WidgetHandle` is a distinct type from `uint64`. You cannot
  accidentally pass a raw ID where a handle is expected, or vice versa.

### Package placement

The `WidgetHandle` type and its `Make` / `Resolve` functions live in
`keelson/runtime/widgethandle`. The package was originally `thestack/internal/widgethandle`
and was promoted to a public location under `keelson/runtime/` by [ADR-0035](../../../../../../doc/adr/0035-keelson-namespace-introduction.md)
when `windowhost` (a runtime consumer outside `thestack/`) needed to depend on it.
Opacity is now enforced by keeping the `secret` field unexported rather than by Go's
`internal` mechanism — `Resolve()` is unexported and so cannot be called from outside
this package; callers still receive opaque handles.

Consumers across the tree:

- `keelson/runtime/windowhost` — to make per-window handles.
- `thestack/fffi2/typed` — to expose `GetWidgetHandle()` on retained holders.
- `thestack/imzero2/egui2/bindings` — to accept handles in `StateManager` public
  methods and resolve them to raw IDs internally.

### Where handles come from

The retained holder (`RetainedFffiHolderTyped[T]`) already contains the raw
widget ID as part of its serialized byte content — it's the `uint64` that was
written to the FFFI buffer when the widget was created. The builder records
the byte offset of this write via `MarkWidgetIdOffset()` (invoked by the
convenience `WriteWidgetId(id uint64)` method), and `BuildRetained()`
propagates the offset into the holder.

```go
func (inst RetainedFffiHolderTyped[T]) GetWidgetHandle() widgethandle.WidgetHandle {
    off := inst.widgetIdOffset
    if int(off)+8 > len(inst.content) {
        return widgethandle.NoWidget
    }
    id := binary.LittleEndian.Uint64(inst.content[off : off+8])
    return widgethandle.Make(id)
}
```

Retained holders are the correct lifetime boundary for handles: callers
already hold them across frames for caching purposes, and the raw ID is
embedded in their bytes by construction. Fluid structs, by contrast, are
transient builder objects that should not be stored.

For callers who don't keep a retained holder (e.g., code that emits
`PaintSenseRegion` opcodes with pre-computed IDs for a batch of cells),
`widgethandle.Make(rawId)` converts a raw `uint64` to a handle directly.

### StateManager API

The `StateManager`'s public API accepts `WidgetHandle`:

```go
func (inst *StateManager) GetResponse(h widgethandle.WidgetHandle) ResponseFlagsE
func (inst *StateManager) OverrideDatabinding(h widgethandle.WidgetHandle)
```

Internally, `components` package code (Fluid struct `Response()` methods,
generated `KeepIter()` helpers) uses unexported `getResponseByIdRaw(id uint64)`
and `overrideDatabindingRaw(id uint64)` variants that skip the XOR
obfuscation. These are implementation details and not part of the public
surface.

The previous `GetResponseById(WidgetIdCreatorI)` and
`OverrideDatabindingWidget(WidgetIdCreatorI)` methods are removed — they had
no external callers and their `Derive()` side effect on the ID stack was a
footgun.

## Invariants

- `Make(id).Resolve() == id` within a single program run.
- The `secret` is generated exactly once at program start and never exposed
  outside the `widgethandle` package.
- Persisted handles do *not* round-trip across runs — a stale handle lookup
  silently misses rather than aliasing an unrelated widget.
- `WidgetHandle` and `uint64` are distinct types at compile time.

## Trade-offs

- The XOR masking is a safety mechanism against accidental persistence, not
  a security mechanism — an attacker with access to the process memory can
  read the secret.
- Generated `components` code retains `*Raw(id uint64)` variants to avoid
  round-tripping through `Make` / `Resolve` in hot loops; these must stay
  unexported to preserve the handle abstraction at the public surface.

## Known follow-ups (not addressed here)

- **`WidgetIdCreatorI` interface split.** The interface still conflates ID
  generation with scope management. `AbsoluteWidgetId` still implements it
  with no-op stack operations. Container widgets still take a
  `WidgetIdCreatorI` parameter rather than distinguishing leaf/container
  signatures.
- **Prepare→Derive state machine.** The two-step protocol on `WidgetIdStack`
  still exists. Callers who want both to create a widget and query its
  response from the stack still pay a Prepare cycle — though in practice,
  they now hold the handle via `GetWidgetHandle()` or `widgethandle.Make`
  and avoid the re-prepare entirely.

## Migration summary (historical)

The adoption of `WidgetHandle` replaced the following usages:

| Before                                         | After                                                       |
|------------------------------------------------|-------------------------------------------------------------|
| `sm.GetResponseByIdRaw(rawId)` (external call) | `sm.GetResponse(handle)`                                    |
| `sm.GetResponseById(idCreator)`                | removed — no external callers                               |
| `sm.OverrideDatabindingWidget(idCreator)`      | `sm.OverrideDatabinding(handle)`                            |
| `cellDesc.senseId uint64`                      | `cellDesc.senseHandle widgethandle.WidgetHandle`            |
| `widgettest.AddR7(rawId, flags)`               | `AddR7(handle, flags)` + `AddR7Raw(rawId, flags)`           |
| Factory: `r.WriteUint64(checkId(v))`           | `r.WriteWidgetId(checkId(v))`                               |

The `WriteWidgetId` change on the builder is purely additive — it records
the widget ID offset so `GetWidgetHandle()` can find the bytes later. It
does not change the wire format or the semantics of the ID itself.

The generator emitting this code lives in
`src/go/public/thestack/fffi2/compiletime/goserver/fffi2_compiletime_go_server.go`
(`generateIdentityHandling`). Regenerate `factories.out.go` via
`egui2gen generate go --goOutputBasePath <dir>`.
