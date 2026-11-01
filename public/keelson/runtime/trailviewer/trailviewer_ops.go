package trailviewer

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/trailviewer/verdict"
)

// The operations catalog (ADR-0350 §SD8, proposed; the M2 subset —
// mark_row and get_exceptions are M3's, get_tree M4's). Queries read the
// window's caches as the snapshot took them with the frame, never the
// store, so a query does not wait on ClickHouse; each says when the cache
// was filled. They select by their own arguments, never by the window's
// case, range or filter (ADR-0345 §SD2), and say when those arguments
// reach outside what the window read. Commands set the view and never
// toggle; nothing here writes the trail.

const (
	resCase      = "case"
	resRange     = "range"
	resFilter    = "filter"
	resSelection = "selection"
	resTab       = "tab"

	opListRows    = "list_rows"
	opGetRow      = "get_row"
	opGetPrevious = "get_previous"
	opGetVerdict  = "get_verdict"
	opGetView     = "get_view"

	opSetCase   = "set_case"
	opSetRange  = "set_range"
	opSetFilter = "set_filter"
	opSelectRow = "select_row"
	opSetTab    = "set_tab"
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

// opsSnap is what queries read: the caches and the view as the frame left
// them. The slices are the caches' own, which a fill replaces whole.
type opsSnap struct {
	src         *Source
	rows        []Row
	more        bool
	scope       scope
	from, to    time.Time
	buckets     []Bucket
	step        time.Duration
	facets      [][]FacetValue
	filled      bool
	refreshedAt time.Time
	lastErr     error
	taken       time.Time
	lines       []verdict.Line
	linesAt     time.Time
	detail      *detailFill

	c        Case
	window   Window
	filter   Filter
	tab      TabE
	selected uint64
}

func (inst *App) opsSnapshot() (sn opsSnap) {
	sn.src = sourceRef.Load()
	ch := &inst.cache
	sn.rows, sn.more, sn.scope, sn.from, sn.to = ch.rows, ch.more, ch.scope, ch.from, ch.to
	sn.buckets, sn.step, sn.facets = ch.buckets, ch.step, ch.facets
	sn.filled, sn.refreshedAt, sn.lastErr = ch.filled, ch.refreshedAt, ch.lastErr
	sn.lines, sn.linesAt = inst.verdicts.lines, inst.verdicts.at
	sn.taken = time.Now()
	sn.c, sn.window, sn.filter, sn.tab = inst.caseValue, inst.window, inst.filter, inst.tab
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
	Exclude bool   `desc:"remove rows with the value instead of keeping only them"`
}

// RangeArgs is a time window: last, or both from and to.
type RangeArgs struct {
	Last string    `desc:"a span ending now, as a Go duration (90s, 15m, 1h, 72h); empty when from and to are given"`
	From time.Time `desc:"the window's start, inclusive, when last is empty"`
	To   time.Time `desc:"the window's end, exclusive, when last is empty"`
}

// ListRowsArgs selects rows; the case and the window are required, as
// they are for the window itself.
type ListRowsArgs struct {
	Case   string    `desc:"run, window, app, conversation, task, subject, principal, entity, destination or all"`
	Value  string    `desc:"the case's value; run:instance for window, type:value for entity, none for all"`
	Range  RangeArgs `desc:"the time window; empty is the last hour"`
	Filter []TermArg `desc:"facet values to keep or remove; within a facet kept values are ORed, facets are ANDed"`
	Limit  int       `desc:"rows to return, newest first; default 50, at most 200"`
	Cursor string    `desc:"next_cursor from a previous call, to continue after its last row; empty starts at the newest"`
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

// GetVerdictArgs names a run, or none for every run read.
type GetVerdictArgs struct {
	Run string `desc:"a writer run; empty returns every run the window has a line for"`
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
	Persisted   bool          `desc:"the trail reaches boxer.facts on this host"`
	Case        string        `desc:"the case's kind"`
	Value       string        `desc:"the case's value"`
	Range       string        `desc:"the window: last <span>, or from – to"`
	From        time.Time     `desc:"the window's start as last read"`
	To          time.Time     `desc:"the window's end as last read"`
	Filter      []TermArg     `desc:"the filter the table is narrowed by"`
	Tab         string        `desc:"the detail pane's tab: fields, previous or context"`
	Selected    uint64        `desc:"the selected row's facts id; 0 for none"`
	Held        int           `desc:"rows the window holds"`
	MoreInStore bool          `desc:"older rows of the case and range are in boxer.facts beyond the cache"`
	Strip       string        `desc:"the strip's line: rows, buckets, the busiest bucket and its time"`
	Verdicts    []VerdictLine `desc:"the verdict line per writer run"`
	RefreshedAt time.Time     `desc:"when the cache was last filled; zero before the first fill"`
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
	Tab string `desc:"fields, previous or context"`
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

// covers says whether the cache answers a list over c, w and f in full.
func (inst opsSnap) covers(c Case, w Window, f Filter) (ok bool, why string) {
	if c != inst.scope.Case {
		return false, "the case differs from the one the window read (" + inst.scope.Case.String() + "); set_case reads it"
	}
	if w != inst.scope.Window {
		from, to := w.Resolve(inst.refreshedAt)
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
	s = RowSummary{Id: r.Id, Ts: r.Ts(), Kind: r.Kind, Headline: cutText(r.Headline, opsMaxHeadline), Status: r.Status,
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

func detailOf(r *Row, lines []verdict.Line) (d RowDetail) {
	d = RowDetail{Row: summarize(r, lines), Breadcrumb: breadcrumb(r), Conversation: r.Conversation, Turn: r.Turn, Round: r.Round,
		Task: r.Task, TaskEpoch: r.TaskEpoch, TaskCall: r.TaskCall, Ref: r.Ref, ParentRef: r.ParentRef, Text: cutText(r.Text, opsMaxText),
		Confined: r.Confined, Tainted: r.Tainted, TokensIn: r.TokensIn, TokensOut: r.TokensOut, ElapsedMs: r.ElapsedMs}
	if a := r.Audit; a != nil {
		d.Domain, d.Action, d.Principal, d.PrincipalBy, d.Entity, d.From, d.FromBy = a.Domain, a.Action, a.Principal, a.PrincipalBy, a.Entity, a.From, a.FromBy
		for i, k := range a.AttrKeys {
			if i == opsMaxAttrs || i >= len(a.AttrValues) {
				break
			}
			d.Attrs = append(d.Attrs, Attr{Key: k, Value: cutText(a.AttrValues[i], opsMaxText)})
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
	s.Resource(resTab, "the detail pane's tab", func(inst *App) any { return inst.tab })
	s.Restorable(resTab, func(inst *App, v any) bool {
		t, ok := v.(TabE)
		if ok {
			inst.tab = t
		}
		return ok
	})

	s.Available(opGetRow, persistedOr)
	s.Available(opGetPrevious, persistedOr)
	s.Available(opSelectRow, persistedOr)

	// --- queries ---

	appops.Query(s, app.OperationSpec{Name: opListRows, Version: 1,
		Summary: "list the trail rows of a case — a run, window, app, conversation, task, subject, principal, entity or destination — over a time window, filtered by facet values, newest first, at most 200, from the window's cache of the trail marts",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in ListRowsArgs) (out ListRowsResult, err error) {
			c, err := parseCaseArgs(in.Case, in.Value)
			if err != nil {
				return
			}
			w, err := parseRangeArgs(in.Range)
			if err != nil {
				return
			}
			f, err := parseTerms(in.Filter)
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
			from, to := w.Resolve(sn.refreshedAt)
			if w == sn.scope.Window {
				from, to = sn.from, sn.to
			}
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
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetRowArgs) (out RowDetail, err error) {
			r, ok := sn.find(in.Id)
			if !ok {
				err = app.RefuseOperation(rowGoneReason)
				return
			}
			out = detailOf(r, sn.lines)
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetPrevious, Version: 1,
		Summary: "read the selected row's previous state: the previous event of the same entity (or grant of the same task) within the look-back, the field table, and the verdict on the row's from claim",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
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
		Agents:  true, Fork: app.OperationForkReadShared},
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
		Summary: "read the case, the range, the filter, the tab, the selection, the strip's line and the verdict lines as the person sees them",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out View, err error) {
			out = View{Persisted: sn.src.persisted(), Case: sn.c.Kind.String(), Value: sn.c.Value, Range: sn.window.String(),
				From: sn.from, To: sn.to, Filter: termArgs(sn.filter), Tab: sn.tab.String(), Selected: sn.selected,
				Held: len(sn.rows), MoreInStore: sn.more, RefreshedAt: sn.refreshedAt}
			if sn.filled {
				out.Strip = stripLine(sn.buckets, sn.step)
			}
			for _, l := range sn.lines {
				out.Verdicts = append(out.Verdicts, verdictLine(l))
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
		Summary: "set the time window: a span ending now, or from and to; the window re-reads the trail marts under it",
		Effect:  app.OperationEffectView, Writes: []string{resRange}, Agents: true,
		Gesture: "the time range picker, a click on a strip bar", Rebuild: app.OperationRebuildVolatile},
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
		Gesture: "a row click, a context row click, the Clear selection button", Rebuild: app.OperationRebuildVolatile},
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
		Summary: "set the detail pane's tab: fields, previous or context",
		Effect:  app.OperationEffectView, Writes: []string{resTab}, Agents: true,
		Gesture: "the detail pane's tab buttons", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetTabArgs) (out appops.None, err error) {
			t, ok := ParseTab(in.Tab)
			if !ok {
				err = app.RefuseOperation("tab is one of fields, previous, context")
				return
			}
			inst.tab = t
			return
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
	gestureOr(inst, opSetTab, SetTabArgs{Tab: t.String()}, func() { inst.tab = t })
}
