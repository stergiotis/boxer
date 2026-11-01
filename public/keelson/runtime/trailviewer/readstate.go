package trailviewer

import (
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
)

// The table read's lifecycle (ADR-0350 §SD12, proposed): one state machine
// the chip above the table shows and the catalog mounts as read_state /
// read_machine. The machine never drives the cache: once per frame
// observeReadState projects the cache's fields onto a state and syncReadFSM
// mirrors it, the way play mirrors its query result's lifecycle
// (apps/play, play_querystate.go).

// readStateE is where the window's read of the trail marts stands.
type readStateE uint8

const (
	// readIdle — the source is not persisted, or nothing has been asked yet.
	readIdle readStateE = iota
	// readReading — a read is in flight, or about to start, and nothing held
	// is the wanted scope's: the first fill, or a changed case, window,
	// filter or colouring.
	readReading
	// readRows, readEmpty, readFailed — settled: what is held is the wanted
	// scope's; failed when the last read erred, the held rows staying on
	// screen.
	readRows
	readEmpty
	readFailed
	// readRefreshing — the wanted scope's rows are held and a refill or an
	// extension is in flight.
	readRefreshing
	// readStopped — the person or an agent stopped the window's reads:
	// nothing starts until Refresh or a scope change.
	readStopped
)

var readStateNames = [...]string{"idle", "reading", "rows", "empty", "failed", "refreshing", "stopped"}

func (inst readStateE) String() string {
	if int(inst) < len(readStateNames) {
		return readStateNames[inst]
	}
	return "unknown"
}

// readObservation is what observeReadState projects: the cache's fields
// the state is a function of, taken in one frame.
type readObservation struct {
	persisted bool
	filled    bool
	// sameScope says what is held is the wanted scope's.
	sameScope bool
	running   bool
	hold      bool
	failed    bool
	rows      int
}

// observeReadState is a pure function of the cache's fields. A hold wins
// over a read still winding down, since the stop was asked; a failure wins
// over what is held, since the line under the controls names it; a wanted
// scope that is not the held one reads as reading while its read is
// pending in the settle window, not only once it is in flight.
func observeReadState(o readObservation) (s readStateE) {
	switch {
	case !o.persisted:
		s = readIdle
	case o.hold:
		s = readStopped
	case o.running && o.filled && o.sameScope:
		s = readRefreshing
	case o.running:
		s = readReading
	case o.failed:
		s = readFailed
	case !o.filled:
		s = readIdle
	case !o.sameScope:
		s = readReading
	case o.rows == 0:
		s = readEmpty
	default:
		s = readRows
	}
	return
}

// observe is the cache's fields as observeReadState wants them.
func (inst *tableCache) observe(persisted bool, want scope) readObservation {
	return readObservation{persisted: persisted, filled: inst.filled, sameScope: inst.scope == want,
		running: inst.runner.Running(), hold: inst.hold, failed: inst.lastErr != nil, rows: len(inst.rows)}
}

// newReadFSM declares the read lifecycle's graph: idle → reading → a
// settled state; a settled state refills or extends (refreshing), reads
// anew on a scope change (reading), or stops; a stop is left by Refresh or
// a scope change. The rules draw the popup's graph and label the edges;
// they need not be exhaustive, since syncReadFSM mirrors what it observes
// (fsmview.Machine.Mirror), warning only when no declared path reaches the
// target. A read that starts and settles between two frames skips
// `reading` or `refreshing`, which is a skip of this graph, not a
// contradiction of it.
func newReadFSM() *fsmview.Machine[readStateE] {
	m := fsmview.NewMachine(readIdle, 64, fsmview.MachineOptions[readStateE]{
		Label:      func(s readStateE) string { return s.String() },
		StateOrder: []readStateE{readIdle, readReading, readRows, readEmpty, readFailed, readRefreshing, readStopped},
		StateColor: readStateColor,
	})
	m.AddRule(readIdle, readReading).
		AddRule(readReading, readRows, readEmpty, readFailed, readStopped).
		AddRule(readRefreshing, readRows, readEmpty, readFailed, readStopped).
		AddRule(readStopped, readReading, readRefreshing).
		EdgeLabel(readIdle, readReading, "first fill").
		EdgeLabel(readReading, readRows, "rows").
		EdgeLabel(readReading, readEmpty, "0 rows").
		EdgeLabel(readReading, readFailed, "error").
		EdgeLabel(readReading, readStopped, "stop_read").
		EdgeLabel(readRefreshing, readRows, "rows").
		EdgeLabel(readRefreshing, readEmpty, "0 rows").
		EdgeLabel(readRefreshing, readFailed, "error").
		EdgeLabel(readRefreshing, readStopped, "stop_read").
		EdgeLabel(readStopped, readReading, "scope change").
		EdgeLabel(readStopped, readRefreshing, "Refresh")
	for _, settled := range []readStateE{readRows, readEmpty, readFailed} {
		m.AddRule(settled, readRefreshing, readReading, readStopped).
			EdgeLabel(settled, readRefreshing, "interval · Refresh · the table's end").
			EdgeLabel(settled, readReading, "scope change").
			EdgeLabel(settled, readStopped, "stop_read")
	}
	return m
}

// readStateColor tints the chip's popup graph: the accent while a read is
// in flight, success, warning and error for the settled states, muted for
// idle and stopped.
func readStateColor(s readStateE, _ bool) styletokens.RGBA8 {
	switch s {
	case readReading, readRefreshing:
		return styletokens.AccentDefault
	case readRows:
		return styletokens.SuccessDefault
	case readEmpty:
		return styletokens.WarningDefault
	case readFailed:
		return styletokens.ErrorDefault
	}
	return styletokens.NeutralSubtle
}

// readStateTone is the chip's badge tone by state.
func readStateTone(s readStateE) badge.ToneE {
	switch s {
	case readReading, readRefreshing:
		return badge.TonePrimary
	case readRows:
		return badge.ToneSuccess
	case readEmpty:
		return badge.ToneWarning
	case readFailed:
		return badge.ToneError
	}
	return badge.ToneNeutral
}

// mirroredStateI is what a state has to be to ride a per-frame mirror:
// comparable, and able to name itself in the diagnostic.
type mirroredStateI interface {
	comparable
	fmt.Stringer
}

// mirrorObservedFSM drives the render-goroutine machine to the state this
// frame observed and grades what it saw. Mirror follows any edge and
// reports whether a rule declared it: a rejecting Transition would wedge
// the mirror, since a memoryless observer re-proposes the same refused
// target every frame. An undeclared edge whose target the declared graph
// reaches is states no frame sampled — a read that started and settled
// between two frames — and logs at debug; a target the graph cannot reach
// at all contradicts the model and logs at warn.
func mirrorObservedFSM[T mirroredStateI](m *fsmview.Machine[T], obs T, subject string) {
	cur := m.Current()
	if cur == obs {
		return
	}
	if declared := m.Mirror(obs); declared {
		return
	}
	ev := log.Warn()
	msg := "trailviewer: " + subject + " FSM observed an edge the declared graph cannot reach (mirrored)"
	if m.CanReach(cur, obs) {
		ev = log.Debug()
		msg = "trailviewer: " + subject + " FSM skipped states no frame sampled (mirrored)"
	}
	ev.Stringer("from", cur).Stringer("to", obs).Msg(msg)
}

// syncReadFSM mirrors the read's observed state once per frame, after the
// cache ticked.
func (inst *App) syncReadFSM(persisted bool) {
	mirrorObservedFSM(inst.readFSM, observeReadState(inst.cache.observe(persisted, inst.scopeValue())), "read")
}
