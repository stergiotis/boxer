package tally

import (
	"context"
	"fmt"
	"io/fs"
	"math"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsql"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsbrowser"
)

// tally_results.go is the query half of ADR-0222: a caller hands the window a
// read over the lading surface that yields paths, and the rows become a
// browsable tree rather than a table. play answers a query with a table;
// answering one here is only worth doing if the answer is files.

// resultRowCap bounds what the pane will hold, whatever LIMIT the caller
// wrote. A path set is something to browse — past a few thousand entries it
// is a query to narrow, and the pane says so rather than filling memory with
// a result nobody can read.
const resultRowCap = 5000

// resultSet is a path-set query's outcome: the leaves keyed by their place in
// the virtual tree, and what the pane has to tell the reader about them.
type resultSet struct {
	leaves map[string]resultLeaf
	// dirs are virtual paths the query itself said are directories (it
	// selected `is_dir`). Without them a directory row would be a leaf that
	// pretends to be a file and fails when something tries to read it; the
	// directories *above* a leaf need no such declaration, being implied by
	// the leaf's path.
	dirs map[string]struct{}
	rows int
	// dirRows counts those directory rows, for the pane's summary.
	dirRows int
	// locs is every distinct snapshot the rows came from, in first-seen
	// order; more than one means the tree carries a location prefix.
	locs      []location
	truncated bool
	// dropped counts rows naming no snapshot: the query selected no `mount`
	// or `snap` and the window had no pane location to anchor them to. They
	// are a real answer to a question the pane cannot place, so it says so
	// rather than showing an empty tree.
	dropped int
}

// runPathSet runs a caller's query and reads its rows as a path set.
//
// The read-only check is the boundary, not a formality: the SQL arrived in a
// launch config from another app and runs against the operator's own
// ClickHouse credentials, while ADR-0200 §SD5 makes "reads, never writes" a
// property of this app. A statement that cannot be proven read-only is
// refused with its classification named, the same posture play's dispatch
// takes before it moves a statement anywhere.
//
// A row's location is its `mount` and `snap` columns where the query
// selected them, and the anchor otherwise — so a query over one pinned
// snapshot need not carry them, and one over `fs('*')` places its rows
// correctly when it does.
func runPathSet(ctx context.Context, exec recordstore.ExecutorI, sql string, anchor location, label func(identifier.TaggedId) string) (out resultSet, err error) {
	if kind := analysis.ClassifyStatementKind(sql); kind != analysis.KindReadOnly {
		err = eb.Build().Str("kind", kind.String()).
			Errorf("the query is not provably read-only, so this window will not run it")
		return
	}
	expanded, err := ladingsql.Expand(infoVisibility, sql)
	if err != nil {
		return
	}
	acc := newPathSetAccumulator(normalizeLocation(anchor), label)
	for rec, qerr := range exec.QueryArrow(ctx, expanded) {
		if qerr != nil {
			err = qerr
			return
		}
		aerr := acc.add(rec)
		rec.Release()
		if aerr != nil {
			err = aerr
			return
		}
		if acc.full() {
			break
		}
		if ctx.Err() != nil {
			err = eh.Errorf("%w", ctx.Err())
			return
		}
	}
	out = acc.result()
	return
}

// pathSetAccumulator folds record batches into leaves. It keeps the prefix
// decision it has made per location, so a batch boundary cannot rename a
// subtree half way through a result.
type pathSetAccumulator struct {
	anchor  location
	label   func(identifier.TaggedId) string
	leaves  map[string]resultLeaf
	dirs    map[string]resultLeaf // the row's location is kept, for re-prefixing
	locs    []location
	seen    map[location]struct{}
	claimed map[string]location // prefix segment → the location holding it
	dropped int
}

func newPathSetAccumulator(anchor location, label func(identifier.TaggedId) string) (inst *pathSetAccumulator) {
	inst = &pathSetAccumulator{
		anchor:  anchor,
		label:   label,
		leaves:  make(map[string]resultLeaf, 64),
		dirs:    make(map[string]resultLeaf, 8),
		seen:    make(map[location]struct{}, 4),
		claimed: make(map[string]location, 4),
	}
	return
}

// full reports whether the cap is reached. Counted from the maps rather than
// from a running total, so two rows naming one path are one entry in the
// tree and one against the cap.
func (inst *pathSetAccumulator) full() bool {
	return len(inst.leaves)+len(inst.dirs) >= resultRowCap
}

// add reads one batch. The `path` column is required; a batch without it is
// the caller's mistake and is reported as one rather than yielding an empty
// pane.
func (inst *pathSetAccumulator) add(rec arrow.RecordBatch) (err error) {
	schema := rec.Schema()
	pathIdx := fieldIndex(schema, "path")
	if pathIdx < 0 {
		err = eh.Errorf("the query has no `path` column, so its rows do not name files")
		return
	}
	mountIdx := fieldIndex(schema, "mount")
	snapIdx := fieldIndex(schema, "snap")
	dirIdx := fieldIndex(schema, "is_dir")
	n := int(rec.NumRows())
	for r := 0; r < n && !inst.full(); r++ {
		p, ok := stringAt(rec.Column(pathIdx), r)
		if !ok || p == "" {
			continue
		}
		loc := inst.anchor
		if mountIdx >= 0 {
			if v, has := uint64At(rec.Column(mountIdx), r); has {
				loc.mount = identifier.TaggedId(v)
			}
		}
		if snapIdx >= 0 {
			if t, has := timeAt(rec.Column(snapIdx), r); has {
				loc.snap = t
			}
		}
		if !loc.mount.IsValid() || loc.snap.IsZero() {
			inst.dropped++
			continue
		}
		loc = normalizeLocation(loc)
		inst.note(loc)
		virt := virtualPath(inst.prefixOf(loc), p)
		if dirIdx >= 0 && truthyAt(rec.Column(dirIdx), r) {
			inst.dirs[virt] = resultLeaf{loc: loc, real: p}
			continue
		}
		inst.leaves[virt] = resultLeaf{loc: loc, real: p}
	}
	return
}

// truthyAt reads a boolean cell. The lading surface projects `is_dir` as a
// ClickHouse UInt8, which Arrow carries as an integer rather than a boolean.
func truthyAt(col arrow.Array, row int) (yes bool) {
	if col.IsNull(row) {
		return false
	}
	switch a := col.(type) {
	case *array.Boolean:
		return a.Value(row)
	case *array.Uint8:
		return a.Value(row) != 0
	case *array.Int8:
		return a.Value(row) != 0
	case *array.Uint32:
		return a.Value(row) != 0
	case *array.Int32:
		return a.Value(row) != 0
	case *array.Uint64:
		return a.Value(row) != 0
	case *array.Int64:
		return a.Value(row) != 0
	}
	return false
}

// normalizeLocation rewrites the instant to a wall-clock-only UTC time. A
// location is a map key here, and two Times naming one instant compare
// unequal when one of them carries a monotonic reading or another zone — the
// anchor comes from the mount list and a row's comes from Arrow, so without
// this a single snapshot could look like two and the tree would grow a
// prefix nobody asked for.
func normalizeLocation(loc location) (out location) {
	out = loc
	if loc.snap.IsZero() {
		// The zero instant means "no snapshot" to every caller here, and the
		// epoch does not; keep it distinguishable.
		return
	}
	out.snap = time.Unix(0, loc.snap.UnixNano()).UTC()
	return
}

func (inst *pathSetAccumulator) note(loc location) {
	if _, ok := inst.seen[loc]; ok {
		return
	}
	inst.seen[loc] = struct{}{}
	inst.locs = append(inst.locs, loc)
}

// prefixOf is the tree segment a location's rows sit under, or "" while the
// result stands on one snapshot. Two locations whose labels collide are told
// apart by falling back to the hex id for the later one — a prefix has to be
// unambiguous or the tree merges two snapshots into one subtree.
func (inst *pathSetAccumulator) prefixOf(loc location) (prefix string) {
	if len(inst.locs) < 2 {
		return ""
	}
	name := hexID(loc.mount)
	if inst.label != nil {
		name = inst.label(loc.mount)
	}
	prefix = sanitizeSegment(name) + "@" + loc.snap.UTC().Format("2006-01-02T15:04:05Z")
	if owner, taken := inst.claimed[prefix]; taken && owner != loc {
		prefix = sanitizeSegment(hexID(loc.mount)) + "@" + loc.snap.UTC().Format("2006-01-02T15:04:05Z")
	}
	inst.claimed[prefix] = loc
	return
}

// result finishes the fold. The prefix decision is per row and the first
// rows of a multi-location result were placed before that was known, so the
// leaves are rebuilt once the location count is final.
func (inst *pathSetAccumulator) result() (out resultSet) {
	out = resultSet{
		rows:      len(inst.leaves),
		dirRows:   len(inst.dirs),
		locs:      inst.locs,
		truncated: inst.full(),
		dropped:   inst.dropped,
	}
	if len(inst.locs) < 2 {
		out.leaves, out.dirs = inst.leaves, keySet(inst.dirs)
		return
	}
	rebuilt := make(map[string]resultLeaf, len(inst.leaves))
	rebuiltDirs := make(map[string]struct{}, len(inst.dirs))
	inst.claimed = make(map[string]location, len(inst.locs))
	for _, leaf := range inst.leaves {
		rebuilt[virtualPath(inst.prefixOf(leaf.loc), leaf.real)] = leaf
	}
	for _, d := range inst.dirs {
		rebuiltDirs[virtualPath(inst.prefixOf(d.loc), d.real)] = struct{}{}
	}
	out.leaves, out.dirs = rebuilt, rebuiltDirs
	return
}

func keySet(m map[string]resultLeaf) (out map[string]struct{}) {
	out = make(map[string]struct{}, len(m))
	for k := range m {
		out[k] = struct{}{}
	}
	return
}

func fieldIndex(schema *arrow.Schema, name string) (idx int) {
	for i, f := range schema.Fields() {
		if f.Name == name {
			return i
		}
	}
	return -1
}

// stringAt reads a text cell. The lading surface hands `path` back as the
// natural key, which arrives as binary or as a string depending on how the
// caller projected it; both are the same bytes.
func stringAt(col arrow.Array, row int) (s string, ok bool) {
	if col.IsNull(row) {
		return
	}
	switch a := col.(type) {
	case *array.String:
		return a.Value(row), true
	case *array.LargeString:
		return a.Value(row), true
	case *array.Binary:
		return string(a.Value(row)), true
	case *array.LargeBinary:
		return string(a.Value(row)), true
	case *array.Dictionary:
		return stringAt(a.Dictionary(), a.GetValueIndex(row))
	}
	return
}

func uint64At(col arrow.Array, row int) (v uint64, ok bool) {
	if col.IsNull(row) {
		return
	}
	switch a := col.(type) {
	case *array.Uint64:
		return a.Value(row), true
	case *array.Int64:
		return uint64(a.Value(row)), true
	case *array.Uint32:
		return uint64(a.Value(row)), true
	case *array.Dictionary:
		return uint64At(a.Dictionary(), a.GetValueIndex(row))
	}
	return
}

// timeAt reads a snapshot instant. `snap` is the entry row's timestamp, which
// the surface projects as a ClickHouse DateTime64 and Arrow carries as a
// timestamp; an integer column is read as Unix nanoseconds, which is the
// spelling the query templates use.
func timeAt(col arrow.Array, row int) (t time.Time, ok bool) {
	if col.IsNull(row) {
		return
	}
	switch a := col.(type) {
	case *array.Timestamp:
		dt, valid := a.DataType().(*arrow.TimestampType)
		if !valid {
			return
		}
		return a.Value(row).ToTime(dt.Unit).UTC(), true
	case *array.Int64:
		return time.Unix(0, a.Value(row)).UTC(), true
	case *array.Uint64:
		return time.Unix(0, int64(a.Value(row))).UTC(), true
	case *array.Date64:
		return a.Value(row).ToTime().UTC(), true
	}
	return
}

// resultsSummary is the line under the Results pane's tree.
func (rs resultSet) summary(label string) (text string) {
	if label == "" {
		label = "query result"
	}
	text = fmt.Sprintf("%s · %d files", label, rs.rows)
	if rs.dirRows > 0 {
		text += fmt.Sprintf(" · %d directories", rs.dirRows)
	}
	if len(rs.locs) > 1 {
		text += fmt.Sprintf(" · %d snapshots", len(rs.locs))
	}
	if rs.truncated {
		text += fmt.Sprintf(" · stopped at %d — narrow the query to see the rest", resultRowCap)
	}
	if rs.dropped > 0 {
		text += fmt.Sprintf(" · %d rows name no snapshot", rs.dropped)
	}
	return
}

// resultFSOf builds the browsable tree over a result, reading each snapshot
// through the connection's cached adapter views.
func (inst *App) resultFSOf(sc *storeConn, rs resultSet) (fsys *resultFS) {
	return newResultFS(func(loc location) (fs.FS, error) {
		return sc.view(loc.mount, loc.snap)
	}, rs.leaves, rs.dirs)
}

// renderResults is the Results pane: the passed query's rows as a tree. It is
// the same browser the snapshot panes use, over the virtual file system, and
// the selection it reports is written back onto the pane so the lower tabs —
// Preview, Info, History — describe the row without knowing where it came
// from.
func (inst *App) renderResults(sc *storeConn) {
	p := &inst.panes[paneIDR]
	anchor, hasAnchor := inst.locationOf(&inst.panes[paneIDA])
	key := "results:" + inst.querySql
	if hasAnchor {
		key += "@" + anchor.key()
	}
	rs, done, qerr, busy := inst.resultLane.demand(key, func(ctx context.Context) (resultSet, error) {
		return runPathSet(ctx, sc.exec, inst.querySql, anchor, inst.mountLabel)
	})
	if busy {
		c.RequestRepaint()
		for range c.HorizontalTop().KeepIter() {
			c.Spinner().Send()
			c.Label("Running the query…").Send()
		}
		return
	}
	if !done {
		return
	}
	if qerr != nil {
		c.Label("The query did not run: " + qerr.Error()).Send()
		return
	}
	if inst.resultKey != key {
		inst.resultSet, inst.resultFS, inst.resultKey = rs, inst.resultFSOf(sc, rs), key
		p.st.SetDir(".")
		p.followLatest = false
	}
	c.Label(rs.summary(inst.queryLabel)).Selectable(false).Send()
	if rs.rows == 0 && rs.dirRows == 0 {
		if rs.dropped > 0 {
			c.Label("The query returned rows, but none of them names a snapshot: select `mount` and `snap` beside `path`, or open the window on a mount so the rows have somewhere to sit.").Send()
			return
		}
		c.Label("The query returned no rows.").Send()
		return
	}
	seq := c.ProbeSeq("tally-pane", paneIDR.String()) ^ inst.ids.PrepareHighEntropy(panePaneProbeSalt).Derive()
	if _, h, ok := c.CapturePaneSize(seq); ok && h > 0 && !math.IsNaN(float64(h)) {
		p.paneH = h
	}
	label := inst.queryLabel
	if label == "" {
		label = "query result"
	}
	res := fsbrowser.Render(fsbrowser.Input{
		Ids:        inst.ids,
		ScopeKey:   "pane-results",
		FS:         inst.resultFS,
		RootLabel:  label,
		CacheKey:   key,
		State:      &p.st,
		Mode:       p.mode,
		ShowHidden: true,
		Striped:    true,
		Widths:     inst.colWidths,
		WidthTag:   "pane-results",
		MaxHeight:  p.paneH,
	})
	if res.Err != nil {
		inst.status = res.Err.Error()
	}
	if res.Clicked >= 0 || res.Activated >= 0 || res.Navigated {
		inst.focus = paneIDR
	}
	inst.applyPendingSelection(p, res)
	inst.trackResultSelection(p, res)
}

// trackResultSelection points the pane at the snapshot behind the selected
// row. A directory or a multi-selection points it nowhere, which is what the
// snapshot panes do too.
func (inst *App) trackResultSelection(p *pane, res fsbrowser.Result) {
	virt := ""
	if sel := p.st.Selection(); len(sel) == 1 {
		virt = sel[0]
	}
	if res.Activated >= 0 && res.Activated < len(res.Rows) {
		virt = res.Rows[res.Activated].Path
	}
	leaf, ok := inst.resultFS.Leaf(virt)
	if !ok {
		p.selected = ""
		return
	}
	p.mount, p.snap, p.followLatest = leaf.loc.mount, leaf.loc.snap, false
	p.selected = leaf.real
}
