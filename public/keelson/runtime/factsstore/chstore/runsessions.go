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
	e.id = hId
	e.tsSec = fmt.Sprintf("toUnixTimestamp(%s)", hTs)
	e.appId = sym(vocab.MembRuntimeApp)
	e.runId = sym(vocab.MembRuntimeRun)
	e.phase = sym(vocab.MembLifecyclePhase)
	e.stopReason = str(vocab.MembLifecycleStopReason)
	e.tileKey = u64(vocab.MembLifecycleTileKey)
	return
}
