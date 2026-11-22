package decision

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// defaultScopeKey scopes a decision whose host gave no ScopeKey.
const defaultScopeKey = "decision"

// Render draws the decision where it is called.
func Render(in Input) (res Result) {
	if in.Ids == nil {
		// No stack to open a scope on: the message is all that is drawn.
		res.Err = in.Validate()
		c.Label(res.Err.Error()).Wrap().Send()
		return
	}
	scopeKey := in.ScopeKey
	if scopeKey == "" {
		scopeKey = defaultScopeKey
	}
	for range c.IdScope(in.Ids.PrepareStr(scopeKey)) {
		if res.Err = in.Validate(); res.Err != nil {
			c.Label(res.Err.Error()).Wrap().Send()
			return
		}
		if in.Unframed {
			res.Chosen = in.body()
		} else {
			for range c.Frame(in.Ids.PrepareStr("frame")).PresetGroup().KeepIter() {
				res.Chosen = in.body()
			}
		}
		res.Has = res.Chosen != ""
	}
	return
}

// body draws everything inside the frame, in the order the package doc
// gives, and returns the key of the option clicked.
func (in Input) body() (chosen string) {
	d := styletokens.ActiveDensity()
	c.LabelAtoms(c.Atoms().BeginRichText(in.Title).Strong().End().Keep()).Wrap().Send()
	if in.Subject != "" {
		c.Label(in.Subject).Wrap().Send()
	}
	// What follows from the answer is what the person consents to: body
	// text, wrapped, never small or weak (ADR-0404 SD3).
	for _, s := range in.Follows {
		c.Label(s).Wrap().Send()
	}
	if len(in.Notes) > 0 {
		for range c.IdScope(in.Ids.PrepareStr("notes")) {
			for i, n := range in.Notes {
				in.note(i, n, d)
			}
		}
	}
	if in.Form != nil {
		for range c.IdScope(in.Ids.PrepareStr("form")) {
			in.Form()
		}
	}
	c.AddSpace(styletokens.GapInline(d))
	for range c.IdScope(in.Ids.PrepareStr("options")) {
		// Wrapped, so a row wider than the host breaks onto a second line
		// instead of running past the edge.
		for range c.HorizontalWrapped().KeepIter() {
			for _, o := range in.Options {
				if in.option(o) {
					chosen = o.Key
				}
			}
		}
	}
	for _, o := range in.Options {
		if o.Disabled {
			c.Label(o.Why).Wrap().Send()
		}
	}
	if in.Detail != nil {
		c.AddSpace(styletokens.GapInline(d))
		for range c.IdScope(in.Ids.PrepareStr("detail")) {
			in.Detail()
		}
	}
	return
}

// note draws one note as body text behind a bar in its tone.
func (in Input) note(i int, n Note, d styletokens.DensityE) {
	bar := styletokens.StrokeStrong
	for range c.Frame(in.Ids.PrepareSeq(uint64(i))).
		EdgeBarLeft(bar, color.Hex(n.Tone.Fill().AsHex())).
		InnerMarginSides(bar+styletokens.PaddingDefault(d), 0, styletokens.StrokeHair, styletokens.StrokeHair).
		KeepIter() {
		c.Label(n.Text).Wrap().Send()
	}
}

// option draws one option's button and reports a click on an enabled one.
func (in Input) option(o Option) (clicked bool) {
	text := o.Label
	if o.Icon != "" {
		// U+00A0 keeps the glyph and the label on one line, as badge does.
		text = o.Icon + " " + o.Label
	}
	draw := func() {
		for range c.EnabledUi(!o.Disabled).KeepIter() {
			clicked = c.Button(in.Ids.PrepareStr(o.Key), c.Atoms().Text(text).Keep()).
				Kind(kindOf(in.Origin, o, in.cautioned())).
				SendResp().HasPrimaryClicked() && !o.Disabled
		}
	}
	if o.Tooltip != "" {
		// The hover scope sits outside the disabled scope: egui gives a
		// disabled widget no hover text.
		for range c.HoverText(o.Tooltip).KeepIter() {
			draw()
		}
		return
	}
	draw()
	return
}
