package agent

import (
	"slices"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// ActionRecord is one row of the action record (ADR-0269 §SD9): one when
// the dispatcher decides a call, one when the call reaches its final phase.
type ActionRecord struct {
	At            time.Time
	Task          string
	Actor         app.AppIdT
	ActorInstance uint64
	// Key is the caller's key for the call: its tool-call id.
	Key        string
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
}

// GrantRow is one task grant as the grants table shows it.
type GrantRow struct {
	Task          string
	Actor         app.AppIdT
	ActorInstance uint64
	Plan          string
	Entries       []string
	Destinations  []string
	CallsUsed     int32
	CallsBudget   int32
	Deadline      time.Time
	Epoch         uint64
	Revoked       string
	Created       time.Time
	Test          bool
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
		Decision: decision, Phase: out.Phase.String(), Reason: out.Reason,
	}
	if t != nil {
		inst.mu.Lock()
		r.Task, r.Actor, r.ActorInstance, r.Test = t.id, t.actor, t.actorInstance, t.test
		r.BudgetLeft = int32(t.callsBudget - t.callsUsed)
		inst.mu.Unlock()
	}
	inst.recMu.Lock()
	defer inst.recMu.Unlock()
	if len(inst.records) < keepRecords {
		inst.records = append(inst.records, r)
		return
	}
	inst.records[inst.recHead] = r
	inst.recHead = (inst.recHead + 1) % keepRecords
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
		for _, e := range t.entries {
			s := strconv.FormatUint(e.instance, 10) + ":" + string(e.app) + ":" + e.mode.String()
			if len(e.ops) > 0 {
				s += ":" + joinComma(e.ops)
			}
			g.Entries = append(g.Entries, s)
		}
		slices.Sort(g.Entries)
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
		StringList("destinations", func(i int) []string { return r(i).Destinations }).
		Int32("calls_used", func(i int) int32 { return r(i).CallsUsed }).
		Int32("calls_budget", func(i int) int32 { return r(i).CallsBudget }).
		Int64("deadline_unix_ms", func(i int) int64 { return r(i).Deadline.UnixMilli() }).
		Uint64("epoch", func(i int) uint64 { return r(i).Epoch }).
		String("revoked", func(i int) string { return r(i).Revoked }).
		Int64("created_unix_ms", func(i int) int64 { return r(i).Created.UnixMilli() }).
		Bool("test", func(i int) bool { return r(i).Test })
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
		// The caller's key: the tool-call id (OpenTelemetry gen_ai.tool.call.id).
		String("tool_call_id", func(i int) string { return r(i).Key }).
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
		Bool("test", func(i int) bool { return r(i).Test })
}
