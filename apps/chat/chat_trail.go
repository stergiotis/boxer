package chat

import (
	"bytes"
	"encoding/json"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
)

// The trail is what a turn with tools did, step by step, as it happens: each
// model call with what it wrote and how long it took, each tool call with
// its arguments and what came back. The tool loop records it on the
// coordinator; the waiting bubble draws it while the turn runs, and the
// tool entries of a landed turn keep their steps to inspect.

// stepKindE is what a step of the trail is.
type stepKindE uint8

const (
	stepModel stepKindE = iota
	stepTool
)

// Bounds on what a step keeps of the text it shows: a result or a
// reasoning can be long, and a turn keeps every step.
const (
	trailArgsMax      = 4 << 10
	trailResultMax    = 16 << 10
	trailReasoningMax = 4 << 10
	trailContentMax   = 4 << 10
)

// trailStep is one step: a model call (round, text, reasoning, tokens, the
// tools it asked for) or a tool call (name, title, arguments, result, the
// transcript's line).
type trailStep struct {
	kind  stepKindE
	round int
	// name is a tool's name; title the person's title the model gave it.
	name  string
	title string
	// args is a tool call's arguments as the model sent them, indented;
	// result what the model read back, bounded.
	args   string
	result string
	// activity is the transcript's line for a tool call.
	activity string
	// content and reasoning are what a model call wrote beside its tool
	// calls, bounded; tools how many it asked for.
	content   string
	reasoning string
	tools     int
	inTokens  int32
	outTokens int32
	started   time.Time
	took      time.Duration
	done      bool
	// refused marks a tool call the host or the coordinator refused, and
	// failed a model call that did not answer.
	refused bool
	failed  bool
}

// turnTrail is the running turn's steps, written by the tool loop and read
// by the window; ver moves with every change so the window copies only on
// news. A nil trail records nothing.
type turnTrail struct {
	mu    sync.Mutex
	steps []trailStep
	ver   uint64
}

// reset starts a turn's trail.
func (inst *turnTrail) reset() {
	if inst == nil {
		return
	}
	inst.mu.Lock()
	inst.steps = nil
	inst.ver++
	inst.mu.Unlock()
}

// begin appends a running step and returns its index.
func (inst *turnTrail) begin(s trailStep) (i int) {
	if inst == nil {
		return -1
	}
	s.started = time.Now()
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.steps = append(inst.steps, s)
	inst.ver++
	return len(inst.steps) - 1
}

// finish completes step i through f.
func (inst *turnTrail) finish(i int, f func(s *trailStep)) {
	if inst == nil || i < 0 {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if i >= len(inst.steps) {
		return
	}
	s := &inst.steps[i]
	f(s)
	s.took, s.done = time.Since(s.started), true
	inst.ver++
}

// snapshot copies the steps when they moved past ver; changed says whether
// they did.
func (inst *turnTrail) snapshot(ver uint64) (steps []trailStep, now uint64, changed bool) {
	if inst == nil {
		return nil, ver, false
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.ver == ver {
		return nil, ver, false
	}
	return append([]trailStep(nil), inst.steps...), inst.ver, true
}

// clip keeps the first n bytes of s, saying how much it left out.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n… " + strconv.Itoa(len(s)-cut) + " bytes more"
}

// clipTail keeps the last n bytes of s, saying how much it left out.
func clipTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := len(s) - n
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	return "… " + strconv.Itoa(cut) + " bytes before\n" + s[cut:]
}

// indentArgs is a tool call's arguments as the person reads them: indented
// when they are JSON, as sent when not.
func indentArgs(raw string) string {
	var buf bytes.Buffer
	if json.Indent(&buf, []byte(raw), "", "  ") != nil {
		return clip(raw, trailArgsMax)
	}
	return clip(buf.String(), trailArgsMax)
}

// stepsOfTools pairs each tool step of a landed turn with the model step
// that asked for it, the latter only on the round's first tool step: the
// details a tool entry keeps. The tool steps are in the order of the
// turn's activity lines.
func stepsOfTools(steps []trailStep) (out [][]trailStep) {
	var model *trailStep
	for i := range steps {
		s := &steps[i]
		if s.kind == stepModel {
			model = s
			continue
		}
		var own []trailStep
		if model != nil {
			own = append(own, *model)
			model = nil
		}
		out = append(out, append(own, *s))
	}
	return
}
