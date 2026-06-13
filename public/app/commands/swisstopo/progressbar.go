package swisstopo

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/hmi/progressbar"
)

// mirrorProgress wraps progressbar.Bar with download-specific counters.
type mirrorProgress struct {
	bar           *progressbar.Bar
	downloaded    atomic.Int64
	existed       atomic.Int64
	errors        atomic.Int64
	bytesReceived atomic.Int64
}

func newMirrorProgress(total int64, label string) (mp *mirrorProgress) {
	mp = &mirrorProgress{}
	mp.bar = progressbar.New(total, label)
	mp.bar.SetDetail(mp.detail)
	return
}

func (inst *mirrorProgress) detail(processed int64, total int64) string {
	dl := inst.downloaded.Load()
	ex := inst.existed.Load()
	er := inst.errors.Load()
	by := inst.bytesReceived.Load()
	if total <= 0 {
		return ""
	}
	return fmt.Sprintf("dl:%d ex:%d err:%d  %s", dl, ex, er, progressbar.FormatBytes(by))
}

func (inst *mirrorProgress) Start(ctx context.Context) { inst.bar.Start(ctx) }
func (inst *mirrorProgress) Stop()                     { inst.bar.Stop() }
func (inst *mirrorProgress) Tick()                     { inst.bar.Tick() }
func (inst *mirrorProgress) Add(n int64)               { inst.bar.Add(n) }

// LogWriter exposes the bar's coordinated log writer so callers can route
// a logger through it without reaching into inst.bar.
func (inst *mirrorProgress) LogWriter() (w io.Writer) { return inst.bar.LogWriter() }

// routeLogsThrough swaps the zerolog global logger for a ConsoleWriter
// whose Out is w (so human-readable formatting is preserved) and returns
// a restore closure. Use it around a bar's Start/Stop so log lines from
// anywhere in the call chain — including deep retry warnings — don't
// collide with the \r-based progress frames:
//
//	restore := routeLogsThrough(bar.LogWriter())
//	defer restore()
//
// The swapped logger uses zerolog defaults (colored, RFC3339Nano time),
// which approximates the app's --logFormat=console setup; the original
// logger is restored verbatim.
func routeLogsThrough(w io.Writer) (restore func()) {
	orig := log.Logger
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: w})
	return func() { log.Logger = orig }
}
