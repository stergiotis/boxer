package runtime

import (
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
)

// The slot keys of ADR-0207 SD2. A tagged section's *group* is its value
// columns' canonical types, stable-sorted by CT string and joined by
// canonicaltypes.GroupSeparator; a co-section group's *signature* is its
// member sections' groups sorted bytewise and joined by
// canonicaltypes.SignatureSeparator, and a standalone section's signature is
// its group. Column names, section names, co-group and streaming-group names,
// use-aspects, value aspects and encoding hints appear in neither.
//
// The empty group and the empty signature both render as "" — the value-less
// section's key. canonicaltypes' own producers cannot be used for that case:
// GroupAstNode.String() and SignatureAstNode.String() render a member-less
// node as "<invalid:empty>", which is deliberately unparseable, so the empty
// case is special-cased here and the non-empty case joins with the same two
// exported separators the producers use (TestGroupMatchesAstProducer pins the
// agreement).

// GroupOf computes a section's group from its value-column canonical types.
//
// order is the permutation the sort applied: order[k] is the index, in cts, of
// the column that lands at key position k. Columns of equal CT keep their
// declaration order (the sort is stable), which is what makes a lat/lng pair
// survive the canonicalisation — ADR-0207 QOC, criterion C3.
//
// A value-less section has no columns: group is "" and order is empty.
func GroupOf(cts []canonicaltypes.PrimitiveAstNodeI) (group string, order []int) {
	n := len(cts)
	order = make([]int, 0, n)
	if n == 0 {
		return
	}
	strs := make([]string, n)
	for i, ct := range cts {
		strs[i] = ct.String()
		order = append(order, i)
	}
	slices.SortStableFunc(order, func(a int, b int) int { return strings.Compare(strs[a], strs[b]) })
	var sb strings.Builder
	sb.Grow(n * 6)
	for k, i := range order {
		if k > 0 {
			sb.WriteString(canonicaltypes.GroupSeparator)
		}
		sb.WriteString(strs[i])
	}
	group = sb.String()
	return
}

// PlainGroupOf computes a plain section's group. It is GroupOf under a name
// that says which side of SD2 it serves: the plain group is not on the wire
// (plains are keyed by their PlainItemTypeE ordinal) and is compared for
// equality at decoder construction instead.
func PlainGroupOf(cts []canonicaltypes.PrimitiveAstNodeI) (group string, order []int) {
	return GroupOf(cts)
}

// SignatureOf computes a slot's signature from its member sections' groups.
//
// order is the permutation the sort applied: order[k] is the index, in groups,
// of the section that lands at signature position k. Equal groups keep their
// declaration order, so the two `s` sections of a co-section group stay
// distinguishable by position.
//
// A standalone section's signature is its group: SignatureOf of one group
// returns that group unchanged, including the empty one.
func SignatureOf(groups []string) (sig string, order []int) {
	n := len(groups)
	order = make([]int, 0, n)
	if n == 0 {
		return
	}
	for i := range groups {
		order = append(order, i)
	}
	slices.SortStableFunc(order, func(a int, b int) int { return strings.Compare(groups[a], groups[b]) })
	var sb strings.Builder
	sb.Grow(n * 8)
	for k, i := range order {
		if k > 0 {
			sb.WriteString(canonicaltypes.SignatureSeparator)
		}
		sb.WriteString(groups[i])
	}
	sig = sb.String()
	return
}
