// Package fairlead is the one opening every SQL statement passes on its way
// out of the process (ADR-0367 §SD1). A fairlead on a ship is the fitting a
// line is led through, so that it runs from one known place however it was
// made up on deck.
//
// An exit point — the function that hands statement text to an HTTP request
// or to a child process — calls [Lead] with the text it is about to send and
// sends what comes back. The passes installed with [Install] see the
// statement there, in order: each may let it through, replace its text, or
// refuse it. With nothing installed [Lead] returns its argument, and that is
// the state of a process nobody configured.
//
// The package states where statements leave and offers the opening. What a
// pass does there — record, place, rewrite, refuse — is the installer's
// policy, and a pass that needs to know what a statement is asks
// [github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis.Classify].
// Nothing here parses.
package fairlead

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ExitE says what a statement leaves the process for.
type ExitE uint8

const (
	ExitUnknown ExitE = iota
	// ExitServer — a database server reached over the network.
	ExitServer
	// ExitLocal — a query process this process started.
	ExitLocal
)

var AllExits = []ExitE{ExitUnknown, ExitServer, ExitLocal}

func (inst ExitE) String() (s string) {
	switch inst {
	case ExitServer:
		s = "server"
	case ExitLocal:
		s = "local"
	default:
		s = "unknown"
	}
	return
}

// Statement is one statement at the moment it leaves.
type Statement struct {
	// SQL is the text about to be sent, complete: no later step adds to it.
	SQL  string
	Exit ExitE
	// Site names the exit point, `<package>.<what>`. It is stable across
	// refactors of the function that holds it, so a journal taken on one
	// build can be read against another.
	Site string
}

// PassI is one step at the fairlead.
type PassI interface {
	// Lead sees st before it leaves and returns the text to send in its
	// place; returning st.SQL lets the statement through unchanged. A
	// non-nil err refuses the statement: it is not sent, and the exit point
	// returns the error to its caller.
	//
	// Lead runs on the caller's goroutine, on every statement, and must be
	// safe for concurrent use.
	Lead(ctx context.Context, st Statement) (sql string, err error)
}

// PassFunc adapts a function to [PassI].
type PassFunc func(ctx context.Context, st Statement) (sql string, err error)

var _ PassI = PassFunc(nil)

func (inst PassFunc) Lead(ctx context.Context, st Statement) (sql string, err error) {
	return inst(ctx, st)
}

// installation is one Install call. Passes are compared by installation, not
// by value: a PassFunc is not comparable.
type installation struct {
	pass PassI
}

var (
	installMu sync.Mutex
	installed atomic.Pointer[[]*installation]
	envOnce   sync.Once
)

// Install adds p after the passes already installed, process-wide, and
// returns the function that removes it again. The host installs its passes
// once, before the first statement; a test installs one and defers remove.
func Install(p PassI) (remove func()) {
	installMu.Lock()
	defer installMu.Unlock()
	mine := &installation{pass: p}
	var next []*installation
	if cur := installed.Load(); cur != nil {
		next = slices.Clone(*cur)
	}
	next = append(next, mine)
	installed.Store(&next)
	remove = sync.OnceFunc(func() {
		installMu.Lock()
		defer installMu.Unlock()
		cur := installed.Load()
		if cur == nil {
			return
		}
		i := slices.Index(*cur, mine)
		if i < 0 {
			return
		}
		if len(*cur) == 1 {
			installed.Store(nil)
			return
		}
		rest := slices.Delete(slices.Clone(*cur), i, i+1)
		installed.Store(&rest)
	})
	return
}

// Lead passes one statement through the installed passes and returns the
// text to send. With none installed it returns sql and allocates nothing.
//
// A refusal comes back as err with out empty; the caller must not send.
func Lead(ctx context.Context, exit ExitE, site string, sql string) (out string, err error) {
	envOnce.Do(installFromEnv)
	passes := installed.Load()
	if passes == nil {
		out = sql
		return
	}
	st := Statement{SQL: sql, Exit: exit, Site: site}
	for _, p := range *passes {
		st.SQL, err = p.pass.Lead(ctx, st)
		if err != nil {
			err = eb.Build().Str("site", site).Stringer("exit", exit).Errorf("statement refused at the fairlead: %w", err)
			return
		}
	}
	out = st.SQL
	return
}
