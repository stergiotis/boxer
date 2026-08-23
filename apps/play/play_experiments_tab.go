package play

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"

	"encoding/json/jsontext"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/semistructured/leeway/card"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/leewaywidgets"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap"
)

// The Experiments tab is a leeway sink playground: it drives one batch through
// whichever `streamreadaccess.SinkI` the user picks and shows what that sink
// makes of it. Its purpose is to make leeway's read path legible — the same
// Begin*/End* callback sequence rendered six different ways, side by side in
// time — and to give every emitter in the tree a venue that isn't a CLI
// invocation nobody runs.
//
// Two sources. The leewaywidgets fixture is a small hand-authored batch that
// exercises plain sections, a co-section group and a repeated tagged section;
// it is always available, so the pane is useful before a query has ever run.
// The current result is whatever the active query returned, usable only when
// its schema is leeway-shaped (CardDriver.EnsureFor decides).

type experimentsSourceE uint8

const (
	experimentsSourceFixture experimentsSourceE = iota
	experimentsSourceResult
)

type experimentsSinkE uint8

const (
	experimentsSinkCard experimentsSinkE = iota
	experimentsSinkTopology
	experimentsSinkJSON
	experimentsSinkUnicode
	experimentsSinkTopoSpark
	experimentsSinkBrailleSpark
	experimentsSinkTreemapSpark
)

const (
	// experimentsMaxRows caps how much of a result the pane drives. The sinks
	// are all whole-batch accumulators, so an unbounded result would build an
	// unbounded model on the render thread; the pane is for reading shape, and
	// shape repeats.
	experimentsMaxRows = 16

	// experimentsUnicodeWidth is the column budget handed to the unicode
	// emitter — wide enough for its box-drawn tables without wrapping in a
	// typical dock leaf.
	experimentsUnicodeWidth = 160

	// experimentsTopoWidgetChromePx is the room the treemap widget takes for
	// itself around the container it is handed: the breadcrumb bar above it,
	// measured off a tour capture at 43pt and rounded up. The summary line it
	// would otherwise draw underneath is off (topologyPointerLine), so it needs
	// no budget here.
	experimentsTopoWidgetChromePx float32 = 48
)

// experimentsTopoPaneFill is the treemap's box — the shared pane rule
// (play_pane_box.go).
//
// The floor is well under the dock tabs' because the leaf is: this pane lives
// in the tools split, and between its two control rows, its reading guide and
// its colour key it has a few hundred points to give the picture. A floor sized
// like the Treemap tab's would exceed what is left, so the box would sit at the
// floor and overflow — which is what the pane did before it read the height at
// all, and the point of reading it is to stop.
var experimentsTopoPaneFill = paneFill{
	slack: 12, minW: 240, maxW: 1600, minH: 120,
	fallbackW: 520, fallbackH: 420,
	chrome: experimentsTopoWidgetChromePx,
}

// experimentsTopoPaneProbeSalt namespaces the pane probe's r21 slot; threading
// it through the instance's id stack makes it window-unique, so two playgrounds
// size their own treemap.
const experimentsTopoPaneProbeSalt uint64 = 0xe89e21a1e57090b0

// experimentsKey is the cache key for the built output: re-drive only when the
// user changes what they asked for, or when the result underneath changes.
// Schema identity is a pointer compare, the same idiom CardDriver.EnsureFor
// uses.
type experimentsKey struct {
	source experimentsSourceE
	sink   experimentsSinkE
	schema *arrow.Schema
	nRows  int64
}

// experimentsDriver owns the pane's selection and its built output. It holds no
// data of its own — every artifact is derived from the fixture or from the
// active result.
type experimentsDriver struct {
	ids *c.WidgetIdStack

	source experimentsSourceE
	sink   experimentsSinkE

	// paneW / paneH are the last box the pane probe reported. Held across
	// frames rather than read fresh: the probe answers nothing on the first
	// frame and again on the frame a hidden tab comes back (a seq that did not
	// capture is absent from the drain), and resizing the treemap to a fallback
	// on those frames would flash.
	paneW, paneH float32

	// Built output, invalidated by key.
	key      experimentsKey
	built    bool
	notice   string
	topoSink *leewaywidgets.TopologySink
	topoView *treemap.Treemap
	jsonView typed.RetainedFffiHolderTyped[c.CodeViewJobS]
	jsonOK   bool
	textOut  []string

	// fixtureCard is the card emitter for the fixture source.
	fixtureCard *leewaywidgets.Table2CardEmitter

	// cards is this pane's OWN CardDriver for the result source. The app's is
	// already driven and rendered by the Detail tab each frame, and one emitter
	// cannot render twice in a frame without re-emitting its ids.
	cards *CardDriver

	// cardIds is the stack both card emitters derive from, held so renderCard
	// can push an id scope onto it — see the scope constants below for why a
	// separate stack alone is not enough.
	cardIds *c.WidgetIdStack
}

func newExperimentsDriver(ids *c.WidgetIdStack, cardIds *c.WidgetIdStack) (inst *experimentsDriver) {
	inst = &experimentsDriver{ids: ids, cardIds: cardIds, cards: NewCardDriver(cardIds, nil)}
	return
}

// isTextSink reports whether the sink writes lines of monospace text rather
// than driving widgets or a codeview.
func (inst *experimentsDriver) isTextSink() bool {
	switch inst.sink {
	case experimentsSinkUnicode, experimentsSinkTopoSpark,
		experimentsSinkBrailleSpark, experimentsSinkTreemapSpark:
		return true
	}
	return false
}

// renderExperimentsTab draws the control row, a reading guide for the selected
// sink, and the sink's output — the guide separated from the output by a rule,
// so what is chrome and what is the artifact stay distinguishable.
func (inst *PlayApp) renderExperimentsTab(rec arrow.RecordBatch, schema *arrow.Schema) {
	d := inst.experiments
	gap := styletokens.GapItems(styletokens.ActiveDensity())
	d.renderControls()
	c.AddSpace(gap)
	d.renderGuide()
	c.AddSpace(gap)
	c.Separator().Horizontal().Send()
	c.AddSpace(gap)
	d.ensureBuilt(rec, schema)
	d.renderBody(rec, schema)
}

// sinkGuide is how to READ each sink's output. Every one of these renders the
// same Begin*/End* callback sequence, so what changes between them is the
// encoding, not the data — and the encoding is the thing a reader has to be
// told. Kept to two lines: this is a legend, not documentation.
func sinkGuide(sink experimentsSinkE) (headline, detail string) {
	switch sink {
	case experimentsSinkCard:
		return "One row per attribute, grouped by section.",
			"Columns are section · primary memberships · secondary memberships · values. " +
				"A section header row carries its own size and share of the entity."
	case experimentsSinkTopology:
		return "Shape only — every value is discarded.",
			"Nesting is entity › co-section group › section › attribute; a cell's AREA is the " +
				"attribute count beneath it, and an attribute's COLOUR is what it carried (key below). " +
				"Click a box to drill in."
	case experimentsSinkJSON:
		return "The canonical lossless card-JSON (ADR-0018).",
			"byStructure holds the schema once per entity; byAttribute is rooted at primary " +
				"memberships. Scalars keep their JSON type — numbers are not stringified."
	case experimentsSinkUnicode:
		return "One box-drawn table per section.",
			"Column headers are the section's value names, one row per attribute. The widest " +
				"cell sets the column, so ragged sections show as ragged tables."
	case experimentsSinkTopoSpark:
		return "One line per entity — arity and types, no values.",
			"◆ plain section · ◇N× tagged section with N attributes · ⟨…⟩ its column canonical " +
				"types · ∥n array of n · {n} set of n · #n membership count · ˡ ʰ ᵐ low/high/mixed cardinality."
	case experimentsSinkBrailleSpark:
		return "One braille cell per four attributes.",
			"Within a cell the LEFT dot column marks attributes that carried a value and the " +
				"RIGHT column those that carried tags; │ separates sections, ⟦ ⟧ wrap a co-section group."
	case experimentsSinkTreemapSpark:
		return "Three lines per entity: a proportional box row.",
			"Box width follows the section's column count. Inside, █ is value+tags, ▓ value only, " +
				"░ tags only, · an empty slot; ═ double rules mark a co-section group."
	}
	return "", ""
}

// renderGuide draws the two-line reading guide for the active sink.
func (inst *experimentsDriver) renderGuide() {
	headline, detail := sinkGuide(inst.sink)
	if headline == "" {
		return
	}
	c.LabelAtoms(c.Atoms().BeginRichText(headline).Strong().End().Keep()).Send()
	if detail == "" {
		return
	}
	// A text sink's guide names the glyphs its output is made of, so it has to
	// be set in the same face: the proportional UI font has no ⟨ ⟩ and draws
	// tofu where the monospace output draws brackets.
	rt := c.Atoms().BeginRichText(detail).Small().Weak()
	if inst.isTextSink() {
		rt = rt.Monospace()
	}
	c.LabelAtoms(rt.End().Keep()).Wrap().Send()
}

func (inst *experimentsDriver) renderControls() {
	gap := styletokens.GapSections(styletokens.ActiveDensity())
	for range c.HorizontalTop().KeepIter() {
		c.Label("source").Send() // designlint:ignore=L1 (field caption; lowercase matches its control's own options)
		selector.Segmented(inst.ids, "exp-source", &inst.source).
			Inline().
			Frameless().
			Style(selector.StyleSelectable).
			Option(experimentsSourceFixture, "fixture").
			Option(experimentsSourceResult, "result").
			SendResp()
		c.AddSpace(gap)
		c.Label("sink").Send() // designlint:ignore=L1 (field caption; lowercase matches its control's own options)
		selector.Segmented(inst.ids, "exp-sink", &inst.sink).
			Inline().
			Frameless().
			Style(selector.StyleSelectable).
			Option(experimentsSinkCard, "card").
			Option(experimentsSinkTopology, "topology").
			Option(experimentsSinkJSON, "json").
			Option(experimentsSinkUnicode, "unicode").
			Option(experimentsSinkTopoSpark, "topo").
			Option(experimentsSinkBrailleSpark, "braille").
			Option(experimentsSinkTreemapSpark, "treemap").
			SendResp()
	}
}

// ensureBuilt (re)drives the selected sink when the selection or the underlying
// result changed. The card sink is excluded: its emitter must be re-driven every
// frame to keep widget ids stable, so renderBody handles it directly.
func (inst *experimentsDriver) ensureBuilt(rec arrow.RecordBatch, schema *arrow.Schema) {
	var nRows int64
	if rec != nil {
		nRows = rec.NumRows()
	}
	key := experimentsKey{source: inst.source, sink: inst.sink, schema: schema, nRows: nRows}
	if inst.built && inst.key == key {
		return
	}
	inst.key = key
	inst.built = true
	inst.notice = ""
	inst.topoSink = nil
	inst.topoView = nil
	inst.textOut = nil
	inst.jsonOK = false

	if inst.sink == experimentsSinkCard {
		return
	}

	sink, finish := inst.makeSink()
	if sink == nil {
		return
	}
	if !inst.drive(sink, rec, schema) {
		return
	}
	finish()
}

// makeSink builds the sink for the current selection and returns the closure
// that turns its accumulated state into something renderable. Both are nil for
// a selection that needs no driving.
func (inst *experimentsDriver) makeSink() (sink streamreadaccess.SinkI, finish func()) {
	buf := bytes.NewBuffer(make([]byte, 0, 4096))
	switch inst.sink {
	case experimentsSinkTopology:
		topo := leewaywidgets.NewTopologySink()
		inst.topoSink = topo
		return topo, func() {
			// The widget's own summary line is off: this pane draws
			// topologyPointerLine above the picture instead, in the unit these
			// sizes are actually in.
			inst.topoView = leewaywidgets.NewTopologyTreemap(inst.ids, "play-exp-topo", topo,
				treemap.WithStatusLine(false))
		}
	case experimentsSinkJSON:
		enc := jsontext.NewEncoder(buf, jsontext.Multiline(true), jsontext.WithIndent("  "))
		return card.NewJsonCardEmitter(enc, nil), func() {
			inst.jsonView = codeview.PrepareJson(buf.String())
			inst.jsonOK = true
		}
	case experimentsSinkUnicode:
		return card.NewUnicodeCardEmitter(buf, experimentsUnicodeWidth), func() {
			inst.textOut = splitTextOutput(buf.String())
		}
	case experimentsSinkTopoSpark:
		return card.NewTopologySpark(buf), func() { inst.textOut = splitTextOutput(buf.String()) }
	case experimentsSinkBrailleSpark:
		return card.NewBrailleSpark(buf), func() { inst.textOut = splitTextOutput(buf.String()) }
	case experimentsSinkTreemapSpark:
		return card.NewTreemapSpark(buf), func() { inst.textOut = splitTextOutput(buf.String()) }
	}
	return nil, nil
}

// drive runs the batch through sink, reporting false (with inst.notice set)
// when the selected source cannot supply one.
func (inst *experimentsDriver) drive(sink streamreadaccess.SinkI, rec arrow.RecordBatch, schema *arrow.Schema) (ok bool) {
	if inst.source == experimentsSourceFixture {
		leewaywidgets.RunFixture(sink)
		return true
	}
	if rec == nil || schema == nil || rec.NumRows() == 0 {
		inst.notice = "No result yet — run a query, or switch the source to the fixture."
		return false
	}
	if !inst.cards.EnsureFor(schema) {
		inst.notice = "The current result is not leeway-shaped, so it carries no section structure to drive. Switch the source to the fixture."
		return false
	}
	drv := inst.cards.Driver()
	if drv == nil {
		inst.notice = "No leeway driver for the current result."
		return false
	}
	n := rec.NumRows()
	if n > experimentsMaxRows {
		n = experimentsMaxRows
		inst.notice = "Showing the first " + strconv.FormatInt(experimentsMaxRows, 10) +
			" of " + strconv.FormatInt(rec.NumRows(), 10) + " rows."
	}
	// One slice, not one per row: every sink brackets its work in
	// BeginBatch/EndBatch, and driving row-by-row would reset the accumulator
	// each time and leave only the last row's model standing.
	slice := rec.NewSlice(0, n)
	defer slice.Release()
	if err := drv.DriveRecordBatch(sink, slice); err != nil {
		inst.notice = "Driving the result failed: " + err.Error()
		return false
	}
	return true
}

func (inst *experimentsDriver) renderBody(rec arrow.RecordBatch, schema *arrow.Schema) {
	if inst.notice != "" {
		c.Label(inst.notice).Send()
		if inst.sink != experimentsSinkTopology || inst.topoView == nil {
			c.AddSpace(styletokens.GapItems(styletokens.ActiveDensity()))
		}
	}
	switch {
	case inst.sink == experimentsSinkCard:
		inst.renderCard(rec, schema)
	case inst.sink == experimentsSinkTopology:
		inst.renderTopology()
	case inst.sink == experimentsSinkJSON:
		if inst.jsonOK {
			c.CodeView(inst.ids.PrepareStr("exp-json"), inst.jsonView).Wrap().Send()
		}
	case inst.isTextSink():
		inst.renderText()
	}
}

// Id-scope seeds for the two card emitters this pane can draw. Both must be
// non-zero and distinct: Table2CardEmitter derives every cell id from a
// per-section counter via PrepareSeq, and PrepareSeq maps its argument through
// makeHighEntropy alone — the *WidgetIdStack instance contributes nothing. Two
// stacks built by the app's mk() therefore share a base salt and, with nothing
// pushed, produce byte-identical ids. Detail renders the same emitter in the
// same frame, so without a pushed scope every cell id here is a duplicate of
// Detail's and egui reports the clash (table2_emitter.go §renderSectionHeaderRow).
const (
	experimentsCardScopeFixture uint64 = 0xE7C1
	experimentsCardScopeResult  uint64 = 0xE7C2

	// experimentsCardSaltMix is XORed into the base salt of the stack those two
	// emitters derive from, so the pane's ids are disjoint from the app's card
	// stack rather than merely scoped apart. See the construction site in
	// play_renderer.go.
	experimentsCardSaltMix uint64 = 0x5EED_E7C0_0000_0001
)

// renderCard drives and draws the Table2 card emitter. Unlike the other sinks
// this happens every frame: the emitter re-bases its widget-id counter at each
// drive, so a cached drive would leave the deferred render pointing at stale
// ids.
//
// Each arm renders inside an IdScope on the SAME stack the emitter derives
// from — scoping a different stack would not move these ids, since the scope is
// pushed onto the instance it is taken from.
func (inst *experimentsDriver) renderCard(rec arrow.RecordBatch, schema *arrow.Schema) {
	if inst.source == experimentsSourceFixture {
		if inst.fixtureCard == nil {
			inst.fixtureCard = leewaywidgets.NewTable2CardEmitter(inst.cardIds, leewaywidgets.ColorPaletteViridis, nil)
			// Without this the emitter flushes at EndBatch — i.e. inside
			// RunFixture — and the Render() below draws the same widgets a
			// second time. CardDriver sets it for the same reason.
			inst.fixtureCard.DeferRender = true
		}
		leewaywidgets.RunFixture(inst.fixtureCard)
		for range c.IdScope(inst.cardIds.PrepareSeq(experimentsCardScopeFixture)) {
			inst.fixtureCard.Render()
		}
		return
	}
	if rec == nil || schema == nil || rec.NumRows() == 0 {
		c.Label("No result yet — run a query, or switch the source to the fixture.").Send()
		return
	}
	if !inst.cards.EnsureFor(schema) {
		c.Label("The current result is not leeway-shaped. Switch the source to the fixture.").Send()
		return
	}
	if err := inst.cards.Prepare(rec, 0); err != nil {
		c.Label("Driving the result failed: " + err.Error()).Send()
		return
	}
	for range c.IdScope(inst.cardIds.PrepareSeq(experimentsCardScopeResult)) {
		inst.cards.Render()
	}
}

// renderTopology sizes the treemap against the pane and draws it. Sizing is
// per-frame (the dock leaf is resizable) and floored so a short leaf scrolls
// rather than collapsing the widget to nothing.
func (inst *experimentsDriver) renderTopology() {
	if inst.topoView == nil {
		return
	}
	// Legend first, then measure: the colours are data-bearing, so the key has
	// to be on screen whatever the pane's height. Drawing it above means the
	// probe already excludes it and the treemap needs no guessed reserve for it
	// — sizing the treemap to the full pane and putting the legend under it
	// pushed the key below the fold.
	//
	// The probe is seq-keyed and window-unique (one frame behind). NOT
	// CaptureAvailableSize: one process-wide slot the frame's last capture
	// wins, and this tab lives in the tools leaf, which renders BEFORE every
	// body tab — so any body pane that captured (Projection, Timeline,
	// Distribution) sized this treemap, no Detail pane needed.
	inst.renderTopologyLegend()
	// The readout goes ABOVE the picture, as the Treemap and Icicle tabs' do. It
	// is drawn unconditionally, hover or no hover, and that is load-bearing
	// here: a line that appeared only while pointing at a cell would change what
	// the probe below measures, and the picture would resize under the pointer.
	c.Label(inst.topologyPointerLine()).Send()
	if availW, availH, ok := c.CapturePaneSize(inst.ids.PrepareHighEntropy(experimentsTopoPaneProbeSalt).Derive()); ok &&
		availW > 0 && availH > 0 &&
		!math.IsNaN(float64(availW)) && !math.IsNaN(float64(availH)) {
		inst.paneW, inst.paneH = availW, availH
	}
	// The box is the pane less what the WIDGET draws around it — the reserve
	// used to be 8pt for a control row the probe already excludes, which left
	// the breadcrumb bar and the summary line unbudgeted and the picture that
	// far past the leaf.
	inst.topoView.SetContainerSize(experimentsTopoPaneFill.box(inst.paneW, inst.paneH))
	inst.topoView.Render()
}

// topologyPointerLine reads the box under the pointer, or names the gesture
// when there is nothing under it.
//
// It replaces the widget's own summary line (WithStatusLine(false) at
// construction), which ran its totals through formatBytes: right for the
// filesystem trees that widget was written against, wrong here, where a cell's
// size is an ATTRIBUTE COUNT. It read "total size: 7 B" for seven attributes —
// and read it invisibly, since it sat below the fold until this pane started
// budgeting its leaf for the widget's chrome.
func (inst *experimentsDriver) topologyPointerLine() string {
	if inst.topoView != nil {
		if n := inst.topoView.HoveredNode(); n != nil {
			return fmt.Sprintf("%s — %.0f attribute(s)", n.Name, n.TotalSize())
		}
	}
	return "hover a box for its name and attribute count"
}

// renderTopologyLegend names the four attribute states. The colours are
// data-bearing — the whole point of the view — so they need a key.
func (inst *experimentsDriver) renderTopologyLegend() {
	gap := styletokens.GapItems(styletokens.ActiveDensity())
	for range c.HorizontalTop().KeepIter() {
		for _, st := range []leewaywidgets.AttrStateE{
			leewaywidgets.AttrStateValueAndTags,
			leewaywidgets.AttrStateValueOnly,
			leewaywidgets.AttrStateTagsOnly,
			leewaywidgets.AttrStateEmpty,
		} {
			// Coloured through the same palette the cells use — a swatch in
			// the default text colour would be a key to nothing.
			for scope := range c.RichTextLabelColored(
				leewaywidgets.AttrStateColor(st), color.Transparent, "■ "+st.String()) {
				scope.Small()
			}
			c.AddSpace(gap)
		}
	}
}

// renderText draws the monospace sinks one line per label. Box-drawing and
// braille output only aligns in a monospace face, and a per-line label keeps
// long rows from being re-wrapped into nonsense.
func (inst *experimentsDriver) renderText() {
	for _, line := range inst.textOut {
		if line == "" {
			c.AddSpace(styletokens.GapItems(styletokens.ActiveDensity()))
			continue
		}
		c.LabelAtoms(c.Atoms().BeginRichText(line).Monospace().End().Keep()).Send()
	}
}

// splitTextOutput normalises a text sink's buffer into renderable lines,
// dropping the trailing empty line a final newline produces.
func splitTextOutput(s string) (lines []string) {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
