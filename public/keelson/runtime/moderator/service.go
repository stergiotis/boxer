package moderator

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/ration"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// AppId is the moderator's identity on the bus: what BOXER_LLM_MODERATORS
// lists, and what its rules and stopped tasks name (ADR-0302 §SD1).
const AppId app.AppIdT = "runtime.moderator"

// SubjectSignal carries every signal the moderator raises, for a console
// to follow. Publish only.
const SubjectSignal = "moderator.event.signal"

// TableSignals is keelson('moderator_signals'): the windows the moderator
// watches and their level.
const TableSignals = "moderator_signals"

// Enabled turns the built-in moderator on (ADR-0302 §SD1).
var Enabled = env.NewBool(env.Spec{
	Name:        "BOXER_MODERATOR",
	Default:     "false",
	Description: "run the host's built-in moderator (ADR-0302): it watches model calls and agent actions for loops and slows, then stops, the windows that loop; true also lists it in BOXER_LLM_MODERATORS",
	Category:    env.CategoryLLM,
})

// Caps is what the moderator's bus client holds: describe (for the
// model's context size), the rule verbs and events, the task verbs and the
// action records, and its own signal events.
func Caps() (caps []app.SubjectFilter) {
	const reason = "moderator: watch model use and agent work for loops (ADR-0302)"
	caps = append(caps, llm.ClientCaps(reason)...)
	caps = append(caps, llm.ModeratorCaps(reason)...)
	caps = append(caps, agent.ModeratorCaps(reason)...)
	caps = append(caps, app.SubjectFilter{Pattern: SubjectSignal, Direction: app.CapDirectionPub, Reason: reason})
	return
}

// Config is the service's: the signals' thresholds and what its actions
// set (ADR-0302 §SD4).
type Config struct {
	Thresholds Thresholds
	// SlowCalls is the Slow rule's model calls a minute; SlowFor and
	// DenyFor are how long the Slow and Deny rules last.
	SlowCalls int64
	SlowFor   time.Duration
	DenyFor   time.Duration
	// Tick is how often quiet windows are stepped down.
	Tick time.Duration
	// Trail records signals and actions; nil records none.
	Trail *trail.Recorder
}

// DefaultConfig is ADR-0302's.
func DefaultConfig() (c Config) {
	return Config{Thresholds: DefaultThresholds(), SlowCalls: 6, SlowFor: 10 * time.Minute, DenyFor: 10 * time.Minute,
		Tick: 30 * time.Second}
}

// inputQueueLen bounds the events waiting for the engine; past it new ones
// are dropped and counted, so a burst never holds up a publisher.
const inputQueueLen = 4096

type input struct {
	call   *llm.CallEvent
	action *agent.ActionRecord
}

// Service is the built-in moderator: it feeds the engine the model
// service's call events and the dispatcher's action records, and carries
// out what the engine decides. Its handlers only queue; one goroutine
// reads the queue, so the engine needs no lock but the snapshot's.
type Service struct {
	cfg      Config
	log      zerolog.Logger
	bus      *inprocbus.Client
	llmCli   *llm.Client
	mod      *llm.Moderator
	agentCli *agent.Client
	in       chan input
	unsubs   []func()
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}

	mu      sync.Mutex
	eng     *Engine
	dropped uint64
}

// NewService subscribes the moderator and starts it. The caller MUST
// invoke Close. The model service must list AppId as a moderator, and the
// agent dispatcher as well, for its actions to be admitted.
func NewService(bus *inprocbus.Inst, log zerolog.Logger, cfg Config) (s *Service, err error) {
	if bus == nil {
		err = eh.Errorf("moderator: nil bus")
		return
	}
	client := bus.NewClient(AppId, Caps())
	s = &Service{cfg: cfg, log: log.With().Str("app", string(AppId)).Logger(), bus: client, llmCli: llm.NewClient(client),
		mod: llm.NewModerator(client), agentCli: agent.NewClient(client), in: make(chan input, inputQueueLen),
		eng: NewEngine(cfg.Thresholds), done: make(chan struct{})}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	for _, sub := range []struct {
		subject string
		handler app.MsgHandlerFunc
	}{{llm.SubjectEventCall, s.hearCall}, {agent.SubjectActionRecorded, s.hearAction}} {
		var unsub func()
		unsub, err = client.Subscribe(sub.subject, sub.handler)
		if err != nil {
			s.Close()
			err = eb.Build().Str("subject", sub.subject).Errorf("moderator: subscribe: %w", err)
			return nil, err
		}
		s.unsubs = append(s.unsubs, unsub)
	}
	go s.run()
	return
}

// Close stops the moderator. Its rules lapse on their own.
func (inst *Service) Close() {
	for _, u := range inst.unsubs {
		u()
	}
	inst.unsubs = nil
	if inst.cancel != nil {
		inst.cancel()
		select {
		case <-inst.done:
		case <-time.After(5 * time.Second):
		}
		inst.cancel = nil
	}
	if inst.bus != nil {
		if err := inst.bus.Close(); err != nil {
			inst.log.Warn().Err(err).Msg("moderator: closing the bus client")
		}
		inst.bus = nil
	}
}

func (inst *Service) enqueue(in input) {
	select {
	case inst.in <- in:
	default:
		inst.mu.Lock()
		inst.dropped++
		inst.mu.Unlock()
	}
}

func (inst *Service) hearCall(msg *app.Msg) {
	e, err := llm.DecodeCallEvent(msg.Payload)
	if err != nil {
		inst.log.Debug().Err(err).Msg("moderator: a call event does not decode")
		return
	}
	inst.enqueue(input{call: &e})
}

func (inst *Service) hearAction(msg *app.Msg) {
	r, err := agent.DecodeActionEvent(msg.Payload)
	if err != nil {
		inst.log.Debug().Err(err).Msg("moderator: an action event does not decode")
		return
	}
	inst.enqueue(input{action: &r})
}

func (inst *Service) run() {
	defer close(inst.done)
	tick := inst.cfg.Tick
	if tick <= 0 {
		tick = 30 * time.Second
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	inst.learnContext()
	for {
		select {
		case <-inst.ctx.Done():
			return
		case <-ticker.C:
			inst.learnContext()
			inst.mu.Lock()
			steps := inst.eng.Tick(time.Now())
			inst.mu.Unlock()
			for _, st := range steps {
				inst.log.Info().Str("window", st.Window.String()).Str("level", st.Level.String()).Msg("moderator: a quiet window steps down")
			}
		case in := <-inst.in:
			inst.handle(in)
		}
	}
}

// learnContext asks the model service for the model's context size until
// it knows it.
func (inst *Service) learnContext() {
	inst.mu.Lock()
	known := inst.eng.cfg.ContextTokens > 0
	inst.mu.Unlock()
	if known {
		return
	}
	ctx, cancel := context.WithTimeout(inst.ctx, 5*time.Second)
	defer cancel()
	d, err := inst.llmCli.Describe(ctx)
	if err != nil || d.ContextTokens <= 0 {
		return
	}
	inst.mu.Lock()
	inst.eng.SetContextTokens(int64(d.ContextTokens))
	inst.mu.Unlock()
}

func (inst *Service) handle(in input) {
	var signals []Signal
	var actions []Action
	inst.mu.Lock()
	switch {
	case in.call != nil:
		e := in.call
		signals, actions = inst.eng.Call(CallObs{At: e.At, CallId: e.CallId, Window: Window{App: e.Chain.App, Instance: e.Chain.Instance},
			Task: e.Chain.Task, Conversation: e.Conversation, Turn: e.Turn, Round: e.Round, InputTokens: e.Usage[ration.QuantityInputTokens],
			Refused: e.Admission == ration.OutcomeRefused.String(), Rule: e.Rule, Failed: e.Failed, Queued: e.Queued})
	case in.action != nil:
		r := in.action
		result := opwire.ResultInFlight
		if p, ok := opwire.ParsePhase(r.Phase); ok {
			result = p.Result()
		}
		signals, actions = inst.eng.Action(ActionObs{At: r.At, Key: r.Key, Task: r.Task,
			Window: Window{App: string(r.Actor), Instance: r.ActorInstance}, Instance: r.Instance, Operation: r.Operation,
			ArgsDigest: r.ArgsDigest, Result: result})
	}
	inst.mu.Unlock()
	for _, s := range signals {
		inst.announce(s)
	}
	for _, a := range actions {
		inst.act(a)
	}
}

// announce publishes a signal and records it.
func (inst *Service) announce(s Signal) {
	if payload, err := buscodec.Encode(wireOfSignal(s)); err == nil {
		if err = inst.bus.Publish(SubjectSignal, payload); err != nil {
			inst.log.Debug().Err(err).Msg("moderator: publish a signal")
		}
	}
	inst.log.Info().Str("window", s.Window.String()).Str("signal", s.Kind.String()).Str("level", s.Level.String()).
		Str("task", s.Task).Str("detail", s.Detail).Msg("moderator: signal")
	inst.audit("signal", trail.OutcomeOk, s, nil, []string{"signal", s.Kind.String(), "level", s.Level.String(), "detail", s.Detail})
}

// act carries out an action through the ADR-0300 levers, and records it
// with the signal that caused it.
func (inst *Service) act(a Action) {
	s := a.Signal
	ctx, cancel := context.WithTimeout(inst.ctx, 10*time.Second)
	defer cancel()
	key := strconv.FormatUint(s.Window.Instance, 10)
	account := ration.Selector{Kind: ration.AccountKindInstance, Key: key}
	reason := s.Kind.String() + ": " + s.Detail
	now := time.Now()
	switch a.Kind {
	case ActionKindSlow:
		err := inst.mod.Set(ctx, ration.Rule{Id: RulePrefix + "slow/" + key, Select: account, Kind: ration.RuleKindRate,
			Quantity: ration.QuantityCalls, Limit: inst.cfg.SlowCalls, Window: time.Minute, Until: now.Add(inst.cfg.SlowFor), Reason: reason})
		inst.recordAction("slow", s, nil, err)
	case ActionKindStop:
		tasks, err := inst.agentCli.ModerateStop(ctx, agent.ModerateTarget{Instance: s.Window.Instance}, reason)
		if err == nil && len(tasks) == 0 {
			// A window that drives no task loops on the model alone: its
			// calls are denied for a while instead.
			err = inst.mod.Set(ctx, ration.Rule{Id: RulePrefix + "deny/" + key, Select: account, Kind: ration.RuleKindDeny,
				Until: now.Add(inst.cfg.DenyFor), Reason: reason})
		}
		if _, cerr := inst.mod.CancelAccount(ctx, ration.Account{Kind: ration.AccountKindInstance, Key: key}, reason); cerr != nil && err == nil {
			err = cerr
		}
		inst.recordAction("stop", s, tasks, err)
	}
}

func (inst *Service) recordAction(action string, s Signal, tasks []string, err error) {
	outcome := trail.OutcomeOk
	attrs := []string{"signal", s.Kind.String(), "level", s.Level.String(), "detail", s.Detail}
	lg := inst.log.Warn()
	if err != nil {
		outcome = trail.OutcomeFailed
		attrs = append(attrs, "error", err.Error())
		lg = inst.log.Error().Err(err)
	}
	lg.Str("window", s.Window.String()).Str("action", action).Strs("tasks", tasks).Str("detail", s.Detail).Msg("moderator: action")
	inst.audit(action, outcome, s, tasks, attrs)
}

// The audit vocabulary (ADR-0302 §SD3, ADR-0296).
const (
	auditDomain    = "moderator"
	auditRefCall   = "moderator-evidence"
	auditRefTask   = "agent-task"
	auditRefWindow = "window"
)

func (inst *Service) audit(action string, outcome string, s Signal, tasks []string, attrs []string) {
	if !inst.cfg.Trail.Durable() {
		return
	}
	row := trail.AuditEvent{Domain: auditDomain, Action: action, Outcome: outcome, Retention: trail.RetentionTrail}
	ref := func(ty, v string) {
		if v != "" && len(row.RefTypes) < trail.MaxRefs {
			row.RefTypes, row.RefValues = append(row.RefTypes, ty), append(row.RefValues, bound(v))
		}
	}
	ref(auditRefWindow, s.Window.String())
	ref(auditRefTask, s.Task)
	for _, t := range tasks {
		if t != s.Task {
			ref(auditRefTask, t)
		}
	}
	for _, e := range s.Evidence {
		ref(auditRefCall, e)
	}
	for i := 0; i+1 < len(attrs) && len(row.AttrKeys) < trail.MaxAttrs; i += 2 {
		if attrs[i+1] != "" {
			row.AttrKeys, row.AttrValues = append(row.AttrKeys, attrs[i]), append(row.AttrValues, bound(attrs[i+1]))
		}
	}
	c := trail.Context{Origin: inst.cfg.Trail.OriginOf(AppId, 0)}
	if err := inst.cfg.Trail.Event(inst.ctx, time.Now().UTC(), c, row); err != nil {
		inst.log.Warn().Err(err).Str("action", action).Msg("moderator: write the audit event")
	}
	inst.cfg.Trail.FlushSoon()
}

// bound cuts s to what an audit value may hold.
func bound(s string) (b string) {
	if r := []rune(s); len(r) > trail.MaxValueRunes {
		return string(r[:trail.MaxValueRunes-1]) + "…"
	}
	return s
}

// States is the engine's windows, for the table.
func (inst *Service) States() (out []State) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.eng.States()
}
