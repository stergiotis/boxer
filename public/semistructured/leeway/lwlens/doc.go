// Package lwlens reads a leeway batch as rows of slots and plans how to draw
// them under two reader intents: how much the reader cares about structure
// versus values, and whether each row is drawn on its own terms or on a frame
// shared across rows.
//
// A slot is a (section, primary membership) pair — the attribute identity a
// leeway record carries in its schema rather than in its values. A row either
// has a slot or lacks it; that presence is the structure. What the slot holds
// is the value.
//
// The package has three layers, each a plain function of the one before:
//
//   - Model, collected by Sink from a streamreadaccess drive: rows, slots,
//     cells.
//   - Analysis, computed by Analyze: slot support, clusters over slot
//     presence (the neighbour graph and HDBSCAN the projection panel runs),
//     a one-vs-rest threshold tree per cluster read as a rule, and per-slot
//     value distributions.
//   - Plan, computed by PlanRows from an Analysis and the two intents: which
//     rows go in which band, which slots each row is drawn against, and in
//     what order.
//
// Drawing is not here; leewaywidgets.LensView paints a Plan.
package lwlens
