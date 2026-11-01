package logviewer

import (
	"context"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
)

// The window's cache (ADR-0345, the update of 2026-10-30). Rows come from
// boxer.facts through the source's ReadFunc, on a bgjob worker, never on
// the render goroutine: the frame starts a refresh, the worker reads, and a
// later frame adopts the result. Everything here but the worker's read runs
// on the render goroutine.

const (
	// lookBack bounds every read, as the app trail's tables are bounded
	// (ADR-0260 §SD5): a scope over every run must not scan a trail that
	// grows for as long as the store is kept.
	lookBack = 7 * 24 * time.Hour
	// readTimeout bounds one read of the store.
	readTimeout = 15 * time.Second
	// scopeSettle is how long a changed filter must stand before it is
	// read, so typing in a filter box reads once, not once per keystroke.
	scopeSettle = 300 * time.Millisecond
)

// cacheFill is one refresh's result: the rows read under scope, newest first.
type cacheFill struct {
	scope filterState
	rows  []Entry
	at    time.Time
}

// logCache is the rows a window holds. rows is replaced whole by a fill and
// never written in place, so a snapshot may share it.
type logCache struct {
	runner bgjob.Runner[cacheFill]

	rows        []Entry
	scope       filterState
	filled      bool
	refreshedAt time.Time
	gen         uint64

	// lastErr is the last refresh's failure, cleared by the next fill.
	lastErr error

	// inFlight is the scope of the read the runner holds; started is
	// when it began.
	inFlight filterState
	started  time.Time
	// want is the scope the window last asked for, and wantSince when.
	want      filterState
	wantSince time.Time
	// refreshNow is a Refresh click waiting for the runner to be free.
	refreshNow bool
}

// tick adopts a finished read and starts the next one: when the scope
// changed and has settled, when Refresh was asked, or when the interval
// has passed since the last read began.
func (inst *logCache) tick(now time.Time, src *Source, want filterState) {
	if res, _, ok := inst.runner.TakeResult(); ok && res.scope == want {
		inst.rows, inst.scope, inst.filled, inst.refreshedAt, inst.lastErr = res.rows, res.scope, true, res.at, nil
		inst.gen++
	}
	if snap := inst.runner.Snapshot(); snap.State == bgjob.StateFailed {
		inst.lastErr = snap.Err
	}
	if !src.persisted() {
		return
	}
	if want != inst.want {
		inst.want, inst.wantSince = want, now
	}
	running := inst.runner.Running()
	switch {
	case !inst.filled || inst.scope != want:
		if running && inst.inFlight == want {
			return
		}
		if inst.lastErr != nil && inst.inFlight == want && now.Sub(inst.started) < refreshInterval() {
			// A failed read of this scope is retried on the interval, not
			// on every frame.
			return
		}
		if inst.filled && now.Sub(inst.wantSince) < scopeSettle {
			return
		}
		inst.start(now, src, want)
	case running:
	case inst.refreshNow || now.Sub(inst.started) >= refreshInterval():
		inst.start(now, src, want)
	}
}

// start supersedes any read in flight and reads want.
func (inst *logCache) start(now time.Time, src *Source, want filterState) {
	if inst.runner.Running() {
		inst.runner.Invalidate()
	}
	q := buildQuery(want, src.run(), now, cacheCap())
	read := src.Read
	inst.inFlight, inst.started, inst.refreshNow = want, now, false
	inst.runner.Start(nil, bgjob.Spec{Kind: "logviewer.refresh", Title: "Log viewer refresh"},
		func(ctx context.Context) (out *cacheFill, err error) {
			ctx, cancel := context.WithTimeout(ctx, readTimeout)
			defer cancel()
			rows, err := read(ctx, q)
			if err != nil {
				return
			}
			out = &cacheFill{scope: want, rows: rows, at: time.Now()}
			return
		})
}

// close abandons a read in flight; its result is dropped.
func (inst *logCache) close() {
	inst.runner.Invalidate()
}

// buildQuery turns a window's filter into the store's query: every filter
// the store can apply goes into the read, so the row cap counts matching
// rows.
func buildQuery(f filterState, runId string, now time.Time, limit int) (q Query) {
	q = Query{Since: now.Add(-lookBack), App: f.AppId, Message: f.Message, InstanceKey: f.Instance, Limit: limit}
	if !f.AllRuns {
		q.RunId = runId
	}
	if f.Level > zerolog.TraceLevel {
		for _, l := range levelOptions {
			if l >= f.Level {
				q.Levels = append(q.Levels, l.String())
			}
		}
	}
	return
}

// covers reports whether every row a query with these arguments would
// keep is a row the cache's scope kept, so filtering the cache answers it
// in full. Each argument must be at least as narrow as the scope's: a
// level floor no lower, and a substring that contains the scope's.
func covers(scope filterState, level zerolog.Level, app, msg string) bool {
	return level >= scope.Level &&
		strings.Contains(strings.ToLower(app), strings.ToLower(scope.AppId)) &&
		strings.Contains(strings.ToLower(msg), strings.ToLower(scope.Message))
}
