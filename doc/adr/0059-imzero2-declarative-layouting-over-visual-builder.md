---
type: adr
status: proposed
date: 2026-04-24
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0059: ImZero2 — Invest in Declarative Layouting and Fast Turnover, Not a Visual GUI Builder

## Context

ImZero2 is a Go-driven, Rust-rendered UI layer over [egui](https://www.egui.rs/). Widgets are declared in hand-written IDL files under [`public/thestack/imzero2/egui2/definition/`](../../public/thestack/imzero2/egui2/definition); a codegen pass produces the Go wrappers (`*.out.go`) and the Rust dispatch in [`rust/imzero2/src/imzero2/interpreter.rs`](../../rust/imzero2/src/imzero2/interpreter.rs). Authoring today is pure code: a panel is a sequence of Go calls into the generated wrappers, emitted as an opcode stream each frame and applied by the continuous Rust `logic()` loop ([`doc/skills/imzero2/SKILL.md`](../skills/imzero2/SKILL.md)).

Two recurring authoring concerns frame this decision:

1. **Verbosity of layout code.** A realistic panel (plot + controls + table + status bar) today requires on the order of dozens of nested `horizontal`/`vertical`/`columns`/`grid` calls with manual sizing. The egui primitives are low-level; larger layouts are expressed by cascades of them. Across existing demos and components under [`egui2/demo/`](../../public/thestack/imzero2/egui2/demo), [`egui2/treemap/`](../../public/thestack/imzero2/egui2/widgets/treemap), and [`egui2/scctree/`](../../public/thestack/imzero2/egui2/widgets/scctree), the same cascade shapes recur.
2. **Audience pressure toward a visual tool.** The Qt-backed domain precedent for trained-user realtime HMI (Qt Designer + `.ui` files) makes the question — explicit or implicit — whether imzero2 should invest in a comparable visual / WYSIWYG designer to lower authoring cost and open contribution to non-programmers.

Three external signals inform the calculus:

- **SDR GUI paradigm case study** (sdrangel Qt vs SDRPlusPlus Dear ImGui) finds a durable ~10× GUI-LOC gap favoring immediate-mode for comparable application scope, but also notes that sdrangel's 248 `.ui` designer files represent real authoring work that any ImGui-style project absorbs as hand-written code. The visual-builder value proposition is observably real in the Qt segment; the cost of not having one is observably bounded in the ImGui segment.
- **Web development trajectory.** Professional web frontend has systematically moved *away* from WYSIWYG builders (FrontPage → Dreamweaver → modern component frameworks with Figma-as-spec). Declarative CSS (flexbox, grid, utility classes) plus component composition reduced the marginal value of a designer tool to the point where pro teams omit it entirely. GUI builders survive in marketing-site (Webflow, Squarespace), WordPress, admin-tool (Retool), email, and no-code niches. The web trajectory is evidence that "no visual builder, pro-quality outcomes" is a reachable steady state given sufficiently declarative primitives.
- **egui layout-ecosystem survey (April 2026).** [Taffy](https://github.com/DioxusLabs/taffy) (DioxusLabs, Apache-2.0, full CSS block + flexbox + grid) is the de facto Rust layout engine, with broad downstream adoption (Dioxus, Bevy, others). [`egui_taffy`](https://github.com/PPakalns/egui_taffy) is its canonical egui wrapper, actively maintained as of April 2026 and tracking egui releases. egui's own `Layout` is directional / 1D-flex-like, and the egui maintainers have explicitly declined to pull a CSS-shaped engine into core — the ecosystem strategy is to keep egui lightweight and let wrappers integrate Taffy where needed. Narrower alternatives (`egui_flex` flex-only, `egui_grid` dormant since 2024) exist but are not the consensus choice. The ecosystem provides a drop-in path for full CSS-shaped layout without building our own solver.

The AI-agent authoring shift further reshapes the calculus:

- LLMs collapse the productivity case for drag-drop tools; for programmers they collapse the tedium of emitting layout code, and for non-programmers the agent itself becomes the authoring interface.
- The need for fast visual *preview* strengthens, because agents make layout mistakes that text review cannot catch.
- The component library becomes the LLM's effective API surface; its prompt-legibility (clear names, docstrings, idiomatic examples) becomes load-bearing in a way it was not pre-LLM.
- New capabilities emerge that had no pre-LLM analog: schema-to-panel generation, style propagation from a reference, retroactive a11y annotation, multi-modality regeneration from one spec.

Forces the decision must respect:

- **Team size.** imzero2 is maintained by a small team. A Qt-Designer-equivalent is a multi-engineer-year project comparable in scope to Qt Creator's Designer or Webflow — infeasible at current capacity.
- **Execution model.** ImZero2's runtime is not a linear opcode stream: deferred blocks are recorded Go-side and spliced in at a different position later; the Rust side can cull whole blocks ([ADR-0052 SD2](0052-imzero2-unified-color-type.md); [ADR-0058 SD2](0058-imzero2-scrolling-texture-widget.md); [`doc/skills/imzero2/SKILL.md`](../skills/imzero2/SKILL.md) §11). A visual authoring tool must either model this or emit code that maps to it — both are meaningful investments.
- **Target audience.** Trained-user realtime HMI (streaming dashboards, scientific instruments, SDR-class displays), not marketing sites or non-programmer dashboard builders.
- **IDL-driven authoring.** Widgets are declared in hand-written IDL; generated files under `components/*.out.go` and [`rust/imzero2/src/imzero2/interpreter.rs`](../../rust/imzero2/src/imzero2/interpreter.rs) are off-limits. Any visual tool must either emit Go-side consumer code or define a new authoring-artifact format.
- **No generic declarative layout layer exists today.** The only `layout/` directory in the tree is the domain-specific [`egui2/treemap/layout/`](../../public/thestack/imzero2/egui2/widgets/treemap/layout). Generic declarative layout is a green field.
- **LLM authoring is already the primary non-programmer path.** A domain expert today interacts with imzero2 through an agent that writes Go, not through a canvas.

The question this ADR settles: where does imzero2 invest its UI-authoring budget — a visual/WYSIWYG designer, declarative layout primitives, fast preview-turnover infrastructure, or some combination?

## Design space (QOC)

**Question.** Where does imzero2 invest to lower the cost of UI authoring while respecting the team-size budget, the IDL/deferred-block execution model, and the agent-driven authoring shift?

**Options.**

- **O1 — WYSIWYG designer.** Build a canvas-based tool (palette, drag-drop, property panels) that emits Go IDL code. Inspired by Qt Designer / Elementor / Webflow. One-way code emission at minimum; round-trip is a separate internal design choice.
- **O2 — Declarative layout DSL + fast preview turnover + LLM-legible component library _(chosen)_.** Three coordinated investments, none of which is a visual tool: (a) a concise declarative layout layer reducing per-panel LOC, (b) tight edit → pixel iteration treated as a tracked KPI, (c) a component library with naming, docstring, and example conventions explicitly optimised for LLM authoring. Schema-to-panel generation is a follow-on, not a fourth pillar.
- **O3 — Screenshot / Figma-to-code agent pipeline only.** Rely entirely on LLMs with screenshots or design files as input; no declarative layer, no visual tool, no component-library investment beyond today's level. Users describe or draw; the agent produces code against the existing imperative surface.
- **O4 — Round-trip graphical tool.** O1 plus the ability to read existing Go layouts back into the canvas for further editing. Parity with the best of Qt Designer.

**Criteria.**

- **C1 — Build and maintenance cost.** Engineer-years to ship, plus ongoing maintenance on the small team.
- **C2 — Fit with agent-driven authoring.** Does an LLM-first workflow benefit materially from this investment, or is it bypassed?
- **C3 — Fit with imzero2's IDL / deferred-block execution model.** Does the proposal preserve or threaten the properties that ADR-0052 and ADR-0058 rely on?
- **C4 — Authoring speed for a realistic panel.** Time to produce "plot + controls + table + status bar" from scratch by a competent contributor.
- **C5 — Accessibility to non-programmer contributors.** Can a designer / domain expert meaningfully contribute without learning Go?
- **C6 — Iteration quality.** Edit → pixel latency for incremental changes during development.
- **C7 — Reversibility.** Cost to change direction if the bet turns out wrong.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −  | +  | ++ | −− |
| C2 | −  | ++ | +  | −  |
| C3 | −− | +  | ++ | −− |
| C4 | +  | +  | +  | ++ |
| C5 | ++ | −  | +  | ++ |
| C6 | +  | ++ | +  | +  |
| C7 | −  | ++ | ++ | −− |

O2 is the Pareto-preferred option for the current constraints. It dominates or ties every option on C1, C2, C3, C6, C7, and trades C5 (non-programmer accessibility) explicitly — a trade the agent-driven shift substantially mitigates because the primary non-programmer authoring path is already conversational, not visual. O3 is a strict subset of O2; O2 is O3 plus the substrate improvements that raise the agent's correctness rate and reduce its token consumption. O1 and O4 are plausible for a larger team or a materially different audience, but here are dominated by cost and execution-model fit.

## Decision

We invest in three coordinated capabilities and explicitly decline to build a WYSIWYG visual designer:

1. **A declarative layout layer** exposed as a Go package under `public/thestack/imzero2/egui2/layout/` (new directory). It reduces the LOC needed to express realistic panel layouts by exposing a higher-level vocabulary (stack / row / column / grid / split / overlay / pad / align / weight / gap) that emits layout-intent IDL opcodes. The Rust interpreter dispatches these opcodes to `egui_taffy` (see SD1), which resolves CSS block + flex + grid each frame on top of egui. Caller code shifts from cascades of primitive calls to declarative composition.
2. **Fast edit → preview turnover** as a measured capability. The canonical authoring loop is: edit Go code → rebuild Go-side → the running Rust host applies the new opcode stream and re-renders. Target latency for a typical edit is under two seconds wall-clock; under one second is the aspirational ceiling. Loop latency is treated as a product KPI and regressions are treated as bugs, not as tolerable drift.
3. **An LLM-legible component library** as a first-class authoring surface. Every widget and layout primitive ships with (a) a name that is an unambiguous noun phrase, (b) a docstring that describes behaviour in one paragraph and covers non-obvious edge cases, (c) at least one idiomatic usage example that compiles as-is. The authoring skill documentation ([`doc/skills/imzero2/SKILL.md`](../skills/imzero2/SKILL.md)) gains a layout-idioms appendix listing reference compositions ("plot over table", "controls-left / viewport-right", "status bar at bottom") with reference code.

A fourth capability is explicitly scoped out of this ADR but called out as the natural follow-on: **schema-to-panel generation**, where a Go struct or IDL specification maps to a reasonable default panel via the component library. That capability compounds on top of (1)–(3); it earns its own ADR when a concrete first consumer materialises.

### Subsidiary design decisions

- **SD1 — Adopt Taffy via `egui_taffy` for layout resolution; Go surface is a flex+grid vocabulary.** We wrap the canonical egui layout integration rather than building our own solver. The Go-side authoring vocabulary (stack / row / column / grid / split / overlay / pad / align / weight / gap) emits layout-intent opcodes; the Rust interpreter dispatches them to `egui_taffy`, which resolves via Taffy each frame. Rationale: (a) wrapping a maintained solver is meaningfully cheaper than building one and inherits its bug-fixes; (b) Taffy delivers full CSS block + flex + grid at approximately the cost of flex alone once it is in the picture — no reason to scope down to flex only; (c) CSS-flexbox is the vocabulary LLMs have the strongest priors for, making agent authoring reliable out of the box; (d) Taffy's downstream adoption (Dioxus, Bevy, `egui_taffy` users) means upstream maintenance is broad, not dependent on one small project. A full constraint solver (Auto Layout / Cassowary) remains out of scope; if a future consumer has a layout problem Taffy cannot express, revisit with that concrete shape in hand.

- **SD2 — Declarative layout composes with imperative escape hatches.** Any layout primitive accepts a child that is itself imperative Go code calling egui wrappers directly. The declarative layer is additive, not an exclusive wrapper; contributors fall back to imperative calls for widget-internal authoring (e.g. a custom [`paintCanvas`](../../public/thestack/imzero2/egui2/definition) payload) without leaving the declarative container. No split-language authoring.

- **SD3 — Fast-turnover relies on the existing per-frame IDL architecture.** Go-side emission already produces a fresh opcode stream every frame; the Rust `logic()` loop applies whatever arrives ([ADR-0058 context](0058-imzero2-scrolling-texture-widget.md)). An edit to Go authoring code takes effect when the rebuilt Go-side emitter is picked up; the Rust render path is unchanged. The investment is in the *rebuild → first-frame* path: incremental Go compilation, a rebuild-triggered reconnect mechanism where the architecture permits it, and robust process orchestration for the development loop. No new reload mechanism is introduced Rust-side.

- **SD4 — LLM-legibility has concrete acceptance criteria.** A widget or primitive is "LLM-legible" when: (i) an agent given only the library docs and the widget's docstring produces a correct usage in one shot for the documented example; (ii) the name does not collide semantically with another primitive in the library; (iii) the docstring states the unit of any sizing / timing parameter and names any register side effects. Enforcement is by convention and review; no codegen rule is added.

- **SD5 — Component library is documented in a single authored skill file.** The existing [`doc/skills/imzero2/SKILL.md`](../skills/imzero2/SKILL.md) is extended with a layout-idioms appendix and a component-legibility section. No parallel UI-style guide is started; the skill doc is the canonical agent-facing authoring reference, consistent with the rest of the boxer skills ecosystem.

- **SD6 — Figma / screenshot input is supported via agent, not via tool import.** A designer hands a Figma file or a screenshot to an agent; the agent translates to declarative layout code using the component library. No native Figma importer, no CSS-to-layout importer, no `.sketch` plugin. Rationale: design-tool importers are a known maintenance sink (every Figma schema version ships a tax, every CSS parser edge case ships a bug report); agent-mediated translation absorbs schema drift without maintenance cost and is already available.

- **SD7 — Pixel-level refinement lives in code, not a nudge tool.** "Move this 2 pixels left" is a code edit. The declarative layer exposes `pad(n)`, `margin(n)`, `gap(n)` primitives with pixel units; fine-grained spacing is an authoring responsibility. No visual nudge handles, no property inspector, no guides. If pixel-nudging becomes a measurable daily pain, revisit — but only with concrete pain, not hypothetically.

- **SD8 — Accessibility concerns deferred.** egui's a11y story is immature in 2026; any a11y investment is orthogonal to this ADR and has its own timing. The declarative layer does not preclude future a11y hooks (they can be added as per-primitive parameters). Explicitly: this ADR does not claim an a11y improvement from declarative layout, nor does it block one.

- **SD9 — Theme / style system is a separate concern.** Colors, typography, spacing tokens — the "look" of the app — are an orthogonal investment. The declarative layout layer consumes style tokens but does not own them. No decision in this ADR commits either way on theme architecture; [ADR-0052](0052-imzero2-unified-color-type.md) already settles the color type.

- **SD10 — Reversibility explicit.** If in 18 months the declarative + LLM bet proves insufficient for a concrete audience (for example, a hypothetical broadening toward non-programmer-facing dashboards), a visual tool remains plausible. The declarative layer is not hostile to a future visual authoring surface: a designer could emit declarative layout code the same way it would emit imperative code. The reverse migration (from visual-tool-authored code to declarative) is far more expensive; this direction preserves optionality.

- **SD11 — Schema-to-panel generation is scoped out but planned.** When a concrete first consumer appears (a leeway-schema-shaped record inspector, an FFFI2 IDL definition viewer, a "plot from this dataframe" capability), a follow-up ADR settles its shape. The component library and declarative layer are the substrate it will be built on; that future ADR depends on this one landing first.

- **SD12 — Existing imperative call sites are not force-ported.** Authored demos and components under [`egui2/demo/`](../../public/thestack/imzero2/egui2/demo), [`egui2/treemap/`](../../public/thestack/imzero2/egui2/widgets/treemap), [`egui2/scctree/`](../../public/thestack/imzero2/egui2/widgets/scctree), and elsewhere remain imperative until their authors choose to migrate. The declarative layer ships as additive surface; no deprecation of imperative calls is declared in this ADR.

- **SD13 — No separate "state-reactive" DSL (Elm / SwiftUI / Compose shape).** Those frameworks carry state-management and reactivity models (signals, observables, unidirectional data flow) that would collide with the existing Go-driven imperative control flow and with the deferred/culled execution model. The declarative layer proposed here is *layout-only*, not state-reactive; it composes with imperative state handling rather than replacing it. If state-reactivity becomes a concrete need, it is a separate ADR.

- **SD14 — Layout primitives are pure composition, not widgets.** A row / column / grid does not register as a widget ID, emit hover state, or surface click events. Interaction remains a widget-level concern. Rationale: layout-as-widget is the main source of debugging confusion in frameworks that conflate the two (e.g. earlier egui container APIs, pre-flexbox CSS). Clean separation keeps the mental model small.

## Alternatives

Rejection rationale for the top-level options is in the QOC matrix; notes below capture detail not visible in the ratings.

- **O1 — WYSIWYG designer.** A credible designer is a multi-engineer-year project comparable in scope to Qt Designer or Webflow. A half-built one — narrow widget coverage, no round-trip, poor layout semantics — is a well-documented failure mode in the space: it generates misleading code, trains users on a model that breaks at the boundary, and saddles the team with support load. For the target audience (trained-user HMI, not marketing / non-programmer dashboards), the C5 accessibility benefit is narrower than it would be for web marketing tools. The AI-era shift reduces the remaining value further: the primary non-programmer authoring path is conversational, not visual.

- **O3 — Agent-only pipeline, no declarative or component-library investment.** Workable in principle; agents can produce imperative layout code against today's surface. Rejected because it under-invests in the substrate the agent operates on. LLM output quality on unfamiliar APIs is visibly lower than on familiar ones (flex, grid), the imperative cascade is verbose and error-prone even for agents, and a concise declarative layer both reduces agent token consumption and improves one-shot correctness rate. O2 is a strict superset: it enables the agent-only workflow O3 describes *and* gives human authors a better surface.

- **O4 — Round-trip graphical tool.** Round-trip means the tool can read and modify existing authored code. This is the Qt Designer standard and the known-hard problem in WYSIWYG. Figma-to-code round-trip has no credible implementation in the industry after several years of tries. Qt Designer's round-trip works because `.ui` is a tool-owned artifact; in imzero2 terms that would mean defining a new layout-artifact format and making it the primary authoring surface — a commitment larger than O1. Dominated by O1 on cost and by O2 on everything else.

- **Adopt an existing declarative UI framework directly (Elm-style, SwiftUI-style, Compose-style).** Considered as an alternative instantiation of O2. Rejected per SD13: reactivity models would collide with Go's imperative control flow and the deferred/culled execution model. The declarative layer proposed is layout-only; richer frameworks are out of scope.

- **Wait-and-see: neither O1 nor O2.** Stay with imperative authoring and revisit in six months. Rejected because (a) the agent-authoring shift is already underway, and the component-library investment is on the critical path for agent-friendly authoring regardless of whether a layout layer ships; (b) the marginal cost of waiting is continued cascading of primitive calls in every new demo / component, which compounds and does not self-correct.

- **Hand-roll the layout math in Go.** An earlier draft of this ADR considered a bespoke Go-side flex-like solver emitting absolute positions to Rust. The April 2026 ecosystem survey (see Context) made that approach redundant: Taffy is the de facto Rust layout engine, and `egui_taffy` is its canonical egui wrapper. Hand-rolling would duplicate solved work, miss grid and block layouts that come for free via Taffy, and diverge on edge cases from the CSS idiom LLMs target natively. Rejected in favour of SD1's `egui_taffy` adoption. The hand-rolled path remains available as a fallback if `egui_taffy` becomes unmaintained; SD10's reversibility clause covers that scenario.

## Consequences

### Positive

- **Authoring LOC drops materially for realistic panels.** A "plot over table with side controls" becomes on the order of ten declarative layout lines plus widget bodies, versus dozens of nested primitive calls today. Fewer source lines; less room for layout typos. The IDL surface grows only by additive layout-intent opcodes (see Neutral); the widget-IDL is unchanged.
- **Agent authoring reliability improves.** LLMs have strong priors for flex-style declarative layouts from CSS; a thin Go wrapper exposing flex vocabulary lets a single-shot agent produce correct layouts for common shapes. The component library's docstrings and examples make widget-level calls similarly reliable.
- **Edit → preview latency becomes a tracked property.** Treating turnover as a KPI gives the team a concrete handle on authoring ergonomics that the alternatives largely leave unmeasured.
- **Team budget stays realistic.** Wrapping `egui_taffy` is a bounded piece of work; we are not writing a layout solver. Estimated scope: ~1–2 weeks for a usable first pass of the Go-side layout vocabulary plus Rust-side opcode dispatch to `egui_taffy`; ~1 quarter for a polished and well-documented version. The component-library formalisation and turnover-latency work are parallel, smaller efforts.
- **Reversibility preserved.** If the bet is wrong, a future visual tool can emit the same declarative layout code this ADR proposes. No authoring artifact is created that a future tool cannot read or regenerate.
- **Compounds with future schema-to-panel generation.** The declarative vocabulary is the natural target for a generator; building the generator *after* the vocabulary exists is meaningfully easier than inventing both at once.
- **Forward path to richer primitives preserved.** Taffy's flex + grid + block coverage is broad; if a future consumer needs constraint-based layout (e.g. Cassowary), it lands as new vocabulary on the same Go-side surface without reopening SD1.

### Negative

- **Non-programmer contributors require an agent mediator.** A domain expert who wants to rearrange a panel writes prose to an agent or code, not mouse gestures. For the target audience this matches the existing mental model; for any future broadening toward marketing-site or dashboard-builder audiences, this ADR must be revisited.
- **No design-tool pipeline for brand or style authoring.** Colors, spacing tokens, and typography remain hand-curated or LLM-generated; there is no Figma-style shared-source-of-truth for non-layout design decisions. SD9 defers this; it is still a gap.
- **Pixel-nudging is a code edit.** A contributor used to Figma-style drag handles for micro-spacing will find code-only spacing worse. SD7 accepts this; expect occasional friction around very precise visual tuning.
- **Layout debugging lacks visual scaffolding.** When a layout does the wrong thing, the debugging artifact is code plus screenshots. Browsers ship flex-debugger overlays; imzero2 does not today. A future inspect-mode (highlight containers at hover, print measured sizes) is plausible but out of scope here.
- **Bet is on LLM quality continuing to improve.** If agent authoring quality plateaus or regresses, the C5 trade gets worse. SD10 is the escape hatch.

### Neutral

- **Wire format additions, not changes.** The declarative layer adds new layout-intent opcodes (row / column / grid / stack / etc.) dispatched Rust-side to `egui_taffy`. Existing opcodes are unchanged; ADR-0052 and ADR-0058 remain unaffected. The widget-IDL surface grows, but only additively.
- **Existing imperative authoring remains valid (SD12).** Migration is per-component, author-driven, not forced by a deprecation schedule.
- **Component library work is partially already done.** Widgets shipping today already have some docs and examples; SD4 formalises the acceptance criteria rather than inventing an authoring norm from scratch.
- **Claims are testable.** "Panel LOC drop" and "turnover latency" targets in the Decision are measurable; a follow-up memo can report observed numbers after the first layout-layer version ships.
- **Inherit `egui_taffy`'s release cadence and breaking-change discipline.** The wrapper is actively maintained but permits breaking changes on minor versions. Mitigation: pin the version, evaluate per bump, and keep the Go-side layout vocabulary stable so consumer code is insulated from most upstream churn. If `egui_taffy` becomes unmaintained, the fallback is direct use of Taffy (which has broad non-egui adoption) or — ultimately — the hand-rolled path named in Alternatives.

### Derived practices

- **New widgets ship with the LLM-legibility checklist satisfied.** SD4's three criteria become reviewer checklist items: name audited for collision, docstring covers units and side effects, at least one compiling example committed.
- **Layout authoring prefers declarative primitives for panels with more than three nested containers.** One-widget or two-widget compositions may stay imperative; deeper nesting reaches for the declarative layer. Guidance, not a lint rule.
- **Turnover latency regressions are treated as bugs.** A change that adds more than one second to the edit → pixel path ships with a recovery plan, same as a performance regression.
- **Figma / screenshot translations go through an agent.** No code importer for design-tool formats is built or maintained.
- **Schema-to-panel generation gets its own ADR when a first consumer lands.** This ADR does not prejudge its shape beyond "it builds on the declarative layer and the component library".
- **Review expectation on new ADRs adjacent to this one.** A future ADR proposing a visual tool, a Figma importer, or a state-reactive DSL must explicitly address why the SD10 reversibility clause is being exercised.

## Status

Proposed. Awaiting review and concrete sizing of: the first-pass Go-side layout vocabulary and its mapping to `egui_taffy` (SD1); the target panel-LOC reduction baseline; the target turnover-latency number that turns SD3 from intention into a KPI.
