package cardgrid

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// SlotsE is the set of slots a model declares. It is a property of the whole
// page, not of a card: the card height follows from it (see [Plan]).
type SlotsE uint16

const (
	SlotsHero SlotsE = 1 << iota
	SlotsOverline
	SlotsTitle
	SlotsSubtitle
	SlotsBody
	SlotsFacts
	SlotsTags
	SlotsFooter
)

// Has reports whether every slot of s is declared.
func (inst SlotsE) Has(s SlotsE) bool { return inst&s == s }

// DensityE is the card size step.
type DensityE uint8

const (
	DensityMedium DensityE = iota // the zero value
	DensitySmall
	DensityLarge
)

// AspectE is the hero box's aspect ratio.
type AspectE uint8

const (
	Aspect16x9 AspectE = iota // the zero value
	Aspect4x3
	Aspect1x1
)

// Model is one page of cards, one slice per slot over card ordinals. A text
// slice is either nil (the slot is not declared) or Count long; an empty
// string is a card that leaves a declared slot unfilled. [Model.Validate]
// checks the invariants.
//
// Strings are drawn as given apart from a display cut to the slot's
// estimated room, and shown whole on hover — so the host bounds them, with
// [OneLine] and [Lines], before they enter the model.
type Model struct {
	// Count is the number of cards.
	Count int
	// Slots is what the page declares. A text slot's bit and its slice go
	// together; SlotsHero has no slice (the hero is [Input.Hero]'s), and
	// SlotsBody may be served by [Input.Body] over the Body text.
	Slots SlotsE

	Overline []string
	Title    []string
	Subtitle []string
	Body     []string
	Footer   []string

	// Tone is the accent edge's colour per card; nil for none, and a zero
	// Color for a card without one.
	Tone []color.Color

	// The facts of card i are FactLabel[FactOff[i]:FactOff[i+1]] with their
	// FactValue and FactColor — a ragged co-array, the leeway
	// values-plus-offsets idiom. len(FactOff) is Count+1 when SlotsFacts is
	// declared. FactColor may be nil; a zero Color is the default text
	// colour. FactMore, when not nil, counts facts the host already left
	// out, added to the "+k more" the widget shows for the ones that do not
	// fit.
	FactOff   []int32
	FactLabel []string
	FactValue []string
	FactColor []color.Color
	FactMore  []int32

	// The tags of card i are Tag[TagOff[i]:TagOff[i+1]]; len(TagOff) is
	// Count+1 when SlotsTags is declared.
	TagOff []int32
	Tag    []string
}

// Validate checks the slice lengths against Count and Slots. A model that
// fails it is not drawn; Render shows the reason instead.
func (inst *Model) Validate() error {
	n := inst.Count
	if n < 0 {
		return eh.Errorf("cardgrid: negative Count %d", n)
	}
	text := []struct {
		slot SlotsE
		name string
		s    []string
	}{
		{SlotsOverline, "Overline", inst.Overline},
		{SlotsTitle, "Title", inst.Title},
		{SlotsSubtitle, "Subtitle", inst.Subtitle},
		{SlotsBody, "Body", inst.Body},
		{SlotsFooter, "Footer", inst.Footer},
	}
	for _, t := range text {
		switch {
		case inst.Slots.Has(t.slot) && len(t.s) != n:
			return eh.Errorf("cardgrid: %s has %d entries for %d cards", t.name, len(t.s), n)
		case !inst.Slots.Has(t.slot) && len(t.s) != 0:
			return eh.Errorf("cardgrid: %s is filled but its slot is not declared", t.name)
		}
	}
	if inst.Tone != nil && len(inst.Tone) != n {
		return eh.Errorf("cardgrid: Tone has %d entries for %d cards", len(inst.Tone), n)
	}
	if inst.Slots.Has(SlotsFacts) {
		if err := validateRagged("Fact", inst.FactOff, len(inst.FactLabel), n); err != nil {
			return err
		}
		if len(inst.FactValue) != len(inst.FactLabel) {
			return eh.Errorf("cardgrid: %d fact values for %d labels", len(inst.FactValue), len(inst.FactLabel))
		}
		if inst.FactColor != nil && len(inst.FactColor) != len(inst.FactLabel) {
			return eh.Errorf("cardgrid: %d fact colours for %d labels", len(inst.FactColor), len(inst.FactLabel))
		}
		if inst.FactMore != nil && len(inst.FactMore) != n {
			return eh.Errorf("cardgrid: FactMore has %d entries for %d cards", len(inst.FactMore), n)
		}
	}
	if inst.Slots.Has(SlotsTags) {
		if err := validateRagged("Tag", inst.TagOff, len(inst.Tag), n); err != nil {
			return err
		}
	}
	return nil
}

func validateRagged(name string, off []int32, values int, n int) error {
	if len(off) != n+1 {
		return eh.Errorf("cardgrid: %sOff has %d entries for %d cards", name, len(off), n)
	}
	if off[0] != 0 || int(off[n]) != values {
		return eh.Errorf("cardgrid: %sOff spans [%d, %d) for %d values", name, off[0], off[n], values)
	}
	for i := range n {
		if off[i] > off[i+1] {
			return eh.Errorf("cardgrid: %sOff is not monotone at card %d", name, i)
		}
	}
	return nil
}

// facts returns card i's fact range and the host's own left-out count.
func (inst *Model) facts(i int) (lo, hi int32, more int32) {
	if !inst.Slots.Has(SlotsFacts) {
		return
	}
	lo, hi = inst.FactOff[i], inst.FactOff[i+1]
	if inst.FactMore != nil {
		more = inst.FactMore[i]
	}
	return
}

// tags returns card i's tags.
func (inst *Model) tags(i int) []string {
	if !inst.Slots.Has(SlotsTags) {
		return nil
	}
	return inst.Tag[inst.TagOff[i]:inst.TagOff[i+1]]
}

// State is what survives frames. The zero value is usable: nothing selected,
// medium density, a 16:9 hero.
type State struct {
	sel     int32 // ordinal+1; 0 none
	reveal  int32 // ordinal+1; 0 none
	density DensityE
	aspect  AspectE

	paneW      float32
	keyFrameID uint64
	focused    bool

	// The scroll viewport as last frame's probes saw it, in screen
	// coordinates: its top and height, and the grid content's top, which
	// sits above the viewport by the scroll offset. haveView is false until
	// both probes have answered, and every card is drawn until then.
	viewTop, viewH, contentTop float32
	haveView                   bool
}

// Focused reports whether the grid held the keyboard focus last frame — when
// the arrow keys move the selection.
func (inst *State) Focused() bool { return inst.focused }

// Selected is the selected card's ordinal, -1 for none.
func (inst *State) Selected() int32 { return inst.sel - 1 }

// SetSelected selects a card by ordinal; a negative ordinal clears. It does
// not scroll — see [State.Reveal].
func (inst *State) SetSelected(ordinal int32) {
	if ordinal < 0 {
		inst.sel = 0
		return
	}
	inst.sel = ordinal + 1
}

// Reveal asks the next Render to scroll a card into view.
func (inst *State) Reveal(ordinal int32) {
	if ordinal < 0 {
		inst.reveal = 0
		return
	}
	inst.reveal = ordinal + 1
}

// Density is the card size step.
func (inst *State) Density() DensityE { return inst.density }

// SetDensity sets the card size step.
func (inst *State) SetDensity(d DensityE) { inst.density = d }

// Aspect is the hero box's aspect ratio.
func (inst *State) Aspect() AspectE { return inst.aspect }

// SetAspect sets the hero box's aspect ratio.
func (inst *State) SetAspect(a AspectE) { inst.aspect = a }

// Box is the room a host block is given, in points.
type Box struct {
	W, H float32
}

// Block is a host-drawn hero or body. Exactly one of the three shapes is
// live: Pending (the host is still building it — the widget draws a
// skeleton), Reason (it cannot be shown — the widget says why, where the
// content would have been), or Render.
type Block struct {
	// W and H are the content's size inside the box; zero takes the whole
	// box. A smaller content is centred, so a contained image reports its
	// fitted size and need not centre itself.
	W, H float32
	// Render runs at draw time inside a clipped rect of that size and must
	// scope its own widget ids. Its interactive widgets keep their clicks;
	// everything else lets the click through to the card. A widget that
	// senses clicks without being a control of its own — an image does —
	// would swallow the card's, so Render reports such a click and the card
	// is selected as if it had received it.
	Render func() (clicked bool)
	// Pending marks a block the host has not built yet.
	Pending bool
	// Reason says why there is nothing to render.
	Reason string
}

// Input is the per-frame render request.
type Input struct {
	// Ids is the host's widget id stack; Render opens IdScope(ScopeKey)
	// around the whole grid, so two instances under one parent need
	// distinct keys.
	Ids      *c.WidgetIdStack
	ScopeKey string
	Model    *Model
	State    *State
	// Hero draws a card's hero; nil, or a false second result, draws the
	// placeholder. Consulted only when the model declares SlotsHero.
	Hero func(ordinal int, box Box) (Block, bool)
	// Body draws a card's body in the host's own way; nil, or a false
	// second result, draws Model.Body wrapped. Consulted only when the
	// model declares SlotsBody.
	Body func(ordinal int, box Box) (Block, bool)
	// FillHost tells Render its host already bounds its height, so the grid
	// fills that rect rather than flooring to a minimum. Dock-tab leaves set
	// it; an unbounded gallery scroll host leaves it false.
	FillHost bool
}

// Result is what the frame's input did.
type Result struct {
	// Clicked is the ordinal of the card clicked this frame, -1 for none.
	// The selection has already moved to it.
	Clicked int32
	// Moved is the ordinal the keyboard moved the selection to this frame,
	// -1 for none. The selection has already moved and a reveal is pending.
	Moved int32
	// Past is a keyboard move the page cannot hold, as an ordinal delta from
	// the selection: -1 or +1 for ← or → at the page's ends, ∓Cols for ↑ on
	// the first row or ↓ on the last, ∓Count for PageUp and PageDown; 0 for
	// none. The selection has not moved — the host, which owns the paging,
	// turns the page and selects the card the delta lands on.
	Past int32
	// Toggled is the ordinal Space was pressed on, -1 for none — the host's
	// cue to play or pause a hero that can.
	Toggled int32
}
