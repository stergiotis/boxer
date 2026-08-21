package fsbrowser

import (
	"errors"
	"math"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
)

const (
	defaultRowHeight   float32 = 22
	defaultNameWidth   float32 = 320
	defaultSizeWidth   float32 = 90
	defaultTimeWidth   float32 = 140
	defaultColumnWidth float32 = 120
	rowOutlineInset    float32 = 1
	filterWidth        float32 = 180

	// Widget-id seeds, one namespace per id kind (ADR-0200 in the prefix).
	seqRowBase    uint64 = 0x0200_0100_0000_0000
	seqCellBase   uint64 = 0x0200_0200_0000_0000
	seqHeaderBase uint64 = 0x0200_0300_0000_0000
	seqCrumbBase  uint64 = 0x0200_0400_0000_0000
)

var (
	selectionFill   = color.Hex(styletokens.AccentSubtle.AsHex())
	selectionStroke = color.Hex(styletokens.AccentDefault.AsHex())
	stripeFill      = color.Hex(styletokens.NeutralBgFaint.AsHex())
	clearFill       = color.Transparent
)

// builtinColumns is how many columns precede the host's: name, size, time.
const builtinColumns = 3

// Render draws the browser for one frame and reports what happened.
func Render(in Input) (res Result) {
	res.Clicked, res.Activated = -1, -1
	if in.Ids == nil || in.State == nil {
		res.Err = errors.New("fsbrowser: Render needs a non-nil Ids and State")
		return
	}
	st := in.State
	st.ensure()
	if st.rekey(in.CacheKey) {
		res.SelectionChanged = true
	}
	scopeKey := in.ScopeKey
	if scopeKey == "" {
		scopeKey = "fsbrowser"
	}
	density := styletokens.DensityFromEnv()
	for range c.IdScope(in.Ids.PrepareStr(scopeKey)) {
		if !in.HideBreadcrumb {
			if in.renderBreadcrumb(st, density) {
				res.Navigated = true
				res.SelectionChanged = true
			}
		}
		if !in.HideFilter {
			in.renderFilter(st, density)
		}
		if in.FS == nil {
			c.Label("No file system").Send()
			return
		}
		switch in.Mode {
		case ModeOutline:
			in.renderOutline(st, density, &res)
		default:
			in.renderList(st, density, &res)
		}
	}
	return
}

// renderBreadcrumb draws the up button, the root and one button per path
// segment; reports true when it navigated.
func (in Input) renderBreadcrumb(st *State, density styletokens.DensityE) (navigated bool) {
	root := in.RootLabel
	if root == "" {
		root = "/"
	}
	for range c.Horizontal().KeepIter() {
		up := c.Button(in.Ids.PrepareSeq(seqCrumbBase), c.Atoms().Text(icons.PhArrowUp).Keep()).
			Frame(false).Small()
		if up.SendResp().HasPrimaryClicked() && st.Up() {
			navigated = true
		}
		c.AddSpace(styletokens.GapInline(density))
		if c.Button(in.Ids.PrepareSeq(seqCrumbBase+1), c.Atoms().BeginRichText(root).Strong().End().Keep()).
			Frame(false).Small().SendResp().HasPrimaryClicked() {
			if st.Dir() != "." {
				st.SetDir(".")
				navigated = true
			}
		}
		if st.Dir() != "." {
			segs := strings.Split(st.Dir(), "/")
			prefix := ""
			for i, seg := range segs {
				if prefix == "" {
					prefix = seg
				} else {
					prefix += "/" + seg
				}
				c.Label("›").Selectable(false).Send()
				last := i == len(segs)-1
				atoms := c.Atoms().Text(seg).Keep()
				if last {
					atoms = c.Atoms().BeginRichText(seg).Strong().End().Keep()
				}
				if c.Button(in.Ids.PrepareSeq(seqCrumbBase+2+uint64(i)), atoms).
					Frame(false).Small().SendResp().HasPrimaryClicked() && !last {
					st.SetDir(prefix)
					navigated = true
				}
			}
		}
	}
	return
}

// renderFilter draws the quick filter. The TextEdit binds to the State's own
// field — a stable pointer, because the databinding lands a frame late.
func (in Input) renderFilter(st *State, density styletokens.DensityE) {
	for range c.Horizontal().KeepIter() {
		c.Label(icons.PhFunnelSimple).Selectable(false).Send()
		c.TextEdit(in.Ids.PrepareStr("filter"), st.filter, false).
			HintText("filter names").
			DesiredWidth(filterWidth).
			SendRespVal(&st.filter)
		if st.filter != "" {
			if c.Button(in.Ids.PrepareStr("filter-clear"), c.Atoms().Text(icons.PhX).Keep()).
				Frame(false).Small().SendResp().HasPrimaryClicked() {
				st.filter = ""
				c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&st.filter)
			}
		}
		c.AddSpace(styletokens.GapInline(density))
		c.Label(st.filterSummary()).Selectable(false).Send()
	}
}

func (st *State) filterSummary() string {
	if n := len(st.selected); n > 0 {
		if n == 1 {
			return "1 selected"
		}
		return itoa(n) + " selected"
	}
	return ""
}

// renderList is list mode: the current directory as an etable inside a key
// capture Frame (the tree widget's shape, ADR-0176 / ADR-0177).
func (in Input) renderList(st *State, density styletokens.DensityE, res *Result) {
	rowH := in.RowHeight
	if rowH <= 0 {
		rowH = defaultRowHeight
	}

	// Last frame's keys, applied to the view the reader saw (st.rows is the
	// previous frame's listing) BEFORE this frame's directory is read — so a
	// Backspace or an Enter on a directory renders the new directory in this
	// frame rather than a blank one.
	if st.keyFrameID != 0 {
		prev := st.rows
		ki := applyKeys(st, prev, st.keyFrameID, st.lastVisibleRows)
		switch {
		case ki.up:
			if st.Up() {
				res.Navigated, res.SelectionChanged = true, true
			}
		case ki.activate:
			if r := rowOfPath(prev, st.cursor); r >= 0 {
				in.activate(st, prev, r, res)
			}
		case ki.moved && ki.row >= 0 && ki.row < len(prev):
			st.SelectOnly(prev[ki.row].Path)
			res.SelectionChanged = true
		}
	}

	l := st.read(in.FS, st.Dir())
	rows := st.view(l, in.ShowHidden, st.rows[:0])
	st.rows = rows
	res.Rows = rows
	if l.err != nil {
		c.Label("Cannot read " + st.Dir() + ": " + l.err.Error()).Selectable(false).Send()
		res.Err = l.err
	}

	clickedRow, activatedRow := -1, -1
	mode := clickMode()
	kf := c.Frame(in.Ids.PrepareStr("keys")).CaptureKeys(uint64(listKeyMask))
	st.keyFrameID = kf.Id()
	for range kf.KeepIter() {
		in.pushColumns()
		et := c.EndETable(in.Ids.PrepareStr("t"), uint64(len(rows)), rowH, 1, 0)
		if in.MaxHeight > 0 {
			et = et.MaxHeight(in.MaxHeight)
		}
		in.renderHeaders(et, st, density)
		rowBegin, rowEnd := 0, len(rows)
		if rb, re, _, _, _, ok := et.VisibleRange(); ok {
			rowBegin = min(int(rb), len(rows))
			rowEnd = min(int(re), rowEnd)
		}
		st.lastVisibleRows = rowEnd - rowBegin
		for i := rowBegin; i < rowEnd; i++ {
			e := rows[i]
			flags := in.rowChrome(et, i, e, rowH, st.IsSelected(e.Path))
			if flags.HasPrimaryClicked() {
				clickedRow = i
			}
			if flags.HasDoubleClicked() {
				activatedRow = i
			}
			et.BeginCells(uint64(i), 0)
			in.paddedCell(e, 0, density, func(e Entry) { nameCell(e) })
			et.EndCells()
			et.BeginCells(uint64(i), 1)
			in.paddedCell(e, 1, density, sizeCell)
			et.EndCells()
			et.BeginCells(uint64(i), 2)
			in.paddedCell(e, 2, density, timeCell)
			et.EndCells()
			for ci := range in.Columns {
				if in.Columns[ci].Cell == nil {
					continue
				}
				et.BeginCells(uint64(i), uint32(builtinColumns+ci))
				in.paddedCell(e, builtinColumns+ci, density, in.Columns[ci].Cell)
				et.EndCells()
			}
		}
		et.Send()
	}
	if clickedRow >= 0 {
		applySelection(st, rows, clickedRow, mode)
		res.Clicked = clickedRow
		res.SelectionChanged = true
		if st.keyFrameID != 0 {
			c.RequestFocus(st.keyFrameID)
		}
	}
	if activatedRow >= 0 {
		in.activate(st, rows, activatedRow, res)
	}
}

// activate enters a directory or reports a file; true when it navigated.
func (in Input) activate(st *State, rows []Entry, row int, res *Result) (navigated bool) {
	e := rows[row]
	if e.IsDir {
		st.SetDir(e.Path)
		res.Navigated, res.SelectionChanged = true, true
		return true
	}
	res.Activated = row
	return false
}

func (in Input) pushColumns() {
	push := func(w, deflt float32, resizable bool) {
		if w <= 0 {
			w = deflt
		}
		c.EtColumn(w).RangeMinMax(w, float32(math.Inf(1))).Resizable(resizable).Send()
	}
	push(0, defaultNameWidth, true)
	push(0, defaultSizeWidth, true)
	push(0, defaultTimeWidth, true)
	for i := range in.Columns {
		push(in.Columns[i].Width, defaultColumnWidth, in.Columns[i].Resizable)
	}
}

// renderHeaders draws the three sortable headers and the host's. A header is
// a frameless button; its glyph says which column orders the listing, and
// which way.
func (in Input) renderHeaders(et c.EndETableFluid, st *State, density styletokens.DensityE) {
	sortable := func(col uint32, text string, by SortByE) {
		for range et.Headers(0, col) {
			for range c.Frame(in.Ids.PrepareSeq(seqHeaderBase + uint64(col))).
				OuterMargin(0).
				InnerMargin(styletokens.PaddingInner(density)).
				KeepIter() {
				label := text
				if st.sortBy == by {
					if st.sortDesc {
						label += " " + icons.PhCaretDown
					} else {
						label += " " + icons.PhCaretUp
					}
				}
				if c.Button(in.Ids.PrepareSeq(seqHeaderBase+0x100+uint64(col)),
					c.Atoms().BeginRichText(label).Strong().End().Keep()).
					Frame(false).Small().SendResp().HasPrimaryClicked() {
					if st.sortBy == by {
						st.sortDesc = !st.sortDesc
					} else {
						st.sortBy, st.sortDesc = by, false
					}
				}
			}
		}
	}
	sortable(0, "name", SortByName)
	sortable(1, "size", SortBySize)
	sortable(2, "modified", SortByModTime)
	for i := range in.Columns {
		col := uint32(builtinColumns + i)
		text := in.Columns[i].Header
		for range et.Headers(0, col) {
			for range c.Frame(in.Ids.PrepareSeq(seqHeaderBase + uint64(col))).
				OuterMargin(0).
				InnerMargin(styletokens.PaddingInner(density)).
				KeepIter() {
				c.LabelAtoms(c.Atoms().BeginRichText(text).Strong().End().Keep()).Selectable(false).Send()
			}
		}
	}
}

// rowChrome is the full-width row layer: fill, selection outline, click sense.
func (in Input) rowChrome(et c.EndETableFluid, rowIdx int, e Entry, rowH float32, selected bool) c.ResponseFlagsE {
	fill := clearFill
	switch {
	case selected:
		fill = selectionFill
	case in.Striped && rowIdx%2 == 1:
		fill = stripeFill
	}
	strokeWidth, stroke := float32(0), clearFill
	if selected {
		strokeWidth, stroke = 1.0, selectionStroke
	}
	var fr c.FrameFluid
	for range et.Rows(uint64(rowIdx)) {
		fr = c.Frame(in.Ids.PrepareSeq(seqRowBase+uint64(e.Ord))).
			Fill(fill).
			Stroke(strokeWidth, stroke).
			OuterMargin(0).
			InnerMargin(0).
			SenseClick().
			HoverCursorPointer()
		for range fr.KeepIter() {
			c.UiSetMinWidthAvailable()
			c.UiSetMinHeight(rowH - rowOutlineInset)
		}
	}
	return c.CurrentApplicationState.StateManager.GetResponseByIdRaw(fr.Id())
}

func (in Input) paddedCell(e Entry, col int, density styletokens.DensityE, body func(e Entry)) {
	ncols := uint64(builtinColumns + len(in.Columns))
	pad := styletokens.PaddingInner(density)
	for range c.Frame(in.Ids.PrepareSeq(seqCellBase+uint64(e.Ord)*ncols+uint64(col))).
		OuterMargin(0).
		InnerMarginSides(pad, pad, 0, 0).
		KeepIter() {
		body(e)
	}
}

// nameCell is the glyph and the name. Labels are non-selectable so the row's
// sense behind them keeps the click.
func nameCell(e Entry) {
	for range c.Horizontal().KeepIter() {
		c.Label(glyphFor(e)).Selectable(false).Send()
		c.Label(e.Name).Selectable(false).Truncate().Send()
	}
}

func sizeCell(e Entry) {
	if e.IsDir {
		return
	}
	c.Label(humanBytes(e.Size)).Selectable(false).Truncate().Send()
}

func timeCell(e Entry) {
	c.Label(formatTime(e.ModTime)).Selectable(false).Truncate().Send()
}

// renderOutline is outline mode: the tree under the current directory.
func (in Input) renderOutline(st *State, density styletokens.DensityE, res *Result) {
	t, nodes := st.buildOutline(in.FS, in.ShowHidden)
	res.Rows = nodes
	if t.Len() == 0 {
		return
	}
	cols := make([]tree.Column, 0, 2+len(in.Columns))
	cols = append(cols,
		tree.Column{Header: "size", Width: defaultSizeWidth, Resizable: true,
			Cell: func(r tree.Row) { sizeCell(nodes[r.Node]) }},
		tree.Column{Header: "modified", Width: defaultTimeWidth, Resizable: true,
			Cell: func(r tree.Row) { timeCell(nodes[r.Node]) }},
	)
	for i := range in.Columns {
		col := in.Columns[i]
		if col.Cell == nil {
			continue
		}
		cell := col.Cell
		cols = append(cols, tree.Column{Header: col.Header, Width: col.Width, Resizable: col.Resizable,
			Cell: func(r tree.Row) {
				if e := nodes[r.Node]; e.Ord >= 0 {
					cell(e)
				}
			}})
	}
	tr := tree.Render(tree.Input{
		Ids:      in.Ids,
		ScopeKey: "outline",
		Tree:     t,
		State:    &st.tree,
		Outline: tree.Column{Header: "name", Width: defaultNameWidth, Resizable: true,
			Cell: func(r tree.Row) {
				e := nodes[r.Node]
				if e.Ord < 0 {
					c.Label(e.Name).Selectable(false).Truncate().Send()
					return
				}
				nameCell(e)
			}},
		Columns:   cols,
		RowHeight: in.RowHeight,
		MaxHeight: in.MaxHeight,
		Striped:   in.Striped,
	})
	if tr.Err != nil {
		res.Err = tr.Err
		return
	}
	// Mirror the tree's selection and cursor onto the path-keyed State, so
	// State.Selection reads the same in both modes.
	sel := st.tree.Selection(nil)
	clear(st.selected)
	for _, n := range sel {
		if e := nodes[n]; e.Ord >= 0 {
			st.selected[e.Path] = struct{}{}
		}
	}
	if cur := st.tree.Cursor(); cur >= 0 && int(cur) < len(nodes) {
		st.cursor = nodes[cur].Path
	}
	if tr.Clicked >= 0 {
		res.Clicked = int(tr.Clicked)
		res.SelectionChanged = true
	}
	if tr.Toggled >= 0 {
		res.SelectionChanged = true
	}
	if tr.Activated >= 0 {
		if e := nodes[tr.Activated]; !e.IsDir && e.Ord >= 0 {
			res.Activated = int(tr.Activated)
		}
	}
}

type selectModeE uint8

const (
	selectModeReplace selectModeE = iota
	selectModeToggle
	selectModeExtend
)

func clickMode() selectModeE {
	mods := c.CurrentApplicationState.StateManager.GetModifiers()
	switch {
	case mods.Command || mods.Ctrl:
		return selectModeToggle
	case mods.Shift:
		return selectModeExtend
	}
	return selectModeReplace
}

func applySelection(st *State, rows []Entry, rowIdx int, mode selectModeE) {
	p := rows[rowIdx].Path
	switch mode {
	case selectModeToggle:
		st.Select(p, !st.IsSelected(p))
		st.cursor = p
	case selectModeExtend:
		anchor := rowOfPath(rows, st.cursor)
		if anchor < 0 {
			st.SelectOnly(p)
			return
		}
		lo, hi := anchor, rowIdx
		if lo > hi {
			lo, hi = hi, lo
		}
		clear(st.selected)
		for i := lo; i <= hi; i++ {
			st.selected[rows[i].Path] = struct{}{}
		}
	default:
		st.SelectOnly(p)
	}
}

// glyphFor picks the Phosphor glyph for an entry: a folder, a link, or a file
// glyph by extension. Phosphor is a font the client loads explicitly, so the
// glyph's size and baseline do not depend on the fallback chain.
func glyphFor(e Entry) string {
	switch {
	case e.IsSymlink:
		return icons.PhLinkSimple
	case e.IsDir:
		return icons.PhFolder
	}
	i := strings.LastIndexByte(e.Name, '.')
	if i < 0 || i == len(e.Name)-1 {
		return icons.PhFile
	}
	switch strings.ToLower(e.Name[i+1:]) {
	case "txt", "log", "cfg", "conf", "ini", "toml", "yaml", "yml", "json", "xml":
		return icons.PhFileText
	case "md", "markdown":
		return icons.PhFileMd
	case "go", "rs", "c", "h", "cc", "cpp", "java", "kt", "swift", "py", "rb", "sh", "bash", "zsh", "js", "jsx", "ts", "tsx", "lua", "pl", "php":
		return icons.PhFileCode
	case "sql":
		return icons.PhFileSql
	case "csv", "tsv", "parquet", "arrow":
		return icons.PhFileCsv
	case "png", "jpg", "jpeg", "gif", "webp", "bmp", "tiff", "ico":
		return icons.PhFileImage
	case "svg":
		return icons.PhFileSvg
	case "pdf":
		return icons.PhFilePdf
	case "zip", "gz", "tgz", "bz2", "xz", "zst", "tar", "7z", "rar":
		return icons.PhFileZip
	case "mp3", "flac", "wav", "ogg", "m4a":
		return icons.PhFileAudio
	case "mp4", "mkv", "webm", "mov", "avi":
		return icons.PhFileVideo
	case "html", "htm":
		return icons.PhFileHtml
	case "css":
		return icons.PhFileCss
	}
	return icons.PhFile
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
