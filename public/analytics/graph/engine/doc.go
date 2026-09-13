// Package engine is the iteration layer of the graph analytics engine
// (ADR-0229 §SD2): a frontier ([Subset]) kept sparse or dense, an edge map
// whose direction is chosen per sweep, and chunked parallelism whose result
// is bit-identical to the serial one.
//
// The shape is Ligra's (Shun & Blelloch, PPoPP 2013) with GraphIt's
// separation of direction from algorithm. Two rules make the parallel result
// a function of the topology alone:
//
//   - A dense sweep pulls: destinations are split into contiguous chunks and
//     every write in a chunk lands on a destination that chunk owns, so no
//     two workers write one slot and the fold within a slot runs in id order.
//   - A reduction across vertices folds fixed-size chunk partials in chunk
//     order, so the summation order does not depend on the worker count.
//
// A sparse push sweep runs serially. It is chosen only when the frontier's
// out-edges are a small fraction of the arcs, so its cost is bounded by
// construction, and a serial push needs no atomics to be deterministic.
package engine
