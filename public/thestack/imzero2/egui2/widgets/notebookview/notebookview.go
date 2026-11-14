package notebookview

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/science/audio/peaks"
	"github.com/stergiotis/boxer/public/science/audio/wavfile"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/resolver"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imagedecode"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
)

// StatementsE is how a notebook's SQL is drawn.
type StatementsE uint8

const (
	// StatementsFolded draws each statement under a fold the reader opens.
	StatementsFolded StatementsE = iota
	// StatementsHidden draws no statement: the report. A record is one
	// line that says what is stored; a presentation cell is its result.
	StatementsHidden
	// StatementsOpen draws every statement unfolded: the notebook.
	StatementsOpen
)

var AllStatements = []StatementsE{StatementsFolded, StatementsHidden, StatementsOpen}

// Options configure a view. The view re-reads them every frame.
type Options struct {
	// Statements is how the SQL is drawn. A change folds or unfolds every
	// statement once; the reader's own folding holds until the next change,
	// and under StatementsOpen until the next notebook, which is unfolded
	// like the first.
	Statements StatementsE
	// LinkClaims lets the host take the links it recognises in the
	// notebook's prose, as [markdown.Input.LinkClaims] does; a click on one
	// arrives in [Events.Links]. The view claims `val:` links itself. Nil
	// leaves every other link a hyperlink.
	LinkClaims func(url string) bool
	// ResultMaxHeight is the ceiling of a drawn result, in points; zero
	// means 360. A table takes what its rows need below it. A renderer is
	// handed a box of this height at most: one written to fill its pane
	// fills the box, not the rest of the window.
	ResultMaxHeight float32
}

// ValClick is a click on a figure in a sentence.
type ValClick struct {
	Record string
	Column string
	Label  string
}

// MediaClick is a click on a recording: where in it, as a fraction.
type MediaClick struct {
	Cell     int
	Path     string
	Fraction float64
}

// Events is what one Render reports.
type Events struct {
	Vals  []ValClick
	Media []MediaClick
	// Links are the clicks on links the host claimed.
	Links []markdown.LinkClick
	// Tasks are the task boxes clicked in prose, their offsets in the
	// notebook's source.
	Tasks []markdown.TaskClick
}

const overviewColumns = 520

// preparedCell is what a cell needs that takes reading or computing.
type preparedCell struct {
	result  *mdnotebook.Result
	refusal error
	cols    []recordtable.Column

	overview *peaks.Overview
	mediaErr string
}

// Prepared is a notebook read and answered, ready to be drawn: its records
// loaded and checked, every presentation cell answered, its recordings and
// images decoded. [Prepare] builds one on any goroutine; a view takes it
// with [View.Set] and releases it.
type Prepared struct {
	nb       *mdnotebook.Notebook
	loaded   *mdnotebook.Loaded
	cells    []preparedCell
	images   map[string]decodedImage
	problems []string
	stale    []string
}

// Notebook is the notebook that was prepared.
func (inst *Prepared) Notebook() *mdnotebook.Notebook { return inst.nb }

// Release releases the records and results. A view releases what it was
// handed; this is for a Prepared no view took.
func (inst *Prepared) Release() {
	if inst == nil {
		return
	}
	for i := range inst.cells {
		inst.cells[i].result.Release()
	}
	inst.cells = nil
	if inst.loaded != nil {
		inst.loaded.Release()
		inst.loaded = nil
	}
}

type decodedImage struct {
	pixels []uint32
	w, h   uint32
}

// Prepare reads a notebook's records, recordings and images from fsys,
// whose root is the notebook's directory, answers every presentation cell
// and decodes the recordings and the images. It draws nothing and may run
// on any goroutine: what it does is the part of showing a notebook that
// reads files.
func Prepare(nb *mdnotebook.Notebook, fsys fs.FS) (p *Prepared) {
	p = &Prepared{nb: nb, loaded: mdnotebook.Load(nb, fsys), images: make(map[string]decodedImage)}
	for _, prob := range mdnotebook.Problems(nb, p.loaded) {
		p.problems = append(p.problems, problemText(prob))
	}
	p.stale = mdnotebook.StaleRecords(nb, p.loaded)
	p.cells = make([]preparedCell, len(nb.Cells))
	for i := range nb.Cells {
		cell := &nb.Cells[i]
		pc := &p.cells[i]
		switch cell.Kind {
		case mdnotebook.CellKindPresentation:
			pc.result, pc.refusal = mdnotebook.Evaluate(cell.Body, p.loaded.Records)
			if pc.result != nil {
				pc.cols = columnsOf(pc.result.Main)
			}
		case mdnotebook.CellKindMedia:
			pc.overview, pc.mediaErr = overviewOf(p.loaded.Assets[cell.Path])
		}
	}
	for _, img := range nb.Images {
		a := p.loaded.Assets[img.Path]
		if _, seen := p.images[img.Path]; seen || a.Err != nil {
			// One that could not be read is a problem already (Load).
			continue
		}
		pixels, w, h, err := imagedecode.DecodeRGBA8(a.Data, imagedecode.DefaultMaxPixels)
		if err != nil {
			p.problems = append(p.problems, problemText(mdnotebook.Problem{Cell: img.Cell, Text: "image " + img.Path + ": " + err.Error()}))
			continue
		}
		p.images[img.Path] = decodedImage{pixels: pixels, w: w, h: h}
	}
	// Decoded: the files' bytes are not needed again.
	p.loaded.Assets = nil
	return p
}

// cellView is one cell as it is drawn.
type cellView struct {
	preparedCell
	// doc is the cell's text as markdown: all of a prose cell, the fence of
	// a record or a presentation cell.
	doc   *markdown.Doc
	title string
	// stored says a record's result was read.
	stored bool
	// stale says why what is stored for a record is not what the text would
	// compute — its statement was edited, or it reads a record whose was —
	// "" for a fresh one.
	stale string
	// staleRead says which stale record a presentation cell reads, "" for
	// none.
	staleRead string
	// renderer is the cell's own instance of the renderer its fence names,
	// nil for the built-in table and for a name nothing registered;
	// noRenderer then says so, for a name that is not the table's.
	renderer   CellRenderer
	noRenderer string
	// grid is the table's state, at one address for the life of the
	// notebook; gridFrames counts the frames it was drawn in.
	grid       recordtable.State
	gridFrames int
}

// View draws one notebook. Build it with [New]; it is not safe for use off
// the render goroutine.
type View struct {
	// Opts is read every frame.
	Opts Options

	ids      *c.WidgetIdStack
	scopeKey string

	prepared *Prepared
	nb       *mdnotebook.Notebook
	loaded   *mdnotebook.Loaded
	cells    []cellView
	problems []string
	stale    []string
	// pinLabel and staleLabel are the status row's texts, made once per
	// notebook: the pin is a hash over the records.
	pinLabel   string
	staleLabel string
	// drawn is the Statements value the folds were last set for: the value
	// the last frame drew with, until a notebook is set.
	drawn StatementsE
	// scrollSection and scrollOffset are one-shot scroll targets for the
	// next Render; scrollOffset is -1 for none.
	scrollSection string
	scrollOffset  int

	events Events
}

// New builds an empty view under ids and scopeKey; an empty scopeKey means
// "notebook".
func New(ids *c.WidgetIdStack, scopeKey string, opts Options) (inst *View) {
	if scopeKey == "" {
		scopeKey = "notebook"
	}
	return &View{Opts: opts, ids: ids, scopeKey: scopeKey, scrollOffset: -1}
}

// ScrollToSection scrolls, on the next Render, to the heading of that slug
// or the block of that `^id` in the notebook's prose, as
// [markdown.Input.ScrollToSection] does. The host calls it when its own
// selection changes, not every frame.
func (inst *View) ScrollToSection(slug string) { inst.scrollSection = slug }

// ScrollToOffset scrolls, on the next Render, to the prose block that holds
// the byte offset of the notebook's source. An offset inside a cell that is
// not prose scrolls nowhere.
func (inst *View) ScrollToOffset(offset int) { inst.scrollOffset = offset }

// Close releases what the view holds of the notebook.
func (inst *View) Close() {
	inst.prepared.Release()
	inst.prepared = nil
	inst.cells = nil
	inst.loaded = nil
	inst.nb = nil
	inst.problems, inst.stale = nil, nil
	inst.pinLabel, inst.staleLabel = "", ""
}

// Notebook is the notebook being drawn, nil for none.
func (inst *View) Notebook() *mdnotebook.Notebook { return inst.nb }

// Problems are what a reader should be told about the notebook: what the
// text and its frontmatter disagree on, blobs that could not be read, and
// figures whose label is no longer the rendering of their value.
func (inst *View) Problems() []string { return inst.problems }

// imageResolver answers the markdown widget's parse from the images
// [Prepare] decoded.
type imageResolver struct {
	resolver.NoopResolver
	images map[string]decodedImage
}

func (inst imageResolver) LoadImage(ref string) (pixels []uint32, widthPx uint32, heightPx uint32, ok bool) {
	d, ok := inst.images[ref]
	return d.pixels, d.w, d.h, ok
}

// SetNotebook replaces the notebook with nb, read from fsys, whose root is
// the notebook's directory. It reads, answers and decodes before it
// returns: [Prepare] and [View.Set] in one call, for a caller whose files
// are at hand. A host whose files take time prepares on a goroutine of its
// own.
func (inst *View) SetNotebook(nb *mdnotebook.Notebook, fsys fs.FS) {
	if nb == nil {
		inst.Close()
		return
	}
	inst.Set(Prepare(nb, fsys))
}

// Set replaces the notebook with a prepared one, which the view owns from
// here; nil is no notebook. It parses the notebook's markdown and builds a
// renderer per cell, so it runs on the render goroutine.
func (inst *View) Set(p *Prepared) {
	inst.Close()
	if p == nil {
		return
	}
	inst.prepared = p
	inst.nb, inst.loaded = p.nb, p.loaded
	inst.problems, inst.stale = p.problems, p.stale
	nb := p.nb
	inst.pinLabel = "data " + mdnotebook.PinOf(nb.Records)
	if len(inst.stale) > 0 {
		inst.staleLabel = "edited since collection: " + strings.Join(inst.stale, ", ")
	}
	// A statement drawn for the first time starts folded, whatever the
	// option says: have the next frame unfold a notebook set under
	// StatementsOpen.
	if inst.drawn == StatementsOpen {
		inst.drawn = StatementsFolded
	}
	popts := markdown.ParseOptions{Resolver: imageResolver{images: p.images}}
	fence := func(c *mdnotebook.Cell) *markdown.Doc {
		// The statement alone: the id line is not part of what is shown.
		return markdown.ParseWith([]byte("```sql\n"+strings.TrimRight(c.Body, "\n")+"\n```\n"), popts)
	}
	inst.cells = make([]cellView, len(nb.Cells))
	for i := range nb.Cells {
		cell := &nb.Cells[i]
		cv := &inst.cells[i]
		cv.preparedCell = p.cells[i]
		switch cell.Kind {
		case mdnotebook.CellKindProse:
			cv.doc = markdown.ParseWith([]byte(nb.Text(i)), popts)
		case mdnotebook.CellKindRecord:
			cv.doc = fence(cell)
			cv.title = "record " + cell.Id
			if rec, found := inst.loaded.Records[cell.Name]; found {
				cv.title += " — " + shape(rec)
				cv.stored = true
			} else {
				cv.title += " — not stored"
			}
			if edited, stale := inst.loaded.StaleSource(cell.Name); stale {
				if edited == cell.Name {
					cv.stale = "edited since collection; what is stored was computed from an earlier statement"
				} else {
					cv.stale = "reads " + edited + ", which was edited since collection; what is stored was computed from its earlier statement"
				}
			}
		case mdnotebook.CellKindPresentation:
			cv.doc = fence(cell)
			cv.title = "sql " + cell.Renderer
			if cv.result != nil {
				for _, name := range cv.result.Reads {
					edited, stale := inst.loaded.StaleSource(name)
					if !stale {
						continue
					}
					cv.staleRead = "reads " + name + ", which was edited since collection"
					if edited != name {
						cv.staleRead = "reads " + name + ", computed from " + edited + ", which was edited since collection"
					}
					break
				}
				if build := rendererNamed(cell.Renderer); build != nil {
					cv.renderer = build(inst.ids)
				} else if cell.Renderer != mdnotebook.RendererTable {
					cv.noRenderer = "no renderer named " + cell.Renderer + "; shown as a table"
				}
			}
		}
	}
}

func problemText(p mdnotebook.Problem) string {
	if p.Cell < 0 {
		return p.Text
	}
	return "cell " + strconv.Itoa(p.Cell) + ": " + p.Text
}

func shape(rec arrow.RecordBatch) string {
	return count(rec.NumRows(), "row") + " × " + count(rec.NumCols(), "column")
}

func count(n int64, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.FormatInt(n, 10) + " " + noun + "s"
}

// columnsOf is a record's columns for the grid: a column that declares a
// gloss is headed by its label and shows its values through the gloss.
func columnsOf(rec arrow.RecordBatch) (cols []recordtable.Column) {
	cat := gloss.Default()
	for i, f := range rec.Schema().Fields() {
		col := recordtable.Column{Arrow: i}
		if d, declared := cat.ParseColumn(f.Name); declared {
			col.Header = d.Label
			col.Sub = []string{d.MediaType}
			if d.Instance != nil {
				arr := rec.Column(i)
				if ok, _ := d.Instance.Accepts(gloss.KindOfArrow(arr.DataType())); ok {
					instance := d.Instance
					col.Cell = func(row int64) recordtable.Cell {
						return recordtable.Cell{Text: instance.Inline(gloss.ArrowCell{Arr: arr, Row: int(row)}).Text}
					}
				}
			}
		}
		cols = append(cols, col)
	}
	return
}

func overviewOf(a mdnotebook.Asset) (overview *peaks.Overview, problem string) {
	if a.Err != nil {
		return nil, a.Err.Error()
	}
	data := a.Data
	f, err := wavfile.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err.Error()
	}
	overview, err = peaks.ComputeOverview(context.Background(), f, overviewColumns)
	if err != nil {
		return nil, err.Error()
	}
	return overview, ""
}

// refusalText is what a cell shows in place of a result it has not got.
func refusalText(err error) (kind string, text string) {
	var r *mdnotebook.Refusal
	if errors.As(err, &r) {
		return "refused", r.Found
	}
	if errors.Is(err, mdnotebook.ErrNotPresentation) {
		return "refused", err.Error()
	}
	return "failed", err.Error()
}
