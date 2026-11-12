package chat

import (
	"encoding/json/v2"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
)

// Stops in the journal (ADR-0370 §SD3, §SD6, proposed). Every entry here is
// extension-private (KindIgnorable): the projection skips it, so the model
// is shown none of it.
//
//   - name "stops", form "set": the stops in force, written when they
//     change, so the record says which stops a turn ran under.
//   - name "stop", form "fired": one stop's firing at a check, with the ids
//     of every row it returned and the new rows in full. Firings of one
//     check share a key.
//   - name "stop", form "noted": a firing at the turn's answer, where there
//     is nothing left to hold or stop.
//   - name "stop", form "continue" or "declined": the person's answer to a
//     held turn, under the check's key.
//
// What the person has acknowledged is a fold of these and of their own
// messages; nothing else remembers it.
const (
	stopEntryName    = "stop"
	stopsEntryName   = "stops"
	stopsFormSet     = "set"
	stopFormFired    = "fired"
	stopFormNoted    = "noted"
	stopFormContinue = "continue"
	stopFormDeclined = "declined"
)

// The points of the turn loop a check runs at (ADR-0370 §SD5).
const (
	stopPointRequest = "request"
	stopPointReply   = "reply"
)

// stopFiring is a firing as the journal keeps it.
type stopFiring struct {
	Stop string `json:"stop"`
	// Then is the action the firing acted as: the stop's own, or stop for
	// one that could not be evaluated.
	Then string `json:"then"`
	What string `json:"what,omitempty"`
	// Rows are the ids of every row the stop returned at the check: what an
	// acknowledgement of this firing acknowledges.
	Rows []string `json:"rows,omitempty"`
	// Shown are the rows not acknowledged before, in full, at most
	// maxStopShown; More counts the rest.
	Shown []string `json:"shown,omitempty"`
	More  int      `json:"more,omitempty"`
	Error string   `json:"error,omitempty"`
	Point string   `json:"point"`
}

// stopsRecord is the stops in force as the journal keeps them: the
// declarations, and which of them are the preset's.
type stopsRecord struct {
	Stops  []Stop   `json:"stops"`
	Preset []string `json:"preset,omitempty"`
}

func isStopEntry(e agentrun.Entry, forms ...string) bool {
	if e.Kind != agentrun.KindIgnorable || e.Name != stopEntryName {
		return false
	}
	for _, f := range forms {
		if e.Form == f {
			return true
		}
	}
	return false
}

// stopAcks is the rows the person has acknowledged, by stop (ADR-0370
// §SD3): the rows of every firing they answered with Continue, and of every
// firing that came before a message of theirs. A stop fires on a row that
// is not among them.
func stopAcks(entries []agentrun.Entry) (acked map[string]map[string]struct{}) {
	acked = make(map[string]map[string]struct{})
	type open struct {
		key string
		f   stopFiring
	}
	var pending []open
	ack := func(keep func(o open) bool) {
		rest := pending[:0]
		for _, o := range pending {
			if keep(o) {
				rest = append(rest, o)
				continue
			}
			set := acked[o.f.Stop]
			if set == nil {
				set = make(map[string]struct{}, len(o.f.Rows))
				acked[o.f.Stop] = set
			}
			for _, id := range o.f.Rows {
				set[id] = struct{}{}
			}
		}
		pending = rest
	}
	for _, e := range entries {
		switch {
		case e.Kind == agentrun.KindUser:
			// The person saw what stopped or ended the turn and spoke again.
			ack(func(open) bool { return false })
		case isStopEntry(e, stopFormFired, stopFormNoted):
			var f stopFiring
			if json.Unmarshal([]byte(e.Body), &f) == nil && f.Stop != "" {
				pending = append(pending, open{key: e.Key, f: f})
			}
		case isStopEntry(e, stopFormContinue):
			ack(func(o open) bool { return o.key != e.Key })
		}
	}
	return
}

// stopEvent is one firing as the Stops tab lists it: the firing, when, and
// what became of it.
type stopEvent struct {
	at     time.Time
	turn   string
	key    string
	firing stopFiring
	// noted says the firing came with the turn's answer; answer is the
	// person's, "continue" or "declined", empty while a held turn waits and
	// for a firing that held nothing.
	noted  bool
	answer string
}

// stopEventsOf lists the firings the journal holds, oldest first.
func stopEventsOf(entries []agentrun.Entry) (events []stopEvent) {
	for _, e := range entries {
		switch {
		case isStopEntry(e, stopFormFired, stopFormNoted):
			var f stopFiring
			if json.Unmarshal([]byte(e.Body), &f) != nil || f.Stop == "" {
				continue
			}
			events = append(events, stopEvent{at: e.At, turn: e.Turn, key: e.Key, firing: f, noted: e.Form == stopFormNoted})
		case isStopEntry(e, stopFormContinue, stopFormDeclined):
			for i := range events {
				if events[i].key == e.Key {
					events[i].answer = e.Form
				}
			}
		}
	}
	return
}

// lastStopsRecord is the stops the journal last recorded as in force.
func lastStopsRecord(entries []agentrun.Entry) (rec stopsRecord, body string, ok bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Kind == agentrun.KindIgnorable && e.Name == stopsEntryName && e.Form == stopsFormSet {
			if json.Unmarshal([]byte(e.Body), &rec) != nil {
				return stopsRecord{}, "", false
			}
			return rec, e.Body, true
		}
	}
	return
}

// record is the set as the journal keeps it.
func (inst stopSet) record() (body string) {
	rec := stopsRecord{Stops: inst.specs()}
	if rec.Stops == nil {
		rec.Stops = []Stop{}
	}
	for _, s := range inst {
		if s.origin == stopOriginPreset {
			rec.Preset = append(rec.Preset, s.spec.Name)
		}
	}
	b, _ := json.Marshal(rec)
	return string(b)
}
