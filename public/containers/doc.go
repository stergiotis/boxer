// Package containers holds small generic in-memory containers:
//
//   - [BinarySearchGrowingKV] — a sorted-iteration key-value container
//     over parallel slices with deferred-batch writes, range reads and
//     a read-free [BinarySearchGrowingKVBuilder]. Preferred over
//     map[K]V when iteration must be deterministic and sorted, when K
//     is not comparable, or when a custom comparator is needed.
//   - [HashSet] — a map-backed set with in-place set algebra
//     (UnionMod, DifferenceMod, IntersectMod) and Clone for the
//     non-destructive forms.
//   - [Stack] — a slice-backed LIFO stack.
//
// None of the types is safe for concurrent use; callers serialise
// externally.
//
// Subpackage co operates on co-indexed parallel slices and panics on
// length mismatches; subpackage ragged zips length-mismatched inputs by
// stopping at the shorter side.
package containers
