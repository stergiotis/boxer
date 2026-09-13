// Package csr is the one adjacency container of the graph analytics engine
// (ADR-0229 §SD1): vertex ids mapped to dense slots, forward and reverse
// adjacency as offset and target arrays, neighbours sorted by id within each
// row, built once per topology by counting sort.
//
// Slots are assigned in ascending id order, so slot order is id order and
// every walk over a row is a function of the topology alone. Parallel edges
// collapse into one arc whose weight is the sum of the collapsed weights;
// self-loops are kept as an arc and counted. An undirected graph stores each
// edge in both rows; its in- and out-adjacency are the same arrays.
//
// The container carries no algorithm. Everything that walks it lives in
// [github.com/stergiotis/boxer/public/analytics/graph/engine] and
// [github.com/stergiotis/boxer/public/analytics/graph/algo].
package csr
