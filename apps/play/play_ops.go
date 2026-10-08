package play

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/fsmops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
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
	opListPanes      = "list_panes"
	opBindPane       = "bind_pane"
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
	// Widget and what it offers are ADR-0274 §SD3's: the control the
	// parameter block draws for the slot.
	Widget  string        `desc:"the control that edits it: text, enum, range (a from/to pair with relative expressions), datetime pair, or expr (a SQL expression)"`
	With    string        `json:",omitzero" desc:"for a range or a datetime pair, the other parameter folded into the same control"`
	Options []ParamOption `json:",omitzero" desc:"for an enum, the values it offers, from its -- play: enum line; set_param refuses any other"`
	// Unfilled is what run refuses on.
	Unfilled bool `json:",omitzero" desc:"true when a run still needs a value for it"`
	// Default is the prelude value Reset restores; only pinned values have one.
	Default *string `json:",omitzero" desc:"the value the buffer's prelude gave it when the buffer was loaded, which the person's Reset restores; set_param it back and run to do the same"`
	Moved   bool    `json:",omitzero" desc:"true when its value is not its default"`
}

// ParamOption is one value an enumerated parameter offers.
type ParamOption struct {
	Value string `desc:"the value set_param takes"`
	Label string `json:",omitzero" desc:"what the dropdown shows for it, when that is not the value"`
}

// SignalState is one signal as get_state reports it: held in the store,
// read by the buffer, or both.
type SignalState struct {
	Name   string `desc:"the signal name"`
	Value  string `desc:"its value, as text"`
	Writer string `desc:"who wrote it last: a pane, a widget, the person's editor, or a task"`
	// The Signals section's facts (ADR-0274 §SD3's signal half).
	Held     bool     `desc:"true when the store holds a value; false for a name the buffer reads and nothing has written"`
	Read     bool     `json:",omitzero" desc:"true when the buffer reads it"`
	Types    []string `json:",omitzero" desc:"the types the buffer and the panes read it as"`
	Conflict bool     `json:",omitzero" desc:"true when it is read as more than one type: one value, divergent casts"`
	Pinned   bool     `json:",omitzero" desc:"true when a SET line in the buffer binds the name, so the constant shadows the store at run time and set_signal changes nothing a run sees"`
	Unfilled bool     `json:",omitzero" desc:"true when the buffer reads it and nothing fills it; a run needs it"`
	Lags     bool     `json:",omitzero" desc:"for selection_id, true when it was written before the selection it accompanies, so it names the previous leeway row"`
	Revision uint64   `json:",omitzero" desc:"the store revision of its last write"`
	Pane     string   `json:",omitzero" desc:"the pane that publishes it, when one does"`
	Refused  string   `json:",omitzero" desc:"why set_signal refuses it, and what to call instead; empty when it may be set"`
}

// ResultState is the main result as get_state reports it.
type ResultState struct {
	Id    uint64 `desc:"the result id; 0 before any run"`
	Phase string `desc:"idle, running, rows, empty or failed, or rows (stale), empty (stale) or failed (stale) when the buffer or a signal moved since the run; query_state has the transitions"`
	Rows  int64  `desc:"its row count"`
	Error string `desc:"the error of a failed run"`
	// Notice is the status line's override of the summary.
	Notice    string `json:",omitzero" desc:"what the status line says in place of the result's summary: a refused run and why, the write gate, a write's outcome, or Live switched off by its breaker"`
	Progress  string `json:",omitzero" desc:"while running, the server's progress: rows and bytes read, rate, estimate"`
	Truncated string `json:",omitzero" desc:"why the main result is a prefix of what the statement returns; empty when it is whole"`
	// RunBy says whose run is in flight, which cancel_run checks.
	RunBy string `json:",omitzero" desc:"while running, whose run it is: person, or task:<id>; cancel_run stops only the calling task's own"`
}

// PlayState is get_state's result.
type PlayState struct {
	Sql        string        `desc:"the SQL buffer"`
	Live       bool          `desc:"whether Live reruns the query when a signal it reads moves"`
	RaisedPane string        `desc:"the pane play last raised"`
	Params     []ParamState  `desc:"the buffer's parameter slots"`
	ParamNote  string        `json:",omitzero" desc:"the parameter block's note on a slot that did not get the control its name suggests: a half-pinned range, an enum or expr line naming no slot, a type mismatch"`
	Signals    []SignalState `desc:"the signals held, and those the buffer reads that nothing holds"`
	Result     ResultState   `desc:"the main result"`
	// Statements says which statement of a buffer of several run ships.
	Statements   int32 `json:",omitzero" desc:"how many statements the buffer holds besides its SET prelude, when more than one"`
	RunStatement int32 `json:",omitzero" desc:"for a buffer of several, which one run ships (1 for the first): the one at the person's caret; run's statement picks another"`
	// Conditions is the run option set_run_options sets.
	Conditions string `json:",omitzero" desc:"the conditions rewrite, on or off; empty when the endpoint does not offer it; set_run_options sets it"`
	// Destination is what a grant must list for a run to reach the
	// endpoint (ADR-0270 §SD2).
	Destination string `desc:"the endpoint as a grant names it, clickhouse:<host>; a run that names a table outside keelson() needs it, and under Auto a run naming only keelson tables needs keelson:<table> for each instead"`
	// Bundle and Followed are what open_bundle and bind_dataset change, so
	// a task reads them here before it calls either (ADR-0269 §SD1).
	Bundle   string   `json:",omitzero" desc:"the ad-hoc bundle the window follows, alias@revision; open_bundle changes it"`
	Followed []string `json:",omitzero" desc:"the dataset names the window binds or waits for; bind_dataset adds to them"`
}

// Column is one result column.
type Column struct {
	Name string `desc:"the column name"`
	Type string `desc:"its Arrow type"`
	// Handle is what the Table's header and describe_table call a leeway
	// column; Label is the name a gloss directive's column goes by.
	Handle string `json:",omitzero" desc:"for a leeway column, its handle (section:column), the name to write"`
	Label  string `json:",omitzero" desc:"for a column whose name carries a gloss directive (mass@gloss/kg), the name it goes by (mass)"`
	// Gloss is the column's gloss as the window resolved it: list_glosses
	// names the glosses, and this says whether a declaration took.
	Gloss *ColumnGloss `json:",omitzero" desc:"the gloss its cells render through, what bound it, and whether it is applied; left out for a plain column"`
}

// ResultDescription is describe_result's result.
type ResultDescription struct {
	Id      uint64   `desc:"the result id; 0 before any run"`
	Node    string   `json:",omitzero" desc:"the split node the result is of"`
	Columns []Column `desc:"the columns"`
	Rows    int64    `desc:"the row count"`
	Loading bool     `desc:"true while a run is in flight"`
	Error   string   `desc:"the error of a failed run"`
	// Truncated is the result's own bound, not a read's: the run kept a
	// prefix of what the statement returns.
	Truncated        bool   `json:",omitzero" desc:"true when the result is a prefix of what the statement returns"`
	TruncationReason string `json:",omitzero" desc:"why the result is a prefix"`
	// Leeway and Reading are the result's leeway reading: its columns
	// carry leeway's encoding, and how to write against them.
	Leeway  bool   `json:",omitzero" desc:"true when the result's columns carry leeway's encoding; write their handles"`
	Reading string `json:",omitzero" desc:"for a leeway result, how to read leeway columns in play"`
	// GlossNote says when the columns' glosses could not be reported.
	GlossNote string `json:",omitzero" desc:"why the columns carry no gloss state"`
}

// SampleArgs is sample_rows' argument.
type SampleArgs struct {
	Pane   string   `json:",omitzero" desc:"the pane whose fed result to sample, as list_panes names it; left out, the result the unbound panels draw"`
	Node   string   `json:",omitzero" desc:"the split node whose result to sample, as list_panes names it; it must be drawn by a pane or be the main result"`
	Offset uint32   `json:",omitzero" desc:"the first row; 0 when left out"`
	Limit  uint32   `json:",omitzero" desc:"how many rows, at most 50; 50 when left out"`
	Rows   []int64  `json:",omitzero" desc:"the rows to read, 0 for the first, at most 50; in place of offset and limit"`
	Fields []string `json:",omitzero" desc:"the columns to return, by name, by gloss label or, for a leeway result, by handle (section:column); every column when left out"`
	// ResultId, when set, must name the result held: a sample never mixes
	// rows of two runs.
	ResultId *uint64 `desc:"the result the sample is of; refused when another is held"`
}

// SampleRows is sample_rows' result.
type SampleRows struct {
	ResultId uint64   `desc:"the result the rows come from"`
	Node     string   `json:",omitzero" desc:"the split node the result is of"`
	Columns  []string `desc:"the columns returned, by their names in the result"`
	// Handles is parallel to Columns: what the Table's header and
	// describe_table call a leeway column, "" where that is its name.
	Handles []string `json:",omitzero" desc:"for a leeway result, each returned column's handle (section:column), parallel to Columns; empty where the column's name is what to write"`
	// Labels is parallel to Columns: the name a gloss directive's column
	// goes by.
	Labels []string   `json:",omitzero" desc:"each returned column's gloss label, parallel to Columns; empty where the column's name carries no gloss directive"`
	Rows   [][]string `desc:"the cells as text; a NULL cell is empty text and listed in Nulls"`
	// RowNumbers is parallel to Rows.
	RowNumbers []int64 `desc:"the result row each row of Rows is, 0 for the first"`
	// Nulls keeps NULL apart from empty text (ADR-0269 §SD10).
	Nulls            [][]int32 `json:",omitzero" desc:"per row of Rows, the indexes into Columns of the cells that are NULL; an empty cell not listed is empty text"`
	Truncated        bool      `desc:"true when the row or byte bound cut the sample"`
	TruncationReason string    `json:",omitzero" desc:"which bound cut the sample, and where to read on"`
	ResultPrefix     string    `json:",omitzero" desc:"why the result itself is a prefix of what the statement returns; empty when it is whole"`
}

// SetSqlArgs is set_sql's argument.
type SetSqlArgs struct {
	Sql string `desc:"the new SQL buffer"`
}

// SetSignalArgs is set_signal's argument.
type SetSignalArgs struct {
	Name  string `desc:"the signal name"`
	Value string `desc:"its new value, as text"`
	// Node is the node a selection's row indexes; selection_node,
	// selection_id and selection_key are written from that node's row.
	Node string `json:",omitzero" desc:"for selection only: the node whose result the row indexes, as list_panes names it; left out, the result the panels draw"`
}

// ShowPaneArgs is show_pane's argument.
type ShowPaneArgs struct {
	Pane string `desc:"the pane id, as list_panes and get_state name it"`
}

// PaneState is one pane as list_panes reports it.
type PaneState struct {
	Pane  string `desc:"the pane id, as show_pane and bind_pane take it"`
	Title string `desc:"its title on the dock strip"`
	// Panel says the pane draws a result; the others are tools.
	Panel bool `desc:"true when the pane draws a result and can be bound to a node"`
	// Node is the split node feeding the pane when it is not the active
	// one: a binding, or the Detail pane following the selection.
	Node string `desc:"the node feeding the pane when it is not the one the main result comes from"`
	// Reason is the pane's own reason it cannot draw what it is fed.
	Reason   string   `desc:"why the pane cannot draw its result; empty when it can, or when it is not a panel"`
	Writes   []string `desc:"the signals the pane writes that the buffer reads"`
	Unfilled []string `desc:"those of them nothing has filled yet; a run needs them"`
	// Draws separates "can draw" from "nothing to judge yet", which an
	// empty Reason leaves open.
	Draws PaneDrawE `desc:"whether the pane can draw what it is fed: unknown before anything has landed, yes, or no (Reason says why)"`
	// Raised is the pane play last brought to the front.
	Raised bool `desc:"whether the pane is the one play last raised"`
	// Publishes is every signal the pane writes when the person uses it,
	// whether or not the buffer reads it yet.
	Publishes []string `desc:"every signal the pane publishes when used; Writes is the part the buffer reads"`
	// Status is the pane's own line under what it drew, quoted as drawn:
	// it can carry the data's values and a failed lane's error.
	Status string `json:",omitzero" desc:"the pane's status line as its last draw showed it; empty when it has none, has not drawn, or cannot draw what it is fed"`
	// StatusOf ties Status to a result: compare it with get_state's result
	// id, since a hidden pane keeps the status of its last draw.
	StatusOf uint64 `json:",omitzero" desc:"the result id Status describes; 0 for a pane that draws its own CTEs"`
	Visible  bool   `desc:"whether the pane's body drew in the last frame: a lazy pane draws only while its tab is in front, the others whenever the dock draws them"`
	Zone     string `desc:"the dock leaf the pane starts in: body, editor, tools, side or bottom"`
	Lazy     bool   `desc:"whether the pane draws only while in front; a hidden lazy pane's status is of its last draw, show_pane raises it"`
	// Ops points from the pane to its own operations.
	Ops []string `json:",omitzero" desc:"the operations that read this pane (get_<pane> first, which says what the pane last drew) and drive it; empty for a pane read through describe_result and sample_rows alone"`
}

// PanesState is list_panes' result.
type PanesState struct {
	Panes []PaneState `desc:"the panes in dock-strip order"`
	// Nodes are what bind_pane accepts.
	Nodes []string `desc:"the split nodes of the buffer: its top-level CTEs and the statement"`
}

// BindPaneArgs is bind_pane's argument.
type BindPaneArgs struct {
	Pane string `desc:"the pane id, as list_panes names it; it must be a panel"`
	Node string `json:",omitzero" desc:"the split node to feed the pane from, as list_panes names it; left out, the pane follows the main result again"`
}

// opsSnap is what queries read: copies taken after the command stage, and
// the graph, whose main snapshot is safe to read from any goroutine.
type opsSnap struct {
	mounted bool
	state   PlayState
	panes   PanesState
	graph   *queryGraph
	// installed is the endpoint's user-defined functions, when the
	// Vocabulary pane's probe has landed them; probed says it has.
	installed map[string]string
	probed    bool
	// client is the window's endpoint client, which the schema reads probe
	// off the render goroutine.
	client *Client
	// projection is the Projection pane's run (play_ops_projection.go).
	projection projectionOpsSnap
	// diagnostics is the Diagnostics pane's sections (play_ops_diagnostics.go).
	diagnostics DiagnosticsState
	// waiting is the aliases bind_dataset or the launch config follows that
	// are not bound yet, with why (play_ops_datasets.go).
	waiting map[string]string
	// results is what describe_result and sample_rows resolve a pane or a
	// node against (play_ops_result.go).
	results opsResults
	// runSql is the text a run would ship (runBuffer): the buffer, or the
	// caret's statement of a multi-statement buffer with the prelude. The
	// statement reads that take an optional sql default to it.
	runSql string
	// paneViews is what the get_<pane> reads see (play_ops_pane_views.go).
	paneViews paneViewsSnap
	// glossCatalog and glossColumns are the gloss catalog and the window's
	// last column resolution (play_ops_glosses.go); docs is the Docs pane's
	// source and what it shows (play_ops_docs.go); detail is what get_detail
	// reads besides the result (play_ops_detail.go).
	glossCatalog *GlossCatalog
	glossColumns *glossColumnsOps
	docs         docsOpsView
	detail       detailOpsView
	// completion is what complete_sql and validate_sql's literal check read
	// (play_ops_completion.go).
	completion completionOpsView
	// queryGraph is get_query_graph's reading (play_ops_query_graph.go).
	queryGraph *QueryGraph
	// bus is the window's bus, which list_bundles reads the catalog over;
	// bundle is the bundle the window follows (play_bundle.go).
	bus    app.BusI
	bundle string
	// lastPublish is the window's last publish_result.
	lastPublish LastPublish
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
	s.Resource(opsResPanes, "the raised pane, the observed node and the pane bindings", func(inst *PlayLauncher) any {
		if inst.inner == nil {
			return ""
		}
		return inst.inner.paneDigest()
	})
	s.Editing(opsResSql, func(inst *PlayLauncher) bool {
		return inst.inner != nil && inst.inner.editor != nil && appops.WidgetEditing(inst.inner.editor.TextHandle())
	})
	s.Confined(func(inst *PlayLauncher) bool { return inst.inner != nil && inst.inner.windowConfined() })
	// The result's lifecycle, as the state chip draws it: query_state and
	// query_machine, the operations every app's mounted machine offers.
	fsmops.Mount(s, "query", "the main result's lifecycle", func(inst *PlayLauncher) opfsm.SourceI {
		if inst.inner == nil || inst.inner.queryFSM == nil {
			return nil
		}
		return inst.inner.queryFSM
	}, fsmops.Options{History: 16})

	appops.Query(s, app.OperationSpec{Name: opGetState, Version: 2,
		Summary: "read the buffer, the parameters, the signals, Live and the main result's phase",
		Reads:   []string{opsResSql, opsResParams, opsResSignals, opsResResult, opsResPanes, opsResBundle, opsResFollowed}, Agents: true, Untrusted: true,
		Follows: []string{"each parameter names its control, an enum's options and the default Reset restores; set_param refuses a value an enum does not offer",
			"result.notice says why a run did not happen or Live switched off"}},
		func(sn opsSnap, in appops.None) (PlayState, error) {
			if !sn.mounted {
				return PlayState{}, app.RefuseOperation("the window has not mounted")
			}
			return sn.state, nil
		})
	// Untrusted: the column names are the statement's and the data's.
	appops.Query(s, app.OperationSpec{Name: opDescribeResult, Version: 2,
		Summary: "describe a result — the one the panels draw, a pane's, or a drawn node's: columns, handles, types, row count, whether it is a prefix",
		Reads:   []string{opsResResult}, Agents: true, Untrusted: true},
		func(sn opsSnap, in ResultArgs) (out ResultDescription, err error) {
			if !sn.mounted {
				return out, app.RefuseOperation("the window has not mounted")
			}
			return describeResult(&sn.results, sn.glossColumns, in)
		})
	appops.Query(s, app.OperationSpec{Name: opSampleRows, Version: 2,
		Summary: "read up to 50 rows of a result — the one the panels draw, a pane's, or a drawn node's — as cell text, NULL kept apart from empty",
		Reads:   []string{opsResResult}, Agents: true, Untrusted: true},
		func(sn opsSnap, in SampleArgs) (out SampleRows, err error) {
			if !sn.mounted {
				return out, app.RefuseOperation("the window has not mounted")
			}
			return sampleResult(&sn.results, in)
		})
	appops.Command(s, app.OperationSpec{Name: opSetSql, Version: 1, Summary: "replace the SQL buffer",
		Effect: app.OperationEffectDocument, Writes: []string{opsResSql}, Agents: true, Gesture: "typing in the editor",
		Follows: []string{"the parameter slots follow the new buffer after a short debounce"}},
		func(inst *PlayLauncher, call app.OperationCall, in SetSqlArgs) (appops.None, error) {
			if inst.inner == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			inst.inner.swapSql(in.Sql)
			inst.inner.markAgent(call.OnBehalfOf)
			return appops.None{}, nil
		})
	appops.Command(s, app.OperationSpec{Name: opSetSignal, Version: 2, Summary: "set one signal, with the task as its writer",
		Effect: app.OperationEffectDocument, Writes: []string{opsResSignals}, Agents: true, Gesture: "the Signals section of the Graph pane",
		Follows: []string{"Live reruns a query that reads the signal",
			"selection is a row of a node's result; selection_node, selection_id and selection_key follow it from that row",
			"a signal a pane publishes and does not take back (tl_from, vp_min_x, gv_selection, …) is refused, naming the pane's command"}},
		func(inst *PlayLauncher, call app.OperationCall, in SetSignalArgs) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			if strings.TrimSpace(in.Name) == "" {
				return appops.None{}, app.RefuseOperation("a signal needs a name")
			}
			name := SignalID(in.Name)
			person := call.Writer == opwire.WriterPerson
			if in.Node != "" && name != signalSelection {
				return appops.None{}, app.RefuseOperation("node applies only to selection")
			}
			if !person {
				// The person's Signals section may write anything; an
				// agent's value must hold, or the call reports a change
				// the pane undoes (ADR-0270, update of 2026-10-05).
				if reason := paneOutputRefusal(name); reason != "" {
					return appops.None{}, app.RefuseOperation(reason)
				}
			}
			writer := call.Writer
			switch writer {
			case opwire.WriterPerson:
				writer = p.takeGestureSignalWriter()
			case "":
				writer = signalWriterApp
			}
			if name == signalSelection {
				if err := p.setSelectionFrom(in.Value, NodeID(in.Node), writer, !person); err != nil {
					return appops.None{}, err
				}
			} else {
				p.graph.setSignalRawFrom(name, in.Value, writer)
			}
			p.markAgent(call.OnBehalfOf)
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
	// Untrusted: a status line quotes the data's values and lane errors.
	appops.Query(s, app.OperationSpec{Name: opListPanes, Version: 2,
		Summary: "list the panes: whether each can draw what it is fed and why not, what its last draw said, the node feeding it, the signals it writes, and the operations that read and drive it (get_<pane>, set_<pane>_options, a pane's select command)",
		Reads:   []string{opsResPanes, opsResResult, opsResSql}, Agents: true, Untrusted: true},
		func(sn opsSnap, in appops.None) (PanesState, error) {
			if !sn.mounted {
				return PanesState{}, app.RefuseOperation("the window has not mounted")
			}
			return sn.panes, nil
		})
	appops.Command(s, app.OperationSpec{Name: opBindPane, Version: 1, Summary: "feed a panel from one split node, or from the main result again",
		Effect: app.OperationEffectDocument, Writes: []string{opsResPanes}, Reads: []string{opsResSql}, Agents: true,
		Gesture: "the fill tab buttons of a node in the Graph pane",
		Follows: []string{"the pane draws the node's result once the node has run"}},
		func(inst *PlayLauncher, call app.OperationCall, in BindPaneArgs) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			return appops.None{}, p.bindPane(in.Pane, NodeID(in.Node))
		})
	addRunOps(s)
	addReferenceOps(s)
	addSchemaOps(s)
	addSchemaPaneOps(s)
	addRewriteOps(s)
	addDatasetOps(s)
	addBundleOps(s)
	addPublishResultOps(s)
	addProjectionOps(s)
	addArchetypesOps(s)
	addDiagnosticsOps(s)
	addChartOps(s)
	addDistOps(s)
	addSeriesOps(s)
	addTimelineOps(s)
	addHierarchyOps(s)
	addKanbanOps(s)
	addCardsOps(s)
	addWorldOps(s)
	addVectorfieldOps(s)
	addGraphOps(s)
	addSankeyOps(s)
	addTableOps(s)
	addFilesOps(s)
	addChatPaneOps(s)
	addMapOps(s)
	addFlowOps(s)
	addDocsOps(s)
	addDetailOps(s)
	addGlossOps(s)
	addCompletionOps(s)
	addEndpointFunctionOps(s)
	addQueryGraphOps(s)
	addHistoryOps(s)
	addWorkOps(s)
	return
}()

// bindPane binds a panel to a node of the current split, or unbinds it
// when node is empty. Unlike BindTab, a node the split lacks is refused:
// a binding that sits inert until the name returns is an embedder's tool,
// and to a caller it reads as a change that did nothing.
func (inst *PlayApp) bindPane(pane string, node NodeID) (err error) {
	spec, ok := inst.tabs.specForSlug(pane)
	if !ok {
		return app.RefuseOperation("no pane " + pane)
	}
	if spec.Panel == nil {
		return app.RefuseOperation("pane " + pane + " draws no result and cannot be bound")
	}
	if spec.Frameless {
		return app.RefuseOperation(framelessRefusal(pane, spec.Panel))
	}
	if node == "" {
		inst.unbindTab(pane)
		return
	}
	if _, found := findSplitNode(inst.currentSplit, node); !found {
		names := make([]string, 0, len(inst.currentSplit.Nodes))
		for _, n := range inst.currentSplit.Nodes {
			names = append(names, string(n.ID))
		}
		return app.ConflictOperation("no node " + string(node) + " in the buffer; it has " + strings.Join(names, ", "))
	}
	inst.bindTab(pane, node)
	return
}

// setSelectionFrom writes the row cursor the way a pane's click does: through
// selectionStamper, so selection_node names the node the row indexes and
// selection_id and selection_key are read off that row, rather than staying
// with the previous one. strict refuses what a click could not produce — a
// value that is not a row, a node not on screen, a row the result lacks —
// where the person's Signals section writes the value as typed.
func (inst *PlayApp) setSelectionFrom(raw string, node NodeID, writer string, strict bool) (err error) {
	direct := func() { inst.graph.setSignalRawFrom(signalSelection, raw, writer) }
	row, perr := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if perr != nil {
		if strict {
			return app.RefuseOperation("selection is a row number of a result, 0 for the first")
		}
		direct()
		return
	}
	if node == "" {
		node = inst.activeNodeID()
	}
	rec, visible := inst.selectionRecord(node)
	if rec != nil {
		defer rec.Release()
	}
	switch {
	case !visible:
		if strict {
			return app.RefuseOperation("node " + string(node) + " is not on screen; a selection indexes the result the panels draw (" +
				string(inst.activeNodeID()) + ") or a node a pane is bound to")
		}
		direct()
		return
	case rec == nil:
		if strict {
			return app.RefuseOperation("node " + string(node) + " holds no result to select in")
		}
		direct()
		return
	case row < 0 || row >= rec.NumRows():
		if strict {
			return app.RefuseOperation("node " + string(node) + " has " + strconv.FormatInt(rec.NumRows(), 10) + " rows; selection " + raw + " is not one of them")
		}
		direct()
		return
	}
	selectionStamper{inner: graphEmitter{graph: inst.graph, writer: writer}, node: node, rec: rec}.Emit(signalSelection, row)
	return
}

// selectionRecord is the result a selection on node indexes: the active
// result, or the lane view of a node a pane was bound to in the last frame.
// visible is false for any other node. The caller releases rec (nil-safe).
func (inst *PlayApp) selectionRecord(node NodeID) (rec arrow.RecordBatch, visible bool) {
	if node == inst.activeNodeID() {
		rec, _, _, _, _, _, _, _, _ = inst.activeSnapshot()
		return rec, true
	}
	if _, bound := inst.boundViews[node]; !bound {
		return
	}
	n, found := findSplitNode(inst.currentSplit, node)
	lane := inst.boundLanes[node]
	if !found || lane == nil {
		return
	}
	view := lane.demand(compileNodeFor(inst.currentSplit, n, inst.lastRunBound, inst.frameSig))
	return view.rec, true
}

// framelessRefusal is bind_pane's answer for a pane that reads its CTEs off
// the split by name: what it reads, and that the buffer is where to change it.
func framelessRefusal(pane string, panel PanelI) (reason string) {
	var ctes []string
	for _, ch := range panel.Channels() {
		label := ch.Label
		if label == "" {
			label = string(ch.ID)
		}
		ctes = append(ctes, "`"+label+"`")
	}
	return "pane " + pane + " reads the " + strings.Join(ctes, ", ") + " CTEs of the buffer by name and cannot be bound; " +
		"write or rename those CTEs with set_sql instead"
}

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
	sn.bus = inst.bus
	if inst.bundle != nil {
		sn.bundle = inst.bundle.alias
	}
	sn.lastPublish = inst.lastPublish()
	sn.installed, sn.probed = p.vocab.known()
	sn.client = p.client
	st := snapshotState(p)
	sn.state = st
	if inst.bundle != nil {
		sn.state.Bundle = inst.bundle.alias + "@" + strconv.FormatUint(inst.bundle.revision, 10)
	}
	if digest := inst.followedDigest(); digest != "" {
		sn.state.Followed = strings.Split(digest, ",")
	}
	sn.runSql, _, _ = p.runBuffer()
	raised, _ := p.tabs.slugForDockID(p.raisedTab)
	for _, row := range p.paneRows(p.frameSchema) {
		ps := PaneState{Pane: row.TabID, Title: row.Title, Panel: row.Panel,
			Node: string(row.Node), Reason: row.Reject, Writes: row.Drives, Unfilled: row.Unfilled,
			Draws: row.Draws, Raised: row.TabID == raised, Publishes: row.Publishes, Ops: paneOperations[row.TabID]}
		if spec, ok := p.tabs.specForSlug(row.TabID); ok {
			p.paneStatusState(&spec, &ps)
		}
		sn.panes.Panes = append(sn.panes.Panes, ps)
	}
	sn.results = snapshotResults(p)
	for _, n := range p.currentSplit.Nodes {
		sn.panes.Nodes = append(sn.panes.Nodes, string(n.ID))
	}
	sn.projection = snapshotProjection(p, sn.panes)
	sn.diagnostics = snapshotDiagnostics(p)
	sn.paneViews = snapshotPaneViews(p)
	sn.glossCatalog = p.glossCatalogView()
	sn.glossColumns = p.glossColumnsView()
	sn.docs = snapshotDocs(p)
	sn.detail = detailOpsView{embedded: p.detailContent != nil}
	sn.completion = p.completionView()
	sn.queryGraph = p.queryGraphView()
	if inst.follower != nil {
		sn.waiting = inst.follower.Waiting()
	}
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

// paneDigest is the raised pane, the observed node and the bindings,
// compared across frames.
func (inst *PlayApp) paneDigest() (digest string) {
	tabs := make([]string, 0, len(inst.tabBindings))
	for t := range inst.tabBindings {
		tabs = append(tabs, t)
	}
	slices.Sort(tabs)
	var b strings.Builder
	b.WriteString(strconv.FormatUint(inst.raisedTab, 10))
	// The observed node feeds every panel without a binding (list_panes'
	// Node), so the person's "observe in panels" moves the resource.
	b.WriteString("|observed=" + string(inst.observedNode))
	for _, t := range tabs {
		b.WriteString("|" + t + "=" + string(inst.tabBindings[t]))
	}
	return b.String()
}

// swapSql replaces the buffer and takes its prelude as the new defaults,
// as any whole-buffer swap does. An agent's call runs before play draws, so
// every reader of the frame sees the new text; the person's swaps reach it
// through set_sql (ADR-0270 §SD6).
func (inst *PlayApp) swapSql(sql string) {
	inst.sql = sql
	inst.captureParamDefaults(sql)
}

// RunArgs is run's argument.
type RunArgs struct {
	// Subquery narrows the run to the innermost query at the caret, as
	// Ctrl+Shift+Enter does.
	Subquery bool `json:",omitzero" desc:"run only the innermost query at the caret"`
	// Statement picks one statement of a buffer of several by number, so
	// an agent need not rely on the person's caret.
	Statement int32 `json:",omitzero" desc:"for a buffer of several statements, the one to run, from 1, shipped with the SET prelude; get_state's statements counts them; left out, the one at the person's caret"`
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
	appops.Command(playOps, app.OperationSpec{Name: opRun, Version: 2, Summary: "run the buffer, or one statement of it, under the agent limits",
		Effect: app.OperationEffectRun, Reads: []string{opsResSql, opsResParams, opsResSignals}, Writes: []string{opsResResult},
		Agents: true, Gesture: "the Run button",
		Follows: []string{"the result replaces the main result; describe_result reads it",
			"a run the grant does not cover is refused, naming the destination the grant would have to list",
			"a run the window's class ceiling would block is refused at the call, with the parameter that raised the class",
			"statement n ships the SET prelude and that statement, and the checks judge that text; cancel_run stops the run while it is in flight"}},
		func(inst *PlayLauncher, call app.OperationCall, in RunArgs) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			if call.Writer == opwire.WriterPerson {
				// The person's Run (ADR-0270 §SD6): no agent limits, and the
				// run path reports an empty buffer or an unfilled input in
				// the status line, as it always has.
				p.applyRunShortcut(true, in.Subquery)
				p.requestStatement = int(in.Statement)
				return appops.None{}, nil
			}
			if call.OnBehalfOf == nil {
				return appops.None{}, app.RefuseOperation("a run through the catalog is an agent's, and carries its context")
			}
			if strings.TrimSpace(p.sql) == "" {
				return appops.None{}, app.RefuseOperation("the buffer is empty")
			}
			if in.Statement != 0 && in.Subquery {
				return appops.None{}, app.RefuseOperation("subquery narrows at the person's caret; statement picks a statement without it: pass one of them")
			}
			names := p.unfilledInputs()
			if in.Statement != 0 {
				// Only what the statement ships needs a value, as the run
				// path judges it.
				stmt, err := p.statementBuffer(int(in.Statement))
				if err != nil {
					return appops.None{}, app.RefuseOperation(err.Error())
				}
				_, _, names = p.resolveRunSignals(stmt)
			}
			if len(names) > 0 {
				return appops.None{}, app.RefuseOperation("parameters need a value first: " + strings.Join(names, ", "))
			}
			if err := p.refuseAgentRunCall(call.OnBehalfOf, in.Subquery, int(in.Statement)); err != nil {
				return appops.None{}, err
			}
			p.markAgent(call.OnBehalfOf)
			p.agentRunRequested = true
			p.requestSubquery = in.Subquery
			p.requestStatement = int(in.Statement)
			p.RequestRun()
			return appops.None{}, nil
		})
	playOps.Available(opRun, func(sn opsSnap) (bool, string) {
		if sn.mounted && sn.state.Result.Phase == "running" {
			return false, "a run is in flight"
		}
		return true, ""
	})
	appops.Command(playOps, app.OperationSpec{Name: opSetParam, Version: 2, Summary: "set one parameter, in its tier",
		Effect: app.OperationEffectDocument, Writes: []string{opsResParams}, Agents: true, Gesture: "the parameter's field above the editor",
		Follows: []string{"a pinned parameter rewrites its SET line in the buffer; a live one writes its signal, and Live reruns",
			"an enum parameter takes one of the options get_state lists; any other value is refused"}},
		func(inst *PlayLauncher, call app.OperationCall, in SetParamArgs) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			d := p.paramDrafts[in.Name]
			if d == nil {
				return appops.None{}, app.ConflictOperation("no parameter " + in.Name + " in the buffer yet; slots follow a new buffer after a short debounce, read get_state again")
			}
			if call.Writer != opwire.WriterPerson {
				if err := p.refuseParamValue(in.Name, in.Value); err != nil {
					return appops.None{}, err
				}
			}
			*d = in.Value
			p.markAgent(call.OnBehalfOf)
			return appops.None{}, nil
		})
}

// refuseAgentRunCall is run's checks at the call, on the text the run will
// ship: the window's class ceiling, then the agent limits (on the whole
// buffer, or on the narrowed text of a subquery run or of statement n). The
// run path checks both again, where a refusal lands only in the status line.
// The ceiling goes first because its reason names the parameter that raised
// the class. statement is run's Statement argument, 0 when left out.
func (inst *PlayApp) refuseAgentRunCall(obo *app.OnBehalfOf, subquery bool, statement int) (err error) {
	runSQL, _, _ := inst.runBuffer()
	checked := inst.sql
	switch {
	case statement != 0:
		if runSQL, err = inst.statementBuffer(statement); err != nil {
			return app.RefuseOperation(err.Error())
		}
		checked = runSQL
	case subquery:
		runSQL, _ = inst.runSubqueryBuffer()
		checked = runSQL
	}
	if reason := inst.exprCeilingRefusal(runSQL); reason != "" {
		return app.RefuseOperation(reason)
	}
	return inst.refuseAgentRunOf(obo, checked)
}

const (
	opRun      = "run"
	opSetParam = "set_param"
)

// playGesture routes one of the person's gestures through play's catalog
// (ADR-0269 §SD8 "One path", ADR-0270 §SD6): the same handler an agent's
// call runs, logged with the person as writer, so a task that read the
// resource pauses. Where no host serves play's catalog — an embedder's
// window, a test — direct applies the change instead.
func playGesture[In any](inst *PlayApp, op string, in In, direct func()) {
	if inst.gestureCtx == nil {
		direct()
		return
	}
	if _, err := appops.Gesture[In, appops.None](inst.gestureCtx, op, in); err != nil {
		direct()
	}
}

// personRun is the person's Run: the button, the Run subquery button,
// Ctrl+Enter and Ctrl+Shift+Enter.
func (inst *PlayApp) personRun(sub bool) {
	playGesture(inst, opRun, RunArgs{Subquery: sub}, func() { inst.applyRunShortcut(true, sub) })
}

// personSetSql is the person swapping the whole buffer.
func (inst *PlayApp) personSetSql(sql string) {
	playGesture(inst, opSetSql, SetSqlArgs{Sql: sql}, func() { inst.swapSql(sql) })
}

// personSetSignal is the person writing a signal; writer is the signal
// writer the store stamps, one of isHumanSignalWriter's.
func (inst *PlayApp) personSetSignal(name SignalID, raw string, writer string) {
	inst.gestureSignalWriter = writer
	playGesture(inst, opSetSignal, SetSignalArgs{Name: string(name), Value: raw}, func() {
		inst.graph.setSignalRawFrom(name, raw, writer)
	})
	inst.gestureSignalWriter = ""
}

// takeGestureSignalWriter is the signal writer of the person's set_signal
// in flight: the surface personSetSignal named, else the Signals section.
func (inst *PlayApp) takeGestureSignalWriter() (writer string) {
	writer = inst.gestureSignalWriter
	if writer == "" {
		writer = signalWriterEditor
	}
	return
}

// personShowPane is the person raising a pane from play's own chrome.
func (inst *PlayApp) personShowPane(pane string) {
	playGesture(inst, opShowPane, ShowPaneArgs{Pane: pane}, func() { _ = inst.ActivateTab(pane) })
}

// personBindPane is the person's fill-tab toggle in the Graph pane.
func (inst *PlayApp) personBindPane(pane string, node NodeID) {
	playGesture(inst, opBindPane, BindPaneArgs{Pane: pane, Node: string(node)}, func() {
		if node == "" {
			inst.unbindTab(pane)
			return
		}
		inst.bindTab(pane, node)
	})
}

// personClearBindings is the Graph pane's clear: each binding undone
// through bind_pane, so each is logged as the person's.
func (inst *PlayApp) personClearBindings() {
	if inst.gestureCtx == nil {
		inst.clearBindings()
		return
	}
	panes := make([]string, 0, len(inst.tabBindings))
	for pane := range inst.tabBindings {
		panes = append(panes, pane)
	}
	slices.Sort(panes)
	for _, pane := range panes {
		inst.personBindPane(pane, "")
	}
}
