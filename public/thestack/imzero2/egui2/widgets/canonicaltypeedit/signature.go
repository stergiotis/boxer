package canonicaltypeedit

import (
	"errors"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/canonicaltypesummary"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

// Separator bytes mirroring canonicaltypes.GroupSeparator ("-") and
// SignatureSeparator ("_"): '-' keeps the next element in the same group, '_'
// starts a new group.
const (
	grpSepByte byte = '-'
	sigSepByte byte = '_'
)

// ErrNeedsIdsSignatureAndState is Result.Err when SignatureInput.Ids,
// SignatureInput.Model or SignatureInput.State is nil.
var ErrNeedsIdsSignatureAndState = errors.New("canonicaltypeedit: SignatureInput.Ids, SignatureInput.Model and SignatureInput.State are required")

// sigElem is one element of a signature: a single-primitive value plus the
// separator to the next element (ignored for the last element).
type sigElem struct {
	prim *Model
	sep  byte // grpSepByte or sigSepByte
}

// SignatureModel is the host-owned value of a canonical-type signature: a
// sequence of primitive elements joined by '-'/'_' separators (ADR-0067
// group/signature cut). The single-primitive [Model] is each element's value.
// Which element is selected for editing is [SignatureState]'s.
type SignatureModel struct {
	elems []*sigElem

	// Derived cache, refreshed by rebuild from the elements + separators.
	canonical string
	ast       canonicaltypes.AstNodeI
	valid     bool
}

// SignatureState is the host-owned UI state of one signature editor: the
// selected chip, one [State] per element (the bar and form of each chip's
// editor, kept in step with the model's elements by RenderSignature), and
// the embedded summary chip's state. The zero value selects the first
// element. It must live at a stable address across frames, like [State].
type SignatureState struct {
	sel     int
	elems   []*State
	summary canonicaltypesummary.State
}

// Selected reports the index of the chip being edited.
func (st *SignatureState) Selected() int { return st.sel }

// Element returns the editor state of element i, or nil when the state has
// not yet been rendered against a model with that many elements.
func (st *SignatureState) Element(i int) *State {
	if i < 0 || i >= len(st.elems) {
		return nil
	}
	return st.elems[i]
}

// sync brings the per-element states in step with the model's elements: a
// grown model gets fresh states, a shrunk one drops the tail, and the
// selection is clamped. Element states are pointers, so a slice that grows
// leaves the bars' bound buffers where they are.
func (st *SignatureState) sync(sm *SignatureModel) {
	for len(st.elems) < len(sm.elems) {
		st.elems = append(st.elems, &State{})
	}
	if len(st.elems) > len(sm.elems) {
		st.elems = st.elems[:len(sm.elems)]
	}
	if st.sel >= len(sm.elems) {
		st.sel = len(sm.elems) - 1
	}
	if st.sel < 0 {
		st.sel = 0
	}
}

// NewSignatureModel returns a signature editor seeded with a single `u32`
// element.
func NewSignatureModel() (sm *SignatureModel) {
	sm = &SignatureModel{
		elems: []*sigElem{{prim: NewModel(), sep: grpSepByte}},
	}
	sm.rebuild()
	return
}

// Canonical returns the assembled signature string.
func (sm *SignatureModel) Canonical() string { return sm.canonical }

// Valid reports whether every element and the assembled node are valid.
func (sm *SignatureModel) Valid() bool { return sm.valid }

// Node returns the assembled AST: a bare primitive for one scalar element, a
// group for one '-'-joined run, or a signature once a '_' separator splits it.
func (sm *SignatureModel) Node() canonicaltypes.AstNodeI { return sm.ast }

// Len is the number of elements.
func (sm *SignatureModel) Len() int { return len(sm.elems) }

// SetCanonical seeds the editor from a signature string, splitting on '_' into
// groups and each group on '-' into primitive elements. Unparseable primitives
// fall back to the element default (a no-op seed) rather than failing the whole
// load.
func (sm *SignatureModel) SetCanonical(s string) {
	var elems []*sigElem
	groups := strings.SplitSeq(s, canonicaltypes.SignatureSeparator)
	for g := range groups {
		prims := strings.Split(g, canonicaltypes.GroupSeparator)
		for pi, p := range prims {
			m := NewModel()
			m.SetCanonical(p)
			sep := grpSepByte
			if pi == len(prims)-1 {
				// Last primitive of a group: the boundary to the next group is
				// '_' (ignored outright for the final group's final element).
				sep = sigSepByte
			}
			elems = append(elems, &sigElem{prim: m, sep: sep})
		}
	}
	if len(elems) == 0 {
		return
	}
	sm.elems = elems
	sm.rebuild()
}

// rebuild reassembles the canonical string and AST from the elements and their
// separators, grouping '-'-joined runs and splitting on '_'.
func (sm *SignatureModel) rebuild() {
	var b strings.Builder
	for i, e := range sm.elems {
		b.WriteString(e.prim.canonical)
		if i < len(sm.elems)-1 {
			b.WriteByte(e.sep)
		}
	}
	sm.canonical = b.String()

	var groups []canonicaltypes.AstNodeI
	var cur []canonicaltypes.PrimitiveAstNodeI
	flush := func() {
		switch len(cur) {
		case 0:
		case 1:
			groups = append(groups, cur[0])
		default:
			groups = append(groups, canonicaltypes.NewGroupAstNode(cur))
		}
		cur = nil
	}
	for i, e := range sm.elems {
		cur = append(cur, e.prim.ast)
		if i < len(sm.elems)-1 && e.sep == sigSepByte {
			flush()
		}
	}
	flush()

	switch len(groups) {
	case 0:
		sm.ast = nil
		sm.valid = false
	case 1:
		sm.ast = groups[0]
		sm.valid = groups[0].IsValid()
	default:
		sig := canonicaltypes.NewSignatureAstNode(groups)
		sm.ast = sig
		sm.valid = sig.IsValid()
	}
}

// removeAt drops element i from the model and its editor state from st
// (clamping the selection); a no-op when it would empty the editor.
func (sm *SignatureModel) removeAt(st *SignatureState, i int) {
	if i < 0 || i >= len(sm.elems) || len(sm.elems) <= 1 {
		return
	}
	sm.elems = append(sm.elems[:i], sm.elems[i+1:]...)
	if i < len(st.elems) {
		st.elems = append(st.elems[:i], st.elems[i+1:]...)
	}
	if st.sel >= len(sm.elems) {
		st.sel = len(sm.elems) - 1
	}
}

// moveSelected swaps the selected element's content with its neighbour `delta`
// steps away and follows it with the selection. Only the primitive content
// moves — the separators stay in their positional gap slots, so a chip slides
// through the existing `-`/`_` structure rather than dragging its separator
// along (e.g. moving `s` left in `u32-s_vc` yields `s-u32_vc`, not `s_u32-vc`).
// The element's editor state moves with it. A no-op at the ends.
func (sm *SignatureModel) moveSelected(st *SignatureState, delta int) {
	j := st.sel + delta
	if st.sel < 0 || st.sel >= len(sm.elems) || j < 0 || j >= len(sm.elems) {
		return
	}
	sm.elems[st.sel].prim, sm.elems[j].prim = sm.elems[j].prim, sm.elems[st.sel].prim
	if st.sel < len(st.elems) && j < len(st.elems) {
		st.elems[st.sel], st.elems[j] = st.elems[j], st.elems[st.sel]
	}
	st.sel = j
}

// appendElem grows the signature by a default element and selects it.
func (sm *SignatureModel) appendElem(st *SignatureState) {
	sm.elems = append(sm.elems, &sigElem{prim: NewModel(), sep: grpSepByte})
	st.elems = append(st.elems, &State{})
	st.sel = len(sm.elems) - 1
}

// SignatureInput is one frame's declaration of a signature editor. It is a
// type of its own because its Model and State are the signature's, not a
// primitive's.
type SignatureInput struct {
	// Ids is the host's widget id stack. RenderSignature opens its own
	// IdScope under it, so two editors in one host need only differ in
	// ScopeKey.
	Ids *c.WidgetIdStack
	// ScopeKey names this editor within the host's id space; empty uses
	// "canonicaltypeedit".
	ScopeKey string
	// Model is the signature being edited; edits mutate it in place.
	// Required.
	Model *SignatureModel
	// State is the editor's host-owned UI state. Required, and at a stable
	// address across frames: the bars bind to it.
	State *SignatureState
}

func (in SignatureInput) scopeKey() string {
	if in.ScopeKey == "" {
		return defaultScopeKey
	}
	return in.ScopeKey
}

// RenderSignature draws the chip strip, the selected element's editor, and
// the assembled signature status. Call once per frame; every widget id is
// derived under one IdScope keyed by ScopeKey. All edits mutate Input.Model
// and Input.State in place.
func RenderSignature(in SignatureInput) (res Result) {
	if in.Ids == nil || in.Model == nil || in.State == nil {
		res.Err = ErrNeedsIdsSignatureAndState
		for rt := range c.RichTextLabel(res.Err.Error()) {
			rt.Small().Weak()
		}
		return
	}
	ids, sm, st := in.Ids, in.Model, in.State
	st.sync(sm)
	for range c.IdScope(ids.PrepareStr(in.scopeKey())) {
		for range c.Vertical().KeepIter() {
			c.UiSetMinWidth(editorMinWidth)
			var changed bool
			// Progressive disclosure: the chip strip (selector + separators +
			// remove) appears only once there is more than one element, so the
			// common single-primitive case stays a bare bar+form editor with no
			// sequence chrome.
			if len(sm.elems) > 1 {
				changed = renderChipStrip(ids, sm, st)
				c.Separator().Send()
			}
			if st.sel >= 0 && st.sel < len(sm.elems) {
				// Each element edits under its own id scope so switching the
				// selected chip swaps the bar/form widget-id namespace (and thus
				// the displayed buffer) cleanly.
				for range c.IdScope(ids.PrepareStr("elem")) {
					for range c.IdScope(ids.PrepareSeq(uint64(st.sel))) {
						if renderEditBody(ids, sm.elems[st.sel].prim, st.elems[st.sel]) {
							changed = true
						}
					}
				}
			}
			if len(sm.elems) == 1 {
				// A single, unobtrusive affordance to grow the lone primitive
				// into a group/signature on demand — the chip strip then takes
				// over from the next frame (and collapses back on remove).
				c.AddSpace(styletokens.PaddingInner(styletokens.ActiveDensity()))
				if c.Button(ids.PrepareStr("grow"), c.Atoms().Text("+ element").Keep()).
					Small().SendResp().HasPrimaryClicked() {
					sm.appendElem(st)
					changed = true
				}
			}
			if changed {
				sm.rebuild()
			}
			res.Changed = changed
			c.Separator().Send()
			// Name the readout for what it currently is: a single primitive
			// until the editor grows into a multi-element group/signature.
			label := "live type"
			if len(sm.elems) > 1 {
				label = "live signature"
			}
			renderStatus(ids, sm.canonical, &st.summary, label)
		}
	}
	return
}

// renderChipStrip draws the element chips (click to select), the per-gap
// separator toggles ('-'/'_'), an add button, and a remove button for the
// selected element. Returns whether the structure changed (needs reassembly).
func renderChipStrip(ids *c.WidgetIdStack, sm *SignatureModel, st *SignatureState) (structureChanged bool) {
	removeReq := -1
	for range c.Horizontal().KeepIter() {
		for range c.IdScope(ids.PrepareStr("chips")) {
			for i, e := range sm.elems {
				for range c.IdScope(ids.PrepareSeq(uint64(i))) {
					label := e.prim.canonical
					if label == "" {
						label = "?"
					}
					selector.RadioValue(ids.PrepareStr("chip"), &st.sel, i).
						Style(selector.StyleSelectable).Text(label).Send()
					if i < len(sm.elems)-1 {
						if c.Button(ids.PrepareStr("sep"), c.Atoms().Text(string(e.sep)).Keep()).
							Small().SendResp().HasPrimaryClicked() {
							if e.sep == grpSepByte {
								e.sep = sigSepByte
							} else {
								e.sep = grpSepByte
							}
							structureChanged = true
						}
					}
				}
			}
		}
		c.AddSpace(styletokens.GapItems(styletokens.ActiveDensity()))
		if c.Button(ids.PrepareStr("add-elem"), c.Atoms().Text("+").Keep()).
			SendResp().HasPrimaryClicked() {
			sm.appendElem(st)
			structureChanged = true
		}
		// Reorder the selected element through the positional separator gaps.
		// The buttons grey out at the ends; the click guard also rejects an
		// out-of-range move so a greyed button can never act.
		leftDisabled := st.sel <= 0
		for range c.Scope().KeepIter() {
			if leftDisabled {
				c.UiDisable()
			}
			if c.Button(ids.PrepareStr("move-left"), c.Atoms().Text(icons.PhCaretLeft).Keep()).
				SendResp().HasPrimaryClicked() && !leftDisabled {
				sm.moveSelected(st, -1)
				structureChanged = true
			}
		}
		rightDisabled := st.sel >= len(sm.elems)-1
		for range c.Scope().KeepIter() {
			if rightDisabled {
				c.UiDisable()
			}
			if c.Button(ids.PrepareStr("move-right"), c.Atoms().Text(icons.PhCaretRight).Keep()).
				SendResp().HasPrimaryClicked() && !rightDisabled {
				sm.moveSelected(st, 1)
				structureChanged = true
			}
		}
		if c.Button(ids.PrepareStr("rm-elem"), c.Atoms().Text("× remove").Keep()).
			SendResp().HasPrimaryClicked() {
			removeReq = st.sel
		}
	}
	if removeReq >= 0 {
		sm.removeAt(st, removeReq)
		structureChanged = true
	}
	return
}
