//go:build llm_generated_opus47

package h3

// Status triage helpers for the per-element [StatusE] slices returned by
// bulk methods. The bulk API deliberately pushes per-element decisions
// onto callers (drop, surface, compute-anyway); these helpers cover the
// common "any failure is a hard error" pattern without forcing every
// caller to reimplement the loop.

// AnyFailure reports whether any status in the slice is not [StatusOk].
// Cheap: returns on the first non-Ok element. Semantically equivalent to
// `_, _, ok := FirstFailure(status); ok` but easier to inline at call
// sites that only need the boolean.
func AnyFailure(statuses []StatusE) (failed bool) {
	for _, s := range statuses {
		if s != StatusOk {
			failed = true
			return
		}
	}
	return
}

// FirstFailure returns the index and code of the first non-[StatusOk]
// entry. `ok` is true when a failure was found; false means every entry
// is StatusOk.
//
// Typical use:
//
//	idx, code, ok := h3.FirstFailure(status)
//	if ok {
//	    err = eb.Build().Int("idx", idx).Stringer("code", code).
//	        Errorf("h3: bulk op reported failure")
//	    return
//	}
func FirstFailure(statuses []StatusE) (idx int, code StatusE, ok bool) {
	for i, s := range statuses {
		if s != StatusOk {
			idx = i
			code = s
			ok = true
			return
		}
	}
	return
}

// CountFailures returns the number of non-[StatusOk] entries. O(n) scan;
// exists for callers that need to distinguish "some failed" from "all
// failed" or want to report failure rates. Callers that only care
// whether *any* failure occurred should prefer [AnyFailure] — it short-
// circuits on the first non-Ok entry.
func CountFailures(statuses []StatusE) (n int) {
	for _, s := range statuses {
		if s != StatusOk {
			n++
		}
	}
	return
}
