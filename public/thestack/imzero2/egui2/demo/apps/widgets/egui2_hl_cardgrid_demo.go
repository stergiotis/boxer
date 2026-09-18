package widgets

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/cardgrid"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

func init() {
	registry.Register(registry.Demo{
		Name: "cardgrid", Category: "Layout & widgets", Title: icons.PhCards + " card grid",
		Stage:       [2]float32{880, 1000},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "A page of items as a responsive grid of uniform cards (ADR-0245): hero, overline, title, subtitle, body, facts, tags, footer. Every card has the same height whatever it carries — the cards here are chosen to be awkward: a panorama, a strip and an icon as heroes (contained, never cropped or scaled up), a hero still being built, one that cannot be shown, one that is missing, a title that is one long path, a body of a hundred kilobytes, more facts and tags than fit, empty slots, and a hero with a control of its own that keeps its clicks. Click a card to select it, then use the arrow keys; S / M / L changes the density and the hero aspect follows the toggle.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			return newCardgridDemoState()
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoCardgrid(ids, state.(*cardgridDemoState))
		},
		SourceFunc: demoCardgrid,
	})
}

// cardgridDemoHero is one demo hero: procedural pixels at a native size, or
// one of the stand-in states.
type cardgridDemoHero struct {
	w, h    uint32
	pixels  []uint32
	pending bool
	reason  string
	control bool // a hero with a button of its own
}

type cardgridDemoState struct {
	model   *cardgrid.Model
	state   cardgrid.State
	heroes  []cardgridDemoHero
	last    cardgrid.Result
	presses int
	tracker *c.ImageVersionTracker[int]
}

// cardgridDemoPixels is a two-axis gradient with a diagonal grid, so scaling
// and letterboxing are visible at any aspect.
func cardgridDemoPixels(w, h uint32, hue int) (px []uint32) {
	px = make([]uint32, w*h)
	base := styletokens.QualitativeCycle(hue)
	for y := range h {
		for x := range w {
			fx, fy := float32(x)/float32(max(w-1, 1)), float32(y)/float32(max(h-1, 1))
			r := uint32(float32(base.R) * (0.35 + 0.65*fx))
			g := uint32(float32(base.G) * (0.35 + 0.65*fy))
			b := uint32(float32(base.B) * (0.35 + 0.65*(1-fx)))
			if (x+y)%16 == 0 {
				r, g, b = min(r+50, 255), min(g+50, 255), min(b+50, 255)
			}
			px[y*w+x] = r<<24 | g<<16 | b<<8 | 0xff
		}
	}
	return
}

func newCardgridDemoState() *cardgridDemoState {
	st := &cardgridDemoState{tracker: c.NewImageVersionTracker[int](), last: cardgrid.Result{Clicked: -1, Moved: -1, Toggled: -1}, model: &cardgrid.Model{
		Slots: cardgrid.SlotsHero | cardgrid.SlotsOverline | cardgrid.SlotsTitle | cardgrid.SlotsSubtitle |
			cardgrid.SlotsBody | cardgrid.SlotsFacts | cardgrid.SlotsTags | cardgrid.SlotsFooter,
		FactOff: []int32{0}, TagOff: []int32{0},
	}}
	m := st.model
	add := func(hero cardgridDemoHero, overline, title, subtitle, body, footer string, tone color.Color, facts [][2]string, tags []string) {
		st.heroes = append(st.heroes, hero)
		m.Overline = append(m.Overline, cardgrid.OneLine(overline, cardgrid.MaxLineRunes))
		m.Title = append(m.Title, cardgrid.OneLine(title, cardgrid.MaxTitleRunes))
		m.Subtitle = append(m.Subtitle, cardgrid.OneLine(subtitle, cardgrid.MaxLineRunes))
		m.Body = append(m.Body, cardgrid.Lines(body, cardgrid.MaxBodyRunes, 8))
		m.Footer = append(m.Footer, cardgrid.OneLine(footer, cardgrid.MaxLineRunes))
		m.Tone = append(m.Tone, tone)
		for _, f := range facts {
			m.FactLabel = append(m.FactLabel, f[0])
			m.FactValue = append(m.FactValue, cardgrid.OneLine(f[1], cardgrid.MaxFactRunes))
			col := color.Color{}
			if f[0] == "check" {
				col = color.Hex(styletokens.SuccessDefault.AsHex())
			}
			m.FactColor = append(m.FactColor, col)
		}
		m.FactOff = append(m.FactOff, int32(len(m.FactLabel)))
		m.Tag = append(m.Tag, tags...)
		m.TagOff = append(m.TagOff, int32(len(m.Tag)))
		m.Count++
	}
	img := func(w, h uint32, hue int) cardgridDemoHero {
		return cardgridDemoHero{w: w, h: h, pixels: cardgridDemoPixels(w, h, hue)}
	}
	none := color.Color{}

	add(img(320, 180, 0), "image", "Harbour at dusk", "a hero at the box's own aspect",
		"The ordinary card: every slot filled, nothing too long.", "2026-03-02 09:00",
		none, [][2]string{{"size", "320 × 180"}, {"bytes", "41 KiB"}, {"check", "ok"}}, []string{"photo", "sample"})
	add(img(640, 80, 1), "image", "Panorama", "eight to one",
		"Contained in the box as a band — never cropped, never stretched.", "2026-03-02 09:01",
		none, [][2]string{{"size", "640 × 80"}}, []string{"photo"})
	add(img(40, 320, 2), "image", "Strip", "one to eight",
		"Contained as a sliver.", "2026-03-02 09:02",
		none, [][2]string{{"size", "40 × 320"}}, []string{"photo"})
	add(img(16, 16, 3), "image", "Icon", "sixteen pixels square",
		"Not scaled past its native size: an icon stays an icon.", "2026-03-02 09:03",
		none, [][2]string{{"size", "16 × 16"}}, []string{"icon"})
	add(cardgridDemoHero{pending: true}, "image", "Still being built", "",
		"The host has not decoded this hero yet; the card draws a skeleton and keeps its size.", "",
		none, nil, nil)
	add(cardgridDemoHero{reason: "image is 30000 × 30000 (900 MP), over the 64 MP budget"}, "image", "Too large to decode", "",
		"The reason is shown where the hero would have been.", "",
		color.Hex(styletokens.WarningDefault.AsHex()), nil, []string{"rejected"})
	add(cardgridDemoHero{}, "note", "No hero on this card", "the box stays, so the row keeps its line",
		"", "", none, [][2]string{{"kind", "note"}}, nil)
	add(img(320, 180, 4), "file",
		"/srv/archive/2026/03/02/sensors/north-pier/anemometer/raw/0000000000000000000000000000000000000000000000000000000017.parquet",
		"a title with no break opportunity",
		strings.Repeat("A very long body. It is cut in Go to the slot's budget before it is drawn, so a hundred kilobytes of text cost what their first lines cost. ", 700),
		"wrapped anywhere, cut at two lines, whole on hover",
		none, [][2]string{{"a rather long fact label that will not fit", strings.Repeat("and a long value ", 20)}}, nil)
	facts := make([][2]string, 12)
	for i := range facts {
		facts[i] = [2]string{"fact " + strconv.Itoa(i+1), "value " + strconv.Itoa(i+1)}
	}
	add(img(200, 200, 5), "wide row", "More facts and tags than fit", "the rest is counted, not dropped silently",
		"Detail is where the rest is read.", "",
		color.Hex(styletokens.AccentDefault.AsHex()), facts,
		[]string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta", "iota", "kappa"})
	add(cardgridDemoHero{control: true}, "audio", "A hero with a control of its own", "the button keeps its clicks; the rest selects the card",
		"", "", color.Hex(styletokens.SuccessDefault.AsHex()), [][2]string{{"length", "0:03"}}, []string{"audio"})
	add(img(320, 180, 6), "", "", "", "", "", none, nil, nil)
	add(img(320, 240, 7), "text", "Ünïcödé — 日本語のタイトル — Ελληνικά — Кириллица", "mixed scripts",
		"Line one\nLine two\nLine three\nLine four\nLine five\nLine six", "",
		none, [][2]string{{"runes", "many"}}, []string{"i18n"})
	return st
}

// demoCardgrid exercises the card grid over a fixed page of awkward cards.
func demoCardgrid(ids *c.WidgetIdStack, st *cardgridDemoState) {
	for range c.HorizontalTop().KeepIter() {
		cardgrid.Toolbar(ids, "cardgrid-demo", &st.state, st.model.Slots)
		c.AddSpace(styletokens.GapItems(styletokens.ActiveDensity()))
		c.Label("selected " + strconv.Itoa(int(st.state.Selected())) +
			"   last click " + strconv.Itoa(int(st.last.Clicked)) +
			"   button presses " + strconv.Itoa(st.presses) +
			"   focused " + strconv.FormatBool(st.state.Focused())).Send()
	}
	res := cardgrid.Render(cardgrid.Input{
		Ids: ids, ScopeKey: "cardgrid-demo", Model: st.model, State: &st.state,
		Hero: func(i int, box cardgrid.Box) (cardgrid.Block, bool) {
			h := &st.heroes[i]
			switch {
			case h.pending:
				return cardgrid.Block{Pending: true}, true
			case h.reason != "":
				return cardgrid.Block{Reason: h.reason}, true
			case h.control:
				return cardgrid.Block{W: 120, H: 28, Render: func() bool {
					if c.Button(ids.PrepareStr("play"), c.Atoms().Text(icons.PhPlay+" play").Keep()).
						SendResp().HasPrimaryClicked() {
						st.presses++
					}
					return false
				}}, true
			case len(h.pixels) == 0:
				return cardgrid.Block{}, false
			}
			w, hh := cardgrid.Fit(float32(h.w), float32(h.h), box)
			return cardgrid.Block{W: w, H: hh, Render: func() bool {
				// Version-tracked: a page of heroes re-sent every frame is
				// megabytes per frame. PixelsToSendFor, not PixelsToSend —
				// the gallery is a host-skippable region, where a send is
				// not a receipt. Two PrepareStr creators, each single-use.
				imgId := ids.PrepareStr("img").Derive()
				pixels := st.tracker.PixelsToSendFor(i, imgId, 1, h.pixels)
				return c.Image(ids.PrepareStr("img"), h.w, h.h, 1,
					uint8(c.FitFixedE), uint32(w), uint32(hh),
					uint8(c.FilterLinearE), c.TintNoneRgba, pixels).
					SendResp().HasPrimaryClicked()
			}}, true
		},
	})
	if res.Clicked >= 0 || res.Moved >= 0 {
		st.last = res
	}
}
