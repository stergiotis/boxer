package notebookview

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"path"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/science/audio/peaks"
	"github.com/stergiotis/boxer/public/science/audio/wavfile"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/resolver"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/glossview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imagedecode"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
)

// StatementsE is whether a notebook's SQL is drawn.
type StatementsE uint8

const (
	// StatementsShown draws each statement in its cell, over its result:
	// the notebook.
	StatementsShown StatementsE = iota
	// StatementsHidden draws no statement: the report. A record is one
	// line that says what is stored; a presentation cell is its result.
	StatementsHidden
)

var AllStatements = []StatementsE{StatementsShown, StatementsHidden}

// Options configure a view. The view re-reads them every frame.
type Options struct {
	// Statements is whether the SQL is drawn. Folding is the reader's and
	// is of whole cells: see [View.FoldAll].
	Statements StatementsE
	// Plain draws the notebook's content and nothing about it: no status
	// row, and no header or rule on a cell that has nothing wrong and is
	// not folded. A cell that is refused, stale or unread keeps both, and
	// the problems are still listed. It is for a notebook shown as part of
	// another surface, where its headings do the naming.
	Plain bool
	// LinkClaims lets the host take the links it recognises in the
	// notebook's prose, as [markdown.Input.LinkClaims] does; a click on one
	// arrives in [Events.Links]. The view claims `val:` links itself. Nil
	// leaves every other link a hyperlink.
	LinkClaims func(url string) bool
	// ResultMaxHeight is the ceiling of a drawn result, in points; zero
	// means 360. A table takes what its rows need below it. A renderer is
	// handed a box of this height at most: one written to fill its pane
	// fills the box, not the rest of the window. A cell names its own
	// ceiling in a `cell_opts` member: see [CellOptsMember].
	ResultMaxHeight float32
	// Recordings plays the notebook's recordings: with one, a recording's
	// cell has a transport under its waveform and a playhead on it. Nil
	// leaves a recording drawn and not heard; a click on it is still
	// reported in [Events.Media].
	Recordings RecordingsI
}

// RecordingState is one recording's playback as its cell shows it.
type RecordingState struct {
	// Active says the recording is the one the host plays or has paused.
	Active bool
	// Opening says it is being read or its output opened.
	Opening bool
	Playing bool
	// Fraction is the playhead in [0, 1] and Position the same as a time.
	Fraction float64
	Position time.Duration
	// Note says why nothing is heard, "" when it is.
	Note string
}

// RecordingsI is what plays a notebook's recordings. The view holds no
// output and reads no file while it draws: the host does both, and the view
// asks it once per frame for each recording it shows. path is a media
// cell's path, relative to the notebook.
type RecordingsI interface {
	// State reports the recording's playback.
	State(path string) RecordingState
	// Toggle plays or pauses it; starting one stops whichever played.
	Toggle(path string)
	// Seek moves its playhead when it is the one that plays.
	Seek(path string, fraction float64)
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

const (
	// CellOptsMember names the member of a presentation cell's WITH list
	// that holds the cell's own settings, a row of constants:
	// `WITH cell_opts AS (SELECT 160 AS height)`. The view reads it; the
	// cell's renderer is handed it with the other members.
	CellOptsMember = "cell_opts"
	// CellOptHeight is the setting that bounds the cell's drawn result, in
	// points, in place of [Options.ResultMaxHeight].
	CellOptHeight = "height"
)

// preparedCell is what a cell needs that takes reading or computing.
type preparedCell struct {
	result  *mdnotebook.Result
	refusal error
	// cols are the result's columns for the grid, fields the same for the
	// `kv` renderer; a cell has the one its renderer draws.
	cols   []recordtable.Column
	fields []field
	// maxHeight is the cell's own ceiling, 0 for the view's.
	maxHeight float32

	overview *peaks.Overview
	// duration is a recording's length.
	duration time.Duration
	mediaErr string
}

// Prepared is a notebook read and answered, ready to be drawn: its records
// loaded and checked, every presentation cell answered, its recordings and
// images decoded. [Prepare] builds one on any goroutine; a view takes it
// with [View.Set] and releases it.
type Prepared struct {
	nb     *mdnotebook.Notebook
	loaded *mdnotebook.Loaded
	// bound says the records were the host's, not read from blobs.
	bound    bool
	cells    []preparedCell
	images   map[string]decodedImage
	problems []string
	stale    []string
}

// Notebook is the notebook that was prepared.
func (inst *Prepared) Notebook() *mdnotebook.Notebook { return inst.nb }

// Check is what stands between a prepared notebook and being sealed, as
// `boxer markdown notebook check` reports it.
type Check struct {
	// Problems are what the text, its frontmatter and its files disagree
	// on, each with the cell it was found in.
	Problems []string
	// Stale are the records whose statement was edited since collection,
	// and Derived the records that read one of them.
	Stale   []string
	Derived []string
	// Refused are the presentation cells that have no answer, each as its
	// renderer and what was refused.
	Refused []string
	// Bound says the notebook was prepared over records its host held: it
	// stores nothing, so it is not sealed however clean it is.
	Bound bool
}

// Clean says nothing is wrong with the notebook as it was prepared.
func (inst Check) Clean() bool {
	return len(inst.Problems) == 0 && len(inst.Stale) == 0 && len(inst.Derived) == 0 && len(inst.Refused) == 0
}

// Sealed says the notebook shows what it stores and nothing else is owed.
func (inst Check) Sealed() bool {
	return !inst.Bound && inst.Clean()
}

// Check reports the notebook as it was prepared. It reads nothing.
func (inst *Prepared) Check() (ck Check) {
	if inst == nil {
		return
	}
	ck.Bound = inst.bound
	ck.Problems = append(ck.Problems, inst.problems...)
	ck.Stale = append(ck.Stale, inst.stale...)
	if inst.loaded != nil {
		for _, d := range mdnotebook.DerivedStaleRecords(inst.nb, inst.loaded) {
			ck.Derived = append(ck.Derived, d.Record+" reads "+d.Edited)
		}
	}
	for i := range inst.cells {
		if err := inst.cells[i].refusal; err != nil {
			ck.Refused = append(ck.Refused, "sql "+inst.nb.Cells[i].Renderer+": "+err.Error())
		}
	}
	return
}

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

// Source is where a prepared notebook's parts come from.
type Source struct {
	// Files is the notebook's directory: its blobs, recordings and images.
	// Nil is a notebook that names no file.
	Files fs.FS
	// Records, when not nil, are the notebook's records, held by the host
	// and handed over by name ([mdnotebook.Bind]): no blob is read, the
	// notebook is not sealed and has no pin, and a figure is drawn as the
	// rendering of its value whatever label the text holds. The caller
	// keeps its references; the prepared notebook holds its own.
	Records map[string]arrow.RecordBatch
	// Glosses is the catalog a column's gloss and a figure's are resolved
	// in; nil is [gloss.Default].
	Glosses *gloss.Catalog
}

// noFiles is the directory of a notebook that was given none.
type noFiles struct{}

func (noFiles) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// Prepare reads a notebook's records, recordings and images from fsys,
// whose root is the notebook's directory, answers every presentation cell
// and decodes the recordings and the images. It draws nothing and may run
// on any goroutine: what it does is the part of showing a notebook that
// reads files.
func Prepare(nb *mdnotebook.Notebook, fsys fs.FS) (p *Prepared) {
	return PrepareFrom(nb, Source{Files: fsys})
}

// PrepareFrom is [Prepare] over a [Source]: records the host holds in place
// of blobs, and a gloss catalog of the host's. The notebook that was
// prepared is [Prepared.Notebook]; over bound records it is nb with its
// figures' labels drawn from their values, read again when one changed.
func PrepareFrom(nb *mdnotebook.Notebook, src Source) (p *Prepared) {
	cat := src.Glosses
	if cat == nil {
		cat = gloss.Default()
	}
	p = &Prepared{bound: src.Records != nil, images: make(map[string]decodedImage)}
	if p.bound {
		if text, changed := mdnotebook.Relabel(nb, src.Records, cat); changed {
			if drawn, err := mdnotebook.Read(text); err == nil {
				nb = drawn
			}
		}
		p.loaded = mdnotebook.Bind(nb, src.Records, src.Files)
	} else {
		files := src.Files
		if files == nil {
			files = noFiles{}
		}
		p.loaded = mdnotebook.Load(nb, files)
	}
	p.nb = nb
	p.loaded.Glosses = cat
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
				if cell.Renderer == RendererKV {
					pc.fields = fieldsOf(pc.result.Main, cat)
				} else {
					pc.cols = columnsOf(pc.result.Main, cat)
				}
				pc.maxHeight = cellHeight(pc.result)
			}
		case mdnotebook.CellKindMedia:
			pc.overview, pc.duration, pc.mediaErr = overviewOf(p.loaded.Assets[cell.Path])
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
	// doc is the cell's text as markdown: all of a prose cell, and the
	// fence of a record or a presentation cell once its statement was
	// drawn. hasImage says a prose cell draws a file, so its doc is the
	// notebook's own; taken marks a cell whose doc and renderer went on to
	// the notebook set after it.
	doc      *markdown.Doc
	hasImage bool
	taken    bool
	title    string
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
	renderer      CellRenderer
	closeRenderer func()
	noRenderer    string
	// folded says the reader folded the cell to its header: nothing under
	// it is built. foldKey names a cell that is not prose, for its fold
	// and its widget ids to outlive a notebook set again: it is unique
	// among the notebook's cells.
	folded  bool
	foldKey string
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
	bound    bool
	// pinLabel and staleLabel are the status row's texts, made on first
	// use: the pin is a hash over the records.
	pinLabel   string
	staleLabel string
	// folds are the cells the reader folded, by fold key: a notebook set
	// again — its text edited, its records collected — keeps them.
	folds map[string]bool
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
	closeRenderers(inst.cells)
	inst.prepared.Release()
	inst.prepared = nil
	inst.cells = nil
	inst.loaded = nil
	inst.nb = nil
	inst.problems, inst.stale = nil, nil
	inst.bound = false
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
//
// A cell whose kind and text are those of a cell of the notebook being
// replaced keeps what was built for that one: a prose cell its parse, a
// presentation cell its renderer. So a notebook set again over other
// records, or with cells added and removed around the ones that stay,
// parses and builds only what is new, and a renderer is handed a result it
// has not seen.
func (inst *View) Set(p *Prepared) {
	if p == nil {
		inst.Close()
		return
	}
	was, wasNb, wasPrepared := inst.cells, inst.nb, inst.prepared
	inst.prepared = p
	inst.nb, inst.loaded, inst.bound = p.nb, p.loaded, p.bound
	inst.problems, inst.stale = p.problems, p.stale
	inst.pinLabel, inst.staleLabel = "", ""
	nb := p.nb
	popts := markdown.ParseOptions{Resolver: imageResolver{images: p.images}}
	// keep finds the cell of the notebook being replaced that has cell i's
	// kind and text: the one in its place first, which is the one when only
	// the records changed.
	keep := func(i int) (kept *cellView) {
		if wasNb == nil {
			return nil
		}
		cell := &nb.Cells[i]
		text := nb.Src[cell.Start:cell.Stop]
		same := func(j int) bool {
			if j >= len(was) || was[j].taken {
				return false
			}
			o := &wasNb.Cells[j]
			return o.Kind == cell.Kind && bytes.Equal(wasNb.Src[o.Start:o.Stop], text)
		}
		if same(i) {
			kept = &was[i]
		} else {
			for j := range was {
				if same(j) {
					kept = &was[j]
					break
				}
			}
		}
		if kept != nil {
			kept.taken = true
		}
		return kept
	}
	// keys makes each fold key the notebook's only one of its spelling.
	var keys map[string]int
	unique := func(key string) string {
		if keys == nil {
			keys = make(map[string]int)
		}
		n := keys[key]
		keys[key] = n + 1
		if n == 0 {
			return key
		}
		return key + "#" + strconv.Itoa(n)
	}
	inst.cells = make([]cellView, len(nb.Cells))
	for _, img := range nb.Images {
		inst.cells[img.Cell].hasImage = true
	}
	for i := range nb.Cells {
		cell := &nb.Cells[i]
		cv := &inst.cells[i]
		cv.preparedCell = p.cells[i]
		kept := keep(i)
		switch cell.Kind {
		case mdnotebook.CellKindProse:
			if kept != nil && !kept.hasImage && !cv.hasImage {
				cv.doc = kept.doc
			} else {
				cv.doc = markdown.ParseWith(nb.Src[cell.Start:cell.Stop], popts)
			}
		case mdnotebook.CellKindRecord:
			cv.foldKey = unique("record:" + cell.Name)
			cv.title = "record " + cell.Id
			if rec, found := inst.loaded.Records[cell.Name]; found {
				cv.title += " — " + shape(rec)
				cv.stored = true
			} else if inst.bound {
				cv.title += " — not bound"
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
			// The heading it stands under names it: a presentation cell has
			// no id of its own, and its place among the cells moves when a
			// cell is put in above it.
			cv.foldKey = unique("cell:" + cell.Renderer + ":" + cell.Section)
			cv.title = cell.Renderer
			if kept != nil {
				// Kept whether or not there is a result to hand it this time.
				cv.renderer, cv.closeRenderer = kept.renderer, kept.closeRenderer
			}
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
				switch build := rendererNamed(cell.Renderer); {
				case cv.renderer != nil:
				case build != nil:
					cv.renderer, cv.closeRenderer = build(inst.ids)
				case cell.Renderer != mdnotebook.RendererTable && cell.Renderer != RendererKV:
					cv.noRenderer = "no renderer named " + cell.Renderer + "; shown as a table"
				}
			}
		case mdnotebook.CellKindMedia:
			cv.foldKey = unique("media:" + cell.Path)
			cv.title = "recording " + path.Base(cell.Path)
			if cell.Alt != "" {
				cv.title = "recording — " + cell.Alt
			}
		}
		if kept != nil && cell.Kind != mdnotebook.CellKindProse {
			// The statement's parse, when it was drawn.
			cv.doc = kept.doc
		}
		cv.folded = inst.folds[cv.foldKey]
	}
	closeRenderers(was)
	wasPrepared.Release()
}

// closeRenderers closes the renderers of the cells that did not go on to
// the next notebook.
func closeRenderers(cells []cellView) {
	for i := range cells {
		if cv := &cells[i]; !cv.taken && cv.closeRenderer != nil {
			cv.closeRenderer()
		}
	}
}

// FoldAll folds every cell to its header, or unfolds every one. A cell's
// fold is otherwise the reader's, by a click on its header, and is kept
// when the notebook is set again.
func (inst *View) FoldAll(folded bool) {
	if inst.folds == nil {
		inst.folds = make(map[string]bool)
	}
	for i := range inst.cells {
		cv := &inst.cells[i]
		if cv.foldKey == "" {
			continue
		}
		cv.folded = folded
		if folded {
			inst.folds[cv.foldKey] = true
		} else {
			delete(inst.folds, cv.foldKey)
		}
	}
}

// Folded counts the cells that are folded.
func (inst *View) Folded() (n int) {
	for i := range inst.cells {
		if inst.cells[i].folded {
			n++
		}
	}
	return
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

// cellHeight reads a cell's own ceiling from its settings member, 0 when it
// names none.
func cellHeight(res *mdnotebook.Result) (height float32) {
	for _, m := range res.Members {
		if m.Name != CellOptsMember || m.Record.NumRows() == 0 {
			continue
		}
		for i, f := range m.Record.Schema().Fields() {
			if f.Name != CellOptHeight {
				continue
			}
			if v, ok := (gloss.ArrowCell{Arr: m.Record.Column(i), Row: 0}).Float64(); ok && v > 0 {
				height = float32(v)
			}
		}
	}
	return
}

// columnsOf is a record's columns for the grid: a column that declares a
// gloss is headed by its label and shows its values through the gloss, in
// the tone the gloss gives them.
func columnsOf(rec arrow.RecordBatch, cat *gloss.Catalog) (cols []recordtable.Column) {
	for i, f := range rec.Schema().Fields() {
		col := recordtable.Column{Arrow: i}
		if d, declared := cat.ParseColumn(f.Name); declared {
			col.Header = d.Label
			col.Sub = []string{d.MediaType}
			if d.Instance != nil {
				arr := rec.Column(i)
				if ok, _ := d.Instance.Accepts(gloss.KindOfArrow(arr.DataType())); ok {
					instance := d.Instance
					col.Cell = func(row int64) (cell recordtable.Cell) {
						face := instance.Inline(gloss.ArrowCell{Arr: arr, Row: int(row)})
						cell.Text = face.Text
						cell.Tone, cell.Toned = glossview.ToneColor(face.Tone)
						return
					}
				}
			}
		}
		cols = append(cols, col)
	}
	return
}

func overviewOf(a mdnotebook.Asset) (overview *peaks.Overview, duration time.Duration, problem string) {
	if a.Err != nil {
		return nil, 0, a.Err.Error()
	}
	data := a.Data
	f, err := wavfile.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, 0, err.Error()
	}
	duration = f.Format().FramesToDuration(f.Frames())
	overview, err = peaks.ComputeOverview(context.Background(), f, overviewColumns)
	if err != nil {
		return nil, 0, err.Error()
	}
	return overview, duration, ""
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
