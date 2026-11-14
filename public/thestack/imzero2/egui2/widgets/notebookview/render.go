package notebookview

import (
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
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
			if inst.header(cell, cv) {
				inst.renderCellBody(i, cell, cv)
			}
		}
	}
}

// hasBody says whether a cell has anything under its header. A record's
// body is its statement: with the statements hidden it is one line.
func (inst *View) hasBody(cell *mdnotebook.Cell, cv *cellView) bool {
	if cell.Kind == mdnotebook.CellKindRecord {
		return inst.Opts.Statements == StatementsShown || cv.stale != ""
	}
	return true
}

// header draws a cell's header: its title, which is the fold of the whole
// cell, and its state, so a folded cell still says what it is and whether
// something is wrong with it. It reports whether the body is to be drawn;
// under a folded header nothing is built, so a folded cell costs its
// header alone.
func (inst *View) header(cell *mdnotebook.Cell, cv *cellView) (open bool) {
	title := cv.title
	if cell.Kind == mdnotebook.CellKindPresentation && inst.Opts.Statements == StatementsShown {
		// As the fence spells it, beside the statement it heads.
		title = "sql " + title
	}
	body := inst.hasBody(cell, cv)
	for range c.Horizontal().KeepIter() {
		if body {
			caret := icons.PhCaretDown
			if cv.folded {
				caret = icons.PhCaretRight
			}
			if c.Button(inst.ids.PrepareStr("fold"), c.Atoms().Text(caret+" "+title).Keep()).Frame(false).
				SendResp().HasPrimaryClicked() {
				cv.folded = !cv.folded
				if inst.folds == nil {
					inst.folds = make(map[string]bool)
				}
				if cv.folded {
					inst.folds[cv.foldKey] = true
				} else {
					delete(inst.folds, cv.foldKey)
				}
				// The cell was drawn before the click was known.
				c.RequestRepaint()
			}
		} else {
			weakLabel(title)
		}
		switch {
		case cv.refusal != nil:
			kind, _ := refusalText(cv.refusal)
			badge.New(inst.ids.PrepareStr("state"), kind).Tone(badge.ToneError).Send()
		case cv.mediaErr != "":
			badge.New(inst.ids.PrepareStr("state"), "media").Tone(badge.ToneError).Send()
		case cv.stale != "", cv.staleRead != "":
			badge.New(inst.ids.PrepareStr("state"), "stale").Tone(badge.ToneWarning).Send()
		}
	}
	return body && !cv.folded
}

func (inst *View) renderCellBody(i int, cell *mdnotebook.Cell, cv *cellView) {
	switch cell.Kind {
	case mdnotebook.CellKindRecord:
		if cv.stale != "" {
			weakLabel(cv.stale)
		}
		inst.statement(cv)
	case mdnotebook.CellKindPresentation:
		inst.statement(cv)
		inst.renderResult(cell, cv)
	case mdnotebook.CellKindMedia:
		if cv.mediaErr != "" {
			c.Label(cv.mediaErr).Send()
			return
		}
		inst.recording(i, cell, cv)
	}
}

// recordingRepaintHz keeps a playhead moving while its recording plays.
const recordingRepaintHz = 30

// recording draws a recording: its waveform, and with a host that plays
// recordings a playhead on it and the transport under it — play or pause,
// the position over the length, and why nothing is heard when nothing is.
// A click on the waveform moves the playhead of the recording that plays
// and starts one that does not; it is reported either way.
func (inst *View) recording(i int, cell *mdnotebook.Cell, cv *cellView) {
	player := inst.Opts.Recordings
	var st RecordingState
	if player != nil {
		st = player.State(cell.Path)
	}
	progress := -1.0
	if st.Active && !st.Opening {
		progress = st.Fraction
	}
	res := waveform.RenderThumbnail(waveform.ThumbnailInput{
		Ids: inst.ids, Key: "wave", Overview: cv.overview, W: thumbW, H: thumbH, Progress: progress,
	})
	if res.Clicked {
		inst.events.Media = append(inst.events.Media, MediaClick{Cell: i, Path: cell.Path, Fraction: res.Fraction})
		if player != nil {
			if !st.Active {
				player.Toggle(cell.Path)
			}
			player.Seek(cell.Path, res.Fraction)
			c.RequestRepaint()
		}
	}
	if player == nil {
		return
	}
	for range c.HorizontalTop().KeepIter() {
		// The word beside the glyph is the button's name: what a person
		// reads and a scene clicks.
		label := icons.PhPlay + " Play"
		if st.Playing {
			label = icons.PhPause + " Pause"
		}
		for range c.EnabledUi(!st.Opening).KeepIter() {
			if c.Button(inst.ids.PrepareStr("toggle"), c.Atoms().Text(label).Keep()).Small().
				SendResp().HasPrimaryClicked() {
				player.Toggle(cell.Path)
				// The row was drawn before the click was known: a paused
				// recording asks for no frame of its own.
				c.RequestRepaint()
			}
		}
		readout := gloss.FormatClock(cv.duration)
		if st.Active {
			readout = gloss.FormatClock(st.Position) + " / " + readout
		}
		c.LabelAtoms(c.Atoms().BeginRichText(readout).Small().Monospace().End().Keep()).Selectable(false).Send()
		if st.Note != "" {
			weakLabel(st.Note)
		}
	}
	if st.Playing || st.Opening {
		c.RequestRepaintAfter(1.0 / recordingRepaintHz)
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
// statement draws a cell's statement, where statements are shown.
func (inst *View) statement(cv *cellView) {
	if inst.Opts.Statements == StatementsShown {
		inst.markdown(cv.doc, nil)
	}
}

func (inst *View) renderResult(cell *mdnotebook.Cell, cv *cellView) {
	// The header carries the badge; here is what it stands for.
	if cv.refusal != nil {
		_, text := refusalText(cv.refusal)
		c.Label(text).Send()
		return
	}
	if cv.staleRead != "" {
		weakLabel(cv.staleRead)
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
