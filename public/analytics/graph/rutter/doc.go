// Package rutter is the road-network routing engine (ADR-0256, proposed):
// a graph of arcs with identities, a metric as a column over them, Dijkstra
// as the oracle, an Inertial Flow nested-dissection order, a Customizable
// Contraction Hierarchy with basic customization and an elimination-tree
// query, many-to-many by buckets, and a grid index for snapping a point to
// the nearest polyline.
//
// Nothing here knows a coordinate system, a road class or a country: a
// graph is built from tail, head and edge arrays, a metric is whatever the
// caller counts per arc, saturating at [Inf], and the plane the order and
// the index work in is whatever two coordinate arrays describe. Every
// structure is slices of fixed-width integers (§SD7); ids are slot indices.
//
// The shape of a session: build a [Graph] once per topology; compute an
// [Order] once per topology (seconds to a minute at country size); build a
// [CCH] from both; then, per metric, [CCH.Customize] and query through a
// [CCHQuery]. A [Dijkstra] over the same graph and metric is the reference
// every hierarchy result is tested against, and the engine for one-to-all
// under a cut-off.
package rutter
