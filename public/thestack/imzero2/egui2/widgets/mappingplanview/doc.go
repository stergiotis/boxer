// Package mappingplanview is an interactive ImZero2 widget for authoring a
// leeway mappingplan spec and live-previewing the code it compiles to.
//
// # What it is
//
// A two-pane playground. The left pane edits a [Model] — the entity kind,
// plain columns, and lw:-tagged value/const fields with their membership,
// section, sub-column, channel and flags. The right pane shows the
// schema-agnostic Go codec that marshallgen emits for the resulting plan,
// re-validated through mappingplan.PlanBuilder on every edit. A status line
// reports the PlanBuilder verdict — valid, or the exact rejection reason.
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
// # Deferred to v2
//
//   - SQL read-back preview (dql.Generator) — needs an IR (physical schema) +
//     a membership resolver that a Plan does not carry (Plan ⊄ physical
//     schema), so it is bound to a seeded schema rather than any edited plan.
//     See ADR-0066.
//   - Carrier channels (mixed* / *parametrized) — require a paired carrier
//     sibling field the editor does not model yet; the channel picker offers
//     the four Cut-1 channels only.
//   - Syntax-highlighted codeview for the preview — codeview retained holders
//     are built once at init() and reused across frames; the live preview
//     re-renders on every edit, so v1 uses a read-only multiline TextEdit
//     (a transient per-frame string, no retained-element churn). Revisit once
//     the retained-element lifecycle for dynamic content is settled.
package mappingplanview
