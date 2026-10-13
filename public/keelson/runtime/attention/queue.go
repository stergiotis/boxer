package attention

import (
	"cmp"
	"iter"
	"slices"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// Item is one decision the person owes, as its source reports it (ADR-0324
// §SD1). Key is unique across sources; Bound is the earliest clock that
// ends the item — its expiry or its task's deadline — zero for none.
type Item struct {
	Key          string
	Kind         string
	Effect       app.OperationEffectE
	App          app.AppIdT
	Instance     uint64
	Conversation string
	Summary      string
	Created      time.Time
	Bound        time.Time
	BoundWhy     string
}

// SourceI is a host service holding decisions the person owes. The queue
// reads its items each frame; the source keeps their state.
type SourceI interface {
	WaitingItems() iter.Seq[Item]
}

// Entry is an item as the queue shows it.
type Entry struct {
	Item
	Level LevelE
	// Acked is the person's acknowledgement: seen, not decided.
	Acked bool
	// ShelvedUntil is when a shelved item returns; zero when not shelved.
	ShelvedUntil time.Time
	// Escalated is a your-turn item left unacknowledged past the escalation
	// interval (ADR-0324 §SD4).
	Escalated bool
	// NearBound is an item within the escalation interval of its bound.
	NearBound bool
}

// ActE is what the person did to an item, for the trail.
type ActE uint8

const (
	ActAcknowledge ActE = iota + 1
	ActShelve
	ActUnshelve
)

func (inst ActE) String() (s string) {
	switch inst {
	case ActAcknowledge:
		s = "acknowledge"
	case ActShelve:
		s = "shelve"
	case ActUnshelve:
		s = "unshelve"
	default:
		s = "unspecified"
	}
	return
}

type itemState struct {
	acked      bool
	shelfUntil time.Time
	// opened is set once the queue asked for the item's card to open on
	// its own, so it asks once.
	opened bool
	seen   bool
	// decided says the person decided it in host chrome; its leaving is
	// not the away log's (ADR-0328 §SD3).
	decided bool
	item    Item
}

// Queue orders the waiting items of its sources (ADR-0324 §SD1) and keeps
// what the person did to them (§SD4). Create it with NewQueue.
type Queue struct {
	mu       sync.Mutex
	levels   *Levels
	sources  []SourceI
	state    map[string]*itemState
	escalate time.Duration
	onAct    func(ActE, Item, time.Time)
	scratch  []Entry
	// presence is the person's status (ADR-0328), nil for Here always.
	presence *Presence
}

// NewQueue is a queue classifying by levels, escalating after escalate (zero
// never).
func NewQueue(levels *Levels, escalate time.Duration) (q *Queue) {
	if levels == nil {
		levels = &Levels{}
	}
	return &Queue{levels: levels, state: make(map[string]*itemState, 16), escalate: escalate}
}

// AddSource adds a source of items.
func (inst *Queue) AddSource(s SourceI) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.sources = append(inst.sources, s)
}

// SetPresence makes the queue open cards by the person's status and log
// what arrives and leaves while they are away (ADR-0328 §SD2, §SD3).
func (inst *Queue) SetPresence(p *Presence) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.presence = p
}

// Decided says the person decided the item in host chrome.
func (inst *Queue) Decided(key string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if st := inst.state[key]; st != nil {
		st.decided = true
	}
}

// SetOnAct installs the hook told of each acknowledgement and shelve, for
// the trail.
func (inst *Queue) SetOnAct(f func(ActE, Item, time.Time)) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.onAct = f
}

// Snapshot is the queue at now: the items to show, highest level first and
// oldest first within a level, and the shelved ones apart. The state of
// items no source reports any more is dropped.
func (inst *Queue) Snapshot(now time.Time) (open []Entry, shelved []Entry) {
	all := inst.gather()
	inst.mu.Lock()
	presence := inst.presence
	var flow []AwayEntry
	for _, st := range inst.state {
		st.seen = false
	}
	for i := range all {
		e := &all[i]
		st := inst.state[e.Key]
		if st == nil {
			st = &itemState{}
			inst.state[e.Key] = st
			if presence != nil {
				flow = append(flow, awayEntryOf(AwayArrived, "", e.Item, e.Level, now))
			}
		}
		st.seen, st.item = true, e.Item
		if !st.shelfUntil.IsZero() && !now.Before(st.shelfUntil) {
			// Back from the shelf, unacknowledged (§SD4).
			st.shelfUntil, st.acked, st.opened = time.Time{}, false, false
		}
		e.Acked, e.ShelvedUntil = st.acked, st.shelfUntil
		if inst.escalate > 0 {
			e.Escalated = e.Level == LevelYourTurn && !e.Acked && e.ShelvedUntil.IsZero() && now.Sub(e.Created) >= inst.escalate
			e.NearBound = !e.Bound.IsZero() && e.Bound.Sub(now) <= inst.escalate
		}
	}
	for k, st := range inst.state {
		if !st.seen {
			if presence != nil && !st.decided {
				why := "no longer waits"
				if !st.item.Bound.IsZero() && !now.Before(st.item.Bound) {
					why = "expired"
				}
				flow = append(flow, awayEntryOf(AwayLeft, why, st.item, LevelYourTurn, now))
			}
			delete(inst.state, k)
		}
	}
	inst.mu.Unlock()
	for _, e := range flow {
		presence.Record(e)
	}
	sortEntries(all)
	for _, e := range all {
		if e.ShelvedUntil.IsZero() {
			open = append(open, e)
		} else {
			shelved = append(shelved, e)
		}
	}
	return
}

// gather reads every source's items and classifies them.
func (inst *Queue) gather() (all []Entry) {
	inst.mu.Lock()
	sources := slices.Clone(inst.sources)
	inst.mu.Unlock()
	all = make([]Entry, 0, 8)
	for _, s := range sources {
		for it := range s.WaitingItems() {
			all = append(all, Entry{Item: it})
		}
	}
	for i := range all {
		e := &all[i]
		e.Level = inst.levels.Classify(Event{Kind: e.Kind, Effect: e.Effect, App: e.App, Conversation: e.Conversation})
		if e.Level.Scale() != ScaleWaiting {
			// The floor (ADR-0323 §SD2): what a source reports as waiting
			// is never shown below your turn.
			e.Level = LevelYourTurn
		}
	}
	return
}

// sortEntries orders entries highest level first, oldest first within a
// level.
func sortEntries(all []Entry) {
	slices.SortStableFunc(all, func(a, b Entry) int {
		if c := cmp.Compare(b.Level, a.Level); c != 0 {
			return c
		}
		return a.Created.Compare(b.Created)
	})
}

// Pending is the queue at now as Snapshot shows it, open and shelved
// together, in Snapshot's order — without Snapshot's bookkeeping: it keeps
// no state for a new item, drops none for a gone one and logs nothing for
// the away log, so a reader other than the chrome may call it from any
// goroutine (ADR-0324, update of 2026-10-13). A shelf that has come due
// reads as back, unacknowledged, as the next Snapshot will make it.
func (inst *Queue) Pending(now time.Time) (all []Entry) {
	all = inst.gather()
	inst.mu.Lock()
	for i := range all {
		e := &all[i]
		if st := inst.state[e.Key]; st != nil {
			e.Acked, e.ShelvedUntil = st.acked, st.shelfUntil
			if !e.ShelvedUntil.IsZero() && !now.Before(e.ShelvedUntil) {
				e.Acked, e.ShelvedUntil = false, time.Time{}
			}
		}
		if inst.escalate > 0 {
			e.Escalated = e.Level == LevelYourTurn && !e.Acked && e.ShelvedUntil.IsZero() && now.Sub(e.Created) >= inst.escalate
			e.NearBound = !e.Bound.IsZero() && e.Bound.Sub(now) <= inst.escalate
		}
	}
	inst.mu.Unlock()
	sortEntries(all)
	return
}

// TakeOpens lists the entries whose cards open on their own: a critical
// item, and an escalated one, each once (ADR-0324 §SD3, §SD4), as the
// person's status lets them (ADR-0328 §SD2). One the status holds back does
// not open later on its own: it is in the list, and while away in the log.
func (inst *Queue) TakeOpens(open []Entry) (keys []string) {
	inst.mu.Lock()
	presence := inst.presence
	inst.mu.Unlock()
	status, _ := presence.Status()
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, e := range open {
		critical := e.Level == LevelCritical
		if !critical && !e.Escalated {
			continue
		}
		st := inst.state[e.Key]
		if st == nil || st.opened {
			continue
		}
		st.opened = true
		if status.Opens(critical, e.Escalated) {
			keys = append(keys, e.Key)
		}
	}
	return
}

func awayEntryOf(what AwayKindE, why string, it Item, level LevelE, now time.Time) AwayEntry {
	return AwayEntry{At: now, What: what, Why: why, Key: it.Key, Kind: it.Kind, App: it.App, Instance: it.Instance,
		Conversation: it.Conversation, Summary: it.Summary, Level: level}
}

// Acknowledge marks an item seen, not decided; it stops its escalation.
func (inst *Queue) Acknowledge(it Item, now time.Time) {
	inst.mu.Lock()
	st := inst.state[it.Key]
	if st == nil || st.acked {
		inst.mu.Unlock()
		return
	}
	st.acked = true
	hook := inst.onAct
	inst.mu.Unlock()
	if hook != nil {
		hook(ActAcknowledge, it, now)
	}
}

// Shelve takes an item out of the count until until, bounded by the item's
// bound: shelving extends no clock (ADR-0324 §SD4). effective is the time it
// returns; ok is false when the bound has passed or the item is unknown.
func (inst *Queue) Shelve(it Item, until time.Time, now time.Time) (effective time.Time, ok bool) {
	if !it.Bound.IsZero() && until.After(it.Bound) {
		until = it.Bound
	}
	if !until.After(now) {
		return
	}
	inst.mu.Lock()
	st := inst.state[it.Key]
	if st == nil {
		inst.mu.Unlock()
		return
	}
	st.shelfUntil, st.acked = until, true
	hook := inst.onAct
	inst.mu.Unlock()
	if hook != nil {
		hook(ActShelve, it, now)
	}
	return until, true
}

// Unshelve returns a shelved item to the queue, acknowledged.
func (inst *Queue) Unshelve(it Item, now time.Time) {
	inst.mu.Lock()
	st := inst.state[it.Key]
	if st == nil || st.shelfUntil.IsZero() {
		inst.mu.Unlock()
		return
	}
	st.shelfUntil = time.Time{}
	hook := inst.onAct
	inst.mu.Unlock()
	if hook != nil {
		hook(ActUnshelve, it, now)
	}
}
