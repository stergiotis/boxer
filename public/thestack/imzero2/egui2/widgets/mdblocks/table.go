package mdblocks

import (
	"sort"
	"strconv"
	"strings"
)

// The table grid's row model (ADR-0334 §SD3): the block's rows of cells,
// the delimiter row's alignment tokens, and the structural operations the
// productivity tools converge on — insert, delete and move a row or a
// column, align a column, sort the body by a column, expand a pasted
// tab-separated block into cells, and read rows out as TSV. Each is a pure
// function over the fields; the grid composes the block from them after
// the operation and splices the difference (applySubWriteBack).
//
// Row 0 is the header and stays row 0. A GFM table has exactly one header
// row and it is the first line, so a row never moves across it and
// nothing is inserted above it; deleting it promotes the row below. The
// operations that keep that invariant say so by refusing, and the menu
// does not offer what would be refused.

type tableState struct {
	// rows are the cells, row 0 the header; the delimiter row is not a
	// row here. align holds the delimiter row's tokens, one per column.
	rows  [][]string
	align []string
	// lines are the block's lines as loaded, and src says per row which
	// line it was loaded from, or -1 for a row composed fresh — a row
	// whose cells still split from its line is re-emitted verbatim,
	// padding and all.
	lines []string
	src   []int

	// The cells' widgets, by position, and the caret each reported.
	ids     [][]uint64
	cursors [][]uint64
	// The cell to put the caret in on the next emit, when focus is set,
	// at focusChar or at the end when it is negative.
	focusR, focusC, focusChar int
	focus                     bool
	// insert is a snippet for one cell's caret on the next emit; op is a
	// menu's operation, recorded for the next frame.
	insert cellInsert
	op     func()
	// widthEpoch bumps when the columns are asked to re-take their initial
	// widths; the table op re-applies them once per change.
	widthEpoch uint32
}

// alignE is a column's alignment as the delimiter row spells it.
type alignE uint8

const (
	alignNone alignE = iota
	alignLeft
	alignCenter
	alignRight
)

var alignTokens = [...]string{alignNone: "---", alignLeft: ":--", alignCenter: ":-:", alignRight: "--:"}

// alignOf reads a delimiter token's alignment.
func alignOf(token string) alignE {
	t := strings.TrimSpace(token)
	l, r := strings.HasPrefix(t, ":"), strings.HasSuffix(t, ":")
	switch {
	case l && r:
		return alignCenter
	case l:
		return alignLeft
	case r:
		return alignRight
	}
	return alignNone
}

// load splits the block's text into rows; a short row is padded to the
// widest, as goldmark pads it.
func (t *tableState) load(text string) {
	*t = tableState{lines: strings.Split(text, "\n")}
	t.align = splitRow(t.lines[1])
	for i, l := range t.lines {
		if i == 1 {
			continue
		}
		t.rows = append(t.rows, splitRow(l))
		t.src = append(t.src, i)
	}
	width := len(t.align)
	for _, r := range t.rows {
		width = max(width, len(r))
	}
	for len(t.align) < width {
		t.align = append(t.align, alignTokens[alignNone])
	}
	for i := range t.rows {
		for len(t.rows[i]) < width {
			t.rows[i] = append(t.rows[i], "")
		}
	}
}

// compose is the block's text as the rows now say it.
func (t *tableState) compose() string {
	lines := make([]string, 0, len(t.rows)+1)
	for i, r := range t.rows {
		if i == 1 {
			lines = append(lines, composeRow(t.align, 1, t.lines))
		}
		lines = append(lines, composeRow(r, t.src[i], t.lines))
	}
	if len(t.rows) == 1 {
		lines = append(lines, composeRow(t.align, 1, t.lines))
	}
	return strings.Join(lines, "\n")
}

// composeRow re-emits a row from its cells, unless the original line still
// splits to the same cells — then it is kept verbatim, padding and all.
func composeRow(cells []string, lineIdx int, lines []string) string {
	if lineIdx >= 0 && lineIdx < len(lines) && equalCells(splitRow(lines[lineIdx]), cells) {
		return lines[lineIdx]
	}
	esc := make([]string, len(cells))
	for i, cell := range cells {
		esc[i] = strings.ReplaceAll(cell, "|", `\|`)
	}
	return "| " + strings.Join(esc, " | ") + " |"
}

func equalCells(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// splitRow is a GFM table line's cells: the outer pipes dropped, split on
// unescaped pipes, trimmed, escapes unescaped.
func splitRow(line string) (cells []string) {
	l := strings.TrimSpace(line)
	l = strings.TrimPrefix(l, "|")
	l = strings.TrimSuffix(l, "|")
	var cur strings.Builder
	for i := 0; i < len(l); i++ {
		switch {
		case l[i] == '\\' && i+1 < len(l) && l[i+1] == '|':
			cur.WriteByte('|')
			i++
		case l[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(l[i])
		}
	}
	cells = append(cells, strings.TrimSpace(cur.String()))
	return
}

func (t *tableState) cols() int { return len(t.align) }

// --- rows ---

// insertRow puts an empty row at index at, which is clamped below the
// header and at most one past the end.
func (t *tableState) insertRow(at int) {
	at = clampInt(at, 1, len(t.rows))
	t.rows = append(t.rows, nil)
	copy(t.rows[at+1:], t.rows[at:])
	t.rows[at] = make([]string, t.cols())
	t.src = append(t.src, 0)
	copy(t.src[at+1:], t.src[at:])
	t.src[at] = -1
}

// deleteRow removes row i. The last row is kept: a table is at least its
// header. Deleting the header promotes the row below it.
func (t *tableState) deleteRow(i int) bool {
	if len(t.rows) <= 1 || i < 0 || i >= len(t.rows) {
		return false
	}
	t.rows = append(t.rows[:i], t.rows[i+1:]...)
	t.src = append(t.src[:i], t.src[i+1:]...)
	return true
}

// moveRow swaps body row i with its neighbour in direction d (-1 up, +1
// down). The header does not move and nothing moves into its place.
func (t *tableState) moveRow(i, d int) bool {
	j := i + d
	if i < 1 || j < 1 || i >= len(t.rows) || j >= len(t.rows) {
		return false
	}
	t.rows[i], t.rows[j] = t.rows[j], t.rows[i]
	t.src[i], t.src[j] = t.src[j], t.src[i]
	return true
}

// --- columns ---

// insertColumn puts an empty column at index at, clamped to the width.
func (t *tableState) insertColumn(at int) {
	at = clampInt(at, 0, t.cols())
	t.align = insertAt(t.align, at, alignTokens[alignNone])
	for i := range t.rows {
		t.rows[i] = insertAt(t.rows[i], at, "")
	}
}

// deleteColumn removes column i; the last column is kept.
func (t *tableState) deleteColumn(i int) bool {
	if t.cols() <= 1 || i < 0 || i >= t.cols() {
		return false
	}
	t.align = append(t.align[:i], t.align[i+1:]...)
	for r := range t.rows {
		t.rows[r] = append(t.rows[r][:i], t.rows[r][i+1:]...)
	}
	return true
}

// moveColumn swaps column i with its neighbour in direction d.
func (t *tableState) moveColumn(i, d int) bool {
	j := i + d
	if i < 0 || j < 0 || i >= t.cols() || j >= t.cols() {
		return false
	}
	t.align[i], t.align[j] = t.align[j], t.align[i]
	for r := range t.rows {
		t.rows[r][i], t.rows[r][j] = t.rows[r][j], t.rows[r][i]
	}
	return true
}

// setAlign writes column col's alignment into the delimiter row.
func (t *tableState) setAlign(col int, a alignE) {
	if col >= 0 && col < t.cols() {
		t.align[col] = alignTokens[a]
	}
}

func insertAt(s []string, at int, v string) []string {
	s = append(s, "")
	copy(s[at+1:], s[at:])
	s[at] = v
	return s
}

// --- sort ---

// sortBody orders the rows below the header by column col, stably, numbers
// before text and each among themselves — a column of prices sorts as
// prices and a column of names as names, case ignored. An empty cell goes
// last either way: the gap in a column is not its largest value.
func (t *tableState) sortBody(col int, desc bool) {
	if col < 0 || col >= t.cols() || len(t.rows) <= 2 {
		return
	}
	idx := make([]int, len(t.rows)-1)
	for i := range idx {
		idx[i] = i + 1
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ca, cb := t.rows[idx[a]][col], t.rows[idx[b]][col]
		if ca == "" || cb == "" {
			return ca != "" && cb == ""
		}
		if desc {
			ca, cb = cb, ca
		}
		return cellLess(ca, cb)
	})
	rows := make([][]string, len(t.rows))
	src := make([]int, len(t.src))
	rows[0], src[0] = t.rows[0], t.src[0]
	for i, from := range idx {
		rows[i+1], src[i+1] = t.rows[from], t.src[from]
	}
	t.rows, t.src = rows, src
}

// cellLess orders two cells: both numeric by value, else text by folded
// case, with a number before text.
func cellLess(a, b string) bool {
	fa, oka := strconv.ParseFloat(strings.TrimSpace(a), 64)
	fb, okb := strconv.ParseFloat(strings.TrimSpace(b), 64)
	switch {
	case oka == nil && okb == nil:
		return fa < fb
	case oka == nil:
		return true
	case okb == nil:
		return false
	}
	return strings.ToLower(a) < strings.ToLower(b)
}

// --- paste and copy ---

// expandPasted turns a cell holding tabs or newlines — which only a paste
// puts there, the cells being single-line fields with Tab captured — into
// a block of cells from that position on, growing the table as it goes.
// Reports whether any cell was expanded.
func (t *tableState) expandPasted() (expanded bool) {
	for r := 0; r < len(t.rows); r++ {
		for col := 0; col < len(t.rows[r]); col++ {
			cell := t.rows[r][col]
			if !strings.ContainsAny(cell, "\t\n") {
				continue
			}
			expanded = true
			cell = strings.ReplaceAll(cell, "\r\n", "\n")
			lines := strings.Split(strings.TrimSuffix(cell, "\n"), "\n")
			for li, line := range lines {
				for ci, v := range strings.Split(line, "\t") {
					rr, cc := r+li, col+ci
					for rr >= len(t.rows) {
						t.insertRow(len(t.rows))
					}
					for cc >= t.cols() {
						t.insertColumn(t.cols())
					}
					t.rows[rr][cc] = strings.TrimSpace(v)
				}
			}
		}
	}
	return
}

// tsv is rows as tab-separated lines, the shape a spreadsheet pastes.
func tsv(rows [][]string) string {
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = strings.Join(r, "\t")
	}
	return strings.Join(lines, "\n")
}

// widestCells is the widest cell per column in runes, for the columns'
// initial widths.
func (t *tableState) widestCells() (runes []uint32) {
	runes = make([]uint32, t.cols())
	for _, r := range t.rows {
		for col, cell := range r {
			if n := uint32(len([]rune(cell))); n > runes[col] {
				runes[col] = n
			}
		}
	}
	return
}
