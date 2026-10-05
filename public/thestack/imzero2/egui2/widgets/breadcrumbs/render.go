package breadcrumbs

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// defaultScopeKey scopes a trail whose host gave no ScopeKey.
const defaultScopeKey = "breadcrumbs"

// Input is one frame's declaration of a trail.
type Input struct {
	// Ids is the host's id stack; the trail opens one scope under it.
	Ids *c.WidgetIdStack
	// ScopeKey tells two trails under one host apart; "" takes a default.
	ScopeKey string
	// Model is the trail.
	Model *Model
	// State is the host-held current item. Required.
	State *State
	// Small draws the items as small buttons, for a trail in dense chrome.
	Small bool
}

// Result is what the frame's input did.
type Result struct {
	// Clicked is the item clicked, -1 for none. A click on the current item
	// or a locked one is not reported.
	Clicked int32
	// Err is a structurally broken Input, drawn in place of the trail.
	Err error
}

// Render draws the trail where it is called.
func Render(in Input) (res Result) {
	res.Clicked = -1
	scopeKey := in.ScopeKey
	if scopeKey == "" {
		scopeKey = defaultScopeKey
	}
	for range c.IdScope(in.Ids.PrepareStr(scopeKey)) {
		switch {
		case in.Model == nil:
			res.Err = eb.Build().Errorf("breadcrumbs: Input.Model is nil")
		case in.State == nil:
			res.Err = eb.Build().Errorf("breadcrumbs: Input.State is nil")
		default:
			res.Err = in.Model.Validate()
		}
		if res.Err != nil {
			c.Label(res.Err.Error()).Send()
			return
		}
		n := in.Model.Len()
		cur := in.State.resolve(n)
		for range c.HorizontalTop().KeepIter() {
			for i := range n {
				if i > 0 {
					weak(icons.PhCaretRight)
				}
				for range c.IdScope(in.Ids.PrepareSeq(uint64(i))) {
					if in.item(i, cur) {
						res.Clicked = int32(i)
					}
				}
			}
		}
		if res.Clicked >= 0 {
			in.State.SetCurrent(res.Clicked)
		}
	}
	return
}

// item draws one item and reports a click that moves the current item.
func (in Input) item(i int, cur int) (clicked bool) {
	m := in.Model
	label := m.Labels[i]
	locked := m.locked(i)
	if i == cur {
		// The current item is where the host is, whatever stood in the way
		// of reaching it.
		locked = ""
	}
	text := c.Atoms().Text(label).Keep()
	switch {
	case i == cur:
		text = c.Atoms().BeginRichText(label).Strong().End().Keep()
	case locked != "":
		text = c.Atoms().BeginRichText(label).Weak().End().Keep()
	}
	if locked != "" {
		// The hover scope sits outside the disabled row: egui gives a
		// disabled widget no hover text, but the scope's own overlay is
		// registered in this enabled ui.
		for range c.HoverText(locked).KeepIter() {
			for range c.HorizontalTop().KeepIter() {
				in.marks(i, cur)
				c.UiDisable()
				in.button(text)
			}
		}
		return
	}
	for range c.HorizontalTop().KeepIter() {
		in.marks(i, cur)
		clicked = in.button(text) && i != cur
	}
	return
}

// marks draws what precedes an item's label: the tick of a done item that
// is not current, then its icon.
func (in Input) marks(i int, cur int) {
	if in.Model.done(i) && i != cur {
		weak(icons.PhCheck)
	}
	if icon := in.Model.icon(i); icon != "" {
		c.Label(icon).Selectable(false).Send()
	}
}

func (in Input) button(text typed.RetainedFffiHolderTyped[c.AtomsS]) (clicked bool) {
	b := c.Button(in.Ids.PrepareStr("item"), text).Frame(false)
	if in.Small {
		b = b.Small()
	}
	return b.SendResp().HasPrimaryClicked()
}

func weak(s string) {
	c.LabelAtoms(c.Atoms().BeginRichText(s).Weak().End().Keep()).Selectable(false).Send()
}
