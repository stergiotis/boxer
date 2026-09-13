// Package algo holds the whole-graph metrics of the graph analytics engine
// (ADR-0229 §SD3): degrees, breadth-first distances, connected and strongly
// connected components, PageRank, k-core decomposition, triangle counting,
// betweenness centrality and maximal cliques — and, over a
// distance-weighted graph, HDBSCAN (ADR-0230 §SD3).
//
// Every function takes a [context.Context] and a budget and returns, beside
// its columns, a [Truncation] saying whether and by which limit it stopped
// short (ADR-0229 §SD4). A truncated result is a valid result with a flag.
// Columns are struct-of-arrays slices aligned with the graph's slots
// (ADR-0229 §SD5); the consumer joins back to ids through
// [github.com/stergiotis/boxer/public/analytics/graph/csr.Graph.IDs].
//
// Results are a function of the topology alone: the same graph and options
// give the same bits at any worker count (ADR-0229 §SD2). Sampling is seeded.
package algo
