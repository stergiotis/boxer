//go:build llm_generated_opus47

package logbridge

import (
	"io"

	"github.com/rs/zerolog"
)

// NewLogger builds a process-wide zerolog.Logger that fans every event
// out to two destinations: the existing baseWriter (typically
// os.Stdout / a file / zerolog.ConsoleWriter for operator-facing logs)
// and the supplied Sink, which CBOR-decodes the zerolog event into
// a LogRow that downstream lands in `runtime.facts` as a RowBinary row
// via chstore. The returned logger has no AppId tagging; per-app
// loggers are derived via app.AppLogger(returnedLogger, appId).
//
// A nil baseWriter routes events through the Sink only. A nil sink is
// also tolerated (the caller might disable fact capture in a test) and
// yields a plain logger over baseWriter.
//
// The composition uses zerolog.MultiLevelWriter so each writer in the
// chain receives the event with its level out-of-band — the Sink's
// MinLevel filter applies as configured at NewSink time without
// affecting baseWriter.
func NewLogger(baseWriter io.Writer, sink *Sink) (logger zerolog.Logger) {
	switch {
	case baseWriter == nil && sink == nil:
		logger = zerolog.Nop()
	case baseWriter == nil:
		logger = zerolog.New(sink).With().Timestamp().Logger()
	case sink == nil:
		logger = zerolog.New(baseWriter).With().Timestamp().Logger()
	default:
		w := zerolog.MultiLevelWriter(toLevelWriter(baseWriter), sink)
		logger = zerolog.New(w).With().Timestamp().Logger()
	}
	return
}

// toLevelWriter promotes a plain io.Writer into a zerolog.LevelWriter
// for use inside MultiLevelWriter. zerolog provides this exact wrapping
// internally but does not export it; reproducing it here keeps the
// returned writer type concrete and avoids a per-event interface
// allocation in the multi-writer fan-out.
func toLevelWriter(w io.Writer) (lw zerolog.LevelWriter) {
	if existing, ok := w.(zerolog.LevelWriter); ok {
		lw = existing
		return
	}
	lw = levelAdapter{w: w}
	return
}

type levelAdapter struct{ w io.Writer }

var _ zerolog.LevelWriter = levelAdapter{}

func (inst levelAdapter) Write(p []byte) (n int, err error) {
	n, err = inst.w.Write(p)
	return
}

func (inst levelAdapter) WriteLevel(_ zerolog.Level, p []byte) (n int, err error) {
	n, err = inst.w.Write(p)
	return
}
