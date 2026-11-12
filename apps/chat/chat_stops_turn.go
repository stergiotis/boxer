package chat

import (
	"context"
	"encoding/json/v2"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/attention"
)

// Stops in the turn loop (ADR-0370 §SD4, §SD5, proposed). A check runs
// before each request and after each reply. It sits beside the
// turn-stopping guard and is not an element of it: a guard is a function of
// a reply, a check calls the worker and may wait on the person, and a
// seam's element names are in the run's fingerprint, so a new one would
// strand every open conversation.

// heldClock adds up the time a turn waited on the person, so a time box
// counts working time. Waits may overlap; an overlap is counted once.
type heldClock struct {
	mu    sync.Mutex
	depth int
	since time.Time
	total time.Duration
}

func (inst *heldClock) enter() {
	inst.mu.Lock()
	if inst.depth == 0 {
		inst.since = time.Now()
	}
	inst.depth++
	inst.mu.Unlock()
}

func (inst *heldClock) leave() {
	inst.mu.Lock()
	if inst.depth > 0 {
		inst.depth--
		if inst.depth == 0 {
			inst.total += time.Since(inst.since)
		}
	}
	inst.mu.Unlock()
}

func (inst *heldClock) reset() {
	inst.mu.Lock()
	inst.depth, inst.total = 0, 0
	inst.mu.Unlock()
}

// held is the time waited so far, a wait in progress included.
func (inst *heldClock) held() (d time.Duration) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	d = inst.total
	if inst.depth > 0 {
		d += time.Since(inst.since)
	}
	return
}

// maxStopEvents bounds the firings a coordinator keeps for the Stops tab;
// the journal keeps all of them.
const maxStopEvents = 64

// stopsRuntime is a coordinator's side of its conversation's stops.
type stopsRuntime struct {
	mu sync.Mutex
	// set is the stops in force, taken when a turn starts; runner evaluates
	// them, nil where the host gives the window no worker.
	set    stopSet
	runner stopRunnerI
	// seq numbers the checks that fired, for their keys; journaled is the
	// record of the set last written to the run.
	seq       int
	journaled string
	// events are the firings, oldest first, for the tab.
	events []stopEvent
	// started is when the running turn began and clock what it waited on
	// the person since.
	started time.Time
	clock   heldClock
}

func (inst *stopsRuntime) setNow() (set stopSet, runner stopRunnerI) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.set, inst.runner
}

// setStops takes the stops the next turn runs under.
func (inst *coordinator) setStops(set stopSet) {
	inst.stops.mu.Lock()
	inst.stops.set = set
	inst.stops.mu.Unlock()
}

// setStopRunner takes the worker the checks run in.
func (inst *coordinator) setStopRunner(r stopRunnerI) {
	inst.stops.mu.Lock()
	inst.stops.runner = r
	inst.stops.mu.Unlock()
}

// stopEvents is a copy of the firings so far, oldest first.
func (inst *coordinator) stopEvents() (events []stopEvent) {
	inst.stops.mu.Lock()
	defer inst.stops.mu.Unlock()
	return append(events, inst.stops.events...)
}

// loadStopEvents takes a resumed run's firings.
func (inst *coordinator) loadStopEvents(entries []agentrun.Entry) {
	events := stopEventsOf(entries)
	if len(events) > maxStopEvents {
		events = events[len(events)-maxStopEvents:]
	}
	inst.stops.mu.Lock()
	inst.stops.events = events
	inst.stops.mu.Unlock()
}

func (inst *stopsRuntime) addEvents(events ...stopEvent) {
	inst.mu.Lock()
	inst.events = append(inst.events, events...)
	if n := len(inst.events); n > maxStopEvents {
		inst.events = append(inst.events[:0], inst.events[n-maxStopEvents:]...)
	}
	inst.mu.Unlock()
}

func (inst *stopsRuntime) answer(key string, form string) {
	inst.mu.Lock()
	for i := range inst.events {
		if inst.events[i].key == key {
			inst.events[i].answer = form
		}
	}
	inst.mu.Unlock()
}

// beginStopsTurn starts the turn's clock and writes the stops in force to
// the run when they differ from what it last recorded. A conversation that
// never had a stop writes nothing.
func (inst *coordinator) beginStopsTurn(ctx context.Context, w *agentrun.Writer, turn string) (err error) {
	st := &inst.stops
	st.clock.reset()
	st.mu.Lock()
	st.started = time.Now()
	set, journaled := st.set, st.journaled
	st.mu.Unlock()
	if journaled == "" {
		if _, body, ok := lastStopsRecord(w.Entries()); ok {
			journaled = body
		}
	}
	rec := set.record()
	if rec == journaled || (journaled == "" && len(set) == 0) {
		return
	}
	if _, err = inst.append(ctx, w, agentrun.Entry{Kind: agentrun.KindIgnorable, Name: stopsEntryName, Form: stopsFormSet, Body: rec, Turn: turn, Label: harnessLabel()}); err != nil {
		return
	}
	st.mu.Lock()
	st.journaled = rec
	st.mu.Unlock()
	return
}

// stopVerdict is what a check made of the turn.
type stopVerdict struct {
	// action is the strongest action in force once the check has run:
	// none when nothing fired or the person said to go on, stop when the
	// turn ends here, end or close when the conversation does.
	action stopActionE
	// reason says why, for a turn that does not go on.
	reason string
}

// The person's choices on a held turn.
const (
	stopChoiceContinue = "Continue"
	stopChoiceStop     = "Stop the turn"
)

// checkStops runs the conversation's stops at one point of the turn.
// answered says the check follows a reply without tool calls, the turn's
// answer: nothing is left to hold or stop, so only a stop that ends the
// conversation acts, and the others are noted. err is the journal's, or the
// turn's own end while it was held.
func (inst *coordinator) checkStops(ctx context.Context, jctx context.Context, w *agentrun.Writer, turn string, round int, point string, answered bool) (v stopVerdict, err error) {
	st := &inst.stops
	set, runner := st.setNow()
	if len(set) == 0 {
		return
	}
	entries := w.Entries()
	st.mu.Lock()
	started := st.started
	st.mu.Unlock()
	in := stopInputs{entries: entries, turn: turn, round: round, now: time.Now()}
	in.elapsed = in.now.Sub(started)
	in.working = max(in.elapsed-st.clock.held(), 0)
	if art := inst.artefactOf(); art != nil {
		in.revision, in.artefact = art.head()
	}
	results := evalStops(ctx, runner, set, in)
	if cerr := ctx.Err(); cerr != nil {
		// The turn was cancelled under the check: that is the turn's end,
		// not a stop that could not be evaluated.
		return v, cerr
	}
	acked := stopAcks(entries)
	var fired, noted []stopFiring
	for _, res := range results {
		s, ok := set.find(res.name)
		if !ok {
			continue
		}
		f := stopFiring{Stop: res.name, Then: s.then.String(), What: s.what, Point: point}
		then := s.then
		if res.err != "" {
			// A stop that cannot be checked does not read as no stop
			// (ADR-0370 §SD4).
			f.Error, f.Then, then = res.err, stopActionStop.String(), stopActionStop
		} else {
			for _, r := range res.rows {
				f.Rows = append(f.Rows, r.id)
				if _, seen := acked[res.name][r.id]; seen {
					continue
				}
				if len(f.Shown) < maxStopShown {
					f.Shown = append(f.Shown, r.text)
				} else {
					f.More++
				}
			}
			if len(f.Shown) == 0 {
				continue
			}
		}
		if answered && then < stopActionEnd {
			noted = append(noted, f)
			continue
		}
		fired = append(fired, f)
		v.action = max(v.action, then)
	}
	if len(fired)+len(noted) == 0 {
		return
	}
	st.mu.Lock()
	st.seq++
	key := "stop/" + turn + "/" + strconv.Itoa(st.seq)
	st.mu.Unlock()
	var write []agentrun.Entry
	var events []stopEvent
	add := func(fs []stopFiring, form string) {
		for _, f := range fs {
			body, _ := json.Marshal(f)
			write = append(write, agentrun.Entry{Kind: agentrun.KindIgnorable, Name: stopEntryName, Form: form, Key: key, Body: string(body), Turn: turn, Label: harnessLabel()})
			events = append(events, stopEvent{at: in.now, turn: turn, key: key, firing: f, noted: form == stopFormNoted})
		}
	}
	add(fired, stopFormFired)
	add(noted, stopFormNoted)
	if _, err = inst.append(jctx, w, write...); err != nil {
		return
	}
	st.addEvents(events...)
	if len(fired) == 0 {
		return
	}
	v.reason = stopReason(fired)
	if !v.action.holds() {
		// The person did not ask for what would follow: a completion does
		// not start another turn until they speak (ADR-0358 §SD5).
		inst.holdWakes()
		return
	}
	goOn, err := inst.holdOnStops(ctx, fired, v.action)
	if err != nil {
		return
	}
	form := stopFormDeclined
	if goOn {
		form = stopFormContinue
	}
	if _, err = inst.append(jctx, w, agentrun.Entry{Kind: agentrun.KindIgnorable, Name: stopEntryName, Form: form, Key: key, Turn: turn, Label: harnessLabel()}); err != nil {
		return
	}
	st.answer(key, form)
	if goOn {
		return stopVerdict{}, nil
	}
	inst.holdWakes()
	v.action, v.reason = stopActionStop, v.reason+"; you ended the turn"
	return
}

// holdOnStops holds the turn until the person says whether it goes on: in
// the window's form, and for ask in the host's queue as well (ADR-0324).
// A skipped form does not continue.
func (inst *coordinator) holdOnStops(ctx context.Context, fired []stopFiring, action stopActionE) (goOn bool, err error) {
	inst.setStage(stagePerson)
	defer inst.setStage(stageModel)
	q := askQuestion{Header: "Stop", Question: stopQuestion(fired),
		Options: []askOption{
			{Label: stopChoiceContinue, Description: "the turn goes on from where it is"},
			{Label: stopChoiceStop, Description: "the turn ends here; what it did stays in the record"}}}
	var via hostAsk
	if action == stopActionAsk {
		via = inst.hostAsk(attention.KindQuestion)
	}
	r, err := inst.ask.askVia(ctx, []askQuestion{q}, via)
	if err != nil || r.Skipped || len(r.Answers) == 0 {
		return false, err
	}
	for _, c := range r.Answers[0].Chosen {
		if c.Label == stopChoiceContinue {
			return true, nil
		}
	}
	return false, nil
}

// stopFiringLine is one firing in a sentence: the stop, what it watches,
// and the first row or the error.
func stopFiringLine(f stopFiring) (s string) {
	s = "the stop " + strconv.Quote(f.Stop)
	if f.Error != "" {
		return s + " could not be checked: " + f.Error
	}
	s += " fired (" + f.What + ")"
	if len(f.Shown) > 0 {
		s += ": " + clip(f.Shown[0], stopMatchClip)
		if rest := len(f.Shown) - 1 + f.More; rest > 0 {
			s += " and " + plural(rest, "more row")
		}
	}
	return
}

// stopReason is why a turn does not go on, naming every stop that fired.
func stopReason(fired []stopFiring) string {
	parts := make([]string, 0, len(fired))
	for _, f := range fired {
		parts = append(parts, stopFiringLine(f))
	}
	return strings.Join(parts, "; ")
}

// stopQuestion is what a held turn asks the person, within what the host's
// card shows.
func stopQuestion(fired []stopFiring) string {
	return clip(upperFirst(stopReason(fired))+". Go on with the turn?", attention.MaxTextRunes)
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
