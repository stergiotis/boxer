// Package co provides operations over co-indexed parallel slices —
// separate slices whose elements correspond by position (a
// struct-of-arrays layout): sorting a lead slice while keeping
// companions aligned (CoSortSlices), sorted insertion and merge over a
// key slice plus a value slice, grouped iteration over key runs, and
// co-filtered iteration.
//
// Length preconditions are the caller's responsibility: companion
// slices must be at least as long as the lead slice, and shorter ones
// panic at first out-of-range access. Package ragged is the
// complementary choice when inputs may legitimately differ in length.
package co
