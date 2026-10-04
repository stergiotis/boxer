package agent

import (
	"encoding/json/v2"
	"slices"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/capture"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
)

// ActionRecord is one row of the action record (ADR-0269 §SD9): one when
// the dispatcher decides a call, one when the call reaches its final phase.
type ActionRecord struct {
	At   time.Time
	Task string
	// Epoch is the task's epoch at the row; Conversation and Turn what the
	// coordinator said the call belongs to.
	Epoch         uint64
	Conversation  string
	Turn          string
	Actor         app.AppIdT
	ActorInstance uint64
	// Key is the caller's key for the call. ModelCall, ToolCallId and
	// ToolIndex are the model call whose reply asked for it, the provider's
	// id for the tool call and its index in that reply, as the coordinator
	// stated them (ADR-0277 §SD1).
	Key        string
	ModelCall  string
	ToolCallId string
	ToolIndex  uint32
	CallId     string
	Instance   uint64
	App        app.AppIdT
	Operation  string
	Effect     string
	ArgsDigest string
	// Decision is "dispatch" for the dispatcher's row and "final" for the
	// row written at the final phase.
	Decision   string
	Phase      string
	Reason     string
	BudgetLeft int32
	Test       bool
	// Tainted says the conversation had read untrusted content by then;
	// Confined that the call's outcome carried confined content.
	Tainted  bool
	Confined bool
}

// GrantRow is one task grant as the grants table shows it.
type GrantRow struct {
	Task          string
	Actor         app.AppIdT
	ActorInstance uint64
	Plan          string
	Entries       []string
	// Launches are the apps the task may open, "app:mode:count" each.
	Launches     []string
	Destinations []string
	CallsUsed    int32
	CallsBudget  int32
	Deadline     time.Time
	Epoch        uint64
	Revoked      string
	Created      time.Time
	Test         bool
	// Desktop is the task's mode over the desktop as a whole, empty when
	// the grant does not name it (ADR-0276 §SD4).
	Desktop string
	// Instances are the window keys the task's entries name, ascending:
	// Entries as numbers, for joining against keelson('windows').
	Instances []uint64
}

// RecordsI is the service as a read side.
type RecordsI interface {
	Actions() (rows []ActionRecord)
	Grants() (rows []GrantRow)
}

func (inst *Service) record(t *task, rec *callRec, decision string, out opwire.Outcome) {
	r := ActionRecord{
		At: time.Now(), Key: rec.key, CallId: rec.callId, Instance: rec.instance, App: rec.app,
		Operation: rec.spec.Name, Effect: rec.spec.Effect.String(), ArgsDigest: rec.argsDigest,
		Decision: decision, Phase: out.Phase.String(), Reason: out.Reason, Confined: out.Confined,
		Turn: rec.turn, Actor: rec.actor, ActorInstance: rec.actorInstance,
	}
	if c := rec.cause; c.Has {
		r.ModelCall, r.ToolCallId, r.ToolIndex = c.Val.ModelCall, c.Val.ToolCall.Val, c.Val.ToolIndex
	}
	if t != nil {
		inst.mu.Lock()
		r.Task, r.Actor, r.ActorInstance, r.Test = t.id, t.actor, t.actorInstance, t.test
		r.Epoch, r.Conversation = t.epoch, t.conversation
		r.BudgetLeft = int32(t.callsBudget - t.callsUsed)
		r.Tainted = inst.tainted(t)
		inst.mu.Unlock()
	}
	inst.recMu.Lock()
	if len(inst.records) < keepRecords {
		inst.records = append(inst.records, r)
	} else {
		inst.records[inst.recHead] = r
		inst.recHead = (inst.recHead + 1) % keepRecords
	}
	if inst.cfg.ActionsLog != nil {
		if line, err := json.Marshal(actionLine{ActionRecord: r, Args: rec.args}); err == nil {
			_, _ = inst.cfg.ActionsLog.Write(append(line, '\n'))
		}
	}
	inst.recMu.Unlock()
	inst.persist(r)
}

// actionLine is a row of the actions file: the record, and under a test
// grant the call's arguments as the model sent them.
type actionLine struct {
	ActionRecord `json:",inline"`
	Args         string `json:",omitempty"`
}

// recordGrantRefusal writes a refused test grant to the actions file, with
// the request as the model's coordinator sent it.
func (inst *Service) recordGrantRefusal(msg *app.Msg, req wireGrantRequest, reason string) {
	if inst.cfg.ActionsLog == nil {
		return
	}
	args, _ := json.Marshal(req)
	r := ActionRecord{At: time.Now(), Actor: msg.Sender, ActorInstance: msg.SenderInstance, Operation: "grant",
		Decision: "grant", Phase: opwire.PhaseRefused.String(), Reason: reason, Test: true}
	inst.recMu.Lock()
	defer inst.recMu.Unlock()
	if line, err := json.Marshal(actionLine{ActionRecord: r, Args: string(args)}); err == nil {
		_, _ = inst.cfg.ActionsLog.Write(append(line, '\n'))
	}
}

// persist buffers the row for the trail and wakes its flusher, so a call
// never waits on the store. A failed buffer is logged; the in-process
// record keeps the row either way.
func (inst *Service) persist(r ActionRecord) {
	c, cause, row := TrailRowOf(inst.cfg.Trail, r)
	if err := inst.cfg.Trail.AgentAction(r.At, c, cause, row); err != nil {
		inst.log.Warn().Err(err).Str("task", r.Task).Str("key", r.Key).Msg("agent: buffer action row")
		return
	}
	inst.cfg.Trail.FlushSoon()
}

// captureRec is what a capture's record names beyond its call.
type captureRec struct {
	format   capture.FormatE
	windows  []uint64
	recorded bool
}

// recordCapture writes a capture's row on the trail (ADR-0281 §SD6), once:
// when the capture service denies it, or when it reaches its final phase.
func (inst *Service) recordCapture(t *task, rec *callRec, d capture.Decision, info capture.Info, phase opwire.PhaseE,
	reason string, confined bool) {
	if rec.capture == nil || rec.capture.recorded {
		return
	}
	rec.capture.recorded = true
	r := ActionRecord{At: time.Now(), Key: rec.key, CallId: rec.callId, Instance: rec.instance, App: rec.app,
		Turn: rec.turn, Actor: rec.actor, ActorInstance: rec.actorInstance}
	if t != nil {
		// The caller holds mu.
		r.Task, r.Actor, r.ActorInstance, r.Epoch, r.Conversation = t.id, t.actor, t.actorInstance, t.epoch, t.conversation
	}
	c, _, _ := TrailRowOf(inst.cfg.Trail, r)
	decision := "deny"
	if d.Effect == capture.EffectPermit {
		decision = "permit"
	}
	row := trail.AgentCapture{
		Format: string(rec.capture.format), Windows: rec.capture.windows, Decision: decision, Policy: d.Policy,
		Obligations: info.Obligations, SpansDigest: info.SpansDigest, Digest: info.Digest, Bytes: uint64(max(info.Bytes, 0)),
		Phase: phase.String(), Confined: confined,
	}
	if reason != "" {
		row.Reason = []string{reason}
	}
	if err := inst.cfg.Trail.AgentCapture(r.At, c, row); err != nil {
		inst.log.Warn().Err(err).Str("task", r.Task).Str("key", r.Key).Msg("agent: buffer capture row")
		return
	}
	inst.cfg.Trail.FlushSoon()
}

// Durable reports whether the action record is also kept on boxer.facts.
func (inst *Service) Durable() (durable bool) { return inst.cfg.Trail.Durable() }

// TrailRowOf is the trail row of r and the context components it composes
// (ADR-0277 §SD2): the coordinator window as origin, the conversation and
// turn, the task with the dispatcher's call, and the model call that asked.
func TrailRowOf(rec *trail.Recorder, r ActionRecord) (c trail.Context, cause option.Option[trail.Cause], row trail.AgentAction) {
	c.Origin = rec.OriginOf(r.Actor, r.ActorInstance)
	if r.Conversation != "" {
		conv := trail.Conversation{Conversation: r.Conversation}
		if r.Turn != "" {
			conv.Turn = option.Some(r.Turn)
		}
		c.Conversation = option.Some(conv)
	}
	if r.Task != "" {
		d := trail.Delegation{Task: r.Task, Epoch: r.Epoch}
		if r.CallId != "" {
			d.Call = option.Some(r.CallId)
		}
		c.Delegation = option.Some(d)
	}
	if r.ModelCall != "" {
		cs := trail.Cause{ModelCall: r.ModelCall, ToolIndex: r.ToolIndex}
		if r.ToolCallId != "" {
			cs.ToolCall = option.Some(r.ToolCallId)
		}
		cause = option.Some(cs)
	}
	row = trail.AgentAction{
		Key: r.Key, Instance: r.Instance, App: string(r.App), Operation: r.Operation, Effect: r.Effect, ArgsDigest: r.ArgsDigest,
		Decision: r.Decision, Phase: r.Phase, BudgetLeft: uint32(max(r.BudgetLeft, 0)), Test: r.Test,
		Tainted: r.Tainted, Confined: r.Confined,
	}
	if r.Reason != "" {
		row.Reason = []string{r.Reason}
	}
	return
}

// grantEvent buffers one event in the life of a grant (ADR-0277 §SD2):
// what was asked, what the person decided, how the task ended. r is the
// request the event is about, t the task; either may be nil. A request
// names what was asked for; without one the row carries the grant as it
// stands. The caller holds mu when t is set.
func (inst *Service) grantEvent(event string, decidedBy string, reason string, t *task, r *request) {
	if !inst.cfg.Trail.Durable() {
		return
	}
	var c trail.Context
	row := trail.AgentGrant{Event: event, DecidedBy: decidedBy}
	conversation := ""
	switch {
	case r != nil:
		c.Origin, conversation = inst.cfg.Trail.OriginOf(r.actor, r.actorInstance), r.conversation
	case t != nil:
		c.Origin, conversation = inst.cfg.Trail.OriginOf(t.actor, t.actorInstance), t.conversation
	}
	if t == nil && r != nil {
		t = r.task
	}
	if t != nil {
		c.Delegation = option.Some(trail.Delegation{Task: t.id, Epoch: t.epoch})
		if conversation == "" {
			conversation = t.conversation
		}
		row.Plan, row.Entries, row.Launches, row.Destinations = t.plan, t.entryStrings(), t.launchStrings(), slices.Clone(t.destinations)
		row.CallsBudget, row.DeadlineMs = uint32(max(t.callsBudget, 0)), t.deadline.UnixMilli()
	}
	if r != nil {
		// What was asked for: not yet decided, or declined.
		row.Plan, row.Entries, row.Launches, row.Destinations = r.plan, r.wantedStrings(), nil, slices.Clone(r.destinations)
		for _, l := range r.launches {
			row.Launches = append(row.Launches, l.App+":"+l.Mode+":"+strconv.FormatUint(uint64(l.Count), 10))
		}
	}
	if conversation != "" {
		c.Conversation = option.Some(trail.Conversation{Conversation: conversation})
	}
	row.PlanDigest = trail.ContentDigest(row.Plan)
	if reason != "" {
		row.Reason = []string{reason}
	}
	if err := inst.cfg.Trail.AgentGrant(time.Now(), c, row); err != nil {
		inst.log.Warn().Err(err).Str("event", event).Msg("agent: buffer grant event")
		return
	}
	inst.cfg.Trail.FlushSoon()
}

// decider is who stands in for the person's decision: the host under test
// grants, the person otherwise.
func (inst *Service) decider() (who string) {
	if inst.cfg.TestGrants {
		return "host"
	}
	return "person"
}

// entryStrings are the task's entries, "instance:app:mode[:operations]"
// each, sorted.
func (inst *task) entryStrings() (out []string) {
	for _, e := range inst.entries {
		s := strconv.FormatUint(e.instance, 10) + ":" + string(e.app) + ":" + e.mode.String()
		if len(e.ops) > 0 {
			s += ":" + joinComma(e.ops)
		}
		out = append(out, s)
	}
	slices.Sort(out)
	return
}

// launchStrings are the apps the task may open, "app:mode:count" each,
// sorted.
func (inst *task) launchStrings() (out []string) {
	for id, l := range inst.launches {
		out = append(out, string(id)+":"+l.mode.String()+":"+strconv.Itoa(l.count))
	}
	slices.Sort(out)
	return
}

// wantedStrings are the windows a request asks for, "instance::mode
// [:operations]" each, sorted; the app is not known until it is granted.
func (inst *request) wantedStrings() (out []string) {
	for k, m := range inst.wanted {
		s := strconv.FormatUint(k, 10) + "::" + m.String()
		if ops := inst.wantedOps[k]; len(ops) > 0 {
			s += ":" + joinComma(ops)
		}
		out = append(out, s)
	}
	slices.Sort(out)
	return
}

// Actions returns the action record, oldest first.
func (inst *Service) Actions() (rows []ActionRecord) {
	inst.recMu.Lock()
	defer inst.recMu.Unlock()
	rows = make([]ActionRecord, 0, len(inst.records))
	rows = append(rows, inst.records[inst.recHead:]...)
	rows = append(rows, inst.records[:inst.recHead]...)
	return
}

// Grants returns every grant this process issued, revoked ones included.
func (inst *Service) Grants() (rows []GrantRow) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, t := range inst.tasks {
		g := GrantRow{Task: t.id, Actor: t.actor, ActorInstance: t.actorInstance, Plan: t.plan,
			Destinations: t.destinations, CallsUsed: int32(t.callsUsed), CallsBudget: int32(t.callsBudget),
			Deadline: t.deadline, Epoch: t.epoch, Revoked: t.revoked, Created: t.created, Test: t.test}
		g.Entries = t.entryStrings()
		g.Launches = t.launchStrings()
		for k := range t.entries {
			g.Instances = append(g.Instances, k)
		}
		slices.Sort(g.Instances)
		if t.desktop != ModeUnspecified {
			g.Desktop = t.desktop.String()
		}
		rows = append(rows, g)
	}
	slices.SortFunc(rows, func(a, b GrantRow) int { return a.Created.Compare(b.Created) })
	return
}

func joinComma(ss []string) (s string) {
	for i, x := range ss {
		if i > 0 {
			s += ","
		}
		s += x
	}
	return
}

// RegisterIntrospect exposes the grants and the action record as
// keelson.agent_grants and keelson.agent_actions. A nil svc registers empty
// tables, so the table names do not depend on whether the service started.
func RegisterIntrospect(reg *introspect.Registry, svc RecordsI) (err error) {
	err = reg.Register(grantsProvider{svc: svc})
	if err != nil {
		return
	}
	err = reg.Register(actionsProvider{svc: svc})
	return
}

type grantsProvider struct{ svc RecordsI }

func (grantsProvider) Name() string                         { return TableGrants }
func (grantsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (inst grantsProvider) Schema() *arrow.Schema           { return grantsTable(nil).Schema() }
func (inst grantsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []GrantRow
	if inst.svc != nil {
		rows = inst.svc.Grants()
	}
	return grantsTable(rows).Build(proj, len(rows)), nil
}

func grantsTable(rows []GrantRow) *introspect.Table {
	r := func(i int) *GrantRow { return &rows[i] }
	return introspect.NewTable().
		String("task", func(i int) string { return r(i).Task }).
		String("actor", func(i int) string { return string(r(i).Actor) }).
		Uint64("actor_instance", func(i int) uint64 { return r(i).ActorInstance }).
		String("plan", func(i int) string { return r(i).Plan }).
		// "instance:app:mode[:operations]" per entry.
		StringList("entries", func(i int) []string { return r(i).Entries }).
		// "app:mode:count" per app the task may open (ADR-0283 §SD2).
		StringList("launches", func(i int) []string { return r(i).Launches }).
		StringList("destinations", func(i int) []string { return r(i).Destinations }).
		Int32("calls_used", func(i int) int32 { return r(i).CallsUsed }).
		Int32("calls_budget", func(i int) int32 { return r(i).CallsBudget }).
		Int64("deadline_unix_ms", func(i int) int64 { return r(i).Deadline.UnixMilli() }).
		Uint64("epoch", func(i int) uint64 { return r(i).Epoch }).
		String("revoked", func(i int) string { return r(i).Revoked }).
		Int64("created_unix_ms", func(i int) int64 { return r(i).Created.UnixMilli() }).
		Bool("test", func(i int) bool { return r(i).Test }).
		String("desktop", func(i int) string { return r(i).Desktop })
}

type actionsProvider struct{ svc RecordsI }

func (actionsProvider) Name() string                         { return TableActions }
func (actionsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (inst actionsProvider) Schema() *arrow.Schema           { return actionsTable(nil).Schema() }
func (inst actionsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []ActionRecord
	if inst.svc != nil {
		rows = inst.svc.Actions()
	}
	return actionsTable(rows).Build(proj, len(rows)), nil
}

func actionsTable(rows []ActionRecord) *introspect.Table {
	r := func(i int) *ActionRecord { return &rows[i] }
	return introspect.NewTable().
		Int64("at_unix_ms", func(i int) int64 { return r(i).At.UnixMilli() }).
		String("task", func(i int) string { return r(i).Task }).
		String("actor", func(i int) string { return string(r(i).Actor) }).
		Uint64("actor_instance", func(i int) uint64 { return r(i).ActorInstance }).
		String("conversation", func(i int) string { return r(i).Conversation }).
		String("turn", func(i int) string { return r(i).Turn }).
		// The caller's key for the call, and what the coordinator said asked
		// for it: the model call and the provider's tool-call id
		// (OpenTelemetry gen_ai.tool.call.id).
		String("key", func(i int) string { return r(i).Key }).
		String("model_call", func(i int) string { return r(i).ModelCall }).
		String("tool_call_id", func(i int) string { return r(i).ToolCallId }).
		String("call_id", func(i int) string { return r(i).CallId }).
		Uint64("instance", func(i int) uint64 { return r(i).Instance }).
		String("app", func(i int) string { return string(r(i).App) }).
		String("operation", func(i int) string { return r(i).Operation }).
		String("effect", func(i int) string { return r(i).Effect }).
		String("args_digest", func(i int) string { return r(i).ArgsDigest }).
		String("decision", func(i int) string { return r(i).Decision }).
		String("phase", func(i int) string { return r(i).Phase }).
		String("reason", func(i int) string { return r(i).Reason }).
		Int32("budget_left", func(i int) int32 { return r(i).BudgetLeft }).
		Bool("test", func(i int) bool { return r(i).Test }).
		Bool("tainted", func(i int) bool { return r(i).Tainted }).
		Bool("confined", func(i int) bool { return r(i).Confined })
}
