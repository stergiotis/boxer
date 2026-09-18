package play

import (
	"fmt"
	"math"
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
// gloss. A slot's or a fact's gloss may be a row value (§SD2, resolved by the
// app's rowGloss as in every other pane), so one result can mix kinds.
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
	// fewer still. Past it, non-NULL columns are counted rather than
	// rendered, and both remainders are counted on the card.
	cardgridMaxFacts = 16
	// cardgridMaxTags bounds the tags folded per card.
	cardgridMaxTags = 24
	// cardgridBodyLines bounds the body text folded per card — past what
	// the largest density shows.
	cardgridBodyLines = 8
	// cardgridThumbStep rounds a retained image's bound up from the box it
	// is drawn in (§SD4), so a pane being resized does not decode the page
	// again at every width.
	cardgridThumbStep = 128
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

// cardgridSlotSpec is one `card_*` column of the §SD1 contract.
type cardgridSlotSpec struct {
	name string
	// slot is the widget slot the column fills; zero for tone, which
	// colours the card rather than filling a slot.
	slot cardgrid.SlotsE
	// glossed slots render through a gloss and may carry a `<slot>_gloss`
	// companion. Tags and tone do not: they are vocabularies of their own.
	glossed bool
	// line slots are one line of text, so a resolution they cannot honour
	// is reported as a fact of its own rather than in the slot (§SD2).
	line bool
	col  func(*cardgridClaim) *int
}

// cardgridSlotSpecs is the contract, in the order the slots are named to
// the user.
var cardgridSlotSpecs = [...]cardgridSlotSpec{
	{cardgridHeroCol, cardgrid.SlotsHero, true, false, func(k *cardgridClaim) *int { return &k.hero }},
	{cardgridOverlineCol, cardgrid.SlotsOverline, true, true, func(k *cardgridClaim) *int { return &k.overline }},
	{cardgridTitleCol, cardgrid.SlotsTitle, true, true, func(k *cardgridClaim) *int { return &k.title }},
	{cardgridSubtitleCol, cardgrid.SlotsSubtitle, true, true, func(k *cardgridClaim) *int { return &k.subtitle }},
	{cardgridBodyCol, cardgrid.SlotsBody, true, false, func(k *cardgridClaim) *int { return &k.body }},
	{cardgridTagsCol, cardgrid.SlotsTags, false, false, func(k *cardgridClaim) *int { return &k.tags }},
	{cardgridToneCol, 0, false, false, func(k *cardgridClaim) *int { return &k.tone }},
	{cardgridFooterCol, cardgrid.SlotsFooter, true, true, func(k *cardgridClaim) *int { return &k.footer }},
}

// cardgridSlotSpecOf finds a slot by its column label.
func cardgridSlotSpecOf(label string) (spec cardgridSlotSpec, ok bool) {
	for _, s := range cardgridSlotSpecs {
		if s.name == label {
			return s, true
		}
	}
	return
}

// cardgridSlotList names the slots for a reject message, the glossed ones
// only when glossedOnly.
func cardgridSlotList(glossedOnly bool) string {
	names := make([]string, 0, len(cardgridSlotSpecs))
	for _, s := range cardgridSlotSpecs {
		if s.glossed || !glossedOnly {
			names = append(names, "`"+s.name+"`")
		}
	}
	return strings.Join(names, ", ")
}

// cardgridClaim is the panel's main-channel claim: the slot columns (-1 when
// absent), the unclaimed columns in schema order, and the selected row from
// the signal.
type cardgridClaim struct {
	hero, overline, title, subtitle, body, tags, tone, footer int
	// lineCompanion is set when a one-line slot has a `<slot>_gloss`: its
	// row values may not bind, and such a slot says so as a fact.
	lineCompanion bool
	factCols      []int
	selRow        int64
}

// slots is the widget's slot set for this claim. Facts are declared whenever
// the result has unclaimed columns, whether or not a given row fills them;
// the fold may add them for the reasons a one-line slot raises (mayRaise).
func (inst cardgridClaim) slots() (s cardgrid.SlotsE) {
	for _, spec := range cardgridSlotSpecs {
		if spec.slot != 0 && *spec.col(&inst) >= 0 {
			s |= spec.slot
		}
	}
	if len(inst.factCols) > 0 {
		s |= cardgrid.SlotsFacts
	}
	return
}

// mayRaise reports whether a one-line slot can raise a reason on some card:
// it carries a companion whose values may not bind, or its column's own
// resolution is refused. Both are schema properties, so the card height
// stays a function of the schema and the density (§SD4).
func (inst cardgridClaim) mayRaise(cols []glossColumn) bool {
	if inst.lineCompanion {
		return true
	}
	for _, spec := range cardgridSlotSpecs {
		ci := *spec.col(&inst)
		if !spec.line || ci < 0 || ci >= len(cols) {
			continue
		}
		if gc := &cols[ci]; gc.mediaType != "" && cardgridWhy(gc) != "" {
			return true
		}
	}
	return false
}

// resolveCardgridColumns applies the §SD1 contract to a schema. Pure and
// schema-only: every reject carries the reason the pane paints in place.
//
// The text slots carry no type requirement, for ADR-0122 §SD1's reason: they
// are read through the gloss cell accessor and formatDisplayCell, which are
// total.
func resolveCardgridColumns(schema *arrow.Schema) (k cardgridClaim, reason string) {
	k = cardgridClaim{selRow: -1}
	for _, spec := range cardgridSlotSpecs {
		*spec.col(&k) = -1
	}
	if schema == nil {
		return k, cardgridContractHint
	}
	companionOf := rowGlossCompanions(schema)
	companions := make(map[int]struct{}, len(companionOf))
	for _, ci := range companionOf {
		companions[ci] = struct{}{}
	}
	for ci, f := range schema.Fields() {
		if _, isCompanion := companions[ci]; isCompanion {
			if stem := strings.TrimSuffix(f.Name, rowGlossSuffix); strings.HasPrefix(stem, cardgridPrefix) {
				spec, _ := cardgridSlotSpecOf(stem)
				if !spec.glossed {
					return k, fmt.Sprintf("`%s` names a gloss for `%s`, which does not render through one; the glossed slots are %s.",
						f.Name, stem, cardgridSlotList(true))
				}
				k.lineCompanion = k.lineCompanion || spec.line
			}
			continue
		}
		label := pathColumnLabel(f.Name)
		spec, isSlot := cardgridSlotSpecOf(label)
		switch {
		case isSlot:
			slot := spec.col(&k)
			if *slot >= 0 {
				return k, fmt.Sprintf("two columns claim `%s` (`%s` and `%s`); a card has one of each slot.",
					label, schema.Field(*slot).Name, f.Name)
			}
			*slot = ci
		case strings.HasPrefix(label, cardgridPrefix):
			// The reserved namespace is what lets a typo be told from a
			// fact: `card_titel` must not quietly become one.
			hint := ""
			if strings.HasSuffix(label, rowGlossSuffix) {
				hint = " A `_gloss` column must be text and sit beside the slot it describes."
			}
			return k, fmt.Sprintf("`%s` is not a card slot. The slots are %s, each glossed one with an optional `<slot>_gloss`.%s",
				f.Name, cardgridSlotList(false), hint)
		default:
			k.factCols = append(k.factCols, ci)
		}
	}
	if k.title < 0 && k.hero < 0 {
		return k, cardgridContractHint
	}
	return k, ""
}

// cardgridContractHint is the reject shown when neither required column is
// present — the pane's teaching moment, so it names a query that satisfies
// the contract.
const cardgridContractHint = "The cards need a `card_title` or a `card_hero` column. Name the slots in the query — e.g. " +
	"SELECT name AS card_title, content AS card_hero, mime AS card_hero_gloss, kind AS card_overline FROM t — " +
	"and optionally `card_subtitle`, `card_body`, `card_tags`, `card_tone` and `card_footer`. " +
	"A `<slot>_gloss` column names that row's media type; every other column becomes a fact on the card."

// cardgridWhy is why a resolution that names a media type cannot be honoured
// for its column: the binding's own reason, else the value kind's refusal.
// Empty when it can.
func cardgridWhy(gc *glossColumn) string {
	if gc.reason != "" {
		return gc.reason
	}
	if !gc.rowOK {
		return gc.rowReason
	}
	return ""
}

// CardGridDriver owns the Cards tab state: the pager, the folded page, the
// widget state and the page's artifacts.
type CardGridDriver struct {
	ids   *c.WidgetIdStack
	state cardgrid.State
	pager *pager.Pager

	// cache holds the page's artifacts (§SD5) — block faces, images reduced
	// to their box, recordings reduced to an overview — keyed by (column,
	// ARROW ROW), so they do not move when the page does. The page is the
	// working set: the cache is dropped on a page turn and needs no LRU.
	cache *richCellCache
	// shipped lists the image keys this page uploaded, so a page turn can
	// forget them rather than wait for the host's idle eviction to notice.
	shipped []string

	// The claim, cached per schema (the pointer-identity idiom): acceptance
	// runs once per registered tab per frame.
	claimFor    *arrow.Schema
	claim       cardgridClaim
	claimReason string
	claimSeen   bool

	// The fold and its cache key.
	model         *cardgrid.Model
	forResult     ResultID
	forStart      int64
	forEnd        int64
	forRaw        bool
	forDirectives string
	forSchema     *arrow.Schema
	folded        bool
	unknownTones  int

	// lastSel is the selection this pane last saw, so a cursor moved
	// elsewhere — and only that — turns the page.
	lastSel int64

	// The frame's artifact budget.
	spent   time.Duration
	builds  int
	pending bool
}

// NewCardGridDriver builds the driver.
func NewCardGridDriver(ids *c.WidgetIdStack, pagerIds *c.WidgetIdStack) (inst *CardGridDriver) {
	return &CardGridDriver{
		ids:     ids,
		pager:   pager.New(pagerIds, cardgridDefaultPageSize).WithPageSizeOptions(cardgridPageSizes).WithUnit("cards"),
		cache:   newRichCellCache(ids),
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
	for _, key := range inst.shipped {
		inst.cache.tracker.Forget(key)
	}
	inst.shipped = inst.shipped[:0]
}

// render pages, folds the page (cached), draws the toolbar and the grid, and
// carries the selection both ways.
func (inst *CardGridDriver) render(app *PlayApp, rec arrow.RecordBatch, result ResultID, k cardgridClaim, emit SignalEmitterI) {
	schema := rec.Schema()
	if !inst.folded || inst.forResult != result {
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

	for range c.HorizontalTop().KeepIter() {
		cardgrid.Toolbar(inst.ids, "play-cards", &inst.state, k.slots())
		c.AddSpace(styletokens.GapSections(styletokens.ActiveDensity()))
		inst.pager.Render()
		if inst.unknownTones > 0 {
			for range c.HoverText("known tones: " + toneTokenNames()).KeepIter() {
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
			return inst.slotBlock(app, rec, schema, cols, k.hero, start+int64(ord), box, rawCells, true)
		}
	}
	if k.body >= 0 && !rawCells {
		in.Body = func(ord int, box cardgrid.Box) (cardgrid.Block, bool) {
			return inst.slotBlock(app, rec, schema, cols, k.body, start+int64(ord), box, rawCells, false)
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

	// A key past the page's edge: select the card the move lands on, and
	// let the page follow it next frame as it follows a cursor moved
	// elsewhere — which also reveals it.
	if sel := inst.state.Selected(); res.Past != 0 && sel >= 0 && rec.NumRows() > 0 {
		cur := start + int64(sel)
		if target := min(max(cur+int64(res.Past), 0), rec.NumRows()-1); target != cur {
			emit.Emit(signalSelection, target)
			return
		}
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
	inst.unknownTones = 0

	n := int(max(end-start, 0))
	m := &cardgrid.Model{Count: n, Slots: k.slots()}
	if !rawCells && k.mayRaise(cols) {
		// A one-line slot reports what it cannot honour as a fact, so the
		// facts are declared even when the result has no column of its own
		// to put there — a misspelt row value must not go unsaid (§SD2).
		m.Slots |= cardgrid.SlotsFacts
	}
	hasFacts := m.Slots.Has(cardgrid.SlotsFacts)
	// raised collects, per card, the reasons the one-line slots raise about
	// themselves; they go ahead of the result's columns so they are not the
	// ones "+k more" hides.
	raised := make([][]cardgridFact, n)
	text := func(col int, maxRunes int) []string {
		if col < 0 {
			return nil
		}
		out := make([]string, n)
		for i := range n {
			row := start + int64(i)
			var into *[]cardgridFact
			if hasFacts {
				into = &raised[i]
			}
			gc := app.rowGloss(rec, cols, col, row)
			out[i] = strings.Clone(cardgrid.OneLine(cardgridCellText(rec, gc, col, row, rawCells, into), maxRunes))
		}
		return out
	}
	if k.tone >= 0 {
		m.Tone = make([]color.Color, n)
	}
	if hasFacts {
		m.FactOff = make([]int32, 1, n+1)
		m.FactMore = make([]int32, n)
	}
	if k.tags >= 0 {
		m.TagOff = make([]int32, 1, n+1)
	}
	m.Overline = text(k.overline, cardgrid.MaxLineRunes)
	m.Title = text(k.title, cardgrid.MaxTitleRunes)
	m.Subtitle = text(k.subtitle, cardgrid.MaxLineRunes)
	m.Footer = text(k.footer, cardgrid.MaxLineRunes)
	if k.body >= 0 {
		m.Body = make([]string, n)
		for i := range n {
			row := start + int64(i)
			gc := app.rowGloss(rec, cols, k.body, row)
			if !rawCells && gc != nil && gc.mediaType != "" && (cardgridWhy(gc) != "" || drawsAsBlock(gc.mediaType)) {
				continue // the body block draws it, or says in the box why it cannot
			}
			m.Body[i] = strings.Clone(cardgrid.Lines(cardgridCellText(rec, gc, k.body, row, rawCells, nil), cardgrid.MaxBodyRunes, cardgridBodyLines))
		}
	}

	for i := range n {
		row := start + int64(i)
		if k.tone >= 0 && !rec.Column(k.tone).IsNull(int(row)) {
			token := strings.ToLower(strings.TrimSpace(formatDisplayCell(rec.Column(k.tone), row)))
			if t, known := toneTokens[token]; known {
				m.Tone[i] = toneTokenColor(t)
			} else if token != "" {
				inst.unknownTones++
			}
		}
		if hasFacts {
			facts := raised[i]
			more := 0
			for _, ci := range k.factCols {
				if rec.Column(ci).IsNull(int(row)) {
					continue
				}
				if len(facts) >= cardgridMaxFacts {
					more++ // counted, not rendered
					continue
				}
				if f, ok := cardgridFactOf(schema, app.rowGloss(rec, cols, ci, row), rec, ci, row, rawCells); ok {
					facts = append(facts, f)
				}
			}
			m.FactMore[i] = int32(more)
			for _, f := range facts {
				m.FactLabel = append(m.FactLabel, f.label)
				m.FactValue = append(m.FactValue, f.value)
				m.FactColor = append(m.FactColor, f.col)
			}
			m.FactOff = append(m.FactOff, int32(len(m.FactLabel)))
		}
		if k.tags >= 0 {
			m.Tag = append(m.Tag, cardgridTags(rec, k.tags, row)...)
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

// cardgridWarning is the colour a reason is drawn in.
func cardgridWarning() color.Color { return color.Hex(styletokens.WarningDefault.AsHex()) }

// cardgridCellText is a text slot's string: the gloss's inline face where the
// cell resolves to one, the plain rendering otherwise. A resolution that
// cannot be honoured — a misspelt row value, a refused kind — keeps the plain
// text and, when raised is not nil, says why as a fact on the same card, so
// it is loud without taking the slot's one line. The result may alias Arrow
// memory; the caller bounds it and copies what it keeps.
func cardgridCellText(rec arrow.RecordBatch, gc *glossColumn, col int, row int64, rawCells bool, raised *[]cardgridFact) string {
	arr := rec.Column(col)
	if arr.IsNull(int(row)) {
		return ""
	}
	if rawCells || gc == nil || gc.mediaType == "" {
		return formatDisplayCell(arr, row)
	}
	if why := cardgridWhy(gc); why != "" {
		if raised != nil {
			*raised = append(*raised, cardgridFact{
				label: strings.Clone(cardgrid.OneLine(pathColumnLabel(rec.Schema().Field(col).Name), cardgrid.MaxLineRunes)),
				value: strings.Clone(cardgrid.OneLine(why, cardgrid.MaxFactRunes)),
				col:   cardgridWarning(),
			})
		}
		return formatDisplayCell(arr, row)
	}
	return gc.inst.Inline(gloss.ArrowCell{Arr: arr, Row: int(row)}).Text
}

// cardgridFactOf folds one unclaimed column of one row: the inline face only
// (§SD1), its tone as the value's colour. ok is false for an empty value.
func cardgridFactOf(schema *arrow.Schema, gc *glossColumn, rec arrow.RecordBatch, ci int, row int64, rawCells bool) (f cardgridFact, ok bool) {
	label := shortColumnLabel(schema.Field(ci).Name)
	if gc != nil && gc.label != "" {
		label = gc.label
	}
	f.label = strings.Clone(cardgrid.OneLine(label, cardgrid.MaxLineRunes))
	var value string
	switch {
	case rawCells || gc == nil || gc.mediaType == "":
		value = formatDisplayCell(rec.Column(ci), row)
	case cardgridWhy(gc) != "":
		value, f.col = cardgridWhy(gc), cardgridWarning()
	default:
		face := gc.inst.Inline(gloss.ArrowCell{Arr: rec.Column(ci), Row: int(row)})
		value = face.Text
		if col, toned := toneColor(face.Tone); toned {
			f.col = col
		}
	}
	f.value = strings.Clone(cardgrid.OneLine(value, cardgrid.MaxFactRunes))
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
			if tag := cardgrid.OneLine(formatDisplayCell(values, j), cardgrid.MaxTagRunes); tag != "" {
				tags = append(tags, strings.Clone(tag))
			}
		}
		return
	}
	if tag := cardgrid.OneLine(formatDisplayCell(arr, row), cardgrid.MaxTagRunes); tag != "" {
		tags = append(tags, strings.Clone(tag))
	}
	return
}

// cardgridThumbSide is the bound a retained image is reduced to for a box:
// its longer side, rounded up to cardgridThumbStep. A page then costs what
// its boxes show — at one pixel per point; a denser display draws the
// thumbnail scaled up.
func cardgridThumbSide(box cardgrid.Box) uint32 {
	side := uint32(math.Ceil(float64(max(box.W, box.H))))
	return max(cardgridThumbStep, (side+cardgridThumbStep-1)/cardgridThumbStep*cardgridThumbStep)
}

// budgetLeft reports whether this frame may build another artifact. The first
// build of a frame is always allowed, so a page whose every artifact is over
// the budget still completes, one per frame.
func (inst *CardGridDriver) budgetLeft() bool {
	return inst.builds == 0 || inst.spent < cardgridFrameBudget
}

// charge books one artifact build, started at t0, against the frame budget.
// Only builds are charged: drawing a cached artifact is not one.
func (inst *CardGridDriver) charge(t0 time.Time) {
	inst.spent += time.Since(t0)
	inst.builds++
}

// slotBlock is the host-drawn hero or body of one card (§SD4, §SD5): an image
// as a contained thumbnail, a recording as its waveform, any other block face
// through glossBlock, and what stands in for one — declined for a NULL cell
// (the widget draws its placeholder) and for a body with no block face (the
// widget draws the text).
func (inst *CardGridDriver) slotBlock(app *PlayApp, rec arrow.RecordBatch, schema *arrow.Schema, cols []glossColumn, col int, row int64, box cardgrid.Box, rawCells bool, hero bool) (cardgrid.Block, bool) {
	if row < 0 || row >= rec.NumRows() || rec.Column(col).IsNull(int(row)) {
		return cardgrid.Block{}, false
	}
	field := schema.Field(col)
	gc := app.rowGloss(rec, cols, col, row)
	if rawCells || gc == nil || gc.mediaType == "" {
		if !hero {
			return cardgrid.Block{}, false
		}
		return plainHero(rec, field, col, row, rawCells)
	}
	if why := cardgridWhy(gc); why != "" {
		return cardgrid.Block{Reason: why}, true
	}
	key := richKey{col: col, ord: int(row)}
	switch {
	case isImageType(gc.mediaType):
		return inst.imageBlock(rec, gc, key, box), true
	case gloss.IsWAVMediaType(gc.mediaType):
		return inst.audioBlock(app, rec, gc, key, box), true
	case drawsAsBlock(gc.mediaType):
		raw, ok := cellRaw(rec, col, row)
		if !ok || raw == "" {
			return cardgrid.Block{}, false
		}
		// A link and a tagged id are drawn from the value each frame; every
		// other face is built once into the cache.
		_, cached := inst.cache.entries[key]
		build := !cached && gc.mediaType != gloss.MediaTypeURL && gc.mediaType != gloss.MediaTypeTaggedId
		if build {
			if !inst.budgetLeft() {
				inst.pending = true
				return cardgrid.Block{Pending: true}, true
			}
		}
		if !cached {
			// The raw cell aliases Arrow memory; the cache retains what it
			// derives from it, and a plain-text face or a link the text.
			raw = strings.Clone(raw)
		}
		t0 := time.Now()
		block := app.glossBlock(inst.cache, "cards", gc, key, raw, gloss.KindOfArrow(listElemType(field.Type)))
		if build {
			inst.charge(t0)
		}
		if block.Render == nil {
			return cardgrid.Block{}, false
		}
		return cardgrid.Block{Render: func() bool { block.Render(); return false }}, true
	case hero:
		// A gloss with an inline face only — a duration, a masked secret —
		// as a hero: the face, in the box.
		face := gc.inst.Inline(gloss.ArrowCell{Arr: rec.Column(col), Row: int(row)})
		return cardgridTextBlock(strings.Clone(cardgrid.Lines(face.Text, cardgrid.MaxBodyRunes, cardgridBodyLines))), true
	}
	return cardgrid.Block{}, false
}

// plainHero is a hero with no gloss. Text is shown as text; bytes are not
// guessed at — the pane says how to declare them instead, because a hex dump
// in a hero box helps nobody.
func plainHero(rec arrow.RecordBatch, field arrow.Field, col int, row int64, rawCells bool) (cardgrid.Block, bool) {
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
		raw = formatDisplayCell(rec.Column(col), row)
	}
	return cardgridTextBlock(strings.Clone(cardgrid.Lines(utfsafe.EnsureUTF8(raw), cardgrid.MaxBodyRunes, cardgridBodyLines))), true
}

// cardgridTextBlock draws bounded text in a block's box.
func cardgridTextBlock(text string) cardgrid.Block {
	return cardgrid.Block{Render: func() bool {
		c.Label(text).Wrap().Selectable(false).Send()
		return false
	}}
}

// imageBlock is an image cell as a thumbnail contained in the box: decoded
// under the header-first pixel budget, reduced to the box (cardgridThumbSide)
// and only the reduction kept (§SD4). A box that grows past what was kept —
// a larger density, a wider pane — has it rebuilt, the smaller one standing
// in meanwhile. Laid out by the SOURCE's size, so an icon stays an icon
// rather than filling the box at the thumbnail's.
func (inst *CardGridDriver) imageBlock(rec arrow.RecordBatch, gc *glossColumn, key richKey, box cardgrid.Box) cardgrid.Block {
	side := cardgridThumbSide(box)
	e, built := inst.cache.entries[key]
	if !built || e.shortOf(side) {
		if !inst.budgetLeft() {
			inst.pending = true
			if !built {
				return cardgrid.Block{Pending: true}
			}
		} else {
			t0 := time.Now()
			raw, _ := cellRaw(rec, key.col, int64(key.ord))
			d, _ := gc.declaration(gc.label)
			e = buildRichEntry(d, raw, side)
			inst.cache.entries[key] = e
			inst.charge(t0)
		}
	}
	if e.reason != "" {
		return cardgrid.Block{Reason: e.reason}
	}
	w, h := cardgrid.Fit(float32(e.srcW), float32(e.srcH), box)
	imgKey := "play-cards-img-" + key.String()
	// The content version carries the bound as well as the page: a rebuild
	// for a grown box is new content under the same widget.
	version := inst.cache.generation<<16 | uint64(e.side)
	return cardgrid.Block{W: w, H: h, Render: func() bool {
		// Two PrepareStr creators: each is a single-use state machine.
		// PixelsToSendFor, not PixelsToSend: the pane is a dock tab, where a
		// send is not a receipt (the Detail image face's note).
		imgId := inst.ids.PrepareStr(imgKey).Derive()
		pixels := inst.cache.tracker.PixelsToSendFor(imgKey, imgId, version, e.pixels)
		if len(pixels) > 0 {
			inst.shipped = append(inst.shipped, imgKey)
		}
		// The image senses clicks, so it reports them for the card.
		return c.Image(inst.ids.PrepareStr(imgKey), e.widthPx, e.heightPx, version,
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
// columns, and only the reduction kept — the cache's audio entry.
func (inst *CardGridDriver) audioBlock(app *PlayApp, rec arrow.RecordBatch, gc *glossColumn, key richKey, box cardgrid.Box) cardgrid.Block {
	e, built := inst.cache.entries[key]
	if !built {
		if !inst.budgetLeft() {
			inst.pending = true
			return cardgrid.Block{Pending: true}
		}
		t0 := time.Now()
		raw, _ := cellRaw(rec, key.col, int64(key.ord))
		d, _ := gc.declaration(gc.label)
		e = inst.cache.entryFor(key, d, raw)
		inst.charge(t0)
	}
	if e.reason != "" || e.audio == nil {
		return cardgrid.Block{Reason: e.reason}
	}
	a := e.audio
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
	if e, ok := inst.cache.entries[key]; ok && e.audio != nil && e.reason == "" {
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
