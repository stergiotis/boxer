package attention

import (
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// The person's status (ADR-0328): Here, Busy or Away. It changes
// which cards open on their own and, while Away, keeps a log of what
// happened, shown when the person returns. It decides nothing, moves no
// clock and reaches no model (§SD4).

// StatusE is the person's status. The zero value is Here, the status at
// start.
type StatusE uint8

const (
	StatusHere StatusE = iota
	StatusBusy
	StatusAway
)

var AllStatuses = []StatusE{StatusHere, StatusBusy, StatusAway}

func (inst StatusE) String() (s string) {
	switch inst {
	case StatusHere:
		s = "here"
	case StatusBusy:
		s = "busy"
	case StatusAway:
		s = "away"
	default:
		s = "unspecified"
	}
	return
}

// Opens says which cards open on their own at this status (§SD2): a
// critical item's when it arrives, an escalated item's.
func (inst StatusE) Opens(critical bool, escalated bool) bool {
	switch inst {
	case StatusHere:
		return critical || escalated
	case StatusBusy:
		return critical
	}
	return false
}

// AwayKindE is what an entry of the away log records (§SD3).
type AwayKindE uint8

const (
	// AwayArrived is an item that arrived in the queue.
	AwayArrived AwayKindE = iota + 1
	// AwayLeft is an item that left the queue without the person deciding
	// it in host chrome; Why says whether it expired.
	AwayLeft
	// AwayRefused is a request the flood limit refused.
	AwayRefused
	// AwayHighlight is a highlight kept for the person.
	AwayHighlight
)

func (inst AwayKindE) String() (s string) {
	switch inst {
	case AwayArrived:
		s = "arrived"
	case AwayLeft:
		s = "left"
	case AwayRefused:
		s = "refused"
	case AwayHighlight:
		s = "highlight"
	default:
		s = "unspecified"
	}
	return
}

// AwayEntry is one line of the away log.
type AwayEntry struct {
	At   time.Time
	What AwayKindE
	// Why says more: "expired", "no longer waits", a refusal's reason.
	Why string
	// Key is the queue item's key, for Open while it still waits.
	Key          string
	Kind         string
	App          app.AppIdT
	Instance     uint64
	Conversation string
	Summary      string
	Level        LevelE
}

// MaxAwayLog bounds the away log; what arrives past it is counted.
const MaxAwayLog = 64

// AwayLog is what happened while the person was away.
type AwayLog struct {
	From    time.Time
	To      time.Time
	Entries []AwayEntry
	// Dropped counts the entries past MaxAwayLog.
	Dropped int
}

// StatusChange is a change of the status, for the trail.
type StatusChange struct {
	From   StatusE
	To     StatusE
	ByIdle bool
	At     time.Time
}

// Presence holds the person's status, the time of their last input and the
// away log. Create it with NewPresence.
type Presence struct {
	mu     sync.Mutex
	status StatusE
	// byIdle says the host set Away for idle time; before is the status
	// it returns to at the next input.
	byIdle    bool
	before    StatusE
	idle      time.Duration
	lastInput time.Time
	// log is being written while Away; returned is the last one, until the
	// person dismisses it.
	log      *AwayLog
	returned *AwayLog
	onChange func(StatusChange)
	version  uint64
}

// NewPresence is Here as of now, turning Away after idle without input
// (zero never).
func NewPresence(idle time.Duration, now time.Time) (p *Presence) {
	return &Presence{idle: idle, lastInput: now}
}

// SetOnChange installs the hook told of each change, for the trail.
func (inst *Presence) SetOnChange(f func(StatusChange)) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.onChange = f
}

// Status is the status, and whether the host set it for idle time.
func (inst *Presence) Status() (s StatusE, byIdle bool) {
	if inst == nil {
		return StatusHere, false
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.status, inst.byIdle
}

// Set is the person setting the status by hand; it ends by hand.
func (inst *Presence) Set(s StatusE, now time.Time) {
	inst.mu.Lock()
	if s == inst.status && !inst.byIdle {
		inst.mu.Unlock()
		return
	}
	ch := inst.changeLocked(s, false, now)
	hook := inst.onChange
	inst.mu.Unlock()
	if hook != nil {
		hook(ch)
	}
}

// Input notes the person's input at now. An Away the host set for idle time
// ends at it, back to the status before.
func (inst *Presence) Input(now time.Time) {
	inst.mu.Lock()
	inst.lastInput = now
	if inst.status != StatusAway || !inst.byIdle {
		inst.mu.Unlock()
		return
	}
	ch := inst.changeLocked(inst.before, false, now)
	hook := inst.onChange
	inst.mu.Unlock()
	if hook != nil {
		hook(ch)
	}
}

// Tick sets Away once the idle interval passed without input.
func (inst *Presence) Tick(now time.Time) {
	inst.mu.Lock()
	if inst.idle <= 0 || inst.status == StatusAway || now.Sub(inst.lastInput) < inst.idle {
		inst.mu.Unlock()
		return
	}
	before := inst.status
	ch := inst.changeLocked(StatusAway, true, now)
	inst.before = before
	hook := inst.onChange
	inst.mu.Unlock()
	if hook != nil {
		hook(ch)
	}
}

func (inst *Presence) changeLocked(to StatusE, byIdle bool, now time.Time) (ch StatusChange) {
	ch = StatusChange{From: inst.status, To: to, ByIdle: byIdle, At: now}
	switch {
	case inst.status != StatusAway && to == StatusAway:
		inst.log = &AwayLog{From: now}
	case inst.status == StatusAway && to != StatusAway && inst.log != nil:
		inst.log.To = now
		inst.returned, inst.log = inst.log, nil
	}
	inst.status, inst.byIdle = to, byIdle
	inst.version++
	return
}

// Record adds e to the away log while the person is away.
func (inst *Presence) Record(e AwayEntry) {
	if inst == nil {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	l := inst.log
	if l == nil {
		return
	}
	if len(l.Entries) >= MaxAwayLog {
		l.Dropped++
		return
	}
	l.Entries = append(l.Entries, e)
	inst.version++
}

// Returned is the log of the last time away, until the person dismisses it.
func (inst *Presence) Returned() (l AwayLog, ok bool) {
	if inst == nil {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.returned == nil {
		return
	}
	l = *inst.returned
	l.Entries = append([]AwayEntry(nil), l.Entries...)
	return l, true
}

// Dismiss drops the returned log.
func (inst *Presence) Dismiss() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.returned != nil {
		inst.returned = nil
		inst.version++
	}
}

// Version counts the changes.
func (inst *Presence) Version() (v uint64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.version
}

// recordRefusal logs a flood refusal of stream: a conversation, or an
// instance named "app#instance".
func (inst *Presence) recordRefusal(stream string, reason string, now time.Time) {
	e := AwayEntry{At: now, What: AwayRefused, Why: reason, Level: LevelYourTurn}
	if strings.Contains(stream, "#") {
		e.Summary = "a request from " + stream
	} else {
		e.Conversation, e.Summary = stream, "a request"
	}
	inst.Record(e)
}

// recordNotice logs a highlight kept.
func (inst *Presence) recordNotice(n Notice) {
	text := n.Text
	if text == "" {
		text = n.Kind
	}
	if n.Title != "" {
		text += " · " + n.Title
	}
	inst.Record(AwayEntry{At: n.Created, What: AwayHighlight, Kind: n.Kind, App: n.App, Instance: n.Instance,
		Conversation: n.Conversation, Summary: text, Level: n.Level})
}
