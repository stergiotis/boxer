package chstore

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
)

// columnExprsLifecycle gathers the projections of an app-lifecycle row that
// the app-trail and launch-statistics reads share.
type columnExprsLifecycle struct {
	id         string
	tsSec      string
	appId      string
	runId      string
	phase      string
	stopReason string
	tileKey    string
}

func buildLifecycleColumnExprs() (e columnExprsLifecycle) {
	const (
		symValue  = "`tv:symbol:value:val:s:124::I:0::data`"
		symLR     = "`tv:symbol:lr:lr:u64:1247:::0::data`"
		symLRCard = "`tv:symbol:lrcard:lrcard:u64:4E:::0::data`"
		strValue  = "`tv:stringArray:value:val:sh:4::8:0::data`"
		strLR     = "`tv:stringArray:lr:lr:u64:1247:::0::data`"
		strLRCard = "`tv:stringArray:lrcard:lrcard:u64:4E:::0::data`"
		u64Value  = "`tv:u64Array:value:val:u64h:4:::0::data`"
		u64LR     = "`tv:u64Array:lr:lr:u64:1247:::0::data`"
		u64LRCard = "`tv:u64Array:lrcard:lrcard:u64:4E:::0::data`"
		idCol     = "`id:id:u64:47::0:`"
		tsCol     = "`ts:ts:z64:47::0:`"
	)
	e.id = idCol
	e.tsSec = fmt.Sprintf("toUnixTimestamp(%s)", tsCol)
	e.appId = pickLcrString(symValue, symLR, symLRCard, vocab.MembRuntimeApp.GetId().Value())
	e.runId = pickLcrString(symValue, symLR, symLRCard, vocab.MembRuntimeRun.GetId().Value())
	e.phase = pickLcrString(symValue, symLR, symLRCard, vocab.MembLifecyclePhase.GetId().Value())
	e.stopReason = pickLcrString(strValue, strLR, strLRCard, vocab.MembLifecycleStopReason.GetId().Value())
	e.tileKey = pickLcrNumeric(u64Value, u64LR, u64LRCard, vocab.MembLifecycleTileKey.GetId().Value(), "0")
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
	const (
		symValue  = "`tv:symbol:value:val:s:124::I:0::data`"
		symLR     = "`tv:symbol:lr:lr:u64:1247:::0::data`"
		symLRCard = "`tv:symbol:lrcard:lrcard:u64:4E:::0::data`"
	)
	return pickLcrString(symValue, symLR, symLRCard, membershipId) + " = " + quoteSqlString(value)
}
