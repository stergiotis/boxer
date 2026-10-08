package play

import (
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsbrowser"
)

// The Files pane as an agent reads and sets it (ADR-0270, update of
// 2026-10-05). The read lists a directory of the tree the pane interned from
// its result — or the paths under it a filter matches — with the counts the
// status line reports, and the pane's view: directory, mode, filter, hidden
// names, order and selection. The tree is immutable once built, so the
// snapshot shares it and the listing runs in the query. The options command
// sets the view; select_files_path selects one path as a click does and
// writes selection_key, and selection where a row named the path.

const (
	opGetFiles            = "get_files"
	opSetFilesOptions     = "set_files_options"
	opSelectFilesPath     = "select_files_path"
	filesPaneId           = "files"
	opsResFiles           = filesPaneId
	filesReadDefaultLimit = 50
	filesReadMaxLimit     = 200
	filesPathMaxBytes     = 512
)

var (
	filesModeNames = []string{"list", "outline"}
	filesSortNames = []string{"name", "size", "mtime"}
)

// FilesEntryReading is one entry of a listing.
type FilesEntryReading struct {
	Path     string `desc:"the entry's path in the tree, '/' separated, no leading slash; select_files_path takes it"`
	Dir      bool   `json:",omitzero" desc:"a directory"`
	Made     bool   `json:",omitzero" desc:"a directory no row named: the tree made it to hold the paths under it, so it has no row, size or time"`
	Symlink  bool   `json:",omitzero" desc:"a symbolic link"`
	Row      *int64 `json:",omitzero" desc:"the result row that named it; sample_rows reads its other columns"`
	Size     int64  `json:",omitzero" desc:"the size the row gave"`
	Modified string `json:",omitzero" desc:"the modification time the row gave, UTC"`
	Children int32  `json:",omitzero" desc:"a directory's entries"`
}

// FilesReading is get_files' result.
type FilesReading struct {
	Drawn         PaneDraw            `desc:"which draw this is of, its status line, and why it drew nothing when it did not"`
	Files         int64               `desc:"files in the tree"`
	Dirs          int64               `desc:"directories in the tree, the made ones included"`
	NotInterned   int64               `json:",omitzero" desc:"rows past the browser's cap that were not interned"`
	NoPath        int64               `json:",omitzero" desc:"rows whose path could not be read as one"`
	Dir           string              `desc:"the pane's current directory; '.' is the root"`
	Mode          string              `desc:"list or outline"`
	Filter        string              `json:",omitzero" desc:"the pane's quick filter: a case-insensitive RE2 matched anywhere in the paths under the directory"`
	FilterLiteral bool                `json:",omitzero" desc:"the filter did not compile as a pattern and matches as plain text"`
	Hidden        bool                `json:",omitzero" desc:"names starting with a dot are shown"`
	SortBy        string              `desc:"the order: name, size or mtime"`
	Descending    bool                `json:",omitzero" desc:"the order is reversed"`
	Selection     []string            `json:",omitzero" desc:"the selected paths"`
	Listed        string              `desc:"the directory this listing is of"`
	ListFilter    string              `json:",omitzero" desc:"the filter this listing applied, over every path under the directory at any depth"`
	Entries       []FilesEntryReading `json:",omitzero" desc:"the entries, directories first in the pane's order"`
	Total         int32               `desc:"entries the listing found"`
	More          int32               `json:",omitzero" desc:"entries past the ones listed; read on with offset"`
}

// GetFilesArgs is get_files' argument.
type GetFilesArgs struct {
	Dir    string `json:",omitzero" desc:"the directory to list; left out, the pane's current one"`
	Filter string `json:",omitzero" desc:"a case-insensitive RE2 over the paths under the directory, at any depth, as the pane's filter matches; left out, the immediate entries (the pane's own filter is not applied)"`
	Offset int32  `json:",omitzero" desc:"entries to skip"`
	Limit  int32  `json:",omitzero" desc:"entries to list, 50 by default and at most 200"`
}

// SetFilesOptionsArgs is set_files_options' argument.
type SetFilesOptionsArgs struct {
	Dir        *string `json:",omitzero" desc:"the directory to show; '.' is the root. Moving clears the selection, as the browser does"`
	Mode       *string `json:",omitzero" desc:"list or outline"`
	Filter     *string `json:",omitzero" desc:"the quick filter, a case-insensitive RE2 over the paths under the directory; empty clears it"`
	Hidden     *bool   `json:",omitzero" desc:"show names starting with a dot"`
	SortBy     *string `json:",omitzero" desc:"name, size or mtime"`
	Descending *bool   `json:",omitzero" desc:"reverse the order"`
}

// SelectFilesPathArgs is select_files_path's argument.
type SelectFilesPathArgs struct {
	Path  string `json:",omitzero" desc:"the path to select, as get_files gives it; the pane moves to its directory"`
	Clear bool   `json:",omitzero" desc:"clear the pane's selection; the signals keep their values"`
}

// filesOpsView is what get_files reads: the tree, shared (it is never
// changed once built), and the view as it stands.
type filesOpsView struct {
	fsys          *rowFS
	status        string
	dir           string
	mode          fsbrowser.ModeE
	filter        string
	filterLiteral bool
	hidden        bool
	sortBy        fsbrowser.SortByE
	desc          bool
	selection     []string
}

func (inst *PlayApp) filesView() filesOpsView {
	d := inst.filesDriver
	by, desc := d.st.Sort()
	return filesOpsView{fsys: d.fsys, status: d.statusLine(), dir: d.st.Dir(), mode: d.mode, filter: d.st.Filter(),
		filterLiteral: d.st.FilterLiteral(), hidden: d.showHidden, sortBy: by, desc: desc, selection: d.st.Selection()}
}

// filesEntryOf reads one node of the tree.
func filesEntryOf(n *rowNode) (e FilesEntryReading) {
	e = FilesEntryReading{Path: truncateBytes(n.fullPath, filesPathMaxBytes), Dir: n.isDir, Symlink: n.symlink, Size: n.size}
	if n.row >= 0 {
		row := n.row
		e.Row = &row
		if !n.mtime.IsZero() {
			e.Modified = n.mtime.UTC().Format(time.DateTime)
		}
	} else {
		e.Made = n.isDir
		e.Size = 0
	}
	if n.isDir {
		e.Children = int32(len(n.sortedChildren()))
	}
	return
}

// filesOrder sorts nodes as the browser lists them: directories first, then
// by the pane's key, names breaking ties.
func filesOrder(nodes []*rowNode, by fsbrowser.SortByE, desc bool) {
	slices.SortStableFunc(nodes, func(a, b *rowNode) int {
		if a.isDir != b.isDir {
			if a.isDir {
				return -1
			}
			return 1
		}
		c := 0
		switch by {
		case fsbrowser.SortBySize:
			c = compareOrdered(a.size, b.size)
		case fsbrowser.SortByModTime:
			c = a.mtime.Compare(b.mtime)
		}
		if c == 0 {
			c = strings.Compare(a.name, b.name)
		}
		if desc {
			c = -c
		}
		return c
	})
}

// filesHiddenName reports a dot name, which the browser hides unless asked.
func filesHiddenName(name string) bool { return strings.HasPrefix(name, ".") && name != "." }

// filesMatcher compiles a filter as the browser does: case-insensitive, a
// pattern that does not compile matching as a literal.
func filesMatcher(src string) *regexp.Regexp {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil
	}
	if re, err := regexp.Compile("(?i)" + src); err == nil {
		return re
	}
	return regexp.MustCompile("(?i)" + regexp.QuoteMeta(src))
}

// filesCleanDir is a directory argument as an io/fs path.
func filesCleanDir(dir string) string {
	dir = path.Clean(strings.TrimPrefix(strings.TrimSpace(dir), "/"))
	if dir == "" || dir == "/" {
		return "."
	}
	return dir
}

// filesReading is get_files.
func filesReading(sn *opsSnap, in GetFilesArgs) (out FilesReading, err error) {
	d, readable, err := paneDrawOf(sn, filesPaneId)
	if err != nil {
		return
	}
	v := &sn.paneViews.files
	out = FilesReading{Drawn: d, Dir: v.dir, Mode: nameAt(filesModeNames, int(v.mode)), Filter: v.filter,
		FilterLiteral: v.filterLiteral, Hidden: v.hidden, SortBy: nameAt(filesSortNames, int(v.sortBy)), Descending: v.desc,
		Selection: v.selection}
	fs := v.fsys
	if !readable || fs == nil {
		return
	}
	out.Files, out.Dirs, out.NotInterned, out.NoPath = fs.files, fs.dirs, fs.dropped, fs.skipped
	limit := in.Limit
	switch {
	case limit < 0 || limit > filesReadMaxLimit:
		return out, app.RefuseOperation("limit is at most " + strconv.Itoa(filesReadMaxLimit))
	case limit == 0:
		limit = filesReadDefaultLimit
	}
	dir := v.dir
	if in.Dir != "" {
		dir = filesCleanDir(in.Dir)
	}
	n, ok := fs.byPath[dir]
	if !ok {
		return out, app.RefuseOperation("the tree has no directory " + strconv.Quote(dir))
	}
	if !n.isDir {
		return out, app.RefuseOperation(strconv.Quote(dir) + " is a file, not a directory")
	}
	out.Listed = dir
	var nodes []*rowNode
	if re := filesMatcher(in.Filter); re != nil {
		out.ListFilter = in.Filter
		var walk func(n *rowNode)
		walk = func(n *rowNode) {
			for _, kid := range n.sortedChildren() {
				if !v.hidden && filesHiddenName(kid.name) {
					continue
				}
				if re.MatchString(kid.fullPath) {
					nodes = append(nodes, kid)
				}
				if kid.isDir {
					walk(kid)
				}
			}
		}
		walk(n)
	} else {
		for _, kid := range n.sortedChildren() {
			if !v.hidden && filesHiddenName(kid.name) {
				continue
			}
			nodes = append(nodes, kid)
		}
	}
	filesOrder(nodes, v.sortBy, v.desc)
	out.Total = int32(len(nodes))
	off := int(in.Offset)
	if off < 0 || (off > 0 && off >= len(nodes)) {
		return out, app.RefuseOperation("the listing has " + strconv.Itoa(len(nodes)) + " entries; offset " + strconv.Itoa(off) + " is past them")
	}
	used := 0
	for i := off; i < len(nodes); i++ {
		e := filesEntryOf(nodes[i])
		used += 48 + len(e.Path)
		if i-off >= int(limit) || (used > opsSampleMaxBytes && i > off) {
			out.More = int32(len(nodes) - i)
			break
		}
		out.Entries = append(out.Entries, e)
	}
	return
}

// setOptions is set_files_options on the driver: everything named is
// checked before anything is applied.
func (inst *filesDriver) setOptions(in SetFilesOptionsArgs) (err error) {
	if in.Dir == nil && in.Mode == nil && in.Filter == nil && in.Hidden == nil && in.SortBy == nil && in.Descending == nil {
		return noOptionsRefusal(filesPaneId, "dir", "mode", "filter", "hidden", "sort_by", "descending")
	}
	mode, sortBy := -1, -1
	if in.Mode != nil {
		if mode = nameIndex(filesModeNames, *in.Mode); mode < 0 {
			return app.RefuseOperation("mode is list or outline")
		}
	}
	if in.SortBy != nil {
		if sortBy = nameIndex(filesSortNames, *in.SortBy); sortBy < 0 {
			return app.RefuseOperation("sort_by is name, size or mtime")
		}
	}
	var dir string
	if in.Dir != nil {
		if inst.fsys == nil {
			return app.RefuseOperation("the Files pane has not interned a result: run a query with a path column, and show_pane files")
		}
		dir = filesCleanDir(*in.Dir)
		n, ok := inst.fsys.byPath[dir]
		if !ok || !n.isDir {
			return app.RefuseOperation("the tree has no directory " + strconv.Quote(dir) + "; get_files lists them")
		}
	}
	if mode >= 0 {
		inst.mode = fsbrowser.ModeE(mode)
	}
	if in.Hidden != nil {
		inst.showHidden = *in.Hidden
	}
	if in.Filter != nil {
		inst.st.SetFilter(*in.Filter)
	}
	if sortBy >= 0 || in.Descending != nil {
		by, desc := inst.st.Sort()
		if sortBy >= 0 {
			by = fsbrowser.SortByE(sortBy)
		}
		if in.Descending != nil {
			desc = *in.Descending
		}
		inst.st.SetSort(by, desc)
	}
	if in.Dir != nil {
		inst.st.SetDir(dir)
	}
	return
}

// requestOptions is a mode button, or a change the browser made inside the
// frame (a directory, the order): through set_files_options as the person's
// gesture where play's launcher routes it, directly otherwise.
func (inst *filesDriver) requestOptions(in SetFilesOptionsArgs) {
	if inst.onOptions != nil {
		inst.onOptions(in)
		return
	}
	_ = inst.setOptions(in)
}

// pinFor resolves select_files_path's argument against the tree: the path
// to select, or "" to clear.
func (inst *filesDriver) pinFor(in SelectFilesPathArgs) (p string, err error) {
	if in.Clear {
		if in.Path != "" {
			return "", app.RefuseOperation("give a path or clear, not both")
		}
		return "", nil
	}
	if in.Path == "" {
		return "", app.RefuseOperation("name the path to select, or clear")
	}
	if inst.fsys == nil {
		return "", app.RefuseOperation("the Files pane has not interned a result: run a query with a path column, and show_pane files")
	}
	p = filesCleanDir(in.Path)
	if p == "." {
		return "", app.RefuseOperation("the root is the result itself, which the browser does not select")
	}
	if _, ok := inst.fsys.byPath[p]; !ok {
		return "", app.RefuseOperation("the tree has no path " + strconv.Quote(p) + "; get_files lists the entries")
	}
	return
}

// selectFilesPath is select_files_path: the browser moves to the path's
// directory and selects it, and selection_key — and selection, when a row
// named the path — are written with writer, as the pane's click publishes
// them. The pane's own publish then finds them in place.
func (inst *PlayApp) selectFilesPath(in SelectFilesPathArgs, writer string) (err error) {
	d := inst.filesDriver
	p, err := d.pinFor(in)
	if err != nil {
		return
	}
	if p == "" {
		d.st.ClearSelection()
		d.emitted = ""
		return
	}
	dir := d.st.Dir()
	under := dir == "." || strings.HasPrefix(p, dir+"/")
	if path.Dir(p) != dir && !(d.mode == fsbrowser.ModeOutline && under) {
		d.st.SetDir(path.Dir(p))
	}
	d.st.SelectOnly(p)
	d.emitted = p
	inst.graph.setSignalRawFrom(signalSelectionKey, p, writer)
	if row := d.fsys.rowOf(p); row >= 0 {
		// Counted as followed: the selection now names the row the browser
		// shows, so the follow does not move the browser again.
		d.followed = row
		inst.graph.setSignalRawFrom(signalSelection, strconv.FormatInt(row, 10), writer)
	}
	return
}

// filesOptionsDigest is the files resource: the view and the selection.
func filesOptionsDigest(p *PlayApp) string {
	d := p.filesDriver
	if d == nil {
		return ""
	}
	by, desc := d.st.Sort()
	return "dir=" + d.st.Dir() + "|m=" + strconv.Itoa(int(d.mode)) + "|f=" + d.st.Filter() + "|h=" + strconv.FormatBool(d.showHidden) +
		"|s=" + strconv.Itoa(int(by)) + ":" + strconv.FormatBool(desc) + "|sel=" + strings.Join(d.st.Selection(), "\x00")
}

func addFilesOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	addPaneOps(s, paneOpsSpec[FilesReading, GetFilesArgs, SetFilesOptionsArgs]{
		pane:     filesPaneId,
		resource: "the Files pane's view: the directory, mode, filter, hidden names, order and selection",
		digest:   filesOptionsDigest,
		get:      opGetFiles,
		getSummary: "list a directory of the tree the Files pane interned from its result, or the paths under it a filter matches, " +
			"with each entry's row, size and time, the counts the status line gives, and the pane's view",
		set:        opSetFilesOptions,
		setSummary: "set the Files pane's directory, mode, quick filter, hidden names or order",
		gesture:    "the List and Outline buttons, moving between directories and sorting by a column header in the browser",
		follows:    []string{"the pane draws with the new view from its next frame; the result is not rerun"},
		read:       filesReading,
		apply:      func(p *PlayApp, in SetFilesOptionsArgs) error { return p.filesDriver.setOptions(in) },
	})
	appops.Command(s, app.OperationSpec{Name: opSelectFilesPath, Version: 1,
		Summary: "select one path in the Files pane, as a click does: the pane moves to its directory, and selection_key is written, with selection when a row named the path",
		Effect:  app.OperationEffectDocument, Writes: []string{opsResSignals, opsResFiles}, Agents: true,
		Gesture: "clicking an entry of the browser",
		Follows: []string{"the Detail pane shows the row; Live reruns a query that reads selection or selection_key"}},
		func(inst *PlayLauncher, call app.OperationCall, in SelectFilesPathArgs) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			if err := p.selectFilesPath(in, paneSignalWriter(call, filesPaneId)); err != nil {
				return appops.None{}, err
			}
			p.markAgent(call.OnBehalfOf)
			return appops.None{}, nil
		})
}
