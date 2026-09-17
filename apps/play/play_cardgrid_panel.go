package play

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/cardgrid"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imagedecode"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/pager"
	"github.com/stergiotis/boxer/public/thestack/utfsafe"
)

// play_cardgrid_panel.go is the ADR-0245 Cards dock tab: a result set as a
// paged grid of uniform cards. The panel observes the active result on
// chMain, like Kanban and Chat.
//
// The contract is named columns rather than detection (§SD1): `card_*` names
// the slots, matched on the gloss label so `card_body@text/markdown` still
// claims `card_body`, and the prefix is a reserved namespace — an unknown
// `card_*` column is a reject, which is how a typo is told from a fact. Every
// column the contract does not claim becomes a fact on the card through its
// gloss. A slot's or a fact's gloss may be a row value (§SD2,
// play_gloss_rowvalue.go), so one result can mix kinds.
//
// "Card" already names the leeway Detail card in this package (CardDriver,
// Table2CardEmitter); this pane's code says cardgrid throughout so the two do
// not differ by one letter.

const (
	cardgridPrefix      = "card_"
	cardgridHeroCol     = "card_hero"
	cardgridOverlineCol = "card_overline"
	cardgridTitleCol    = "card_title"
	cardgridSubtitleCol = "card_subtitle"
	cardgridBodyCol     = "card_body"
	cardgridTagsCol     = "card_tags"
	cardgridToneCol     = "card_tone"
	cardgridFooterCol   = "card_footer"

	// cardgridMaxFacts bounds the facts folded per card; the widget shows
	// fewer still, and both remainders are counted on the card.
	cardgridMaxFacts = 16
	// cardgridMaxTags bounds the tags folded per card.
	cardgridMaxTags = 24
	// cardgridBodyLines bounds the body text folded per card — past what
	// the largest density shows.
	cardgridBodyLines = 8
	// cardgridThumbMaxSide bounds a retained hero: a page costs thumbnails,
	// not originals (§SD4). About the largest hero box at 1.5× — a
	// compromise between a sharp hero on a dense display and what 96 of
	// them weigh.
	cardgridThumbMaxSide = 512
	// cardgridFrameBudget is the artifact-building time one frame may spend
	// before the remaining cards draw a skeleton (§SD5). At least one
	// artifact is always built, so a page always makes progress.
	cardgridFrameBudget = 6 * time.Millisecond
	// cardgridDefaultPageSize is the pager's initial page size.
	cardgridDefaultPageSize = 24
	// cardgridPagerSalt tells this pane's pager ids from the Table pager's:
	// both derive the same ids from their stack ("CardPagr").
	cardgridPagerSalt uint64 = 0x43617264_50616772
)

// cardgridPageSizes are the pager's buckets (§SD5).
var cardgridPageSizes = []int64{12, 24, 48, 96}

// cardgridSlotNames lists the slots for a reject message.
var cardgridSlotNames = []string{cardgridHeroCol, cardgridOverlineCol, cardgridTitleCol, cardgridSubtitleCol,
	cardgridBodyCol, cardgridTagsCol, cardgridToneCol, cardgridFooterCol}

// cardgridGlossedSlots are the slots whose value renders through a gloss and
// may therefore carry a `<slot>_gloss` companion. Tags and tone do not: they
// are vocabularies of their own.
var cardgridGlossedSlots = map[string]struct{}{
	cardgridHeroCol: {}, cardgridOverlineCol: {}, cardgridTitleCol: {},
	cardgridSubtitleCol: {}, cardgridBodyCol: {}, cardgridFooterCol: {},
}

// cardgridClaim is the panel's main-channel claim: the slot columns (-1 when
// absent), each column's companion, the unclaimed columns in schema order,
// and the selected row from the signal.
type cardgridClaim struct {
	hero, overline, title, subtitle, body, tags, tone, footer int
	// companionOf maps a value column to its `<label>_gloss` column.
	companionOf map[int]int
	factCols    []int
	selRow      int64
}

// slots is the widget's slot set for this claim. Facts are declared whenever
// the result has unclaimed columns, whether or not a given row fills them.
func (inst cardgridClaim) slots() (s cardgrid.SlotsE) {
	for _, p := range []struct {
		col  int
		slot cardgrid.SlotsE
	}{
		{inst.hero, cardgrid.SlotsHero}, {inst.overline, cardgrid.SlotsOverline}, {inst.title, cardgrid.SlotsTitle},
		{inst.subtitle, cardgrid.SlotsSubtitle}, {inst.body, cardgrid.SlotsBody}, {inst.tags, cardgrid.SlotsTags},
		{inst.footer, cardgrid.SlotsFooter},
	} {
		if p.col >= 0 {
			s |= p.slot
		}
	}
	if len(inst.factCols) > 0 {
		s |= cardgrid.SlotsFacts
	}
	return
}

// resolveCardgridColumns applies the §SD1 contract to a schema. Pure and
// schema-only: every reject carries the reason the pane paints in place.
//
// The text slots carry no type requirement, for ADR-0122 §SD1's reason: they
// are read through the gloss cell accessor and formatCell, which are total.
func resolveCardgridColumns(schema *arrow.Schema) (k cardgridClaim, reason string) {
	k = cardgridClaim{hero: -1, overline: -1, title: -1, subtitle: -1, body: -1, tags: -1, tone: -1, footer: -1, selRow: -1}
	if schema == nil {
		return k, cardgridContractHint
	}
	k.companionOf = rowGlossCompanions(schema)
	companions := make(map[int]struct{}, len(k.companionOf))
	for _, ci := range k.companionOf {
		companions[ci] = struct{}{}
	}
	claim := func(slot *int, ci int, name string) string {
		if *slot >= 0 {
			return fmt.Sprintf("two columns claim `%s` (`%s` and `%s`); a card has one of each slot.",
				pathColumnLabel(name), schema.Field(*slot).Name, name)
		}
		*slot = ci
		return ""
	}
	for ci, f := range schema.Fields() {
		if _, isCompanion := companions[ci]; isCompanion {
			if stem := strings.TrimSuffix(f.Name, rowGlossSuffix); strings.HasPrefix(stem, cardgridPrefix) {
				if _, glossed := cardgridGlossedSlots[stem]; !glossed {
					return k, fmt.Sprintf("`%s` names a gloss for `%s`, which does not render through one; the glossed slots are %s.",
						f.Name, stem, cardgridGlossedSlotNames())
				}
			}
			continue
		}
		label := pathColumnLabel(f.Name)
		var why string
		switch label {
		case cardgridHeroCol:
			why = claim(&k.hero, ci, f.Name)
		case cardgridOverlineCol:
			why = claim(&k.overline, ci, f.Name)
		case cardgridTitleCol:
			why = claim(&k.title, ci, f.Name)
		case cardgridSubtitleCol:
			why = claim(&k.subtitle, ci, f.Name)
		case cardgridBodyCol:
			why = claim(&k.body, ci, f.Name)
		case cardgridTagsCol:
			why = claim(&k.tags, ci, f.Name)
		case cardgridToneCol:
			why = claim(&k.tone, ci, f.Name)
		case cardgridFooterCol:
			why = claim(&k.footer, ci, f.Name)
		default:
			if strings.HasPrefix(label, cardgridPrefix) {
				// The reserved namespace is what lets a typo be told from a
				// fact: `card_titel` must not quietly become one.
				hint := ""
				if strings.HasSuffix(label, rowGlossSuffix) {
					hint = " A `_gloss` column must be text and sit beside the slot it describes."
				}
				return k, fmt.Sprintf("`%s` is not a card slot. The slots are %s, each with an optional `<slot>_gloss`.%s",
					f.Name, "`"+strings.Join(cardgridSlotNames, "`, `")+"`", hint)
			}
			k.factCols = append(k.factCols, ci)
		}
		if why != "" {
			return k, why
		}
	}
	if k.title < 0 && k.hero < 0 {
		return k, cardgridContractHint
	}
	return k, ""
}

func cardgridGlossedSlotNames() string {
	names := make([]string, 0, len(cardgridGlossedSlots))
	for n := range cardgridGlossedSlots {
		names = append(names, "`"+n+"`")
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// cardgridContractHint is the reject shown when neither required column is
// present — the pane's teaching moment, so it names a query that satisfies
// the contract.
const cardgridContractHint = "The cards need a `card_title` or a `card_hero` column. Name the slots in the query — e.g. " +
	"SELECT name AS card_title, content AS card_hero, mime AS card_hero_gloss, kind AS card_overline FROM t — " +
	"and optionally `card_subtitle`, `card_body`, `card_tags`, `card_tone` and `card_footer`. " +
	"A `<slot>_gloss` column names that row's media type; every other column becomes a fact on the card."

// cardgridThumb is one retained hero image: the thumbnail and the source's
// own size, which is what the card lays it out by.
type cardgridThumb struct {
	pixels     []uint32
	w, h       uint32
	srcW, srcH uint32
	reason     string
}

// CardGridDriver owns the Cards tab state: the pager, the folded page, the
// widget state, the row-value bindings and the page's artifacts.
type CardGridDriver struct {
	ids   *c.WidgetIdStack
	state cardgrid.State
	pager *pager.Pager

	binder *rowGlossBinder

	// The page's artifacts (§SD5): the page is the working set, so both maps
	// are dropped on a page turn and need no LRU. cache holds the block
	// faces glossBlock serves (a parsed markdown Doc, a highlighted job);
	// thumbs holds the images, reduced, and waves the recordings, likewise. Keys carry the ARROW ROW as the
	// ordinal, so they do not move when the page does.
	cache   *richCellCache
	thumbs  map[richKey]*cardgridThumb
	waves   map[richKey]*audioArtifact
	tracker *c.ImageVersionTracker[string]
	// shipped lists the image keys this page uploaded, so a page turn can
	// forget them rather than wait for the host's idle eviction to notice.
	shipped []string
	// generation is the image widgets' contentVersion, bumped with every
	// drop, as richCellCache's is.
	generation uint64

	// The claim, cached per schema (the pointer-identity idiom): acceptance
	// runs once per registered tab per frame.
	claimFor    *arrow.Schema
	claim       cardgridClaim
	claimReason string
	claimSeen   bool

	// The fold and its cache key.
	model         *cardgrid.Model
	pageStart     int64
	forResult     ResultID
	forStart      int64
	forEnd        int64
	forRaw        bool
	forDirectives string
	forSchema     *arrow.Schema
	folded        bool
	unknownTones  int
	// pendingFacts collects, during a fold, the facts the text slots raise
	// about themselves (cellText); nil outside one.
	pendingFacts [][]cardgridFact

	// lastSel is the selection this pane last saw, so a cursor moved
	// elsewhere — and only that — turns the page.
	lastSel int64

	// The frame's artifact budget.
	spent   time.Duration
	builds  int
	pending bool
}

// NewCardGridDriver builds the driver.
func NewCardGridDriver(ids *c.WidgetIdStack, pagerIds *c.WidgetIdStack, catalog *gloss.Catalog) (inst *CardGridDriver) {
	return &CardGridDriver{
		ids:     ids,
		pager:   pager.New(pagerIds, cardgridDefaultPageSize).WithPageSizeOptions(cardgridPageSizes).WithUnit("cards"),
		binder:  newRowGlossBinder(catalog),
		cache:   newRichCellCache(ids),
		thumbs:  make(map[richKey]*cardgridThumb, cardgridDefaultPageSize),
		waves:   make(map[richKey]*audioArtifact, cardgridDefaultPageSize),
		tracker: c.NewImageVersionTracker[string](),
		lastSel: -1,
	}
}

// claimOf resolves (or returns the cached resolution of) the contract.
func (inst *CardGridDriver) claimOf(schema *arrow.Schema) (cardgridClaim, string) {
	if inst.claimSeen && inst.claimFor == schema {
		return inst.claim, inst.claimReason
	}
	inst.claim, inst.claimReason = resolveCardgridColumns(schema)
	inst.claimFor, inst.claimSeen = schema, true
	return inst.claim, inst.claimReason
}

// cardgridPanel is the PanelI face. It carries the app rather than the driver
// alone because the faces come from the app's gloss resolution.
type cardgridPanel struct {
	app *PlayApp
}

func (inst cardgridPanel) ID() PanelID { return "cards" }

func (inst cardgridPanel) Channels() []ChannelSpec {
	return []ChannelSpec{{ID: chMain, Required: true, Label: "cards"}}
}

func (inst cardgridPanel) AcceptForChannel(_ ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	if schema == nil {
		return nil, "Run a query to see the cards."
	}
	k, reason := inst.app.cardgridDriver.claimOf(schema)
	if reason != "" {
		return nil, reason
	}
	k.selRow, _ = readSelection(sig)
	return k, ""
}

func (inst cardgridPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	main := filled[chMain]
	k, ok := main.Claim.(cardgridClaim)
	if !ok || main.Rec == nil {
		return
	}
	inst.app.cardgridDriver.render(inst.app, main.Rec, main.Result, k, emit)
}

// dropArtifacts forgets the page's artifacts and the textures it shipped.
func (inst *CardGridDriver) dropArtifacts() {
	clear(inst.cache.entries)
	inst.cache.generation++
	clear(inst.thumbs)
	clear(inst.waves)
	for _, key := range inst.shipped {
		inst.tracker.Forget(key)
	}
	inst.shipped = inst.shipped[:0]
	inst.generation++
}

// render pages, folds the page (cached), draws the toolbar and the grid, and
// carries the selection both ways.
func (inst *CardGridDriver) render(app *PlayApp, rec arrow.RecordBatch, result ResultID, k cardgridClaim, emit SignalEmitterI) {
	schema := rec.Schema()
	if !inst.folded || inst.forResult != result {
		inst.binder.reset()
		inst.pager.Reset()
		inst.lastSel = -1
	}
	inst.pager.Configure(rec.NumRows())

	// The page follows a selection that moved elsewhere (§SD5) — and only
	// one that moved: following the standing selection every frame would
	// pin the pager to its page.
	selMoved := k.selRow != inst.lastSel
	inst.lastSel = k.selRow
	if selMoved && k.selRow >= 0 && k.selRow < rec.NumRows() {
		inst.pager.GoToIndex(k.selRow)
	}

	slots := k.slots()
	for range c.HorizontalTop().KeepIter() {
		cardgrid.Toolbar(inst.ids, "play-cards", &inst.state, slots)
		c.AddSpace(styletokens.GapSections(styletokens.ActiveDensity()))
		inst.pager.Render()
		if inst.unknownTones > 0 {
			for range c.HoverText("known tones: " + kanbanTokenNames()).KeepIter() {
				for rt := range c.RichTextLabel(strconv.Itoa(inst.unknownTones) + " unknown `card_tone` on this page") {
					rt.Small().Weak()
				}
			}
		}
	}
	start, end := inst.pager.Range()

	cols := app.glossColumns(schema)
	rawCells := app.tableOpts.rawCells
	inst.refold(app, rec, schema, cols, result, k, start, end, rawCells)
	m := inst.model
	if m == nil || m.Count == 0 {
		for rt := range c.RichTextLabel("The query returned no rows, so there are no cards.") {
			rt.Small().Weak()
		}
		return
	}

	// Follow the shared selection before drawing, as the board does: a card
	// paints its selection, so it must not keep painting its own last click
	// after the rest of the dock moved on.
	if k.selRow >= start && k.selRow < end {
		inst.state.SetSelected(int32(k.selRow - start))
		if selMoved {
			inst.state.Reveal(int32(k.selRow - start))
		}
	} else {
		inst.state.SetSelected(-1)
	}

	inst.spent, inst.builds, inst.pending = 0, 0, false
	in := cardgrid.Input{
		Ids: inst.ids, ScopeKey: "play-cards", Model: m, State: &inst.state, FillHost: true,
	}
	if k.hero >= 0 {
		in.Hero = func(ord int, box cardgrid.Box) (cardgrid.Block, bool) {
			return inst.slotBlock(app, rec, schema, cols, k, k.hero, start+int64(ord), box, rawCells, true)
		}
	}
	if k.body >= 0 && !rawCells {
		in.Body = func(ord int, box cardgrid.Box) (cardgrid.Block, bool) {
			return inst.slotBlock(app, rec, schema, cols, k, k.body, start+int64(ord), box, rawCells, false)
		}
	}
	res := cardgrid.Render(in)
	if inst.pending {
		// Skeletons are on screen: keep the frames coming until the page is
		// complete, then let the host go reactive again.
		c.RequestRepaint()
	}

	if res.Toggled >= 0 && int(res.Toggled) < m.Count {
		inst.toggleAudio(app, rec, k, start+int64(res.Toggled))
	}

	// Publish a click or a keyboard move. Comparing against the claim's row
	// means a selection that merely echoes the signal back does not re-emit.
	picked := res.Clicked
	if picked < 0 {
		picked = res.Moved
	}
	if picked >= 0 && int(picked) < m.Count {
		if row := start + int64(picked); row != k.selRow {
			inst.lastSel = row // our own move is not a cursor moved elsewhere
			emit.Emit(signalSelection, row)
		}
	}
}

// refold rebuilds the page's model when anything it was folded from changed.
func (inst *CardGridDriver) refold(app *PlayApp, rec arrow.RecordBatch, schema *arrow.Schema, cols []glossColumn, result ResultID, k cardgridClaim, start, end int64, rawCells bool) {
	directives := app.glossRes.directives
	if inst.folded && inst.forResult == result && inst.forStart == start && inst.forEnd == end &&
		inst.forRaw == rawCells && inst.forDirectives == directives && inst.forSchema == schema {
		return
	}
	inst.dropArtifacts()
	inst.folded = true
	inst.forResult, inst.forStart, inst.forEnd = result, start, end
	inst.forRaw, inst.forDirectives, inst.forSchema = rawCells, directives, schema
	inst.pageStart = start
	inst.unknownTones = 0

	n := int(max(end-start, 0))
	m := &cardgrid.Model{Count: n, Slots: k.slots()}
	text := func(col int, maxRunes int) []string {
		if col < 0 {
			return nil
		}
		out := make([]string, n)
		for i := range n {
			out[i] = cardgrid.OneLine(inst.cellText(app, rec, cols, k, col, start+int64(i), rawCells, m, i), maxRunes)
		}
		return out
	}
	if k.tone >= 0 {
		m.Tone = make([]color.Color, n)
	}
	if m.Slots.Has(cardgrid.SlotsFacts) {
		m.FactOff = make([]int32, 1, n+1)
		m.FactMore = make([]int32, n)
	}
	if k.tags >= 0 {
		m.TagOff = make([]int32, 1, n+1)
	}
	// Facts first: a slot that cannot honour its row value reports the
	// reason as a fact of its own (cellText), and those go ahead of the
	// result's columns so they are not the ones "+k more" hides.
	pendingFacts := make([][]cardgridFact, n)
	inst.pendingFacts = pendingFacts
	m.Overline = text(k.overline, cardgrid.MaxLineRunes)
	m.Title = text(k.title, cardgrid.MaxTitleRunes)
	m.Subtitle = text(k.subtitle, cardgrid.MaxLineRunes)
	m.Footer = text(k.footer, cardgrid.MaxLineRunes)
	if k.body >= 0 {
		m.Body = make([]string, n)
		for i := range n {
			row := start + int64(i)
			if gc := inst.resolve(rec, cols, k, k.body, row); !rawCells && gc != nil && gc.reason == "" && gc.rowOK && cardgridHasBlock(gc) {
				continue // drawn by the body block; the text stays empty
			}
			m.Body[i] = cardgrid.Lines(inst.cellText(app, rec, cols, k, k.body, row, rawCells, m, i), cardgrid.MaxBodyRunes, cardgridBodyLines)
		}
	}
	inst.pendingFacts = nil

	for i := range n {
		row := start + int64(i)
		if k.tone >= 0 && !rec.Column(k.tone).IsNull(int(row)) {
			token := strings.ToLower(strings.TrimSpace(formatCell(rec, k.tone, row)))
			if t, known := kanbanDotTokens[token]; known {
				m.Tone[i] = kanbanTokenColor(t)
			} else if token != "" {
				inst.unknownTones++
			}
		}
		if m.Slots.Has(cardgrid.SlotsFacts) {
			facts := pendingFacts[i]
			for _, ci := range k.factCols {
				if rec.Column(ci).IsNull(int(row)) {
					continue
				}
				if f, ok := inst.fact(app, rec, schema, cols, k, ci, row, rawCells); ok {
					facts = append(facts, f)
				}
			}
			if len(facts) > cardgridMaxFacts {
				m.FactMore[i] = int32(len(facts) - cardgridMaxFacts)
				facts = facts[:cardgridMaxFacts]
			}
			for _, f := range facts {
				m.FactLabel = append(m.FactLabel, f.label)
				m.FactValue = append(m.FactValue, f.value)
				m.FactColor = append(m.FactColor, f.col)
			}
			m.FactOff = append(m.FactOff, int32(len(m.FactLabel)))
		}
		if k.tags >= 0 {
			for _, tag := range cardgridTags(rec, k.tags, row) {
				m.Tag = append(m.Tag, tag)
			}
			m.TagOff = append(m.TagOff, int32(len(m.Tag)))
		}
	}
	inst.model = m
}

// cardgridFact is one folded fact.
type cardgridFact struct {
	label, value string
	col          color.Color
}

// resolve is one cell's gloss resolution: the row value where the column has
// a companion carrying one, else the column's own. Loud inside the card_
// namespace, slash-gated outside it (§SD2).
func (inst *CardGridDriver) resolve(rec arrow.RecordBatch, cols []glossColumn, k cardgridClaim, col int, row int64) *glossColumn {
	var base *glossColumn
	if col < len(cols) {
		base = &cols[col]
	}
	if comp, ok := k.companionOf[col]; ok {
		loud := strings.HasPrefix(pathColumnLabel(rec.Schema().Field(col).Name), cardgridPrefix)
		return inst.binder.resolve(rec, col, comp, row, base, loud)
	}
	return base
}

// cellText is a text slot's string: the gloss's inline face where the cell
// resolves to one, the plain rendering otherwise. A resolution that cannot be
// honoured — a misspelt row value, a refused kind — keeps the plain text and
// says why as a fact on the same card, so it is loud without taking the
// slot's one line. The result is always a copy: a cell's text may alias Arrow
// memory, and the model outlives the frame.
func (inst *CardGridDriver) cellText(app *PlayApp, rec arrow.RecordBatch, cols []glossColumn, k cardgridClaim, col int, row int64, rawCells bool, m *cardgrid.Model, i int) string {
	if rec.Column(col).IsNull(int(row)) {
		return ""
	}
	gc := inst.resolve(rec, cols, k, col, row)
	if rawCells || gc == nil || gc.mediaType == "" {
		return strings.Clone(formatCell(rec, col, row))
	}
	why := gc.reason
	if why == "" && !gc.rowOK {
		why = gc.rowReason
	}
	if why != "" {
		if inst.pendingFacts != nil && m.Slots.Has(cardgrid.SlotsFacts) {
			inst.pendingFacts[i] = append(inst.pendingFacts[i], cardgridFact{
				label: pathColumnLabel(rec.Schema().Field(col).Name),
				value: cardgrid.OneLine(why, cardgrid.MaxFactRunes),
				col:   color.Hex(styletokens.WarningDefault.AsHex()),
			})
		}
		return strings.Clone(formatCell(rec, col, row))
	}
	face := gc.inst.Inline(gloss.ArrowCell{Arr: rec.Column(col), Row: int(row)})
	return strings.Clone(face.Text)
}

// fact folds one unclaimed column of one row: the inline face only (§SD1),
// its tone as the value's colour.
func (inst *CardGridDriver) fact(app *PlayApp, rec arrow.RecordBatch, schema *arrow.Schema, cols []glossColumn, k cardgridClaim, ci int, row int64, rawCells bool) (f cardgridFact, ok bool) {
	gc := inst.resolve(rec, cols, k, ci, row)
	f.label = shortColumnLabel(schema.Field(ci).Name)
	if gc != nil && gc.label != "" {
		f.label = gc.label
	}
	f.label = cardgrid.OneLine(strings.Clone(f.label), cardgrid.MaxLineRunes)
	switch {
	case rawCells || gc == nil || gc.mediaType == "":
		f.value = formatCell(rec, ci, row)
	case gc.reason != "":
		f.value, f.col = gc.reason, color.Hex(styletokens.WarningDefault.AsHex())
	case !gc.rowOK:
		f.value, f.col = gc.rowReason, color.Hex(styletokens.WarningDefault.AsHex())
	default:
		face := gc.inst.Inline(gloss.ArrowCell{Arr: rec.Column(ci), Row: int(row)})
		f.value = face.Text
		if col, toned := toneColor(face.Tone); toned {
			f.col = col
		}
	}
	f.value = cardgrid.OneLine(strings.Clone(f.value), cardgrid.MaxFactRunes)
	return f, f.value != ""
}

// cardgridTags reads one row's tags: the items of a list column, or a
// scalar's text as one tag.
func cardgridTags(rec arrow.RecordBatch, col int, row int64) (tags []string) {
	arr := rec.Column(col)
	if arr.IsNull(int(row)) {
		return nil
	}
	if list, isList := arr.(array.ListLike); isList {
		lo, hi := list.ValueOffsets(int(row))
		values := list.ListValues()
		for j := lo; j < hi && len(tags) < cardgridMaxTags; j++ {
			if values.IsNull(int(j)) {
				continue
			}
			if tag := cardgrid.OneLine(strings.Clone(gloss.FormatArrowElem(values, j)), cardgrid.MaxTagRunes); tag != "" {
				tags = append(tags, tag)
			}
		}
		return
	}
	if tag := cardgrid.OneLine(strings.Clone(formatCell(rec, col, row)), cardgrid.MaxTagRunes); tag != "" {
		tags = append(tags, tag)
	}
	return
}

// cardgridHasBlock reports whether a resolution draws as a block on a card:
// a media type this pane binds a face to.
func cardgridHasBlock(gc *glossColumn) bool {
	return hasBlockFace(gc.mediaType) || gc.mediaType == gloss.MediaTypeURL || gc.mediaType == gloss.MediaTypeTaggedId
}

// budgetLeft reports whether this frame may build another artifact. The first
// build of a frame is always allowed, so a page whose every artifact is over
// the budget still completes, one per frame.
func (inst *CardGridDriver) budgetLeft() bool {
	return inst.builds == 0 || inst.spent < cardgridFrameBudget
}

// slotBlock is the host-drawn hero or body of one card (§SD4, §SD5): an image
// as a contained thumbnail, any other block face through glossBlock, and what
// stands in for one — declined for a NULL cell (the widget draws its
// placeholder) and for a body with no block face (the widget draws the text).
func (inst *CardGridDriver) slotBlock(app *PlayApp, rec arrow.RecordBatch, schema *arrow.Schema, cols []glossColumn, k cardgridClaim, col int, row int64, box cardgrid.Box, rawCells bool, hero bool) (cardgrid.Block, bool) {
	if row < 0 || row >= rec.NumRows() || rec.Column(col).IsNull(int(row)) {
		return cardgrid.Block{}, false
	}
	field := schema.Field(col)
	gc := inst.resolve(rec, cols, k, col, row)
	if rawCells || gc == nil || gc.mediaType == "" {
		if !hero {
			return cardgrid.Block{}, false
		}
		return inst.plainHero(rec, field, col, row, rawCells)
	}
	if gc.reason != "" {
		return cardgrid.Block{Reason: gc.reason}, true
	}
	if !gc.rowOK {
		return cardgrid.Block{Reason: gc.rowReason}, true
	}
	key := richKey{col: col, ord: int(row)}
	switch {
	case isImageType(gc.mediaType):
		return inst.imageBlock(rec, key, box), true
	case gloss.IsWAVMediaType(gc.mediaType):
		return inst.audioBlock(app, rec, key, box), true
	case cardgridHasBlock(gc):
		raw, ok := cellRaw(rec, col, row)
		if !ok || raw == "" {
			return cardgrid.Block{}, false
		}
		if _, cached := inst.cache.entries[key]; !cached {
			if !inst.budgetLeft() {
				inst.pending = true
				return cardgrid.Block{Pending: true}, true
			}
			// The raw cell aliases Arrow memory; the cache retains what it
			// derives from it, and a plain-text face retains the text.
			raw = strings.Clone(raw)
		}
		t0 := time.Now()
		block := app.glossBlock(inst.cache, "cards", gc, key, raw, gloss.KindOfArrow(listElemType(field.Type)))
		inst.spent += time.Since(t0)
		inst.builds++
		if block.Render == nil {
			return cardgrid.Block{}, false
		}
		return cardgrid.Block{Render: func() bool { block.Render(); return false }}, true
	case hero:
		// A gloss with an inline face only — a duration, a masked secret —
		// as a hero: the face, in the box.
		face := gc.inst.Inline(gloss.ArrowCell{Arr: rec.Column(col), Row: int(row)})
		text := cardgrid.Lines(strings.Clone(face.Text), cardgrid.MaxBodyRunes, cardgridBodyLines)
		return cardgridTextBlock(text), true
	}
	return cardgrid.Block{}, false
}

// plainHero is a hero with no gloss. Text is shown as text; bytes are not
// guessed at — the pane says how to declare them instead, because a hex dump
// in a hero box helps nobody and formatCell would build one every frame.
func (inst *CardGridDriver) plainHero(rec arrow.RecordBatch, field arrow.Field, col int, row int64, rawCells bool) (cardgrid.Block, bool) {
	raw, isRaw := cellRaw(rec, col, row)
	switch field.Type.ID() {
	case arrow.BINARY, arrow.LARGE_BINARY, arrow.FIXED_SIZE_BINARY, arrow.BINARY_VIEW:
		size := humanize.IBytes(uint64(len(raw)))
		if rawCells {
			return cardgrid.Block{Reason: size + " — raw cells is on"}, true
		}
		return cardgrid.Block{Reason: size + " with no media type. Name it per row in `" + cardgridHeroCol + rowGlossSuffix +
			"`, or for the whole column as `" + cardgridHeroCol + "@image/png`."}, true
	}
	if !isRaw {
		raw = formatCell(rec, col, row)
	}
	return cardgridTextBlock(cardgrid.Lines(strings.Clone(utfsafe.EnsureUTF8(raw)), cardgrid.MaxBodyRunes, cardgridBodyLines)), true
}

// cardgridTextBlock draws bounded text in a block's box.
func cardgridTextBlock(text string) cardgrid.Block {
	return cardgrid.Block{Render: func() bool {
		c.Label(text).Wrap().Selectable(false).Send()
		return false
	}}
}

// imageBlock is an image cell as a thumbnail contained in the box: decoded
// under the header-first pixel budget, reduced, and only the reduction kept
// (§SD4). Laid out by the SOURCE's size, so an icon stays an icon rather than
// filling the box at the thumbnail's.
func (inst *CardGridDriver) imageBlock(rec arrow.RecordBatch, key richKey, box cardgrid.Box) cardgrid.Block {
	th, ok := inst.thumbs[key]
	if !ok {
		if !inst.budgetLeft() {
			inst.pending = true
			return cardgrid.Block{Pending: true}
		}
		t0 := time.Now()
		th = &cardgridThumb{}
		raw, _ := cellRaw(rec, key.col, int64(key.ord))
		t, err := imagedecode.DecodeThumbnailRGBA8([]byte(raw), richMaxImagePixels, cardgridThumbMaxSide)
		if err != nil {
			th.reason = err.Error()
		} else {
			th.pixels, th.w, th.h, th.srcW, th.srcH = t.Pixels, t.WidthPx, t.HeightPx, t.SrcWidthPx, t.SrcHeightPx
		}
		inst.thumbs[key] = th
		inst.spent += time.Since(t0)
		inst.builds++
	}
	if th.reason != "" {
		return cardgrid.Block{Reason: th.reason}
	}
	w, h := cardgrid.Fit(float32(th.srcW), float32(th.srcH), box)
	imgKey := "play-cards-img-" + key.String()
	return cardgrid.Block{W: w, H: h, Render: func() bool {
		// Two PrepareStr creators: each is a single-use state machine.
		// PixelsToSendFor, not PixelsToSend: the pane is a dock tab, where a
		// send is not a receipt (the Detail image face's note).
		imgId := inst.ids.PrepareStr(imgKey).Derive()
		pixels := inst.tracker.PixelsToSendFor(imgKey, imgId, inst.generation, th.pixels)
		if len(pixels) > 0 {
			inst.shipped = append(inst.shipped, imgKey)
		}
		// The image senses clicks, so it reports them for the card.
		return c.Image(inst.ids.PrepareStr(imgKey), th.w, th.h, inst.generation,
			uint8(c.FitFixedE), uint32(w), uint32(h),
			uint8(c.FilterLinearE), c.TintNoneRgba, pixels).
			SendResp().HasPrimaryClicked()
	}}
}

// cardgridAudioKey names a card's recording for the now-playing session.
func cardgridAudioKey(key richKey) string {
	return audioKey("cards", key.col, int64(key.ord), 0)
}

// audioBlock is a recording cell as its waveform with play and pause (§SD6):
// read from the cell's bytes where they are, reduced to a few hundred
// columns, and only the reduction kept.
func (inst *CardGridDriver) audioBlock(app *PlayApp, rec arrow.RecordBatch, key richKey, box cardgrid.Box) cardgrid.Block {
	a, ok := inst.waves[key]
	if !ok {
		if !inst.budgetLeft() {
			inst.pending = true
			return cardgrid.Block{Pending: true}
		}
		t0 := time.Now()
		raw, _ := cellRaw(rec, key.col, int64(key.ord))
		a = buildAudioArtifact(raw)
		inst.waves[key] = a
		inst.spent += time.Since(t0)
		inst.builds++
	}
	if a.reason != "" {
		return cardgrid.Block{Reason: a.reason}
	}
	return cardgrid.Block{Render: func() bool {
		return app.renderAudioFace(inst.ids, "audio", cardgridAudioKey(key), a, box.W, box.H, func() string {
			raw, _ := cellRaw(rec, key.col, int64(key.ord))
			return raw
		})
	}}
}

// toggleAudio is Space on a card: play or pause its hero when that is a
// recording this page has already read.
func (inst *CardGridDriver) toggleAudio(app *PlayApp, rec arrow.RecordBatch, k cardgridClaim, row int64) {
	if k.hero < 0 {
		return
	}
	key := richKey{col: k.hero, ord: int(row)}
	if a, ok := inst.waves[key]; ok && a.reason == "" {
		app.audioToggle(cardgridAudioKey(key), func() string {
			raw, _ := cellRaw(rec, key.col, row)
			return raw
		})
	}
}

// renderCardgridTab is the Cards dock tab body (ADR-0245): the active result
// as a grid of cards. The same guards as the Kanban and Chat tabs.
func (inst *PlayApp) renderCardgridTab(rec arrow.RecordBatch, schema *arrow.Schema, loading bool, err error, result ResultID) {
	if loading && rec == nil {
		inst.renderResultsLoading()
		return
	}
	if err != nil && rec == nil {
		inst.renderResultsFailed()
		// A buffer restored from a previous session may name the fixture,
		// which does not outlive the process that published it.
		inst.renderCardgridFixtureOffer()
		return
	}
	if rec == nil {
		for rt := range c.RichTextLabel("Run a query naming a `card_title` or a `card_hero` column to see cards.") {
			rt.Small().Weak()
		}
		inst.renderCardgridFixtureOffer()
		return
	}
	inputs := map[ChannelID]channelInput{
		chMain: {node: inst.resolvedTabNode("cards"), rec: rec, schema: schema, sig: inst.frameSig, result: result},
	}
	if reject := dispatchPanel(cardgridPanel{app: inst}, inputs, inst.sigEmit); reject != "" {
		c.LabelAtoms(c.Atoms().BeginRichText(reject).Small().Weak().End().Keep()).Wrap().Send()
		inst.renderCardgridFixtureOffer()
	}
}
