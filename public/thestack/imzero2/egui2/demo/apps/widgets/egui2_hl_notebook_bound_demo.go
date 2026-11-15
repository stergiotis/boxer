package widgets

import (
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookcells"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookview"
)

func init() {
	registry.Register(registry.Demo{
		Name:     "notebook-bound",
		Category: "Text & code",
		Title:    "notebook written by a programme over what it holds",
		Stage:    [2]float32{1024, 900},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "A notebook (ADR-0371) a programme writes for the thing it is asked to show, bound to relations it holds: no blob is " +
			"stored and none read. Each thing gets the sections it has data for and no others, drawn plainly, as a report inside " +
			"another surface is. The `kv` cell is the view's own; `marks` and `points` are package notebookcells. Choosing another " +
			"thing writes and binds the notebook again, and the cells whose text did not change keep what was built for them.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			state = newNotebookBoundState(ids)
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoNotebookBound(ids, state.(*notebookBoundState))
		},
		SourceFunc: demoNotebookBound,
	})
}

// boundThing is one thing the demo shows: what a host would hold as a row
// and the relations hanging off it.
type boundThing struct {
	name, kind, site string
	lag              float64
	dates            [][3]string // label, begins, ends ("" for an instant)
	places           [][3]any    // name, lat, lon
}

var boundThings = []boundThing{
	{
		name: "Meridian Freight", kind: "company", site: "https://example.org/meridian", lag: 412.3,
		dates:  [][3]string{{"registered", "2011-03-02", ""}, {"licence", "2019-06-01", "2027-05-31"}, {"audited", "2024-09-14", ""}, {"audited", "2026-02-20", ""}},
		places: [][3]any{{"Rotterdam", 51.9244, 4.4777}, {"Piraeus", 37.9420, 23.6465}, {"Lisbon", 38.7223, -9.1393}},
	},
	{
		name: "Halvard Line", kind: "vessel", lag: 38.0,
		places: [][3]any{{"Bergen", 60.3913, 5.3221}},
	},
	{
		name: "Orsolya Kiss", kind: "person", site: "https://example.org/kiss", lag: 1250,
		dates: [][3]string{{"born", "1978-08-21", ""}, {"appointed", "2020-01-15", ""}},
	},
}

type notebookBoundState struct {
	view          *notebookview.View
	chosen, shown int
	clicked       bool
	plain         bool
	writer        mdnotebook.Writer
	last          string
}

func newNotebookBoundState(ids *c.WidgetIdStack) (st *notebookBoundState) {
	st = &notebookBoundState{shown: -1, plain: true}
	st.view = notebookview.New(ids, "notebook-bound", notebookview.Options{
		Statements:      notebookview.StatementsHidden,
		Plain:           true,
		ResultMaxHeight: 220,
	})
	return
}

// boundRecords builds the relations of one thing. A host that holds them
// already hands over slices of what it has; the demo holds literals.
func boundRecords(t *boundThing) (records map[string]arrow.RecordBatch) {
	mem := memory.DefaultAllocator
	records = make(map[string]arrow.RecordBatch, 3)
	{
		b := array.NewRecordBuilder(mem, arrow.NewSchema([]arrow.Field{
			{Name: "name", Type: arrow.BinaryTypes.String},
			{Name: "kind", Type: arrow.BinaryTypes.String},
			{Name: "lag", Type: arrow.PrimitiveTypes.Float64},
			{Name: "site", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil))
		b.Field(0).(*array.StringBuilder).Append(t.name)
		b.Field(1).(*array.StringBuilder).Append(t.kind)
		b.Field(2).(*array.Float64Builder).Append(t.lag)
		if t.site == "" {
			b.Field(3).AppendNull()
		} else {
			b.Field(3).(*array.StringBuilder).Append(t.site)
		}
		records["thing"] = b.NewRecordBatch()
		b.Release()
	}
	if len(t.dates) > 0 {
		ts := &arrow.TimestampType{Unit: arrow.Millisecond, TimeZone: "UTC"}
		b := array.NewRecordBuilder(mem, arrow.NewSchema([]arrow.Field{
			{Name: "what", Type: arrow.BinaryTypes.String},
			{Name: "begins", Type: ts},
			{Name: "ends", Type: ts, Nullable: true},
		}, nil))
		day := func(s string) arrow.Timestamp {
			d, _ := time.Parse("2006-01-02", s)
			return arrow.Timestamp(d.UnixMilli())
		}
		for _, d := range t.dates {
			b.Field(0).(*array.StringBuilder).Append(d[0])
			b.Field(1).(*array.TimestampBuilder).Append(day(d[1]))
			if d[2] == "" {
				b.Field(2).AppendNull()
			} else {
				b.Field(2).(*array.TimestampBuilder).Append(day(d[2]))
			}
		}
		records["dates"] = b.NewRecordBatch()
		b.Release()
	}
	if len(t.places) > 0 {
		b := array.NewRecordBuilder(mem, arrow.NewSchema([]arrow.Field{
			{Name: "place", Type: arrow.BinaryTypes.String},
			{Name: "latitude", Type: arrow.PrimitiveTypes.Float64},
			{Name: "longitude", Type: arrow.PrimitiveTypes.Float64},
		}, nil))
		for _, p := range t.places {
			b.Field(0).(*array.StringBuilder).Append(p[0].(string))
			b.Field(1).(*array.Float64Builder).Append(p[1].(float64))
			b.Field(2).(*array.Float64Builder).Append(p[2].(float64))
		}
		records["places"] = b.NewRecordBatch()
		b.Release()
	}
	return
}

// write is the notebook of one thing: a section for each relation it has.
// The text depends on which relations there are, not on what they hold.
func (inst *notebookBoundState) write(records map[string]arrow.RecordBatch) (text []byte, err error) {
	w := &inst.writer
	w.Reset()
	w.Markdown("# " + mdnotebook.Figure("thing", "name", "") + "\n\nIts last reading lagged by " +
		mdnotebook.Figure("thing", "lag", "gloss/duration;unit=ms") + ".")
	w.Cell(notebookview.RendererKV, "SELECT kind, lag AS "+mdnotebook.Alias("lag", "gloss/duration;unit=ms")+
		", site AS "+mdnotebook.Alias("site", "gloss/url")+" FROM thing")
	if _, has := records["dates"]; has {
		w.Heading(2, "When")
		w.Cell(notebookcells.RendererMarks, "SELECT begins AS "+notebookcells.ColTime+", ends AS "+notebookcells.ColTimeEnd+
			", what AS "+notebookcells.ColLabel+" FROM dates")
	}
	if _, has := records["places"]; has {
		w.Heading(2, "Where")
		w.Cell(notebookcells.RendererPoints, "WITH "+notebookview.CellOptsMember+" AS (SELECT 260 AS "+notebookview.CellOptHeight+")\n"+
			"SELECT latitude AS "+notebookcells.ColLat+", longitude AS "+notebookcells.ColLon+", place AS "+notebookcells.ColPlace+" FROM places")
		w.Cell(mdnotebook.RendererTable, "SELECT place, latitude AS "+mdnotebook.Alias("latitude", "gloss/coordinate;axis=lat")+
			", longitude AS "+mdnotebook.Alias("longitude", "gloss/coordinate;axis=lon")+" FROM places")
	}
	return append([]byte(nil), w.Bytes()...), w.Err()
}

// load writes the chosen thing's notebook and hands it to the view bound to
// the thing's relations.
func (inst *notebookBoundState) load() {
	inst.shown = inst.chosen
	inst.last = ""
	records := boundRecords(&boundThings[inst.chosen])
	defer func() {
		// The prepared notebook holds its own references.
		for _, r := range records {
			r.Release()
		}
	}()
	text, err := inst.write(records)
	if err != nil {
		inst.last = err.Error()
		return
	}
	nb, err := mdnotebook.Read(text)
	if err != nil {
		inst.last = err.Error()
		return
	}
	inst.view.Set(notebookview.PrepareFrom(nb, notebookview.Source{Records: records}))
}

func demoNotebookBound(ids *c.WidgetIdStack, st *notebookBoundState) {
	for range c.HorizontalTop().KeepIter() {
		c.Label("Show").Send()
		for i := range boundThings {
			for range c.IdScope(ids.PrepareSeq(uint64(i))) {
				if c.RadioButton(ids.PrepareStr("thing"), c.Atoms().Text(boundThings[i].name).Keep(), st.chosen == i).SendRespVal(&st.clicked).HasPrimaryClicked() {
					st.chosen = i
				}
			}
		}
		c.Checkbox(ids.PrepareStr("plain"), st.plain, "Plain").SendRespVal(&st.plain)
	}
	if st.chosen != st.shown {
		st.load()
	}
	if st.last != "" {
		c.Label("last: " + st.last).Send()
		return
	}
	st.view.Opts.Plain = st.plain
	nb := st.view.Notebook()
	c.Label(fmt.Sprintf("bound: %d cells · %d problems", len(nb.Cells), len(st.view.Problems()))).Send()
	c.Separator().Send()
	st.view.Render()
}
