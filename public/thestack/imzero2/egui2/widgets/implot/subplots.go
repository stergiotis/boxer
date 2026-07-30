package implot

import (
	"fmt"
	"iter"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// SetupAxisLinks links this axis to caller-held min/max values — the
// upstream contract verbatim: if the values changed since this plot last
// wrote them (another linked plot moved the axis), the plot adopts them;
// otherwise this plot's own gestures win, and End writes the resulting
// range back through the pointers. Plots rendered later in the same frame
// see an earlier plot's move immediately; earlier ones catch up next
// frame. The pointers must be stable across frames.
func (p *Plot) SetupAxisLinks(axis AxisE, vmin *float64, vmax *float64) *Plot {
	if p.warnIfLocked("SetupAxisLinks") {
		return p
	}
	ax := &p.st.x
	if axis == AxisY1 {
		ax = &p.st.y
	}
	if vmin == nil || vmax == nil {
		ax.linkMin, ax.linkMax = nil, nil
		return p
	}
	if *vmin != ax.lastLinkMin || *vmax != ax.lastLinkMax {
		ax.rng = sanitizeScaled(Range{*vmin, *vmax}, ax.scale)
	}
	ax.linkMin, ax.linkMax = vmin, vmax
	return p
}

// writeLinks pushes the settled ranges back through any link pointers;
// runs at End after fits and sanitization.
func (st *plotState) writeLinks() {
	for _, ax := range [2]*axisState{&st.x, &st.y} {
		if ax.linkMin == nil {
			continue
		}
		*ax.linkMin = ax.rng.Min
		*ax.linkMax = ax.rng.Max
		ax.lastLinkMin, ax.lastLinkMax = ax.rng.Min, ax.rng.Max
	}
}

// SubplotFlags selects which axes a Subplots grid shares.
type SubplotFlags uint32

const (
	SubplotFlagsNone SubplotFlags = 0
	// SubplotFlagsLinkAllX links the x axis across every cell.
	SubplotFlagsLinkAllX SubplotFlags = 1 << 0
	// SubplotFlagsLinkAllY links the y axis across every cell.
	SubplotFlagsLinkAllY SubplotFlags = 1 << 1
	// SubplotFlagsLinkRows links the y axis within each row.
	SubplotFlagsLinkRows SubplotFlags = 1 << 2
	// SubplotFlagsLinkCols links the x axis within each column.
	SubplotFlagsLinkCols SubplotFlags = 1 << 3
)

// subplotState retains a grid's link storage across frames, keyed by the
// subplots title scope.
type subplotState struct {
	allX, allY [2]float64
	rows, cols [][2]float64
	inited     bool
}

var subplotPool = make(map[uint64]*subplotState, 4)

// SubplotCtx hands each cell its pre-linked plot. Re-idiomized from
// upstream's implicit BeginSubplots/EndSubplots cursor: Go gets an
// explicit per-cell callback (doc.go records the deviation).
type SubplotCtx struct {
	ids    *c.WidgetIdStack
	st     *subplotState
	flags  SubplotFlags
	cellW  float32
	cellH  float32
	row    int
	col    int
	titled string
}

// Scoped opens this cell's plot and yields it exactly once; End runs
// when the body finishes or breaks early — the range-based counterpart
// to Plot, mirroring the package-level Scoped.
func (sp *SubplotCtx) Scoped(title string) iter.Seq[*Plot] {
	return func(yield func(*Plot) bool) {
		p := sp.Plot(title)
		defer p.End()
		yield(p)
	}
}

// Plot opens this cell's plot (the callback must End it) with the grid's
// links applied per the flags.
func (sp *SubplotCtx) Plot(title string) *Plot {
	p := Begin(sp.ids, title, sp.cellW, sp.cellH)
	if sp.flags&SubplotFlagsLinkAllX != 0 {
		p.SetupAxisLinks(AxisX1, &sp.st.allX[0], &sp.st.allX[1])
	} else if sp.flags&SubplotFlagsLinkCols != 0 {
		p.SetupAxisLinks(AxisX1, &sp.st.cols[sp.col][0], &sp.st.cols[sp.col][1])
	}
	if sp.flags&SubplotFlagsLinkAllY != 0 {
		p.SetupAxisLinks(AxisY1, &sp.st.allY[0], &sp.st.allY[1])
	} else if sp.flags&SubplotFlagsLinkRows != 0 {
		p.SetupAxisLinks(AxisY1, &sp.st.rows[sp.row][0], &sp.st.rows[sp.row][1])
	}
	return p
}

// Subplots lays out rows×cols plot cells in a grid, owning the row
// scaffolding and the shared-axis storage. The callback runs once per
// cell in row-major order and must open exactly one plot — via
// sp.Scoped (preferred), or sp.Plot paired with End.
func Subplots(ids *c.WidgetIdStack, title string, rows int, cols int, w float32, h float32, flags SubplotFlags, cell func(sp *SubplotCtx, row int, col int)) {
	if rows < 1 || cols < 1 {
		return
	}
	scope := ids.PrepareStr(title)
	scopeId := scope.DeriveStacked()
	defer ids.PopIdFromStackChecked(scopeId)
	st, ok := subplotPool[scopeId]
	if !ok {
		st = &subplotState{
			rows: make([][2]float64, rows),
			cols: make([][2]float64, cols),
		}
		subplotPool[scopeId] = st
	}
	if len(st.rows) != rows || len(st.cols) != cols {
		st.rows = make([][2]float64, rows)
		st.cols = make([][2]float64, cols)
	}
	sp := &SubplotCtx{ids: ids, st: st, flags: flags,
		cellW: w / float32(cols), cellH: h / float32(rows)}
	for r := range rows {
		for range c.IdScope(ids.PrepareStr(fmt.Sprintf("row-%d", r))) {
			for range c.Horizontal().KeepIter() {
				for cix := range cols {
					sp.row, sp.col = r, cix
					cell(sp, r, cix)
				}
			}
		}
	}
	st.inited = true
}
