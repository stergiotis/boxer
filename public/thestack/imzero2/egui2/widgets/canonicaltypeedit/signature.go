package canonicaltypeedit

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/canonicaltypesummary"
)

// Separator bytes mirroring canonicaltypes.GroupSeparator ("-") and
// SignatureSeparator ("_"): '-' keeps the next element in the same group, '_'
// starts a new group.
const (
	grpSepByte byte = '-'
	sigSepByte byte = '_'
)

// Widget-id seq bases for the chip strip; kept distinct so chips, separator
// toggles, the per-element edit scope, and the embedded summary do not collide.
const (
	chipSeqBase   uint64 = 0xC4190000
	sepSeqBase    uint64 = 0xC4191000
	editScopeBase uint64 = 0xC4192000
	sigSummarySeq uint64 = 0xC4193001
)

// sigElem is one element of a signature: a single-primitive editor plus the
// separator to the next element (ignored for the last element).
type sigElem struct {
	prim *Model
	sep  byte // grpSepByte or sigSepByte
}

// SignatureModel is the caller-owned editor for a canonical-type signature: a
// chip strip of primitive elements joined by '-'/'_' separators, with one
// shared bar+form editing the selected chip (ADR-0067 group/signature cut).
// The single-primitive [Model] is reused as each element's editor.
type SignatureModel struct {
	elems []*sigElem
	sel   int

	// Derived cache, refreshed by rebuild from the elements + separators.
	canonical string
	ast       canonicaltypes.AstNodeI
	valid     bool
}

// NewSignatureModel returns a signature editor seeded with a single `u32`
// element.
func NewSignatureModel() (sm *SignatureModel) {
	sm = &SignatureModel{
		elems: []*sigElem{{prim: NewModel(), sep: grpSepByte}},
		sel:   0,
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

// SetCanonical seeds the editor from a signature string, splitting on '_' into
// groups and each group on '-' into primitive elements. Unparseable primitives
// fall back to the element default (a no-op seed) rather than failing the whole
// load.
func (sm *SignatureModel) SetCanonical(s string) {
	var elems []*sigElem
	groups := strings.Split(s, canonicaltypes.SignatureSeparator)
	for _, g := range groups {
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
	sm.sel = 0
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

// removeAt drops element i (clamping the selection); a no-op when it would
// empty the editor.
func (sm *SignatureModel) removeAt(i int) {
	if i < 0 || i >= len(sm.elems) || len(sm.elems) <= 1 {
		return
	}
	sm.elems = append(sm.elems[:i], sm.elems[i+1:]...)
	if sm.sel >= len(sm.elems) {
		sm.sel = len(sm.elems) - 1
	}
}

// moveSelected swaps the selected element's content with its neighbour `delta`
// steps away and follows it with the selection. Only the primitive content
// moves — the separators stay in their positional gap slots, so a chip slides
// through the existing `-`/`_` structure rather than dragging its separator
// along (e.g. moving `s` left in `u32-s_vc` yields `s-u32_vc`, not `s_u32-vc`).
// A no-op at the ends.
func (sm *SignatureModel) moveSelected(delta int) {
	j := sm.sel + delta
	if sm.sel < 0 || sm.sel >= len(sm.elems) || j < 0 || j >= len(sm.elems) {
		return
	}
	sm.elems[sm.sel].prim, sm.elems[j].prim = sm.elems[j].prim, sm.elems[sm.sel].prim
	sm.sel = j
}

// Render draws the chip strip, the selected element's editor, and the assembled
// signature status. Call once per frame; scopeKey scopes every widget id. All
// edits mutate the receiver in place.
func (sm *SignatureModel) Render(ids *c.WidgetIdStack, scopeKey string) {
	for range c.IdScope(ids.PrepareStr(scopeKey)) {
		for range c.Vertical().KeepIter() {
			c.UiSetMinWidth(editorMinWidth)
			var changed bool
			// Progressive disclosure: the chip strip (selector + separators +
			// remove) appears only once there is more than one element, so the
			// common single-primitive case stays a bare bar+form editor with no
			// sequence chrome.
			if len(sm.elems) > 1 {
				changed = sm.renderChipStrip(ids)
				c.Separator().Send()
			}
			if sm.sel >= 0 && sm.sel < len(sm.elems) {
				// Each element edits under its own id scope so switching the
				// selected chip swaps the bar/form widget-id namespace (and thus
				// the displayed buffer) cleanly.
				for range c.IdScope(ids.PrepareSeq(editScopeBase + uint64(sm.sel))) {
					if sm.elems[sm.sel].prim.renderEditBody(ids) {
						changed = true
					}
				}
			}
			if len(sm.elems) == 1 {
				// A single, unobtrusive affordance to grow the lone primitive
				// into a group/signature on demand — the chip strip then takes
				// over from the next frame (and collapses back on remove).
				c.AddSpace(styletokens.PaddingInner(styletokens.DensityFromEnv()))
				if c.Button(ids.PrepareStr("grow"), c.Atoms().Text("+ element").Keep()).
					Small().SendResp().HasPrimaryClicked() {
					sm.elems = append(sm.elems, &sigElem{prim: NewModel(), sep: grpSepByte})
					sm.sel = len(sm.elems) - 1
					changed = true
				}
			}
			if changed {
				sm.rebuild()
			}
			c.Separator().Send()
			sm.renderStatus(ids)
		}
	}
}

// renderChipStrip draws the element chips (click to select), the per-gap
// separator toggles ('-'/'_'), an add button, and a remove button for the
// selected element. Returns whether the structure changed (needs reassembly).
func (sm *SignatureModel) renderChipStrip(ids *c.WidgetIdStack) (structureChanged bool) {
	removeReq := -1
	for range c.Horizontal().KeepIter() {
		for i, e := range sm.elems {
			label := e.prim.canonical
			if label == "" {
				label = "?"
			}
			if c.SelectableLabel(ids.PrepareSeq(chipSeqBase+uint64(i)), i == sm.sel, label).
				SendResp().HasPrimaryClicked() {
				sm.sel = i
			}
			if i < len(sm.elems)-1 {
				if c.Button(ids.PrepareSeq(sepSeqBase+uint64(i)), c.Atoms().Text(string(e.sep)).Keep()).
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
		c.AddSpace(styletokens.GapItems(styletokens.DensityFromEnv()))
		if c.Button(ids.PrepareStr("add-elem"), c.Atoms().Text("+").Keep()).
			SendResp().HasPrimaryClicked() {
			sm.elems = append(sm.elems, &sigElem{prim: NewModel(), sep: grpSepByte})
			sm.sel = len(sm.elems) - 1
			structureChanged = true
		}
		// Reorder the selected element through the positional separator gaps.
		// The buttons grey out at the ends; the click guard also rejects an
		// out-of-range move so a greyed button can never act.
		leftDisabled := sm.sel <= 0
		for range c.Scope().KeepIter() {
			if leftDisabled {
				c.UiDisable()
			}
			if c.Button(ids.PrepareStr("move-left"), c.Atoms().Text(icons.PhCaretLeft).Keep()).
				SendResp().HasPrimaryClicked() && !leftDisabled {
				sm.moveSelected(-1)
				structureChanged = true
			}
		}
		rightDisabled := sm.sel >= len(sm.elems)-1
		for range c.Scope().KeepIter() {
			if rightDisabled {
				c.UiDisable()
			}
			if c.Button(ids.PrepareStr("move-right"), c.Atoms().Text(icons.PhCaretRight).Keep()).
				SendResp().HasPrimaryClicked() && !rightDisabled {
				sm.moveSelected(1)
				structureChanged = true
			}
		}
		if c.Button(ids.PrepareStr("rm-elem"), c.Atoms().Text("× remove").Keep()).
			SendResp().HasPrimaryClicked() {
			removeReq = sm.sel
		}
	}
	if removeReq >= 0 {
		sm.removeAt(removeReq)
		structureChanged = true
	}
	return
}

// renderStatus shows the assembled signature via the embedded
// canonicaltypesummary level-1 chip (validity dot + footprint + inspector
// toggle over the whole signature).
func (sm *SignatureModel) renderStatus(ids *c.WidgetIdStack) {
	// Name the readout for what it currently is: a single primitive until the
	// editor grows into a multi-element group/signature.
	label := "live type"
	if len(sm.elems) > 1 {
		label = "live signature"
	}
	smallLabel(label)
	canonicaltypesummary.New("ctedit-sig-sum").Render(ids.PrepareSeq(sigSummarySeq), sm.canonical)
}
