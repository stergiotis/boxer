// Package mappingplanview is an interactive ImZero2 widget for authoring a
// leeway mappingplan spec and live-previewing the code it compiles to.
//
// # What it is
//
// A dockable playground. The editor pane (left) edits a [Model] — the entity
// kind, plain columns, and lw:-tagged value/const fields with their membership,
// section, sub-column, channel and flags. The output panes (right, one dock tab
// each) show what the resulting plan compiles to: the schema-agnostic Go codec
// (marshallgen), the parsed Plan IR (JSON), and the dql SQL read-back artefacts
// (presence / projection / validator, bound to a seeded schema). The whole plan
// is re-validated through mappingplan.PlanBuilder on every edit; a status line
// reports the plan-level verdict plus a per-field roll-up.
//
// # Per-field validity
//
// Every field card carries its own validity state machine ([FieldState]) shown
// as a tethered inspector chip (built on [fsmview]): a colour-coded badge
// (empty / incomplete / valid / rejected / conflict / blocked) you click to open
// a floating window with the state graph, the transition history (each move
// tagged with the reason it fired), and the rejection text. The widget decides
// Empty / Incomplete from the row alone; Valid / Rejected / Conflicting /
// Blocked come from the host's sequential build report ([BuildResult]) —
// PlanBuilder is fail-fast and stateful, so the first bad field is Rejected /
// Conflicting and every later field is Blocked. Rejected (the field's own
// shape / tag) is told apart from Conflicting (a clash with another field) by
// the rejection message ([classifyConflict]).
//
// # Why a Model instead of a *mappingplan.Plan
//
// A mappingplan.Plan has no constructor other than PlanBuilder and no setters:
// it is the validated *output* of AddField / AddUnderscoreField / Finish,
// built from a DTO's Go type + lw: tags (via marshallgen's go/ast front-end,
// or marshallreflect). It also has no serialised form. So the widget's
// editable state is a [Model] whose rows mirror the PlanBuilder *input*
// sequence: each edit re-runs the builder, and the genuine front-end
// validator is the validation feedback — no rules are reimplemented here.
//
// The rebuild itself is injected by the host through [Input.Recompute] (see
// the mappingplanview demo), so this package depends only on the lightweight
// mappingplan data model, not on the marshallgen / dql back-ends.
//
// # Editing is exploratory (no write-back)
//
// The Model is authored in-widget and never written back to Go source. A Plan
// built by reflection mirrors a compiled type whose tags are immutable at
// runtime, so round-tripping edits to source is out of scope; the value here
// is seeing what a given lw: tagging validates to and compiles to.
//
// # Still deferred
//
//   - Carrier channels (mixed* / *parametrized) — require a paired carrier
//     sibling field the editor does not model yet; the channel picker offers
//     the four Cut-1 channels only.
//   - Per-field Conflicting attribution for cross-field failures that surface
//     only at Finish (channel mixing, carrier pairing): the builder error names
//     the field in structured data the rendered message drops, so those stay
//     plan-level in the global verdict rather than colouring one card.
//
// (The SQL read-back preview and the syntax-highlighted codeview panes, once
// listed here as v2 work, have since shipped: the output panes are highlighted
// [codeview] jobs rebuilt per recompute, and the SQL artefacts come from
// dql.Generate against a seeded schema. See ADR-0066.)
package mappingplanview
