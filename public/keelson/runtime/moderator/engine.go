// Package moderator is the host's built-in moderator (ADR-0302): it reads
// the model service's call events and the agent dispatcher's action
// records, raises signals that tell a loop from work, and moves each
// window up a fixed ladder — note, slow, stop — through the levers
// ADR-0300 provides.
//
// The [Engine] is the policy and knows nothing of the bus; the [Service]
// feeds it events and carries out what it decides.
package moderator

import (
	"cmp"
	"slices"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

// SignalE is what a signal reports (ADR-0302 §SD3).
type SignalE uint8

const (
	SignalNone SignalE = iota
	// SignalRepeat is the same operation on the same window with the same
	// arguments, completed again and again under one task.
	SignalRepeat
	// SignalRounds is a turn whose model calls reached a round.
	SignalRounds
	// SignalContext is a turn's input grown round after round to a share of
	// the model's context.
	SignalContext
	// SignalErrors is calls refused or failed in a row.
	SignalErrors
	// SignalQueue is calls waiting long in the model service's queue, again
	// and again. It is recorded and never escalates.
	SignalQueue
)

var signalNames = [...]string{"", "repeat", "rounds", "context", "errors", "queue"}

func (inst SignalE) String() string {
	if int(inst) < len(signalNames) {
		return signalNames[inst]
	}
	return "signal-" + strconv.Itoa(int(inst))
}

// escalates says the signal moves its window up the ladder.
func (inst SignalE) escalates() (yes bool) { return inst != SignalQueue && inst != SignalNone }

// LevelE is a window's step on the ladder (ADR-0302 §SD4).
type LevelE uint8

const (
	LevelNone LevelE = iota
	LevelNoted
	LevelSlowed
	LevelStopped
)

var levelNames = [...]string{"none", "noted", "slowed", "stopped"}

func (inst LevelE) String() string {
	if int(inst) < len(levelNames) {
		return levelNames[inst]
	}
	return "level-" + strconv.Itoa(int(inst))
}

// ActionKindE is what the engine asks the service to do.
type ActionKindE uint8

const (
	ActionKindNone ActionKindE = iota
	// ActionKindSlow sets a short-lived rate rule on the window's account.
	ActionKindSlow
	// ActionKindStop stops the window's agent tasks, or denies the window's
	// model calls for a while when it drives none.
	ActionKindStop
)

var actionNames = [...]string{"none", "slow", "stop"}

func (inst ActionKindE) String() string {
	if int(inst) < len(actionNames) {
		return actionNames[inst]
	}
	return "action-" + strconv.Itoa(int(inst))
}

// Window is a signal's subject: the window whose model calls a loop
// spends, the coordinator's for an agent's actions. An agent task is
// evidence, not the subject: a coordinator's own model calls carry no
// task, so the window is the account a loop is charged to.
type Window struct {
	App      string
	Instance uint64
}

func (inst Window) String() string { return inst.App + "#" + strconv.FormatUint(inst.Instance, 10) }

// CallObs is one llm.event.call as the engine reads it.
type CallObs struct {
	At           time.Time
	CallId       string
	Window       Window
	Task         string
	Conversation string
	Turn         string
	Round        uint32
	InputTokens  int64
	// Refused says admission refused the call, Rule which rule did.
	Refused bool
	Rule    string
	// Failed says the provider call failed.
	Failed bool
	Queued time.Duration
}

// ActionObs is one action record as the engine reads it.
type ActionObs struct {
	At         time.Time
	Key        string
	Task       string
	Window     Window
	Instance   uint64
	Operation  string
	ArgsDigest string
	Result     opwire.ResultE
}

// Signal is a signal raised on a window.
type Signal struct {
	Kind   SignalE
	Window Window
	At     time.Time
	// Task is the task the evidence came from, when one did.
	Task string
	// Detail says what was seen, for the audit event and a stopped task's
	// reason.
	Detail string
	// Evidence names the calls that raised it: model call ids or action
	// keys.
	Evidence []string
	// Level is the window's level after the signal.
	Level LevelE
}

// Action is what the engine decided on a signal.
type Action struct {
	Kind   ActionKindE
	Signal Signal
}

// Thresholds are the signals' thresholds (ADR-0302 §SD3, §SD4).
type Thresholds struct {
	RepeatCount  int
	RepeatWindow time.Duration
	// Rounds is the round (counted from 1) a turn's call raises rounds at.
	Rounds uint32
	// ContextPercent of ContextTokens raises context; ContextTokens 0, the
	// model's context unknown, raises none.
	ContextPercent int64
	ContextTokens  int64
	ErrorStreak    int
	QueueWait      time.Duration
	QueueCount     int
	// Quiet is how long a window goes without an escalating signal before
	// it steps down one level.
	Quiet time.Duration
	// RulePrefix names the moderator's own rules; refusals by them are not
	// counted as errors.
	RulePrefix string
}

// DefaultThresholds is ADR-0302's first set.
func DefaultThresholds() (c Thresholds) {
	return Thresholds{RepeatCount: 3, RepeatWindow: 10 * time.Minute, Rounds: 16, ContextPercent: 80, ErrorStreak: 5,
		QueueWait: 30 * time.Second, QueueCount: 3, Quiet: 10 * time.Minute, RulePrefix: RulePrefix}
}

// RulePrefix precedes the ids of the rules the built-in moderator sets.
const RulePrefix = "moderator/"

type repeatKey struct {
	task       string
	instance   uint64
	operation  string
	argsDigest string
}

type turnState struct {
	inputs  []int64
	rounds  bool
	context bool
	at      time.Time
}

type windowState struct {
	level      LevelE
	lastRaised time.Time
	lastKind   SignalE
	counts     map[SignalE]int
	tasks      map[string]time.Time
	repeats    map[repeatKey][]time.Time
	seen       map[string]time.Time
	turns      map[string]*turnState
	streak     int
	waits      int
	lastAction ActionKindE
	lastActAt  time.Time
}

// Engine is the policy. Not safe for concurrent use: the service feeds it
// from one goroutine.
type Engine struct {
	cfg     Thresholds
	windows map[Window]*windowState
}

// NewEngine is an engine with cfg.
func NewEngine(cfg Thresholds) (inst *Engine) {
	return &Engine{cfg: cfg, windows: map[Window]*windowState{}}
}

// SetContextTokens sets the model's context size, once known.
func (inst *Engine) SetContextTokens(n int64) { inst.cfg.ContextTokens = n }

func (inst *Engine) window(w Window) (ws *windowState) {
	ws = inst.windows[w]
	if ws == nil {
		ws = &windowState{counts: map[SignalE]int{}, tasks: map[string]time.Time{}, repeats: map[repeatKey][]time.Time{},
			seen: map[string]time.Time{}, turns: map[string]*turnState{}}
		inst.windows[w] = ws
	}
	return
}

// Call reads one model call. A call no window made is not watched.
func (inst *Engine) Call(o CallObs) (signals []Signal, actions []Action) {
	if o.Window.Instance == 0 {
		return
	}
	ws := inst.window(o.Window)
	if o.Task != "" {
		ws.tasks[o.Task] = o.At
	}
	raise := func(kind SignalE, detail string) {
		s, a := inst.raise(ws, Signal{Kind: kind, Window: o.Window, At: o.At, Task: o.Task, Detail: detail, Evidence: []string{o.CallId}})
		signals = append(signals, s)
		actions = append(actions, a...)
	}
	switch {
	case o.Refused && inst.cfg.RulePrefix != "" && len(o.Rule) >= len(inst.cfg.RulePrefix) && o.Rule[:len(inst.cfg.RulePrefix)] == inst.cfg.RulePrefix:
		// The moderator's own slowing is not the window's error.
	case o.Refused || o.Failed:
		ws.streak++
		if ws.streak >= inst.cfg.ErrorStreak {
			ws.streak = 0
			raise(SignalErrors, strconv.Itoa(inst.cfg.ErrorStreak)+" model calls and actions in a row refused or failed")
		}
	default:
		ws.streak = 0
	}
	if o.Queued >= inst.cfg.QueueWait {
		ws.waits++
		if ws.waits >= inst.cfg.QueueCount {
			ws.waits = 0
			raise(SignalQueue, strconv.Itoa(inst.cfg.QueueCount)+" calls in a row waited "+inst.cfg.QueueWait.String()+" or longer for a slot")
		}
	} else if !o.Refused {
		ws.waits = 0
	}
	if o.Turn == "" || o.Refused {
		return
	}
	tk := o.Conversation + "\x00" + o.Turn
	ts := ws.turns[tk]
	if ts == nil {
		ts = &turnState{}
		ws.turns[tk] = ts
	}
	ts.at = o.At
	if !ts.rounds && inst.cfg.Rounds > 0 && o.Round+1 >= inst.cfg.Rounds {
		ts.rounds = true
		raise(SignalRounds, "a turn reached round "+strconv.FormatUint(uint64(o.Round+1), 10))
	}
	if o.InputTokens > 0 {
		ts.inputs = append(ts.inputs, o.InputTokens)
		if len(ts.inputs) > 3 {
			ts.inputs = ts.inputs[len(ts.inputs)-3:]
		}
	}
	if !ts.context && inst.cfg.ContextTokens > 0 && len(ts.inputs) == 3 &&
		ts.inputs[0] < ts.inputs[1] && ts.inputs[1] < ts.inputs[2] &&
		ts.inputs[2]*100 >= inst.cfg.ContextPercent*inst.cfg.ContextTokens {
		ts.context = true
		raise(SignalContext, "a turn's input grew each round to "+strconv.FormatInt(ts.inputs[2], 10)+" of "+
			strconv.FormatInt(inst.cfg.ContextTokens, 10)+" context tokens")
	}
	return
}

// Action reads one action record. Each call counts once, by its key, when
// a record first shows it done or not done.
func (inst *Engine) Action(o ActionObs) (signals []Signal, actions []Action) {
	if o.Window.Instance == 0 || o.Result == opwire.ResultInFlight || o.Result == opwire.ResultWaiting {
		return
	}
	ws := inst.window(o.Window)
	if o.Task != "" {
		ws.tasks[o.Task] = o.At
	}
	seenKey := o.Task + "\x00" + o.Key
	if _, dup := ws.seen[seenKey]; dup {
		return
	}
	ws.seen[seenKey] = o.At
	raise := func(kind SignalE, detail string, evidence []string) {
		s, a := inst.raise(ws, Signal{Kind: kind, Window: o.Window, At: o.At, Task: o.Task, Detail: detail, Evidence: evidence})
		signals = append(signals, s)
		actions = append(actions, a...)
	}
	if o.Result == opwire.ResultNotDone {
		ws.streak++
		if ws.streak >= inst.cfg.ErrorStreak {
			ws.streak = 0
			raise(SignalErrors, strconv.Itoa(inst.cfg.ErrorStreak)+" model calls and actions in a row refused or failed", []string{o.Key})
		}
		return
	}
	ws.streak = 0
	rk := repeatKey{task: o.Task, instance: o.Instance, operation: o.Operation, argsDigest: o.ArgsDigest}
	times := ws.repeats[rk][:0:0]
	for _, t := range ws.repeats[rk] {
		if o.At.Sub(t) < inst.cfg.RepeatWindow {
			times = append(times, t)
		}
	}
	times = append(times, o.At)
	if len(times) >= inst.cfg.RepeatCount {
		delete(ws.repeats, rk)
		raise(SignalRepeat, o.Operation+" on window "+strconv.FormatUint(o.Instance, 10)+" with the same arguments "+
			strconv.Itoa(len(times))+" times within "+inst.cfg.RepeatWindow.String(), []string{o.Key})
		return
	}
	ws.repeats[rk] = times
	return
}

// raise records a signal on its window and, when it escalates, moves the
// window one step up and says what to do.
func (inst *Engine) raise(ws *windowState, s Signal) (out Signal, actions []Action) {
	ws.counts[s.Kind]++
	if s.Kind.escalates() {
		ws.lastRaised, ws.lastKind = s.At, s.Kind
		if ws.level < LevelStopped {
			ws.level++
		}
		switch ws.level {
		case LevelSlowed:
			actions = []Action{{Kind: ActionKindSlow}}
		case LevelStopped:
			actions = []Action{{Kind: ActionKindStop}}
		}
	}
	s.Level = ws.level
	for i := range actions {
		actions[i].Signal = s
		ws.lastAction, ws.lastActAt = actions[i].Kind, s.At
	}
	return s, actions
}

// Step is a window stepping down a level.
type Step struct {
	Window Window
	Level  LevelE
}

// Tick steps down each window quiet for Quiet, one level per Quiet, and
// forgets what no window can count any more.
func (inst *Engine) Tick(now time.Time) (steps []Step) {
	forget := now.Add(-max(inst.cfg.RepeatWindow, inst.cfg.Quiet, time.Hour))
	for w, ws := range inst.windows {
		if ws.level > LevelNone && now.Sub(ws.lastRaised) >= inst.cfg.Quiet {
			ws.level--
			ws.lastRaised = now
			steps = append(steps, Step{Window: w, Level: ws.level})
		}
		for k, t := range ws.seen {
			if t.Before(forget) {
				delete(ws.seen, k)
			}
		}
		for k, ts := range ws.turns {
			if ts.at.Before(forget) {
				delete(ws.turns, k)
			}
		}
		for k, times := range ws.repeats {
			if len(times) == 0 || times[len(times)-1].Before(forget) {
				delete(ws.repeats, k)
			}
		}
		for k, t := range ws.tasks {
			if t.Before(forget) {
				delete(ws.tasks, k)
			}
		}
		if ws.level == LevelNone && len(ws.seen) == 0 && len(ws.turns) == 0 && len(ws.repeats) == 0 && len(ws.tasks) == 0 &&
			ws.lastRaised.Before(forget) {
			delete(inst.windows, w)
		}
	}
	slices.SortFunc(steps, func(a, b Step) int { return cmp.Compare(a.Window.Instance, b.Window.Instance) })
	return
}

// State is a window as keelson('moderator_signals') shows it.
type State struct {
	Window     Window
	Level      LevelE
	LastSignal SignalE
	LastAt     time.Time
	Counts     map[SignalE]int
	Tasks      []string
	LastAction ActionKindE
	LastActAt  time.Time
}

// States lists the windows the engine watches, by instance.
func (inst *Engine) States() (out []State) {
	for w, ws := range inst.windows {
		st := State{Window: w, Level: ws.level, LastSignal: ws.lastKind, LastAt: ws.lastRaised, Counts: map[SignalE]int{},
			LastAction: ws.lastAction, LastActAt: ws.lastActAt}
		for k, v := range ws.counts {
			st.Counts[k] = v
		}
		for t := range ws.tasks {
			st.Tasks = append(st.Tasks, t)
		}
		slices.Sort(st.Tasks)
		out = append(out, st)
	}
	slices.SortFunc(out, func(a, b State) int { return cmp.Compare(a.Window.Instance, b.Window.Instance) })
	return
}
