package imztop

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmreplay"
)

// Coverage state for the availability strip (ADR-0197 §SD9).
//
// It is process-wide because the session it describes is, and because every
// open window would otherwise ask the same server the same question about the
// same host. The queries run on their own goroutine: coverage is a database
// read, and the render thread reads only the slice it left behind.

// coverageRefreshGap is the minimum wall-clock spacing between queries. Panning
// the strip changes the view every frame; without a floor that would be a query
// per frame, all but the last of them wasted.
const coverageRefreshGap = 400 * time.Millisecond

// coverageSlackFraction is how far the view may move before the fetched runs
// stop being good enough. Coverage is fetched for a padded range, so small
// pans stay inside what is already loaded and cost nothing.
const coverageSlackFraction = 0.25

// coveragePadFraction is how much extra range each query fetches either side of
// the view, so a pan has somewhere to go before it needs a refetch.
const coveragePadFraction = 0.5

var (
	coverageMu sync.Mutex
	// coverageRuns is what the strip draws. Replaced wholesale by a completed
	// query; readers take the slice header under the lock and never mutate it.
	coverageRuns []sysmreplay.CoverageRun
	// coverageFrom/To is the range coverageRuns describes — the padded range
	// that was queried, not the view that prompted it.
	coverageFrom time.Time
	coverageTo   time.Time
	coverageErr  error
	// coverageInFlight keeps two windows from firing the same query at once.
	coverageInFlight bool
	coverageLastAt   time.Time

	// previewPoints is the load strip: mean CPU busy per bin over the same
	// range coverage was fetched for. Loaded by the same background pass, so
	// the two layers of the strip never disagree about what span they describe.
	previewPoints []sysmreplay.PreviewPoint
)

// resetCoverage drops everything a previous session loaded. Called when replay
// ends so a new session cannot briefly draw the old host's availability.
func resetCoverage() {
	coverageMu.Lock()
	coverageRuns, coverageErr = nil, nil
	previewPoints = nil
	coverageFrom, coverageTo = time.Time{}, time.Time{}
	coverageInFlight = false
	coverageLastAt = time.Time{}
	coverageMu.Unlock()
}

// coverageSnapshot is what a renderer reads: the runs and whether they cover
// the range being drawn.
func coverageSnapshot() (runs []sysmreplay.CoverageRun, from, to time.Time, err error) {
	coverageMu.Lock()
	runs, from, to, err = coverageRuns, coverageFrom, coverageTo, coverageErr
	coverageMu.Unlock()
	return
}

// ensureCoverage kicks a background query when the view has moved outside what
// is loaded. It never blocks: the caller is the render thread, and the answer
// arrives for a later frame.
func ensureCoverage(src *StoreSource, viewFrom, viewTo time.Time) {
	if src == nil || !viewTo.After(viewFrom) {
		return
	}
	coverageMu.Lock()
	if coverageInFlight || coverageNeedsNoRefresh(viewFrom, viewTo) {
		coverageMu.Unlock()
		return
	}
	coverageInFlight = true
	coverageLastAt = time.Now()
	coverageMu.Unlock()

	span := viewTo.Sub(viewFrom)
	pad := time.Duration(float64(span) * coveragePadFraction)
	from, to := viewFrom.Add(-pad), viewTo.Add(pad)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		runs, err := src.Coverage(ctx, sysmreplay.Window{From: from, To: to}, 0)
		if err != nil {
			log.Warn().Err(err).Msg("imztop: coverage query failed")
		}
		// The preview is a section read and the coverage a envelope one, so
		// this one can fail on its own; a failed preview leaves the
		// availability bands standing rather than blanking both.
		points, perr := src.Preview(ctx, sysmreplay.Window{From: from, To: to}, 0)
		if perr != nil {
			log.Warn().Err(perr).Msg("imztop: load preview query failed")
		}

		coverageMu.Lock()
		coverageInFlight = false
		coverageErr = err
		if err == nil {
			coverageRuns, coverageFrom, coverageTo = runs, from, to
		}
		if perr == nil {
			previewPoints = points
		}
		coverageMu.Unlock()
	}()
}

// seedCoverage installs availability and load directly, bypassing the query.
//
// It exists for the screenshot tour (ADR-0197 M5), which has no database and
// must still capture the strip with something on it. Keeping it here rather
// than reaching into the variables from the tour file means the locking stays
// in one place, and the seam is visible to anyone reading the coverage state.
func seedCoverage(runs []sysmreplay.CoverageRun, points []sysmreplay.PreviewPoint, from, to time.Time) {
	coverageMu.Lock()
	coverageRuns, previewPoints = runs, points
	coverageFrom, coverageTo = from, to
	coverageErr = nil
	// Far enough ahead that the background pass will not immediately replace
	// the seed with an empty answer from a source that does not exist.
	coverageLastAt = time.Now()
	coverageMu.Unlock()
}

// previewSnapshot is what the strip's rug reads.
func previewSnapshot() (points []sysmreplay.PreviewPoint) {
	coverageMu.Lock()
	points = previewPoints
	coverageMu.Unlock()
	return
}

// coverageNeedsNoRefresh reports whether the loaded range still covers the view
// with enough slack. Called under coverageMu.
func coverageNeedsNoRefresh(viewFrom, viewTo time.Time) (fresh bool) {
	if time.Since(coverageLastAt) < coverageRefreshGap {
		return true // rate-limited; the pan is still in progress
	}
	if coverageFrom.IsZero() || coverageTo.IsZero() {
		return false
	}
	span := viewTo.Sub(viewFrom)
	if span <= 0 {
		return true
	}
	slack := time.Duration(float64(span) * coverageSlackFraction)
	// Fresh while the view plus its slack still sits inside what was fetched.
	fresh = !viewFrom.Add(-slack).Before(coverageFrom) && !viewTo.Add(slack).After(coverageTo)
	return
}
