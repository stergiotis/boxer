package attention

import (
	"strconv"
	"sync"
	"time"
)

// FloodWindow is the span the flood limit counts over (ADR-0324 §SD6).
const FloodWindow = 10 * time.Minute

// Flood admits waiting items at a bounded rate per stream and per person
// (ADR-0324 §SD6). A limit of zero or less does not bound. The count is kept
// by stream id, so it survives a stream's turns. Create it with NewFlood.
type Flood struct {
	mu        sync.Mutex
	perStream int
	perPerson int
	window    time.Duration
	streams   map[string][]time.Time
	person    []time.Time
	onRefuse  func(stream string, reason string, now time.Time)
}

// SetOnRefuse installs the hook told of each refusal, outside the lock.
func (inst *Flood) SetOnRefuse(f func(stream string, reason string, now time.Time)) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.onRefuse = f
}

func NewFlood(perStream int, perPerson int) (f *Flood) {
	return &Flood{perStream: perStream, perPerson: perPerson, window: FloodWindow, streams: make(map[string][]time.Time, 8)}
}

// Admit counts one item of stream at now, or refuses it; reason says which
// limit refused it, in words the asking model can act on. A refused item is
// not counted.
func (inst *Flood) Admit(stream string, now time.Time) (ok bool, reason string) {
	if inst == nil {
		return true, ""
	}
	inst.mu.Lock()
	ok, reason = inst.admitLocked(stream, now)
	hook := inst.onRefuse
	inst.mu.Unlock()
	if !ok && hook != nil {
		hook(stream, reason, now)
	}
	return
}

func (inst *Flood) admitLocked(stream string, now time.Time) (ok bool, reason string) {
	cut := now.Add(-inst.window)
	inst.person = prune(inst.person, cut)
	s := prune(inst.streams[stream], cut)
	switch {
	case inst.perStream > 0 && len(s) >= inst.perStream:
		inst.streams[stream] = s
		return false, "this conversation has asked the person " + strconv.Itoa(len(s)) +
			" times in the last ten minutes; wait for a decision, or ask for fewer and broader"
	case inst.perPerson > 0 && len(inst.person) >= inst.perPerson:
		inst.streams[stream] = s
		return false, strconv.Itoa(len(inst.person)) +
			" decisions were asked of the person in the last ten minutes; wait for one, or ask for fewer and broader"
	}
	inst.streams[stream] = append(s, now)
	inst.person = append(inst.person, now)
	for k, v := range inst.streams {
		if len(v) == 0 {
			delete(inst.streams, k)
		}
	}
	return true, ""
}

func prune(ts []time.Time, cut time.Time) []time.Time {
	i := 0
	for i < len(ts) && !ts[i].After(cut) {
		i++
	}
	return ts[i:]
}
