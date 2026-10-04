package chat

// The agent surface of a conversation (ADR-0283): every window operation,
// launchable app and desktop verb the model could reach, each with the
// status the ceiling and the live grant give it and the calls the
// conversation made on it. A snapshot is fetched off the render thread from
// the host's catalog and keelson tables, then built by buildSurface, which
// reads nothing else.

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonquery"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providersgui"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// aliasSurface is the dataset Open in play publishes the surface under.
const aliasSurface = "chat_surface"

// surfaceGroupE is the part of the surface a cell belongs to.
type surfaceGroupE uint8

const (
	groupWindow surfaceGroupE = iota
	groupLaunch
	groupDesktop
)

func (inst surfaceGroupE) String() (s string) {
	switch inst {
	case groupWindow:
		s = "window"
	case groupLaunch:
		s = "launch"
	default:
		s = "desktop"
	}
	return
}

// useE buckets a call's last phase: what became of it.
type useE uint8

const (
	// useDone got through the gate: queued, applied, completed.
	useDone useE = iota
	// useProposed waits on, or went to, the person as a proposal.
	useProposed
	// useAsked was outside the grant: the person was asked to widen it.
	useAsked
	// useRefused was refused, denied, rejected or lost a conflict.
	useRefused
	// useFailed failed, expired or was cancelled.
	useFailed
	useCount
)

var useNames = [useCount]string{"done", "proposed", "asked", "refused", "failed"}

func useOf(phase string) (u useE) {
	switch phase {
	case "completed", "applied", "rendered", "accepted", "running":
		return useDone
	case "proposed":
		return useProposed
	case "input_required":
		return useAsked
	case "refused", "denied", "rejected", "conflict", "stale":
		return useRefused
	}
	return useFailed
}

// surfaceCell is one thing the model could do.
type surfaceCell struct {
	group surfaceGroupE
	// instance is the window's key, zero outside groupWindow; window its
	// name for the heading, and open whether it still is.
	instance uint64
	window   string
	open     bool
	app      string
	op       string
	effect   string
	status   agent.CellStatusE
	uses     [useCount]int
}

func (inst *surfaceCell) calls() (n int) {
	for _, u := range inst.uses {
		n += u
	}
	return
}

// surfaceModel is one snapshot of the surface.
type surfaceModel struct {
	cells []surfaceCell
	// task is the live task, ended how many tasks of the conversation ended.
	task  string
	ended int
	// conv is the conversation it describes, at when it was read.
	conv string
	at   time.Time
}

// statusCounts are the cells per status, and of the granted ones how many
// the conversation used.
func (inst *surfaceModel) statusCounts() (n map[agent.CellStatusE]int, used int, calls int) {
	n = make(map[agent.CellStatusE]int, len(agent.AllCellStatuses))
	for i := range inst.cells {
		cell := &inst.cells[i]
		n[cell.status]++
		k := cell.calls()
		calls += k
		if cell.status == agent.CellStatusGranted && k > 0 {
			used++
		}
	}
	return
}

// The rows the snapshot reads, as the queries below select them. Integers
// travel as strings: JSONEachRow quotes 64-bit integers.
type windowRow struct {
	Key     string `json:"key"`
	App     string `json:"app_id"`
	Display string `json:"display"`
}

type grantRow struct {
	Task     string   `json:"task"`
	Entries  []string `json:"entries"`
	Launches []string `json:"launches"`
	Desktop  string   `json:"desktop"`
	Revoked  string   `json:"revoked"`
	Created  string   `json:"created"`
}

type actionRow struct {
	At        string `json:"at"`
	Task      string `json:"task"`
	Key       string `json:"key"`
	Instance  string `json:"instance"`
	App       string `json:"app"`
	Operation string `json:"operation"`
	Effect    string `json:"effect"`
	Decision  string `json:"decision"`
	Phase     string `json:"phase"`
}

// surfaceInput is what buildSurface reads.
type surfaceInput struct {
	ceiling *agent.Ceiling
	apps    []agent.AppOperations
	windows []windowRow
	grants  []grantRow
	actions []actionRow
}

// windowVerbs are the desktop verbs that act on one window; they need the
// window in act mode.
var windowVerbs = []string{agent.ActionRaise, agent.ActionPlace}

// buildSurface lays the surface out: the windows that are open, granted or
// used, each with its app's operations and the window verbs; every app the
// catalog lists, as a launch; arranging the desktop. Statuses come from the
// live grant — the task not revoked — under the ceiling; uses from every
// call of the conversation, under whichever task made it.
func buildSurface(in surfaceInput) (m surfaceModel) {
	ops := make(map[string][]agent.Operation, len(in.apps))
	for _, a := range in.apps {
		ops[a.App] = a.Operations
	}
	var live *grantRow
	for i := range in.grants {
		g := &in.grants[i]
		if g.Revoked != "" {
			m.ended++
			continue
		}
		if live == nil || g.Created > live.Created {
			live = g
		}
	}
	entries := map[uint64]*agent.GrantedEntry{}
	launches := map[string]*agent.GrantLaunch{}
	desktop := agent.ModeUnspecified
	if live != nil {
		m.task = live.Task
		for _, s := range live.Entries {
			if e, err := agent.ParseGrantedEntry(s); err == nil {
				entries[e.Instance] = &e
			}
		}
		for _, s := range live.Launches {
			if l, err := agent.ParseGrantedLaunch(s); err == nil {
				launches[l.App] = &l
			}
		}
		desktop = agent.ParseMode(live.Desktop)
	}
	type winInfo struct {
		app, name string
		open      bool
	}
	wins := map[uint64]*winInfo{}
	for _, w := range in.windows {
		k, err := strconv.ParseUint(w.Key, 10, 64)
		if err != nil {
			continue
		}
		name := w.Display
		if name == "" {
			name = w.App
		}
		wins[k] = &winInfo{app: w.App, name: name, open: true}
	}
	calls := lastPhases(in.actions)
	used := map[uint64]bool{}
	for _, a := range calls {
		if k, _ := strconv.ParseUint(a.Instance, 10, 64); k != 0 && a.Operation != agent.ActionOpenWindow {
			used[k] = true
			if wins[k] == nil {
				wins[k] = &winInfo{app: a.App, name: a.App}
			}
		}
	}
	for k, e := range entries {
		if wins[k] == nil {
			wins[k] = &winInfo{app: e.App, name: e.App}
		}
	}

	// The cells, keyed so the calls find theirs.
	index := map[string]int{}
	add := func(cell surfaceCell) {
		index[cellKey(cell.group, cell.instance, cell.app, cell.op)] = len(m.cells)
		m.cells = append(m.cells, cell)
	}
	keys := make([]uint64, 0, len(wins))
	for k, w := range wins {
		// A window whose app offers no operation is the model's only to raise
		// or place, and only once granted: it is drawn when a grant or a call
		// names it.
		if len(ops[w.app]) > 0 || entries[k] != nil || used[k] {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		w, e := wins[k], entries[k]
		cell := surfaceCell{group: groupWindow, instance: k, window: w.name, open: w.open, app: w.app}
		for _, o := range ops[w.app] {
			cell.op, cell.effect = o.Name, o.Effect
			cell.status = agent.ClassifyOperation(in.ceiling, e, o.Name, effectOf(o.Effect))
			add(cell)
		}
		for _, v := range windowVerbs {
			cell.op, cell.effect = v, app.OperationEffectView.String()
			cell.status = agent.ClassifyWindowVerb(in.ceiling, e)
			add(cell)
		}
	}
	apps := slices.SortedFunc(slices.Values(in.apps), func(a, b agent.AppOperations) int { return cmp.Compare(a.App, b.App) })
	for _, a := range apps {
		add(surfaceCell{group: groupLaunch, app: a.App, window: a.Display, op: agent.ActionOpenWindow,
			status: agent.ClassifyLaunch(in.ceiling, launches[a.App])})
	}
	add(surfaceCell{group: groupDesktop, op: agent.ActionArrange, effect: app.OperationEffectView.String(),
		status: agent.ClassifyArrange(in.ceiling, desktop)})

	for _, a := range calls {
		k, _ := strconv.ParseUint(a.Instance, 10, 64)
		key := cellKey(groupWindow, k, "", a.Operation)
		switch a.Operation {
		case agent.ActionOpenWindow:
			key = cellKey(groupLaunch, 0, a.App, a.Operation)
		case agent.ActionArrange:
			key = cellKey(groupDesktop, 0, "", a.Operation)
		}
		i, ok := index[key]
		if !ok {
			// An operation the catalog no longer lists, or an app no longer
			// launchable: the call still counts, as a cell of its own.
			w := wins[k]
			cell := surfaceCell{group: groupWindow, instance: k, app: a.App, window: a.App, op: a.Operation, effect: a.Effect,
				status: agent.ClassifyOperation(in.ceiling, entries[k], a.Operation, effectOf(a.Effect))}
			if w != nil {
				cell.window, cell.open = w.name, w.open
			}
			if a.Operation == agent.ActionOpenWindow {
				cell = surfaceCell{group: groupLaunch, app: a.App, window: a.App, op: a.Operation,
					status: agent.ClassifyLaunch(in.ceiling, launches[a.App])}
			}
			add(cell)
			i = len(m.cells) - 1
		}
		m.cells[i].uses[useOf(a.Phase)]++
	}
	slices.SortStableFunc(m.cells, func(a, b surfaceCell) int {
		return cmp.Or(cmp.Compare(a.group, b.group), cmp.Compare(a.instance, b.instance), cmp.Compare(a.app, b.app))
	})
	return
}

// cellKey names a cell: a window's by key and operation, a launch by app.
func cellKey(g surfaceGroupE, instance uint64, appId string, op string) (k string) {
	switch g {
	case groupWindow:
		return "w:" + strconv.FormatUint(instance, 10) + ":" + op
	case groupLaunch:
		return "l:" + appId
	}
	return "d:" + op
}

// lastPhases is one row per call: the final row where there is one, else
// the latest. A call is its task's key.
func lastPhases(rows []actionRow) (out []actionRow) {
	last := map[string]int{}
	for _, r := range rows {
		id := r.Task + "\x00" + r.Key
		i, seen := last[id]
		switch {
		case !seen:
			last[id] = len(out)
			out = append(out, r)
		case out[i].Decision == "final" && r.Decision != "final":
		case r.Decision == "final" || r.At >= out[i].At:
			out[i] = r
		}
	}
	return
}

func effectOf(s string) (e app.OperationEffectE) {
	for _, x := range app.AllOperationEffects {
		if x.String() == s {
			return x
		}
	}
	return app.OperationEffectUnspecified
}

// sqlString quotes s as a ClickHouse string literal.
func sqlString(s string) (lit string) {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

// fetchSurface reads the snapshot's sources: the catalog, the open windows,
// the conversation's tasks and its calls. It makes bus round trips, so it
// runs off the render thread.
func fetchSurface(ctx context.Context, cli *agent.Client, kq *keelsonquery.Client, ceiling agent.Ceiling, conversation string, tasks []string) (m *surfaceModel, err error) {
	if cli == nil || kq == nil {
		err = eh.Errorf("the agent surface needs the app runtime")
		return
	}
	in := surfaceInput{ceiling: &ceiling}
	in.apps, err = cli.Describe(ctx, agent.DescribeRequest{})
	if err != nil {
		err = eh.Errorf("describe the apps: %w", err)
		return
	}
	in.windows, err = queryRows[windowRow](ctx, kq, providersgui.TableWindows,
		"SELECT toString(key) AS key, app_id, display FROM keelson('windows')")
	if err != nil {
		return
	}
	if len(tasks) > 0 {
		quoted := make([]string, 0, len(tasks))
		for _, t := range tasks {
			quoted = append(quoted, sqlString(t))
		}
		in.grants, err = queryRows[grantRow](ctx, kq, agent.TableGrants,
			"SELECT task, entries, launches, desktop, revoked, toString(created_unix_ms) AS created FROM keelson('agent_grants') WHERE task IN ("+
				strings.Join(quoted, ", ")+")")
		if err != nil {
			return
		}
	}
	in.actions, err = queryRows[actionRow](ctx, kq, agent.TableActions,
		"SELECT toString(at_unix_ms) AS at, task, key, toString(instance) AS instance, app, operation, effect, decision, phase "+
			"FROM keelson('agent_actions') WHERE conversation = "+sqlString(conversation)+" ORDER BY at_unix_ms")
	if err != nil {
		return
	}
	got := buildSurface(in)
	got.conv, got.at = conversation, time.Now()
	m = &got
	return
}

// queryRows runs sql over one keelson table and decodes its JSONEachRow.
func queryRows[T any](ctx context.Context, kq *keelsonquery.Client, table string, sql string) (rows []T, err error) {
	res, err := kq.Query(ctx, table, sql, keelsonquery.FormatJSONEachRow)
	if err != nil {
		err = eb.Build().Str("table", table).Errorf("read the keelson table: %w", err)
		return
	}
	for line := range bytes.Lines(res.Body) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var r T
		if err = json.Unmarshal(line, &r); err != nil {
			err = eb.Build().Str("table", table).Errorf("decode a keelson row: %w", err)
			return
		}
		rows = append(rows, r)
	}
	return
}

var surfaceSchema = arrow.NewSchema([]arrow.Field{
	{Name: "group", Type: arrow.BinaryTypes.String},
	{Name: "instance", Type: arrow.PrimitiveTypes.Uint64},
	{Name: "window", Type: arrow.BinaryTypes.String},
	{Name: "open", Type: arrow.FixedWidthTypes.Boolean},
	{Name: "app", Type: arrow.BinaryTypes.String},
	{Name: "operation", Type: arrow.BinaryTypes.String},
	{Name: "effect", Type: arrow.BinaryTypes.String},
	{Name: "status", Type: arrow.BinaryTypes.String},
	{Name: "calls", Type: arrow.PrimitiveTypes.Uint32},
	{Name: "done", Type: arrow.PrimitiveTypes.Uint32},
	{Name: "proposed", Type: arrow.PrimitiveTypes.Uint32},
	{Name: "asked", Type: arrow.PrimitiveTypes.Uint32},
	{Name: "refused", Type: arrow.PrimitiveTypes.Uint32},
	{Name: "failed", Type: arrow.PrimitiveTypes.Uint32},
}, nil)

// surfaceArrow is the cells as an Arrow IPC stream.
func surfaceArrow(cells []surfaceCell) (stream []byte, err error) {
	rb := array.NewRecordBuilder(memory.DefaultAllocator, surfaceSchema)
	defer rb.Release()
	for i := range cells {
		cell := &cells[i]
		rb.Field(0).(*array.StringBuilder).Append(cell.group.String())
		rb.Field(1).(*array.Uint64Builder).Append(cell.instance)
		rb.Field(2).(*array.StringBuilder).Append(cell.window)
		rb.Field(3).(*array.BooleanBuilder).Append(cell.open)
		rb.Field(4).(*array.StringBuilder).Append(cell.app)
		rb.Field(5).(*array.StringBuilder).Append(cell.op)
		rb.Field(6).(*array.StringBuilder).Append(cell.effect)
		rb.Field(7).(*array.StringBuilder).Append(cell.status.String())
		rb.Field(8).(*array.Uint32Builder).Append(uint32(cell.calls()))
		for u := range useCount {
			rb.Field(9 + int(u)).(*array.Uint32Builder).Append(uint32(cell.uses[u]))
		}
	}
	rec := rb.NewRecordBatch()
	defer rec.Release()
	return adhocdata.EncodeRecord(rec)
}
