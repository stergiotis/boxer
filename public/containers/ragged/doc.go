// Package ragged provides zip iterators that tolerate length-mismatched
// ("ragged") inputs by stopping at the shorter side: [Zip2] for two
// slices, and [Zip2L], [Zip2R], [Zip2LR] where the suffix letters mark
// which operand positions are lazy (iter.Seq-valued).
//
// A lazy operand may be pulled one element past the point where the
// other side ends; the per-function docs state the exact pull-count
// contract, which matters when zipping single-use or side-effecting
// sequences (such as the fffi2 stream-backed sequences).
package ragged
