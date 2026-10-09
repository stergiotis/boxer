package chat

import (
	"cmp"
	"context"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/capture"
)

// read_window_tree (ADR-0301): a window tree capture of windows of the
// task, read back and handed to the model as an indented outline — one line
// per message of the stream that drew a visible widget, with the widgets it
// drew, their roles, names and values. Every name is the apps' text.

// maxTreeBytes bounds the outline the model reads; a busy window's tree is
// tens of kilobytes.
const maxTreeBytes = 24 << 10

func (inst *coordinator) readWindowTree(ctx context.Context, o toolOrigin, windows []uint64) (content string, activity string) {
	if apps, _ := inst.offers(); !apps {
		return "error: a window tree needs Apps, which this conversation was started without", "window tree: no Apps"
	}
	if len(windows) == 0 {
		return "error: name the windows, as list_windows gives them", "window tree: no windows"
	}
	h := inst.handle()
	if h == "" {
		return "error: no task yet; call request_access for the windows first", "window tree: no task"
	}
	key := o.key()
	out, err := inst.cli.CaptureWith(ctx, agent.CaptureRequest{Handle: h, Instances: windows, Format: agent.CaptureFormatTree, Key: key})
	if err == nil {
		out, err = inst.settle(ctx, h, key, out)
	}
	if err != nil {
		return "error: " + err.Error(), "window tree: " + err.Error()
	}
	if out.Phase != "completed" || out.Job == "" {
		if out.Phase == "refused" || out.Phase == "denied" {
			inst.refuse(out.Reason)
		}
		why := strings.TrimSpace(out.Phase + " " + out.Reason)
		return "error: the window tree " + why, "window tree: " + why
	}
	res, err := inst.cli.Read(ctx, h, out.Job)
	if err != nil {
		return "error: the window tree could not be read: " + err.Error(), "window tree: " + err.Error()
	}
	if res.Untrusted {
		inst.markTainted()
	}
	if res.Confined {
		inst.mu.Lock()
		inst.confined = true
		inst.mu.Unlock()
	}
	t, err := capture.ParseTree(res.Data)
	if err != nil {
		return "error: " + err.Error(), "window tree: " + err.Error()
	}
	return wrapUntrusted("window tree of "+windowsText(windows), treeOutline(t, maxTreeBytes)),
		"read the window tree of " + windowsText(windows) + " (" + strconv.Itoa(len(t.Ops)) + " messages)"
}

// noiseRoles are roles that name no content: layout wrappers, resize
// handles, scroll bars. A widget with one of them and no name or value is
// left out of the outline; how much is out of view is said by the counts.
var noiseRoles = map[string]bool{"": true, "unknown": true, "generic_container": true, "splitter": true,
	"scroll_bar": true, "text_run": true}

// treeOutline renders a window tree for the model, one line per part that
// shows something:
//
//	#2 button "Save" [20,60 40x18] · Button
//	#5 [20,90 300x140] · EndETable · 13 of 20 parts not shown
//	  row [20,92 300x18]: "A320" | "2005"
//
// Lines lead with the widget's role and name; the message that drew it comes
// last. A message that shows nothing of its own and holds one shown part is
// folded into it. Sibling deferred blocks sharing a top edge are a table's
// cells: they print as one row of their texts, so data does not read as
// controls. A message's counts say what is not shown: widgets clipped out of
// view, and deferred blocks it received that are not on screen — not drawn,
// or drawn and scrolled away. The outline is cut at limit bytes.
func treeOutline(t capture.Tree, limit int) (s string) {
	n := len(t.Ops)
	children := make([][]int, n)
	var roots []int
	for i, op := range t.Ops {
		if op.Parent >= 0 {
			children[op.Parent] = append(children[op.Parent], i)
		} else {
			roots = append(roots, i)
		}
	}
	own := make([][]capture.TreeWidget, n)
	shown := make([]bool, n)
	for i := n - 1; i >= 0; i-- {
		for _, w := range t.Ops[i].Widgets {
			if !noiseRoles[w.Role] || w.Name != "" || w.Value != "" {
				own[i] = append(own[i], w)
			}
		}
		if len(own[i]) > 0 || t.Ops[i].Clipped > 0 || t.Ops[i].Blocks > 0 {
			shown[i] = true
		}
		if shown[i] && t.Ops[i].Parent >= 0 {
			shown[t.Ops[i].Parent] = true
		}
	}
	var b strings.Builder
	if t.Taken != "" {
		b.WriteString("taken " + t.Taken + "\n")
	}
	cut := false
	emit := func(line string) {
		if cut {
			return
		}
		if b.Len()+len(line) > limit {
			b.WriteString("… cut at " + strconv.Itoa(limit) + " bytes; read fewer windows")
			cut = true
			return
		}
		b.WriteString(line + "\n")
	}
	var walk func(i int, depth int)
	walkAll := func(ids []int, depth int) {
		var shownIds []int
		for _, c := range ids {
			if shown[c] {
				shownIds = append(shownIds, c)
			}
		}
		rows, rest := tableRows(t, shownIds)
		for _, r := range rows {
			emit(strings.Repeat("  ", depth) + rowText(t, r, children, own))
		}
		for _, c := range rest {
			walk(c, depth)
		}
	}
	walk = func(i int, depth int) {
		op := t.Ops[i]
		var kids []int
		for _, c := range children[i] {
			if shown[c] {
				kids = append(kids, c)
			}
		}
		bare := len(own[i]) == 0 && op.Clipped == 0 && op.Blocks == 0
		if bare && (len(kids) <= 1 || op.Op == capture.TreeBlockOp) {
			walkAll(children[i], depth)
			return
		}
		pad := strings.Repeat("  ", depth)
		head := pad + "#" + strconv.Itoa(i)
		if len(own[i]) > 0 {
			head += " " + widgetText(own[i][0]) + " " + rectText(own[i][0].Rect)
		} else {
			head += " " + rectText(op.Rect)
		}
		head += " · " + op.Op
		if op.Clipped > 0 {
			head += " · " + strconv.Itoa(op.Clipped) + " out of view"
		}
		if op.Blocks > 0 {
			drawn := 0
			for _, c := range children[i] {
				if t.Ops[c].Op == capture.TreeBlockOp {
					drawn++
				}
			}
			if op.Blocks > drawn {
				head += " · " + strconv.Itoa(op.Blocks-drawn) + " of " + strconv.Itoa(op.Blocks) + " parts not shown"
			}
		}
		emit(head)
		for _, w := range own[i][min(1, len(own[i])):] {
			emit(pad + "  - " + widgetText(w) + " " + rectText(w.Rect))
		}
		walkAll(children[i], depth+1)
	}
	walkAll(roots, 0)
	return strings.TrimSuffix(b.String(), "\n")
}

// tableRows picks out of ids the deferred blocks that share a top edge with
// at least one sibling block — a table's cells — grouped by row, top to
// bottom and left to right; rest keeps every other id in order.
func tableRows(t capture.Tree, ids []int) (rows [][]int, rest []int) {
	byTop := map[float32][]int{}
	var tops []float32
	for _, i := range ids {
		if t.Ops[i].Op != capture.TreeBlockOp {
			continue
		}
		y := float32(math.Round(float64(t.Ops[i].Rect[1])))
		if _, ok := byTop[y]; !ok {
			tops = append(tops, y)
		}
		byTop[y] = append(byTop[y], i)
	}
	inRow := map[int]bool{}
	slices.Sort(tops)
	for _, y := range tops {
		cells := byTop[y]
		if len(cells) < 2 {
			continue
		}
		slices.SortFunc(cells, func(a, b int) int { return cmp.Compare(t.Ops[a].Rect[0], t.Ops[b].Rect[0]) })
		rows = append(rows, cells)
		for _, c := range cells {
			inRow[c] = true
		}
	}
	for _, i := range ids {
		if !inRow[i] {
			rest = append(rest, i)
		}
	}
	return
}

// rowText is one table row: the rect the cells span, and each cell's texts.
func rowText(t capture.Tree, cells []int, children [][]int, own [][]capture.TreeWidget) string {
	r := t.Ops[cells[0]].Rect
	minX, minY, maxX, maxY := r[0], r[1], r[0]+r[2], r[1]+r[3]
	texts := make([]string, 0, len(cells))
	for _, c := range cells {
		cr := t.Ops[c].Rect
		minX, minY = min(minX, cr[0]), min(minY, cr[1])
		maxX, maxY = max(maxX, cr[0]+cr[2]), max(maxY, cr[1]+cr[3])
		var parts []string
		var collect func(i int)
		collect = func(i int) {
			for _, w := range own[i] {
				if w.Name != "" {
					parts = append(parts, quoteAppText(w.Name))
				}
				if w.Value != "" {
					parts = append(parts, quoteAppText(w.Value))
				}
			}
			for _, k := range children[i] {
				collect(k)
			}
		}
		collect(c)
		if len(parts) == 0 {
			parts = append(parts, "∅")
		}
		texts = append(texts, strings.Join(parts, " "))
	}
	return "row " + rectText([4]float32{minX, minY, maxX - minX, maxY - minY}) + ": " + strings.Join(texts, " | ")
}

func widgetText(w capture.TreeWidget) (s string) {
	s = w.Role
	if w.Name != "" {
		s += " " + quoteAppText(w.Name)
	}
	if w.Value != "" {
		s += " = " + quoteAppText(w.Value)
	}
	return strings.TrimSpace(s)
}

// quoteAppText quotes the apps' text, so a name cannot close the untrusted
// fence around it.
func quoteAppText(v string) string {
	return strconv.Quote(strings.ReplaceAll(v, "<<", "< <"))
}

func rectText(r [4]float32) string {
	f := func(v float32) string { return strconv.FormatFloat(float64(v), 'f', -1, 32) }
	return "[" + f(r[0]) + "," + f(r[1]) + " " + f(r[2]) + "x" + f(r[3]) + "]"
}
