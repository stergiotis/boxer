package cbordiag

import (
	"errors"
	"strconv"

	"github.com/zeebo/xxh3"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/semistructured/cbor/diag"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

var (
	mutedFg       = color.Hex(styletokens.NeutralTextSecondary.AsHex())
	errorFg       = color.Hex(styletokens.ErrorDefault.AsHex())
	transparentBg = color.Transparent
	copyAtoms     = c.Atoms().Text(icons.PhCopy).Keep()
)

// Input is one frame's view.
type Input struct {
	// Ids is the host's widget id stack. Render opens its own IdScope under
	// it, so two views in one frame need only differ in ScopeKey.
	Ids *c.WidgetIdStack
	// ScopeKey names this view within the host's id space; empty uses
	// "cbordiag".
	ScopeKey string
	// Item is the CBOR to show.
	Item []byte
	// Options drive the printer; Options.Compact is overridden by the State's
	// toggle.
	Options diag.Options
	// State is the host's memo and toggle, one per place a view is drawn.
	// Required.
	State *State
	// HideToolbar drops the row above the notation. Without it the compact
	// toggle is whatever State.Compact says and copying is the host's.
	HideToolbar bool
}

// Result is what one Render reports.
type Result struct {
	// Copied is true on the frame the copy button put the notation on the
	// clipboard.
	Copied bool
	// Err is set when Input.State is nil; the widget draws the message in
	// place. The printer's own verdict is [State.Err].
	Err error
}

// State is the caller's: one per place a view is drawn. Compact is the
// toggle the toolbar drives and Verdict an optional line the host sets —
// play puts the canonical-order check's result there. The rest is the memo
// of the last rendering.
type State struct {
	Compact bool
	Verdict string

	have bool
	key  uint64
	job  typed.RetainedFffiHolderTyped[c.CodeViewJobS]
	text string
	err  error
}

// Invalidate drops the memo, so the next Render rebuilds. Needed only when
// something the key does not see changes: the Annotate hook.
func (st *State) Invalidate() { st.have = false }

// Text is the plain notation of the last rendering — what the copy button
// puts on the clipboard.
func (st *State) Text() string { return st.text }

// Err is the printer's verdict on the last rendering: nil for well-formed
// input, else why it stopped.
func (st *State) Err() error { return st.err }

// keyOf fingerprints the bytes and every option that moves the rendering.
// The Annotate hook is not a value and is not part of it.
func keyOf(item []byte, opts diag.Options) uint64 {
	h := xxh3.New()
	_, _ = h.Write(item)
	_, _ = h.Write([]byte{0})
	_, _ = h.WriteString(opts.Indent)
	_, _ = h.Write([]byte{0})
	_, _ = h.WriteString(strconv.Itoa(opts.Width))
	_, _ = h.Write([]byte{0})
	_, _ = h.WriteString(strconv.Itoa(opts.BytesFold))
	var flags byte
	if opts.Compact {
		flags |= 1
	}
	if opts.FloatPrecision {
		flags |= 2
	}
	if opts.TagComments {
		flags |= 4
	}
	if opts.Sequence {
		flags |= 8
	}
	_, _ = h.Write([]byte{0, flags})
	return h.Sum64()
}

// prepare rebuilds the memo when the bytes or the options moved.
func (st *State) prepare(item []byte, opts diag.Options) {
	opts.Compact = st.Compact
	k := keyOf(item, opts)
	if st.have && st.key == k {
		return
	}
	spans, err := diag.Print(item, opts)
	st.text = diag.Text(spans)
	st.err = err
	st.job = codeview.BuildCborDiagSpans(spans)
	st.key = k
	st.have = true
}

// Render draws the view at the current ui scope, inside one IdScope under
// Input.Ids: the toolbar, then the notation. A nil Ids draws nothing.
func Render(in Input) (res Result) {
	if in.Ids == nil {
		return
	}
	scopeKey := in.ScopeKey
	if scopeKey == "" {
		scopeKey = "cbordiag"
	}
	for range c.IdScope(in.Ids.PrepareStr(scopeKey)) {
		res = in.render()
	}
	return
}

func (in Input) render() (res Result) {
	st := in.State
	if st == nil {
		res.Err = errors.New("cbordiag: Input.State is nil")
		for rt := range c.RichTextLabel(res.Err.Error()) {
			rt.Small().Weak()
		}
		return
	}
	st.prepare(in.Item, in.Options)
	if !in.HideToolbar {
		res.Copied = in.renderToolbar(st, len(in.Item))
	}
	c.CodeView(in.Ids.PrepareStr("notation"), st.job).Send()
	return
}

func (in Input) renderToolbar(st *State, nBytes int) (copied bool) {
	ids := in.Ids
	density := styletokens.ActiveDensity()
	for range c.HorizontalTop().KeepIter() {
		c.LabelAtoms(c.Atoms().
			BeginRichTextColored(mutedFg, transparentBg, strconv.Itoa(nBytes)+" bytes").Small().End().
			Keep()).Selectable(false).Send()
		c.AddSpace(styletokens.GapInline(density))
		c.Checkbox(ids.PrepareStr("compact"), st.Compact, "compact").SendRespVal(&st.Compact)
		c.AddSpace(styletokens.GapInline(density))
		for range c.HoverText("copy the diagnostic notation").KeepIter() {
			if c.Button(ids.PrepareStr("copy"), copyAtoms).Small().SendResp().HasPrimaryClicked() {
				c.CopyTextToClipboard(st.text)
				copied = true
			}
		}
		if st.Verdict != "" {
			c.AddSpace(styletokens.GapInline(density))
			c.LabelAtoms(c.Atoms().
				BeginRichTextColored(mutedFg, transparentBg, st.Verdict).Small().End().
				Keep()).Selectable(false).Truncate().Send()
		}
		if st.err != nil {
			c.AddSpace(styletokens.GapInline(density))
			c.LabelAtoms(c.Atoms().
				BeginRichTextColored(errorFg, transparentBg, icons.PhWarning+" "+st.err.Error()).Small().End().
				Keep()).Selectable(false).Truncate().Send()
		}
	}
	return
}
