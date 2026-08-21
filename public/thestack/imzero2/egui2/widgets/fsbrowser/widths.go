package fsbrowser

import (
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
)

// Column-width persistence (ADR-0151). The widget keys its columns by what
// they show — name, size, modified, then the host's — with the view appended
// to the type, so a width fitted to the list does not reach the outline,
// whose name column carries the indent and the disclosure control.
const (
	// MaxColumnWidth bounds a drag and a stored width.
	MaxColumnWidth float32 = 1200
	// widthContentMin is the narrowest a column's content may be; the floor
	// adds the cell inset on both sides (the how-to's "two insets" rule).
	widthContentMin float32 = 24

	widthViewList    = ";view=list"
	widthViewOutline = ";view=outline"
)

// MinColumnWidth is the drag floor for the density: content plus both cell
// insets. A host building a [colwidth.Resolver] for this widget passes the
// same number as Opts.MinPoints, so a column cannot be dragged below what
// will come back on the next load.
func MinColumnWidth(density styletokens.DensityE) float32 {
	return widthContentMin + 2*cellInset(density)
}

// cellInset is the gap between a cell's content and its column gridline, both
// sides. It is the tree widget's number and moves with it — the outline mode
// draws its cells through that widget, so a browser whose list mode insets its
// columns differently would shift every column as the reader switches modes.
func cellInset(density styletokens.DensityE) float32 {
	return styletokens.PaddingTight(density)
}

// widthColumns names the columns for the resolver, in emission order.
func (in Input) widthColumns(view string) []colwidth.Column {
	cols := make([]colwidth.Column, 0, builtinColumns+len(in.Columns))
	cols = append(cols,
		colwidth.Column{Name: "name", Type: "fsname" + view},
		colwidth.Column{Name: "size", Type: "bytes" + view},
		colwidth.Column{Name: "modified", Type: "time" + view},
	)
	for _, col := range in.Columns {
		t := col.WidthType
		if t == "" {
			t = "host"
		}
		cols = append(cols, colwidth.Column{Name: col.Header, Type: t + view})
	}
	return cols
}

// widthDefaults is what the columns are without an override.
func (in Input) widthDefaults() []float64 {
	out := make([]float64, 0, builtinColumns+len(in.Columns))
	out = append(out, float64(defaultNameWidth), float64(defaultSizeWidth), float64(defaultTimeWidth))
	for _, col := range in.Columns {
		w := col.Width
		if w <= 0 {
			w = defaultColumnWidth
		}
		out = append(out, float64(w))
	}
	return out
}

// widthPlan is one frame's resolved widths for one view of the widget.
type widthPlan struct {
	on     bool
	tag    string
	cols   []colwidth.Column
	widths []float64
	epoch  uint32
}

// planWidths resolves the view's widths through the host's resolver, opening
// the resolver's settle window when the column set changed (a report that
// follows a change describes the previous columns). Without a resolver the
// plan is the defaults and nothing is observed.
func (in Input) planWidths(st *State, view string) (plan widthPlan) {
	plan.widths = in.widthDefaults()
	plan.cols = in.widthColumns(view)
	if in.Widths == nil {
		return
	}
	tag := in.WidthTag
	if tag == "" {
		tag = in.ScopeKey
	}
	if view == widthViewOutline {
		tag += "/outline"
	}
	plan.on, plan.tag = true, tag
	sig := widthSignature(plan.cols)
	seen := &st.widthSigList
	if view == widthViewOutline {
		seen = &st.widthSigOutline
	}
	if *seen != sig {
		if *seen != "" {
			in.Widths.MarkReseed(tag)
		}
		*seen = sig
	}
	plan.widths = in.Widths.Resolve(tag, plan.cols, 0, plan.widths)
	plan.epoch = in.Widths.Epoch(tag)
	return
}

// observeWidths feeds the table's reported widths back to the resolver.
func (in Input) observeWidths(st *State, plan widthPlan, fetched []float32, view string) {
	if !plan.on || len(fetched) == 0 {
		return
	}
	seen := &st.widthsSeenList
	if view == widthViewOutline {
		seen = &st.widthsSeenOutline
	}
	firstShow := !*seen
	*seen = true
	widths := make([]float64, len(fetched))
	for i, w := range fetched {
		widths[i] = float64(w)
	}
	in.Widths.Observe(plan.tag, plan.cols, widths, 0, firstShow, time.Now())
}

func widthSignature(cols []colwidth.Column) string {
	var sb strings.Builder
	for _, c := range cols {
		sb.WriteString(c.Name)
		sb.WriteByte(0)
		sb.WriteString(c.Type)
		sb.WriteByte(1)
	}
	return sb.String()
}
