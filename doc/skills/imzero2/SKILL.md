---
name: imzero2
description: "Use when writing imzero2 / egui2 GUI code — the FFFI render pipeline, memory management, the deterministic XOR id stack, retained/deferred bodies, and widget contracts."
type: reference
audience: contributor
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-04-24
---

This document provides a technical specification and developer guide for the **FFFI-based Immediate Mode GUI (IMZERO2)** library.
It is designed for Go developers and as a context-injection for LLM agents.

---

# 1. Core Architecture: The FFFI Pattern

The library operates on a **Framed Foreign Function Interface (FFFI)**. Unlike standard FFIs that call across language boundaries synchronously, FFFI is optimized for high-frequency, batch-driven UI updates.

*   **Server-Client Split**: The "Server" (Go) contains the business logic and UI description. The "Client" (Presentation Tier) is an interpreter loop that executes rendering and handles low-level input.
*   **Framed Execution**: Commands are not sent individually. Instead, the Go side builds a command buffer for the entire frame. Calling `FinishServersideFrame()` flushes this buffer to the client.
*   **State Syncing**: Interaction results (clicks, focus, text input) are collected by the Client and synced back to the Server at the start of the next frame (`StartServersideFrame()`).
*   **Interpreter Loop**: The client-side consumes a linear stream of FFFI instructions, mapping them to draw calls or layout updates.

---

# 2. Memory Management

The FFFI architecture avoids the overhead of shared pointers or CGo garbage collection issues through specific strategies:

*   **Region-Based Mapping**: The transport layer typically uses memory-mapped regions where the Go side writes arguments and the Client reads them linearly.
*   **Retained Holders (`.Keep()`)**:
    *   Fluid builders (e.g., `AtomsFluid`) are transient.
    *   Calling `.Keep()` serializes the current configuration into a `RetainedFffiHolderTyped[T]`.
    *   These holders are "Value Objects"—they contain the data needed for a command and can be reused across frames to reduce Go-side allocations.
*   **No Pointer Sharing**: All data passed to the client is copied or serialized. The client does not access Go memory directly.

---

# 3. Deterministic ID Management (The XOR Stack)

The library uses a **64-bit XOR-stack** to maintain widget identity. Identity is required for the client to know if "Button A" in Frame 1 is the same as "Button A" in Frame 2.

### The Formula
$$ID_{effective} = ID_{stack} \oplus ID_{local}$$
Where $\oplus$ is the XOR operator. This allows for efficient namespacing: if you move a group of widgets into a new sub-scope, their relative identities remain stable while their effective IDs change.

### ID Categories
| Type                              | Behavior | Best Use Case |
|:----------------------------------| :--- | :--- |
| **Relative (`*WidgetIdStack`)**  | XORed with the current stack value. | Standard widgets, list items, nested components. |
| **Absolute (`AbsoluteWidgetId`)** | Replaces the stack value; ignores parents. | Top-level Windows, Modals, Global Overlays. |
Both types do satisfy the `components.WidgetIdCreatorI` interface. The WidgetIdStack must be created by the application:
```go
ids := components.NewWidgetIdStack()
```
Use of string (absolute) id:
```go
components.Button(c.MakeAbsoluteIdStr("my id"), c.Atoms().Text("button").Keey()).Send()
```
Use of numerical high-entropy (e.g. hash) (absolute) id:
```go
components.Button(c.MakeAbsoluteIdHighEntropy(0xcaffebabe), c.Atoms().Text("button").Keey()).Send()
```
Use of numerical low-entropy (e.g. counter) (absolute) id:
```go
components.Button(c.MakeAbsoluteIdSeq(i), c.Atoms().Text("button").Keey()).Send()
```
Use of string (relative) id:
```go
components.Button(ids.PrepareStr("my id"), c.Atoms().Text("button").Keey()).Send()
```
Use of numerical high-entropy (e.g. hash) (relative) id:
```go
components.Button(ids.PrepareHighEntropy(0xcaffebabe), c.Atoms().Text("button").Keey()).Send()
```
Use of numerical low-entropy (e.g. counter) (relative) id:
```go
components.Button(ids.PrepareSeq(i), c.Atoms().Text("button").Keey()).Send()
```
### Explicit ID Scoping
When the framework cannot derive an ID (e.g., in a loop with identical labels), the user must manage the stack using **`components.IdScope(id)`**:
```go
for range components.IdScope(ids.PrepareStr("myscope")) {
    ...
}
```

---

# 4. The Fluid API & Terminal Methods

The API uses a **Fluid Builder** pattern. Every widget factory returns a "Fluid" type that represents a pending instruction. The instruction is only executed or transformed when a **Terminal Method** is called.

| Terminal Method | Output | Purpose                                                                                       |
| :--- | :--- |:----------------------------------------------------------------------------------------------|
| **`.Send()`** | `void` | Standard fire-and-forget command (e.g., `Label`).                                             |
| **`.SendResp()`** | `ResponseFlagsE` | Executes and returns interaction bitmasks (Click, Hover, etc.).                               |
| **`.KeepIter()`** | `iter.Seq[...]` | Used for containers. Opens a block in Go; sends "Close" on exit.                              |
| **`.SendIter()`** | `iter.Seq[...]` | Similiar to `.KeepIter()`                                                                     |
| **`.Keep()`** | `RetainedHolder` | Converts a builder into a data object to be passed into another widget (evaluated arguments). |

---

# 5. `.Keep()` and Deferred Blocks — Capture-Now-Use-Later Patterns

The framework has two complementary mechanisms for deferring when and where opcode bytes are sent to Rust.

## 5.1 `.Keep()` — Retain a Single Builder's Bytes

Calling `.Keep()` on a fluid builder serializes the builder's accumulated bytes into a `RetainedFffiHolderTyped[T]`. **Nothing is sent to Rust.** The retained holder is later **spliced** into another widget's message via `SpliceRetained`.

```go
atoms := c.Atoms().RichText("bold").Strong().EndRichText().Keep()
// Serialized bytes stored in Go memory, NOT sent to Rust yet.

c.Button(ids, atoms).Send()
// Button's message now contains: [Button opcode][id][Atoms bytes...][ButtonBuild]
// The Atoms payload is embedded inside the Button message.
```

On the Rust side, `Button`'s handler calls `interpret_inner` on the embedded `Atoms` opcode. Everything is processed within the scope of that one `Button` message.

### Properties

- **Content-addressed deduplication**: `BuildRetained()` interns the byte buffer via `unique.Make`. Identical builders across frames produce the same `RetainedElementId` — same pointer, zero allocation.
- **Reusable across frames**: A retained holder can be stored in a `var` and reused every frame without rebuilding.
- **Single-widget scope**: Captures exactly one builder's output. Used for evaluated arguments (`Atoms`, `WidgetText`, `Color32`, `ScalarSize`).

### Usage Scenarios

| Pattern | Example | Why `.Keep()` |
|:---|:---|:---|
| Evaluated arg for a widget | `Button(ids, Atoms().Text("x").Keep())` | Atoms embedded inside Button's message |
| Evaluated arg for a block | `Window(ids, WidgetText().Text("title").Keep())` | Title embedded inside Window's message |
| Pre-built constant | `var label = Atoms().Text("OK").Keep()` (global) | Avoid rebuilding identical bytes every frame |
| Color argument (primary) | `color.RGB(0, 200, 200).Keep()` | ADR-0003: unified `color.Color` type; encoder picks transport per-arg |
| Color argument (legacy escape-hatch) | `color.FromRetainedHolder(c.Color().FromRgbaUnmultiplied(r,g,b,a).Keep().Untype(), rgba)` | For non-premult / `FromBlackAlpha` semantics not surfaced by `color.*` constructors |

## 5.2 Deferred Blocks — Capture Arbitrary Opcode Sequences

Deferred blocks capture **multiple independent `.Send()` calls** into a keyed buffer. A parent widget embeds all captured blocks in its own message, and the Rust side replays them inside callbacks.

```go
et := c.EndETable(ids, numRows, rowHeight, 1, 0)

et.BeginHeaders(0, 0)           // redirect SendIntermediate → capture buffer
c.DisplayRichText("Name", ...)  // internally calls LabelAtoms(...).Send()
                                // Send() writes framed message to capture buffer, NOT to Rust
et.EndHeaders()                 // stop capture, store bytes keyed by (0, 0)

et.BeginCells(row, col)         // capture cell content
c.Label(value).Send()           // captured
et.EndCells()

et.Send()                       // SpliceDeferredBlockMap: all blocks embedded in EndETable message
```

On the Rust side, `EndETable` reads the block map, stores it, and passes it to the `TableDelegate`. When egui calls `cell_ui(row, col)`, the delegate calls `replay_deferred_block(ctx, ui, block)` which feeds the captured bytes back through `interpret_outer` with a real `ui`.

### Properties

- **Multi-message capture**: Captures a sequence of `.Send()` calls — each becomes a framed message in the buffer.
- **Keyed storage**: Each block is identified by a composite key (e.g., `(row, col)` for cells, `(header_row, col)` for headers).
- **Callback replay**: Captured opcodes are replayed inside Rust-side callbacks that provide a `ui` context. Required for APIs like `egui_table::TableDelegate` where Rust calls back into the interpreter.
- **Fresh each frame**: Unlike `.Keep()`, deferred block captures are not reused across frames.

### IDL Declaration

```go
idl.NewBuilderFactoryNode("endETable").
    WithDeferredBlockMap("cells", ctabb.U64, ctabb.U32).   // generates BeginCells/EndCells
    WithDeferredBlockMap("headers", ctabb.U32, ctabb.U32). // generates BeginHeaders/EndHeaders
```

## 5.3 How They Compose

`.Keep()` and deferred blocks are complementary layers that can nest:

```
                         ┌─────────────────────────────────────┐
  .Keep()                │ Captures ONE builder's bytes         │
  (evaluated arg)        │ Spliced INTO a parent widget message │
                         │ Processed via interpret_inner        │
                         └─────────────────────────────────────┘
                                         │
                                    can be used inside
                                         │
                         ┌─────────────────────────────────────┐
  Deferred Block         │ Captures MANY Send() calls          │
  (BeginX/EndX)          │ Spliced into drain-node message     │
                         │ Replayed via replay_deferred_block  │
                         └─────────────────────────────────────┘
```

A `.Keep()` holder can appear **inside** a deferred block capture. When you call `.Send()` on a widget that contains a spliced `.Keep()` holder during a `BeginCells`/`EndCells` scope, the entire message (including the retained bytes) goes into the capture buffer:

```go
et.BeginHeaders(0, 0)
c.LabelAtoms(c.Atoms().RichText("Name").Strong().EndRichText().Keep()).Send()
//            ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^ .Keep() → retained, spliced into LabelAtoms
//           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^ .Send() → captured by deferred scope
et.EndHeaders()
```

Two layers of "capture now, use later" working together: `.Keep()` scopes the Atoms arg inside LabelAtoms, and the deferred block scopes the entire LabelAtoms call inside the table header.

## 5.4 Nested Deferred Blocks

Deferred blocks can nest inside other deferred blocks. `Fffi2.captureStack` is a stack of buffers — each `BeginCapture` pushes, `EndCapture` pops, and `SendIntermediate` writes to the innermost. This unlocks compositions like "an etable inside a dock-area tab body":

```go
for dock := range c.DockArea(ids.PrepareStr("main")) {
    for range dock.Tab(2, "data") {
        // this begins ANOTHER deferred capture (etable cells) inside the
        // one opened by Tab() — perfectly fine since the framework's
        // capture stack supports arbitrary nesting
        et := c.EndETable(ids.PrepareStr("inner"), rows, 20.0, 1, 0)
        for row := range rows {
            et.BeginCells(row, 0)
            c.Label(val).Send()
            et.EndCells()
        }
        et.Send()
    }
}
```

Each inner `Send()` routes to the innermost active capture buffer. On the wire, the etable's whole message (opcode + args + spliced cells/headers block maps) ends up embedded in the tab body's bytes; on replay the interpreter reads the tab body, sees the etable opcode, reads ITS deferred block maps from the replay stream (which recursively points at the tab body's byte slice), etc.

Historical footnote: an earlier version of `Fffi2` stored a single `captureBuf` and paniced on "nested BeginCapture". Any deferred-block widget inside another deferred-block widget — including the first `DockArea` attempt to host an etable — tripped it. The stack conversion is the enabling invariant.

There is also a complementary primitive, `Fffi2.AppendRawToCapture(raw []byte)`, that writes already-framed bytes directly into the innermost capture without adding a new frame header. Used when a builder has captured opcodes into a detached buffer at declaration time and wants to flush them into a deferred block at a later `Send` — see the `DockArea` iter-scope pattern below.

## 5.5 Iter-Scope Wrapper for Deferred-Block Widgets

When an IDL-generated deferred-block factory takes upfront arg arrays (`dockAreaRaw(ids, titles)` — all tabs need to be known before the first `BeginTabBody` is called on the wire) but you want ergonomic iter-style per-item declaration with `(id, title, body)` grouped at the call site, wrap the generated factory with a hand-written iter-scope helper. Reference: `DockArea` in `egui2_methods.go`.

### Pattern

1. Rename the IDL node to a `*Raw` variant so the primary name is available for the wrapper. The generated `DockAreaRaw(id, ids, titles) DockAreaRawFluid` stays as the low-level entry point; users never call it directly.
2. Define a hand-written `*Fluid` struct that accumulates the per-item args plus pre-captured body bytes:
   ```go
   type DockAreaFluid struct {
       idGen     WidgetIdCreatorI
       derivedId uint64
       ids       []uint64
       titles    []string
       bodies    [][]byte
   }
   ```
3. Make the primary entry point an `iter.Seq[*Fluid]` (matches `IdScope`/`KeepIter` lifecycle). On entry: `DeriveStacked` to consume the prepared id state and push — inner PrepareStr calls then work, and tab-body widget ids are scoped under the dock id. Defer: `send()` + `PopIdFromStackChecked`:
   ```go
   func DockArea(id WidgetIdCreatorI) iter.Seq[*DockAreaFluid] {
       return func(yield func(*DockAreaFluid) bool) {
           fluid := &DockAreaFluid{idGen: id, derivedId: id.DeriveStacked()}
           defer func() {
               fluid.send()
               id.PopIdFromStackChecked(fluid.derivedId)
           }()
           yield(fluid)
       }
   }
   ```
4. Per-item method (`Tab(id, title)`) returns `iter.Seq` and captures its body into a detached `*bytes.Buffer` via `BeginCapture`/`EndCapture`, then appends `(id, title, buf.Bytes())` to the fluid's slices.
5. Private `send()` calls the generated raw factory with the accumulated arrays, then for each item opens `BeginTabBody(id)` (pushes the deferred block map's temp buf onto the capture stack), flushes the detached bytes via `AppendRawToCapture`, closes `EndTabBody`, and finally calls raw `.Send()`.

### Usage shape

```go
for dock := range c.DockArea(ids.PrepareStr("main")) {
    for range dock.Tab(1, "widgets") { /* body emits widgets */ }
    for range dock.Tab(2, "data")    { /* body emits etable, plot, … */ }
    // no explicit Send — iter exit emits the opcode and pops the id
}
```

### Why this shape

- `iter.Seq` lifecycle + defer mirrors `IdScope` and `KeepIter` — the existing idiom for "push an id, do work, pop an id".
- Grouping `(id, title, body)` at the call site is the whole reason for the wrapper; plain `DockAreaRaw` separates ids/titles (constructor args) from bodies (Begin/End calls), which reads awkwardly.
- `AppendRawToCapture` sidesteps double-framing: bodies captured into detached buffers already carry their inner `[u32 len][payload]` frames per `SendIntermediate`; concatenating raw bytes into the deferred block map's temp buf preserves framing.
- `DeriveStacked` on entry consumes the "prepared" id-stack state that `ids.PrepareStr(…)` left behind, so user code inside a tab body can freely call `ids.PrepareStr(…)` / `IdScope`. Without this step the first `PrepareStr` inside a body panics with "invalid state transition — allowed=initial state=prepared".

## 5.6 Colors — Unified `color.Color` over Two FFFI Transports (ADR-0003)

Color-bearing widget arguments are surfaced uniformly as a single Go type, [`egui2/color.Color`](../../../public/thestack/imzero2/egui2/widgets/color), regardless of whether the underlying FFFI2 transport for that argument is a `PlainArg(U32)` or an `EvaluatedArg(Color32)`. The IDL annotates color-shaped args with `.AsColor()` (scalar) or `.AsColors()` (slice, literal-only per SD9); the generator emits `color.Color` / `color.Colors` in the Go signature and routes the wire encode through `color.PutAsU32` / `components.PutColorAsRetainedColor32` / `color.PutColorsSlice` based on the per-arg transport.

### Construction

| Need | Call | Notes |
|:---|:---|:---|
| Hex literal | `color.Hex(0xRRGGBBAA)` | sRGB non-premultiplied per SD8 |
| RGB (opaque) | `color.RGB(r, g, b)` | alpha=0xff |
| RGBA | `color.RGBA(r, g, b, a)` | full control |
| Gray (opaque) | `color.Gray(v)` | shorthand for `RGB(v,v,v)` |
| Retained variant | `<any literal>.Keep()` | promotes to retained kind; retains the originating u32 so PlainArg-transport flattening is zero-cost |
| Bulk (literal-only) | `color.NewColors(n)`, `color.ColorsFromU32(s)`, `color.ColorsFromSlice(cs)` | wire = packed `U32h`; nil-slice sentinel guarded |
| Escape hatch | `color.FromRetainedHolder(c.Color().FromRgbaUnmultiplied(r,g,b,a).Keep().Untype(), rgba)` | for `FromBlackAlpha` / `FromRgbaUnmultiplied` semantics not surfaced by `color.*` constructors |

### Key invariants

- **Wire format unchanged.** A literal `color.Color` over a `PlainArg(U32)` transport emits the same 4 bytes as the pre-ADR `uint32` arg. A literal over an `EvaluatedArg(Color32)` transport emits the same Color32-construction opcodes that `c.Color().FromRgbaUnmultiplied(r,g,b,a).Keep()` + splice produced — verified byte-for-byte by [`components/egui2_color_splice_test.go`](../../../public/thestack/imzero2/egui2/bindings/egui2_color_splice_test.go).
- **Retained variant is stateless.** `Color.Keep()` on a literal flips the kind flag and stashes the originating `u32`; no retained holder is built eagerly. The actual opcode splice is synthesised by `components.PutColorAsRetainedColor32` at wire time. This sidesteps the deferred-block / culling discipline issue that any encoder-side state would face (ADR-0003 SD2).
- **Arrays are literal-only.** `color.Colors` is `type Colors []uint32`. Retained values cannot enter a `Colors` (no `Set(int, Color)` overload) — the constraint is enforced by construction (SD9). For "share one color across many calls", use a retained scalar, not an array of retained colors.

### When you might still see `Color32S` / `c.Color().Foo().Keep()`

The legacy fluent factory at [`egui2_definition_d_colors.go`](../../../public/thestack/imzero2/egui2/definition/egui2_definition_d_colors.go) is unchanged. It remains the right tool when you need:
- `FromRgbaUnmultiplied` for wire-explicit unmultiplied semantics in a one-off site (most code paths default to non-premult through `color.*` constructors anyway).
- `FromBlackAlpha`, `GammaMultiplyU8`, `LinearMultiplyF32`, `ToOpaque`, named palette constants (`ColorCyan`, `ColorGold`, etc.) — egui-side modifiers that the `color.*` constructor surface deliberately does not mirror.
- A pre-built retained holder you intend to share across many widget calls without re-emission. Wrap with `color.FromRetainedHolder(holder.Untype(), rgba)` before passing to a `.AsColor()`-typed argument.

The hot path for SQL syntax highlighting in [`widgets/codeview/sql.go`](../../../public/thestack/imzero2/egui2/widgets/codeview/sql.go) uses this escape hatch to keep its per-category palette as pre-built retained holders, paying zero per-frame synthesis cost.

---

# 6. Rich Text (Styled Text in Widgets)

Widgets that display text come in two flavors based on their egui type:

| egui Type | Go Evaluated Arg | Widgets |
|:---|:---|:---|
| `Atoms` (multi-segment, styled) | `Atoms().Keep()` | `Button`, `RadioButton`, `LabelAtoms`, `MenuButton` |
| `WidgetText` (single string) | `WidgetText().Text("...").Keep()` | `Label`, `Window`, `CollapsingHeader`, `ComboBox` |

There is no conversion between `Atoms` and `WidgetText` in egui — they are distinct types.

## 6.1 Plain Text

For unstyled text, use `Atoms().Text()` or `WidgetText().Text()`:

```go
// Button with plain text
c.Button(ids, c.Atoms().Text("Click me").Keep()).Send()

// Window with plain title
for range c.Window(ids, c.WidgetText().Text("My Window").Keep()).KeepIter() { ... }
```

## 6.2 Inline Rich Text via the Typed RichTextScope

Styled (rich) text is constructed inline on `Atoms()` using `BeginRichText(text)`, which returns a `RichTextScope`. This scope **only** exposes style methods — calling `.Text()` or `.Keep()` inside it is a compile error. Call `.End()` to close the segment and return to `AtomsFluid`. Multiple segments can be chained.

```go
// Single styled segment:
c.Button(ids, c.Atoms().BeginRichText("bold").Strong().End().Keep()).Send()

// Multi-segment rich text:
c.Button(ids, c.Atoms().
    BeginRichText("bold").Strong().End().
    BeginRichText(" normal").End().
    BeginRichText(" code").Code().End().
    Keep()).Send()

// Mixed plain + styled:
c.Button(ids, c.Atoms().
    Text("plain ").
    BeginRichText("styled").Italics().End().
    Keep()).Send()
```

### Type Safety

| Expression | Compiles? | Why |
|:---|:---|:---|
| `Atoms().BeginRichText("x").Strong().End()` | Yes | `Strong()` is on `RichTextScope` |
| `Atoms().Strong()` | No | `Strong()` not on `AtomsFluid` (only on the generated flat API) |
| `Atoms().BeginRichText("x").Text("y")` | No | `Text()` not on `RichTextScope` |
| `Atoms().BeginRichText("x").Keep()` | No | `Keep()` not on `RichTextScope` |

### Available Style Methods (on RichTextScope)

| Method | Argument | Effect |
|:---|:---|:---|
| `Strong()` | — | Bold |
| `Weak()` | — | Dimmed |
| `Italics()` | — | Italic |
| `Underline()` | — | Underlined |
| `Strikethrough()` | — | Struck through |
| `Code()` | — | Monospace code style |
| `Monospace()` | — | Monospace font |
| `Heading()` | — | Heading size |
| `Small()` | — | Small text |
| `SmallRaised()` | — | Small + raised |
| `Raised()` | — | Raised baseline |
| `Size(f32)` | font size | Custom font size |
| `ExtraLetterSpacing(f32)` | spacing | Additional letter spacing |
| `LineHeight(f32)` | height | Custom line height |
| `LineHeightDefault()` | — | Reset to default line height |

Style methods can be chained: `.BeginRichText("x").Strong().Italics().Small().End()`.

## 6.3 DisplayRichText Convenience

For the common case of displaying a single styled label:

```go
// Shows a bold label
c.DisplayRichText("hello", func(a c.RichTextScope) c.RichTextScope { return a.Strong() })

// No styling — just plain:
c.DisplayRichText("hello", nil)
```

The closure receives a `RichTextScope`, so only style methods are available — type-safe by construction.

## 6.4 Rich Text in Tables

For the register-drain table (`Table`), use plain text cells (`TableCellText`). For styled table content, use the deferred-block etable (`EndETable`) with `DisplayRichText` inside `BeginHeaders`/`BeginCells` blocks:

```go
et := c.EndETable(ids, numRows, rowHeight, 1, 0)

et.BeginHeaders(0, 0)
c.DisplayRichText("Name", func(a c.RichTextScope) c.RichTextScope { return a.Strong() })
et.EndHeaders()

et.BeginCells(row, col)
c.DisplayRichText(value, func(a c.RichTextScope) c.RichTextScope { return a.Monospace() })
et.EndCells()

et.Send()
```

## 6.5 Design Note: Why Not PushRichText?

An earlier API used `PushRichText("text").Strong().Send()` to push styled text into a global register (`r0_atoms`), which a later widget would drain. This was replaced because:

1. **Global register coupling** — any widget evaluating an Atoms arg would drain whatever happened to be in the register, regardless of intent.
2. **Cross-widget leaks** — if a consuming widget was culled or skipped, atoms could leak to unrelated widgets.
3. **WidgetText contamination** — an attempt to auto-drain atoms into WidgetText affected all WidgetText evaluations.

The inline sub-protocol eliminates these issues: everything is scoped within a single `Atoms()` builder evaluation.

---

# 7. The Node & Tree System

This system decouples **Logical Hierarchy** from **Visual Rendering**.

1.  **The Register**: Calling `NodeDir` or `NodeLeaf` does not draw immediately. It populates a "Node Register" on the client.
2.  **The Flush**: Calling `components.Tree(id).Send()` takes everything in the current Register, renders it as a tree-view, and **clears the register**.
3.  **The Scope**: `NodeDir(...).SendIter()` automatically handles the nesting depth within the register.

---

# 8. Interpreted Values (Scalar/Vector Size)

Because the Server (Go) is often decoupled from the Client (Display), Go does not always know the exact pixel dimensions of the UI.
*   **Concept**: Instead of sending `width: 500px`, Go sends an **Instruction** like `ScalarSize().AvailableWidth()`.
*   **Evaluation**: The Client evaluates this instruction locally during the render pass to determine the actual size.
*   **Usage**: `.Keep()` these instructions to pass them as constraints to other widgets.

---

# 9. Idiomatic Best Practices

### ID Management Stability
| Scenario | Approach                                         | Why? |
| :--- |:-------------------------------------------------| :--- |
| **Dynamic Labels** | `ids.PrepareStr("fixed_key")`                    | If the label changes from "Start" to "Stop", the ID stays the same so focus isn't lost. |
| **Loops/Lists** | `components.IdScope(MakeWidgetIdStr(item.UUID))` | Prevents ID collisions between identical rows. |
| **Localized Text** | `components.PrepareStr("internal_id")`           | Ensures the ID doesn't change when the user changes language. |

### The "Loop Namespacing" Pattern
Always wrap dynamic content in an `IdScope` to ensure child widgets (like an "Edit" button) have unique effective IDs.

---

# 10. Comprehensive Example

```go
package demo

import (
	"fmt"
	"time"
	"github.com/rs/zerolog/log"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

var n int
var sliderVal float64
var checkboxVal = false
var frame uint64
var myText string
var myDragFloat float64
var dropDownSelected int = -1
var radioChoice uint8
var ids = c.NewWidgetIdStack()
func RenderDemoWindow() {
	c.CurrentApplicationState.StartServersideFrame()
	defer c.CurrentApplicationState.FinishServersideFrame()
	statemanager := c.CurrentApplicationState.StateManager

	incrementLabelAtoms := c.Atoms().Text("increment/decrement").Keep()
	for range c.Window(ids.PrepareStr("imzero2"), c.WidgetText().Text("imzero2").Keep()).KeepIter() {
		c.Label(time.Now().GoString()).Send()
		{
			r := c.Button(c.MakeAbsoluteIdSeq(0xdeadbeef), incrementLabelAtoms).SendResp()
			if r.HasPrimaryClicked() {
				n++
			} else if r.HasSecondaryClicked() {
				n--
			}
		}
		for range c.IdScope(ids.PrepareStr("myscope")) {
			c.TextEdit(ids.PrepareSeq(0xf4f4), myText).SendRespVal(&myText)
			c.DragValueF64(ids.PrepareSeq(0x45445), myDragFloat).SendRespVal(&myDragFloat)
		}
		for range c.ComboBox(ids.PrepareStr("combobox"), c.WidgetText().Text("combobox").Keep(), c.WidgetText().Text(fmt.Sprintf("option %d", dropDownSelected)).Keep()).KeepIter() {
			for i := 0; i < 10; i++ {
				selected := i == dropDownSelected
				if c.Button(ids.PrepareSeq(uint64(0x1111+i)), c.Atoms().Text(fmt.Sprintf("option %d", i)).Keep()).Selected(selected).FrameWhenInactive(!selected).Frame(true).SendResp().HasPrimaryClicked() {
					dropDownSelected = i
				}
			}
		}
		if c.RadioButton(ids.PrepareStr("radio 1"), c.Atoms().Text("radio 1").Keep(), radioChoice == 1).SendResp().HasPrimaryClicked() {
			radioChoice = 1
		}
		if c.RadioButton(ids.PrepareStr("radio 2"), c.Atoms().Text("radio 2").Keep(), radioChoice == 2).SendResp().HasPrimaryClicked() {
			radioChoice = 2
		}
		if c.RadioButton(ids.PrepareStr("radio 3"), c.Atoms().Text("radio 3").Keep(), radioChoice == 3).SendResp().HasPrimaryClicked() {
			radioChoice = 3
		}

		c.Label(fmt.Sprintf("%d", n)).Selectable(false).Send()
		c.Separator().Send()

		c.SliderF64(ids.PrepareSeq(0xfefe), sliderVal, 0.0, 100.0).
			Text("my text").
			SendRespVal(&sliderVal)
		c.Label(fmt.Sprintf("checked=%v", checkboxVal)).Send()
		if c.Checkbox(ids.PrepareSeq(0x343af), checkboxVal, "my checkbox").SendRespVal(&checkboxVal).HasChanged() {
			log.Info().Bool("value", checkboxVal).Msg("checkbox has changed")
		}

		if c.Button(ids.PrepareSeq(0x33333), c.Atoms().Text("set to true").Keep()).SendResp().HasPrimaryClicked() {
			checkboxVal = true
			statemanager.OverrideDatabindingWidget(0x343af)
		}
		if c.Button(ids.PrepareSeq(0x33334), c.Atoms().Text("set to false").Keep()).SendResp().HasPrimaryClicked() {
			statemanager.OverrideDatabindingWidget(0x343af)
			checkboxVal = false
		}
		{
			c.Label(fmt.Sprintf("frame=%d", frame)).Send()
			c.Passthrough(ids.PrepareSeq(123456789), frame)
			frame += 2
		}

		for range c.VerticalCenteredJustified().KeepIter() {
			c.Label("A").Send()
			c.Label("B").Send()
			c.Label("C").Send()
		}
		for range c.Grid(ids.PrepareSeq(0xfefe)).NumColumns(3).KeepIter() {
			c.Label("A").Send()
			c.Label("B").Send()
			c.Label("C").Send()
			c.EndRow()
			c.Label("D").Send()
			c.Label("E").Send()
			c.Label("F").Send()
		}

		for range c.NodeDir(ids.PrepareStr(""), c.WidgetText().Text("dir 0").Keep()).SendIter() {
			for range c.NodeDir(ids.PrepareStr(""), c.WidgetText().Text("dir 1").Keep()).SendIter() {
				if c.NodeLeaf(ids.PrepareStr(""), c.WidgetText().Text("leaf 0").Keep()).SendResp().HasNodelikeSelected() {
					c.NodeLeaf(ids.PrepareStr(""), c.WidgetText().Text("--- leaf 0 has is selected ---").Keep()).Send()
				}
				c.NodeLeaf(ids.PrepareStr(""), c.WidgetText().Text("leaf 1").Keep()).Send()
				c.NodeLeaf(ids.PrepareStr(""), c.WidgetText().Text("leaf 2").Keep()).Send()
			}
			for range c.NodeDir(ids.PrepareStr(""), c.WidgetText().Text("dir 2").Keep()).SendIter() {
				c.NodeLeaf(ids.PrepareStr(""), c.WidgetText().Text("leaf 3").Keep()).Send()
				c.NodeLeaf(ids.PrepareStr(""), c.WidgetText().Text("leaf 4").Keep()).Send()
			}
		}
		for range c.ScrollArea().Vscroll(true).KeepIter() {
			for range c.ScrollArea().Vscroll(true).KeepIter() {
				c.Tree(ids.PrepareSeq(0xaaaa)).Send()
			}

			for range c.CollapsingHeader(ids.PrepareStr("section 1"), c.WidgetText().Text("section 1").Keep()).KeepIter() {
				c.Label("hello section1").Send()
				r := c.Button(ids.PrepareSeq(0xcaffe), incrementLabelAtoms).SendResp()
				if r.HasPrimaryClicked() {
					n++
				} else if r.HasSecondaryClicked() {
					n--
				}
			}
		}
	}

	for range c.Window(ids.PrepareSeq(0xffeefe), c.WidgetText().Text("imzero2 debug tools").Keep()).KeepIter() {
		c.ShowDebugTools()
		c.ShowPuffinProfiler()
	}

	c.RequestRepaint()
}
```

---

# 11. Debugging & Troubleshooting

1.  **Ghost Interactions**: If clicking "Button A" triggers "Button B", you have an **ID Collision**. Check if you are using identical labels in the same scope without an `IdScope`.
2.  **Focus Loss**: If a text field loses focus as soon as you type, your ID is **unstable**. Check if your ID is derived from a string that changes based on the input text.
3.  **Missing Nodes**: If nodes aren't appearing in your Tree, ensure you aren't calling `Tree()` before the `NodeDir` loops finish, or that you aren't mixing node registers intended for different trees.
4.  **Layout Jumps**: Ensure Absolute widgets (like Windows) use `AbsoluteLabelDefinedIdG` to avoid being shifted by the relative stack of a parent container.

# 12. Pitfalls
##  Pattern: Stable Pointers for Delayed FFI State (ImZero2)

**The Pitfall: Frame-Local Variables**
In traditional immediate-mode GUIs (like Dear ImGui in C++), passing a pointer to a local stack variable works because interactions are processed synchronously within the same frame. However, **ImZero2** operates across an RPC/FFI boundary, meaning data bindings and event responses suffer a **1-frame delay**.
If you pass a pointer to a temporary frame-local variable (e.g., `val := state[key]; widget.SendRespVal(&val)`), the framework will attempt to write the user's input to a discarded pointer on the next frame. The local UI state will fail to update.

**The Solution: Stable Heap Pointers**
Widgets that accept pointer bindings (`*string`, `*float64`, `*bool`) must be bound to **stable, heap-allocated memory** that survives across frame boundaries.

1. **Store pointers, not values:** Change your state maps from `map[string]string` to `map[string]*string`.
2. **Initialize stably:** If a key doesn't exist, allocate a new string on the heap (`newVal := ""; state[key] = &newVal`).
3. **Bind directly:** Pass this stable pointer directly to the widget (`widget.SendRespVal(state[key])`).
4. **Read on demand:** When an action occurs (like a button click), dereference the stable pointer (`*state[key]`) to capture the asynchronously updated value.


* **The Symptom:** Text edits or sliders reset the user's input instantly, or the text cursor behaves erratically.
* **The Cause:** ImZero2 operates across an FFI (Foreign Function Interface) boundary, meaning user input events are processed with a 1-frame delay. If you bind a widget to a local stack variable (`val := state[key]; widget.SendRespVal(&val)`), the backend attempts to write the asynchronous user input into a pointer that died on the previous frame.
* **The Pattern:** **Stable Heap Pointers**. Always bind input widgets to stable memory that survives frame boundaries.
  ```go
  // WRONG:
  val := myMap[key]
  c.TextEdit(id, val).SendRespVal(&val)

  // RIGHT:
  valPtr, ok := myMap[key]
  if !ok {
      newVal := "default"
      valPtr = &newVal
      myMap[key] = valPtr // Store the pointer stably
  }
  c.TextEdit(id, *valPtr).SendRespVal(valPtr)
  ```

## Jumping UI (ID Drift)
* **The Symptom:** Windows reset their positions when you click a button, or scroll areas jump wildly when a new item is added to a list.
* **The Cause:** Widgets rely on a hash ID to remember their state (position, scroll, focus). If you rely on a single flat auto-incrementing ID stack, dynamically rendering a new element (like an error message or a new list item) shifts the auto-IDs of *everything rendered after it*. ImZero2 thinks the shifted widgets are completely new elements and resets their state.
* **The Pattern:** **Tree Hashing & ID Scopes**. Wrap dynamic lists and conditional blocks in `c.IdScope()`. This pushes a namespace onto the hash tree, isolating the auto-ID counter so siblings aren't affected.
  ```go
  // WRONG: Flat stack shifting
  if hasError { c.Label("Error").Send() }
  c.Window(ids.PrepareStr("win"))... // ID changes if hasError toggles!

  // RIGHT: Scoped namespaces
  if hasError {
      for range c.IdScope(ids.PrepareStr("error_scope")) {
          c.Label("Error").Send()
      }
  }
  // The Window ID remains perfectly stable
  for range c.Window(ids.PrepareStr("win"))... 
  ```

## Flickering Widget (Boolean Short-Circuiting)
* **The Symptom:** Buttons or input fields completely vanish from the screen when a certain condition is met (like a background task starting), causing the layout to collapse and expand.
* **The Cause:** In Go, `if condition && widget.SendResp().HasClicked()` will short-circuit if `condition` is false. In immediate-mode GUIs, if you don't call `.Send()` or `.SendResp()` on a widget during the frame loop, **it is not added to the render tree at all**.
* **The Pattern:** **Unconditional Rendering**. Always evaluate the widget's send method *before* or *independent of* the logical condition.
  ```go
  // WRONG: Widget vanishes when processing
  if !isProcessing && c.Button(id, "Save").SendResp().HasPrimaryClicked() { ... }

  // RIGHT: Widget always renders, but click is conditionally ignored
  btn := c.Button(id, "Save")
  if btn.SendResp().HasPrimaryClicked() && !isProcessing { ... }
  ```

## Micro-Flash (Sub-Frame UI Locking)
* **The Symptom:** When triggering local disk I/O, disabled widgets "flash" or "strobe" erratically instead of looking deliberately locked.
* **The Cause:** Local tasks (like a simple file write or `pijul` local execution) might complete in 2-5 milliseconds. This means `isProcessing` becomes `true` and then `false` within the span of a single 16ms frame (60FPS). The widget drops focus and grays out for exactly one frame, which the human eye perceives as a visual glitch.
* **The Pattern:** **Artificial Delays for Micro-Tasks**. If you intend to use a global "Locked/Loading" state for the UI, ensure the task takes long enough for the user to register the state change.
  ```go
  func WorkerLoop() {
      // Lock UI
      isProcessing = true
      
      DoFastLocalDiskIO() 
      
      // Ensure the "Locked" state is visible to the human eye
      time.Sleep(300 * time.Millisecond) 
      
      // Unlock UI
      isProcessing = false
  }
  ```

## Stubborn Text (Frontend State Override)
* **The Symptom:** You update a variable in the backend (e.g., from a network request or disk read), but the `TextEdit` widget immediately reverts it back to what the user last typed.
* **The Cause:** Widgets with internal state (like text cursors) consider the frontend the "source of truth". If you modify the bound pointer from the backend, the frontend simply overwrites it on the next frame with its cached state.
* **The Pattern:** **State Manager Overrides**. When you programmatically change a value that is bound to an interactive widget, you must call `OverrideDatabinding...` to command the frontend to drop its cache.
  ```go
  *valPtr = "new data from network"
  c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(valPtr)
  ```
## Self-Deadlock (Non-Reentrant Mutexes)
* **The Symptom:** A background worker or UI interaction freezes the entire application indefinitely.
* **The Cause:** Go’s `sync.Mutex` and `sync.RWMutex` are **not reentrant**. If a goroutine acquires a lock, it cannot acquire it again. In our app, `WorkerLoop` held the lock and called `ReloadAllActors`, which subsequently called `runCmd`. Because `runCmd` *also* tried to acquire the lock to append to the CLI log, the thread deadlocked waiting for itself.
* **The Pattern:** **Lock-Free Internal Helpers**. Never call a lock-acquiring method from inside another lock-acquiring block. Separate functions into public (locking) methods and private (lock-free) helpers. Alternatively, bypass the locking method entirely (as we did by using a raw `exec.Command` for silent background logging instead of the UI-bound `runCmd`).

## Framework Data Race (Thread-Unsafe GUI APIs)
* **The Symptom:** The Go race detector panics, or the app crashes randomly when background tasks finish and try to wake up the UI or trigger repaints.
* **The Cause:** **All functions in the ImZero2 `c` package are strictly single-threaded** and belong exclusively to the main UI frame lifecycle. Calling *any* framework method (like `c.RequestRepaint()`, widget builders, or layout scopes) from a background `WorkerLoop` goroutine causes a severe data race with the main render thread.
* **The Pattern:** **Main-Thread Handoffs**. Never invoke UI framework functions from background goroutines. If a background worker needs to trigger a repaint or an override, it must signal the main thread using thread-safe Go primitives (channels, atomic booleans, or a locked pending-overrides map). The main `RenderWindow` loop must check this signal and call the `c.*` functions itself.

## FFFI2 Widget Definition Rules

When defining new widgets via `idl.NewBuilderFactoryNode()`:

### Argument Naming: Minimum 2 Characters
All argument names in factories and methods **must be at least 2 characters long**. Single-letter names (e.g. `r`, `s`, `w`) clash with the FFFI2 framework's internal variables in generated Rust code (`r` is the response, `w` is the widget instance, `u` is the UI context, `i` is the ID, `c` is the egui context, `d` is the recursion depth).

```go
// WRONG: single-letter arg name clashes with framework variables
BeginMethod("radius").Arg("r", ctabb.F32)

// RIGHT: use descriptive 2+ character names
BeginMethod("radius").Arg("ra", ctabb.F32)
```

### Non-Scalar PlainArg Types (Homogeneous Arrays)
`PlainArg` supports non-scalar canonical types like `ctabb.F64h` (homogeneous `[]float64` array), `ctabb.U64h`, `ctabb.I64h`, `ctabb.Sh`, etc. These generate:
- **Go factory**: `func PlotLine(name string, xs []float64, ys []float64)` using `runtime.PutFloat64SliceArg(r, xs)`
- **Rust read**: `let mut xs = self.io.read_plain_f64h()` returning `Vec<f64>`
- **Wire format**: `u32 length` + `element * length`

### Return Types Are Required
Every `BuilderFactoryNode` must have a `WithReturnType(...)` call. Missing it produces `<invalid>` in the generated `Keep()` method signature. Define concrete type helpers in `egui2_definition_d_types.go`:
```go
func structMyWidget() ir.ConcreteType {
    return ir.NewConcreteType("myWidget")
}
```

### Never Edit Generated Files
Files ending in `.out.go` and `enums_out.rs` are regenerated by `./generate.sh`. All customizations must go into:
- **Definition files** (`egui2_definition_d_*.go`) for widget IDL
- **Hand-maintained Rust** (`interpreter.rs` struct fields/data types, `io.rs` read methods, `fenums.rs` constants)
- **Hand-maintained Go** (non-`.out.go` files in `components/`)

### Register-Drain Pattern for Non-Widget APIs
APIs that take closures with non-`egui::Ui` contexts (like `egui_plot::Plot::show(|plot_ui|{})`) cannot use the BlockIterator pattern. Use register-drain instead:
1. Define accumulator nodes that push data into Rust-side `Vec` registers
2. Define a drain node whose apply code calls `.drain(..)` on all registers and renders inside the closure

### DeferredBlockMap for Callback APIs
APIs with `egui::Ui` callbacks (like `egui_table::TableDelegate`) use `WithDeferredBlockMap(name, keyTypes...)`. Go captures opcode blocks via `BeginCells/EndCells`, Rust replays them in callbacks via `replay_deferred_block()`.

## State Bleed (Unscoped Background Updates)
* **The Symptom:** User A is actively typing in a text field. User B clicks a button that triggers a background update. When the update finishes, User A's unsaved typing is instantly reverted to the old value on disk.
* **The Cause:** The background worker blindly synchronized the *entire* disk state back to the UI state manager, failing to differentiate between the data that actually changed and the data that users were currently modifying in memory.
* **The Pattern:** **Targeted Cache Overrides**. Background tasks must return an explicit list of "Affected Targets" (e.g., `affectedActors []string`). The synchronization loop must strictly limit UI pointer overrides to those specific targets, ensuring the unsaved memory of other UI components remains mathematically isolated and protected.

## Silent Hover Tooltip (Scope Response Hit-Test Order)
* **The Symptom:** A block wraps a widget in `ui.scope(...)`, takes the scope's response, and calls `.on_hover_text("tip")` or `.on_hover_ui(|ui| ...)`. The tooltip never appears, even though the pointer is clearly over the widget. No panic, no log line — just silence.
* **The Cause:** In egui (verified against 0.34), `ui.scope` registers its response widget at **child-ui construction**, i.e. BEFORE the scope body runs and adds its children. In the frame's back-to-front hit-test order the scope therefore sits BEHIND its children. egui's interaction snapshot (`interaction.rs`) only marks a non-interactive widget (hover-only sense) as `hovered` when it lies ABOVE the topmost interactive widget — the rule is there so that a label rendered on top of a draggable window still shows a tooltip. For a scope wrapping a `Button`, the scope is below the button, so `scope.response.hovered()` stays false whenever the pointer is over the button, and `on_hover_text` / `on_hover_ui` early-return silently via `should_show_tooltip` → `response.hovered()` guard.
* **The Pattern:** **Overlay Interact Widget**. After the scope body closes, re-register a fresh hover-only widget at the scope's rect via `ui.interact(rect, id.with("suffix"), egui::Sense::hover())`. This new widget is inserted AFTER all children, so it sits in front → hit-test sees it → `hovered()` returns true → tooltip fires. `HoverText` and `HoverUi` use this pattern. The Frame block uses the same technique for `senseClick`.
  ```rust
  // WRONG: tooltip never shows when pointer is over the button inside
  let resp = ui.scope(|ui| { /* button here */ }).response;
  resp.on_hover_text(text);

  // RIGHT: explicit overlay widget registered post-body
  let scope_resp = ui.scope(|ui| { /* button here */ }).response;
  let hover_resp = ui.interact(
      scope_resp.rect,
      scope_resp.id.with("imzero2_hover_text"),
      egui::Sense::hover(),
  );
  hover_resp.on_hover_text(text);
  ```

## Ragged Control Row (First Item in a Centered Horizontal Row)
* **The Symptom:** A toolbar of mixed controls — combo boxes, checkboxes, labels — laid out with `c.Horizontal()` does not sit on a single baseline. The *first* widget in the row renders a few pixels higher than everything after it, so the row reads as vertically "unstable" / ragged even though every control is the same height.
* **The Cause:** `c.Horizontal()` maps to egui's `ui.horizontal()` = `Layout::left_to_right(Align::Center)`, which vertically centers each item against the row. In immediate mode egui fixes the row's cross-axis line from the first item and anchors it differently from the items that follow; when the controls are all `interact_size.y` tall (combos, checkboxes, sliders) the leading widget lands a few px *above* its neighbours instead of on the shared centre line. (Observed against egui 0.34; the `sccmap` app's control row hit this exact issue.)
* **The Pattern:** **Top-Align Equal-Height Control Rows**. When every control in the row is the same height, use `c.HorizontalTop()` (`Align::Min`) instead of `c.Horizontal()`. Top-aligning skips the per-item cross-axis centering, so identical-height controls all land on one stable line. Reserve `c.Horizontal()` (centered) for rows that deliberately mix tall and short widgets and want them centred relative to one another.
  ```go
  // WRONG: centered row — the first control anchors a few px above the rest
  for range c.Horizontal().KeepIter() {
      sizeIdx = renderMetricCombo(ids, "size", "Size", sizeIdx)
      colorIdx = renderMetricCombo(ids, "color", "Color", colorIdx)
      c.Checkbox(ids.PrepareStr("tests"), incTests, "Include tests").SendRespVal(&incTests)
  }

  // RIGHT: equal-height controls top-aligned → one stable baseline
  for range c.HorizontalTop().KeepIter() {
      sizeIdx = renderMetricCombo(ids, "size", "Size", sizeIdx)
      colorIdx = renderMetricCombo(ids, "color", "Color", colorIdx)
      c.Checkbox(ids.PrepareStr("tests"), incTests, "Include tests").SendRespVal(&incTests)
  }
  ```

## Gallery Scroll-Host Layout — Side Panels Collapse, and the Tour Hides It
* **The Symptom:** A widget-gallery demo puts a control column beside a filling content area with `c.PanelLeftInside(...)` + `c.PanelCentralInside()`. The screenshot tour looks perfect, but in the *interactive* gallery the left panel collapses to a sliver and clips its controls while the central area fills wide. Related symptoms from the same layout: a fixed-width output column (`UiSetMaxWidth`) strands dead space on the right of a wide window with its scrollbar floating mid-pane, and a `DockArea` overflows / clips instead of scrolling.
* **The Cause:** The two demo hosts are not equivalent. The **TestDriver** (tour; `IMZERO2_SCREENSHOT_DIR` set) wraps each demo in a bounded `c.AllocateUiAtRect(0, 0, stageW, stageH)` — finite width **and** height, with a real region for side panels to claim. The **InteractiveDriver** (the gallery you click around in) wraps each demo in `c.ScrollArea().Vscroll(true).AutoShrink(false, false)` — full host width but **unbounded, scrollable height**, and **no CentralPanel region**. egui side panels (`SidePanel` / the `*Inside` variants) size against a CentralPanel region; inside a bare scroll area they get a degenerate width and clip. A `DockArea` (egui_dock fills its allocated rect) has no finite height to fill in the vscroll, so it clips. The tour's bounded rect papers over both — which is exactly why a tour-only "fix" readily regresses the gallery.
* **The Pattern:** **Author gallery-demo layouts for an unbounded-height, full-width scroll host.**
  - Top-level split: a plain `c.Horizontal()` — pin the control column to its fixed-control width (`UiSetMinWidth` + `UiSetMaxWidth`) and leave the content column **unconstrained** so the dock / scroll area inside fills the remaining width. Do **not** use `*Inside` side panels in a gallery demo. (A standalone *app* with its own `c.Window` may use them — the Window supplies the region the gallery's scroll host lacks; cf. `regex_explorer`.)
  - Give any `DockArea` / canvas a **bounded height** via `c.UiSetMinHeight(...)` before it, so it has a finite rect in the vscroll.
  - Fill content panes with `c.ScrollArea().AutoShrink(false, false)` so they occupy the full pane width (scrollbar at the pane edge, not mid-pane).
  - **Verify host-dependent layout in the interactive gallery, not the tour.** The tour is ideal for deterministic *content* checks (syntax highlighting, per-widget rendering) but a poor proxy for fill / clipping / bounded-height behaviour.
  ```go
  // WRONG — side panels collapse in the gallery's scroll host, clipping the editor
  for range c.PanelLeftInside(ids.PrepareStr("editor")).DefaultSize(540).Resizable(true).KeepIter() { renderEditor() }
  for range c.PanelCentralInside().KeepIter() { renderOutput() }

  // RIGHT — fixed control column + unconstrained fill column + bounded dock
  for range c.Horizontal().KeepIter() {
      for range c.Vertical().KeepIter() {
          c.UiSetMinWidth(560); c.UiSetMaxWidth(560)   // controls are fixed-width
          renderEditor()
      }
      for range c.Vertical().KeepIter() {              // unconstrained → fills remaining width
          c.UiSetMinHeight(360)                        // a DockArea needs a finite rect in the vscroll
          renderOutput()                               // DockArea + ScrollArea(AutoShrink(false,false)) fill the pane
      }
  }
  ```

---

# 13. Culling, Block Skipping, and Register Mechanics

This section describes the internal machinery that keeps the FFFI message stream and register state consistent when blocks are collapsed, closed, or otherwise skipped.

## 13.1 The Global Register File

The Rust interpreter maintains a set of **global registers** that act as staging areas between push operations and consuming widgets. They are **not** scoped to any block — all widgets in a frame share the same register file.

| Register | Rust Field | Pushed By | Drained By |
|:---|:---|:---|:---|
| **Atoms** | `r0_atoms` | `PushRichText`, `PushRichTextColored`, `Atoms().Text(...)` | Widgets with `structAtoms()` arg: `Button`, `LabelAtoms`, `MenuButton`, `RadioButton` — via `std::mem::take(&mut self.r0_atoms)` |
| **WidgetText** | `r1_widget_text` | `WidgetText` apply code (drains `r0_atoms` into it), `WidgetText().Text(...)` | Widgets with `structWidgetText()` arg: `Label`, `CollapsingHeader`, `Window`, `ComboBox`, `TableCellRichText` — via `std::mem::take(&mut self.r1_widget_text)` |
| **Color32** | `r11_color32` | `Color()` evaluated arg (legacy) / synthesised inline by `components.PutColorAsRetainedColor32` for `color.Color` literal variants (ADR-0003) | Widgets with `.AsColor()`-annotated `EvaluatedArg(Color32)`: `Frame.Fill/Stroke/Shadow`, `ProgressBar.Fill`, `Atoms.RichTextColored`, `CodeViewJob.Section` |
| **Table columns** | `table_columns` | `TableColumn` | `Table` drain node |
| **Table headers** | `table_header_texts` | `TableHeaderText` | `Table` drain node |
| **Table cells** | `table_cells` | `TableCellText`, `TableCellRichText` | `Table` drain node |
| **Node commands** | `r3_node_cmds` | `NodeDir`, `NodeLeaf` | `Tree` drain node |
| **Plot elements** | plot registers | `PlotLine`, `PlotBars`, etc. | `Plot` drain node |

### Register Lifecycle Within a Frame

1. **Push**: A registered or evaluated node writes data into the register.
2. **Drain**: A consuming widget or drain node calls `std::mem::take` (evaluated args) or `.drain(..)` (registered nodes) to consume the data.
3. **Safety net**: `prepare_next_frame()` clears any non-empty registers at frame boundaries and logs a debug warning. This catches programming errors where a push has no matching drain.

## 13.2 Evaluated Arguments: Always Consumed

Evaluated arguments (`structAtoms()`, `structWidgetText()`, `structColor32()`) are embedded inside their parent widget's message via `SpliceRetained`. On the Rust side, the parent widget calls `interpret_inner` to evaluate the argument and then immediately calls `std::mem::take` on the target register. This sequence runs **unconditionally** — before any `u.is_some()` check:

```rust
// Button handler (interpreter.rs)
let atoms = {
    let (f2, _) = self.read_from_repr(FuncProcId::from_repr)...;
    self.interpret_inner(c, u, &f2, d+1);   // evaluates Atoms opcode
    std::mem::take(&mut self.r0_atoms)       // ALWAYS drains register
};
let mut w = egui::Button::new(atoms);
// ... builder methods ...
self.apply_widget(w, u, f, Some(i));         // skips if u is None
```

**Consequence**: Even when a widget is culled (`u` is `None`), its evaluated arguments are read from the stream and the registers are drained. The widget is constructed but `apply_widget` returns `None` without rendering. The register is left clean.

## 13.3 Culling on the Rust Side

### Widgets

`apply_widget(w, u, f, i)` checks `u.is_some()`:
- **`Some(ui)`**: Renders the widget, captures response flags.
- **`None`**: Logs `"late culled widget"`, returns `None`. No rendering, but the widget's arguments and builder methods have already been consumed from the stream.

### Blocks: Two Distinct Culling Scenarios

**Scenario A — Block decides to cull its own children** (e.g., Window closed, CollapsingHeader collapsed):

The block's egui call returns a result indicating the body was not shown. The block then calls `interpret_outer(ctx, &mut None)` to drain all child messages from the stream without rendering them. Example from Window:

```rust
if retr.is_none() {
    // Window closed
    resp2.insert(ResponseFlags::BLOCK_SKIPPED);
    self.interpret_outer(ctx, &mut None);   // drain children
}
```

All child opcodes are processed with `u=None`. Their evaluated args drain registers normally. Their `apply_widget` calls skip rendering. The `End` sentinel terminates the drain loop.

**Scenario B — Block is already inside a culled context** (`u` is already `None`):

Most blocks guard their body with `if u.is_some()`:
```rust
// Horizontal (and Frame, ScrollArea, ComboBox, CollapsingHeader, etc.)
if u.is_some() {
    u.as_mut().unwrap().horizontal(|ui| {
        self.interpret_outer(c, &mut Some(ui));
    });
}
// No else branch — children not consumed here
```

When `u` is already `None`, the block **does not call `interpret_outer`** for its children. However, this is safe because these blocks can only be reached in this state when a **parent block** is running a drain loop via `interpret_outer(ctx, &mut None)`. The parent's loop continues to the next message in the stream, which picks up the orphaned children and processes them with `u=None`. Since registers are global, the drain semantics are preserved — just at the wrong nesting depth, which doesn't matter for register operations.

### Table Drain Node: The Cleanup Reference Pattern

The `Table` drain node demonstrates explicit register cleanup under culling:
```rust
if u.is_some() {
    // ... render table using table_columns, table_header_texts, table_cells ...
} else {
    self.table_columns.clear();
    self.table_header_texts.clear();
    self.table_cells.clear();
}
```

New registered-node patterns (like Plot, Tree) should follow this: **always clear registers in the else branch** of the drain node.

## 13.4 Go-Side Yield Always — Rust Drains When Collapsed (ADR-0012)

Block iterators **always yield**. Rust drains body opcodes when egui says the block is collapsed; the previous-frame `BLOCK_SKIPPED` flag is now an **advisory** signal for app-level perf decisions, not a structural gate.

```go
func (inst WindowFluid) KeepIter() iter.Seq[...] {
    inst.r.WriteOpCode(uint32(WindowMethodIdBuild))
    r := inst.r.BuildRetained()
    return func(yield func(...) bool) {
        defer func() { inst.idGen.PopIdFromStackChecked(inst.id) }()
        r.SyncRetained()                    // send block header to Rust
        defer func() { End() }()            // always send End sentinel
        yield(...)                          // user code runs, emits children
    }
}
```

**Why the gate was removed.** It read the previous frame's `BLOCK_SKIPPED` and decided whether to emit body opcodes this frame. On a closed→open click, Rust toggled egui's open state in the same frame the click landed but Go had already chosen *not* to emit body content (gate read frame N-1 = skipped). Result: a one-frame "open header with empty body" flicker per level of nested collapsible. See ADR-0012.

**Why this is now safe (framing).** Every Rust block apply code in [`egui2_definition_d_blocks.go`](../../../public/thestack/imzero2/egui2/definition/egui2_definition_d_blocks.go) carries an explicit `else { interpret_outer(c, &mut None)?; }` arm. Each block drains its own body and `End` sentinel when its `u` is `None` (parent culling). A collapsed parent's drain therefore reads a balanced stream and terminates only on its own `End` — Scenario B's "leak nested End to the parent" failure mode is eliminated by construction.

### Frame Sequence for a Window Transitioning Open → Closed

| Frame | Go Side | Rust Side | Register State |
|:---|:---|:---|:---|
| **N** (open) | Yields. Emits children. | Window open. Renders children with `u=Some(ui)`. Response: no skip. | Atoms pushed and rendered. |
| **N+1** (user closes) | Yields. Emits children. | Window closed (animation may still call closure during animation). If body not rendered, drains via `interpret_outer(ctx, &mut None)` and sets `BLOCK_SKIPPED`. | Atoms pushed; drained or rendered depending on animation phase. |
| **N+2** (steady closed) | Yields. Emits children. | Window closed. show() does not call closure; Rust calls `interpret_outer(ctx, &mut None)` which recursively drains every block (each block's else-arm drains its own body + End). Response: `BLOCK_SKIPPED`. | Atoms pushed and drained (not rendered). |

**Cost characterization.** Bodies emit every frame, regardless of collapse state. For a tree of N nested collapsibles, the wire carries N body emissions per frame in steady-state-collapsed; Rust drains them. For tiny bodies (a few labels) the cost is unmeasurable. For heavy bodies (treemap layout, walkers tile fetch, force-layout graphs) callers should opt into app-level skipping:

```go
ch := c.CollapsingHeader(idCreator, label)
handle := ch.Handle()
for range ch.KeepIter() {
    if c.IsBlockSkipped(handle) {
        continue       // advisory; carries one-frame lag per ADR-0012
    }
    // heavy body
}
```

This is the same kind of skip the gate used to do, but **opt-in at the application level** rather than imposed on every block iterator. App-level skip reintroduces the one-frame click-to-open lag for the skipped portion, which is acceptable when the heavy body would otherwise fire every frame. ADR-0012 Phase 2 (retained bodies) is the long-term replacement for both.

## 13.5 PushRichText Safety Invariants

The `PushRichText` → consumer coupling is safe because of three reinforcing mechanisms:

1. **`Display()` is atomic**: `PushRichText("x").Display()` expands to `Send()` + `LabelAtoms(atomsKept).Send()`. Both messages are emitted together — no split possible.

2. **`KeepIter()` skips all-or-nothing**: When a block is skipped on the Go side, the entire `for range` body is not executed. Both the push and the consumer are omitted together.

3. **`prepare_next_frame()` safety net**: If atoms leak (Go-side programming error — push without consumer), they are cleared at frame boundary with a debug log. This prevents cross-frame register corruption.

## 13.6 Message Framing and Stream Recovery

Each FFFI message is length-prefixed. `end_consume_message()` validates that the handler consumed exactly the expected bytes:
- **Under-consumed**: Skips remaining bytes (logs warning). Prevents stream corruption from incomplete handlers.
- **Over-consumed**: Panics. Prevents silent data corruption.

This framing is per-message, not per-block-hierarchy. Block children are separate top-level messages in the `interpret_outer` loop, not nested inside the parent's message frame.

## 14. Screenshot & Visual Testing Infrastructure

### 14.1 FFFI2 Commands

Three procedural commands support programmatic screenshot capture:

| Go API | Purpose |
|---|---|
| `c.RequestScreenshot("/path/to/file.png")` | Captures the current frame as PNG. Uses `ViewportCommand::Screenshot` with the path carried via `UserData` round-trip (1-frame delay). |
| `c.MoveWindowToTop(ids.PrepareStr("name"))` | Brings a window to the foreground via `ctx.move_to_top(LayerId)`. |
| `c.SetWindowCollapsed(ids.PrepareStr("name"), true/false)` | Programmatically collapses/expands a window by manipulating its `CollapsingState`. |

**Timing**: `RequestScreenshot` is async. The request is sent on frame N, eframe captures `glReadPixels` after painting frame N, and the `Event::Screenshot` arrives on frame N+1. The Rust-side `handle_screenshot_event()` extracts the path from `UserData` and writes the PNG. No register or interpreter state is needed.

**ID re-preparation**: `SetWindowCollapsed` and `MoveWindowToTop` both consume the ID via `Derive()`. To call both on the same window, re-prepare the ID between calls:
```go
ids.PrepareStr("mywindow")
c.SetWindowCollapsed(ids, false)
ids.PrepareStr("mywindow")   // re-prepare
c.MoveWindowToTop(ids)
```

### 14.2 Screenshot Tour

Set `IMZERO2_SCREENSHOT_DIR` to a directory path to capture every demo window in isolation:

```bash
IMZERO2_SCREENSHOT_DIR=/tmp/screenshots bash hmi.sh
```

Produces: `tables.png`, `plots.png`, `painter.png`, `treemap.png`, `sql.png`, `i18n.png`, `nerdfont.png`, `imzero2.png`, `debug_tools.png`, `colors_styling.png`.

The tour uses a 4-phase state machine per window (setup → settle → capture → advance) to ensure layout is stable before each screenshot. The settle frame is necessary because collapse/uncollapse commands take effect on the next frame.

### 14.3 PaintCubicBezier

A cubic bezier curve paint command bound to egui's native `CubicBezierShape`:

```go
c.PaintCubicBezier(startX, startY, cp1x, cp1y, cp2x, cp2y, endX, endY, strokeRgba, strokeWidth).Send()
```

All coordinates are canvas-relative (translated to screen coords at render time by PaintCanvas). The bezier is rendered via `painter.add(CubicBezierShape)` for hardware-accelerated tessellation.

## 15. Binding an External egui Widget Library

Distilled from `egui_dock`, `egui_table`, `egui_plot`, and `egui_graphs`. When the widget you're wrapping isn't just a leaf painter but brings its own state (layout positions, selection, scroll, drag offsets) or its own callback-driven API, these patterns recur.

### 15.1 State location — pick the right bucket

| Bucket | When | Examples |
|---|---|---|
| **Per-frame register** — `Vec<FooData>` on the interpreter, cleared in `prepare_next_frame()` | Pure accumulators. The drain widget consumes all of it and the buffer is empty at frame end. | `plot_lines`, `table_cells`, `graph_pending_nodes` |
| **Retained HashMap keyed by widget id** — `HashMap<u64, State>` on the interpreter, *not* cleared per-frame | Library owns state that must survive frames (positions, selection, collapse, scroll) | `dock_states` (egui_dock layout), `graph_states` (egui_graphs graph + node/edge index maps) |
| **Local scope in apply code** — `RefCell`, channel, `&mut Vec`, closure captures | Only needed for the duration of this frame's `ui.add_sized(…)` call | FR event-sink `frame_events`, `EtPrefetchInfo` visible-range probes |

The retained-HashMap pattern is **the** way to bind a stateful library. Go is authoritative about which entities *exist*; the library is authoritative about each entity's layout / position / selection. Every frame Go re-declares the topology, the apply code reconciles (remove-missing + add-new + update-in-place), and the library continues with the same state slot untouched by the reconciliation.

For reconciliation performance, keep a bidirectional map `HashMap<u64_go_key, library_index>` inside the state struct so reconcile and edge/child lookups stay O(1) — scanning the library's container every frame is a trap.

### 15.2 Multi-instance safety — always thread a unique id in

Most egui libraries expose `.with_id(Option<String>)` / `.id_source(...)` on their widget builder. **Two instances without a unique id silently share internal state** (pan/zoom transform, selection, drag, animation). Symptoms: the second widget clusters all its nodes at (0, 0), or has an inexplicably offset pan, or loses its selection when the first widget is interacted with.

Rule: in the apply code, pass the FFFI2 widget gid through:
```rust
.with_id(Some(gid.to_string()))
```
where `gid = {{Id}}.value()`. The short gid is already globally unique and non-zero — don't compose `"prefix-{gid}"`, it just allocates without adding anything.

If the library only takes an `egui::Id`, use `egui::Id::new(gid)` directly — avoids the `to_string()` allocation entirely.

### 15.3 Canvas sizing — fill by default, fixed on opt-in

`ui.add_sized(vec2(w, h), &mut view)` with hard-coded pixel values *does not flow with window resizes* — egui gives you exactly the rect you asked for. For canvas-shaped widgets (graphs, plots, code views) the natural default is "fill available space along both axes".

Pattern: accept `.Width(f32)` / `.Height(f32)` builder methods, default to `0.0`, interpret zero as "use `ui.available_size()` for that axis":
```rust
let avail = ui.available_size();
let size = egui::vec2(
    if gv_width  > 0.0 { gv_width  } else { avail.x },
    if gv_height > 0.0 { gv_height } else { avail.y },
);
```
Users who want a fixed pixel size call `.Width(500)`; everyone else gets flow-with-container for free.

### 15.4 Runtime-switchable library generics — dispatch with `macro_rules!`

egui libraries often pin non-trivial generics at the widget type (`GraphView<…, LayoutState, Layout>`, `TableBuilder<…, Delegate>`, etc.). When the user wants to pick the variant at runtime, you get one match arm per variant, each otherwise identical. Rather than copy the 8-line `GraphView::new().with_id().with_interactions()…ui.add_sized()` block four times, use a `macro_rules!` local to the render function:

```rust
macro_rules! render_variant {
    ($S:ty, $L:ty) => {{
        let mut view: egui_graphs::GraphView<'_, …, $S, $L>
            = egui_graphs::GraphView::new(&mut state.graph)
                .with_id(id.clone())
                .with_interactions(interaction)
                /* …all shared builder methods… */;
        ui.add_sized(size, &mut view);
    }};
}
match layout_kind {
    GRAPH_LAYOUT_FORCE_DIRECTED => render_variant!(FruchtermanReingoldState, LayoutForceDirected<FruchtermanReingold>),
    /* etc. */
}
```
The type parameters are the only thing that varies; every other knob stays in one place. Generic helper *functions* don't work here because calls like `GraphView::<…>::fast_forward(...)` need the full generic list at the call site — a macro splices it cleanly.

Switching variant at runtime discards the prior variant's state (different state types occupy the same egui id slot); document that or fire a reset on switch.

### 15.5 Callback / event streams — scoped sink + push to register

Libraries with event subscriptions typically accept `dyn Trait` or `crossbeam::channel::Sender<Event>`. Neither plays well with `&mut self.interpreter` borrows. The pattern that works without contortion:

```rust
// Scoped to one frame's render call.
let frame_events: std::cell::RefCell<Vec<egui_graphs::events::Event>> =
    std::cell::RefCell::new(Vec::new());
let sink = |e: egui_graphs::events::Event| {
    frame_events.borrow_mut().push(e);
};
{
    let mut view = /* build widget */ .with_event_sink(&sink);
    ui.add_sized(size, &mut view);
}                                                // view dropped → sink borrow released
for e in frame_events.into_inner() {
    /* translate + push to self.graph_events_pending */
}
```

The closure borrows the `RefCell` — local to the apply code. After `view` drops, the borrow is released and we own the Vec. Then translate library-internal ids (petgraph NodeIndex, egui_table CellInfo) into Go-side u64 keys via the retained state and push to a global pending register for a fetcher to drain.

### 15.6 Optional-tunable builder methods — twin `_set` flags

When exposing a library state field that's typically owned by the simulation (FR damping, hierarchical row_dist, etc.) but the Go side wants to *occasionally* override it from a slider, a per-frame "set-if-called" pattern keeps the simulation's running bookkeeping intact. Each builder method flips two locals:

```go
BeginMethod("layoutDamping").Arg("dp", ctabb.F32).
    CodeClientRust(rustClientCode("fr_damping = dp; fr_damping_set = true;\n")).EndMethod().
```

Defaults: `let mut fr_damping = 0.0; let mut fr_damping_set = false;` — the value only matters when the flag is true. Apply code loads the library's current state, overlays only the touched fields, writes back:

```rust
if any_set {
    let mut s = egui_graphs::get_layout_state::<S>(ui, id.clone());
    if fr_damping_set { s.damping = fr_damping; }
    /* …other fields… */
    egui_graphs::set_layout_state(ui, s, id);
}
```

This avoids sentinel-value fragility (`0.0` meaning "unset"?) and preserves any simulation-managed fields we don't expose (`step_count`, `last_avg_displacement`).

### 15.7 IDL pitfall — backticks inside Go raw strings

`rustClientCode(\`…\`)` uses a Go raw-string literal. A backtick character anywhere inside — even in a code-style comment like `` `\`_set\`` flag`` `` — terminates the raw string early and the Rust snippet fragments into broken Go. Symptom: a `syntax error: unexpected name <rust_local> in argument list` during `go build` of the `definition` package, pointing at what *looks* like valid Rust. Fix: never put backticks inside the rust-code raw string; use plain quotes, asterisks, or nothing.

### 15.8 Verification shortcut

- Add a short-lived `tracing::info!(…)` inside the drain/apply for the duration of bring-up (node count, pending lengths, edges added this frame). Remove before committing.
- For visual checks, run `IMZERO2_SCREENSHOT_DIR=/tmp/verify bash rust/imzero2/hmi.sh` and inspect `<window>.png`. Beware the 4-frame tour isn't enough for libraries that do first-frame geometry (force-directed convergence, etc.) — the screenshot captures an un-settled state; that doesn't mean the binding is broken.
- A temporary revisit entry in `demoWindows` (same handle, different filename) lets the tour close and re-open a window to verify state persistence across hide/show cycles.
## 16. walkers map + H3 overlays — binding limitations & gotchas

Companion notes for the bindings in [`egui2_definition_d_walkers.go`](../../../public/thestack/imzero2/egui2/definition/egui2_definition_d_walkers.go) and the Rust glue in [`interpreter.rs`](../../../rust/imzero2/src/imzero2/interpreter.rs). Covers what works, what doesn't, and the shapes that cross the FFI. See ADR-0007 for the design rationale.

### 16.1 API surface, in one page

| What | IDL node | Shape |
|---|---|---|
| Basemap | `walkersMap` (plain widget) | `(id, initLat, initLon, noTiles) + .Width/.Height/.SetZoom/.CenterAt/.ZoomGesture/.Panning/.TileUrl/.TileAttribution/.TileMaxZoom/.TileSize` |
| Point marker | `mapMarker` (register-drain) | `(markerId, lat, lon) + .Label/.Color/.Radius` |
| Polyline / closed ring | `mapPolyline` (register-drain) | `(lats[], lons[]) + .Stroke/.Closed` |
| Bulk choropleth | `h3CellsColored` (register-drain) | `(cellIds[], rgbas[]) + .StrokeWidth/.StrokeColor` |
| Aggregated ROI outline | `h3Region` (register-drain) | `(cellIds[]) + .Fill/.Stroke/.Label` |
| Viewport / pointer read-back | `fetchR15WalkersCamera` (fetcher) | `(found, mapId, zoom, center{Lat,Lon}, {min,max}{Lat,Lon}, screen{Width,Height}Px, hover{Lat,Lon,Valid}, clicked, viewHash)` |

Two emission patterns trip up new walkers code routinely — read §16.2 (overlay ordering) and §16.3 (sticky `SetZoom`/`CenterAt` semantics) before writing or modifying a walkers demo. The remaining subsections are reference material for narrower gotchas.

### 16.2 Overlays must be emitted before the map opcode in the same frame

* **Symptom.** `MapMarker` / `MapPolyline` / `H3CellsColored` / `H3Region` calls don't render on the map. Rust logs `walkers pending overlays leaked` and the registers are cleared at frame end.
* **Cause.** Overlays are a register-drain pattern (see §13.1 "The Global Register File"). Each overlay opcode pushes into a per-frame `Vec<…>` on the interpreter; the next `WalkersMap.Send()` in the same frame `std::mem::take`s and consumes it. Overlays emitted *after* the map have no consumer this frame — `prepare_next_frame()` clears the leaked registers and logs.
* **Pattern.** Always emit overlays before the map opcode:

```go
// WRONG — overlays orphaned, never rendered
c.WalkersMap(ids.PrepareStr("m"), lat, lon, false).Width(w).Height(h).Send()
c.MapMarker(1, lat, lon).Color(color.Hex(0xff0000ff)).Send()
c.MapPolyline(lats, lons).Stroke(color.Hex(0xffffffff), 2).Send()

// RIGHT — overlays drain into the map render
c.MapMarker(1, lat, lon).Color(color.Hex(0xff0000ff)).Send()
c.MapPolyline(lats, lons).Stroke(color.Hex(0xffffffff), 2).Send()
c.WalkersMap(ids.PrepareStr("m"), lat, lon, false).Width(w).Height(h).Send()
```

If overlays must be computed from the visible viewport (e.g. a viewport-driven heatmap), emit them from the **previous** frame's camera via `StateManager.GetWalkersCamera()` and accept the one-frame lag — imperceptible at interactive cadence. The camera cannot be fetched inline during render; see `doc/skills/imzero2-fetchers/SKILL.md` for the deadlock rationale and §16.5 below for the multi-map ambiguity.

### 16.3 `SetZoom` and `CenterAt` are sticky for one frame — gate them on an apply-once flag

* **Symptom.** After the app computes a new view (e.g. "fit the two clicked points", "recentre on selection") the user can no longer pan or zoom the map interactively. Every drag snaps back to the computed position on the next frame; the zoom slider has no visible effect.
* **Cause.** `WalkersMap.SetZoom(z)` and `WalkersMap.CenterAt(lat, lon)` set `override_zoom` / `override_center` arguments on the walkers widget. On the Rust side these are **sticky for exactly one frame**: walkers applies the override to its internal `MapMemory`, then clears it. User pan/zoom writes into the same `MapMemory` between frames. If Go code calls `SetZoom` / `CenterAt` unconditionally every frame, the user's interactive state is overwritten on every render before walkers gets a chance to honour it.
* **Pattern.** Gate the calls on a boolean that flips on a discrete event and clears after one use:

```go
type st struct {
    overrideZoom   float64
    applyZoom      bool
    overrideCenter [2]float64
    applyCenter    bool
}

// On the event that should retarget the view (e.g. user finishes selection):
st.overrideZoom = 12.0
st.applyZoom = true
st.overrideCenter = [2]float64{midLat, midLon}
st.applyCenter = true

// In the render body:
mw := c.WalkersMap(ids.PrepareStr("m"), initLat, initLon, false).
    Width(w).Height(h)
if st.applyZoom {
    mw = mw.SetZoom(st.overrideZoom)
    st.applyZoom = false
}
if st.applyCenter {
    mw = mw.CenterAt(st.overrideCenter[0], st.overrideCenter[1])
    st.applyCenter = false
}
mw.Send()
```

The canonical implementation lives in `egui2_hl_walkers_demo.go` (search for `applyZoom`). The deliberate opposite case — driving a non-interactive secondary map from a primary's camera *every frame* — is described in §16.4; outside that pattern, an unconditional per-frame `SetZoom` / `CenterAt` is almost always a bug.

### 16.4 Ctrl+Wheel zooms all visible walkers maps at once

* **Symptom.** Multiple `walkersMap` widgets visible; Ctrl+Wheel over one of them zooms all of them.
* **Cause.** Walkers' gesture handler reads `ui.input(|i| i.zoom_delta())` once per map and gates via `ui.ui_contains_pointer()` on the parent Ui, not the map's response rect. In overlapping or stacked layouts more than one parent Ui's rect can contain the pointer, so multiple maps apply the zoom delta. This is walkers-side, not ImZero2-side.
* **Pattern.** For dashboards with multiple maps, keep zoom interactive on exactly one — call `.ZoomGesture(false)` on the others and mirror state explicitly: read the primary's camera via `StateManager.GetWalkersCamera()` and drive the secondaries with `.SetZoom(z).CenterAt(lat, lon)` every frame. (Driving every frame is appropriate here because the secondary is non-interactive by construction — for the user-interactive case, the per-frame override is a bug; see §16.3.)

### 16.5 The camera fetcher returns the last-rendered map's viewport

* **Symptom.** `fetchR15WalkersCamera` called after rendering map B reports map A's viewport (or vice versa) with multiple maps in the frame.
* **Cause.** `walkers_last_camera` is a single `Option<WalkersCamera>` written by the overlay plugin's `run()` at the end of each `walkersMap` render. The fetcher returns the last write. The fetcher does **not** take/consume — subsequent reads in the same frame see the same value.
* **Pattern.** If you want a specific map's viewport, arrange for it to be the **last** `walkersMap` rendered in the frame before fetching; or (simpler) host overlays on a single map. The uniform-heatmap demo takes the second route — it emits overlays *before* the main map's render and reads the previous frame's camera, giving a one-frame lag on pan that's imperceptible at interactive rates.

### 16.6 Antimeridian-crossing polygons are culled incorrectly

* **Symptom.** A region whose cells straddle ±180° longitude (Russia, Fiji, Aleutians, NZ dateline) disappears or renders as a world-spanning ghost.
* **Cause.** `OverlayPlugin` culls each overlay by a naive AABB in lat/lng space. For a polygon that spans the antimeridian, `min_lon` and `max_lon` invert (e.g. `-179.5` to `+179.5` becomes the whole world) and the AABB check either admits everything or nothing depending on the viewport.
* **Pattern.** Either (a) split such polygons at ±180° on the Go side before sending, or (b) skip the Go-side cull for sensitive data and rely on egui's screen-space clipping. Long-term fix is an antimeridian-aware splitter in `bbox_of_rings`; deferred until the first real dataset hits it.

### 16.7 `h3Region` fill paints per-cell hexes, not a single tessellated polygon

* **Symptom.** At low zoom levels with large cell counts (thousands of cells in a country-scale ROI), you can see the hex grid inside the filled area; fill performance drops roughly linearly with cell count.
* **Cause.** `h3Region.Fill` is implemented by drawing each cell as a `egui::Shape::convex_polygon`. Concave tessellation of the dissolved outline would require `lyon_tessellation` (not yet a dep). Per-cell fill is honest at H3-native scales (hundreds of cells) and visually reveals the H3 grid — often desired.
* **Pattern.** For country-scale ROIs where you want a smooth fill, use `h3Region.Stroke(...)` only (omit `.Fill(...)`) and let the dissolved outline do the work. Or compact the cellset to the coarsest resolution that still bounds your area.

### 16.8 Custom tile servers

* **`.TileUrl(template)`** — XYZ template with `{z}`, `{x}`, `{y}` placeholders. Empty (default) uses walkers' built-in `OpenStreetMap`.
* **No `{s}` subdomain rotation.** Replace `{s}` with a concrete subdomain (`a`, `b`, `c`) before passing the URL. This is a deliberate v1 cut; revisit if rate-limiting becomes a real problem.
* **Change detection.** The Rust side hashes `(url, attribution, size, maxZoom, noTiles)` per map id. When the hash changes, `HttpTiles` is rebuilt in place; `MapMemory` (pan/zoom) survives. Logged at `INFO`.
* **Attribution leaks once.** `walkers::sources::Attribution` requires `&'static str`. The custom-source bridge `Box::leak`s the user-supplied string **once at construction time**, not per `attribution()` call. Bounded growth in practice (one leak per unique attribution seen during the process lifetime).

### 16.9 `walkers::Position` coordinate order is `(lng, lat)` in constructors, not `(lat, lng)`

* **Symptom.** Points render on the wrong hemisphere or meridian.
* **Cause.** `walkers::Position` is a type alias for `geo_types::Point<f64>`, where `x=longitude, y=latitude`. Constructors: `walkers::lon_lat(lon, lat)` and `walkers::lat_lon(lat, lon)`. Accessors: `.lng()` and `.lat()` — **not** `.lon()` (does not exist on `geo_types::Point`).
* **Pattern.** Always use the named constructors (`walkers::lon_lat`/`walkers::lat_lon`) and accessors (`.lat()`/`.lng()`). Never poke `.x`/`.y` unless converting from a flat `Vec2`.

### 16.10 `Projector::project`/`unproject` take and return absolute viewport coordinates

* **Symptom.** Overlays drift relative to tiles when zooming — millimetres at low zoom, kilometres at high zoom (Mercator scaling amplifies the error).
* **Cause.** Walkers' `Projector::project(Position) -> Vec2` returns **absolute** viewport-space pixels — it internally adds `clip_rect.center()`. Same for `unproject(Vec2) -> Position`, which expects absolute pixels and subtracts `clip_center`. Adding `rect.center()` to `project`'s output (or subtracting it from `unproject`'s input) double-counts the center and the error grows with zoom.
* **Pattern.** Convert directly: `projector.project(pos).to_pos2()` and `projector.unproject(screen_pos.to_vec2())`. Do not add/subtract any `rect.center()`. The fix is baked into `OverlayPlugin`; mentioned here so plugin authors extending the binding don't re-introduce it.

### 16.11 h3o-wazero initialization and handle reuse

* **Pattern.** The demo boots a single `h3.Runtime` with `PoolSize: 1` via `sync.Once` and checks out exactly one `Handle` that lives for the process. ImZero2's Go side is single-threaded (see pitfall §12 "Framework Data Race"), so a single handle is safe across frames and amortises wazero's scratch allocation.
* **Lazy init, do not panic.** `ensureH3()` returns an error instead of panicking; UI code that needs H3 should render a graceful fallback label (`"h3 runtime not ready"`) and skip the overlay. Burst-starting the runtime on frame 0 adds ~10–50 ms of wasm compile time — acceptable, but don't do it synchronously inside a hot render path if you care about first-paint latency.
* **Resolution choice.** The demo uses the heuristic `h3ResForZoom(zoom) = clamp(round(zoom/2 - 1), 1, 12)`. Works for a rough "cells scale with view" mapping; real apps should tune (or precompute per-resolution cell sets and pick based on data size, not zoom).

### 16.12 Tokio + reqwest pulled in by walkers

* **Footprint.** Walkers uses `reqwest` (rustls) + `tokio` (native only) for tile fetches. Binary grows by ~5 MB. Tokio is a direct dep on native; wasm build path omits it.
* **Thread safety.** Walkers' tile fetches spawn tokio tasks; they only touch `egui::Context`'s repaint channel (itself `Send + Sync`). The ImZero2 single-thread rule for the Go-facing `c.*` API is unaffected.

### 16.13 Resolution of common errors

| Log line | Likely cause | Fix |
|---|---|---|
| `late culled walkers map` | Parent block (window/collapsing header) was skipped this frame | Move the `walkersMap` out of a collapsing header, or use `.DefaultOpen(true)` and a wrapper that survives the 4-frame screenshot tour (see §12 CollapsingHeader pitfall) |
| `walkers pending overlays leaked` | Register-drain overlays sent without a following `walkersMap` to drain them | Ensure overlay calls happen *before* the `walkersMap` opcode, in the same frame (see §16.2) |
| `walkers tile config changed — rebuilt HttpTiles` (repeated) | Tile config signature changing every frame | Likely inadvertent (changing URL / attribution / zoom / size in a tight loop). Pin the config to a Go-side variable and only update on real user input |
| `h3 runtime init failed` | h3o-wasm artifact missing or wazero compile error | Verify boxer's `public/science/geo/h3/internal/h3o_wasm` has the built artifact (`.wasm` file); rebuild boxer if stale |

### 16.14 Known limitations — longer-term work

- **Bug 2** (§16.4) — upstream walkers issue; needs a tighter `response.rect.contains(pointer_pos)` gate in walkers' own gesture handler.
- **Antimeridian culling** (§16.6) — wait for first real dataset to hit this; then add splitter in `bbox_of_rings`.
- **Concave tessellation** (§16.7) — add `lyon_tessellation` dep only when a real ROI workflow demands smooth fills at country scale.
- **`{s}` subdomain rotation** (§16.8) — add a `.TileSubdomains([]string)` method if public-tile rate-limiting becomes visible.
- **`.TileAttributionUrl(url)`** — not exposed; walkers' attribution widget supports it but current binding ignores the URL slot.
- **Per-id camera snapshots** (§16.5) — current single `walkers_last_camera` is ambiguous in multi-map frames. Could key by map id at the cost of a small HashMap per frame.
- **Mapbox/Geoportal presets** — users can pass the right URL template with `.TileUrl` today; a named preset would save typing but adds little beyond that.
- **Interactive ROI drawing** — v1 is display-only. Brush / click-to-draw / vertex-drag edit modes are scoped in the design but not implemented. Go owns drawing state by design (no Rust-side draw-tool state).

## 17. Badge / Chip — high-level Frame composition

Companion to [`widgets/badge/badge.go`](../../../public/thestack/imzero2/egui2/widgets/badge/badge.go). Badge is a pure Go composition over existing FFFI2 primitives — `Frame.Fill / Stroke / CornerRadius / InnerMarginSides` wrapping an `Atoms.RichTextColored` `LabelAtoms`. **No IDL or Rust changes** were added to ship it; the wire format is identical to a hand-written Frame+LabelAtoms pair.

Treat it as the canonical recipe for "rounded, padded, coloured text pill" — anywhere you would otherwise reach for a one-off `c.Frame(…).Fill(…).CornerRadius(…)…` block, prefer `badge.New(id, label).Tone(…).Variant(…)` and pick up tone/variant/size/icon/pill/tooltip handling for free.

### 17.1 API at a glance

| Knob                         | Effect |
|------------------------------|--------|
| `Tone(badge.ToneE)`          | Semantic colour family — Neutral / Primary / Success / Warning / Error / Info |
| `Variant(badge.VariantE)`    | Solid (filled) / Soft (translucent) / Outline (border) / Ghost (text-only) |
| `Size(badge.SizeE)`          | Sm / Md / Lg — adjusts inner padding, corner radius, font scale |
| `Icon(string)`               | Prefixes label with a glyph (typically a `nf.*` NerdFont rune) |
| `Selected(bool)`             | Forces "pressed" look (Solid + tone stroke) regardless of Variant — pair with `SendResp` for filter chips |
| `Strong()` / `Monospace()`   | Bold / monospace label. Monospace is recommended for numeric counts. |
| `Pill()`                     | Forces full-rounding (corner = 100 px, egui clamps to half-height). Use for notification dots / status pills. |
| `Tooltip(string)`            | Wraps the chip in a `HoverText` scope — shows on hover via §12 overlay-interact pattern. |
| `Send()` / `SendResp()`      | Display-only / interactive (threads `SenseClick`, returns `ResponseFlagsE`). |

### 17.2 v1 keeps Badge atomic — close-X is a separate Badge

Dismissible chips (`go ×`) are **not** a built-in. Compose them as two adjacent badges inside a `Horizontal` + `IdScope`:

```go
for range c.IdScope(ids.PrepareSeq(uint64(i))) {
    badge.New(ids.PrepareStr("tag"),  tag).Tone(badge.TonePrimary).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
    if badge.New(ids.PrepareStr("close"), nf.CodClose).
        Tone(badge.TonePrimary).Variant(badge.VariantGhost).Size(badge.SizeSm).
        SendResp().HasPrimaryClicked() {
        removeAt = i
    }
}
```

Why two badges instead of a built-in: the close affordance needs its own widget id for the response, and the colours / variant of the close are application-specific (some chips want a ghost ×, others a solid coloured one). Keeping the primitive minimal lets the compositions stay readable.

### 17.3 Tone palette is hard-coded for the dark theme

`badgeTonePalette` resolves each tone to four constants — base / soft / fgOnSolid / fgOnSoft — picked from the Tailwind 500/600 family for legibility against egui's default dark background. Light-theme support would mean reading from `egui::Visuals` instead; deferred until that's a concrete need.

### 17.4 Pill = `corner = 100.0`, not size-dependent

`.Pill()` ignores the size-derived corner and writes 100.0 directly. egui's `CornerRadius` clamps internally to fit the rect, so the result is always a fully-rounded chip regardless of label width or `Size(...)`. Don't try to compute "half height" yourself — Go doesn't know the rendered height.
