package play

import (
	"encoding/hex"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/semistructured/cbor/diag"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/cbordiag"
)

// play_detail_identity.go is the Detail pane's canonical-identity strip
// (ADR-0219 SD5): one row per form — the canonform digest and the canonwire
// fingerprint, hex, with a copy button — and, behind a disclosure closed by
// default, the CBOR items both were computed from in diagnostic notation.
//
// Cost model: the values are computed once per (result, row) by driving a
// one-row slice through fresh encoders on the render thread — the card's own
// Prepare does the same drive every frame — and the items only while the
// disclosure is open, once per (result, row) as well.

// identityDetail owns the strip's state. One per PlayApp.
type identityDetail struct {
	ids *c.WidgetIdStack

	// Per-schema: the computer over the CardDriver's driver.
	schema  *arrow.Schema
	comp    *identityComputer
	compErr error

	// Per-(result, row) cache of the values.
	result ResultID
	row    int64
	has    bool
	vals   identityValues
	err    error

	// Per-(result, row) cache of the items, filled only while open.
	itemsFor   bool
	canonItems []byte
	wireItem   []byte
	itemsErr   error

	canonView  cbordiag.Renderer
	wireView   cbordiag.Renderer
	canonState cbordiag.State
	wireState  cbordiag.State
}

func newIdentityDetail(ids *c.WidgetIdStack) (inst *identityDetail) {
	inst = &identityDetail{ids: ids, row: -1}
	inst.canonView = cbordiag.New(ids, "idn-cf")
	inst.wireView = cbordiag.New(ids, "idn-cw")
	return
}

// ensureComputer binds the encoders to the current schema's reconstructed
// table, once per schema pointer. The CardDriver must have been probed for
// the schema already (RenderDefaultDetailContent does that first).
func (inst *identityDetail) ensureComputer(cards *CardDriver, schema *arrow.Schema) bool {
	if schema == inst.schema {
		return inst.comp != nil
	}
	inst.schema = schema
	inst.comp = nil
	inst.compErr = nil
	inst.dropRow()
	if cards == nil || cards.TableDesc() == nil || cards.IR() == nil || cards.Driver() == nil {
		return false
	}
	inst.comp, inst.compErr = newIdentityComputer(cards.TableDesc(), cards.IR(), cards.Driver())
	return inst.comp != nil
}

func (inst *identityDetail) dropRow() {
	inst.result = 0
	inst.row = -1
	inst.has = false
	inst.err = nil
	inst.itemsFor = false
	inst.canonItems = nil
	inst.wireItem = nil
	inst.itemsErr = nil
	inst.canonState.Invalidate()
	inst.wireState.Invalidate()
}

// valuesFor returns the identities of row of rec, computed on the first
// call for a (result, row) and cached after. ok is false off the leeway path.
func (inst *identityDetail) valuesFor(cards *CardDriver, rec arrow.RecordBatch, row int64, result ResultID) (v identityValues, err error, ok bool) {
	if rec == nil || !inst.ensureComputer(cards, rec.Schema()) {
		return v, inst.compErr, false
	}
	if inst.result == result && inst.row == row && result != 0 {
		return inst.vals, inst.err, true
	}
	inst.dropRow()
	inst.result = result
	inst.row = row
	inst.vals, inst.err = inst.comp.row(rec, row)
	inst.has = inst.err == nil
	return inst.vals, inst.err, true
}

// itemsFor returns the CBOR items behind the cached row, computed on the
// first call after valuesFor cached that row.
func (inst *identityDetail) items(cards *CardDriver, rec arrow.RecordBatch, row int64) (canonItems []byte, wireItem []byte, err error) {
	if inst.itemsFor {
		return inst.canonItems, inst.wireItem, inst.itemsErr
	}
	inst.itemsFor = true
	inst.canonItems, inst.wireItem, inst.itemsErr = inst.comp.rowItems(cards.IR(), rec, row)
	inst.canonState.Invalidate()
	inst.wireState.Invalidate()
	return inst.canonItems, inst.wireItem, inst.itemsErr
}

// render draws the strip for row of rec and reports whether it drew anything.
// A non-leeway result draws nothing; a failed computation draws its error.
func (inst *identityDetail) render(app *PlayApp, rec arrow.RecordBatch, row int64, result ResultID) (shown bool) {
	if inst == nil || app == nil || app.cards == nil {
		return false
	}
	vals, err, ok := inst.valuesFor(app.cards, rec, row, result)
	if !ok {
		if inst.compErr != nil {
			for rt := range c.RichTextLabel("identity: " + inst.compErr.Error()) {
				rt.Weak().Small()
			}
			return true
		}
		return false
	}
	ids := inst.ids
	for rt := range c.RichTextLabel("identity") {
		rt.Weak().Small()
	}
	if err != nil {
		for rt := range c.RichTextLabel("identity: " + err.Error()) {
			rt.Weak().Small()
		}
		return true
	}
	canonHex := hex.EncodeToString(vals.canon[:])
	wireHex := hex.EncodeToString(vals.wire[:])
	inst.valueRow(app, "canonform", canonHex, inst.comp.pin, "idn-copy-cf")
	verdict := "canonical"
	if vals.wireErr != nil {
		verdict = "not canonical: " + vals.wireErr.Error()
	}
	inst.valueRow(app, "canonwire", wireHex,
		fmt.Sprintf("fingerprint of the canonical wire item — keyed BLAKE3-256 over %d bytes (ADR-0210 form v1, no tagger); %s", vals.wireLen, verdict),
		"idn-copy-cw")
	for range c.CollapsingHeader(ids.PrepareStr("idn-cbor"),
		c.WidgetText().Text("CBOR").Keep()).DefaultOpen(false).KeepIter() {
		canonItems, wireItem, ierr := inst.items(app.cards, rec, row)
		if ierr != nil {
			for rt := range c.RichTextLabel("items: " + ierr.Error()) {
				rt.Weak().Small()
			}
			continue
		}
		for rt := range c.RichTextLabel("canonform · attribute items, then the entity item") {
			rt.Weak().Small()
		}
		inst.canonView.Render(&inst.canonState, canonItems, diag.Options{Sequence: true, TagComments: true, Annotate: annotateCanonform})
		for rt := range c.RichTextLabel("canonwire · entity item") {
			rt.Weak().Small()
		}
		inst.wireState.Verdict = verdict
		inst.wireView.Render(&inst.wireState, wireItem, diag.Options{TagComments: true, Annotate: annotateCanonwire})
	}
	return true
}

// valueRow draws one label · hex · copy row; hover is the value's provenance.
func (inst *identityDetail) valueRow(app *PlayApp, label string, value string, hover string, copyID string) {
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel(label) {
			rt.Weak().Monospace()
		}
		for range c.HoverText(hover).KeepIter() {
			for rt := range c.RichTextLabel(value) {
				rt.Monospace()
			}
		}
		if app.CanCopy() {
			if c.Button(inst.ids.PrepareStr(copyID), c.Atoms().Text("copy").Keep()).SendResp().HasPrimaryClicked() {
				app.copyToClipboard(value)
			}
		}
	}
}

// cborKeyUint decodes a small CBOR unsigned integer from an encoded map key
// — the plain item type keys of both forms — or reports false.
func cborKeyUint(key []byte) (n uint64, ok bool) {
	if len(key) == 0 || key[0]>>5 != 0 {
		return
	}
	switch ai := key[0] & 0x1f; {
	case ai < 24:
		return uint64(ai), true
	case ai == 24 && len(key) >= 2:
		return uint64(key[1]), true
	case ai == 25 && len(key) >= 3:
		return uint64(key[1])<<8 | uint64(key[2]), true
	}
	return
}

// annotateCanonwire labels the positions of a canonical-wire entity item
// (ADR-0210 SD1–SD3): the three top-level elements, the plain item types,
// the slots and each attribute's memberships element.
func annotateCanonwire(path []diag.PathElem) string {
	switch len(path) {
	case 1:
		if path[0].Kind == diag.PathElemIndex {
			switch path[0].Index {
			case 0:
				return "version"
			case 1:
				return "plains"
			case 2:
				return "tagged"
			}
		}
	case 2:
		if path[0].Kind != diag.PathElemIndex || path[1].Kind != diag.PathElemKey {
			return ""
		}
		switch path[0].Index {
		case 1:
			if n, ok := cborKeyUint(path[1].Key); ok {
				return common.PlainItemTypeE(n).String()
			}
		case 2:
			return "slot"
		}
	case 3:
		if path[0].Kind == diag.PathElemIndex && path[0].Index == 2 && path[2].Kind == diag.PathElemIndex {
			return "attribute"
		}
	case 4:
		if path[0].Kind == diag.PathElemIndex && path[0].Index == 2 && path[3].Kind == diag.PathElemIndex && path[3].Index == 0 {
			return "memberships"
		}
	}
	return ""
}

// annotateCanonform labels the canonform items (ADR-0201 SD1, SD6): an
// attribute item is [memberships, value]; the entity item is
// {0: plains, 1: leaf digests}.
func annotateCanonform(path []diag.PathElem) string {
	if len(path) != 1 {
		return ""
	}
	switch path[0].Kind {
	case diag.PathElemIndex:
		switch path[0].Index {
		case 0:
			return "memberships"
		case 1:
			return "value"
		}
	case diag.PathElemKey:
		if n, ok := cborKeyUint(path[0].Key); ok {
			switch n {
			case 0:
				return "plains"
			case 1:
				return "leaf digests"
			}
		}
	}
	return ""
}
