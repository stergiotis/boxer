// effects — the live effect graph of the running app instances (ADR-0188
// §SD4): who is subscribed to what, which client holds which capability,
// which task is running for whom. keelson('apps') answers what this process
// declares and keelson('windows') what it currently shows; neither answers
// what an instance has actually acquired. These three tables do, read from
// the bookkeeping the runtime keeps anyway — the bus router's subscription
// table and live-client registry, and the task supervisor's in-flight map —
// so the graph is a query rather than a parallel data structure.
//
// Live: every snapshot reads the current state. Registered unconditionally
// through RegisterEffects, in the workingsets precedent: a host without a
// bus or without a supervisor answers with empty tables rather than absent
// ones, so the table names do not depend on how the host was wired.

package providers

import (
	"sort"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/task/supervisor"
)

// RegisterEffects registers the subscriptions, client_caps and tasks
// providers into r (ADR-0188 §SD4). bus and tasks may each be nil — the
// tables are then empty, never absent. Beware the typed-nil interface
// trap on the caller's side: hand over a supervisor only when it started.
func RegisterEffects(r *introspect.Registry, bus *inprocbus.Inst, tasks *supervisor.Supervisor) (err error) {
	if err = r.Register(subscriptionsProvider{bus: bus}); err != nil {
		return
	}
	if err = r.Register(clientCapsProvider{bus: bus}); err != nil {
		return
	}
	err = r.Register(tasksProvider{sup: tasks})
	return
}

// subscriptionsProvider exposes the router's subscription table as
// keelson.subscriptions: one row per live subscription, attributed to the
// app id of the client that made it and, where the host stamped one, to
// the window or embed instance (ADR-0188 §SD1). Reply inboxes of in-flight
// Requests are subscriptions like any other and are included, flagged.
type subscriptionsProvider struct{ bus *inprocbus.Inst }

func (subscriptionsProvider) Name() string                         { return "subscriptions" }
func (subscriptionsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (subscriptionsProvider) Schema() *arrow.Schema                { return subscriptionsTable(nil).Schema() }

func (p subscriptionsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []inprocbus.SubscriptionInfo
	if p.bus != nil {
		rows = p.bus.Subscriptions()
		sort.Slice(rows, func(i, j int) bool { return rows[i].Id < rows[j].Id })
	}
	return subscriptionsTable(rows).Build(proj, len(rows)), nil
}

func subscriptionsTable(rows []inprocbus.SubscriptionInfo) *introspect.Table {
	return introspect.NewTable().
		Uint64("subscription_id", func(i int) uint64 { return rows[i].Id }).
		String("app_id", func(i int) string { return string(rows[i].AppId) }).
		// The host-minted window/embed key of the client that subscribed;
		// 0 for clients minted without one (services, CLI, tests). Joins
		// keelson('windows').key.
		Uint64("instance_key", func(i int) uint64 { return rows[i].InstanceKey }).
		String("pattern", func(i int) string { return rows[i].Pattern }).
		// A reply inbox lives for one Request; a row here with is_inbox
		// is a request in flight, not a standing subscription.
		Bool("is_inbox", func(i int) bool { return strings.HasPrefix(rows[i].Pattern, inprocbus.InboxPrefix) })
}

// clientCapsProvider exposes every live bus client's current permission set
// as keelson.client_caps: manifest caps as minted at open, the host-injected
// persist cap, and every runtime grant the broker added since — the whole
// of what the instance may reach right now, which is what dies with the
// client at the closing edge (ADR-0188 §SD1). declared tells a manifest cap
// from the rest by comparing against the app's registration; a cap that is
// not declared was injected by the host or granted at runtime.
type clientCapsProvider struct{ bus *inprocbus.Inst }

func (clientCapsProvider) Name() string                         { return "client_caps" }
func (clientCapsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (clientCapsProvider) Schema() *arrow.Schema                { return clientCapsTable(nil).Schema() }

type clientCapRow struct {
	appId       app.AppIdT
	instanceKey uint64
	filter      app.SubjectFilter
	declared    bool
}

func (p clientCapsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []clientCapRow
	if p.bus != nil {
		clients := p.bus.LiveClients()
		sort.Slice(clients, func(i, j int) bool {
			if clients[i].AppId() != clients[j].AppId() {
				return clients[i].AppId() < clients[j].AppId()
			}
			return clients[i].InstanceKey() < clients[j].InstanceKey()
		})
		for _, c := range clients {
			declared := declaredCaps(c.AppId())
			for _, f := range c.Caps() {
				_, isDeclared := declared[capKey(f)]
				rows = append(rows, clientCapRow{appId: c.AppId(), instanceKey: c.InstanceKey(), filter: f, declared: isDeclared})
			}
		}
	}
	return clientCapsTable(rows).Build(proj, len(rows)), nil
}

func capKey(f app.SubjectFilter) string {
	return f.Pattern + "\x00" + f.Direction.String()
}

// declaredCaps returns the (pattern, direction) set the app's registered
// manifest declares; empty when the id is not registered (a service client
// or a test identity).
func declaredCaps(id app.AppIdT) (set map[string]struct{}) {
	set = make(map[string]struct{})
	m, ok := app.LookupManifest(id)
	if !ok {
		return
	}
	for _, f := range m.Caps {
		set[capKey(f)] = struct{}{}
	}
	return
}

func clientCapsTable(rows []clientCapRow) *introspect.Table {
	return introspect.NewTable().
		String("app_id", func(i int) string { return string(rows[i].appId) }).
		Uint64("instance_key", func(i int) uint64 { return rows[i].instanceKey }).
		String("pattern", func(i int) string { return rows[i].filter.Pattern }).
		String("direction", func(i int) string { return rows[i].filter.Direction.String() }).
		String("reason", func(i int) string { return rows[i].filter.Reason }).
		Bool("sticky", func(i int) bool { return rows[i].filter.Sticky }).
		Bool("declared", func(i int) bool { return rows[i].declared })
}

// tasksProvider exposes the supervisor's in-flight map as keelson.tasks:
// one row per task that has been created and not yet reached a terminal
// verb, attributed to the app instance that spawned it (the OwnerTileKey
// task.ForApp stamps, which is the same key keelson('windows') carries).
type tasksProvider struct{ sup *supervisor.Supervisor }

func (tasksProvider) Name() string                         { return "tasks" }
func (tasksProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (tasksProvider) Schema() *arrow.Schema                { return tasksTable(nil).Schema() }

func (p tasksProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []supervisor.InflightEntry
	if p.sup != nil {
		rows = p.sup.InflightSnapshot()
		sort.Slice(rows, func(i, j int) bool { return rows[i].Created.TaskId < rows[j].Created.TaskId })
	}
	return tasksTable(rows).Build(proj, len(rows)), nil
}

func tasksTable(rows []supervisor.InflightEntry) *introspect.Table {
	return introspect.NewTable().
		String("task_id", func(i int) string { return rows[i].Created.TaskId }).
		String("kind", func(i int) string { return rows[i].Created.Kind }).
		String("title", func(i int) string { return rows[i].Created.Title }).
		String("owner_app_id", func(i int) string { return rows[i].Created.OwnerAppId }).
		// The spawning window's instance key (task.ForApp's OwnerTileKey);
		// joins keelson('windows').key and keelson('subscriptions').instance_key.
		Uint64("owner_instance_key", func(i int) uint64 { return rows[i].Created.OwnerTileKey }).
		String("owner_run_id", func(i int) string { return rows[i].Created.OwnerRunId }).
		String("state", func(i int) string { return rows[i].State.String() }).
		String("created_at", func(i int) string { return rows[i].Created.At.UTC().Format(time.RFC3339) }).
		Int64("last_emit_ms", func(i int) int64 { return rows[i].LastEmitMs }).
		Bool("cancellable", func(i int) bool { return rows[i].Created.CancellableB })
}
