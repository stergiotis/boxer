package play

import (
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/cardgrid"
)

// The Cards pane as an agent reads and sets it (ADR-0270, update of
// 2026-10-05): the page it drew, each card's text as the glosses resolved
// it, the warning facts the pane raised where a gloss could not be
// honoured, and the page, page size, density and hero aspect. The read is
// of the page drawn: the pane folds one page at a time, through glosses
// that only the render goroutine reaches, so another page is turned to,
// not read off-screen. Hero bytes never travel; a capture shows them.

const (
	opGetCards          = "get_cards"
	opSetCardsOptions   = "set_cards_options"
	opsResCards         = cardsPaneId
	cardsPaneId         = "cards"
	cardsReadMaxCards   = 24
	cardsReadBodyRunes  = 200
	cardsReadFactsRunes = 120
)

var (
	// cardgridDensityNames and cardgridAspectNames are indexed by the
	// widget's enums, spelled as its toolbar labels them.
	cardgridDensityNames = []string{"M", "S", "L"}
	cardgridAspectNames  = []string{"16:9", "4:3", "1:1"}
)

// CardFactReading is one fact a card shows.
type CardFactReading struct {
	Label   string `desc:"the fact's label: the column's gloss label or its short name"`
	Value   string `desc:"the value as the card shows it"`
	Warning bool   `json:",omitzero" desc:"drawn in the warning colour: a slot or fact whose gloss could not be honoured, the value then being the reason, or a value its gloss tones as a warning"`
}

// CardReading is one card of the page.
type CardReading struct {
	Row       int64             `desc:"the card's row in the result"`
	Overline  string            `json:",omitzero" desc:"the line above the title"`
	Title     string            `json:",omitzero" desc:"the title as drawn"`
	Subtitle  string            `json:",omitzero" desc:"the subtitle as drawn"`
	Body      string            `json:",omitzero" desc:"the body text, cut at 200 characters; empty when the body draws as a block (markdown, an image)"`
	Footer    string            `json:",omitzero" desc:"the footer as drawn"`
	Tags      []string          `json:",omitzero" desc:"the tags"`
	Facts     []CardFactReading `json:",omitzero" desc:"the facts shown, at most 16"`
	FactsMore int32             `json:",omitzero" desc:"facts past the 16 a card shows"`
	Hero      string            `json:",omitzero" desc:"the hero's media type, as its gloss resolved it"`
	HeroNote  string            `json:",omitzero" desc:"why the hero cannot be shown, as the card says in its box"`
}

// CardsReading is get_cards' result.
type CardsReading struct {
	Drawn        PaneDraw      `desc:"which draw this is of, its status line, and why it drew nothing when it did not"`
	Rows         int64         `desc:"rows in the result: one card each"`
	Page         int64         `desc:"the page drawn, 1 for the first"`
	Pages        int64         `desc:"pages in all"`
	PageSize     int64         `desc:"cards per page: 12, 24, 48 or 96"`
	Density      string        `desc:"card size: S, M or L"`
	Aspect       string        `json:",omitzero" desc:"the hero box's aspect: 16:9, 4:3 or 1:1; only with a card_hero"`
	RawCells     bool          `json:",omitzero" desc:"the Table pane's raw cells is on, so no gloss resolves and the cards show cell text"`
	UnknownTones int32         `json:",omitzero" desc:"card_tone values on this page that name no tone"`
	Cards        []CardReading `json:",omitzero" desc:"the page's cards from offset, at most 24 per call"`
	More         int32         `json:",omitzero" desc:"cards of the page past the ones listed; read on with offset"`
}

// GetCardsArgs is get_cards' argument.
type GetCardsArgs struct {
	Offset int32 `json:",omitzero" desc:"the first card of the page to list, 0 for the first"`
	Limit  int32 `json:",omitzero" desc:"cards to list, at most 24 (the default)"`
}

// SetCardsOptionsArgs is set_cards_options' argument.
type SetCardsOptionsArgs struct {
	Page     *int64  `json:",omitzero" desc:"the page to turn to, 1 for the first"`
	PageSize *int64  `json:",omitzero" desc:"cards per page: 12, 24, 48 or 96; the card mid-page stays in view"`
	Density  *string `json:",omitzero" desc:"card size: S, M or L"`
	Aspect   *string `json:",omitzero" desc:"the hero box's aspect: 16:9, 4:3 or 1:1; only with a card_hero"`
}

// cardsOpsView is what get_cards reads: the page's model and hero notes,
// shared (a refold replaces them and nothing edits them), and the
// settings.
type cardsOpsView struct {
	model        *cardgrid.Model
	heroMedia    []string
	heroNote     []string
	start        int64
	rows         int64
	page         int64
	pages        int64
	pageSize     int64
	density      cardgrid.DensityE
	aspect       cardgrid.AspectE
	hero         bool
	rawCells     bool
	unknownTones int
}

func (inst *CardGridDriver) opsView(rawCells bool) cardsOpsView {
	return cardsOpsView{model: inst.model, heroMedia: inst.heroMedia, heroNote: inst.heroNote, start: inst.forStart,
		rows: inst.rows, page: inst.pager.CurrentPage(), pages: inst.pager.NumPages(), pageSize: inst.pager.PageSize(),
		density: inst.state.Density(), aspect: inst.state.Aspect(), hero: inst.claimSeen && inst.claim.hero >= 0,
		rawCells: rawCells, unknownTones: inst.unknownTones}
}

// cardsView is the Cards' view for the snapshot.
func (inst *PlayApp) cardsView() cardsOpsView {
	return inst.cardgridDriver.opsView(inst.tableOpts.rawCells)
}

// cardgridHeroNote is a card's hero as get_cards reads it: its media type,
// and the reason the card's box gives when it cannot show it.
func cardgridHeroNote(app *PlayApp, rec arrow.RecordBatch, schema *arrow.Schema, cols []glossColumn, col int, row int64, rawCells bool) (media, note string) {
	if row < 0 || row >= rec.NumRows() || rec.Column(col).IsNull(int(row)) {
		return
	}
	gc := app.rowGloss(rec, cols, col, row)
	if !rawCells && gc != nil && gc.mediaType != "" {
		return gc.mediaType, cardgridWhy(gc)
	}
	field := schema.Field(col)
	switch field.Type.ID() {
	case arrow.BINARY, arrow.LARGE_BINARY, arrow.FIXED_SIZE_BINARY, arrow.BINARY_VIEW:
		block, _ := plainHero(rec, field, col, row, rawCells)
		note = block.Reason
	}
	return
}

func cardsText(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}

// cardReading reads card i of the page.
func (v *cardsOpsView) cardReading(i int) (r CardReading) {
	m := v.model
	r = CardReading{Row: v.start + int64(i), Overline: cardsText(m.Overline, i), Title: cardsText(m.Title, i),
		Subtitle: cardsText(m.Subtitle, i), Footer: cardsText(m.Footer, i),
		Body: truncateRunes(cardsText(m.Body, i), cardsReadBodyRunes),
		Hero: cardsText(v.heroMedia, i), HeroNote: cardsText(v.heroNote, i)}
	if len(m.TagOff) > i+1 {
		r.Tags = m.Tag[m.TagOff[i]:m.TagOff[i+1]:m.TagOff[i+1]]
	}
	if len(m.FactOff) > i+1 {
		warn := cardgridWarning()
		for j := m.FactOff[i]; j < m.FactOff[i+1]; j++ {
			f := CardFactReading{Label: m.FactLabel[j], Value: truncateRunes(m.FactValue[j], cardsReadFactsRunes)}
			f.Warning = int(j) < len(m.FactColor) && m.FactColor[j] == warn
			r.Facts = append(r.Facts, f)
		}
	}
	if i < len(m.FactMore) {
		r.FactsMore = m.FactMore[i]
	}
	return
}

// cardsReading is get_cards.
func cardsReading(sn *opsSnap, in GetCardsArgs) (out CardsReading, err error) {
	d, readable, err := paneDrawOf(sn, cardsPaneId)
	if err != nil {
		return
	}
	v := &sn.paneViews.cards
	out = CardsReading{Drawn: d, Rows: v.rows, Page: v.page + 1, Pages: v.pages, PageSize: v.pageSize,
		Density: nameAt(cardgridDensityNames, int(v.density)), RawCells: v.rawCells}
	if v.hero {
		out.Aspect = nameAt(cardgridAspectNames, int(v.aspect))
	}
	if !readable || v.model == nil {
		return
	}
	out.UnknownTones = int32(v.unknownTones)
	n := v.model.Count
	off := int(in.Offset)
	if off < 0 || (off > 0 && off >= n) {
		return out, app.RefuseOperation("the page drawn has " + strconv.Itoa(n) + " cards; offset " + strconv.Itoa(off) + " is past them")
	}
	limit := cardsReadMaxCards
	if in.Limit > 0 {
		limit = min(int(in.Limit), cardsReadMaxCards)
	}
	used := 0
	for i := off; i < n; i++ {
		r := v.cardReading(i)
		used += cardReadingBytes(&r)
		if i-off >= limit || (used > opsSampleMaxBytes && i > off) {
			out.More = int32(n - i)
			break
		}
		out.Cards = append(out.Cards, r)
	}
	return
}

// cardReadingBytes estimates a card's share of a reply.
func cardReadingBytes(r *CardReading) (n int) {
	n = 64 + len(r.Overline) + len(r.Title) + len(r.Subtitle) + len(r.Body) + len(r.Footer) + len(r.Hero) + len(r.HeroNote)
	for _, t := range r.Tags {
		n += len(t) + 4
	}
	for _, f := range r.Facts {
		n += len(f.Label) + len(f.Value) + 16
	}
	return
}

// nameIndex is the index of name in names, ignoring case and spaces.
func nameIndex(names []string, name string) int {
	for i, n := range names {
		if strings.EqualFold(n, strings.TrimSpace(name)) {
			return i
		}
	}
	return -1
}

// setOptions is set_cards_options on the driver: all named options are
// checked before any is applied; the page size goes before the page.
func (inst *CardGridDriver) setOptions(in SetCardsOptionsArgs) (err error) {
	if in.Page == nil && in.PageSize == nil && in.Density == nil && in.Aspect == nil {
		return noOptionsRefusal(cardsPaneId, "page", "page_size", "density", "aspect")
	}
	density, aspect := -1, -1
	if in.Density != nil {
		if density = nameIndex(cardgridDensityNames, *in.Density); density < 0 {
			return app.RefuseOperation("density is S, M or L")
		}
	}
	if in.Aspect != nil {
		if inst.claimSeen && inst.claim.hero < 0 {
			return app.RefuseOperation("the cards have no card_hero, so there is no hero box to shape")
		}
		if aspect = nameIndex(cardgridAspectNames, *in.Aspect); aspect < 0 {
			return app.RefuseOperation("aspect is 16:9, 4:3 or 1:1")
		}
	}
	size := inst.pager.PageSize()
	if in.PageSize != nil {
		ok := false
		for _, s := range cardgridPageSizes {
			ok = ok || s == *in.PageSize
		}
		if !ok {
			return app.RefuseOperation("page_size is 12, 24, 48 or 96")
		}
		size = *in.PageSize
	}
	if in.Page != nil {
		pages := max((inst.rows+size-1)/size, 1)
		if *in.Page < 1 || *in.Page > pages {
			return app.RefuseOperation("page is 1 to " + strconv.FormatInt(pages, 10) + " at " + strconv.FormatInt(size, 10) + " cards a page")
		}
	}
	if density >= 0 {
		inst.state.SetDensity(cardgrid.DensityE(density))
	}
	if aspect >= 0 {
		inst.state.SetAspect(cardgrid.AspectE(aspect))
	}
	if in.PageSize != nil {
		inst.pager.SetPageSize(size)
	}
	if in.Page != nil {
		inst.pager.GoToIndex((*in.Page - 1) * size)
	}
	return
}

// requestOptions is a toolbar or pager change: through set_cards_options
// when play's launcher routes it, directly otherwise.
func (inst *CardGridDriver) requestOptions(in SetCardsOptionsArgs) {
	if inst.onOptions != nil {
		inst.onOptions(in)
		return
	}
	_ = inst.setOptions(in)
}

// cardsOptionsDigest is the cards resource: the page, its size, the density
// and the aspect.
func cardsOptionsDigest(p *PlayApp) string {
	d := p.cardgridDriver
	if d == nil {
		return ""
	}
	return "page=" + strconv.FormatInt(d.pager.CurrentPage(), 10) + "|size=" + strconv.FormatInt(d.pager.PageSize(), 10) +
		"|d=" + strconv.Itoa(int(d.state.Density())) + "|a=" + strconv.Itoa(int(d.state.Aspect()))
}

func addCardsOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	addPaneOps(s, paneOpsSpec[CardsReading, GetCardsArgs, SetCardsOptionsArgs]{
		pane:     cardsPaneId,
		resource: "the Cards pane's settings: the page, cards per page, density and hero aspect",
		digest:   cardsOptionsDigest,
		get:      opGetCards,
		getSummary: "read the page the Cards pane last drew: each card's text as its glosses resolved it, its facts with the " +
			"warnings the pane raised, the hero's media type or why it cannot be shown, and the page and settings",
		set:        opSetCardsOptions,
		setSummary: "turn the Cards pane's page, or set cards per page, card density or hero aspect",
		gesture:    "the density and aspect buttons and the pager above the cards",
		follows: []string{"the pane draws with the new settings from its next frame; the result is not rerun",
			"a new result turns back to the first page; a selection moved elsewhere turns to its page"},
		read:  cardsReading,
		apply: func(p *PlayApp, in SetCardsOptionsArgs) error { return p.cardgridDriver.setOptions(in) },
	})
}
