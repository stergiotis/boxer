package trailviewer

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// The rest of the catalog (ADR-0350 §SD8, proposed): the queries behind the
// panes and the two M4 views, the commands behind every control the person
// has that trailviewer_ops.go does not already serve, the saved views and
// the export. The same rules hold: a query selects by its own arguments and
// answers from the snapshot's cache, bounded, Untrusted where row-derived
// text is returned; a command sets and never toggles, and the control
// routes through it (gestureOr).

// Result bounds for the pane queries.
const (
	opsMaxContext = 20
)

// readValue is the read resource's value: when the cache was last filled
// and what the last read reported, comparable so the host can tell one
// fill from the next. The cache adopts a fill inside the frame (tick), so
// the change is the app's.
type readValue struct {
	Refreshed time.Time
	Error     string
}

func (inst *App) readValueOf() (v readValue) {
	v.Refreshed = inst.cache.refreshedAt
	if inst.cache.lastErr != nil {
		v.Error = inst.cache.lastErr.Error()
	}
	return
}

// GetFacetsArgs names a facet of the read the pane counts over: the case
// and the range, each exactly the one the window read. The facets count
// the case and range whole, whatever filter the table is narrowed by, as
// the pane shows them.
type GetFacetsArgs struct {
	Case  string    `desc:"the case's kind, as the window read it"`
	Value string    `desc:"the case's value"`
	Range RangeArgs `desc:"the window's range, as the window read it"`
	Facet string    `desc:"kind, status, app, domain.action or principal"`
	Limit int       `desc:"values to return, most rows first; default and at most 200"`
}

// FacetValueOut is one value of a facet with its count.
type FacetValueOut struct {
	Value string `desc:"the value, as set_filter names it; empty is the column's empty value"`
	Rows  uint64 `desc:"rows in the case and range with the value"`
}

// FacetsResult is get_facets's result.
type FacetsResult struct {
	Facet       string          `desc:"the facet read"`
	Count       int             `desc:"distinct values in the case and range, before the limit"`
	Values      []FacetValueOut `desc:"the first limit values, most rows first"`
	Cut         bool            `desc:"the values stop at the limit"`
	Persisted   bool            `desc:"the trail reaches boxer.facts on this host"`
	RefreshedAt time.Time       `desc:"when the table was last filled; the facets were read with it"`
	Diagnostic  string          `desc:"why the result may be empty: nothing persisted, not read yet, a failed refresh, or a case or range other than the one the window read; empty when none applies"`
}

// GetStripArgs names the read the strip is over, as GetDayHourArgs does,
// and the colouring.
type GetStripArgs struct {
	Case   string    `desc:"the case's kind, as the window read it"`
	Value  string    `desc:"the case's value"`
	Range  RangeArgs `desc:"the window's range, as the window read it"`
	Filter []TermArg `desc:"the filter the table is narrowed by, as the window read it; empty for none"`
	By     string    `desc:"kind or status: what the bars are stacked by; empty for the window's colouring"`
	Limit  int       `desc:"bars to return, oldest bucket first; default and at most 200"`
}

// StripBar is one (bucket, key) of the strip.
type StripBar struct {
	Start time.Time `desc:"the bucket's start"`
	Key   string    `desc:"the kind or status the bar is for"`
	Rows  uint64    `desc:"rows of the key in the bucket"`
}

// StripResult is get_strip's result.
type StripResult struct {
	By          string     `desc:"what the bars are stacked by: kind or status"`
	StepMs      int64      `desc:"the bucket size, in milliseconds"`
	Rows        uint64     `desc:"rows over the strip"`
	Buckets     int        `desc:"buckets with rows"`
	Bars        []StripBar `desc:"the first limit (bucket, key) bars, oldest bucket first"`
	Cut         bool       `desc:"the bars stop at the limit"`
	Line        string     `desc:"the strip in words, as the window states it, times in UTC"`
	Persisted   bool       `desc:"the trail reaches boxer.facts on this host"`
	RefreshedAt time.Time  `desc:"when the table was last filled; the strip was read with it"`
	Diagnostic  string     `desc:"why the result may be empty: nothing persisted, not read yet, a failed refresh, a colouring other than the window's, or a case, range or filter other than the one the window read; empty when none applies"`
}

// GetContextArgs names the selected row.
type GetContextArgs struct {
	Id uint64 `desc:"the selected row's facts id"`
	N  int    `desc:"rows to return on each side, nearest first read; default and at most 20, and never more than the window read"`
}

// ContextResult is get_context's result: the rows around the selected one
// in its writer run, as the context tab shows them.
type ContextResult struct {
	Id     uint64       `desc:"the row asked for"`
	Run    string       `desc:"the writer run the rows are from"`
	Before []RowSummary `desc:"the rows before it, oldest first; the filter does not apply"`
	After  []RowSummary `desc:"the rows after it, oldest first"`
	Cut    bool         `desc:"a side stopped at n"`
}

// GetCompareArgs names two held rows.
type GetCompareArgs struct {
	A uint64 `desc:"one row's facts id"`
	B uint64 `desc:"the other's"`
}

// CompareResult is get_compare's result: the field table of two rows, the
// older on the before side.
type CompareResult struct {
	OlderId   uint64      `desc:"the older row's facts id; its values are the before column"`
	NewerId   uint64      `desc:"the newer row's facts id; its values are the after column"`
	OlderTs   time.Time   `desc:"the older row's time"`
	NewerTs   time.Time   `desc:"the newer row's time"`
	BothAudit bool        `desc:"both rows are audit events, so their own columns and attributes are compared too"`
	Base      string      `desc:"which row is which, in words, as the compare tab states it"`
	Fields    []FieldDiff `desc:"the field table: the newer row's change against the older"`
}

// GetMatrixArgs names the read the matrix is over, as GetDayHourArgs does,
// and the two axes.
type GetMatrixArgs struct {
	Case   string    `desc:"the case's kind, as the window read it"`
	Value  string    `desc:"the case's value"`
	Range  RangeArgs `desc:"the window's range, as the window read it"`
	Filter []TermArg `desc:"the filter the table is narrowed by, as the window read it; empty for none"`
	Actor  string    `desc:"principal, app or window; empty for the window's pick"`
	Object string    `desc:"subject, entity or operation; empty for the window's pick"`
	Limit  int       `desc:"cells to return, actors in order of their rows then objects; default and at most 200"`
}

// MatrixCellOut is one (actor, object) pair with rows.
type MatrixCellOut struct {
	Actor       string `desc:"the actor"`
	Object      string `desc:"the object"`
	Rows        uint64 `desc:"rows of the pair"`
	LastOutcome string `desc:"the outcome or status of the pair's latest row"`
}

// MatrixResult is get_matrix's result.
type MatrixResult struct {
	Actor         string          `desc:"the actor axis read"`
	Object        string          `desc:"the object axis read"`
	Actors        int             `desc:"distinct actors over the read"`
	Objects       int             `desc:"distinct objects over the read"`
	Rows          uint64          `desc:"rows counted"`
	Pairs         int             `desc:"(actor, object) pairs with rows"`
	Cells         []MatrixCellOut `desc:"the first limit cells of the grid the tab draws, which keeps the busiest actors and objects; actors in order of their rows, then objects"`
	BusiestActor  string          `desc:"the actor of the busiest pair"`
	BusiestObject string          `desc:"the object of the busiest pair"`
	BusiestRows   uint64          `desc:"the busiest pair's rows"`
	Cut           bool            `desc:"the cells stop at the limit; the line says when the read itself stopped at its bound"`
	Line          string          `desc:"the matrix in words, as the tab states it"`
	Persisted     bool            `desc:"the trail reaches boxer.facts on this host"`
	RefreshedAt   time.Time       `desc:"when the table was last filled; the view is read against that fill"`
	Diagnostic    string          `desc:"why the result may be empty or behind: nothing persisted, the view not read yet, a read of an earlier fill, axes other than the window's, or a case, range or filter other than the one the window read; empty when none applies"`
}

// GetDottedArgs names the read the dotted chart is over, as GetDayHourArgs
// does, and the row kind.
type GetDottedArgs struct {
	Case   string    `desc:"the case's kind, as the window read it"`
	Value  string    `desc:"the case's value"`
	Range  RangeArgs `desc:"the window's range, as the window read it"`
	Filter []TermArg `desc:"the filter the table is narrowed by, as the window read it; empty for none"`
	Row    string    `desc:"run, window, conversation or entity: what one row of the chart is; empty for the window's pick"`
	Limit  int       `desc:"marks to return, by row then bucket; default and at most 200"`
}

// DottedMark is one (row, bucket) with rows.
type DottedMark struct {
	Key   string    `desc:"the chart row: the run, window, conversation or entity"`
	Start time.Time `desc:"the bucket's start"`
	Rows  uint64    `desc:"rows of the key in the bucket"`
}

// DottedResult is get_dotted's result.
type DottedResult struct {
	Row         string       `desc:"what one row of the chart is"`
	StepMs      int64        `desc:"the bucket size, in milliseconds: the strip's"`
	Keys        int          `desc:"distinct rows over the read"`
	Buckets     int          `desc:"buckets with rows"`
	Rows        uint64       `desc:"rows counted"`
	Marks       []DottedMark `desc:"the first limit marks of the rows the chart draws, which are the busiest; by row, then bucket"`
	BusiestKey  string       `desc:"the row with the most rows"`
	BusiestRows uint64       `desc:"its rows"`
	Largest     uint64       `desc:"the largest mark: the most rows of one row in one bucket"`
	Cut         bool         `desc:"the marks stop at the limit; the line says when the read itself stopped at its bound"`
	Line        string       `desc:"the chart in words, as the tab states it"`
	Persisted   bool         `desc:"the trail reaches boxer.facts on this host"`
	RefreshedAt time.Time    `desc:"when the table was last filled; the view is read against that fill"`
	Diagnostic  string       `desc:"why the result may be empty or behind: nothing persisted, the view not read yet, a read of an earlier fill, a row kind other than the window's, or a case, range or filter other than the one the window read; empty when none applies"`
}

// SetStripArgs sets what the strip's bars are coloured by.
type SetStripArgs struct {
	By string `desc:"kind or status"`
}

// SetGroupingArgs sets whether the table folds rows under their parent.
type SetGroupingArgs struct {
	On bool `desc:"true folds rows under the model call that asked for them; false lists them flat"`
}

// SetCompareArgs sets the row the detail pane compares the selected one
// with.
type SetCompareArgs struct {
	Id uint64 `desc:"a held row's facts id, from list_rows; 0 clears the comparison"`
}

// SetMatrixArgs sets the matrix's axes.
type SetMatrixArgs struct {
	Actor  string `desc:"principal, app or window"`
	Object string `desc:"subject, entity or operation"`
}

// SetDottedArgs sets what one row of the dotted chart is.
type SetDottedArgs struct {
	Row string `desc:"run, window, conversation or entity"`
}

// SetSideArgs sets the pane beside the table.
type SetSideArgs struct {
	Pane string `desc:"facets, exceptions, rules or views"`
}

// SetTreeArgs expands or collapses the tree tab's nodes.
type SetTreeArgs struct {
	Expanded bool `desc:"true opens every node, false closes every node"`
}

// ViewNameArgs names a saved view.
type ViewNameArgs struct {
	Name string `desc:"the saved view's name, as get_view lists it under saved_views"`
}

func parseLimit(limit int) (n int, err error) {
	switch {
	case limit < 0:
		err = app.RefuseOperation("limit is not negative")
	case limit == 0 || limit > opsMaxRows:
		n = opsMaxRows
	default:
		n = limit
	}
	return
}

// addControlOps registers the panes', the views' and the controls'
// resources, queries and commands.
func addControlOps(s *appops.Set[*App, opsSnap]) {
	s.Resource(resStrip, "what the strip's bars are coloured by: kind or status", func(inst *App) any { return inst.strip })
	s.Restorable(resStrip, func(inst *App, v any) bool {
		by, ok := v.(StripByE)
		if ok {
			inst.applyStrip(by)
		}
		return ok
	})
	s.Resource(resGrouping, "whether the table folds rows under their parent", func(inst *App) any { return inst.grouped })
	s.Restorable(resGrouping, func(inst *App, v any) bool {
		on, ok := v.(bool)
		if ok {
			inst.setGrouped(on)
		}
		return ok
	})
	s.Resource(resCompare, "the row the detail pane compares the selected one with, a facts id; 0 for none", func(inst *App) any { return inst.compareId() })
	s.Restorable(resCompare, func(inst *App, v any) bool {
		id, ok := v.(uint64)
		return ok && inst.applyCompare(id) == nil
	})
	s.Resource(resMatrix, "the matrix tab's actor and object", func(inst *App) any { return inst.an.pair })
	s.Restorable(resMatrix, func(inst *App, v any) bool {
		p, ok := v.(matrixPick)
		if ok {
			inst.applyMatrix(p)
		}
		return ok
	})
	s.Resource(resDotted, "what one row of the dotted chart is", func(inst *App) any { return inst.an.row })
	s.Restorable(resDotted, func(inst *App, v any) bool {
		r, ok := v.(DottedRowE)
		if ok {
			inst.applyDotted(r)
		}
		return ok
	})
	s.Resource(resSide, "the pane beside the table: facets, exceptions, rules or views", func(inst *App) any { return inst.side })
	s.Restorable(resSide, func(inst *App, v any) bool {
		side, ok := v.(sideE)
		if ok {
			inst.applySide(side)
		}
		return ok
	})
	s.Resource(resTree, "the tree tab's expansion as last set: every node open, or every node closed; a click on one node moves that node only", func(inst *App) any { return inst.treeV.expanded })
	s.Restorable(resTree, func(inst *App, v any) bool {
		expanded, ok := v.(bool)
		if ok {
			inst.applyTree(expanded)
		}
		return ok
	})
	s.Resource(resRead, "the window's last read of the trail marts: when it landed and what it reported; refresh asks for the next", func(inst *App) any { return inst.readValueOf() })
	s.Resource(resViews, "the saved views' names, in the persist store under the viewer's app id", func(inst *App) any { return inst.views.namesKey() })

	s.Available(opGetContext, persistedOr)
	s.Available(opGetCompare, persistedOr)
	s.Available(opSetCompare, persistedOr)

	// --- queries ---

	appops.Query(s, app.OperationSpec{Name: opGetFacets, Version: 1,
		Summary: "list one facet's values with their counts over a case and range — the kinds, statuses, apps, domain.actions or principals the facets pane shows, which set_filter may name — as the window last read them; the counts are over the case and range whole, whatever filter the table is narrowed by",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetFacetsArgs) (out FacetsResult, err error) {
			c, err := parseCaseArgs(in.Case, in.Value)
			if err != nil {
				return
			}
			w, err := parseRangeArgs(in.Range)
			if err != nil {
				return
			}
			facet, ok := ParseFacet(in.Facet)
			if !ok {
				err = app.RefuseOperation("facet is one of kind, status, app, domain.action, principal")
				return
			}
			limit, err := parseLimit(in.Limit)
			if err != nil {
				return
			}
			out.Facet = facet.String()
			out.Persisted, out.RefreshedAt = sn.src.persisted(), sn.refreshedAt
			out.Diagnostic = sn.cacheDiagnostic()
			if out.Diagnostic != "" || !sn.filled {
				return
			}
			if ok, why := sn.readCovers(c, w); !ok {
				out.Diagnostic = why
				return
			}
			var vs []FacetValue
			if int(facet) < len(sn.facets) {
				vs = sn.facets[facet]
			}
			out.Count = len(vs)
			for _, v := range vs[:min(len(vs), limit)] {
				out.Values = append(out.Values, FacetValueOut{Value: appops.CutText(v.Value, opsMaxHeadline), Rows: v.N})
			}
			out.Cut = len(vs) > limit
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetStrip, Version: 1,
		Summary: "read the strip of a case over a range and filter — rows per time bucket, stacked by kind or by status, with the bucket size and the strip's line — as the window last read it, for the colouring set_strip sets",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetStripArgs) (out StripResult, err error) {
			c, w, f, err := parseAggArgs(in.Case, in.Value, in.Range, in.Filter)
			if err != nil {
				return
			}
			by := sn.scope.Strip
			if in.By != "" {
				var ok bool
				if by, ok = ParseStripBy(in.By); !ok {
					err = app.RefuseOperation("by is kind or status")
					return
				}
			}
			limit, err := parseLimit(in.Limit)
			if err != nil {
				return
			}
			out.By = by.String()
			out.Persisted, out.RefreshedAt = sn.src.persisted(), sn.refreshedAt
			out.Diagnostic = sn.cacheDiagnostic()
			if out.Diagnostic != "" || !sn.filled {
				return
			}
			if ok, why := sn.aggCovers(c, w, f); !ok {
				out.Diagnostic = why
				return
			}
			if by != sn.scope.Strip {
				out.Diagnostic = "the strip is coloured by " + sn.scope.Strip.String() + "; set_strip reads it by " + by.String()
				return
			}
			out.StepMs, out.Line = sn.step.Milliseconds(), stripLine(sn.buckets, sn.step)
			per := map[int64]bool{}
			for _, b := range sn.buckets {
				out.Rows += b.N
				per[b.Bucket] = true
			}
			out.Buckets = len(per)
			for _, b := range sn.buckets[:min(len(sn.buckets), limit)] {
				out.Bars = append(out.Bars, StripBar{Start: time.Unix(b.Bucket, 0).UTC(), Key: appops.CutText(b.Key, opsMaxHeadline), Rows: b.N})
			}
			out.Cut = len(sn.buckets) > limit
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetContext, Version: 1,
		Summary: "read the rows before and after the selected row in the same writer run, whatever the filter hides, as the context tab shows them; the read is made for the selected row, so select_row it first",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetContextArgs) (out ContextResult, err error) {
			n := in.N
			switch {
			case n < 0:
				err = app.RefuseOperation("n is not negative")
				return
			case n == 0 || n > opsMaxContext:
				n = opsMaxContext
			}
			if in.Id == 0 || in.Id != sn.selected {
				err = app.RefuseOperation("the context read runs for the selected row; select_row it first")
				return
			}
			d := sn.detail
			switch {
			case d == nil:
				err = app.RefuseOperation("the selected row's context has not been read yet; ask again shortly")
				return
			case d.err != nil:
				err = app.RefuseOperation("the context read failed: " + d.err.Error())
				return
			}
			r, _ := sn.find(in.Id)
			out.Id = in.Id
			if r != nil {
				out.Run = r.Run
			}
			// before is oldest first, so the nearest rows are its last n.
			before := d.before[max(0, len(d.before)-n):]
			after := d.after[:min(len(d.after), n)]
			out.Cut = len(before) < len(d.before) || len(after) < len(d.after)
			for i := range before {
				out.Before = append(out.Before, summarize(&before[i], sn.lines))
				if out.Run == "" {
					out.Run = before[i].Run
				}
			}
			for i := range after {
				out.After = append(out.After, summarize(&after[i], sn.lines))
				if out.Run == "" {
					out.Run = after[i].Run
				}
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetCompare, Version: 1,
		Summary: "compare two held rows field by field — the timeline's columns and, when both are audit events, their own columns and attributes — the older row as before, as the compare tab shows them",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetCompareArgs) (out CompareResult, err error) {
			if in.A == 0 || in.B == 0 || in.A == in.B {
				err = app.RefuseOperation("a and b are two different held rows' facts ids")
				return
			}
			a, okA := sn.find(in.A)
			b, okB := sn.find(in.B)
			if !okA || !okB {
				err = app.RefuseOperation(rowGoneReason)
				return
			}
			cmp := compareRows(*a, *b, func(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) })
			out = CompareResult{OlderId: cmp.Older.Id, NewerId: cmp.Newer.Id, OlderTs: cmp.Older.Ts(), NewerTs: cmp.Newer.Ts(),
				BothAudit: cmp.Older.Audit != nil && cmp.Newer.Audit != nil, Base: cmp.Base}
			for _, f := range cmp.Fields {
				out.Fields = append(out.Fields, FieldDiff{Field: f.Field, Before: appops.CutText(f.Before, opsMaxText), After: appops.CutText(f.After, opsMaxText), Change: f.Change})
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetMatrix, Version: 1,
		Summary: "read the actor × object matrix of a case over a range and filter — rows per (principal, app or window) × (subject, entity or operation) with each pair's last outcome — as the window last read it; the view is read while its tab is open, for the axes set_matrix sets",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetMatrixArgs) (out MatrixResult, err error) {
			c, w, f, err := parseAggArgs(in.Case, in.Value, in.Range, in.Filter)
			if err != nil {
				return
			}
			pick := sn.matrixPick
			if in.Actor != "" {
				var ok bool
				if pick.Actor, ok = ParseActor(in.Actor); !ok {
					err = app.RefuseOperation("actor is one of " + actorNameList())
					return
				}
			}
			if in.Object != "" {
				var ok bool
				if pick.Object, ok = ParseObject(in.Object); !ok {
					err = app.RefuseOperation("object is one of " + objectNameList())
					return
				}
			}
			limit, err := parseLimit(in.Limit)
			if err != nil {
				return
			}
			out.Actor, out.Object = pick.Actor.String(), pick.Object.String()
			out.Persisted, out.RefreshedAt = sn.src.persisted(), sn.refreshedAt
			out.Diagnostic = sn.cacheDiagnostic()
			if out.Diagnostic != "" || !sn.filled {
				return
			}
			if ok, why := sn.aggCovers(c, w, f); !ok {
				out.Diagnostic = why
				return
			}
			m := sn.matrix
			if m == nil {
				out.Diagnostic = "the matrix has not been read; set_tab matrix opens it, and it reads while open"
				return
			}
			if (matrixPick{Actor: m.actor, Object: m.object}) != pick {
				out.Diagnostic = "the window's matrix is " + m.actor.String() + " × " + m.object.String() + "; set_matrix reads " + pick.String()
				return
			}
			if sn.matrixStale {
				out.Diagnostic = "the matrix is of the fill or pick before the current one and is being re-read; ask again shortly"
			}
			out.Actors, out.Objects, out.Rows, out.Pairs, out.Line = m.allA, m.allO, m.rows, m.pairs, m.line()
			out.BusiestActor, out.BusiestObject, out.BusiestRows = m.busiestA, m.busiestO, m.busiestN
			for i, a := range m.actors {
				for j, o := range m.objects {
					if m.counts[i][j] == 0 {
						continue
					}
					if len(out.Cells) == limit {
						out.Cut = true
						return
					}
					out.Cells = append(out.Cells, MatrixCellOut{Actor: appops.CutText(a, opsMaxHeadline), Object: appops.CutText(o, opsMaxHeadline),
						Rows: m.counts[i][j], LastOutcome: m.last[i][j]})
				}
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetDotted, Version: 1,
		Summary: "read the dotted chart of a case over a range and filter — rows per (run, window, conversation or entity) per bucket of the strip's step — as the window last read it; the view is read while its tab is open, for the row kind set_dotted sets",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetDottedArgs) (out DottedResult, err error) {
			c, w, f, err := parseAggArgs(in.Case, in.Value, in.Range, in.Filter)
			if err != nil {
				return
			}
			row := sn.dottedPick
			if in.Row != "" {
				var ok bool
				if row, ok = ParseDottedRow(in.Row); !ok {
					err = app.RefuseOperation("row is one of " + dottedRowNameList())
					return
				}
			}
			limit, err := parseLimit(in.Limit)
			if err != nil {
				return
			}
			out.Row = row.String()
			out.Persisted, out.RefreshedAt = sn.src.persisted(), sn.refreshedAt
			out.Diagnostic = sn.cacheDiagnostic()
			if out.Diagnostic != "" || !sn.filled {
				return
			}
			if ok, why := sn.aggCovers(c, w, f); !ok {
				out.Diagnostic = why
				return
			}
			d := sn.dotted
			if d == nil {
				out.Diagnostic = "the dotted chart has not been read; set_tab dotted opens it, and it reads while open"
				return
			}
			if d.row != row {
				out.Diagnostic = "the window's chart has a row per " + d.row.String() + "; set_dotted reads a row per " + row.String()
				return
			}
			if sn.dottedStale {
				out.Diagnostic = "the chart is of the fill or pick before the current one and is being re-read; ask again shortly"
			}
			out.StepMs, out.Keys, out.Buckets, out.Rows, out.Line = d.step.Milliseconds(), d.allKeys, d.buckets, d.rows, d.line()
			out.BusiestKey, out.BusiestRows, out.Largest = d.busiestK, d.busiestN, d.maxN
			for _, g := range d.marks[:min(len(d.marks), limit)] {
				out.Marks = append(out.Marks, DottedMark{Key: appops.CutText(g.Key, opsMaxHeadline), Start: time.Unix(g.Bucket, 0).UTC(), Rows: g.N})
			}
			out.Cut = len(d.marks) > limit
			return
		})

	// --- view commands ---

	appops.Command(s, app.OperationSpec{Name: opSetStrip, Version: 1,
		Summary: "set what the strip's bars are coloured by, kind or status; the strip is re-read with the table",
		Effect:  app.OperationEffectView, Writes: []string{resStrip}, Agents: true,
		Gesture: "the Colour by kind / by status button under the strip", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetStripArgs) (out appops.None, err error) {
			by, ok := ParseStripBy(in.By)
			if !ok {
				err = app.RefuseOperation("by is kind or status")
				return
			}
			inst.applyStrip(by)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetGrouping, Version: 1,
		Summary: "set whether the table folds rows under the model call that asked for them",
		Effect:  app.OperationEffectView, Writes: []string{resGrouping}, Agents: true,
		Gesture: "the Group by parent button above the table", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetGroupingArgs) (out appops.None, err error) {
			inst.setGrouped(in.On)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetCompare, Version: 1,
		Summary: "set the row the detail pane compares the selected one with, a held row's facts id; 0 clears the comparison; set_tab compare shows it",
		Effect:  app.OperationEffectView, Writes: []string{resCompare}, Agents: true,
		Gesture: "Compare with… then a row click, a Ctrl- or Cmd-click on a row, the Clear comparison button", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetCompareArgs) (out appops.None, err error) {
			err = inst.applyCompare(in.Id)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetMatrix, Version: 1,
		Summary: "set the matrix tab's actor (principal, app, window) and object (subject, entity, operation); the matrix is re-read for them while its tab is open",
		Effect:  app.OperationEffectView, Writes: []string{resMatrix}, Agents: true,
		Gesture: "the matrix tab's actor and object combos", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetMatrixArgs) (out appops.None, err error) {
			a, ok := ParseActor(in.Actor)
			if !ok {
				err = app.RefuseOperation("actor is one of " + actorNameList())
				return
			}
			o, ok := ParseObject(in.Object)
			if !ok {
				err = app.RefuseOperation("object is one of " + objectNameList())
				return
			}
			inst.applyMatrix(matrixPick{Actor: a, Object: o})
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetDotted, Version: 1,
		Summary: "set what one row of the dotted chart is: run, window, conversation or entity; the chart is re-read for it while its tab is open",
		Effect:  app.OperationEffectView, Writes: []string{resDotted}, Agents: true,
		Gesture: "the dotted chart's a-row-per combo", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetDottedArgs) (out appops.None, err error) {
			r, ok := ParseDottedRow(in.Row)
			if !ok {
				err = app.RefuseOperation("row is one of " + dottedRowNameList())
				return
			}
			inst.applyDotted(r)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetSide, Version: 1,
		Summary: "set the pane beside the table: facets, exceptions, rules or views",
		Effect:  app.OperationEffectView, Writes: []string{resSide}, Agents: true,
		Gesture: "the Facets, Exceptions, Rules and Views buttons", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetSideArgs) (out appops.None, err error) {
			side, ok := parseSide(in.Pane)
			if !ok {
				err = app.RefuseOperation("pane is one of " + sideNameList())
				return
			}
			inst.applySide(side)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetTree, Version: 1,
		Summary: "open every node of the tree tab, or close every node; the tree resource reports the value last set, since a click moves one node",
		Effect:  app.OperationEffectView, Writes: []string{resTree}, Agents: true,
		Gesture: "the tree tab's Expand all and Collapse all buttons", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetTreeArgs) (out appops.None, err error) {
			inst.applyTree(in.Expanded)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opRefresh, Version: 1,
		Summary: "ask the window to read its case from the trail marts again, and lift a stop; the read lands in a later frame, and get_view's refreshed_at says when",
		Effect:  app.OperationEffectView, Writes: []string{resRead, resReadState}, Agents: true,
		Gesture: "the Refresh button", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, _ appops.None) (out appops.None, err error) {
			inst.cache.refresh()
			return
		})

	// A stop is a hold, not a failure (ADR-0350 §SD12): it sets and never
	// toggles; refresh or a scope change lifts it.
	appops.Command(s, app.OperationSpec{Name: opStopRead, Version: 1,
		Summary: "stop the window's reads of the trail marts: cancel the read in flight and start none — no refill, no interval refresh, no extension at the table's end — until refresh or a change of the case, range, filter or colouring; the rows held stay on screen",
		Effect:  app.OperationEffectView, Writes: []string{resRead, resReadState}, Agents: true,
		Gesture: "the Cancel button beside the progress bar", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, _ appops.None) (out appops.None, err error) {
			inst.cache.stop()
			return
		})

	appops.Command(s, app.OperationSpec{Name: opOpenView, Version: 1,
		Summary: "open a saved view: set the case, the range, the filter and the tab it keeps; the window re-reads the trail marts under them",
		Effect:  app.OperationEffectView, Writes: []string{resCase, resRange, resFilter, resTab}, Agents: true,
		Gesture: "a saved view's Open button", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in ViewNameArgs) (out appops.None, err error) {
			err = inst.openViewNamed(in.Name)
			return
		})

	// --- document commands: the saved views are authored state ---

	appops.Command(s, app.OperationSpec{Name: opSaveView, Version: 1,
		Summary: "save the window's case, range, filter and tab as a view under a name in the host's persist store, shared by every trail viewer window; a name already saved is replaced; the write lands in a later frame, and get_view's saved_views lists it then",
		Effect:  app.OperationEffectDocument, Writes: []string{resViews}, Agents: true,
		Gesture: "the Views pane's name box and Save", Rebuild: app.OperationRebuildDurable},
		func(inst *App, _ app.OperationCall, in ViewNameArgs) (out appops.None, err error) {
			err = inst.saveView(in.Name)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opDeleteView, Version: 1,
		Summary: "delete a saved view by name from the host's persist store; the write lands in a later frame",
		Effect:  app.OperationEffectDocument, Writes: []string{resViews}, Agents: true,
		Gesture: "a saved view's Delete button", Rebuild: app.OperationRebuildDurable},
		func(inst *App, _ app.OperationCall, in ViewNameArgs) (out appops.None, err error) {
			err = inst.deleteView(in.Name)
			return
		})

	// --- the export: it writes outside the app ---

	appops.Command(s, app.OperationSpec{Name: opSendToPlay, Version: 1,
		Summary: "publish the table as it stands — the rows the window holds, in the columns it shows — as the ad-hoc dataset trail_rows and open a play window reading it; the publish is itself a trail row, and the person confirms each call",
		Effect:  app.OperationEffectConsequential, Agents: true,
		Gesture: "the Send to play button above the table",
		Follows: []string{"the publish and the window open run off the frame; the line beside the button says how they went",
			"the dataset lives as long as this window, and sending again replaces it"}},
		func(inst *App, _ app.OperationCall, _ appops.None) (out SendToPlayResult, err error) {
			return inst.sendToPlay()
		})
}

// The controls' gestures (ADR-0345 §SD3); the analytic views', the tree's,
// the saved views' and the export's are beside their apply functions.

func (inst *App) refreshGesture() {
	gestureOr(inst, opRefresh, appops.None{}, func() { inst.cache.refresh() })
}

func (inst *App) stopReadGesture() {
	gestureOr(inst, opStopRead, appops.None{}, func() { inst.cache.stop() })
}

func (inst *App) setStripGesture(by StripByE) {
	gestureOr(inst, opSetStrip, SetStripArgs{By: by.String()}, func() { inst.applyStrip(by) })
}

func (inst *App) setGroupedGesture(on bool) {
	gestureOr(inst, opSetGrouping, SetGroupingArgs{On: on}, func() { inst.setGrouped(on) })
}

func (inst *App) setCompareGesture(id uint64) {
	gestureOr(inst, opSetCompare, SetCompareArgs{Id: id}, func() { _ = inst.applyCompare(id) })
}

func (inst *App) setSideGesture(side sideE) {
	gestureOr(inst, opSetSide, SetSideArgs{Pane: side.String()}, func() { inst.applySide(side) })
}
