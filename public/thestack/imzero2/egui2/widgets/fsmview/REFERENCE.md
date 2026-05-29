---
type: reference
audience: package consumer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Public API surfaces and field shapes may change. Pin a commit if you depend on them.

# widgets/fsmview — public API reference

Two-level finite-state-machine viewer for the ImZero2 framework. Level 1 is a compact chip showing the current state; level 2 is a click-to-pin floating popup with Table / Graph (egui_graphs FR+CG) / History views. Couples tightly to [`hishamk/statetrooper`](https://pkg.go.dev/github.com/hishamk/statetrooper) through a thin `Machine[T comparable]` wrapper — see [ADR-0045](../../../../../../../../doc/adr/0045-imzero2-fsmview-widget.md) for the design rationale and milestone plan.

## Types

| Type | Role |
|---|---|
| [`Machine[T comparable]`](./machine.go) | Visualization-aware wrapper around `*statetrooper.FSM[T]`. Owns rule mirror, display labels, edge labels, per-state colours, stable node ids. |
| [`Widget[T comparable]`](./widget.go) | The two-level composite IM widget. Holds popup-open / selected-renderer / graph-prewarm state on the receiver pointer. |
| [`Option[T comparable]`](./machine.go) | Construction-time builder for [`Machine`]. Each option mutates the in-progress machine; later options win. |
| [`StateColorFn[T comparable]`](./machine.go) | `func(state T, isCurrent bool) styletokens.RGBA8`. Hook for [`WithStateColor`] — domain-specific per-state colouring. |
| [`EdgeKey[T comparable]`](./machine.go) | `struct { From, To T }`. Comparable map key over arbitrary state types. Used by [`Machine.Edges`] iteration. |
| [`Transition[T comparable]`](./machine.go) | A recorded state change `{From, To, At, Metadata}`. Defined locally so the public API does not leak `statetrooper.Transition`. |
| [`RendererE`](./widget.go) | `uint8` enum: `RendererTable` (default), `RendererGraph`, `RendererHistory`. |

## Constructors

### `NewMachine[T comparable](initial T, maxHistory int, opts ...Option[T]) *Machine[T]`

Build a fresh machine. `maxHistory` caps the [`statetrooper.FSM`] transition history; pass `0` to disable history tracking (the History tab then renders "no transitions yet" indefinitely, and the [`Widget.ShowSubscript`] readout reads "").

### `New[T comparable](ids *c.WidgetIdStack, scopeKey string, m *Machine[T]) *Widget[T]`

Build a viewer bound to the given machine. Panics on nil `ids`, nil `m`, or empty `scopeKey` — these are programmer errors. `scopeKey` is folded into every emitted widget id so two viewers on the same id stack do not collide.

## `Option[T]` builders

| Option | Default | Purpose |
|---|---|---|
| [`WithLabel(fn func(T) string)`](./machine.go) | `fmt.Sprint` | Display label per state. Override for struct states. |
| [`WithStateOrder(order []T)`](./machine.go) | first-seen via `AddRule` | Pin the table-display order. Useful when alphabetic order hides the FSM's natural flow. |
| [`WithStateColor(fn StateColorFn[T])`](./machine.go) | `AccentDefault` if current, `NeutralSubtle` otherwise | Override per-state colouring. Domain-specific (error states red, terminal states muted, …). |

## `Machine[T]` methods

### Mutators (call at setup or between frames)

| Method | Notes |
|---|---|
| `AddRule(from T, to ...T) *Machine[T]` | Forwards to [`statetrooper.FSM.AddRule`] **and** mirrors the rule locally. statetrooper's `ruleset` field is unexported, so the widget cannot enumerate transitions from the FSM alone. Returns the receiver for chaining. |
| `EdgeLabel(from, to T, label string) *Machine[T]` | Attach a display label to one transition. Surfaces in the table column and as a `c.GraphEdge.Label(...)` on the graph view. Empty string clears. |
| `Transition(target T) error` | Delegates to [`statetrooper.FSM.Transition`]. Records timestamped history when `maxHistory > 0`. The widget reads the new state on the next frame via [`Current`]; no event fires. |

### Read accessors (frame-safe; the widget calls these per frame)

| Method | Returns |
|---|---|
| `Current() T` | The current FSM state. |
| `CanTransition(target T) bool` | Whether `target` is reachable in one step from the current state. |
| `Label(state T) string` | The display label, via `WithLabel` (or `fmt.Sprint` default). |
| `Color(state T) styletokens.RGBA8` | The IDS palette colour, via `WithStateColor` (or the default scheme). |
| `States() iter.Seq[T]` | Display-ordered states (pinned via `WithStateOrder`, then `AddRule` insertion order). |
| `Edges() iter.Seq2[EdgeKey[T], string]` | All declared transitions with their labels (empty string when unlabelled). |
| `History() iter.Seq[Transition[T]]` | Recorded transitions, oldest first. |
| `HistoryReverse() iter.Seq[Transition[T]]` | Recorded transitions, newest first — drives the History tab and the chip subscript. |
| `LastTransition() (Transition[T], bool)` | Most recent recorded transition, or `ok=false` for a fresh machine / `maxHistory=0`. |
| `HistoryLen() int` | Cap-bounded count of retained transitions. |
| `NodeId(state T) uint64` | Stable FNV-derived `u64` for `c.GraphNode`. Two states with identical labels collide intentionally (the operator can't tell them apart visually either). |

## `Widget[T]` methods

### Construction-time toggles

| Method | Default | Purpose |
|---|---|---|
| `Title(name string) *Widget[T]` | `scopeKey` | Human-facing FSM name shown in the level-2 popup header (`"<Name> · <CurrentState>"`) and the chip's hover-tooltip. Override when `scopeKey` is a terse id and the operator-facing label needs to read differently. |
| `ShowSubscript(on bool) *Widget[T]` | `false` | Render the "Xs ago" subscript next to the chip (sourced from [`Machine.LastTransition`] via `dustin/go-humanize`; switches to absolute UTC for transitions older than 24h). Returns the receiver for chaining. |
| `PopupAnchor(x, y float32) *Widget[T]` | unset (egui cascade) | Pin the level-2 Window's `default_pos` to `(x, y)` in egui logical pixels. Applies on the first open of a fresh widget instance; egui retains the user's dragged position thereafter. |
| `ClearPopupAnchor() *Widget[T]` | — | Revert to egui's default cascade positioning. |
| `AutoAnchor(on bool) *Widget[T]` | `false` | Capture the cursor position the frame the chip is clicked (via [`StateManager.GetPointer`] / R20) and write it into `PopupAnchor` so the popup pops where the click landed. Overrides any prior manual anchor on the click frame. |

### Per-frame state

| Method | Effect |
|---|---|
| `Render()` | Emit the chip + (when open) the popup. Call once per frame inside an active egui surface. The chip renders inline at the current cursor; embed inside a `c.Horizontal` flow or a panel. |
| `IsOpen() bool` | Whether the popup is currently open. |
| `Open()` / `Close()` | Programmatically toggle the popup. |
| `SelectedRenderer() RendererE` | Current level-2 view. |
| `SetRenderer(r RendererE)` | Pin which view shows next. |

## Affordances

| Affordance | Mechanism | Lag |
|---|---|---|
| Chip click | `widgets/badge` `.SendResp().HasPrimaryClicked()`; toggles `popupOpen`. | one frame |
| Popup title-bar X | `c.Window.OpenBound(win.Id())` + per-frame `StateManager.AddR10Databinding(win.Id(), &popupOpen)`. egui-native two-way binding (per `feedback_egui_native_affordances`). | one frame |
| Tab switch | `c.SelectableLabel(...).SendResp().HasPrimaryClicked()`. | one frame |
| Graph drag / hover / pan / zoom | `c.Graph` interaction flags. | per egui_graphs |
| Active state highlight | `defaultStateColor` returns `AccentDefault` for current, `NeutralSubtle` otherwise. Overridable via [`WithStateColor`]. | none |
| Next-possible edge highlight | Edges leaving the current state are tinted `AccentSubtle`; others tinted `NeutralBorderFaint`. | none |
| Graph pre-warm | First-frame `.ResetLayout().FastForwardSteps(200)` (tracked via per-Widget `graphPrewarmed` flag) so FR converges before the operator sees the graph. | none |

## Conventions

- **Receivers**: pointer (`*Widget[T]`) for the stateful composite; pointer (`*Machine[T]`) so callers can pass the machine to multiple widgets without copying.
- **Build tag**: `//go:build llm_generated_opus47`, matching peer widgets.
- **IDS tokens**: `AccentDefault` for the active state, `AccentSubtle` for next-possible edges, `NeutralBorderFaint` / `NeutralSubtle` / `NeutralTextSecondary` for resting affordances. No raw hex; everything goes through `styletokens.*.AsHex()` + `widgets/color.Hex` per ADR-0031 §SD2.
- **One-frame lag**: chip click, tab switch, popup-X — all reflect the previous frame's input, like every other R7/R10-backed widget in the framework.

## Example

```go
m := fsmview.NewMachine("idle", 32,
    fsmview.WithStateOrder([]string{"idle", "running", "cancelling", "cancelled", "done"}),
    fsmview.WithLabel(strings.ToUpper),
)
m.AddRule("idle", "running").
    AddRule("running", "cancelling", "done").
    AddRule("cancelling", "cancelled").
    EdgeLabel("running", "cancelling", "cancel()").
    EdgeLabel("running", "done", "complete")

view := fsmview.New(ids, "projector", m).ShowSubscript(true)

for range c.Window(...).KeepIter() {
    for range c.Horizontal().KeepIter() {
        c.Label("Projector:").Send()
        view.Render()
    }
}
```

## Limitations & milestone deferrals

- **AutoAnchor follows the cursor, not the chip's bottom-left** — the R20 pointer fetcher gives us the click position, which is *near* the chip but not its precise bottom-left corner. True bottom-left anchoring would need a widget-rect fetcher (recording every widget's `response.rect` keyed by id) — substantial Rust-side change; not yet scoped.
- **statetrooper is not hierarchical** — surfaces that need substates (nested workflows) will either fork statetrooper locally or migrate to a richer FSM lib. M4 in the ADR.
- **Per-frame egui_graphs cost** — when the Graph tab is selected, force-directed simulation runs every frame even after convergence. Acceptable at <20 nodes; promote `LayoutRunning` to a knob if a caller needs to pin a converged layout.

## See also

- [ADR-0045](../../../../../../../../doc/adr/0045-imzero2-fsmview-widget.md) — design rationale and milestone plan
- [ADR-0013](../../../../../../../../doc/adr/0013-imzero2-stateful-widget-contract.md) — composite-widget pattern (fsmview is **not** an FFFI2 primitive; state lives on the receiver)
- [ADR-0021](../../../../../../../../doc/adr/0021-imzero2-snarl-node-editor-binding.md) — egui-snarl (manual node editor; complementary, for editing FSM topology rather than visualising it)
- [ADR-0026](../../../../../../../../doc/adr/0026-app-runtime-and-capability-subjects.md) — native title-bar X via `.OpenBound`/R10 (M2 lands the same pattern here)
- [`github.com/hishamk/statetrooper`](https://pkg.go.dev/github.com/hishamk/statetrooper) — the chosen FSM library
