// Package schemaview is an imzero2 widget that renders a leeway schema —
// a [common.TableDesc] — as a master-detail inspector across two dock panes:
// a collapsible section navigator ("structure") and a decoded property pane
// ("detail"). The panes are draggable / resizable (egui_dock persists the
// layout) and each scrolls independently, so a tall schema no longer relies on
// the host window's scroll.
//
// It is a read-only structure view. TableDesc carries no entity values, so
// this widget shows shape, not data: plain item-types, tagged sections,
// their value columns, canonical types, the encoding-hint / value-semantic
// / use-aspect sets, membership specs, and the co-section / streaming
// groupings.
//
// # Why it reads TableDesc directly
//
// The leeway readers next door — the read model behind leewaywidgets'
// RecordCard, the lens's sink — are [streamreadaccess.SinkI] implementations
// driven over an Arrow batch. The sink protocol is lossy for schema metadata: memberships
// only ever surface as runtime instances (AddMembership*), so it carries no
// MembershipSpec, and it does not surface encoding hints. A faithful schema
// inspector wants exactly those. So this widget reads the TableDesc fields
// directly rather than going through a Driver. It shares the readers'
// membership-role notions but not their plumbing.
//
// # Glyph vocabulary
//
// The tree's glyphs, first drawn by the retired topology spark (ADR-0289,
// proposed), now belong to this widget:
//
//	◆ plain item-type section
//	◇ tagged section
//	❖ co-section group
//	ˡ ʰ ᵐ  the section's MembershipSpec cardinality class (low / high /
//	       mixed) — the spec, not an instance count
//	·∅ a value-less (membership-only) section
//
// A column leaf shows the terse canonical type, a section node shows the
// accepted membership spec as a badge, and the full MembershipSpec / aspect /
// type decodes live in the detail pane.
//
// The vocabulary travels with the widget: a "?" toggle in the navigator
// header opens a tethered legend window (the canonicaltypesummary inspector
// idiom — anchor + bezier connector) keying every glyph, so a reader needs
// neither the demo description nor this doc to decode the tree. The long-form
// reference ships as the "Schema inspector" help book (see [HelpFS]), which the
// carousel layer registers with the runtime help library for the Help app.
//
// # Detail pane
//
// Polymorphic on the selected node, headed by the node's navigator glyph (in
// its category tone) and a kind chip. A column leaf shows scope, item-type,
// the canonical type via the [canonicaltypesummary] inspector, and its
// encoding hints / value semantics. A section — selected by clicking its own
// row — shows its membership spec decoded, use aspects, co-section + streaming
// groups, and value-column count. Aspect sets render as toned chips (one per
// aspect) rather than comma-joined text.
//
// # Scope
//
// v1 renders the authored TableDesc. The IntermediateTableRepresentation
// expansion (support / membership columns), the physical-column descent and
// DDL coverage, and rendering sample data through a Driver are out of scope
// — natural later tabs, not v1.
//
// The widget takes a [*common.TableDesc] and owns only selection, filter, and
// legend-popup state; the host supplies the schema. The demo wires the fixtures
// (leewaywidgets.BuildFixtureTableDesc, mapping.NewJsonMapping) and the
// chooser, keeping this package free of fixture and mapping imports.
package schemaview
