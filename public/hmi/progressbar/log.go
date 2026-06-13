package progressbar

import (
	"fmt"
	"io"
)

const ansiEraseLine = "\x1b[2K"

// LogWriter returns an io.Writer whose output is serialised with the bar's
// render loop via inst.writeMu. Point a logger at it so log lines land on
// their own terminal rows instead of tangling with the \r-based progress
// line:
//
//	bar := progressbar.New(total, "items")
//	log.Logger = log.Output(bar.LogWriter())
//	bar.Start(ctx)
//
// Each Write is treated as one line-oriented message: on a TTY we first
// emit \r + erase-line so the in-place bar frame disappears, then the
// payload, then a '\n' if the payload didn't already end with one. The
// next render tick (≤250 ms later) repaints the bar on a fresh row below
// the log line.
//
// In non-TTY mode the writer is a straight passthrough aside from the
// trailing-newline guarantee.
//
// The trailing-newline guarantee assumes callers pass one complete event
// per Write. Standard logger writers (log.Logger, zerolog.ConsoleWriter,
// slog's text/JSON handlers) all satisfy this.
func (inst *Bar) LogWriter() (w io.Writer) {
	return &barLogWriter{bar: inst}
}

// Println writes one line through LogWriter — convenience for ad-hoc
// messages that don't go through a logger.
func (inst *Bar) Println(args ...any) {
	_, _ = fmt.Fprintln(inst.LogWriter(), args...)
}

// Printf formats through LogWriter. A trailing newline is appended by
// LogWriter if the formatted output doesn't already end with one.
func (inst *Bar) Printf(format string, args ...any) {
	_, _ = fmt.Fprintf(inst.LogWriter(), format, args...)
}

type barLogWriter struct {
	bar *Bar
}

func (inst *barLogWriter) Write(p []byte) (n int, err error) {
	bar := inst.bar
	bar.writeMu.Lock()
	defer bar.writeMu.Unlock()

	if bar.isTTY {
		if _, err = fmt.Fprint(bar.w, "\r", ansiEraseLine); err != nil {
			return 0, err
		}
	}
	n, err = bar.w.Write(p)
	if err != nil {
		return n, err
	}
	if n == len(p) && n > 0 && p[n-1] != '\n' {
		_, _ = bar.w.Write([]byte("\n"))
	}
	return n, nil
}
