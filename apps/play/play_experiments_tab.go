package play

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"

	"encoding/json/jsontext"
	"encoding/json/v2"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/card"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/leewaywidgets"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval"
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
//
// The sinks, their row caps and their settings are vizeval's catalogue
// (ADR-0257, proposed, §SD1–SD2): the sink bar and the option controls are
// drawn from it, and TestExperimentsImplementsTheCatalogue holds the pane to
// it. What the pane shows can be seeded at launch as one candidate
// (BOXER_PLAY_EXPERIMENTS, §SD3), and its output is wrapped in one named
// accessibility node (experimentsArtifactName) so a headless run can find it.

type experimentsSourceE uint8

const (
	experimentsSourceFixture experimentsSourceE = iota
	experimentsSourceResult
)

const (
	// experimentsArtifactName names the accessibility node around the sink's
	// output — the rect a capture is cropped to and metrics are scoped to.
	experimentsArtifactName = "experiments.artifact"

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

// experimentsChartPaneProbeSalt is the chart's probe slot, apart from the
// treemap's so the two never read each other's box.
const experimentsChartPaneProbeSalt uint64 = 0x3c7a1f0e5b2d9c41

// experimentsChartPaneFill is the chart's box: the pane less a little slack,
// floored where tick labels stop fitting.
var experimentsChartPaneFill = paneFill{
	slack: 12, minW: 360, maxW: 2400, minH: 200,
	fallbackW: 900, fallbackH: 420,
}

// experimentsKey is the cache key for the built output: re-drive only when the
// user changes what they asked for, or when the result underneath changes.
// Schema identity is a pointer compare, the same idiom CardDriver.EnsureFor
// uses.
type experimentsKey struct {
	source    experimentsSourceE
	candidate string
	schema    *arrow.Schema
	nRows     int64
}

// experimentsKnob is one option control's state. Numeric controls bind a
// float64 whatever the option's kind; the candidate rounds an int option.
type experimentsKnob struct {
	text string
	num  float64
	flag bool
}

// experimentsDriver owns the pane's selection and its built output. It holds no
// data of its own — every artifact is derived from the fixture or from the
// active result.
type experimentsDriver struct {
	ids *c.WidgetIdStack

	source experimentsSourceE
	// sink is a vizeval sink id.
	sink string
	// knobs holds every catalogued sink's option state, by sink id then option
	// name, so switching sinks and back keeps what was set.
	knobs map[string]map[string]*experimentsKnob

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
	// chartModel is the chart sink's projection of the batch; chartView draws
	// it and keeps the heatmap's colour scale across frames.
	chartModel *leewaywidgets.ChartModel
	chartView  *leewaywidgets.ChartView
	jsonView   typed.RetainedFffiHolderTyped[c.CodeViewJobS]
	jsonOK     bool
	textOut    []string

	// card is the pane's card emitter, for both sources; cardPalette is the
	// palette it was built with, since the emitter takes it at construction.
	card        *leewaywidgets.Table2CardEmitter
	cardPalette string

	// cards is this pane's OWN CardDriver for the result source: it decides
	// whether a result is leeway-shaped and holds the driver for it. The app's
	// is already driven and rendered by the Detail tab each frame.
	cards *CardDriver

	// cardIds is the stack both card emitters derive from, held so renderCard
	// can push an id scope onto it — see the scope constants below for why a
	// separate stack alone is not enough.
	cardIds *c.WidgetIdStack
}

func newExperimentsDriver(ids *c.WidgetIdStack, cardIds *c.WidgetIdStack) (inst *experimentsDriver) {
	inst = &experimentsDriver{
		ids: ids, cardIds: cardIds, cards: NewCardDriver(cardIds, nil),
		sink:  vizeval.SinkCard,
		knobs: make(map[string]map[string]*experimentsKnob, len(vizeval.Sinks())),
	}
	for _, spec := range vizeval.Sinks() {
		inst.knobs[spec.ID] = make(map[string]*experimentsKnob, len(spec.Space))
		inst.setKnobs(spec, spec.Space.Defaults())
	}
	return
}

// setKnobs puts resolved values into a sink's controls.
func (inst *experimentsDriver) setKnobs(spec vizeval.SinkSpec, vals vizeval.Values) {
	for _, o := range spec.Space {
		k := &experimentsKnob{}
		switch v := vals[o.Name].(type) {
		case string:
			k.text = v
		case int64:
			k.num = float64(v)
		case float64:
			k.num = v
		case bool:
			k.flag = v
		}
		inst.knobs[spec.ID][o.Name] = k
	}
}

// spec is the selected sink's catalogue entry.
func (inst *experimentsDriver) spec() vizeval.SinkSpec {
	spec, ok := vizeval.SinkByID(inst.sink)
	if !ok {
		// Unreachable: sink is only ever set from the catalogue.
		panic("experiments: sink not in the vizeval catalogue: " + inst.sink)
	}
	return spec
}

// candidate is what the controls currently say: the sink and its options,
// resolved. The controls are bounded by the same declaration, so resolving
// fails only on a declaration bug; the error is carried all the same.
func (inst *experimentsDriver) candidate() (cand vizeval.Candidate, err error) {
	spec := inst.spec()
	raw := make(map[string]any, len(spec.Space))
	for _, o := range spec.Space {
		k := inst.knobs[spec.ID][o.Name]
		switch o.Kind {
		case vizeval.OptionKindEnum:
			raw[o.Name] = k.text
		case vizeval.OptionKindInt:
			raw[o.Name] = math.Round(k.num)
		case vizeval.OptionKindFloat:
			raw[o.Name] = k.num
		case vizeval.OptionKindBool:
			raw[o.Name] = k.flag
		}
	}
	return vizeval.NewCandidate(spec.ID, raw)
}

// experimentsSeed is BOXER_PLAY_EXPERIMENTS: a candidate plus the source it is
// drawn from.
type experimentsSeed struct {
	Source  string         `json:"source"`
	Sink    string         `json:"sink"`
	Options map[string]any `json:"options"`
}

// applySeed puts the pane in the state a seed names. Anything that does not
// resolve against the catalogue is refused — the caller fails the mount —
// rather than drawn with defaults, because a scripted run that captures the
// wrong candidate is worse than one that stops (ADR-0257 §SD3).
func (inst *experimentsDriver) applySeed(raw string) (err error) {
	var seed experimentsSeed
	if err = json.Unmarshal([]byte(raw), &seed, json.RejectUnknownMembers(true)); err != nil {
		return eh.Errorf("unable to decode the experiments seed: %w", err)
	}
	cand, err := vizeval.NewCandidate(seed.Sink, seed.Options)
	if err != nil {
		return eh.Errorf("unable to resolve the experiments seed: %w", err)
	}
	switch seed.Source {
	case "", "fixture":
		inst.source = experimentsSourceFixture
	case "result":
		inst.source = experimentsSourceResult
	default:
		return eb.Build().Str("source", seed.Source).Errorf("unknown experiments source (want fixture or result)")
	}
	inst.sink = cand.Sink
	inst.setKnobs(inst.spec(), cand.Options)
	inst.built = false
	return nil
}

// isTextSink reports whether the sink writes lines of monospace text rather
// than driving widgets or a codeview.
func (inst *experimentsDriver) isTextSink() bool {
	switch inst.sink {
	case vizeval.SinkUnicode, vizeval.SinkTopoSpark,
		vizeval.SinkBrailleSpark, vizeval.SinkTreemapSpark:
		return true
	}
	return false
}

// capRows is how many of n rows the selected sink draws, and the notice to
// show when that is fewer than n: the cut is said, never silent (ADR-0257
// §SD1).
func (inst *experimentsDriver) capRows(n int64) (drawn int64, notice string) {
	spec := inst.spec()
	if n <= spec.RowCap {
		return n, ""
	}
	return spec.RowCap, "Showing the first " + strconv.FormatInt(spec.RowCap, 10) +
		" of " + strconv.FormatInt(n, 10) + " rows — the " + spec.Title + "'s row cap."
}

// experimentsPalettes maps the catalogue's palette names onto the emitter's.
var experimentsPalettes = map[string]leewaywidgets.ColorPaletteE{
	"inferno": leewaywidgets.ColorPaletteInferno,
	"viridis": leewaywidgets.ColorPaletteViridis,
	"magma":   leewaywidgets.ColorPaletteMagma,
	"plasma":  leewaywidgets.ColorPalettePlasma,
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
	cand, err := d.candidate()
	if err != nil {
		c.Label("The controls do not resolve to a candidate: " + err.Error()).Send()
		return
	}
	d.ensureBuilt(rec, schema, cand)
	d.renderBody(rec, schema, cand)
}

// sinkGuide is how to READ each sink's output. Every one of these renders the
// same Begin*/End* callback sequence, so what changes between them is the
// encoding, not the data — and the encoding is the thing a reader has to be
// told. Kept to two lines: this is a legend, not documentation.
func sinkGuide(sink string) (headline, detail string) {
	switch sink {
	case vizeval.SinkCard:
		return "One row per attribute, grouped by section.",
			"Columns are section · primary memberships · secondary memberships · values. " +
				"A section header row carries its own size and share of the entity."
	case vizeval.SinkTopology:
		return "Shape only — every value is discarded.",
			"Nesting is entity › co-section group › section › attribute; a cell's AREA is the " +
				"attribute count beneath it, and an attribute's COLOUR is what it carried (key below). " +
				"Click a box to drill in."
	case vizeval.SinkJSON:
		return "The canonical lossless card-JSON (ADR-0018).",
			"byStructure holds the schema once per entity; byAttribute is rooted at primary " +
				"memberships. Scalars keep their JSON type — numbers are not stringified."
	case vizeval.SinkUnicode:
		return "One box-drawn table per section.",
			"Column headers are the section's value names, one row per attribute. The widest " +
				"cell sets the column, so ragged sections show as ragged tables."
	case vizeval.SinkTopoSpark:
		return "One line per entity — arity and types, no values.",
			"◆ plain section · ◇N× tagged section with N attributes · ⟨…⟩ its column canonical " +
				"types · ∥n array of n · {n} set of n · #n membership count · ˡ ʰ ᵐ low/high/mixed cardinality."
	case vizeval.SinkBrailleSpark:
		return "One braille cell per four attributes.",
			"Within a cell the LEFT dot column marks attributes that carried a value and the " +
				"RIGHT column those that carried tags; │ separates sections, ⟦ ⟧ wrap a co-section group."
	case vizeval.SinkChart:
		return "One category per entity, one series per membership.",
			"The first tagged section with a numeric value is charted: each attribute's value, in the series its " +
				"first membership names. seriesBy entity swaps the two; a heatmap puts series on rows."
	case vizeval.SinkTreemapSpark:
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
		bar := selector.Segmented(inst.ids, "exp-sink", &inst.sink).
			Inline().
			Frameless().
			Style(selector.StyleSelectable)
		for _, spec := range vizeval.Sinks() {
			bar = bar.Option(spec.ID, spec.ID)
		}
		bar.SendResp()
	}
	inst.renderOptionControls()
}

// renderOptionControls draws the selected sink's settings from its declared
// space: a segmented bar for an enum, a slider for a number, a checkbox for a
// flag. A sink with no settings draws nothing.
func (inst *experimentsDriver) renderOptionControls() {
	spec := inst.spec()
	if len(spec.Space) == 0 {
		return
	}
	gap := styletokens.GapSections(styletokens.ActiveDensity())
	for range c.HorizontalTop().KeepIter() {
		for i, o := range spec.Space {
			if i > 0 {
				c.AddSpace(gap)
			}
			k := inst.knobs[spec.ID][o.Name]
			key := "exp-opt-" + spec.ID + "-" + o.Name
			switch o.Kind {
			case vizeval.OptionKindEnum:
				c.Label(o.Name).Send() // designlint:ignore=L1 (field caption; the option's own name, as a seed spells it)
				bar := selector.Segmented(inst.ids, key, &k.text).
					Inline().
					Frameless().
					Style(selector.StyleSelectable)
				for _, ch := range o.Choices {
					bar = bar.Option(ch, ch)
				}
				bar.SendResp()
			case vizeval.OptionKindInt:
				c.SliderF64(inst.ids.PrepareStr(key), k.num, o.Min, o.Max).Integer().Text(o.Name).SendRespVal(&k.num)
			case vizeval.OptionKindFloat:
				c.SliderF64(inst.ids.PrepareStr(key), k.num, o.Min, o.Max).Text(o.Name).SendRespVal(&k.num)
			case vizeval.OptionKindBool:
				c.Checkbox(inst.ids.PrepareStr(key), k.flag, o.Name).SendRespVal(&k.flag)
			}
		}
	}
}

// ensureBuilt (re)drives the selected sink when the selection or the underlying
// result changed. The card sink is excluded: its emitter must be re-driven every
// frame to keep widget ids stable, so renderBody handles it directly.
func (inst *experimentsDriver) ensureBuilt(rec arrow.RecordBatch, schema *arrow.Schema, cand vizeval.Candidate) {
	var nRows int64
	if rec != nil {
		nRows = rec.NumRows()
	}
	key := experimentsKey{source: inst.source, candidate: cand.ID(), schema: schema, nRows: nRows}
	if inst.built && inst.key == key {
		return
	}
	inst.key = key
	inst.built = true
	inst.notice = ""
	inst.topoSink = nil
	inst.topoView = nil
	inst.chartModel = nil
	inst.textOut = nil
	inst.jsonOK = false

	if inst.sink == vizeval.SinkCard {
		return
	}

	sink, finish := inst.makeSink(cand)
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
func (inst *experimentsDriver) makeSink(cand vizeval.Candidate) (sink streamreadaccess.SinkI, finish func()) {
	buf := bytes.NewBuffer(make([]byte, 0, 4096))
	switch cand.Sink {
	case vizeval.SinkTopology:
		topo := leewaywidgets.NewTopologySink()
		inst.topoSink = topo
		return topo, func() {
			// The widget's own summary line is off: this pane draws
			// topologyPointerLine above the picture instead, in the unit these
			// sizes are actually in.
			inst.topoView = leewaywidgets.NewTopologyTreemap(inst.ids, "play-exp-topo", topo,
				treemap.WithStatusLine(false))
		}
	case vizeval.SinkJSON:
		enc := jsontext.NewEncoder(buf, jsontext.Multiline(true), jsontext.WithIndent("  "))
		return card.NewJsonCardEmitter(enc, nil), func() {
			inst.jsonView = codeview.PrepareJson(buf.String())
			inst.jsonOK = true
		}
	case vizeval.SinkUnicode:
		width, _ := cand.Options[vizeval.OptionWidth].(int64)
		cfg := card.DefaultUnicodeEmitterConfig()
		if maxCol, ok := cand.Options[vizeval.OptionMaxColumnWidth].(int64); ok {
			cfg.MaxColumnWidth = int(maxCol)
		}
		return card.NewUnicodeCardEmitterWithConfig(buf, int(width), cfg), func() {
			inst.textOut = splitTextOutput(buf.String())
		}
	case vizeval.SinkTopoSpark:
		return card.NewTopologySpark(buf), func() { inst.textOut = splitTextOutput(buf.String()) }
	case vizeval.SinkBrailleSpark:
		return card.NewBrailleSpark(buf), func() { inst.textOut = splitTextOutput(buf.String()) }
	case vizeval.SinkTreemapSpark:
		return card.NewTreemapSpark(buf), func() { inst.textOut = splitTextOutput(buf.String()) }
	case vizeval.SinkChart:
		cs := leewaywidgets.NewChartSink()
		return cs, func() { inst.chartModel = cs.Model() }
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
	n, notice := inst.capRows(rec.NumRows())
	inst.notice = notice
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

// renderBody draws the notice, if any, and then the artifact inside its named
// accessibility region. The notice stays outside the region: it is the pane
// talking about the picture, not part of it.
func (inst *experimentsDriver) renderBody(rec arrow.RecordBatch, schema *arrow.Schema, cand vizeval.Candidate) {
	var cardReady bool
	if cand.Sink == vizeval.SinkCard {
		cardReady = inst.prepareCard(rec, schema, cand)
	}
	if inst.notice != "" {
		c.Label(inst.notice).Send()
		if inst.sink != vizeval.SinkTopology || inst.topoView == nil {
			c.AddSpace(styletokens.GapItems(styletokens.ActiveDensity()))
		}
	}
	for range c.AccessibleRegion(experimentsArtifactName).KeepIter() {
		switch {
		case inst.sink == vizeval.SinkCard:
			if cardReady {
				inst.renderCard()
			}
		case inst.sink == vizeval.SinkTopology:
			inst.renderTopology()
		case inst.sink == vizeval.SinkJSON:
			if inst.jsonOK {
				c.CodeView(inst.ids.PrepareStr("exp-json"), inst.jsonView).Wrap().Send()
			}
		case inst.sink == vizeval.SinkChart:
			inst.renderChart(cand)
		case inst.isTextSink():
			inst.renderText()
		}
	}
}

// Id-scope seeds for the card emitter, one per source. Both must be non-zero
// and distinct: Table2CardEmitter derives every cell id from a per-section
// counter via PrepareSeq, and PrepareSeq maps its argument through
// makeHighEntropy alone — the *WidgetIdStack instance contributes nothing. Two
// stacks built by the app's mk() therefore share a base salt and, with nothing
// pushed, produce byte-identical ids. Detail renders the same emitter in the
// same frame, so without a pushed scope every cell id here is a duplicate of
// Detail's and egui reports the clash (table2_emitter.go §renderSectionHeaderRow).
const (
	experimentsCardScopeFixture uint64 = 0xE7C1
	experimentsCardScopeResult  uint64 = 0xE7C2

	// experimentsCardSaltMix is XORed into the base salt of the stack the
	// emitter derives from, so the pane's ids are disjoint from the app's card
	// stack rather than merely scoped apart. See the construction site in
	// play_renderer.go.
	experimentsCardSaltMix uint64 = 0x5EED_E7C0_0000_0001
)

// cardEmitter returns the pane's card emitter, rebuilt when the palette
// changed: the emitter takes its palette at construction.
func (inst *experimentsDriver) cardEmitter(palette string) *leewaywidgets.Table2CardEmitter {
	if inst.card == nil || inst.cardPalette != palette {
		inst.card = leewaywidgets.NewTable2CardEmitter(inst.cardIds, experimentsPalettes[palette], nil)
		// Without this the emitter flushes at EndBatch — inside the drive —
		// and renderCard draws the same widgets a second time. CardDriver sets
		// it for the same reason.
		inst.card.DeferRender = true
		inst.cardPalette = palette
	}
	return inst.card
}

// prepareCard drives the card emitter for this frame, setting inst.notice when
// there is nothing to draw or when the row cap cut the batch. Unlike the other
// sinks this happens every frame: the emitter re-bases its widget-id counter at
// each drive, so a cached drive would leave the deferred render pointing at
// stale ids.
func (inst *experimentsDriver) prepareCard(rec arrow.RecordBatch, schema *arrow.Schema, cand vizeval.Candidate) (ok bool) {
	palette, _ := cand.Options[vizeval.OptionPalette].(string)
	em := inst.cardEmitter(palette)
	inst.notice = ""
	if inst.source == experimentsSourceFixture {
		leewaywidgets.RunFixture(em)
		return true
	}
	if rec == nil || schema == nil || rec.NumRows() == 0 {
		inst.notice = "No result yet — run a query, or switch the source to the fixture."
		return false
	}
	if !inst.cards.EnsureFor(schema) {
		inst.notice = "The current result is not leeway-shaped. Switch the source to the fixture."
		return false
	}
	drv := inst.cards.Driver()
	if drv == nil {
		inst.notice = "No leeway driver for the current result."
		return false
	}
	n, notice := inst.capRows(rec.NumRows())
	slice := rec.NewSlice(0, n)
	defer slice.Release()
	if err := drv.DriveRecordBatch(em, slice); err != nil {
		inst.notice = "Driving the result failed: " + err.Error()
		return false
	}
	inst.notice = notice
	return true
}

// renderCard draws what prepareCard drove, inside an IdScope on the SAME stack
// the emitter derives from — scoping a different stack would not move these
// ids, since the scope is pushed onto the instance it is taken from.
func (inst *experimentsDriver) renderCard() {
	scope := experimentsCardScopeResult
	if inst.source == experimentsSourceFixture {
		scope = experimentsCardScopeFixture
	}
	for range c.IdScope(inst.cardIds.PrepareSeq(scope)) {
		inst.card.Render()
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

// renderChart draws the chart sink's model under the candidate's options, in
// the box the pane probe last reported.
func (inst *experimentsDriver) renderChart(cand vizeval.Candidate) {
	if inst.chartModel == nil {
		return
	}
	if inst.chartView == nil {
		inst.chartView = leewaywidgets.NewChartView(inst.ids)
	}
	if availW, availH, ok := c.CapturePaneSize(inst.ids.PrepareHighEntropy(experimentsChartPaneProbeSalt).Derive()); ok &&
		availW > 0 && availH > 0 &&
		!math.IsNaN(float64(availW)) && !math.IsNaN(float64(availH)) {
		inst.paneW, inst.paneH = availW, availH
	}
	w, h := experimentsChartPaneFill.box(inst.paneW, inst.paneH)
	inst.chartView.Render(inst.chartModel, chartOptionsOf(cand), inst.chartModel.ValueName, w, h)
}

// experimentsColormaps maps the catalogue's colormap names onto palettes.
var experimentsColormaps = map[string][]uint32{
	"viridis": colormap.Viridis8, "inferno": colormap.Inferno8, "magma": colormap.Magma8,
	"plasma": colormap.Plasma8, "cividis": colormap.Cividis8, "turbo": colormap.Turbo8,
}

// experimentsChartMarks maps the catalogue's mark names onto the view's.
var experimentsChartMarks = map[string]leewaywidgets.ChartMarkE{
	"bar": leewaywidgets.ChartMarkBar, "line": leewaywidgets.ChartMarkLine,
	"scatter": leewaywidgets.ChartMarkScatter, "heatmap": leewaywidgets.ChartMarkHeatmap,
}

// experimentsChartSorts maps the catalogue's sort names onto the view's.
var experimentsChartSorts = map[string]leewaywidgets.ChartSortE{
	"none": leewaywidgets.ChartSortNone, "ascending": leewaywidgets.ChartSortAscending,
	"descending": leewaywidgets.ChartSortDescending,
}

func chartOptionsOf(cand vizeval.Candidate) (o leewaywidgets.ChartOptions) {
	mark, _ := cand.Options[vizeval.OptionMark].(string)
	seriesBy, _ := cand.Options[vizeval.OptionSeriesBy].(string)
	sort, _ := cand.Options[vizeval.OptionSort].(string)
	cm, _ := cand.Options[vizeval.OptionColormap].(string)
	legend, _ := cand.Options[vizeval.OptionLegend].(bool)
	return leewaywidgets.ChartOptions{
		Mark: experimentsChartMarks[mark], Transpose: seriesBy == "entity", Legend: legend,
		Sort: experimentsChartSorts[sort], Colormap: experimentsColormaps[cm],
	}
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
