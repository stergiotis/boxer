package tally

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/apps/tally/launchcfg"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/help/search"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsbrowser"
)

// The operations catalog (ADR-0269, keelson-app A1–A3): what an agent reads
// and moves on an open window. Queries answer from the frame's snapshot —
// the panes' locations and the listings the browser last drew — and take
// their own arguments; find and grep are external reads that run the lane's
// own statements against the store, bounded at the lane's caps. The
// commands set the view: a pane's location, its selection, the raised tab,
// and the Results query, which run_query runs asynchronously and settles
// when the lane lands. Nothing here writes to the store, which the app
// never does (ADR-0200 §SD5).

const (
	resPanes = "panes"
	resQuery = "query"

	opGetView      = "get_view"
	opListEntries  = "list_entries"
	opGetSelection = "get_selection"
	opFind         = "find"
	opGrep         = "grep"
	opSetLocation  = "set_location"
	opSelectEntry  = "select_entry"
	opShowTab      = "show_tab"
	opSetQuery     = "set_query"
	opRunQuery     = "run_query"
)

// Result bounds: what one inline result may carry. The find and grep caps
// are the lanes' own (tally_find.go).
const (
	opsMaxEntries     = 200
	opsDefaultEntries = 50
	findRowCap        = 500
	grepRowCap        = 1000
	// externalReadTimeout bounds a find or grep an agent asked for; the
	// lanes' runs are cancellable from the progress row instead.
	externalReadTimeout = 60 * time.Second
)

// Refusal reasons, each naming its remedy.
const (
	reasonPane         = "pane is A or B for a browse pane, R for the Results pane"
	reasonBrowsePane   = "pane is A or B; the Results pane's location is the query's"
	reasonNoMount      = "no mount of that id is in the store's mount list; get_view names the mounts"
	reasonNoLocation   = "the pane has no location yet; set_location gives it a mount"
	reasonNotListed    = "the pane's listing does not carry that path; list_entries names the entries, set_location changes the directory"
	reasonNoTab        = "no tab of that name; get_view lists the tabs"
	reasonNoResultsTab = "the Results tab exists only while the window carries a query; set_query first"
	reasonNoQuery      = "the window carries no query; set_query first"
	reasonRunning      = "the query is running; its outcome settles the run_query that started it"
	reasonNoStore      = "the window is not connected to a store; get_view says why"
)

// PaneView is one pane as the catalog reports it.
type PaneView struct {
	Pane         string `desc:"A, B or R"`
	Mount        string `desc:"the mount id in hex, the SFTP spelling; empty when the pane has none"`
	Snapshot     string `desc:"the pinned snapshot as RFC 3339 with nanoseconds; empty while following the newest"`
	FollowLatest bool   `desc:"the pane follows the mount's newest complete snapshot"`
	Dir          string `desc:"the directory the pane lists, as an io/fs path; . is the root"`
	Selected     string `desc:"the selected file's path; empty when none, or when a directory is selected"`
	Entries      int    `desc:"entries in the pane's last listing"`
}

// PanesValue is the panes resource: both browse panes, the Results pane,
// which one the Mounts clicks address and which one the lower tabs describe.
type PanesValue struct {
	A      PaneView
	B      PaneView
	R      PaneView
	Target string `desc:"the pane the Mounts list's clicks address: A or B"`
	Focus  string `desc:"the pane the lower tabs describe: A, B or R"`
	Tab    string `desc:"the tab the window last raised; empty when it raised none"`
}

// MountInfo is one mount of the store, for set_location.
type MountInfo struct {
	Id        string `desc:"the mount id in hex, as set_location takes it"`
	Label     string `desc:"the mount's name, or its id when it has none"`
	Snapshots int    `desc:"complete snapshots the store holds for it"`
}

// ResultState is where the Results query stands.
type ResultState struct {
	Phase     string `desc:"none (no query), idle (not run yet), running, landed or failed"`
	Error     string `desc:"the failure's reason; empty otherwise"`
	Rows      int    `desc:"rows the last landed run placed, directories included"`
	Dropped   int    `desc:"rows the last landed run could place nowhere"`
	Truncated bool   `desc:"the last landed run hit the pane's row cap"`
}

// View is get_view's result.
type View struct {
	Panes      PanesValue
	Mounts     []MountInfo `desc:"the store's mounts, as the Mounts list shows them"`
	Tabs       []string    `desc:"the tab names show_tab takes"`
	Query      string      `desc:"the Results SQL; empty when the window carries none"`
	QueryLabel string      `desc:"what the Results pane calls the result set"`
	Result     ResultState
	Connected  bool   `desc:"the window has a store connection; find, grep and run_query need one"`
	Status     string `desc:"the toolbar's status line"`
}

// PaneArgs names a pane.
type PaneArgs struct {
	Pane string `desc:"A, B or R"`
}

// ListEntriesArgs is list_entries's argument.
type ListEntriesArgs struct {
	Pane  string `desc:"A, B or R"`
	Limit int    `json:",omitzero" desc:"entries to return, in the listing's order; default 50, at most 200"`
}

// Entry is one row of a pane's listing.
type Entry struct {
	Name    string    `desc:"the base name"`
	Path    string    `desc:"the io/fs path from the pane's root; what select_entry takes"`
	IsDir   bool      `desc:"a directory"`
	Size    int64     `desc:"the recorded size in bytes; 0 for a directory"`
	ModTime time.Time `desc:"the recorded modification time"`
}

// ListEntriesResult is list_entries's result.
type ListEntriesResult struct {
	Pane      string
	Dir       string  `desc:"the directory listed"`
	Matched   int     `desc:"entries in the listing"`
	Entries   []Entry `desc:"the first limit entries, in the listing's order"`
	Truncated bool    `desc:"the listing holds more than Entries carries"`
}

// Selection is get_selection's result.
type Selection struct {
	Pane     string
	Path     string `desc:"the selected file's path; empty when none"`
	Mount    string `desc:"the mount the file is in, in hex"`
	Snapshot string `desc:"the snapshot the file is read from, RFC 3339 with nanoseconds"`
}

// FindArgs is find's argument: the Find tab's knobs, with the pattern as a
// battery.
type FindArgs struct {
	Pane    string `desc:"the pane whose location scopes the search: A or B"`
	Query   string `desc:"a regex battery over the path: space-separated, case-insensitive RE2 tokens that must all match; a token that does not compile matches literally"`
	Ext     string `json:",omitzero" desc:"an extension the file must have, with the dot; empty keeps every extension"`
	MinSize int64  `json:",omitzero" desc:"the smallest size kept, in bytes"`
	Scope   string `json:",omitzero" desc:"dir (the pane's directory, the default), snapshot (the pane's whole snapshot) or all (every mount's newest)"`
}

// FindRow is one file find returned.
type FindRow struct {
	Mount string `desc:"the mount id in hex"`
	Path  string
	Size  int64
	Ext   string
	Hash  string `desc:"the content's BLAKE3, lower-case hex"`
}

// FindResult is find's result, largest file first.
type FindResult struct {
	Rows      []FindRow
	Truncated bool   `desc:"more files matched than the cap of 500"`
	Literal   bool   `desc:"a token did not compile as a regexp and matched literally"`
	SQL       string `desc:"the statement run, to run uncapped in play"`
}

// GrepArgs is grep's argument.
type GrepArgs struct {
	Pane    string `desc:"the pane whose location scopes the search: A or B"`
	Pattern string `desc:"an RE2 pattern matched against each line of the files the store marked text"`
	Scope   string `json:",omitzero" desc:"dir (the default), snapshot or all, as find takes it"`
}

// GrepRow is one matching line.
type GrepRow struct {
	Mount string `desc:"the mount id in hex"`
	Path  string
	Line  int    `desc:"the line number, from 1"`
	Text  string `desc:"the line"`
}

// GrepResult is grep's result, ordered by mount, path and line.
type GrepResult struct {
	Rows      []GrepRow
	Truncated bool   `desc:"more lines matched than the cap of 1000"`
	SQL       string `desc:"the statement run, to run uncapped in play"`
}

// SetLocationArgs is set_location's argument; every field but Pane is
// optional and an empty one leaves that part of the location alone.
type SetLocationArgs struct {
	Pane     string `desc:"A or B"`
	Mount    string `json:",omitzero" desc:"a mount id from get_view, in hex; empty keeps the pane's mount"`
	Snapshot string `json:",omitzero" desc:"a snapshot as RFC 3339 with nanoseconds to pin; 'latest' follows the newest; empty keeps the pane's"`
	Dir      string `json:",omitzero" desc:"the directory to list, as an io/fs path; . is the root; empty keeps the pane's (a new mount starts at the root)"`
}

// SelectEntryArgs is select_entry's argument.
type SelectEntryArgs struct {
	Pane string `desc:"A, B or R"`
	Path string `json:",omitzero" desc:"a path from list_entries; empty clears the selection"`
}

// ShowTabArgs is show_tab's argument.
type ShowTabArgs struct {
	Tab string `desc:"paneA, paneB, results, preview, info, history, diff, find, du or problems"`
}

// SetQueryArgs is set_query's argument.
type SetQueryArgs struct {
	Sql   string `desc:"a read-only query over the lading relations yielding a path column, with mount and snap optional (ADR-0222 §SD3)"`
	Label string `json:",omitzero" desc:"what the Results pane calls the result set"`
}

// paneSnap is one pane in the snapshot: its view and the listing it drew.
type paneSnap struct {
	view  PaneView
	loc   location
	locOk bool
	rows  []fsbrowser.Entry
}

// opsSnap is what queries read: a copy taken after the command stage.
type opsSnap struct {
	panes      [paneCount]paneSnap
	target     paneIDE
	focus      paneIDE
	raisedTab  uint64
	mounts     []MountInfo
	querySql   string
	queryLabel string
	result     ResultState
	status     string
	// sc is the store connection the external reads run against; nil
	// while the window is not connected. The lanes read it off-thread the
	// same way.
	sc *storeConn
}

func paneOf(s string) (id paneIDE, ok bool) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "A":
		return paneIDA, true
	case "B":
		return paneIDB, true
	case "R":
		return paneIDR, true
	}
	return 0, false
}

func (inst *App) paneView(id paneIDE) (v PaneView) {
	p := &inst.panes[id]
	v = PaneView{Pane: id.String(), Mount: mountText(p.mount), Snapshot: snapText(p), FollowLatest: p.followLatest,
		Dir: p.st.Dir(), Selected: p.selected, Entries: len(p.rows)}
	if id == paneIDR {
		// The Results pane follows the selected row's snapshot, not the
		// mount list's newest; it never follows latest.
		v.FollowLatest = false
	}
	return
}

func (inst *App) panesValue() (v PanesValue) {
	v = PanesValue{A: inst.paneView(paneIDA), B: inst.paneView(paneIDB), R: inst.paneView(paneIDR),
		Target: inst.target.String(), Focus: inst.focus.String(), Tab: tabSlug(inst.raisedTab)}
	return
}

func (inst *App) mountInfos() (out []MountInfo) {
	out = make([]MountInfo, 0, len(inst.mountRows))
	for _, m := range inst.mountRows {
		out = append(out, MountInfo{Id: hexID(m.id), Label: m.label(), Snapshots: len(m.snapshots)})
	}
	return
}

// resultState reads the Results lane as the catalog reports it.
func (inst *App) resultState() (rs ResultState) {
	if inst.querySql == "" {
		rs.Phase = "none"
		return
	}
	snap := inst.resultLane.Snapshot()
	switch snap.State {
	case bgjob.StateRunning:
		rs.Phase = "running"
	case bgjob.StateFailed:
		rs.Phase = "failed"
		if snap.Err != nil {
			rs.Error = snap.Err.Error()
		}
	case bgjob.StateDone:
		rs.Phase = "landed"
		rs.Rows, rs.Dropped, rs.Truncated = inst.resultSet.rows+inst.resultSet.dirRows, inst.resultSet.dropped, inst.resultSet.truncated
	default:
		rs.Phase = "idle"
	}
	return
}

func (inst *App) opsSnapshot() (sn opsSnap) {
	for id := range paneIDE(paneCount) {
		p := &inst.panes[id]
		sn.panes[id] = paneSnap{view: inst.paneView(id), rows: p.rows}
		sn.panes[id].loc, sn.panes[id].locOk = inst.locationOf(p)
	}
	sn.target, sn.focus, sn.raisedTab = inst.target, inst.focus, inst.raisedTab
	sn.mounts = inst.mountInfos()
	sn.querySql, sn.queryLabel = inst.querySql, inst.queryLabel
	sn.result = inst.resultState()
	sn.status = inst.status
	// A question, not an order: the lane is demanded by the frame.
	if sc, done, _, _ := inst.conn.Demand("connect", nil); done {
		sn.sc = sc
	}
	return
}

func (sn opsSnap) panesValue() (v PanesValue) {
	v = PanesValue{A: sn.panes[paneIDA].view, B: sn.panes[paneIDB].view, R: sn.panes[paneIDR].view,
		Target: sn.target.String(), Focus: sn.focus.String(), Tab: tabSlug(sn.raisedTab)}
	return
}

func entryView(e fsbrowser.Entry) (out Entry) {
	out = Entry{Name: e.Name, Path: e.Path, IsDir: e.IsDir, Size: e.Size, ModTime: e.ModTime}
	return
}

// scopeOf reads a find or grep scope; empty is the pane's directory.
func scopeOf(s string) (scope findScopeE, err error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "dir":
		return findScopeDir, nil
	case "snapshot":
		return findScopeMount, nil
	case "all":
		return findScopeAll, nil
	}
	return 0, app.RefuseOperation("scope is dir, snapshot or all")
}

// searchPane resolves the pane an external read is scoped to.
func searchPane(sn opsSnap, pane string) (ps paneSnap, err error) {
	id, ok := paneOf(pane)
	if !ok || id == paneIDR {
		err = app.RefuseOperation(reasonBrowsePane)
		return
	}
	ps = sn.panes[id]
	if !ps.locOk {
		err = app.RefuseOperation(reasonNoLocation)
		return
	}
	if sn.sc == nil {
		err = app.RefuseOperation(reasonNoStore)
	}
	return
}

// findBatterySQL is the name search as find runs it for an agent: the Find
// tab's statement with one match() per token of the battery, each token's
// effective source — `(?i)` plus the token, quote-meta'd when it degraded —
// so ClickHouse's RE2 judges exactly what the window's regexps would. The
// limit is one past the cap, so the caller can tell a full page from a cut
// one.
func findBatterySQL(loc location, scope findScopeE, dir string, b search.Battery, ext string, minSize int64, limit int) string {
	all := scope == findScopeAll
	pred := "1"
	if scope == findScopeDir {
		pred = scopePredicate(dir)
	}
	var where strings.Builder
	for i := range b.Patterns {
		where.WriteString("\n  AND match(path, ")
		where.WriteString(ladingschema.QuoteLiteral(b.Patterns[i].EffectiveSource()))
		where.WriteString(")")
	}
	return fmt.Sprintf(`SELECT mount, path, size, ext, lower(hex(content_hash)) AS hash
FROM %s
WHERE NOT is_dir
  AND %s%s
  AND (%s = '' OR ext = %s)
  AND size >= %d
ORDER BY size DESC
LIMIT %d`,
		relation("fs", loc, all), pred, where.String(),
		ladingschema.QuoteLiteral(ext), ladingschema.QuoteLiteral(ext),
		minSize, limit)
}

// hexCell reads a mount cell as the table renders it, back to hex.
func hexCell(s string) (hex string) {
	if v, err := strconv.ParseUint(s, 10, 64); err == nil {
		return hexID(identifier.TaggedId(v))
	}
	return s
}

func cell(row []string, i int) (s string) {
	if i >= 0 && i < len(row) {
		return row[i]
	}
	return ""
}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet((*App).opsSnapshot)

	s.Resource(resPanes, "the two browse panes and the Results pane: location, selection, which pane the Mounts clicks address, the raised tab",
		func(inst *App) any { return inst.panesValue() })
	s.Resource(resQuery, "the Results SQL the window carries; empty for none",
		func(inst *App) any { return inst.querySql })

	appops.Query(s, app.OperationSpec{Name: opGetView, Version: 1,
		Summary: "read the panes, the store's mounts, the tabs, the Results query and where its run stands",
		Reads:   []string{resPanes, resQuery}, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out View, err error) {
			out = View{Panes: sn.panesValue(), Mounts: sn.mounts, Tabs: launchcfg.TabIds(),
				Query: sn.querySql, QueryLabel: sn.queryLabel, Result: sn.result, Connected: sn.sc != nil, Status: sn.status}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opListEntries, Version: 1,
		Summary: "list one pane's directory as the browser last drew it, at most 200 entries; the pane's quick filter is not applied",
		Reads:   []string{resPanes}, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in ListEntriesArgs) (out ListEntriesResult, err error) {
			id, ok := paneOf(in.Pane)
			if !ok {
				err = app.RefuseOperation(reasonPane)
				return
			}
			limit := in.Limit
			switch {
			case limit < 0:
				err = app.RefuseOperation("limit is not negative")
				return
			case limit == 0:
				limit = opsDefaultEntries
			case limit > opsMaxEntries:
				limit = opsMaxEntries
			}
			ps := sn.panes[id]
			out = ListEntriesResult{Pane: id.String(), Dir: ps.view.Dir, Matched: len(ps.rows)}
			for _, e := range ps.rows {
				if len(out.Entries) >= limit {
					out.Truncated = true
					break
				}
				out.Entries = append(out.Entries, entryView(e))
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetSelection, Version: 1,
		Summary: "read one pane's selected file and the snapshot it is read from",
		Reads:   []string{resPanes}, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in PaneArgs) (out Selection, err error) {
			id, ok := paneOf(in.Pane)
			if !ok {
				err = app.RefuseOperation(reasonPane)
				return
			}
			ps := sn.panes[id]
			out = Selection{Pane: id.String(), Path: ps.view.Selected}
			if ps.view.Selected != "" && ps.locOk {
				out.Mount, out.Snapshot = hexID(ps.loc.mount), ps.loc.snap.UTC().Format(time.RFC3339Nano)
			}
			return
		})

	appops.ExternalRead(s, app.OperationSpec{Name: opFind, Version: 1,
		Summary: "search file names in the store as the Find tab does: a regex battery over the path, an extension and a minimum size, largest first, at most 500 rows",
		Reads:   []string{resPanes}, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ app.OperationCall, in FindArgs) (out FindResult, err error) {
			ps, err := searchPane(sn, in.Pane)
			if err != nil {
				return
			}
			scope, err := scopeOf(in.Scope)
			if err != nil {
				return
			}
			b := search.ParseQuery(in.Query)
			for i := range b.Patterns {
				out.Literal = out.Literal || b.Patterns[i].Literal
			}
			out.SQL = findBatterySQL(ps.loc, scope, ps.view.Dir, b, strings.TrimSpace(in.Ext), in.MinSize, findRowCap+1)
			ctx, cancel := context.WithTimeout(context.Background(), externalReadTimeout)
			defer cancel()
			res, err := runTable(ctx, sn.sc.exec, sn.sc.sql, out.SQL)
			if err != nil {
				return
			}
			mc, pc, sc, ec, hc := columnIndex(res.headers, "mount"), columnIndex(res.headers, "path"),
				columnIndex(res.headers, "size"), columnIndex(res.headers, "ext"), columnIndex(res.headers, "hash")
			for i, row := range res.rows {
				if i >= findRowCap {
					out.Truncated = true
					break
				}
				size, _ := strconv.ParseInt(cell(row, sc), 10, 64)
				out.Rows = append(out.Rows, FindRow{Mount: hexCell(cell(row, mc)), Path: cell(row, pc), Size: size, Ext: cell(row, ec), Hash: cell(row, hc)})
			}
			return
		})

	appops.ExternalRead(s, app.OperationSpec{Name: opGrep, Version: 1,
		Summary: "search file contents in the store as the Find tab does: an RE2 pattern over each line of the files marked text, with line numbers, at most 1000 rows",
		Reads:   []string{resPanes}, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ app.OperationCall, in GrepArgs) (out GrepResult, err error) {
			ps, err := searchPane(sn, in.Pane)
			if err != nil {
				return
			}
			scope, err := scopeOf(in.Scope)
			if err != nil {
				return
			}
			needle := strings.TrimSpace(in.Pattern)
			if needle == "" {
				err = app.RefuseOperation("pattern is the RE2 to match against each line")
				return
			}
			out.SQL = grepSQL(ps.loc, scope, ps.view.Dir, needle, grepRowCap+1)
			ctx, cancel := context.WithTimeout(context.Background(), externalReadTimeout)
			defer cancel()
			res, err := runTable(ctx, sn.sc.exec, sn.sc.sql, out.SQL)
			if err != nil {
				return
			}
			mc, pc, lc, tc := columnIndex(res.headers, "mount"), columnIndex(res.headers, "path"),
				columnIndex(res.headers, "lineno"), columnIndex(res.headers, "line")
			for i, row := range res.rows {
				if i >= grepRowCap {
					out.Truncated = true
					break
				}
				line, _ := strconv.Atoi(cell(row, lc))
				out.Rows = append(out.Rows, GrepRow{Mount: hexCell(cell(row, mc)), Path: cell(row, pc), Line: line, Text: cell(row, tc)})
			}
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetLocation, Version: 1,
		Summary: "point a browse pane at a mount, a snapshot and a directory; a part left empty keeps the pane's",
		Effect:  app.OperationEffectView, Writes: []string{resPanes}, Agents: true,
		Gesture: "a mount in the Mounts list, a snapshot under it, a directory in the pane", Rebuild: app.OperationRebuildVolatile,
		Follows: []string{"the pane lists the directory in a later frame; list_entries reads it once drawn", "a new mount starts at its root, following the newest snapshot"}},
		func(inst *App, _ app.OperationCall, in SetLocationArgs) (out appops.None, err error) {
			id, ok := paneOf(in.Pane)
			if !ok || id == paneIDR {
				err = app.RefuseOperation(reasonBrowsePane)
				return
			}
			p := &inst.panes[id]
			var mount identifier.TaggedId
			if in.Mount != "" {
				if mount, ok = parseMountText(in.Mount); !ok || !inst.hasMount(mount) {
					err = app.RefuseOperation(reasonNoMount)
					return
				}
			}
			var snap time.Time
			follow := false
			switch s := strings.TrimSpace(in.Snapshot); {
			case s == "":
			case strings.EqualFold(s, "latest"):
				follow = true
			default:
				if snap, err = time.Parse(time.RFC3339Nano, s); err != nil {
					err = app.RefuseOperation("snapshot is RFC 3339 with nanoseconds, as get_view spells it, or latest")
					return
				}
			}
			if mount != 0 && mount != p.mount {
				inst.selectMount(p, mount)
			}
			switch {
			case follow:
				p.followLatest, p.snap = true, time.Time{}
			case !snap.IsZero():
				inst.pinSnapshot(p, snap)
			}
			if dir := path.Clean(strings.TrimSpace(in.Dir)); in.Dir != "" && dir != p.st.Dir() {
				p.st.SetDir(dir)
				p.selected = ""
			}
			inst.target, inst.focus = id, id
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSelectEntry, Version: 1,
		Summary: "select one entry of a pane's listing, so the lower tabs describe it; an empty path clears the selection",
		Effect:  app.OperationEffectView, Writes: []string{resPanes}, Agents: true,
		Gesture: "a row of the pane", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SelectEntryArgs) (out appops.None, err error) {
			id, ok := paneOf(in.Pane)
			if !ok {
				err = app.RefuseOperation(reasonPane)
				return
			}
			p := &inst.panes[id]
			if in.Path == "" {
				for _, sel := range p.st.Selection() {
					p.st.Select(sel, false)
				}
				p.selected = ""
				return
			}
			e, found := entryOf(p.rows, in.Path)
			if !found {
				err = app.RefuseOperation(reasonNotListed)
				return
			}
			p.st.SelectOnly(in.Path)
			if e.IsDir {
				p.selected = ""
			} else {
				p.selected = in.Path
			}
			inst.focus = id
			return
		})

	appops.Command(s, app.OperationSpec{Name: opShowTab, Version: 1,
		Summary: "raise one of the dock's tabs",
		Effect:  app.OperationEffectView, Writes: []string{resPanes}, Agents: true,
		Gesture: "the tab's title in the dock", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in ShowTabArgs) (out appops.None, err error) {
			slug := strings.TrimSpace(in.Tab)
			if dockTabForSlug(slug) == 0 {
				err = app.RefuseOperation(reasonNoTab)
				return
			}
			id := inst.resolveTab(slug)
			if id == 0 {
				err = app.RefuseOperation(reasonNoResultsTab)
				return
			}
			inst.raiseTab(id)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetQuery, Version: 1,
		Summary: "set the Results SQL; a statement not provably read-only is refused with its classification",
		Effect:  app.OperationEffectDocument, Writes: []string{resQuery}, Agents: true,
		Rebuild: app.OperationRebuildVolatile,
		Follows: []string{"nothing runs until run_query; a changed query drops the last result"}},
		func(inst *App, _ app.OperationCall, in SetQueryArgs) (out appops.None, err error) {
			sql := strings.TrimSpace(in.Sql)
			if sql == "" {
				err = app.RefuseOperation("sql is the query; an empty one would remove the Results tab, which the window keeps")
				return
			}
			if kind := analysis.ClassifyStatementKind(sql); kind != analysis.KindReadOnly {
				err = app.RefuseOperation("the query classifies as " + kind.String() + ", not read-only; this window runs reads only")
				return
			}
			if sql != inst.querySql {
				inst.resultLane.Invalidate()
			}
			inst.querySql, inst.queryLabel = sql, in.Label
			return
		})

	appops.Command(s, app.OperationSpec{Name: opRunQuery, Version: 1,
		Summary: "run the Results SQL against the store; the call stays running until the lane lands or fails, and answers with the result's state",
		Effect:  app.OperationEffectRun, Reads: []string{resQuery}, Writes: []string{resPanes}, Agents: true,
		Gesture: "the Run again button in the Results tab", Async: true, Rebuild: app.OperationRebuildVolatile,
		Follows: []string{"the Results tab is raised, and its pane lists the rows once they land"}},
		func(inst *App, _ app.OperationCall, _ appops.None) (out ResultState, err error) {
			if inst.querySql == "" {
				err = app.RefuseOperation(reasonNoQuery)
				return
			}
			if inst.resultLane.Snapshot().State == bgjob.StateRunning {
				err = app.RefuseOperation(reasonRunning)
				return
			}
			inst.resultLane.Invalidate()
			// A run of the same key lands under the same key; clearing it
			// makes the Results pane take the new rows, and tells the settle
			// a landed lane from rows the pane has not taken yet.
			inst.resultKey = ""
			inst.runArmed = true
			inst.raiseTab(dockTabResults)
			return
		})
	appops.Settles(s, opRunQuery, func(inst *App, _ app.OperationCall) (out ResultState, settled bool, err error) {
		snap := inst.resultLane.Snapshot()
		landed := snap.State == bgjob.StateFailed || (snap.State == bgjob.StateDone && inst.resultKey != "")
		if inst.runArmed && !landed {
			return
		}
		inst.runArmed = false
		return inst.resultState(), true, nil
	})
	s.Available(opRunQuery, func(sn opsSnap) (ok bool, reason string) {
		switch {
		case sn.querySql == "":
			return false, reasonNoQuery
		case sn.result.Phase == "running":
			return false, reasonRunning
		}
		return true, ""
	})
	return
}()

// Operations serves the catalog for this window (app.OperationsAppI).
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

var _ app.OperationsAppI = (*App)(nil)

func (inst *App) hasMount(id identifier.TaggedId) (ok bool) {
	for _, m := range inst.mountRows {
		if m.id == id {
			return true
		}
	}
	return
}

// raiseTab asks the dock to raise a tab on its next send and records it as
// the raised one; the Results tab also takes the lower tabs' focus.
func (inst *App) raiseTab(id uint64) {
	inst.pendingDockActivate, inst.raisedTab = id, id
	if id == dockTabResults {
		inst.focus = paneIDR
	}
}

// gestureOr routes the person's control through the catalog (ADR-0269
// §SD8), so the command log sees it as theirs, and applies it directly
// where no host serves the catalog (tour, tests, headless).
func gestureOr[In any](inst *App, op string, in In, direct func()) {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[In, appops.None](inst.frameCtx, op, in); err == nil {
			return
		}
	}
	direct()
}

// runQueryGesture is the Run again button under a cancelled Results run.
func (inst *App) runQueryGesture() {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[appops.None, ResultState](inst.frameCtx, opRunQuery, appops.None{}); err == nil {
			return
		}
	}
	inst.resultLane.Invalidate()
}

// selectMountGesture is a click on a mount in the Mounts list.
func (inst *App) selectMountGesture(id paneIDE, mount identifier.TaggedId) {
	gestureOr(inst, opSetLocation, SetLocationArgs{Pane: id.String(), Mount: hexID(mount)}, func() {
		inst.selectMount(&inst.panes[id], mount)
	})
}

// pinSnapshotGesture is a click on a snapshot under the selected mount.
func (inst *App) pinSnapshotGesture(id paneIDE, snap time.Time) {
	gestureOr(inst, opSetLocation, SetLocationArgs{Pane: id.String(), Snapshot: snap.UTC().Format(time.RFC3339Nano)}, func() {
		inst.pinSnapshot(&inst.panes[id], snap)
	})
}
