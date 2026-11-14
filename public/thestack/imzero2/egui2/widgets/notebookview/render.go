package notebookview

import (
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/waveform"
)

const (
	thumbW = 520
	thumbH = 64
	// defaultResultMaxHeight keeps a result short enough that the prose
	// around it stays in view.
	defaultResultMaxHeight = 360
	// gridRefitFrame is the frame a table fits its columns again. The fit
	// measures the rows the table reported visible the frame before, and a
	// table reports its full range only from its second frame on.
	gridRefitFrame = 3
)

// weakLabel is a line of secondary text.
func weakLabel(text string) {
	c.LabelAtoms(c.Atoms().BeginRichText(text).Weak().End().Keep()).Send()
}

func isVal(url string) bool { return strings.HasPrefix(url, mdnotebook.ValScheme) }

// Render draws the notebook into the current Ui, one cell under another.
func (inst *View) Render() (ev Events) {
	inst.events.Vals = inst.events.Vals[:0]
	inst.events.Media = inst.events.Media[:0]
	inst.events.Links = inst.events.Links[:0]
	inst.events.Tasks = inst.events.Tasks[:0]
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		if inst.nb == nil {
			weakLabel("no notebook")
			break
		}
		inst.renderStatus()
		for i := range inst.cells {
			for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
				inst.renderCell(i)
			}
		}
		inst.renderProblems()
	}
	inst.drawn = inst.Opts.Statements
	inst.scrollSection, inst.scrollOffset = "", -1
	return inst.events
}

// renderStatus says whether the notebook is sealed: every record stored
// under the pin and fresh.
func (inst *View) renderStatus() {
	for range c.Horizontal().KeepIter() {
		switch {
		case len(inst.stale) > 0:
			badge.New(inst.ids.PrepareStr("status"), "stale").Tone(badge.ToneWarning).Send()
			weakLabel(inst.staleLabel)
		case len(inst.problems) > 0:
			badge.New(inst.ids.PrepareStr("status"), "not sealed").Tone(badge.ToneError).Send()
		default:
			badge.New(inst.ids.PrepareStr("status"), "sealed").Tone(badge.ToneSuccess).Send()
		}
		c.LabelAtoms(c.Atoms().BeginRichText(inst.pinLabel).Weak().Monospace().End().Keep()).Send()
	}
}

func (inst *View) renderProblems() {
	if len(inst.problems) == 0 {
		return
	}
	c.Separator().Send()
	for i, p := range inst.problems {
		for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
			for range c.Horizontal().KeepIter() {
				badge.New(inst.ids.PrepareStr("problem"), "problem").Tone(badge.ToneError).Send()
				c.Label(p).Send()
			}
		}
	}
}

// cellFrame is the frame of a cell that is not prose: a rule along its left
// edge and the cell set in from it, no box, as the markdown widget draws a
// quote. What the rule spans is one cell — its statement, what it says of
// its state and its result — so a reader sees where a stored result ends
// and the prose goes on. The rule takes the tone of the cell's state, which
// then reads down the page's margin: neutral for a cell as collected,
// warning for a stale one, error for one that has nothing to show.
func (inst *View) cellFrame(cell *mdnotebook.Cell, cv *cellView) c.FrameFluid {
	rule := styletokens.NeutralBorderDefault
	switch {
	case cv.refusal != nil, cv.mediaErr != "", cell.Kind == mdnotebook.CellKindRecord && !cv.stored:
		rule = styletokens.ErrorDefault
	case cv.stale != "", cv.staleRead != "":
		rule = styletokens.WarningDefault
	}
	bar := styletokens.StrokeStrong
	return c.Frame(inst.ids.PrepareStr("cell")).
		EdgeBarLeft(bar, color.Hex(rule.AsHex())).
		InnerMarginSides(bar+styletokens.PaddingDefault(styletokens.ActiveDensity()), 0, styletokens.StrokeHair, styletokens.StrokeHair)
}

func (inst *View) renderCell(i int) {
	cell := &inst.nb.Cells[i]
	cv := &inst.cells[i]
	if cell.Kind == mdnotebook.CellKindProse {
		inst.markdown(cv.doc, cell)
		return
	}
	for range inst.cellFrame(cell, cv).KeepIter() {
		for range c.Vertical().KeepIter() {
			inst.renderCellBody(i, cell, cv)
		}
	}
}

func (inst *View) renderCellBody(i int, cell *mdnotebook.Cell, cv *cellView) {
	switch cell.Kind {
	case mdnotebook.CellKindRecord:
		if cv.stale != "" {
			for range c.Horizontal().KeepIter() {
				badge.New(inst.ids.PrepareStr("stale"), "stale").Tone(badge.ToneWarning).Send()
				weakLabel(cv.stale)
			}
		}
		inst.statement(cell, cv)
	case mdnotebook.CellKindPresentation:
		inst.statement(cell, cv)
		inst.renderResult(cell, cv)
	case mdnotebook.CellKindMedia:
		switch {
		case cv.mediaErr != "":
			for range c.Horizontal().KeepIter() {
				badge.New(inst.ids.PrepareStr("media"), "media").Tone(badge.ToneError).Send()
				c.Label(cv.mediaErr).Send()
			}
		default:
			res := waveform.RenderThumbnail(waveform.ThumbnailInput{
				Ids: inst.ids, Key: "wave", Overview: cv.overview, W: thumbW, H: thumbH, Progress: -1,
			})
			if res.Clicked {
				inst.events.Media = append(inst.events.Media, MediaClick{Cell: i, Path: cell.Path, Fraction: res.Fraction})
			}
			if cell.Alt != "" {
				weakLabel(cell.Alt)
			}
		}
	}
}

// markdown draws a cell's text and collects what was clicked in it. cell
// is the prose cell the text is, nil for a statement: scrolling and task
// boxes are prose's.
func (inst *View) markdown(doc *markdown.Doc, cell *mdnotebook.Cell) {
	in := markdown.Input{Ids: inst.ids, ScopeKey: "text", Doc: doc, LinkClaims: inst.claims}
	if cell != nil {
		in.ScrollToSection = inst.scrollSection
		if off := inst.scrollOffset; off >= cell.Start && off < cell.Stop {
			in.ScrollToOffset, in.ScrollToOffsetSet = off-cell.Start, true
		}
	}
	res := markdown.Render(in)
	for _, l := range res.Links {
		if record, column, ok := mdnotebook.ParseVal(l.URL); ok {
			inst.events.Vals = append(inst.events.Vals, ValClick{Record: record, Column: column, Label: l.Label})
			continue
		}
		inst.events.Links = append(inst.events.Links, l)
	}
	if cell != nil {
		for _, t := range res.Tasks {
			t.Offset += cell.Start
			inst.events.Tasks = append(inst.events.Tasks, t)
		}
	}
}

// claims is the view's own claim on `val:` links and the host's on its.
func (inst *View) claims(url string) bool {
	return isVal(url) || (inst.Opts.LinkClaims != nil && inst.Opts.LinkClaims(url))
}

// statement draws a cell's SQL under a fold named by its title. Hidden, a
// record is its title alone and a presentation cell draws nothing here.
func (inst *View) statement(cell *mdnotebook.Cell, cv *cellView) {
	mode := inst.Opts.Statements
	if mode == StatementsHidden {
		if cell.Kind == mdnotebook.CellKindRecord {
			weakLabel(cv.title)
		}
		return
	}
	h := c.CollapsingHeader(inst.ids.PrepareStr("statement"), c.WidgetText().Text(cv.title).Keep())
	if mode != inst.drawn {
		// The frame the option changes on sets every fold; after it the
		// fold is the reader's.
		h = h.Open(mode == StatementsOpen)
	}
	for range h.KeepIter() {
		inst.markdown(cv.doc, nil)
	}
}

func (inst *View) renderResult(cell *mdnotebook.Cell, cv *cellView) {
	if cv.refusal != nil {
		kind, text := refusalText(cv.refusal)
		for range c.Horizontal().KeepIter() {
			badge.New(inst.ids.PrepareStr("refusal"), kind).Tone(badge.ToneError).Send()
			c.Label(text).Send()
		}
		return
	}
	if cv.staleRead != "" {
		for range c.Horizontal().KeepIter() {
			badge.New(inst.ids.PrepareStr("stale"), "stale").Tone(badge.ToneWarning).Send()
			weakLabel(cv.staleRead)
		}
	}
	drawn := false
	if cell.Renderer != mdnotebook.RendererTable {
		reason := cv.noRenderer
		if cv.renderer != nil {
			for range c.IdScope(inst.ids.PrepareStr("renderer")) {
				// A child Ui of bounded height: what a renderer reads as
				// the room left is the box.
				for range c.Vertical().KeepIter() {
					c.UiSetMaxHeight(inst.resultMaxHeight())
					reason = cv.renderer(cv.result)
				}
			}
		}
		if reason == "" {
			drawn = true
		} else {
			weakLabel(reason)
		}
	}
	if !drawn {
		cv.gridFrames++
		inst.grid("main", cv.result.Main, cv.cols, &cv.grid, cv.gridFrames == gridRefitFrame)
	}
}

func (inst *View) resultMaxHeight() float32 {
	if h := inst.Opts.ResultMaxHeight; h > 0 {
		return h
	}
	return defaultResultMaxHeight
}

// grid draws a record as a table. refit asks the grid to fit its columns
// again: its first fit measures the rows of its first frame, which are
// fewer than the rows it goes on to show, so a column whose first values
// are its shortest comes out too narrow.
func (inst *View) grid(key string, rec arrow.RecordBatch, cols []recordtable.Column, state *recordtable.State, refit bool) {
	res := recordtable.RenderGrid(recordtable.Input{
		Ids: inst.ids, ScopeKey: key, Record: rec, Columns: cols, State: state, MaxHeight: inst.resultMaxHeight(), Selected: -1, Refit: refit,
	})
	if res.Err != nil {
		weakLabel(res.Err.Error())
	}
}
