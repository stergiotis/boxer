// Package knn is the neighbour-graph producer of the graph analytics engine
// (ADR-0230 §SD1): a feature matrix in, a weighted undirected
// [github.com/stergiotis/boxer/public/analytics/graph/csr.Graph] out.
//
// The graph is the one UMAP builds before it lays anything out (McInnes,
// Healy & Melville 2018): an exact k-nearest-neighbour graph, each row's
// bandwidth σ found by binary search so that its membership sum equals
// log₂(k+1) with the nearest neighbour's distance ρ as the offset, membership
// weights exp(−(d−ρ)/σ), and the directed graph symmetrised by the fuzzy
// union W + Wᵀ − W∘Wᵀ. Every layout on the attraction–repulsion spectrum of
// Böhm, Berens & Kobak (2022) — t-SNE, UMAP, ForceAtlas2 — is a force layout
// of this graph, which is why the producer and the layout are separate here.
//
// Results are a function of the input alone: candidates tie-break by slot,
// rows are computed in contiguous chunks with per-worker scratch, and the
// result at one worker equals the result at any other count (ADR-0229 §SD2).
// Columns are aligned with the graph's slots (ADR-0229 §SD5), which are in
// ascending id order, not input row order; [Result.Rows] maps back.
package knn
