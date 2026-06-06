// Package schemaview is an imzero2 widget that renders a leeway schema —
// a [common.TableDesc] — as a master-detail inspector: a collapsible
// section tree on the left, a decoded property pane for the selected node
// on the right.
//
// It is a read-only structure view. TableDesc carries no entity values, so
// this widget shows shape, not data: plain item-types, tagged sections,
// their value columns, canonical types, the encoding-hint / value-semantic
// / use-aspect sets, membership specs, and the co-section / streaming
// groupings.
//
// # Why it reads TableDesc directly
//
// The rest of the leeway "card" family — UnicodeCard, JsonCard, the SVG and
// topology sparks, and the egui Table2CardEmitter next door in leewaywidgets
// — are [streamreadaccess.SinkI] implementations driven over an Arrow batch.
// There is a schema-only traversal (Driver.DriveSchema, which
// JsonCardSchemaEmitter consumes), but it is lossy for schema metadata:
// memberships only ever surface as runtime instances (AddMembership*), so
// the schema path carries no MembershipSpec, and it does not surface
// encoding hints. A faithful schema inspector wants exactly those. So this
// widget reads the TableDesc fields directly rather than going through a
// Driver. It stays consistent with the card family in vocabulary (the
// topology-spark glyphs, the membership-role notions) but not in plumbing.
//
// # Glyph vocabulary
//
// The tree reuses TopologySpark's legend, rebound from data to schema:
//
//	◆ plain item-type section
//	◇ tagged section
//	◈ co-section group
//	ˡ ʰ ᵐ  the section's MembershipSpec cardinality class (low / high /
//	       mixed) — the spec, not an instance count
//	·∅ a value-less (membership-only) section
//
// TopologySpark's instance counts (#2 tags, ∥4 four-element array) have no
// schema analog and are dropped; a column leaf shows the terse canonical
// type, a section node shows the accepted membership spec as a badge, and
// the full MembershipSpec / aspect / type decodes live in the detail pane.
//
// # Detail pane
//
// Polymorphic on the selected node. A column leaf shows name, scope,
// item-type, canonical type (terse plus a decomposed line and the
// scalar/array/set shape), encoding hints and value semantics. A section —
// reached via its "properties" leaf, or directly when it is value-less —
// shows its membership spec decoded, use aspects, co-section + streaming
// groups, and value-column count.
//
// # Scope
//
// v1 renders the authored TableDesc. The IntermediateTableRepresentation
// expansion (support / membership columns), the physical-column descent and
// DDL coverage, and rendering sample data through a Driver are out of scope
// — natural later tabs, not v1.
//
// The widget takes a [*common.TableDesc] and owns only selection + filter
// state; the host supplies the schema. The demo wires the fixtures
// (leewaywidgets.BuildFixtureTableDesc, mapping.NewJsonMapping) and the
// chooser, keeping this package free of fixture and mapping imports.
package schemaview
