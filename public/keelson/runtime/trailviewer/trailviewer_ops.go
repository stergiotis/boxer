package trailviewer

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/fsmops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
	"github.com/stergiotis/boxer/public/keelson/runtime/trailviewer/verdict"
)

// The operations catalog (ADR-0350 §SD8, proposed): the table's queries
// and the view's commands here, the panes' and the views' in
// trailviewer_ops_controls.go.
// Queries read the window's caches as the snapshot took them with the
// frame, never the store, so a query does not wait on ClickHouse; each
// says when the cache was filled. They select by their own arguments and
// say when those reach outside what the window read; what they leave out
// is the window's own (scopeArgs). Each reports the revisions of
// the resources the commands beside it write, so an agent's read admits
// its next write. The view commands set the view and never toggle; every control the person has
// routes through one (gestureOr). The saved views are authored state, so
// save_view and delete_view are document commands; send_to_play writes
// outside the app and is consequential, confirmed by the person on an
// agent's call. One command writes the trail: mark_row, consequential and
// person-only (ADR-0350 §SD7), which an agent is not offered and whose
// handler refuses an agent's task.

const (
	resCase      = "case"
	resRange     = "range"
	resFilter    = "filter"
	resSelection = "selection"
	resTab       = "tab"
	resSeries    = "series"
	resStrip     = "strip"
	resGrouping  = "grouping"
	resCompare   = "compare"
	resMatrix    = "matrix"
	resDotted    = "dotted"
	resSide      = "side"
	resTree      = "tree"
	resRead      = "read"
	resViews     = "views"
	// resReadState is the read lifecycle's state, declared by fsmops.Mount
	// under mountRead with the read_state and read_machine queries.
	mountRead    = "read"
	resReadState = mountRead + "_state"

	opListRows    = "list_rows"
	opGetRow      = "get_row"
	opGetPrevious = "get_previous"
	opGetVerdict  = "get_verdict"
	opGetView     = "get_view"
	opGetExc      = "get_exceptions"
	opGetTree     = "get_tree"
	opGetDayHour  = "get_dayhour"
	opGetSeries   = "get_series"
	opGetFacets   = "get_facets"
	opGetStrip    = "get_strip"
	opGetContext  = "get_context"
	opGetCompare  = "get_compare"
	opGetMatrix   = "get_matrix"
	opGetDotted   = "get_dotted"

	opSetCase     = "set_case"
	opSetRange    = "set_range"
	opSetFilter   = "set_filter"
	opSelectRow   = "select_row"
	opSetTab      = "set_tab"
	opSetSeries   = "set_series"
	opSetStrip    = "set_strip"
	opSetGrouping = "set_grouping"
	opSetCompare  = "set_compare"
	opSetMatrix   = "set_matrix"
	opSetDotted   = "set_dotted"
	opSetSide     = "set_side"
	opSetTree     = "set_tree"
	opRefresh     = "refresh"
	opStopRead    = "stop_read"
	opSaveView    = "save_view"
	opOpenView    = "open_view"
	opDeleteView  = "delete_view"
	opSendToPlay  = "send_to_play"
	opMarkRow     = "mark_row"
)

// Result bounds: what one inline result may carry.
const (
	opsMaxRows     = 200
	opsDefaultRows = 50
	opsMaxHeadline = 512
	opsMaxText     = 4096
	opsMaxAttrs    = 64
)

const rowGoneReason = "no row with that id is in the window's cache; it may be outside the window's case, range or filter, or past the rows read so far, and is still in boxer.facts"

// What the queries read, as their answers report it: the resources whose
// revisions a following command is checked against.
var (
	readsRead    = []string{resRead}
	readsScope   = []string{resCase, resRange, resFilter, resRead}
	readsRows    = []string{resCase, resRange, resFilter, resSelection, resCompare, resRead}
	readsTree    = []string{resCase, resTree, resRead}
	readsSeries  = []string{resCase, resRange, resFilter, resSeries, resRead}
	readsStrip   = []string{resCase, resRange, resFilter, resStrip, resRead}
	readsCompare = []string{resSelection, resCompare, resRead}
	readsMatrix  = []string{resCase, resRange, resFilter, resMatrix, resRead}
	readsDotted  = []string{resCase, resRange, resFilter, resDotted, resRead}
	readsView    = []string{resCase, resRange, resFilter, resSelection, resTab, resSeries, resStrip, resGrouping,
		resCompare, resMatrix, resDotted, resSide, resTree, resRead, resViews}
)

// opsSnap is what queries read: the caches and the view as the frame left
// them. The slices are the caches' own, which a fill replaces whole.
type opsSnap struct {
	src         *Source
	rows        []Row
	more        bool
	scope       scope
	from, to    time.Time
	resolvedAt  time.Time
	buckets     []Bucket
	step        time.Duration
	facets      [][]FacetValue
	filled      bool
	refreshedAt time.Time
	lastErr     error
	readState   string
	taken       time.Time
	lines       []verdict.Line
	linesAt     time.Time
	detail      *detailFill
	exc         exceptionSet
	marks       map[uint64]Review
	asOf        *AsOf
	gen         uint64
	views       []string
	// The analytic views as read for the window's current key, each with
	// whether the read predates it, and the window's picks.
	dayhour      *dayHourData
	dayhourStale bool
	series       *seriesData
	seriesStale  bool
	seriesPick   seriesPick
	matrix       *matrixData
	matrixStale  bool
	matrixPick   matrixPick
	dotted       *dottedData
	dottedStale  bool
	dottedPick   DottedRowE

	c        Case
	window   Window
	filter   Filter
	tab      TabE
	selected uint64
	// The history's top and the context extent as last read.
	previous       Window
	hasPrevious    bool
	ctxFrom, ctxTo time.Time
	// The rest of the view: the strip's colouring, the grouping, the
	// compare row, the side pane and the tree's expansion.
	strip        StripByE
	grouped      bool
	compare      Row
	hasCompare   bool
	side         sideE
	treeExpanded bool
}

func (inst *App) opsSnapshot() (sn opsSnap) {
	sn.src = sourceRef.Load()
	ch := &inst.cache
	sn.rows, sn.more, sn.scope, sn.from, sn.to, sn.resolvedAt = ch.rows, ch.more, ch.scope, ch.from, ch.to, ch.resolvedAt
	sn.buckets, sn.step, sn.facets = ch.buckets, ch.step, ch.facets
	sn.filled, sn.refreshedAt, sn.lastErr = ch.filled, ch.refreshedAt, ch.lastErr
	sn.readState = inst.readFSM.Current().String()
	sn.lines, sn.linesAt = inst.verdicts.lines, inst.verdicts.at
	sn.exc, sn.marks, sn.asOf = inst.exceptions(), inst.marks(), ch.asOf
	sn.taken = time.Now()
	sn.gen, sn.views = ch.gen, inst.views.names()
	sn.c, sn.window, sn.filter, sn.tab = inst.caseValue, inst.window, inst.filter, inst.tab
	sn.previous, sn.hasPrevious = inst.history.top()
	sn.ctxFrom, sn.ctxTo = ch.ctxFrom, ch.ctxTo
	sn.dayhour, sn.dayhourStale = inst.an.dayhour.current(inst.dayHourKey())
	sn.series, sn.seriesStale = inst.an.series.data.current(inst.seriesKey())
	sn.seriesPick = inst.an.series.pick
	sn.matrix, sn.matrixStale = inst.an.matrix.current(inst.matrixKey())
	sn.matrixPick = inst.an.pair
	sn.dotted, sn.dottedStale = inst.an.dotted.current(inst.dottedKey())
	sn.dottedPick = inst.an.row
	sn.strip, sn.grouped, sn.compare, sn.hasCompare = inst.strip, inst.grouped, inst.compare, inst.hasCompare
	sn.side, sn.treeExpanded = inst.side, inst.treeV.expanded
	if inst.hasSelected {
		sn.selected = inst.selected.Id
		sn.detail = inst.detail.current(inst.selected.Id)
	}
	return
}

func (inst opsSnap) ageMs() (ms int64) {
	ms = -1
	if inst.filled {
		ms = inst.taken.Sub(inst.refreshedAt).Milliseconds()
	}
	return
}

// cacheDiagnostic says why the cache may hold fewer rows than the marts.
func (inst opsSnap) cacheDiagnostic() (d string) {
	switch {
	case !inst.src.persisted():
		d = notPersistedReason(inst.src)
	case !inst.filled && inst.lastErr != nil:
		d = "the window could not read the trail marts: " + inst.lastErr.Error()
	case !inst.filled:
		d = "the window has not read the trail marts yet; ask again shortly"
	case inst.lastErr != nil:
		d = "the last refresh failed, so the rows are from the refresh before it: " + inst.lastErr.Error()
	}
	return
}

func (inst opsSnap) find(id uint64) (r *Row, ok bool) {
	for i := range inst.rows {
		if inst.rows[i].Id == id {
			return &inst.rows[i], true
		}
	}
	return
}

// TermArg is one facet value a filter includes or excludes.
type TermArg struct {
	Facet   string `desc:"kind, status, app, domain.action or principal"`
	Value   string `desc:"the value, as the facets list it"`
	Exclude bool   `json:",omitzero" desc:"remove rows with the value instead of keeping only them"`
}

// RangeArgs is a time window: last, or both from and to.
type RangeArgs struct {
	Last string    `json:",omitzero" desc:"a span ending now, as a Go duration (90s, 15m, 1h, 72h); empty when from and to are given"`
	From time.Time `json:",omitzero" desc:"the window's start, inclusive, when last is empty"`
	To   time.Time `json:",omitzero" desc:"the window's end, exclusive, when last is empty"`
}

// ListRowsArgs selects rows; the case and the window are required, as
// they are for the window itself.
type ListRowsArgs struct {
	Case   string    `json:",omitzero" desc:"run, window, app, conversation, task, subject, principal, entity, destination or all; the window's case when omitted"`
	Value  string    `json:",omitzero" desc:"the case's value; run:instance for window, type:value for entity, none for all"`
	Range  RangeArgs `json:",omitzero" desc:"the time window; the window's range when omitted"`
	Filter []TermArg `json:",omitzero" desc:"facet values to keep or remove; within a facet kept values are ORed, facets are ANDed; the window's filter when case is omitted too, else none"`
	Limit  int       `json:",omitzero" desc:"rows to return, newest first; default 50, at most 200"`
	Cursor string    `json:",omitzero" desc:"next_cursor from a previous call, to continue after its last row; empty starts at the newest"`
}

// RowSummary is one row as the table shows it.
type RowSummary struct {
	Id       uint64    `desc:"the row's facts id; pass to get_row, get_previous or select_row"`
	Ts       time.Time `desc:"when the event happened"`
	Kind     string    `desc:"the trail kind: llmCall, agentAction, auditEvent, httpFetch, …"`
	Headline string    `desc:"the one-line summary, cut at 512 characters"`
	Status   string    `desc:"the row's status or outcome"`
	Subject  string    `desc:"what the event is about: a purpose, app.operation, destination or domain.action"`
	App      string    `desc:"the app that wrote it"`
	Instance uint64    `desc:"the window that wrote it; 0 for none"`
	Run      string    `desc:"the writer run"`
	Verdict  string    `desc:"the writer run's evidence state: holds, broken, gap, fork, unanchored or unverified"`
}

// ListRowsResult is list_rows's result.
type ListRowsResult struct {
	Matched     int          `desc:"rows in the window's cache that match the arguments, after the cursor"`
	Rows        []RowSummary `desc:"the first limit matches, newest first"`
	NextCursor  string       `desc:"pass as cursor to continue; empty when no matched row follows"`
	Persisted   bool         `desc:"the trail reaches boxer.facts on this host; when false there is nothing to list"`
	Held        int          `desc:"rows the window's cache holds"`
	MoreInStore bool         `desc:"the window's last read was full, so older rows of its case and range are in boxer.facts beyond the cache"`
	RefreshedAt time.Time    `desc:"when the cache was last filled; zero before the first fill"`
	AgeMs       int64        `desc:"how old the cache was when this was read, in milliseconds; -1 before the first fill"`
	Diagnostic  string       `desc:"why the result may be short or empty: nothing persisted, not read yet, a failed refresh, or arguments wider than what the window read; empty when none applies"`
}

// GetRowArgs names one row.
type GetRowArgs struct {
	Id uint64 `desc:"a facts id from list_rows or get_view"`
}

// Attr is one attribute or reference of an audit event.
type Attr struct {
	Key   string `desc:"the attribute's key, or the reference's type"`
	Value string `desc:"its value"`
}

// RowDetail is get_row's result: what the detail pane shows.
type RowDetail struct {
	Row          RowSummary `desc:"the row as list_rows lists it"`
	Breadcrumb   string     `desc:"the identity chain: person › window › task › model call › tool call, absent links omitted"`
	Conversation string     `desc:"the conversation, on agentic rows"`
	Turn         string     `desc:"the turn within it"`
	Round        uint32     `desc:"the round within the turn"`
	Task         string     `desc:"the agent task"`
	TaskEpoch    uint64     `desc:"the task's grant epoch"`
	TaskCall     string     `desc:"the dispatcher's call that caused the row"`
	Ref          string     `desc:"the row's own identifier within its kind"`
	ParentRef    string     `desc:"what the row hangs off: the model call that asked for it"`
	Text         string     `desc:"the row's longer text where kept, cut at 4096 characters; empty when there is none"`
	Confined     bool       `desc:"the row is labelled confined (passed through, not enforced)"`
	Tainted      bool       `desc:"the row is labelled tainted (passed through, not enforced)"`
	TokensIn     uint64     `desc:"input tokens, on model calls"`
	TokensOut    uint64     `desc:"output tokens, on model calls"`
	ElapsedMs    uint64     `desc:"elapsed milliseconds where recorded"`
	// The audit event's own columns, on auditEvent rows.
	Domain      string `desc:"the audit event's domain"`
	Action      string `desc:"the audit event's action"`
	Principal   string `desc:"who caused it, a pseudonymous reference"`
	PrincipalBy string `desc:"how the principal was established"`
	Entity      string `desc:"the entity the event changed, type:value; empty when none"`
	From        string `desc:"the row's own claim of the state before, its from attribute"`
	FromBy      string `desc:"where the state before came from when no event, its from-by attribute"`
	Attrs       []Attr `desc:"the event's attributes, at most 64"`
	Refs        []Attr `desc:"the event's references, type and value"`
	// The review: the rules that raised the row and its latest mark.
	Exceptions []ExceptionRow `desc:"the exceptions the rules raised for the row, open or cleared by a mark"`
	Mark       string         `desc:"the row's latest review mark: reviewed, flagged or dismissed; empty when unmarked"`
	MarkReason string         `desc:"the mark's reason"`
}

// ExceptionRow is one exception: a row a rule raised.
type ExceptionRow struct {
	Id       uint64    `desc:"the raised row's facts id"`
	Ts       time.Time `desc:"when the row's event happened"`
	Kind     string    `desc:"the row's trail kind"`
	Headline string    `desc:"the row's one-line summary, cut at 512 characters"`
	Rule     string    `desc:"the rule that raised it; see rules"`
	Reason   string    `desc:"why, in one line"`
}

// RuleRow is one exception rule as the window shows it.
type RuleRow struct {
	Name        string `desc:"the rule's name"`
	Description string `desc:"what it raises"`
	Predicate   string `desc:"its SQL predicate over dm_trail_timeline, or in words what it tests over what the window holds"`
}

// GetExceptionsArgs selects the exceptions of a case over a range.
type GetExceptionsArgs struct {
	Case  string    `json:",omitzero" desc:"run, window, app, conversation, task, subject, principal, entity, destination or all; the window's case when omitted"`
	Value string    `json:",omitzero" desc:"the case's value; run:instance for window, type:value for entity, none for all"`
	Range RangeArgs `json:",omitzero" desc:"the time window; the window's range when omitted"`
	Rule  string    `json:",omitzero" desc:"one rule's name to keep only its exceptions; empty keeps every rule's"`
	Limit int       `json:",omitzero" desc:"exceptions to return, newest row first; default 50, at most 200"`
}

// ExceptionsResult is get_exceptions's result.
type ExceptionsResult struct {
	Open        int            `desc:"open exceptions that match, before the limit"`
	Exceptions  []ExceptionRow `desc:"the first limit open exceptions, newest row first; a row a review mark names has left the list"`
	Cleared     int            `desc:"exceptions of the window's rows a review mark has cleared"`
	Rules       []RuleRow      `desc:"every rule, with its predicate"`
	Persisted   bool           `desc:"the trail reaches boxer.facts on this host"`
	Held        int            `desc:"rows the window's cache holds; the rules ran over these"`
	MoreInStore bool           `desc:"older rows of the case and range are in boxer.facts beyond the cache, and the rules have not run over them"`
	RefreshedAt time.Time      `desc:"when the cache was last filled; zero before the first fill"`
	Diagnostic  string         `desc:"why the result may be short or empty: nothing persisted, not read yet, a failed refresh, or arguments wider than what the window read; empty when none applies"`
}

// GetPreviousArgs names one row.
type GetPreviousArgs struct {
	Id uint64 `desc:"the selected row's facts id"`
}

// PreviousResult is get_previous's result: SD5's diff and verdict.
type PreviousResult struct {
	Id         uint64      `desc:"the row asked for"`
	HasPrev    bool        `desc:"a previous event of the same entity (or grant of the same task) was found within the look-back"`
	PrevId     uint64      `desc:"its facts id"`
	PrevTs     time.Time   `desc:"its time"`
	Fields     []FieldDiff `desc:"the field table, this event's keys first"`
	Claim      string      `desc:"matches, differs, no-previous, gap-between, or no-claim when the row states no from"`
	Verdict    string      `desc:"the verdict in words"`
	From       string      `desc:"the row's from claim"`
	FromBy     string      `desc:"the row's from-by"`
	LookbackMs int64       `desc:"how far back the read looked, in milliseconds"`
}

// GetTreeArgs names a task or conversation case.
type GetTreeArgs struct {
	Case  string `json:",omitzero" desc:"task or conversation; the window's case when omitted"`
	Value string `json:",omitzero" desc:"the task's or the conversation's id"`
	Limit int    `json:",omitzero" desc:"nodes to return in depth-first order; default and at most 200"`
}

// TreeResult is get_tree's result.
type TreeResult struct {
	Nodes       int        `desc:"nodes in the tree, before the limit"`
	Roots       int        `desc:"nodes with no parent among the rows held"`
	Line        string     `desc:"the tree in words, as the tab states it"`
	Tree        []TreeNode `desc:"the first limit nodes in depth-first order, a parent before its children, siblings oldest first"`
	Persisted   bool       `desc:"the trail reaches boxer.facts on this host"`
	Held        int        `desc:"rows the window's cache holds; the tree is built over these"`
	MoreInStore bool       `desc:"older rows of the case are in boxer.facts beyond the cache, and the tree does not hold them"`
	RefreshedAt time.Time  `desc:"when the cache was last filled; zero before the first fill"`
	Diagnostic  string     `desc:"why the result may be short or empty: nothing persisted, not read yet, a failed refresh, or a case or filter other than the one the window read; empty when none applies"`
}

// GetDayHourArgs names the read the view is over: the case, the range
// and the filter, each exactly the one the window read, since the view
// aggregates that read whole.
type GetDayHourArgs struct {
	Case   string    `json:",omitzero" desc:"the case's kind, as the window read it; the window's case when omitted"`
	Value  string    `json:",omitzero" desc:"the case's value"`
	Range  RangeArgs `json:",omitzero" desc:"the window's range, as the window read it; the window's range when omitted"`
	Filter []TermArg `json:",omitzero" desc:"the filter the table is narrowed by, as the window read it; the window's filter when case is omitted too, else none"`
}

// DayHourCellOut is one (weekday, hour) with rows.
type DayHourCellOut struct {
	Weekday string `desc:"Mon to Sun"`
	Hour    int    `desc:"the hour of the day, 0 to 23, in the zone"`
	Rows    uint64 `desc:"rows in that weekday and hour over the window"`
}

// DayHourResult is get_dayhour's result.
type DayHourResult struct {
	Zone           string           `desc:"the IANA zone the weekdays and hours are in: the picker's, or UTC when it has none"`
	Rows           uint64           `desc:"rows counted"`
	Cells          []DayHourCellOut `desc:"the cells with rows, weekday then hour; at most 168"`
	BusiestWeekday string           `desc:"the weekday of the busiest cell"`
	BusiestHour    int              `desc:"the hour of the busiest cell"`
	BusiestRows    uint64           `desc:"the busiest cell's rows"`
	OffHoursShare  int              `desc:"the share of rows outside working hours, in whole percent"`
	WorkingHours   string           `desc:"the working hours the share is against"`
	Line           string           `desc:"the grid in words, as the tab states it"`
	Persisted      bool             `desc:"the trail reaches boxer.facts on this host"`
	RefreshedAt    time.Time        `desc:"when the table was last filled; the view is read against that fill"`
	Diagnostic     string           `desc:"why the result may be empty or behind: nothing persisted, the view not read yet, a read of an earlier fill, or a case, range or filter other than the one the window read; empty when none applies"`
}

// GetSeriesArgs names the read the series is over, as GetDayHourArgs
// does, and the measure and statistic.
type GetSeriesArgs struct {
	Case      string    `json:",omitzero" desc:"the case's kind, as the window read it; the window's case when omitted"`
	Value     string    `json:",omitzero" desc:"the case's value"`
	Range     RangeArgs `json:",omitzero" desc:"the window's range, as the window read it; the window's range when omitted"`
	Filter    []TermArg `json:",omitzero" desc:"the filter the table is narrowed by, as the window read it; the window's filter when case is omitted too, else none"`
	Measure   string    `json:",omitzero" desc:"rows, tokens-in, tokens-out or elapsed-ms; empty for the window's pick"`
	Statistic string    `json:",omitzero" desc:"sum, p50, p95 or max; empty for the window's pick; rows has sum only, the count"`
	Limit     int       `json:",omitzero" desc:"buckets to return, from the first; default and at most 200"`
}

// SeriesBucketOut is one bucket of the series.
type SeriesBucketOut struct {
	Start time.Time `desc:"the bucket's start"`
	Rows  uint64    `desc:"rows in the bucket carrying the measure"`
	Value float64   `desc:"the statistic over them"`
}

// SeriesResult is get_series' result.
type SeriesResult struct {
	Measure     string            `desc:"the measure read"`
	Statistic   string            `desc:"the statistic read"`
	StepMs      int64             `desc:"the bucket size, in milliseconds: the strip's"`
	Buckets     int               `desc:"buckets with rows, before the limit"`
	Series      []SeriesBucketOut `desc:"the first limit buckets, oldest first"`
	Rows        uint64            `desc:"rows carrying the measure over the window"`
	PeakAt      time.Time         `desc:"the start of the bucket with the largest value"`
	Peak        float64           `desc:"the largest value"`
	Total       float64           `desc:"the sum over buckets, for the sum statistic"`
	Cut         bool              `desc:"the read stopped at its bucket bound, so later buckets are missing"`
	Line        string            `desc:"the series in words, as the tab states it, times in UTC"`
	Persisted   bool              `desc:"the trail reaches boxer.facts on this host"`
	RefreshedAt time.Time         `desc:"when the table was last filled; the view is read against that fill"`
	Diagnostic  string            `desc:"why the result may be empty or behind: nothing persisted, the view not read yet, a read of an earlier fill, a pick other than the window's, or a case, range or filter other than the one the window read; empty when none applies"`
}

// SetSeriesArgs sets the series' measure and statistic.
type SetSeriesArgs struct {
	Measure   string `desc:"rows, tokens-in, tokens-out or elapsed-ms"`
	Statistic string `desc:"sum, p50, p95 or max; for rows, sum (the count)"`
}

// GetVerdictArgs names a run, or none for every run read.
type GetVerdictArgs struct {
	Run string `json:",omitzero" desc:"a writer run; empty returns every run the window has a line for"`
}

// VerdictLine is one run's verdict.
type VerdictLine struct {
	Run     string `desc:"the writer run"`
	State   string `desc:"holds, broken, gap, fork, unanchored or unverified"`
	Label   string `desc:"the line as the window shows it"`
	Law     string `desc:"the law it comes from: L34 the seal stream, L32 its anchors; empty when unverified"`
	Ordinal uint64 `desc:"the seal a break or an unanchored stretch starts at"`
	Count   int    `desc:"rows no seal names, for gap"`
}

// VerdictResult is get_verdict's result.
type VerdictResult struct {
	Lines  []VerdictLine `desc:"one line per writer run among the window's rows, at most 8"`
	ReadAt time.Time     `desc:"when the lines were read; zero before the first read"`
}

// View is get_view's result.
type View struct {
	Persisted     bool          `desc:"the trail reaches boxer.facts on this host"`
	Case          string        `desc:"the case's kind"`
	Value         string        `desc:"the case's value"`
	Range         string        `desc:"the window: last <span>, or from – to"`
	From          time.Time     `desc:"the window's start as last read"`
	To            time.Time     `desc:"the window's end as last read"`
	Live          bool          `desc:"the window is a span ending now, re-resolved on every read; false for a fixed window"`
	PreviousRange string        `desc:"the window Back returns to, as range reads it; empty when the history is empty"`
	ContextFrom   time.Time     `desc:"the context band's extent as last read: five times the window, clamped at now"`
	ContextTo     time.Time     `desc:"the context band's extent's end as last read"`
	Filter        []TermArg     `desc:"the filter the table is narrowed by"`
	Tab           string        `desc:"the window's tab: fields, previous, context, compare, tree, matrix, dotted, dayhour or series"`
	Series        string        `desc:"the series tab's measure and statistic"`
	Matrix        string        `desc:"the matrix tab's actor and object"`
	Dotted        string        `desc:"the dotted chart's row kind: run, window, conversation or entity"`
	StripBy       string        `desc:"what the strip's bars are coloured by: kind or status"`
	Grouping      bool          `desc:"the table is grouped by parent"`
	Compare       uint64        `desc:"the row the detail pane compares the selected one with, a facts id; 0 for none"`
	Side          string        `desc:"the pane beside the table: facets, exceptions, rules or views"`
	Tree          bool          `desc:"the tree tab's expansion as last set: every node open, or every node closed"`
	Selected      uint64        `desc:"the selected row's facts id; 0 for none"`
	Held          int           `desc:"rows the window holds"`
	MoreInStore   bool          `desc:"older rows of the case and range are in boxer.facts beyond the cache"`
	ReadState     string        `desc:"where the read of the trail marts stands: idle, reading, rows, empty, failed, refreshing or stopped; read_state has the transitions"`
	Strip         string        `desc:"the strip's line: rows, buckets, the busiest bucket and its time"`
	Verdicts      []VerdictLine `desc:"the verdict line per writer run"`
	Exceptions    int           `desc:"open exceptions among the rows held"`
	AsOf          string        `desc:"for the entity case, the entity's state as of the window's end in each domain it has events in, and the event each comes from"`
	RefreshedAt   time.Time     `desc:"when the cache was last filled; zero before the first fill"`
	SavedViews    []string      `desc:"the names of the saved views this window has read from the persist store"`
}

// SetCaseArgs sets the case whole.
type SetCaseArgs struct {
	Case  string `desc:"run, window, app, conversation, task, subject, principal, entity, destination or all"`
	Value string `desc:"the case's value; run:instance for window, type:value for entity, none for all"`
}

// SetFilterArgs sets the filter whole; an empty list clears it.
type SetFilterArgs struct {
	Terms []TermArg `desc:"facet values to keep or remove"`
}

// SelectRowArgs selects a row, which opens its detail.
type SelectRowArgs struct {
	Id uint64 `desc:"a facts id from list_rows; 0 clears the selection"`
}

// SetTabArgs sets the detail pane's tab.
type SetTabArgs struct {
	Tab string `desc:"fields, previous, context or compare (the detail pane under the table); tree (task or conversation case only), matrix, dotted, dayhour or series (an analytic view in the table's place)"`
}

func parseCaseArgs(kind, value string) (c Case, err error) {
	k, ok := ParseCaseKind(kind)
	if !ok {
		err = app.RefuseOperation("case is one of " + caseKindNameList())
		return
	}
	c = Case{Kind: k, Value: strings.TrimSpace(value)}
	err = c.validate()
	return
}

func parseRangeArgs(r RangeArgs) (w Window, err error) {
	if r.Last == "" && r.From.IsZero() && r.To.IsZero() {
		return defaultWindow, nil
	}
	if r.Last != "" {
		if !r.From.IsZero() || !r.To.IsZero() {
			err = app.RefuseOperation("a range is last, or from and to, not both")
			return
		}
		d, perr := time.ParseDuration(r.Last)
		if perr != nil || d <= 0 {
			err = app.RefuseOperation("last is a positive Go duration such as 15m or 72h")
			return
		}
		return Window{Last: d}, nil
	}
	w = Window{From: r.From.UTC(), To: r.To.UTC()}
	err = w.validate()
	return
}

// parseSeriesArgs reads a measure's and a statistic's names; an empty
// statistic is sum.
func parseSeriesArgs(measure, statistic string) (pick seriesPick, err error) {
	m, ok := ParseMeasure(measure)
	if !ok {
		err = app.RefuseOperation("measure is one of " + measureNameList())
		return
	}
	st := StatSum
	if statistic != "" {
		if st, ok = ParseStat(statistic); !ok {
			err = app.RefuseOperation("statistic is one of " + statNameList())
			return
		}
	}
	pick = seriesPick{Measure: m, Stat: st}
	err = pick.validate()
	return
}

func parseTerms(ts []TermArg) (f Filter, err error) {
	for _, t := range ts {
		facet, ok := ParseFacet(t.Facet)
		if !ok {
			err = app.RefuseOperation("a term's facet is one of kind, status, app, domain.action, principal")
			return
		}
		f.Terms = append(f.Terms, Term{Facet: facet, Value: t.Value, Exclude: t.Exclude})
	}
	f = f.normalized()
	return
}

func termArgs(f Filter) (out []TermArg) {
	for _, t := range f.normalized().Terms {
		out = append(out, TermArg{Facet: t.Facet.String(), Value: t.Value, Exclude: t.Exclude})
	}
	return
}

// cursorKey names a keyset position for a caller.
func cursorKey(r *Row) string {
	return strconv.FormatInt(r.TsNs, 10) + "-" + strconv.FormatUint(r.Id, 10)
}

func parseCursor(s string) (c Cursor, err error) {
	a, b, ok := strings.Cut(s, "-")
	if ok {
		c.TsNs, err = strconv.ParseInt(a, 10, 64)
		if err == nil {
			c.Id, err = strconv.ParseUint(b, 10, 64)
		}
	}
	if !ok || err != nil {
		err = app.RefuseOperation("cursor is a next_cursor from list_rows")
	}
	return
}

// after reports whether r comes after the cursor, newest first: strictly
// older in (ts, id).
func (inst Cursor) after(r *Row) bool {
	return r.TsNs < inst.TsNs || (r.TsNs == inst.TsNs && r.Id < inst.Id)
}

// facetValue is r's value for a facet, and whether the row has the facet.
func facetValue(r *Row, f FacetE) (v string, ok bool) {
	switch f {
	case FacetKind:
		return r.Kind, true
	case FacetStatus:
		return r.Status, true
	case FacetApp:
		return r.App, true
	case FacetDomainAction:
		if r.Kind == kindAuditEvent {
			return r.Subject, true
		}
	case FacetPrincipal:
		if r.Audit != nil {
			return r.Audit.Principal, true
		}
	}
	return
}

// matchesFilter is the filter as the probe applies it, over a held row.
func matchesFilter(r *Row, f Filter) bool {
	for _, facet := range AllFacets {
		incl, excl := f.split(facet)
		v, has := facetValue(r, facet)
		if len(incl) > 0 && (!has || !slices.Contains(incl, v)) {
			return false
		}
		if len(excl) > 0 && has && slices.Contains(excl, v) {
			return false
		}
	}
	return true
}

// filterCovers reports whether every row narrow keeps is a row wide keeps:
// per facet, narrow keeps no value wide drops.
func filterCovers(wide Filter, narrow Filter) bool {
	for _, facet := range AllFacets {
		wi, we := wide.split(facet)
		ni, ne := narrow.split(facet)
		if len(wi) > 0 {
			if len(ni) == 0 {
				return false
			}
			for _, v := range ni {
				if !slices.Contains(wi, v) {
					return false
				}
			}
		}
		for _, v := range we {
			if !slices.Contains(ne, v) && (len(ni) == 0 || slices.Contains(ni, v)) {
				return false
			}
		}
	}
	return true
}

// bounds resolves a question's window as the cache's read did: the
// window's own bounds when it is the cache's, otherwise w at the instant
// the cache's window was resolved — not at refreshedAt, which is when the
// read finished and lies past the cache's to.
func (inst opsSnap) bounds(w Window) (from, to time.Time) {
	if w == inst.scope.Window {
		return inst.from, inst.to
	}
	return w.Resolve(inst.resolvedAt)
}

// aggCovers says whether an aggregate view read for the window's scope is
// the view over c, w and f: the view aggregates the read whole, so each
// must be the one the window read.
func (inst opsSnap) aggCovers(c Case, w Window, f Filter) (ok bool, why string) {
	if ok, why = inst.readCovers(c, w); !ok {
		return
	}
	if f.key() != parseFilterKeyOrEmpty(inst.scope.Filter).key() {
		return false, "the filter differs from the one the window read; the view aggregates the filtered rows, so set_filter reads it"
	}
	return true, ""
}

// readCovers says whether c and w name exactly the case and range the
// window read; what a view over the read whole, whatever its filter, is
// keyed by.
func (inst opsSnap) readCovers(c Case, w Window) (ok bool, why string) {
	if c != inst.scope.Case {
		return false, "the case differs from the one the window read (" + inst.scope.Case.String() + "); set_case reads it"
	}
	if w != inst.scope.Window {
		from, to := inst.bounds(w)
		if !from.Equal(inst.from) || !to.Equal(inst.to) {
			return false, "the range differs from the one the window read; the view aggregates the whole range, so set_range reads it"
		}
	}
	return true, ""
}

// caseOr is the case kind and value name, the window's when they name none.
func (inst opsSnap) caseOr(kind, value string) (c Case, err error) {
	if kind == "" {
		if value != "" {
			err = app.RefuseOperation("value belongs to a case: pass case with it, or leave both out for the window's")
			return
		}
		return inst.c, nil
	}
	return parseCaseArgs(kind, value)
}

// scopeArgs reads a query's case, range and filter. A case or range left
// out is the window's, and so is the filter when the case is left out too:
// a query with no arguments reads what the window shows, and one that names
// its case filters by its own terms alone.
func (inst opsSnap) scopeArgs(kind, value string, r RangeArgs, terms []TermArg) (c Case, w Window, f Filter, err error) {
	if c, err = inst.caseOr(kind, value); err != nil {
		return
	}
	w = inst.window
	if r.Last != "" || !r.From.IsZero() || !r.To.IsZero() {
		if w, err = parseRangeArgs(r); err != nil {
			return
		}
	}
	if len(terms) == 0 && kind == "" {
		f = inst.filter
		return
	}
	f, err = parseTerms(terms)
	return
}

// covers says whether the cache answers a list over c, w and f in full.
func (inst opsSnap) covers(c Case, w Window, f Filter) (ok bool, why string) {
	if c != inst.scope.Case {
		return false, "the case differs from the one the window read (" + inst.scope.Case.String() + "); set_case reads it"
	}
	if w != inst.scope.Window {
		from, to := inst.bounds(w)
		if from.Before(inst.from) || to.After(inst.to) {
			return false, "the range reaches outside the one the window read; set_range reads it"
		}
	}
	if !filterCovers(parseFilterKeyOrEmpty(inst.scope.Filter), f) {
		return false, "the filter is wider than the one the window read; set_filter reads it"
	}
	return true, ""
}

func summarize(r *Row, lines []verdict.Line) (s RowSummary) {
	s = RowSummary{Id: r.Id, Ts: r.Ts(), Kind: r.Kind, Headline: appops.CutText(r.Headline, opsMaxHeadline), Status: r.Status,
		Subject: r.Subject, App: r.App, Instance: r.Instance, Run: r.Run, Verdict: verdictOf(lines, r.Run).State.String()}
	return
}

func verdictOf(lines []verdict.Line, run string) (l verdict.Line) {
	for _, x := range lines {
		if x.Run == run {
			return x
		}
	}
	return verdict.Line{Run: run, Reason: "not read yet"}
}

func exceptionRow(e Exception, r *Row) ExceptionRow {
	return ExceptionRow{Id: e.Id, Ts: r.Ts(), Kind: r.Kind, Headline: appops.CutText(r.Headline, opsMaxHeadline), Rule: e.Rule, Reason: e.Reason}
}

func ruleRows() (out []RuleRow) {
	for _, r := range Rules {
		out = append(out, RuleRow{Name: r.Name, Description: r.Description, Predicate: r.PredicateText()})
	}
	return
}

func detailOf(r *Row, lines []verdict.Line) (d RowDetail) {
	d = RowDetail{Row: summarize(r, lines), Breadcrumb: breadcrumb(r), Conversation: r.Conversation, Turn: r.Turn, Round: r.Round,
		Task: r.Task, TaskEpoch: r.TaskEpoch, TaskCall: r.TaskCall, Ref: r.Ref, ParentRef: r.ParentRef, Text: appops.CutText(r.Text, opsMaxText),
		Confined: r.Confined, Tainted: r.Tainted, TokensIn: r.TokensIn, TokensOut: r.TokensOut, ElapsedMs: r.ElapsedMs}
	if a := r.Audit; a != nil {
		d.Domain, d.Action, d.Principal, d.PrincipalBy, d.Entity, d.From, d.FromBy = a.Domain, a.Action, a.Principal, a.PrincipalBy, a.Entity, a.From, a.FromBy
		for i, k := range a.AttrKeys {
			if i == opsMaxAttrs || i >= len(a.AttrValues) {
				break
			}
			d.Attrs = append(d.Attrs, Attr{Key: k, Value: appops.CutText(a.AttrValues[i], opsMaxText)})
		}
		for i, k := range a.RefTypes {
			if i < len(a.RefValues) {
				d.Refs = append(d.Refs, Attr{Key: k, Value: a.RefValues[i]})
			}
		}
	}
	return
}

func verdictLine(l verdict.Line) VerdictLine {
	return VerdictLine{Run: l.Run, State: l.State.String(), Label: l.Label(), Law: l.Law(), Ordinal: l.Ordinal, Count: l.Count}
}

func persistedOr(sn opsSnap) (ok bool, reason string) {
	ok = sn.src.persisted()
	if !ok {
		reason = notPersistedReason(sn.src)
	}
	return
}

// parseFilterKey reads Filter.key back.
func parseFilterKey(s string) (f Filter, ok bool) {
	for s != "" {
		eq := strings.IndexByte(s, '=')
		if eq <= 0 {
			return
		}
		name, exclude := s[:eq], false
		if strings.HasSuffix(name, "!") {
			name, exclude = name[:len(name)-1], true
		}
		facet, fok := ParseFacet(name)
		if !fok {
			return
		}
		rest := s[eq+1:]
		q, err := strconv.QuotedPrefix(rest)
		if err != nil {
			return
		}
		v, _ := strconv.Unquote(q)
		f.Terms = append(f.Terms, Term{Facet: facet, Value: v, Exclude: exclude})
		rest = rest[len(q):]
		if !strings.HasPrefix(rest, ";") {
			return
		}
		s = rest[1:]
	}
	return f, true
}

func parseFilterKeyOrEmpty(s string) (f Filter) {
	f, _ = parseFilterKey(s)
	return
}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet((*App).opsSnapshot)

	s.Resource(resCase, "the case: a key kind and a value", func(inst *App) any { return inst.caseValue })
	s.Restorable(resCase, func(inst *App, v any) bool {
		c, ok := v.(Case)
		if ok {
			inst.applyCase(c)
		}
		return ok
	})
	s.Editing(resCase, func(inst *App) bool { return appops.WidgetEditing(inst.valueH) })
	s.Resource(resRange, "the time window the case is read over", func(inst *App) any { return inst.window })
	s.Restorable(resRange, func(inst *App, v any) bool {
		w, ok := v.(Window)
		if ok {
			inst.applyWindow(w)
		}
		return ok
	})
	s.Resource(resFilter, "the facet values the table is narrowed by", func(inst *App) any { return inst.filter.key() })
	s.Restorable(resFilter, func(inst *App, v any) bool {
		k, ok := v.(string)
		if !ok {
			return false
		}
		f, ok := parseFilterKey(k)
		if ok {
			inst.applyFilter(f)
		}
		return ok
	})
	s.Resource(resSelection, "the selected row's facts id, which opens the detail pane; 0 for none", func(inst *App) any { return inst.selectionId() })
	s.Restorable(resSelection, func(inst *App, v any) bool {
		id, ok := v.(uint64)
		if !ok {
			return false
		}
		return inst.selectById(id)
	})
	s.Resource(resTab, "the window's tab: a detail pane's tab under the table, or an analytic view in its place", func(inst *App) any { return inst.tab })
	s.Restorable(resTab, func(inst *App, v any) bool {
		t, ok := v.(TabE)
		if ok {
			inst.applyTab(t)
		}
		return ok
	})

	s.Resource(resSeries, "the series tab's measure and statistic", func(inst *App) any { return inst.an.series.pick })
	s.Restorable(resSeries, func(inst *App, v any) bool {
		p, ok := v.(seriesPick)
		if ok {
			inst.applySeries(p)
		}
		return ok
	})

	// The read lifecycle (ADR-0350 §SD12): the read_state resource and the
	// read_state and read_machine queries over the mirrored machine.
	fsmops.Mount(s, mountRead, "the window's read of the trail marts", func(inst *App) opfsm.SourceI {
		if inst.readFSM == nil {
			return nil
		}
		return inst.readFSM
	}, fsmops.Options{History: 16})

	s.Available(opGetRow, persistedOr)
	s.Available(opGetPrevious, persistedOr)
	s.Available(opSelectRow, persistedOr)
	s.Available(opMarkRow, persistedOr)

	// --- queries ---

	appops.Query(s, app.OperationSpec{Name: opListRows, Version: 1,
		Summary: "list the trail rows of a case — a run, window, app, conversation, task, subject, principal, entity or destination — over a time window, filtered by facet values, newest first, at most 200, from the window's cache of the trail marts",
		Reads:   readsRows, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in ListRowsArgs) (out ListRowsResult, err error) {
			c, w, f, err := sn.scopeArgs(in.Case, in.Value, in.Range, in.Filter)
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
			var cur *Cursor
			if in.Cursor != "" {
				cc, cerr := parseCursor(in.Cursor)
				if cerr != nil {
					err = cerr
					return
				}
				cur = &cc
			}
			out.Persisted = sn.src.persisted()
			out.Held, out.MoreInStore = len(sn.rows), sn.more
			out.RefreshedAt, out.AgeMs = sn.refreshedAt, sn.ageMs()
			out.Diagnostic = sn.cacheDiagnostic()
			if out.Diagnostic != "" || !sn.filled {
				return
			}
			if ok, why := sn.covers(c, w, f); !ok {
				out.Diagnostic = why
				return
			}
			from, to := sn.bounds(w)
			out.Rows = make([]RowSummary, 0, min(limit, len(sn.rows)))
			for i := range sn.rows {
				r := &sn.rows[i]
				ts := r.Ts()
				if ts.Before(from) || !ts.Before(to) || (cur != nil && !cur.after(r)) || !matchesFilter(r, f) {
					continue
				}
				out.Matched++
				if len(out.Rows) < limit {
					out.Rows = append(out.Rows, summarize(r, sn.lines))
					if len(out.Rows) == limit {
						out.NextCursor = cursorKey(r)
					}
				}
			}
			if out.Matched <= limit {
				out.NextCursor = ""
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetRow, Version: 1,
		Summary: "read one trail row in full: its context columns, the identity chain, its text, and for an audit event its attributes, references and from claim",
		Reads:   readsRows, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetRowArgs) (out RowDetail, err error) {
			r, ok := sn.find(in.Id)
			if !ok {
				err = app.RefuseOperation(rowGoneReason)
				return
			}
			out = detailOf(r, sn.lines)
			for _, e := range sn.exc.byRow[r.Id] {
				out.Exceptions = append(out.Exceptions, exceptionRow(e, r))
			}
			if m, ok := sn.marks[r.Id]; ok {
				out.Mark, out.MarkReason = m.Mark, m.Reason
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetPrevious, Version: 1,
		Summary: "read the selected row's previous state: the previous event of the same entity (or grant of the same task) within the look-back, the field table, and the verdict on the row's from claim",
		Reads:   readsRows, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetPreviousArgs) (out PreviousResult, err error) {
			if in.Id == 0 || in.Id != sn.selected {
				err = app.RefuseOperation("the previous-state read runs for the selected row; select_row it first")
				return
			}
			d := sn.detail
			switch {
			case d == nil:
				err = app.RefuseOperation("the selected row's previous state has not been read yet; ask again shortly")
				return
			case d.err != nil:
				err = app.RefuseOperation("the previous-state read failed: " + d.err.Error())
				return
			case d.previous == nil:
				err = app.RefuseOperation("the row has no previous state to read: it is neither an audit event naming an entity nor an agent grant of a task")
				return
			}
			p := d.previous
			out = PreviousResult{Id: in.Id, HasPrev: p.Has, PrevId: p.PrevId, PrevTs: p.PrevTs, Fields: p.Fields,
				Claim: p.Claim.String(), Verdict: p.Verdict, From: p.From, FromBy: p.FromBy, LookbackMs: p.Lookback.Milliseconds()}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetVerdict, Version: 1,
		Summary: "read the evidence verdict per writer run among the window's rows: holds, broken at an ordinal, a gap, a fork, or unanchored",
		Reads:   readsRead, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetVerdictArgs) (out VerdictResult, err error) {
			out.ReadAt = sn.linesAt
			for _, l := range sn.lines {
				if in.Run == "" || l.Run == in.Run {
					out.Lines = append(out.Lines, verdictLine(l))
				}
			}
			if in.Run != "" && len(out.Lines) == 0 {
				err = app.RefuseOperation("the window has no verdict line for that run: it reads one for each of the first 8 runs among its rows")
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetView, Version: 1,
		Summary: "read the window as the person sees it: the case, the range, the filter, the tab and every other control's value, the selection, the strip's line, the verdict lines and the saved views' names",
		Reads:   readsView, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out View, err error) {
			out = View{Persisted: sn.src.persisted(), Case: sn.c.Kind.String(), Value: sn.c.Value, Range: sn.window.String(),
				From: sn.from, To: sn.to, Filter: termArgs(sn.filter), Tab: sn.tab.String(), Series: sn.seriesPick.String(), Selected: sn.selected,
				Matrix: sn.matrixPick.String(), Dotted: sn.dottedPick.String(), StripBy: sn.strip.String(), Grouping: sn.grouped,
				Side: sn.side.String(), Tree: sn.treeExpanded, Held: len(sn.rows), MoreInStore: sn.more, ReadState: sn.readState, RefreshedAt: sn.refreshedAt,
				Live: sn.window.Last > 0, ContextFrom: sn.ctxFrom, ContextTo: sn.ctxTo}
			if sn.hasPrevious {
				out.PreviousRange = sn.previous.String()
			}
			if sn.hasCompare {
				out.Compare = sn.compare.Id
			}
			if sn.filled {
				out.Strip = stripLine(sn.buckets, sn.step)
			}
			out.Exceptions = len(sn.exc.open)
			if sn.c.Kind == CaseEntity && sn.scope.Case == sn.c {
				out.AsOf = asOfText(sn.asOf, func(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) })
			}
			for _, l := range sn.lines {
				out.Verdicts = append(out.Verdicts, verdictLine(l))
			}
			out.SavedViews = sn.views
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetTree, Version: 1,
		Summary: "read the chain of a task or conversation case as a tree over parent-ref, in depth-first order: each node's row id, depth, parent, kind, headline, status and elapsed time, from the window's cache",
		Reads:   readsTree, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetTreeArgs) (out TreeResult, err error) {
			c, err := sn.caseOr(in.Case, in.Value)
			if err != nil {
				return
			}
			if !c.Kind.chained() {
				err = app.RefuseOperation("the tree is read for a task or a conversation case")
				return
			}
			limit := in.Limit
			switch {
			case limit < 0:
				err = app.RefuseOperation("limit is not negative")
				return
			case limit == 0:
				limit = opsMaxRows
			case limit > opsMaxRows:
				limit = opsMaxRows
			}
			out.Persisted, out.Held, out.MoreInStore, out.RefreshedAt = sn.src.persisted(), len(sn.rows), sn.more, sn.refreshedAt
			out.Diagnostic = sn.cacheDiagnostic()
			if out.Diagnostic != "" || !sn.filled {
				return
			}
			// The tree is built over every row the window read for the
			// case, whatever its filter kept, so the window's filter must
			// be empty for the answer to be the case's chain.
			if ok, why := sn.covers(c, sn.scope.Window, Filter{}); !ok {
				out.Diagnostic = why
				return
			}
			tr := buildTree(sn.rows)
			nodes, terr := tr.depthFirst()
			if terr != nil {
				err = app.RefuseOperation("the tree does not build: " + terr.Error())
				return
			}
			out.Nodes, out.Roots, out.Line = len(nodes), tr.roots, tr.line()
			out.Tree = nodes[:min(len(nodes), limit)]
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetDayHour, Version: 1,
		Summary: "read the day × hour view of a case over a range and filter — rows per weekday and hour in the picker's zone, the busiest cell and the share outside working hours — as the window last read it; the view is read while its tab is open",
		Reads:   readsScope, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetDayHourArgs) (out DayHourResult, err error) {
			c, w, f, err := sn.scopeArgs(in.Case, in.Value, in.Range, in.Filter)
			if err != nil {
				return
			}
			out.Persisted, out.RefreshedAt = sn.src.persisted(), sn.refreshedAt
			out.WorkingHours = fmt.Sprintf("Mon–Fri %02d:00–%02d:00", workStartHour, workEndHour)
			out.Diagnostic = sn.cacheDiagnostic()
			if out.Diagnostic != "" || !sn.filled {
				return
			}
			if ok, why := sn.aggCovers(c, w, f); !ok {
				out.Diagnostic = why
				return
			}
			d := sn.dayhour
			if d == nil {
				out.Diagnostic = "the day × hour view has not been read; set_tab dayhour opens it, and it reads while open"
				return
			}
			if sn.dayhourStale {
				out.Diagnostic = "the view is of the fill or zone before the current one and is being re-read; ask again shortly"
			}
			out.Zone, out.Rows, out.Line, out.OffHoursShare = d.zone, d.rows, d.line(), d.offShare()
			if d.rows > 0 {
				out.BusiestWeekday, out.BusiestHour, out.BusiestRows = weekdayNames[d.busiestD], d.busiestH, d.maxN
			}
			for i := range d.counts {
				for h, n := range d.counts[i] {
					if n > 0 {
						out.Cells = append(out.Cells, DayHourCellOut{Weekday: weekdayNames[i], Hour: h, Rows: n})
					}
				}
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetSeries, Version: 1,
		Summary: "read the numeric series of a case over a range and filter — a measure (rows, tokens-in, tokens-out, elapsed-ms) under a statistic (sum, p50, p95, max) per bucket of the strip's step — as the window last read it; the view is read while its tab is open, for the measure and statistic set_series sets",
		Reads:   readsSeries, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetSeriesArgs) (out SeriesResult, err error) {
			c, w, f, err := sn.scopeArgs(in.Case, in.Value, in.Range, in.Filter)
			if err != nil {
				return
			}
			pick := sn.seriesPick
			if in.Measure != "" || in.Statistic != "" {
				if pick, err = parseSeriesArgs(in.Measure, in.Statistic); err != nil {
					return
				}
			}
			limit := in.Limit
			switch {
			case limit < 0:
				err = app.RefuseOperation("limit is not negative")
				return
			case limit == 0 || limit > opsMaxRows:
				limit = opsMaxRows
			}
			out.Measure, out.Statistic = pick.Measure.String(), pick.Stat.String()
			out.Persisted, out.RefreshedAt = sn.src.persisted(), sn.refreshedAt
			out.Diagnostic = sn.cacheDiagnostic()
			if out.Diagnostic != "" || !sn.filled {
				return
			}
			if ok, why := sn.aggCovers(c, w, f); !ok {
				out.Diagnostic = why
				return
			}
			d := sn.series
			if d == nil {
				out.Diagnostic = "the series has not been read; set_tab series opens it, and it reads while open"
				return
			}
			if d.pick != pick {
				out.Diagnostic = "the window's series is " + d.pick.String() + "; set_series reads " + pick.String()
				return
			}
			if sn.seriesStale {
				out.Diagnostic = "the series is of the fill or pick before the current one and is being re-read; ask again shortly"
			}
			out.StepMs, out.Buckets, out.Rows, out.Cut, out.Line = d.step.Milliseconds(), len(d.points), d.rows, d.cut, d.line()
			if d.hasPeak {
				out.PeakAt, out.Peak = time.Unix(d.peakT, 0).UTC(), d.peakV
			}
			if pick.Stat == StatSum {
				out.Total = d.total
			}
			for _, p := range d.points[:min(len(d.points), limit)] {
				out.Series = append(out.Series, SeriesBucketOut{Start: time.Unix(p.Bucket, 0).UTC(), Rows: p.N, Value: p.V})
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetExc, Version: 1,
		Summary: "list the exceptions the window's named rules raise among the rows of a case over a time window, each with its rule and reason, from the window's cache; a row a review mark names has left the list",
		Reads:   readsRows, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetExceptionsArgs) (out ExceptionsResult, err error) {
			c, w, _, err := sn.scopeArgs(in.Case, in.Value, in.Range, nil)
			if err != nil {
				return
			}
			if in.Rule != "" {
				if _, ok := LookupRule(in.Rule); !ok {
					err = app.RefuseOperation("rule is one of the names get_exceptions lists under rules")
					return
				}
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
			out.Rules = ruleRows()
			out.Persisted = sn.src.persisted()
			out.Held, out.MoreInStore = len(sn.rows), sn.more
			out.RefreshedAt = sn.refreshedAt
			out.Diagnostic = sn.cacheDiagnostic()
			if out.Diagnostic != "" || !sn.filled {
				return
			}
			// The rules ran over the rows the window's filter kept, so a
			// window with a filter covers no unfiltered question.
			if ok, why := sn.covers(c, w, Filter{}); !ok {
				out.Diagnostic = why
				return
			}
			from, to := sn.bounds(w)
			byId := make(map[uint64]*Row, len(sn.rows))
			for i := range sn.rows {
				byId[sn.rows[i].Id] = &sn.rows[i]
			}
			out.Cleared = sn.exc.cleared
			for _, e := range sn.exc.open {
				r, ok := byId[e.Id]
				if !ok || (in.Rule != "" && e.Rule != in.Rule) {
					continue
				}
				if ts := r.Ts(); ts.Before(from) || !ts.Before(to) {
					continue
				}
				out.Open++
				if len(out.Exceptions) < limit {
					out.Exceptions = append(out.Exceptions, exceptionRow(e, r))
				}
			}
			return
		})

	// --- commands ---

	appops.Command(s, app.OperationSpec{Name: opSetCase, Version: 1,
		Summary: "set the case whole — a key kind and a value; the window re-reads the trail marts under it",
		Effect:  app.OperationEffectView, Writes: []string{resCase}, Agents: true,
		Gesture: "the case combo and value box with Apply", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetCaseArgs) (out appops.None, err error) {
			c, err := parseCaseArgs(in.Case, in.Value)
			if err != nil {
				return
			}
			inst.applyCase(c)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetRange, Version: 1,
		Summary: "set the time window: a span ending now, or from and to; the window re-reads the trail marts under it, and the window it replaces goes on the history Back returns to",
		Effect:  app.OperationEffectView, Writes: []string{resRange}, Agents: true,
		Gesture: "the time range picker, the context band's range, the detail strip's brush, the navigation row's Back, Earlier, Later, Zoom out, Zoom in, Fit and Live", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in RangeArgs) (out appops.None, err error) {
			if in.Last == "" && in.From.IsZero() && in.To.IsZero() {
				err = app.RefuseOperation("a range is last, or from and to")
				return
			}
			w, err := parseRangeArgs(in)
			if err != nil {
				return
			}
			inst.applyWindow(w)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetFilter, Version: 1,
		Summary: "set the filter whole: the facet values the table keeps or removes; an empty list clears it",
		Effect:  app.OperationEffectView, Writes: []string{resFilter}, Agents: true,
		Gesture: "a click on a facet value, a modified click, Clear filter", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetFilterArgs) (out appops.None, err error) {
			f, err := parseTerms(in.Terms)
			if err != nil {
				return
			}
			inst.applyFilter(f)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSelectRow, Version: 1,
		Summary: "select a row by facts id, which opens its detail pane and reads its context and previous state; 0 clears the selection",
		Effect:  app.OperationEffectView, Writes: []string{resSelection}, Agents: true,
		Gesture: "a row click, a context row click, the Clear selection button, a settled position on the context band, a click on a detail strip cell", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SelectRowArgs) (out appops.None, err error) {
			if in.Id == 0 {
				inst.clearSelection()
				return
			}
			if src := sourceRef.Load(); !src.persisted() {
				err = app.RefuseOperation(notPersistedReason(src))
				return
			}
			if !inst.selectById(in.Id) {
				err = app.RefuseOperation(rowGoneReason)
			}
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetTab, Version: 1,
		Summary: "set the window's tab: fields, previous, context or compare under the table; tree (for a task or conversation case), matrix, dotted, dayhour or series in its place",
		Effect:  app.OperationEffectView, Writes: []string{resTab}, Agents: true,
		Gesture: "the detail pane's tab buttons", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetTabArgs) (out appops.None, err error) {
			t, ok := ParseTab(in.Tab)
			if !ok {
				err = app.RefuseOperation("tab is one of " + tabNameList())
				return
			}
			if !t.availableFor(inst.caseValue.Kind) {
				err = app.RefuseOperation("the tree tab is drawn for a task or a conversation case only; set_case one first")
				return
			}
			inst.applyTab(t)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetSeries, Version: 1,
		Summary: "set the series tab's measure (rows, tokens-in, tokens-out, elapsed-ms) and statistic (sum, p50, p95, max); the series is re-read for them while its tab is open",
		Effect:  app.OperationEffectView, Writes: []string{resSeries}, Agents: true,
		Gesture: "the series tab's measure and statistic combos", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetSeriesArgs) (out appops.None, err error) {
			pick, err := parseSeriesArgs(in.Measure, in.Statistic)
			if err != nil {
				return
			}
			inst.applySeries(pick)
			return
		})

	addControlOps(s)

	// The one command that writes the trail (ADR-0350 §SD7): consequential,
	// and not offered to agents, since review is the person's act.
	appops.Command(s, app.OperationSpec{Name: opMarkRow, Version: 1,
		Summary: "mark a trail row reviewed, flagged or dismissed with a reason, written to the trail as a review-domain audit event under the person's principal; a mark clears the row's exceptions",
		Effect:  app.OperationEffectConsequential, Agents: false,
		Gesture: "the detail pane's reviewed, flagged and dismissed buttons"},
		func(inst *App, call app.OperationCall, in MarkRowArgs) (out MarkRowResult, err error) {
			return inst.markRow(call, in)
		})

	return
}()

// Operations serves the catalog for this window (app.OperationsAppI).
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

// gestureOr routes the person's control through the catalog (ADR-0345
// §SD3) and applies it directly where no host serves the catalog.
func gestureOr[In any](inst *App, op string, in In, direct func()) {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[In, appops.None](inst.frameCtx, op, in); err == nil {
			return
		}
	}
	direct()
}

func (inst *App) setCaseGesture(c Case) {
	gestureOr(inst, opSetCase, SetCaseArgs{Case: c.Kind.String(), Value: c.Value}, func() { inst.applyCase(c) })
}

func (inst *App) setWindowGesture(w Window) {
	in := RangeArgs{From: w.From, To: w.To}
	if w.Last > 0 {
		in = RangeArgs{Last: w.Last.String()}
	}
	gestureOr(inst, opSetRange, in, func() { inst.applyWindow(w) })
}

func (inst *App) setFilterGesture(f Filter) {
	gestureOr(inst, opSetFilter, SetFilterArgs{Terms: termArgs(f)}, func() { inst.applyFilter(f) })
}

func (inst *App) selectGesture(id uint64) {
	gestureOr(inst, opSelectRow, SelectRowArgs{Id: id}, func() {
		if id == 0 {
			inst.clearSelection()
		} else {
			inst.selectById(id)
		}
	})
}

func (inst *App) setTabGesture(t TabE) {
	gestureOr(inst, opSetTab, SetTabArgs{Tab: t.String()}, func() { inst.applyTab(t) })
}
