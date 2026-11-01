package logviewer

import (
	"encoding/base64"
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
)

// The operations catalog (ADR-0345). Queries read the window's cache of
// rows from boxer.facts as the snapshot took it with the frame, never the
// store itself, so a query does not wait on ClickHouse; each says when the
// cache was filled. They filter by their own arguments, never by the
// window's filter row (§SD2), and say when those arguments reach outside
// the scope the cache was filled under. Commands set the view and never
// toggle; nothing here writes the store.

const (
	resFilter    = "filter"
	resSelection = "selection"
	resFollow    = "follow"

	opGetCounters = "get_counters"
	opListRows    = "list_rows"
	opGetRow      = "get_row"
	opGetView     = "get_view"

	opSetFilter = "set_filter"
	opSetFollow = "set_follow"
	opSelectRow = "select_row"
)

// Result bounds (§SD2): what one inline result may carry.
const (
	opsMaxRows       = 200
	opsDefaultRows   = 50
	opsMaxMessageLen = 512
	opsMaxTextLen    = 4096
	opsMaxFields     = 64
	opsMaxFacts      = 64
)

const (
	rowGoneReason = "no row with that key is in the window's cache; it may be outside the window's filter or past the cache's row cap, and is still in boxer.facts"
)

// filterState is the filter resource's value: comparable, so the host can
// tell a change across the frame's write-back. The cache is filled under
// one, so it is also the cache's scope.
type filterState struct {
	Level   zerolog.Level
	AppId   string
	Message string
	// AllRuns reads every run in the look-back; false reads this
	// process's run.
	AllRuns bool
	// Instance keeps one window's rows; 0 keeps every window's.
	Instance uint64
}

func (inst *LogViewerApp) filterValue() (f filterState) {
	f = filterState{Level: inst.filterLevel, AppId: inst.filterAppId, Message: inst.filterMessage,
		AllRuns: inst.filterAllRuns, Instance: inst.filterInstance}
	return
}

func (inst *LogViewerApp) applyFilter(f filterState) {
	inst.filterLevel, inst.filterAppId, inst.filterMessage = f.Level, f.AppId, f.Message
	inst.filterAllRuns, inst.filterInstance = f.AllRuns, f.Instance
}

// selectionKey is the selection resource's value: the selected row's key,
// or "" for none.
func (inst *LogViewerApp) selectionKey() (key string) {
	if inst.hasSelected {
		key = opsRowKey(inst.selected)
	}
	return
}

func (inst *LogViewerApp) applySelection(r factsstore.LogRow, has bool) {
	inst.selected, inst.hasSelected = r, has
	if !has {
		inst.selected = factsstore.LogRow{}
	}
}

// opsRowKey names a row for a caller: its timestamp and a digest of its
// caller and message, the same three the window's rowKey compares, in a
// form short enough to pass back. Fact ids restart in every process, so
// the row's id in boxer.facts would not name it across runs.
func opsRowKey(r factsstore.LogRow) (key string) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(r.Caller))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(r.Message))
	key = strconv.FormatInt(r.Ts.UnixNano(), 10) + "-" + strconv.FormatUint(uint64(h.Sum32()), 16)
	return
}

// findRow returns the entry key names among rows.
func findRow(rows []Entry, key string) (e Entry, ok bool) {
	for i := range rows {
		if opsRowKey(rows[i].Row) == key {
			e, ok = rows[i], true
			return
		}
	}
	return
}

// opsSnap is what queries read: the cache and the view as the frame left
// them. rows is the cache's own slice, which a fill replaces whole and
// nothing writes in place.
type opsSnap struct {
	src         *Source
	rows        []Entry
	scope       filterState
	filled      bool
	refreshedAt time.Time
	lastErr     error
	taken       time.Time
	capRows     int
	counters    Counters
	filter      filterState
	follow      bool
	selected    string
}

func (inst *LogViewerApp) opsSnapshot() (sn opsSnap) {
	sn.filter = inst.filterValue()
	sn.follow = inst.follow
	sn.selected = inst.selectionKey()
	sn.src = sourceRef.Load()
	sn.rows, sn.scope, sn.filled = inst.cache.rows, inst.cache.scope, inst.cache.filled
	sn.refreshedAt, sn.lastErr = inst.cache.refreshedAt, inst.cache.lastErr
	sn.taken = time.Now()
	sn.capRows = cacheCap()
	sn.counters = Counters{Persisted: sn.src.persisted(), TailRows: len(sn.rows), TailCapacity: sn.capRows,
		RefreshedAt: sn.refreshedAt, AgeMs: sn.ageMs()}
	if sn.src != nil && sn.src.Sink != nil {
		sink := sn.src.Sink
		sn.counters.Installed = true
		sn.counters.Decoded = sink.Decoded()
		sn.counters.Written = sink.Written()
		sn.counters.Dropped = sink.Dropped()
		sn.counters.ParseErrors = sink.ParseErrors()
		sn.counters.WriteErrors = sink.WriteErrors()
	}
	return
}

// ageMs is how old the cache was when the snapshot was taken; -1 before
// its first fill.
func (inst opsSnap) ageMs() (ms int64) {
	ms = -1
	if inst.filled {
		ms = inst.taken.Sub(inst.refreshedAt).Milliseconds()
	}
	return
}

// cacheDiagnostic says why the cache may hold fewer rows than the store:
// nothing persisted, not filled yet, or the last refresh failed. Empty
// when none applies.
func (inst opsSnap) cacheDiagnostic() (d string) {
	switch {
	case !inst.src.persisted():
		d = notPersistedReason(inst.src)
	case !inst.filled && inst.lastErr != nil:
		d = "the window could not read boxer.facts: " + inst.lastErr.Error()
	case !inst.filled:
		d = "the window has not read boxer.facts yet; ask again shortly"
	case inst.lastErr != nil:
		d = "the last refresh failed, so the rows are from the refresh before it: " + inst.lastErr.Error()
	}
	return
}

// Counters is get_counters's result: the bridge's running totals since the
// process started, and the window's cache.
type Counters struct {
	Installed    bool      `desc:"the host installed a log bridge; the bridge's totals are 0 when false"`
	Persisted    bool      `desc:"log lines reach boxer.facts on this host, which is where the window reads them"`
	TailRows     int       `desc:"rows the window's cache holds"`
	TailCapacity int       `desc:"rows the cache can hold: the newest matching the window's filter"`
	RefreshedAt  time.Time `desc:"when the cache was last filled from boxer.facts; zero before the first fill"`
	AgeMs        int64     `desc:"how old the cache was when this was read, in milliseconds; -1 before the first fill"`
	Decoded      uint64    `desc:"events decoded into rows"`
	Written      uint64    `desc:"rows handed to the facts store"`
	Dropped      uint64    `desc:"rows the flush ring evicted because the store fell behind"`
	ParseErrors  uint64    `desc:"events that failed to decode"`
	WriteErrors  uint64    `desc:"rows the store rejected"`
}

// ListRowsArgs filters the cache; every field is optional.
type ListRowsArgs struct {
	MinLevel string `desc:"the lowest level to return: trace, debug, info, warn, error, fatal or panic; empty keeps every row"`
	App      string `desc:"case-insensitive substring of the app id"`
	Message  string `desc:"case-insensitive substring of the message"`
	Limit    int    `desc:"rows to return, the newest; default 50, at most 200"`
}

// RowSummary is one row as the table shows it.
type RowSummary struct {
	Key      string    `desc:"pass to get_row or select_row; valid while the row is in the window's cache"`
	Ts       time.Time `desc:"when the event was written"`
	Level    string    `desc:"zerolog level; empty for a level-less event"`
	App      string    `desc:"the app id the event was tagged with"`
	Caller   string    `desc:"file:line of the writer"`
	Message  string    `desc:"the message, cut at 512 characters"`
	HasError bool      `desc:"the event carries an error"`
	Fields   int       `desc:"structured fields on the event"`
}

// ListRowsResult is list_rows's result.
type ListRowsResult struct {
	Matched      int          `desc:"rows in the cache that match the arguments"`
	Rows         []RowSummary `desc:"the last limit matches, oldest first"`
	TailRows     int          `desc:"rows the window's cache holds"`
	TailCapacity int          `desc:"rows the cache can hold"`
	Persisted    bool         `desc:"log lines reach boxer.facts on this host; when false there is nothing to list"`
	RefreshedAt  time.Time    `desc:"when the cache was last filled from boxer.facts; zero before the first fill"`
	AgeMs        int64        `desc:"how old the cache was when this was read, in milliseconds; -1 before the first fill"`
	Scope        CacheScope   `desc:"the window's filter the cache was filled under; rows outside it are not in the cache"`
	Diagnostic   string       `desc:"why the result may be short or empty: nothing persisted, the cache not filled yet, a failed refresh, or arguments wider than the cache's scope; empty when none applies"`
}

// CacheScope is the filter a window's cache was filled under.
type CacheScope struct {
	MinLevel string `desc:"the level floor; trace keeps every row"`
	App      string `desc:"the app substring"`
	Message  string `desc:"the message substring"`
	AllRuns  bool   `desc:"every run in the look-back; false is this process's run"`
	Instance uint64 `desc:"one window's rows; 0 is every window's"`
}

// GetRowArgs names one row.
type GetRowArgs struct {
	Key string `desc:"a key from list_rows or get_view"`
}

// FieldValue is one structured field, rendered as text.
type FieldValue struct {
	Name  string `desc:"the field's name"`
	Kind  string `desc:"string, int, uint, float, bool, bytes, time or unknown"`
	Value string `desc:"the value as text; bytes as base64; cut at 4096 characters"`
}

// ErrorFact is one node of a structured error chain.
type ErrorFact struct {
	Msg      string `desc:"the fact's message"`
	Source   string `desc:"source file of the frame, when the fact carries one"`
	Line     string `desc:"line of the frame"`
	Function string `desc:"function of the frame"`
	Data     string `desc:"CBOR diagnostic of attached data, cut at 4096 characters"`
}

// ErrorStream is one stream of a structured error chain: the facts that
// share a stack.
type ErrorStream struct {
	Name  string      `desc:"no-stack, or stack-N"`
	Facts []ErrorFact `desc:"the stream's facts, at most 64"`
}

// RowDetail is get_row's result: everything the detail pane shows.
type RowDetail struct {
	Key         string        `desc:"the key asked for"`
	Ts          time.Time     `desc:"when the event was written"`
	Level       string        `desc:"zerolog level"`
	App         string        `desc:"the app id"`
	InstanceKey uint64        `desc:"the window the event belonged to; 0 when none"`
	RunId       string        `desc:"the run the event was written in; empty for a row written without one"`
	Service     string        `desc:"the service field, when set"`
	Caller      string        `desc:"file:line of the writer"`
	Message     string        `desc:"the message, cut at 4096 characters"`
	Error       string        `desc:"the flat error text, cut at 4096 characters"`
	Stack       string        `desc:"the stack text, when the writer attached one, cut at 4096 characters"`
	Fields      []FieldValue  `desc:"structured fields, at most 64"`
	FieldsTotal int           `desc:"structured fields on the event"`
	ErrorChain  []ErrorStream `desc:"the structured error chain; boxer.facts keeps only its flat summary, so a row read back from it has none"`
}

// View is get_view's result: the filter row, follow and the selection as
// the person sees them, and the cache behind the table.
type View struct {
	Installed   bool      `desc:"the host installed a log bridge"`
	Persisted   bool      `desc:"log lines reach boxer.facts on this host"`
	MinLevel    string    `desc:"the filter row's level floor; trace keeps every row"`
	App         string    `desc:"the filter row's app substring"`
	Message     string    `desc:"the filter row's message substring"`
	AllRuns     bool      `desc:"the window reads every run in its look-back; false reads this process's run"`
	Instance    uint64    `desc:"the window whose rows are read; 0 reads every window's"`
	Follow      bool      `desc:"the table scrolls to the newest row"`
	Selected    string    `desc:"the selected row's key; empty for none"`
	Matched     int       `desc:"rows in the cache the filter row keeps"`
	Shown       int       `desc:"rows the table draws"`
	RefreshedAt time.Time `desc:"when the cache was last filled from boxer.facts; zero before the first fill"`
}

// SetFilterArgs sets the filter row whole.
type SetFilterArgs struct {
	MinLevel string `desc:"trace, debug, info, warn, error, fatal or panic; empty means trace"`
	App      string `desc:"case-insensitive substring of the app id; empty matches every row"`
	Message  string `desc:"case-insensitive substring of the message; empty matches every row"`
	AllRuns  bool   `desc:"read every run in the look-back instead of this process's run"`
	Instance uint64 `desc:"read one window's rows by its instance key; 0 reads every window's"`
}

// SetFollowArgs sets whether the table follows the newest row.
type SetFollowArgs struct {
	Follow bool `desc:"scroll to the newest row as rows arrive"`
}

// SelectRowArgs selects a row, which opens the detail pane.
type SelectRowArgs struct {
	Key string `desc:"a key from list_rows; empty clears the selection"`
}

func levelNameList() (s string) {
	names := make([]string, 0, len(levelOptions))
	for _, l := range levelOptions {
		names = append(names, l.String())
	}
	s = strings.Join(names, ", ")
	return
}

// parseLevelArg reads a level name a caller wrote; empty is the no-floor
// sentinel, as the combo's trace is.
func parseLevelArg(s string) (lvl zerolog.Level, err error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		lvl = zerolog.TraceLevel
		return
	}
	for _, l := range levelOptions {
		if l.String() == s {
			lvl = l
			return
		}
	}
	err = app.RefuseOperation("min_level is one of " + levelNameList())
	return
}

func summarizeRow(r factsstore.LogRow) (s RowSummary) {
	s = RowSummary{Key: opsRowKey(r), Ts: r.Ts, Level: r.Level, App: string(r.AppId), Caller: r.Caller,
		Message: appops.CutText(r.Message, opsMaxMessageLen), HasError: r.Error != "" || r.ErrorContext != nil, Fields: len(r.Fields)}
	return
}

func fieldText(f factsstore.LogField) (v FieldValue) {
	v.Name = f.Name
	switch f.Kind {
	case factsstore.LogFieldKindString:
		v.Kind, v.Value = "string", f.Str
	case factsstore.LogFieldKindInt:
		v.Kind, v.Value = "int", strconv.FormatInt(f.Int, 10)
	case factsstore.LogFieldKindUint:
		v.Kind, v.Value = "uint", strconv.FormatUint(f.Uint, 10)
	case factsstore.LogFieldKindFloat:
		v.Kind, v.Value = "float", strconv.FormatFloat(f.Float, 'g', -1, 64)
	case factsstore.LogFieldKindBool:
		v.Kind, v.Value = "bool", strconv.FormatBool(f.Bool)
	case factsstore.LogFieldKindBytes:
		v.Kind, v.Value = "bytes", base64.StdEncoding.EncodeToString(f.Bytes)
	case factsstore.LogFieldKindTime:
		v.Kind, v.Value = "time", f.Time.UTC().Format(time.RFC3339Nano)
	default:
		v.Kind, v.Value = "unknown", f.Str
	}
	v.Value = appops.CutText(v.Value, opsMaxTextLen)
	return
}

func detailRow(key string, e Entry) (d RowDetail) {
	r := e.Row
	d = RowDetail{Key: key, Ts: r.Ts, Level: r.Level, App: string(r.AppId), InstanceKey: r.InstanceKey, RunId: e.RunId,
		Service: r.Service, Caller: r.Caller, Message: appops.CutText(r.Message, opsMaxTextLen),
		Error: appops.CutText(r.Error, opsMaxTextLen), Stack: appops.CutText(r.Stack, opsMaxTextLen), FieldsTotal: len(r.Fields)}
	for i, f := range r.Fields {
		if i == opsMaxFields {
			break
		}
		d.Fields = append(d.Fields, fieldText(f))
	}
	if r.ErrorContext == nil {
		return
	}
	for _, s := range r.ErrorContext.Streams {
		es := ErrorStream{Name: s.Name}
		for i, f := range s.Facts {
			if i == opsMaxFacts {
				break
			}
			es.Facts = append(es.Facts, ErrorFact{Msg: f.Msg, Source: f.Source, Line: f.Line, Function: f.Function,
				Data: appops.CutText(f.DataDiag, opsMaxTextLen)})
		}
		d.ErrorChain = append(d.ErrorChain, es)
	}
	return
}

func persistedOr(sn opsSnap) (ok bool, reason string) {
	ok = sn.src.persisted()
	if !ok {
		reason = notPersistedReason(sn.src)
	}
	return
}

func scopeOf(f filterState) (c CacheScope) {
	c = CacheScope{MinLevel: f.Level.String(), App: f.AppId, Message: f.Message, AllRuns: f.AllRuns, Instance: f.Instance}
	return
}

var ops = func() (s *appops.Set[*LogViewerApp, opsSnap]) {
	s = appops.NewSet((*LogViewerApp).opsSnapshot)

	s.Resource(resFilter, "the filter row: level floor, app substring, message substring",
		func(inst *LogViewerApp) any { return inst.filterValue() })
	s.Restorable(resFilter, func(inst *LogViewerApp, v any) bool {
		f, ok := v.(filterState)
		if ok {
			inst.applyFilter(f)
		}
		return ok
	})
	s.Editing(resFilter, func(inst *LogViewerApp) bool {
		return appops.WidgetEditing(inst.appFilterH) || appops.WidgetEditing(inst.msgFilterH)
	})
	s.Resource(resSelection, "the selected row's key, which opens the detail pane; empty for none",
		func(inst *LogViewerApp) any { return inst.selectionKey() })
	s.Restorable(resSelection, func(inst *LogViewerApp, v any) bool {
		key, ok := v.(string)
		if !ok {
			return false
		}
		if key == "" {
			inst.applySelection(factsstore.LogRow{}, false)
			return true
		}
		e, found := findRow(inst.cache.rows, key)
		if found {
			inst.applySelection(e.Row, true)
		}
		return found
	})
	s.Resource(resFollow, "whether the table scrolls to the newest row", func(inst *LogViewerApp) any { return inst.follow })
	s.Restorable(resFollow, func(inst *LogViewerApp, v any) bool {
		f, ok := v.(bool)
		if ok {
			inst.applyFollow(f)
		}
		return ok
	})

	// list_rows answers without a store, with an empty set and the
	// reason; a row cannot be read or selected where there are none.
	s.Available(opGetRow, persistedOr)
	s.Available(opSelectRow, persistedOr)

	// --- queries ---

	appops.Query(s, app.OperationSpec{Name: opGetCounters, Version: 1,
		Summary: "read the log bridge's running totals — decoded, written, dropped, parse and write errors — and the window's cache: rows held, when it was filled",
		Agents:  true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out Counters, err error) {
			out = sn.counters
			return
		})

	appops.Query(s, app.OperationSpec{Name: opListRows, Version: 1,
		Summary: "list the newest log rows in the window's cache of boxer.facts that match a level floor and substrings, at most 200, with when the cache was filled",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in ListRowsArgs) (out ListRowsResult, err error) {
			lvl, err := parseLevelArg(in.MinLevel)
			if err != nil {
				return
			}
			limit := in.Limit
			switch {
			case limit < 0:
				err = app.RefuseOperation("limit is not negative")
				return
			case limit == 0:
				limit = opsDefaultRows
			case limit > opsMaxRows:
				limit = opsMaxRows
			}
			out.Persisted = sn.src.persisted()
			out.TailRows, out.TailCapacity = len(sn.rows), sn.capRows
			out.RefreshedAt, out.AgeMs = sn.refreshedAt, sn.ageMs()
			out.Scope = scopeOf(sn.scope)
			out.Diagnostic = sn.cacheDiagnostic()
			if out.Diagnostic == "" && !covers(sn.scope, lvl, in.App, in.Message) {
				out.Diagnostic = "the arguments are wider than the filter the window's cache was filled under (see scope), so rows outside it are not listed; set_filter widens it"
			}
			rows := filterEntries(sn.rows, lvl, in.App, in.Message)
			out.Matched = len(rows)
			if len(rows) > limit {
				rows = rows[:limit]
			}
			out.Rows = make([]RowSummary, 0, len(rows))
			for i := len(rows) - 1; i >= 0; i-- {
				out.Rows = append(out.Rows, summarizeRow(rows[i].Row))
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetRow, Version: 1,
		Summary: "read one log row in full: fields, the error chain, the stack",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetRowArgs) (out RowDetail, err error) {
			e, ok := findRow(sn.rows, in.Key)
			if !ok {
				err = app.RefuseOperation(rowGoneReason)
				return
			}
			out = detailRow(in.Key, e)
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetView, Version: 1,
		Summary: "read the filter row, follow, the selection, how many rows the table shows and when its cache was filled",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out View, err error) {
			out = View{MinLevel: sn.filter.Level.String(), App: sn.filter.AppId, Message: sn.filter.Message,
				AllRuns: sn.filter.AllRuns, Instance: sn.filter.Instance}
			out.Installed, out.Persisted = sn.counters.Installed, sn.counters.Persisted
			out.Follow, out.Selected, out.RefreshedAt = sn.follow, sn.selected, sn.refreshedAt
			out.Matched = len(filterEntries(sn.rows, sn.filter.Level, sn.filter.AppId, sn.filter.Message))
			out.Shown = out.Matched
			return
		})

	// --- commands ---

	appops.Command(s, app.OperationSpec{Name: opSetFilter, Version: 1,
		Summary: "set the filter row whole: level floor, app substring, message substring, every run or this one, one window or all; the window re-reads boxer.facts under it",
		Effect:  app.OperationEffectView, Writes: []string{resFilter}, Agents: true,
		Gesture: "the level combo, the App and Message boxes", Rebuild: app.OperationRebuildVolatile},
		func(inst *LogViewerApp, _ app.OperationCall, in SetFilterArgs) (out appops.None, err error) {
			lvl, err := parseLevelArg(in.MinLevel)
			if err != nil {
				return
			}
			inst.applyFilter(filterState{Level: lvl, AppId: in.App, Message: in.Message, AllRuns: in.AllRuns, Instance: in.Instance})
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetFollow, Version: 1,
		Summary: "set whether the table scrolls to the newest row",
		Effect:  app.OperationEffectView, Writes: []string{resFollow}, Agents: true,
		Gesture: "the Follow checkbox", Rebuild: app.OperationRebuildVolatile},
		func(inst *LogViewerApp, _ app.OperationCall, in SetFollowArgs) (out appops.None, err error) {
			inst.applyFollow(in.Follow)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSelectRow, Version: 1,
		Summary: "select a row by key, which opens its detail pane; an empty key clears the selection",
		Effect:  app.OperationEffectView, Writes: []string{resSelection}, Agents: true,
		Gesture: "a row click, the Clear selection button", Rebuild: app.OperationRebuildVolatile},
		func(inst *LogViewerApp, _ app.OperationCall, in SelectRowArgs) (out appops.None, err error) {
			if in.Key == "" {
				inst.applySelection(factsstore.LogRow{}, false)
				return
			}
			if src := sourceRef.Load(); !src.persisted() {
				err = app.RefuseOperation(notPersistedReason(src))
				return
			}
			e, ok := findRow(inst.cache.rows, in.Key)
			if !ok {
				err = app.RefuseOperation(rowGoneReason)
				return
			}
			inst.applySelection(e.Row, true)
			return
		})

	return
}()

// Operations serves the catalog for this window (app.OperationsAppI).
func (inst *LogViewerApp) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

// gestureOr routes the person's control through the catalog (ADR-0345
// §SD3), so the command log sees it as theirs, and applies it directly
// where no host serves the catalog (tour, tests).
func gestureOr[In any](inst *LogViewerApp, op string, in In, direct func()) {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[In, appops.None](inst.frameCtx, op, in); err == nil {
			return
		}
	}
	direct()
}

func (inst *LogViewerApp) setLevelGesture(lvl zerolog.Level) {
	f := inst.filterValue()
	f.Level = lvl
	inst.setFilterGesture(f)
}

// setAllRunsGesture is the This run / All runs toggle: a button, answered
// from the frame's response flags, so it routes inside the frame.
func (inst *LogViewerApp) setAllRunsGesture(all bool) {
	f := inst.filterValue()
	f.AllRuns = all
	inst.setFilterGesture(f)
}

func (inst *LogViewerApp) setFilterGesture(f filterState) {
	gestureOr(inst, opSetFilter, SetFilterArgs{MinLevel: f.Level.String(), App: f.AppId, Message: f.Message,
		AllRuns: f.AllRuns, Instance: f.Instance}, func() { inst.applyFilter(f) })
}

// applyFollow sets follow and the checkbox's draft together, so a command's
// write is not mistaken for the person's click on the next frame.
func (inst *LogViewerApp) applyFollow(follow bool) {
	inst.follow, inst.followDraft = follow, follow
}

func (inst *LogViewerApp) setFollowGesture(follow bool) {
	gestureOr(inst, opSetFollow, SetFollowArgs{Follow: follow}, func() { inst.applyFollow(follow) })
}

// selectGesture selects r, or clears the selection when has is false. A
// row click computes the next state — a click on the selected row clears
// it — so the command itself sets and never toggles.
func (inst *LogViewerApp) selectGesture(r factsstore.LogRow, has bool) {
	key := ""
	if has {
		key = opsRowKey(r)
	}
	gestureOr(inst, opSelectRow, SelectRowArgs{Key: key}, func() { inst.applySelection(r, has) })
}
