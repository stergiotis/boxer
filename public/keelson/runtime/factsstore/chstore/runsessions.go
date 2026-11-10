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
	e.id = idCol
	e.tsSec = fmt.Sprintf("toUnixTimestamp(%s)", tsCol)
	e.appId = pickLcrString(symValue, symLR, symLRCard, vocab.MembRuntimeApp.GetId().Value())
	e.runId = pickLcrString(symValue, symLR, symLRCard, vocab.MembRuntimeRun.GetId().Value())
	e.phase = pickLcrString(symValue, symLR, symLRCard, vocab.MembLifecyclePhase.GetId().Value())
	e.stopReason = pickLcrString(strValue, strLR, strLRCard, vocab.MembLifecycleStopReason.GetId().Value())
	e.tileKey = pickLcrNumeric(u64Value, u64LR, u64LRCard, vocab.MembLifecycleTileKey.GetId().Value(), "0")
	return
}
