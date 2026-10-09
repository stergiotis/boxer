package agent

// The agent surface as a coordinator draws it (ADR-0283 §SD1): one status
// per window operation, launchable app and desktop verb, decided in the
// dispatcher's order so a picture of the grant says what a call would meet.

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// CellStatusE is where one cell of the surface stands under a ceiling and a
// grant. Use is not a status: a cell of any status may carry calls.
type CellStatusE uint8

const (
	CellStatusUnspecified CellStatusE = 0
	// CellStatusAboveCeiling: the coordinator's settings refuse it whatever a
	// grant holds.
	CellStatusAboveCeiling CellStatusE = 1
	// CellStatusNotGranted: no grant entry covers it; a call asks the person.
	CellStatusNotGranted CellStatusE = 2
	// CellStatusObserveOnly: covered in observe mode, and it has an effect; a
	// call asks the person to raise the mode.
	CellStatusObserveOnly CellStatusE = 3
	// CellStatusGranted: a call goes through, or to the person as a proposal
	// where the effect or the mode says so.
	CellStatusGranted CellStatusE = 4
)

var AllCellStatuses = []CellStatusE{CellStatusAboveCeiling, CellStatusNotGranted, CellStatusObserveOnly, CellStatusGranted}

func (inst CellStatusE) String() (s string) {
	switch inst {
	case CellStatusAboveCeiling:
		s = "above-ceiling"
	case CellStatusNotGranted:
		s = "not-granted"
	case CellStatusObserveOnly:
		s = "observe-only"
	case CellStatusGranted:
		s = "granted"
	default:
		s = "unspecified"
	}
	return
}

// GrantedEntry is one entry of keelson('agent_grants'), parsed from its
// "instance:app:mode[:operations]" spelling.
type GrantedEntry struct {
	Instance uint64
	App      string
	Mode     ModeE
	// Ops are the operations the entry names; none covers every operation
	// the app exposes to agents.
	Ops []string
}

// ParseGrantedEntry reads one entries element of keelson('agent_grants').
func ParseGrantedEntry(s string) (e GrantedEntry, err error) {
	parts := strings.SplitN(s, ":", 4)
	if len(parts) < 3 {
		err = eb.Build().Str("entry", s).Errorf("grant entry: want instance:app:mode[:operations]")
		return
	}
	e.Instance, err = strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		err = eb.Build().Str("entry", s).Errorf("grant entry: instance: %w", err)
		return
	}
	e.App, e.Mode = parts[1], ParseMode(parts[2])
	if e.Mode == ModeUnspecified {
		err = eb.Build().Str("entry", s).Str("mode", parts[2]).Errorf("grant entry: no such mode")
		return
	}
	if len(parts) == 4 && parts[3] != "" {
		e.Ops = strings.Split(parts[3], ",")
	}
	return
}

// Covers reports whether the entry names op, under the dispatcher's rule.
func (inst *GrantedEntry) Covers(op string) (ok bool) {
	return (&entry{ops: inst.Ops}).covers(op)
}

// ParseGrantedLaunch reads one launches element of keelson('agent_grants'),
// "app:mode:count".
func ParseGrantedLaunch(s string) (l GrantLaunch, err error) {
	i, j := strings.Index(s, ":"), strings.LastIndex(s, ":")
	if i <= 0 || j <= i {
		err = eb.Build().Str("launch", s).Errorf("grant launch: want app:mode:count")
		return
	}
	n, err := strconv.ParseUint(s[j+1:], 10, 32)
	if err != nil {
		err = eb.Build().Str("launch", s).Errorf("grant launch: count: %w", err)
		return
	}
	l = GrantLaunch{App: s[:i], Mode: ParseMode(s[i+1 : j]), Count: uint32(n)}
	return
}

// ClassifyOperation is the status of operation op, of the given effect, in
// a window entry e covers; e is nil for a window no grant names. A nil
// ceiling forbids nothing. The order is check's in dispatch.go, from the
// ceiling down to the mode; what it leaves to the moment of the call — the
// budget, another task's hold, a pause — is not the surface's.
func ClassifyOperation(c *Ceiling, e *GrantedEntry, op string, effect app.OperationEffectE) (s CellStatusE) {
	switch {
	case c.refuseEffect(effect) != "":
		return CellStatusAboveCeiling
	case e == nil || !e.Covers(op):
		return CellStatusNotGranted
	case effect != app.OperationEffectNone && e.Mode == ModeObserve:
		return CellStatusObserveOnly
	}
	return CellStatusGranted
}

// ClassifyLaunch is the status of opening a window of an app; l is the
// task's launch for it, nil when the grant names none. A launch whose count
// is spent stays granted: the surface shows the grant, the calls its use.
func ClassifyLaunch(c *Ceiling, l *GrantLaunch) (s CellStatusE) {
	switch {
	case c.refuseLaunch() != "":
		return CellStatusAboveCeiling
	case l == nil || l.Count == 0:
		return CellStatusNotGranted
	}
	return CellStatusGranted
}

// ClassifyArrange is the status of arranging the desktop under the task's
// desktop mode; the order is windowCheck's.
func ClassifyArrange(c *Ceiling, desktop ModeE) (s CellStatusE) {
	switch {
	case c.refuseDesktop() != "":
		return CellStatusAboveCeiling
	case desktop == ModeUnspecified:
		return CellStatusNotGranted
	case desktop != ModeAct:
		return CellStatusObserveOnly
	}
	return CellStatusGranted
}

// ClassifyWindowVerb is the status of raising or placing the window e
// covers, nil when no grant names it; the order is windowCheck's.
func ClassifyWindowVerb(c *Ceiling, e *GrantedEntry) (s CellStatusE) {
	switch {
	case e == nil:
		return CellStatusNotGranted
	case e.Mode == ModeAct && c.refuseMode(ModeAct) != "":
		return CellStatusAboveCeiling
	case e.Mode != ModeAct:
		return CellStatusObserveOnly
	}
	return CellStatusGranted
}

// The operation names the action record gives what is not an app's
// operation: a launch and the desktop verbs.
const (
	// ActionOpenWindow is a launch.
	ActionOpenWindow = launchOperation
	// ActionArrange is arranging the desktop.
	ActionArrange = verbArrange
	// ActionRaise is raising one window.
	ActionRaise = verbRaise
	// ActionPlace is placing one window.
	ActionPlace = verbPlace
	// ActionMark is putting a mark on the overlay, ActionUnmark
	// removing the task's (ADR-0297).
	ActionMark   = verbMark
	ActionUnmark = verbUnmark
	// ActionDescribe, ActionHelp and ActionList are the coordinator's reads
	// of the surface, ActionCapture a capture and ActionDisclose a
	// screenshot's view: the dispatcher's own, recorded but no cell of the
	// surface (ADR-0283 §SD1).
	ActionDescribe = "describe"
	ActionHelp     = "help"
	ActionList     = "list"
	ActionCapture  = "capture"
	ActionDisclose = "disclose"
)

// OnSurface says whether an action row's operation is a cell of the agent
// surface (ADR-0283 §SD1): an app's operation, a launch or a desktop verb —
// not one of the dispatcher's own reads, captures or disclosures.
func OnSurface(operation string) (on bool) {
	switch operation {
	case ActionDescribe, ActionHelp, ActionList, ActionCapture, ActionDisclose:
		return false
	}
	return true
}
