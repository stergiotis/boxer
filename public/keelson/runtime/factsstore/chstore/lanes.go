package chstore

import "fmt"

// The physical columns of the facts table the reads here draw on, and the
// lookups over them, in one place (ADR-0367 §SD5): a change to the envelope
// or to a section's lanes is made in this file.
//
// A section has a value column with one entry per attribute, and beside it
// the lanes that say which memberships each attribute carries. `lr` holds the
// low-card-ref membership ids of the row, flattened; `lrcard` is parallel to
// the value column and counts how many of them each attribute carries. An
// array-valued section adds `len`, the length of each attribute's run in the
// flattened value column.
const (
	idCol = "`id:id:u64:47::0:`"
	tsCol = "`ts:ts:z64:47::0:`"

	symValue  = "`tv:symbol:value:val:s:124::I:0::data`"
	symLR     = "`tv:symbol:lr:lr:u64:1247:::0::data`"
	symLRCard = "`tv:symbol:lrcard:lrcard:u64:4E:::0::data`"

	strValue  = "`tv:stringArray:value:val:sh:4::8:0::data`"
	strLen    = "`tv:stringArray:len:len:u64:4D:::0::data`"
	strLR     = "`tv:stringArray:lr:lr:u64:1247:::0::data`"
	strLRCard = "`tv:stringArray:lrcard:lrcard:u64:4E:::0::data`"

	textValue  = "`tv:textArray:value:val:sh:5::7:0::data`"
	textLen    = "`tv:textArray:len:len:u64:4D:::0::data`"
	textLR     = "`tv:textArray:lr:lr:u64:1247:::0::data`"
	textLRCard = "`tv:textArray:lrcard:lrcard:u64:4E:::0::data`"

	u64Value  = "`tv:u64Array:value:val:u64h:4:::0::data`"
	u64Len    = "`tv:u64Array:len:len:u64:4D:::0::data`"
	u64LR     = "`tv:u64Array:lr:lr:u64:1247:::0::data`"
	u64LRCard = "`tv:u64Array:lrcard:lrcard:u64:4E:::0::data`"
)

// pickLcrList is the values of the attribute under membershipId on an
// array-valued section, read on the plain channel: the attribute that owns
// the membership's position in the lr lane (what LW_RAGGED_PARENT_IDS
// computes over lrcard), then its run of the flattened value array, located
// by the per-attribute len column (what LW_LIST_BY_TAG_EQUAL does); [] when
// the row has no such attribute.
func pickLcrList(valueArr, lenArr, lrArr, lrCardArr string, membershipId uint64) (expr string) {
	attr := fmt.Sprintf("arrayElement(arrayFlatten(arrayMap((i, c) -> arrayWithConstant(c, i), arrayEnumerate(%[1]s), %[1]s)), indexOf(%[2]s, %[3]d))",
		lrCardArr, lrArr, membershipId)
	return fmt.Sprintf("if(%[1]s > 0, arraySlice(%[2]s, arraySum(arraySlice(%[3]s, 1, %[1]s - 1)) + 1, arrayElement(%[3]s, %[1]s)), [])",
		attr, valueArr, lenArr)
}

// ownerOf maps each position of a membership lane (lr or lmr) to the
// 1-based attribute that carries it, by expanding the per-attribute card
// column: the same arithmetic as LW_RAGGED_PARENT_IDS. Unlike the
// cumulative-sum lookup in pickLcrString, it stays right when one
// attribute carries more than one membership on the lane.
func ownerOf(cardArr string) string {
	return fmt.Sprintf("arrayFlatten(arrayMap((i, c) -> arrayWithConstant(c, i), arrayEnumerate(%[1]s), %[1]s))", cardArr)
}

// pickScalarAttr is the value of the attribute that carries membershipId on
// the plain lane of a scalar section (one value per attribute); zero when
// the row has none.
func pickScalarAttr(valueArr, lrArr, lrCardArr string, membershipId uint64, zero string) string {
	idx := fmt.Sprintf("indexOf(%s, %d)", lrArr, membershipId)
	return fmt.Sprintf("if(%[1]s > 0, arrayElement(%[2]s, arrayElement(%[3]s, %[1]s)), %[4]s)",
		idx, valueArr, ownerOf(lrCardArr), zero)
}

// pickLcrString emits the expression that returns the value carried by
// the attribute tagged with low-card-ref `membershipId`. The leeway
// encoding is non-trivial: `value` has one entry per attribute, but
// `lr` only contains the LCR membership ids — different sizes (any
// attribute can also be HCR/LMR-tagged). The `lrcard` array is
// parallel to `value` and counts LCR memberships at each position; the
// running prefix-sum of lrcard yields the lr-index for each value
// position. Looking up our membership id in lr gives an lr-index;
// finding that index in the cumulative-sum array yields the value
// position. The whole pipeline is wrapped in an `if` on `indexOf > 0`
// falling back to the empty string, because `indexOf(lr, missing) = 0`
// would otherwise lead arrayElement to misbehave on edge cases.
func pickLcrString(valueArr, lrArr, lrCardArr string, membershipId uint64) (expr string) {
	idxInLr := fmt.Sprintf("indexOf(%s, %d)", lrArr, membershipId)
	expr = fmt.Sprintf("if(%s > 0, arrayElement(%s, indexOf(arrayCumSum(%s), %s)), '')",
		idxInLr, valueArr, lrCardArr, idxInLr)
	return
}

// pickLcrNumeric is the numeric counterpart to pickLcrString: same
// cumulative-sum lookup, but the not-found default is a caller-chosen
// constant (e.g. "0" for u64, "false" for bool) rather than the empty
// string.
func pickLcrNumeric(valueArr, lrArr, lrCardArr string, membershipId uint64, zero string) (expr string) {
	idxInLr := fmt.Sprintf("indexOf(%s, %d)", lrArr, membershipId)
	expr = fmt.Sprintf("if(%s > 0, arrayElement(%s, indexOf(arrayCumSum(%s), %s)), %s)",
		idxInLr, valueArr, lrCardArr, idxInLr, zero)
	return
}

// symbolEquals is "the row's symbol attribute under membershipId equals
// value", on the plain channel.
func symbolEquals(membershipId uint64, value string) (expr string) {
	return pickLcrString(symValue, symLR, symLRCard, membershipId) + " = " + quoteSqlString(value)
}
