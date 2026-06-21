---
type: adr
status: proposed
date: 2026-04-26
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0014: ImZero2 — Context-Typed Ui for Container-Nesting Safety

## Context

ImZero2's widget surface is reached through a single Ui-shaped value (the `*c.WidgetIdStack` receiver in generated code, conventionally bound to the variable `c` at call sites). Every widget method is defined on this one type, and every widget therefore appears callable in every nesting context. Container preconditions exist — but only as runtime rules carried in CLAUDE.md and the per-frame visual-feedback loop. From a downstream consumer's local supplement:

- `AllocateUiAtRect` positions its child Ui at the parent's *absolute* coordinates and silently breaks an enclosing `Vertical` / `Horizontal` flow.
- `c.PanelCentral()` is mandatory for full-screen apps; widgets emitted outside any panel have no Ui scope (flicker + lost input).
- egui_dock 0.19's `DockArea::show_inside` greedily consumes `available_rect_before_wrap` and overrides clip rect; nesting it inside a `ScrollArea` / `Window` requires `UiSetHeight` (not `UiSetMinHeight`).
- The `RadioButton().SendResp().HasPrimaryClicked()` pattern (resolved by [ADR-0013](0013-imzero2-stateful-widget-contract.md)) was a sibling shape: API admitted a syntactically valid call whose runtime semantics were broken.

Each rule above costs a memory entry, a CLAUDE.md bullet, and discovery time during an incident. The cognitive-load survey identifies this as **driver 3 — container/layout preconditions live outside the type system** and ranks it among the highest-leverage interventions: the rules are *static*, the API enforcement is *dynamic*, and Go's type system can in principle bridge that gap. Future widget vocabulary (TUI cell renderer, custom paint contexts, dock subtrees) will introduce more rules of the same shape unless the type system carries them.

The Rust precedent — typestate via phantom type parameters or marker generics — solves this cleanly in languages with `impl Type<State>` specialisation. Go's generics admit only a partial form. This ADR captures *which* Go-shaped form to adopt and *why* a literal port of the Rust idiom does not work.

### Today's mechanism

A single Ui-shaped receiver carries every method:

```go
// Conceptual; the actual receiver is *c.WidgetIdStack with helper methods.
func (c *Ui) Label(s string)
func (c *Ui) Vertical(fn func(*Ui))
func (c *Ui) AllocateUiAtRect(r Rect, fn func(*Ui))
func (c *Ui) ScrollArea(fn func(*Ui))
func (c *Ui) DockArea(...)
func (c *Ui) PanelCentral(fn func(*Ui))
```

Every container's callback receives the same `*Ui`. There is no encoding of "this Ui is inside a flow container" vs "this Ui is the root, before any panel" vs "this Ui is inside an absolute-positioning rect." The codegen DSL ([`egui2_definition_d_widgets.go`](../../public/thestack/imzero2/egui2/definition/egui2_definition_d_widgets.go)) emits all widget methods onto the same receiver regardless of the contexts where the widget is semantically valid.

## Design space (QOC)

**Question.** How should ImZero2 encode container-nesting preconditions so the Go compiler enforces them at call sites?

**Options.**

- **O1** — *Phantom-typed `Ui[Ctx]` generic.* Single generic type with a phantom type parameter naming the context. Methods defined parametrically over `Ctx`; the type parameter documents intent.
- **O2** — *Multiple concrete context types.* Separate `RootUi`, `FlowUi`, `FreeUi`, `BoundedUi`, `PaintUi` value types, each with its own method set. A common widget base is embedded into the contexts where its methods are valid.
- **O3** — *Capability interfaces.* Methods defined on a single Ui type but accept constraint interfaces (`type Flow interface{ flowMarker() }`) at parameter positions. Compose via `interface{ Flow | Free }` for widgets valid in multiple contexts.
- **O4** — *Status quo + lint / runtime asserts.* One Ui type. Container preconditions enforced at runtime (panic / error) and by convention rules in CLAUDE.md.

**Criteria.**

- **C1 — Compile-time enforcement.** Does an illegal nesting (e.g. `AllocateUiAtRect` inside `Vertical`) fail to compile?
- **C2 — Call-site verbosity.** How much surface ceremony does the user see per call site (extra type parameters, casts, conversions)?
- **C3 — Codegen + spec DSL refactor cost.** What changes to `egui2_definition_d_widgets.go`, the generators, and the regenerated outputs are required?
- **C4 — IDE completion narrowing.** At a given cursor position, does autocomplete show only the methods legal in that context?
- **C5 — Migration cost for existing call sites.** How many existing usages must be edited?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −  | ++ | +  | −− |
| C2 | +  | −− | −  | ++ |
| C3 | −  | −− | −  | ++ |
| C4 | −− | ++ | +  | −  |
| C5 | +  | −− | −  | ++ |

**O1's fundamental gap.** Go's generics define methods *parametrically over the type parameters of the receiver type*. There is no `impl Ui[Flow]` block analogous to Rust's `impl Ui<Flow>`. Concretely:

```go
// Go does NOT allow these to coexist as different methods on different
// instantiations:
func (u *Ui[Flow]) AllocateUiAtRect(...)  // method-on-instantiation — illegal
func (u *Ui[Free]) AllocateUiAtRect(...)  // same name, different param — illegal

// Only this is allowed — one method, parametric over Ctx:
func (u *Ui[Ctx]) AllocateUiAtRect(...)   // exists on every instantiation
```

A pure O1 therefore reduces to documentation: the type parameter records intent, but every method is callable on every `Ui[Ctx]`. C1 collapses to `−`, C4 to `−−`. The phantom name is intuitive but the enforcement isn't there.

**O2 is the actual enforcing design** — the Go-idiomatic realisation of typestate. Multiple concrete types, each with its own method set, sharing a common embedded base for the widget vocabulary that's valid across contexts. The cost is verbosity: ~5 receiver types declared, plus generator support for emitting per-context method sets, plus migration of every call site whose receiver type changes.

**O3 sits between.** Capability interfaces approximate constraints via interface satisfaction, but composition is awkward: an `interface{ Flow; Free }` is constructible (any type that implements both) and a method that accepts it admits values the user shouldn't be able to pass. Goroutine-style "marker methods" (`flowMarker()`) discourage external implementation but don't prevent it inside this module.

**O4** is the status quo: zero compile-time work, zero migration, zero enforcement. Worth recording as the baseline; not the choice.

## Decision

We will adopt **O2 — multiple concrete context types with an embedded common widget base**.

The receiver hierarchy:

- **`RootUi`** — entry point passed to `App.Run`. Only panel-creation methods are defined: `PanelCentral`, `PanelLeft`, `PanelRight`, `PanelTop`, `PanelBottom`. No widgets.
- **`FlowUi`** — the common case, returned by panel-entry methods, `Vertical`, `Horizontal`, `Group`, `IdScope`, `CollapsingHeader`. Embeds `coreWidgets` for the standard widget vocabulary; locally adds `Vertical`, `Horizontal`, `Group`, `ScrollArea` (returns `BoundedUi`), `IdScope`, `CollapsingHeader`. **Does not** define `AllocateUiAtRect`.
- **`FreeUi`** — returned by `AllocateUiAtRect`'s callback. Absolute-positioning context. Embeds `coreWidgets`. Locally re-defines `AllocateUiAtRect` (re-entrant). **Does not** define `Vertical` / `Horizontal` (they would silently break the absolute-coords contract).
- **`BoundedUi`** — returned by `ScrollArea`'s and `Window`'s callbacks. Embeds `coreWidgets`. Locally adds `DockArea` (which requires bounded height to render correctly under egui_dock 0.19+).
- **`PaintUi`** — returned by `PaintCanvas`'s callback. *Does not* embed `coreWidgets`; its method set is the paint-primitive vocabulary (lines, rects, text overlays). Standard widgets are not legal here because their layout assumptions don't apply inside a paint canvas.

`coreWidgets` is the embedded base carrying methods for `Label`, `Button`, `Checkbox`, `RadioButton`, `Slider`, `DragValue`, `TextEdit`, etc. — every widget whose semantics are independent of the enclosing container's layout discipline.

Codegen DSL extension (one new clause on `idl.NewBuilderFactoryNode`):

```go
// In egui2_definition_d_widgets.go:
idl.NewBuilderFactoryNode("vertical").
    WithContexts(definition.CtxFlow).        // valid ONLY in FlowUi
    EntersContext(definition.CtxFlow).       // callback receives FlowUi
    /* ... */

idl.NewBuilderFactoryNode("allocateUiAtRect").
    WithContexts(definition.CtxFree).        // valid ONLY in FreeUi
    EntersContext(definition.CtxFree).       // callback receives FreeUi
    /* ... */

idl.NewBuilderFactoryNode("checkbox").
    WithContexts(definition.CtxCore).        // common — emits on coreWidgets
    /* ... */
```

The generator emits each widget's `.Send` / `.SendResp` / `.SendRespVal` methods onto the receiver type(s) named by `WithContexts`. Container methods that introduce a new context (panels, `ScrollArea`, `AllocateUiAtRect`) take a typed callback whose parameter type is the entered context.

A drift-guard test analogous to `TestStatefulWidgetsAreGated` ([ADR-0013](0013-imzero2-stateful-widget-contract.md)) asserts every widget spec declares at least one context, and that container widgets that introduce a context name an `EntersContext` matching the callback signature in their construction template.

## Alternatives

- **O1 — Pure phantom-typed `Ui[Ctx]`.** Rejected as primary mechanism: Go's generic methods cannot be specialised per instantiation, so the type parameter degenerates into documentation. Could be layered *on top of* O2 as type aliases (`type FlowUi = ContextualUi[Flow]`) for callers who prefer the generic-shaped surface — bikeshed; punt.
- **O3 — Capability interfaces.** Rejected as the primary mechanism: interface satisfaction is structural, so `interface{ Flow }` can be implemented by any type with a `flowMarker()` method, including outside this module if the interface is exported, or inside via test-fakes. Compositional weakness (`interface{ Flow | Free }`) further dilutes constraints. Useful as a *secondary* mechanism for cross-cutting traits (e.g. "this widget can render in any context" via an `AnyContext` capability), but not the primary nesting model.
- **O4 — Status quo.** Rejected: the costs of driver-3 traps already exceed the cost of the refactor across a multi-year horizon, and every new widget compounds them.
- **Linear types via consumed tokens.** Considered (each container yields a token whose `Drop` is checked); rejected as un-Go-idiomatic and verbose at call sites.
- **Phantom-typed `Ui[Ctx]` plus free functions for context-specific operations.** Considered:

  ```go
  func Vertical[C ~Flow](u *Ui[C], fn func(*Ui[Flow])) { ... }
  ```

  Compiles in modern Go and provides per-context constraints. Rejected because free-function form breaks the egui-style fluent receiver API pervasively used in current call sites — migration cost dominates the saving.

## Consequences

### Positive

- The driver-3 traps in CLAUDE.md become compile errors, not runtime symptoms. `AllocateUiAtRect` inside a `Vertical` body cannot be expressed; `DockArea` outside a bounded scope cannot be expressed.
- IDE completion narrows to the methods legal in the cursor's context. Discoverability improves — the surface is not "all 50+ widgets all the time" but "the ~10–20 valid in this scope."
- New widget vocabulary lands cleanly. Adding a `MapMarker` widget that's only valid inside `MapView` is a one-line `WithContexts(CtxMap)` clause in the spec; the generator does the rest.
- The hybrid TUI/egui-tandem direction (separate thread; not a decision yet) reuses the same context types: a cell-grid renderer respects the same nesting contract since the *contract* is the API, not a property of the egui backend.
- ADR-0013's stateful-widget contract composes naturally: stateful widgets live on `coreWidgets`, are valid in every flow / free / bounded context, and their `SendRespVal(*T)` method is generated once on the embedded base.

### Negative

- API surface expands from one receiver type to ~5. Each widget's spec declares its valid contexts; the generator emits the right method on the right receiver(s); the drift guard enforces.
- Migration cost is significant. Every existing call site whose `c` is currently `*Ui` must be re-typed against the new contexts. In practice the embedded-base approach absorbs most of this — `c.Label()` works whether `c` is `FlowUi`, `FreeUi`, or `BoundedUi`. The paying cases are container entries (`PanelCentral`'s callback signature changes), `AllocateUiAtRect` re-entries, and any code that holds a `*Ui` value in a struct or passes it around generically.
- The codegen DSL gains weight: `WithContexts` and `EntersContext` clauses on every spec, plus a per-context `coreWidgets` synthesis step in the generator. The drift guard becomes a multi-axis check (ADR-0013 was one-axis).
- The egui-side has its own `Ui` type that's context-free in this sense; we are adding a Go-side discipline on top of egui's own. Contributors familiar with egui Rust may be momentarily confused — particularly when a Go method maps to an egui call that takes any `&mut Ui`. Documentation must explain that the Go context types are an over-approximation enforced for the user's benefit.

### Neutral

- The legacy `c.RadioButton(...)` shape is preserved syntactically because most widgets live on `coreWidgets`, embedded everywhere a widget makes sense. The user may not notice the type change at all for the common cases.
- An `Ui[Ctx]` phantom-generic surface (O1) can be added as a *type-alias layer* over O2 if there's call-site demand — `type FlowUi = ContextualUi[Flow]` etc. — without affecting the implementation. Defer until needed.
- Migration can be staged. Phase one: introduce the context types alongside the existing single Ui; widget specs declare contexts; generator emits methods on both the legacy receiver (deprecated) and the new context types. Phase two: migrate call sites in batches. Phase three: remove the legacy receiver. The ADR does not prescribe a phase boundary; the implementation plan should.
- ADR-0014 does not address driver-2 (manual opaque widget identity) or driver-5 (temporal input semantics). Those remain open. A call-site ID generator (driver-2) layers cleanly on top of O2: `WithKey(i)` extensions live on the same context types as the widgets they parameterise.

## Status

Proposed — awaiting review by @stergiotis.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## References

- Cognitive-load driver 3 (container/layout preconditions outside the type system) and the broader survey of TUI library APIs: Go TUI Library API Styles.
- Go generics method-on-instantiation limitation: [The Go Programming Language Specification — Type parameters](https://go.dev/ref/spec#Type_parameter_declarations) (methods are declared on the generic type, not on instantiations).
- Rust typestate prior art: hyper's `Connection` typestate, embedded-hal's ownership model, and the broader [typestate pattern](https://yoric.github.io/post/rust-typestate/).
- Haskell precedent: indexed monads in PureScript Halogen and `Control.Monad.Indexed`.
- Related ADRs: [ADR-0059 — declarative layouting over visual builder](0059-imzero2-declarative-layouting-over-visual-builder.md), [ADR-0013 — stateful-widget API contract](0013-imzero2-stateful-widget-contract.md).
- Driver inventory: see `feedback_*` and `project_*` memory entries enumerating the runtime-discovered nesting rules this ADR proposes to elevate to compile-time.
