package trailviewer

import (
	"time"

	"github.com/stergiotis/boxer/public/config/env"
)

// The window's settings (ADR-0350 §SD3, §SD5, proposed): how often it
// re-reads the marts, how many rows it keeps, and how far back the
// previous-state read looks.
var RefreshEnv = env.NewDuration(env.Spec{
	Name:        "BOXER_TRAILVIEWER_REFRESH",
	Default:     "5s",
	Description: "how often an open trail viewer window re-reads its case from the trail marts on boxer.facts in the background; the Refresh button reads at once; under 1s reads every second",
	Category:    env.CategoryObservability,
})

var RowsEnv = env.NewInt(env.Spec{
	Name:        "BOXER_TRAILVIEWER_ROWS",
	Default:     "2000",
	Description: "how many of the newest rows of its case and window a trail viewer window reads at once; reaching the end of the table reads the next as many; clamped to 100..100000",
	Category:    env.CategoryObservability,
})

var LookbackEnv = env.NewDuration(env.Spec{
	Name:        "BOXER_TRAILVIEWER_LOOKBACK",
	Default:     "720h",
	Description: "how far before an audit event the trail viewer looks for the previous event of the same entity, which its previous-state diff compares against; an entity whose last change is older shows no before-state; at least 1m",
	Category:    env.CategoryObservability,
})

const (
	minRefresh  = time.Second
	minRows     = 100
	maxRows     = 100000
	minLookback = time.Minute
)

func refreshInterval() (d time.Duration) {
	d = max(RefreshEnv.Get(), minRefresh)
	return
}

func rowCap() (n int) {
	n = int(min(max(RowsEnv.Get(), minRows), maxRows))
	return
}

func lookback() (d time.Duration) {
	d = max(LookbackEnv.Get(), minLookback)
	return
}
