//go:build llm_generated_opus46

package card

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/dim13/colormap"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/pebble2impl/src/go/public/thestack/fffi2/typed"
	c "github.com/stergiotis/pebble2impl/src/go/public/thestack/imzero2/egui2/components"
)

var _ streamreadaccess.SinkI = (*ImZero2CardEmitter)(nil)

// ImZero2CardEmitter renders Leeway entities as ImZero2 immediate-mode widgets.
//
// Architecture: because ImZero2 KeepIter() scopes execute synchronously and cannot
// span across multiple StructuredOutput2I method calls, this emitter uses an
// accumulate-then-render pattern:
//   - Column data is buffered during BeginColumn..EndColumn
//   - Tags are buffered during BeginTags..EndTags
//   - The complete attribute card is rendered at EndPlainValue/EndTaggedValue
//   - The section wrapper (label + HorizontalWrapped card flow) is rendered at EndSection/EndPlainSection
//
// This matches the TextCardEmitter's strategy of buffering rows and flushing at section end.
type ImZero2CardEmitter struct {
	ids     *c.WidgetIdStack
	palette color.Palette

	// Running ID counter
	idCounter uint64

	// Per-section state
	sectionName string
	sectionIdx  int
	colNames    []naming.StylableName
	nAttrs      int
	cards       []imzeroCard // accumulated attribute cards for current section

	// Per-attribute card accumulation
	currentCard *imzeroCard

	// Per-column accumulation
	currentColName string
	currentColType string
	cellBuf        strings.Builder
	inCollection   bool
	collType       int // 1=array, 2=set
	itemIdx        int

	// Callback for tag interaction (caller provides clipboard integration)
	// displayText: the tag's display value, detail: full tag description
	OnTagClicked func(displayText string, detail string)

	// Entity counter
	entityIdx int

	err error
}

type imzeroCard struct {
	columns []imzeroColumn
	tags    []imzeroTag
}

type imzeroColumn struct {
	name  string
	ctype string
	value string
}

type imzeroTag struct {
	display string
	detail  string
}

func NewImzeroCardEmitter(ids *c.WidgetIdStack, palette ColorPaletteE) (inst *ImZero2CardEmitter) {
	var pal color.Palette
	switch palette {
	case ColorPaletteViridis:
		pal = colormap.Viridis
	case ColorPaletteMagma:
		pal = colormap.Magma
	case ColorPalettePlasma:
		pal = colormap.Plasma
	default:
		pal = colormap.Inferno
	}
	inst = &ImZero2CardEmitter{
		ids:     ids,
		palette: pal,
		cards:   make([]imzeroCard, 0, 16),
	}
	return
}

// --- ID generation ---

func (inst *ImZero2CardEmitter) nextId() *c.WidgetIdStack {
	inst.idCounter++
	return inst.ids.PrepareSeq(inst.idCounter)
}

// --- Palette ---

func (inst *ImZero2CardEmitter) sectionColorRGB(idx int) (r, g, b uint8) {
	n := len(inst.palette)
	if n == 0 {
		return 0x88, 0x88, 0x88
	}
	lo := n / 5
	hi := n * 4 / 5
	span := hi - lo
	if span <= 0 {
		span = 1
	}
	pos := lo + (idx*37)%span
	if pos >= n {
		pos = pos % n
	}
	rv, gv, bv, _ := inst.palette[pos].RGBA()
	return uint8(rv >> 8), uint8(gv >> 8), uint8(bv >> 8)
}

func (inst *ImZero2CardEmitter) makeAccentColor(sectionIdx int) typed.RetainedFffiHolderTyped[c.Color32S] {
	r, g, b := inst.sectionColorRGB(sectionIdx)
	return c.Color().FromRgb(r, g, b).Keep()
}

// --- Batch ---

func (inst *ImZero2CardEmitter) BeginBatch() {
	inst.entityIdx = 0
	inst.idCounter = 0x10000
	inst.sectionIdx = 0
}

func (inst *ImZero2CardEmitter) EndBatch() (err error) {
	return inst.err
}

// --- Entity ---

func (inst *ImZero2CardEmitter) BeginEntity() {
	if inst.entityIdx > 0 {
		c.Separator().Spacing(8).Send()
	}
	c.PushRichText(fmt.Sprintf("Entity %d", inst.entityIdx)).Strong().Display()
	inst.entityIdx++
	inst.sectionIdx = 0
}

func (inst *ImZero2CardEmitter) EndEntity() (err error) {
	return inst.err
}

// --- Plain section ---

func (inst *ImZero2CardEmitter) BeginPlainSection(itemType common.PlainItemTypeE, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	inst.sectionName = itemType.String()
	inst.colNames = valueNames
	inst.nAttrs = nAttrs
	inst.cards = inst.cards[:0]
	inst.currentCard = nil
	inst.sectionIdx++
}

func (inst *ImZero2CardEmitter) EndPlainSection() (err error) {
	if inst.nAttrs == 0 || len(inst.cards) == 0 {
		return inst.err
	}
	inst.flushSection(false)
	return inst.err
}

// --- Plain value ---

func (inst *ImZero2CardEmitter) BeginPlainValue() {
	card := imzeroCard{
		columns: make([]imzeroColumn, 0, len(inst.colNames)),
	}
	inst.cards = append(inst.cards, card)
	inst.currentCard = &inst.cards[len(inst.cards)-1]
}

func (inst *ImZero2CardEmitter) EndPlainValue() (err error) {
	inst.currentCard = nil
	return inst.err
}

// --- Tagged sections scope ---

func (inst *ImZero2CardEmitter) BeginTaggedSections() {}

func (inst *ImZero2CardEmitter) EndTaggedSections() (err error) {
	return inst.err
}

// --- Co-section group ---

func (inst *ImZero2CardEmitter) BeginCoSectionGroup(name naming.Key) {
	// Rendered as a visual label before the contained section
	accentColor := inst.makeAccentColor(inst.sectionIdx)
	transparentBg := c.Color().FromBlackAlpha(0).Keep()
	c.PushRichTextColored(accentColor, transparentBg, fmt.Sprintf("co: %s", string(name))).Small().Monospace().Display()
}

func (inst *ImZero2CardEmitter) EndCoSectionGroup() (err error) {
	return inst.err
}

// --- Section ---

func (inst *ImZero2CardEmitter) BeginSection(name naming.StylableName, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	inst.sectionName = name.String()
	inst.colNames = valueNames
	inst.nAttrs = nAttrs
	inst.cards = inst.cards[:0]
	inst.currentCard = nil
	inst.sectionIdx++
}

func (inst *ImZero2CardEmitter) EndSection() (err error) {
	if inst.nAttrs == 0 || len(inst.cards) == 0 {
		return inst.err
	}
	inst.flushSection(true)
	return inst.err
}

// --- Tagged value ---

func (inst *ImZero2CardEmitter) BeginTaggedValue() {
	card := imzeroCard{
		columns: make([]imzeroColumn, 0, len(inst.colNames)),
		tags:    make([]imzeroTag, 0, 2),
	}
	inst.cards = append(inst.cards, card)
	inst.currentCard = &inst.cards[len(inst.cards)-1]
}

func (inst *ImZero2CardEmitter) EndTaggedValue() (err error) {
	inst.currentCard = nil
	return inst.err
}

// --- Column ---

func (inst *ImZero2CardEmitter) BeginColumn(colAddr streamreadaccess.PhysicalColumnAddr, name naming.StylableName, canonicalType canonicaltypes.PrimitiveAstNodeI) {
	inst.currentColName = name.String()
	if canonicalType != nil {
		inst.currentColType = canonicalType.String()
	} else {
		inst.currentColType = ""
	}
	inst.cellBuf.Reset()
	inst.inCollection = false
}

func (inst *ImZero2CardEmitter) EndColumn() {
	if inst.currentCard != nil {
		inst.currentCard.columns = append(inst.currentCard.columns, imzeroColumn{
			name:  inst.currentColName,
			ctype: inst.currentColType,
			value: inst.cellBuf.String(),
		})
	}
	inst.inCollection = false
}

// --- Scalar ---

func (inst *ImZero2CardEmitter) BeginScalarValue() {
	inst.inCollection = false
}

func (inst *ImZero2CardEmitter) EndScalarValue() (err error) {
	return inst.err
}

// --- Array ---

func (inst *ImZero2CardEmitter) BeginHomogenousArrayValue(card int) {
	inst.inCollection = true
	inst.collType = 1
}

func (inst *ImZero2CardEmitter) EndHomogenousArrayValue() {
	inst.inCollection = false
}

// --- Set ---

func (inst *ImZero2CardEmitter) BeginSetValue(card int) {
	inst.inCollection = true
	inst.collType = 2
}

func (inst *ImZero2CardEmitter) EndSetValue() {
	inst.inCollection = false
}

// --- Value item ---

func (inst *ImZero2CardEmitter) BeginValueItem(index int) {
	inst.itemIdx = index
	if inst.cellBuf.Len() > 0 {
		inst.cellBuf.WriteByte('\n')
	}
	switch inst.collType {
	case 1: // array
		fmt.Fprintf(&inst.cellBuf, "[%d] ", index)
	case 2: // set
		inst.cellBuf.WriteString("• ")
	}
}

func (inst *ImZero2CardEmitter) EndValueItem() {}

// --- Write ---

func (inst *ImZero2CardEmitter) Write(p []byte) (n int, err error) {
	return inst.WriteString(string(p))
}

func (inst *ImZero2CardEmitter) WriteString(s string) (n int, err error) {
	n = len(s)
	inst.cellBuf.WriteString(s)
	return
}

// --- Tags ---

func (inst *ImZero2CardEmitter) BeginTags(nTags int) {}
func (inst *ImZero2CardEmitter) EndTags()            {}

func (inst *ImZero2CardEmitter) AddMembershipRef(lowCard bool, ref uint64, humanReadableRef string) {
	if inst.currentCard == nil {
		return
	}
	cardStr := "high-card"
	if lowCard {
		cardStr = "low-card"
	}
	inst.currentCard.tags = append(inst.currentCard.tags, imzeroTag{
		display: fmt.Sprintf("ref:%s", humanReadableRef),
		detail:  fmt.Sprintf("Reference (%s) ref=0x%x display=%s", cardStr, ref, humanReadableRef),
	})
}

func (inst *ImZero2CardEmitter) AddMembershipVerbatim(lowCard bool, verbatim string, humanReadableVerbatim string) {
	if inst.currentCard == nil {
		return
	}
	cardStr := "high-card"
	if lowCard {
		cardStr = "low-card"
	}
	inst.currentCard.tags = append(inst.currentCard.tags, imzeroTag{
		display: fmt.Sprintf("v:%s", humanReadableVerbatim),
		detail:  fmt.Sprintf("Verbatim (%s) value=%q display=%s", cardStr, verbatim, humanReadableVerbatim),
	})
}

func (inst *ImZero2CardEmitter) AddMembershipRefParametrized(lowCard bool, ref uint64, humanReadableRef string, params string, humanReadableParams string) {
	if inst.currentCard == nil {
		return
	}
	cardStr := "high-card"
	if lowCard {
		cardStr = "low-card"
	}
	display := fmt.Sprintf("rp:%s", humanReadableRef)
	if humanReadableParams != "" {
		display = fmt.Sprintf("rp:%s(%s)", humanReadableRef, humanReadableParams)
	}
	inst.currentCard.tags = append(inst.currentCard.tags, imzeroTag{
		display: display,
		detail:  fmt.Sprintf("RefParametrized (%s) ref=0x%x params=%q", cardStr, ref, params),
	})
}

func (inst *ImZero2CardEmitter) AddMembershipMixedLowCardRefHighCardParam(ref uint64, humanReadableRef string, params string, humanReadableParams string) {
	if inst.currentCard == nil {
		return
	}
	display := fmt.Sprintf("mr:%s", humanReadableRef)
	if humanReadableParams != "" {
		display = fmt.Sprintf("mr:%s(%s)", humanReadableRef, humanReadableParams)
	}
	inst.currentCard.tags = append(inst.currentCard.tags, imzeroTag{
		display: display,
		detail:  fmt.Sprintf("MixedLowCardRef ref=0x%x params=%q", ref, params),
	})
}

func (inst *ImZero2CardEmitter) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, humanReadableVerbatim string, params string, humanReadableParams string) {
	if inst.currentCard == nil {
		return
	}
	display := fmt.Sprintf("mv:%s", humanReadableVerbatim)
	if humanReadableParams != "" {
		display = fmt.Sprintf("mv:%s(%s)", humanReadableVerbatim, humanReadableParams)
	}
	inst.currentCard.tags = append(inst.currentCard.tags, imzeroTag{
		display: display,
		detail:  fmt.Sprintf("MixedLowCardVerbatim value=%q params=%q", verbatim, params),
	})
}

// --- Rendering (flush) ---

// flushSection renders the accumulated cards for the current section.
// Called at EndSection (tagged) or EndPlainSection (plain).
func (inst *ImZero2CardEmitter) flushSection(hasTags bool) {
	// Section label with accent color
	accentColor := inst.makeAccentColor(inst.sectionIdx)
	transparentBg := c.Color().FromBlackAlpha(0).Keep()

	sectionLabel := inst.sectionName
	if hasTags && inst.nAttrs > 0 {
		sectionLabel = fmt.Sprintf("%s (%d)", inst.sectionName, inst.nAttrs)
	}
	c.PushRichTextColored(accentColor, transparentBg, sectionLabel).Monospace().Small().Display()

	// Cards in HorizontalWrapped flow
	//for range c.HorizontalWrapped().KeepIter()
	{
		for cardIdx := range inst.cards {
			card := &inst.cards[cardIdx]
			inst.renderCard(card, hasTags, accentColor)
		}
	}
}

// renderCard renders a single attribute card as a Frame containing a Grid kv-table
// and optional tag buttons.
func (inst *ImZero2CardEmitter) renderCard(card *imzeroCard, hasTags bool, accentColor typed.RetainedFffiHolderTyped[c.Color32S]) {
	for range c.Frame(inst.nextId()).InnerMargin(4).CornerRadius(4).KeepIter() {
		// Section name accent label at top of card
		transparentBg := c.Color().FromBlackAlpha(0).Keep()
		c.PushRichTextColored(accentColor, transparentBg, inst.sectionName).Monospace().Small().Display()

		// Key-value grid
		if len(card.columns) > 0 {
			for range c.Grid(inst.nextId()).NumColumns(2).KeepIter() {
				for _, col := range card.columns {
					// Key cell: name + type
					keyLabel := col.name
					if col.ctype != "" {
						keyLabel = fmt.Sprintf("%s %s", col.name, col.ctype)
					}
					c.PushRichText(keyLabel).Monospace().Weak().Small().Display()

					// Value cell
					c.PushRichText(col.value).Monospace().Strong().Small().Display()
				}
			}
		}

		// Tags
		if hasTags && len(card.tags) > 0 {
			c.Separator().Spacing(2).Send()
			//for range c.HorizontalWrapped().KeepIter()
			{
				for _, tag := range card.tags {
					tagAtoms := c.Atoms().Text(tag.display).Keep()
					resp := c.Button(inst.nextId(), tagAtoms).Small().SendResp()
					if resp.HasPrimaryClicked() && inst.OnTagClicked != nil {
						inst.OnTagClicked(tag.display, tag.detail)
					}
				}
			}
		}
	}
}
