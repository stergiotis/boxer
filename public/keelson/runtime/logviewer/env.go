package logviewer

import (
	"time"

	"github.com/stergiotis/boxer/public/config/env"
)

// RefreshEnv and RowsEnv are the window's cache settings (ADR-0345, the
// update of 2026-10-30): how often it re-reads boxer.facts, and how many
// rows it keeps.
var RefreshEnv = env.NewDuration(env.Spec{
	Name:        "BOXER_LOGVIEWER_REFRESH",
	Default:     "5s",
	Description: "how often an open log viewer window re-reads its rows from boxer.facts in the background; the Refresh button reads at once; under 1s reads every second",
	Category:    env.CategoryObservability,
})

var RowsEnv = env.NewInt(env.Spec{
	Name:        "BOXER_LOGVIEWER_ROWS",
	Default:     "2000",
	Description: "how many of the newest matching log rows a log viewer window keeps in memory and shows; bounds each read from boxer.facts; clamped to 100..100000",
	Category:    env.CategoryObservability,
})

const (
	minRefresh = time.Second
	minRows    = 100
	maxRows    = 100000
)

func refreshInterval() (d time.Duration) {
	d = max(RefreshEnv.Get(), minRefresh)
	return
}

func cacheCap() (n int) {
	n = int(min(max(RowsEnv.Get(), minRows), maxRows))
	return
}
