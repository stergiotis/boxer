package play

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// Play's operations catalog (ADR-0270): what an agent may read and do in a
// play window under a task grant (ADR-0269). Commands run on the render
// goroutine at the top of the window's frame, before play draws, so a buffer
// an agent sets shows in that frame; queries read the snapshot taken right
// after.

const (
	opsResSql     = "sql"
	opsResParams  = "params"
	opsResSignals = "signals"
	opsResResult  = "result"
	opsResPanes   = "panes"

	opGetState       = "get_state"
	opDescribeResult = "describe_result"
	opSampleRows     = "sample_rows"
	opSetSql         = "set_sql"
	opSetSignal      = "set_signal"
	opShowPane       = "show_pane"
)

// Bounds on what sample_rows returns (ADR-0270 §SD1).
const (
	opsSampleMaxRows  = 50
	opsSampleMaxBytes = 8 << 10
)

// ParamState is one parameter slot as get_state reports it.
type ParamState struct {
	Name string `desc:"the parameter name"`
	Type string `desc:"its ClickHouse type"`
	// Tier is pinned (a SET line in the buffer) or live (the signal store).
	Tier  string `desc:"pinned or live"`
	Value string `desc:"its current value, as text"`
}

// SignalState is one signal as get_state reports it.
type SignalState struct {
	Name   string `desc:"the signal name"`
	Value  string `desc:"its value, as text"`
	Writer string `desc:"who wrote it last: a pane, a widget, the person's editor, or a task"`
}

// ResultState is the main result as get_state reports it.
type ResultState struct {
	Id    uint64 `desc:"the result id; 0 before any run"`
	Phase string `desc:"idle, running, rows, empty or failed, with -stale when the buffer or a signal moved since the run"`
	Rows  int64  `desc:"its row count"`
	Error string `desc:"the error of a failed run"`
}

// PlayState is get_state's result.
type PlayState struct {
	Sql        string        `desc:"the SQL buffer"`
	Live       bool          `desc:"whether Live reruns the query when a signal it reads moves"`
	RaisedPane string        `desc:"the pane play last raised"`
	Params     []ParamState  `desc:"the buffer's parameter slots"`
	Signals    []SignalState `desc:"the signals held"`
	Result     ResultState   `desc:"the main result"`
	// Destination is what a grant must list for a run to reach the
	// endpoint (ADR-0270 §SD2).
	Destination string `desc:"the endpoint as a grant names it, clickhouse:<host>; a run that names a table outside keelson() needs it, and under Auto a run naming only keelson tables needs keelson:<table> for each instead"`
}

// Column is one result column.
type Column struct {
	Name string `desc:"the column name"`
	Type string `desc:"its Arrow type"`
}

// ResultDescription is describe_result's result.
type ResultDescription struct {
	Id      uint64   `desc:"the result id; 0 before any run"`
	Columns []Column `desc:"the columns"`
	Rows    int64    `desc:"the row count"`
	Loading bool     `desc:"true while a run is in flight"`
	Error   string   `desc:"the error of a failed run"`
}

// SampleArgs is sample_rows' argument.
type SampleArgs struct {
	Offset uint32   `json:",omitzero" desc:"the first row; 0 when left out"`
	Limit  uint32   `json:",omitzero" desc:"how many rows, at most 50; 50 when left out"`
	Fields []string `json:",omitzero" desc:"the columns to return; every column when left out"`
	// ResultId, when set, must name the result held: a sample never mixes
	// rows of two runs.
	ResultId *uint64 `desc:"the result the sample is of; refused when another is held"`
}

// SampleRows is sample_rows' result.
type SampleRows struct {
	ResultId  uint64     `desc:"the result the rows come from"`
	Columns   []string   `desc:"the columns returned"`
	Rows      [][]string `desc:"the cells as text"`
	Truncated bool       `desc:"true when the row or byte bound cut the sample"`
}

// SetSqlArgs is set_sql's argument.
type SetSqlArgs struct {
	Sql string `desc:"the new SQL buffer"`
}

// SetSignalArgs is set_signal's argument.
type SetSignalArgs struct {
	Name  string `desc:"the signal name"`
	Value string `desc:"its new value, as text"`
}

// ShowPaneArgs is show_pane's argument.
type ShowPaneArgs struct {
	Pane string `desc:"the pane id, as list_panes and get_state name it"`
}

// opsSnap is what queries read: copies taken after the command stage, and
// the graph, whose main snapshot is safe to read from any goroutine.
type opsSnap struct {
	mounted bool
	state   PlayState
	graph   *queryGraph
}

var playOps = func() (s *appops.Set[*PlayLauncher, opsSnap]) {
	s = appops.NewSet(snapshotPlay)
	s.Resource(opsResSql, "the SQL buffer", func(inst *PlayLauncher) any {
		if inst.inner == nil {
			return ""
		}
		return inst.inner.sql
	})
	s.Resource(opsResParams, "the parameter values", func(inst *PlayLauncher) any {
		if inst.inner == nil {
			return ""
		}
		return inst.inner.paramDigest()
	})
	s.Resource(opsResSignals, "the signal store", func(inst *PlayLauncher) any {
		if inst.inner == nil {
			return uint64(0)
		}
		return inst.inner.graph.signals().Revision()
	})
	// The result the frame draws, not the store's: a result lands on the
	// query's goroutine, and its revision must move inside a frame, where
	// the change is the app's (ADR-0269 §SD4).
	s.Resource(opsResResult, "the main result", func(inst *PlayLauncher) any {
		if inst.inner == nil {
			return ResultID(0)
		}
		return inst.inner.frameResult
	})
	s.Resource(opsResPanes, "the raised pane and the pane bindings", func(inst *PlayLauncher) any {
		if inst.inner == nil {
			return ""
		}
		return inst.inner.paneDigest()
	})
	s.Editing(opsResSql, func(inst *PlayLauncher) bool {
		return inst.inner != nil && inst.inner.editor != nil && appops.WidgetEditing(inst.inner.editor.TextHandle())
	})
	s.Confined(func(inst *PlayLauncher) bool { return inst.inner != nil && inst.inner.graph.MainConfined() })

	appops.Query(s, app.OperationSpec{Name: opGetState, Version: 1,
		Summary: "read the buffer, the parameters, the signals, Live and the main result's phase",
		Reads:   []string{opsResSql, opsResParams, opsResSignals, opsResResult, opsResPanes}, Agents: true, Untrusted: true},
		func(sn opsSnap, in appops.None) (PlayState, error) {
			if !sn.mounted {
				return PlayState{}, app.RefuseOperation("the window has not mounted")
			}
			return sn.state, nil
		})
	appops.Query(s, app.OperationSpec{Name: opDescribeResult, Version: 1, Summary: "describe the main result: columns, types, row count",
		Reads: []string{opsResResult}, Agents: true},
		func(sn opsSnap, in appops.None) (out ResultDescription, err error) {
			if !sn.mounted {
				return out, app.RefuseOperation("the window has not mounted")
			}
			rec, schema, numRows, loading, _, _, _, runErr, id := sn.graph.MainSnapshot()
			if rec != nil {
				rec.Release()
			}
			out = ResultDescription{Id: uint64(id), Rows: numRows, Loading: loading}
			if runErr != nil {
				out.Error = runErr.Error()
			}
			if schema != nil {
				for _, f := range schema.Fields() {
					out.Columns = append(out.Columns, Column{Name: f.Name, Type: f.Type.String()})
				}
			}
			return
		})
	appops.Query(s, app.OperationSpec{Name: opSampleRows, Version: 1, Summary: "read up to 50 rows of the main result as cell text",
		Reads: []string{opsResResult}, Agents: true, Untrusted: true},
		func(sn opsSnap, in SampleArgs) (out SampleRows, err error) {
			if !sn.mounted {
				return out, app.RefuseOperation("the window has not mounted")
			}
			return sampleMain(sn.graph, in)
		})
	appops.Command(s, app.OperationSpec{Name: opSetSql, Version: 1, Summary: "replace the SQL buffer",
		Effect: app.OperationEffectDocument, Writes: []string{opsResSql}, Agents: true, Gesture: "typing in the editor",
		Follows: []string{"the parameter slots follow the new buffer after a short debounce"}},
		func(inst *PlayLauncher, call app.OperationCall, in SetSqlArgs) (appops.None, error) {
			if inst.inner == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			inst.inner.setSqlFromAgent(in.Sql)
			inst.inner.markAgent(call.OnBehalfOf)
			return appops.None{}, nil
		})
	appops.Command(s, app.OperationSpec{Name: opSetSignal, Version: 1, Summary: "set one signal, with the task as its writer",
		Effect: app.OperationEffectDocument, Writes: []string{opsResSignals}, Agents: true, Gesture: "the Signals section of the Graph pane",
		Follows: []string{"Live reruns a query that reads the signal"}},
		func(inst *PlayLauncher, call app.OperationCall, in SetSignalArgs) (appops.None, error) {
			if inst.inner == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			if strings.TrimSpace(in.Name) == "" {
				return appops.None{}, app.RefuseOperation("a signal needs a name")
			}
			writer := call.Writer
			if writer == "" {
				writer = signalWriterApp
			}
			inst.inner.graph.setSignalRawFrom(SignalID(in.Name), in.Value, writer)
			inst.inner.markAgent(call.OnBehalfOf)
			return appops.None{}, nil
		})
	appops.Command(s, app.OperationSpec{Name: opShowPane, Version: 1, Summary: "raise a pane",
		Effect: app.OperationEffectView, Writes: []string{opsResPanes}, Agents: true, Gesture: "clicking the pane's tab"},
		func(inst *PlayLauncher, call app.OperationCall, in ShowPaneArgs) (appops.None, error) {
			if inst.inner == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			if err := inst.inner.ActivateTab(in.Pane); err != nil {
				return appops.None{}, app.RefuseOperation("no pane " + in.Pane)
			}
			return appops.None{}, nil
		})
	addRunOps(s)
	return
}()

// Operations serves play's catalog for this window.
func (inst *PlayLauncher) Operations() (h app.OperationsHandlerI) { return playOps.Bind(inst) }

var _ app.OperationsAppI = (*PlayLauncher)(nil)

// snapshotPlay copies what get_state reads, on the render goroutine.
func snapshotPlay(inst *PlayLauncher) (sn opsSnap) {
	p := inst.inner
	if p == nil {
		return
	}
	sn.mounted, sn.graph = true, p.graph
	st := PlayState{Sql: p.sql, Live: p.liveMain}
	if p.client != nil {
		st.Destination = DestinationClickHouse(endpointHost(p.client.URL()))
	}
	if slug, ok := p.tabs.slugForDockID(p.raisedTab); ok {
		st.RaisedPane = slug
	}
	for _, slot := range p.paramSlots {
		ps := ParamState{Name: slot.Name, Type: slot.Type, Tier: "live"}
		if p.paramPinned(slot.Name) {
			ps.Tier = "pinned"
		}
		if d := p.paramDrafts[slot.Name]; d != nil {
			ps.Value = *d
		}
		st.Params = append(st.Params, ps)
	}
	for _, r := range p.graph.signalRows() {
		st.Signals = append(st.Signals, SignalState{Name: r.Name, Value: r.Raw, Writer: r.Writer})
	}
	rec, _, numRows, loading, _, _, executed, runErr, id := p.graph.MainSnapshot()
	if rec != nil {
		rec.Release()
	}
	st.Result = ResultState{Id: uint64(id), Phase: p.observeQueryState(loading, numRows, executed, runErr).String(), Rows: numRows}
	if runErr != nil {
		st.Result.Error = runErr.Error()
	}
	sn.state = st
	return
}

// paramDigest is the parameter drafts by name, compared across the
// write-back so the person's edit of a parameter field moves the revision.
func (inst *PlayApp) paramDigest() (digest string) {
	names := make([]string, 0, len(inst.paramDrafts))
	for n := range inst.paramDrafts {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		b.WriteString(n)
		b.WriteByte('=')
		if d := inst.paramDrafts[n]; d != nil {
			b.WriteString(*d)
		}
		b.WriteByte(0)
	}
	return b.String()
}

// paneDigest is the raised pane and the bindings, compared across frames.
func (inst *PlayApp) paneDigest() (digest string) {
	tabs := make([]string, 0, len(inst.tabBindings))
	for t := range inst.tabBindings {
		tabs = append(tabs, t)
	}
	slices.Sort(tabs)
	var b strings.Builder
	b.WriteString(strconv.FormatUint(inst.raisedTab, 10))
	for _, t := range tabs {
		b.WriteString("|" + t + "=" + string(inst.tabBindings[t]))
	}
	return b.String()
}

// setSqlFromAgent replaces the buffer before play draws, as a picked file
// does (consumePickedSql), so every reader of the frame sees the new text.
func (inst *PlayApp) setSqlFromAgent(sql string) {
	inst.sql = sql
	inst.captureParamDefaults(sql)
}

// sampleMain reads cells of the main result, bounded by rows and bytes.
func sampleMain(g *queryGraph, in SampleArgs) (out SampleRows, err error) {
	rec, schema, numRows, _, _, _, _, _, id := g.MainSnapshot()
	if rec == nil || schema == nil {
		return out, app.RefuseOperation("no result is held")
	}
	defer rec.Release()
	if in.ResultId != nil && *in.ResultId != uint64(id) {
		return out, app.ConflictOperation("another result is held now: " + strconv.FormatUint(uint64(id), 10))
	}
	out.ResultId = uint64(id)
	var cols []int
	for i, f := range schema.Fields() {
		if len(in.Fields) == 0 || slices.Contains(in.Fields, f.Name) {
			cols = append(cols, i)
			out.Columns = append(out.Columns, f.Name)
		}
	}
	limit := int64(in.Limit)
	if limit <= 0 || limit > opsSampleMaxRows {
		limit = opsSampleMaxRows
	}
	bytes := 0
	for row := int64(in.Offset); row < numRows && row < int64(in.Offset)+limit; row++ {
		cells := make([]string, 0, len(cols))
		for _, col := range cols {
			v := strings.Clone(formatCell(rec, col, row))
			bytes += len(v)
			cells = append(cells, v)
		}
		if bytes > opsSampleMaxBytes {
			out.Truncated = true
			break
		}
		out.Rows = append(out.Rows, cells)
	}
	if int64(in.Offset)+int64(len(out.Rows)) < numRows {
		out.Truncated = out.Truncated || int64(len(out.Rows)) == limit
	}
	return
}

// RunArgs is run's argument.
type RunArgs struct {
	// Subquery narrows the run to the innermost query at the caret, as
	// Ctrl+Shift+Enter does.
	Subquery bool `json:",omitzero" desc:"run only the innermost query at the caret"`
}

// SetParamArgs is set_param's argument.
type SetParamArgs struct {
	Name  string `desc:"the parameter name, as get_state lists it"`
	Value string `desc:"its new value, as text"`
}

// markAgent records that the window acts on a task's input (ADR-0270
// §SD3): a Live rerun carries the task's context until the person edits.
func (inst *PlayApp) markAgent(obo *app.OnBehalfOf) {
	if obo == nil {
		return
	}
	inst.setAgentDriven(obo)
	inst.agentFresh = true
}

// setAgentDriven sets the mark and hands it to the window's client at once,
// so a run started later in the same frame — the person's Run, which
// clears it — is judged by the mark that started it.
func (inst *PlayApp) setAgentDriven(obo *app.OnBehalfOf) {
	inst.agentDriven = obo
	if inst.client != nil {
		inst.client.SetAgentMark(obo)
	}
}

// settleAgentMark records, at the end of the frame an agent's command ran
// in, what the window then holds: a later difference is the person's edit.
func (inst *PlayApp) settleAgentMark() {
	if !inst.agentFresh {
		return
	}
	inst.agentFresh = false
	inst.agentSql, inst.agentParams = inst.sql, inst.paramDigest()
}

// checkAgentMark clears the mark once the person edited the buffer or a
// parameter; it runs after the write-back and before play draws.
func (inst *PlayApp) checkAgentMark() {
	if inst.agentDriven == nil || inst.agentFresh {
		return
	}
	if inst.sql != inst.agentSql || inst.paramDigest() != inst.agentParams {
		inst.setAgentDriven(nil)
	}
}

// takeAgentForRun decides whose run this is: the task's when it asked for
// it or when Live reruns after its input; otherwise the person's, which
// makes the window's work the person's again.
func (inst *PlayApp) takeAgentForRun(auto bool) (obo *app.OnBehalfOf) {
	if inst.agentRunRequested {
		inst.agentRunRequested = false
		return inst.agentDriven
	}
	if auto {
		return inst.agentDriven
	}
	inst.setAgentDriven(nil)
	return
}

// addRunOps declares run and set_param; playOps' initializer calls it, so
// they are in the catalog before play registers its manifest.
func addRunOps(playOps *appops.Set[*PlayLauncher, opsSnap]) {
	appops.Command(playOps, app.OperationSpec{Name: opRun, Version: 1, Summary: "run the buffer, under the agent limits",
		Effect: app.OperationEffectRun, Reads: []string{opsResSql, opsResParams, opsResSignals}, Writes: []string{opsResResult},
		Agents: true, Gesture: "the Run button",
		Follows: []string{"the result replaces the main result; describe_result reads it, and fails with an agent limit when the grant does not cover the statement"}},
		func(inst *PlayLauncher, call app.OperationCall, in RunArgs) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			if call.OnBehalfOf == nil {
				return appops.None{}, app.RefuseOperation("a run through the catalog is an agent's, and carries its context")
			}
			if strings.TrimSpace(p.sql) == "" {
				return appops.None{}, app.RefuseOperation("the buffer is empty")
			}
			if names := p.unfilledInputs(); len(names) > 0 {
				return appops.None{}, app.RefuseOperation("parameters need a value first: " + strings.Join(names, ", "))
			}
			p.markAgent(call.OnBehalfOf)
			p.agentRunRequested = true
			p.requestSubquery = in.Subquery
			p.RequestRun()
			return appops.None{}, nil
		})
	playOps.Available(opRun, func(sn opsSnap) (bool, string) {
		if sn.mounted && sn.state.Result.Phase == "running" {
			return false, "a run is in flight"
		}
		return true, ""
	})
	appops.Command(playOps, app.OperationSpec{Name: opSetParam, Version: 1, Summary: "set one parameter, in its tier",
		Effect: app.OperationEffectDocument, Writes: []string{opsResParams}, Agents: true, Gesture: "the parameter's field above the editor",
		Follows: []string{"a pinned parameter rewrites its SET line in the buffer; a live one writes its signal, and Live reruns"}},
		func(inst *PlayLauncher, call app.OperationCall, in SetParamArgs) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			d := p.paramDrafts[in.Name]
			if d == nil {
				return appops.None{}, app.ConflictOperation("no parameter " + in.Name + " in the buffer yet; slots follow a new buffer after a short debounce, read get_state again")
			}
			*d = in.Value
			p.markAgent(call.OnBehalfOf)
			return appops.None{}, nil
		})
}

const (
	opRun      = "run"
	opSetParam = "set_param"
)
