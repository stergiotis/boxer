// Package canonicaltypeedit is an immediate-mode widget (ADR-0267): an editor
// for a single primitive leeway canonical type ([canonicaltypes]), and for a
// signature of them. It is the editor half of ADR-0067.
//
// The editor presents two synchronised views of one type, kept consistent by
// the bidirectional discipline in ADR-0067 §SD2:
//
//   - A formula bar — a free-text [c.TextEdit] holding the canonical string.
//   - A structured form whose controls mirror the grammar productions
//     (family → base → family-specific modifiers → scalar shape), so invalid
//     *shapes* are unrepresentable from the form (ADR-0067 §SD3).
//
// The value is a flat draft (the unexported fields of [Model]), which the
// host owns and reads back with [Model.Canonical] / [Model.Node] /
// [Model.Valid]. What the editor needs to remember between frames that is
// not the value — the bar's text buffer and its parse-error headline, the
// form's disclosure — is a [State] the host also owns and passes on every
// [Render] (ADR-0267 W9/W10). Each frame, at most one side can have been
// edited (egui edits one widget per frame), so Render applies a simple
// edge-ownership rule: a bar edit re-parses into the draft (keeping the
// buffer on a parse failure so mid-typing survives); a form edit
// re-canonicalises the bar. The editor also embeds the level-1 chip of
// [canonicaltypesummary] over the live value, so the same anchor can pop the
// full tethered inspector (ADR-0067 §SD4).
//
// [RenderSignature] edits a [SignatureModel] — a chip strip of primitive
// elements joined by '-'/'_' — with the same bar+form over the selected chip.
package canonicaltypeedit

import (
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/canonicaltypesummary"
)

// familyE is the primitive family, derived from the base rune.
type familyE uint8

const (
	familyString familyE = iota
	familyNumeric
	familyTemporal
	familyNetwork
)

// Model is the host-owned value of one primitive canonical type: the flat
// draft and what is derived from it. It carries no UI state; that is
// [State]'s.
type Model struct {
	// Flat draft (ADR-0067 §SD1). base is the canonical base rune; the family
	// is derived from it via familyOf. The modifier fields are read only when
	// they apply to the current family (draftToNode), so a value left over
	// from another family is harmless.
	base       byte
	fixedWidth bool
	width      uint16
	byteOrder  canonicaltypes.ByteOrderModifierE
	cidr       bool
	scalarMod  canonicaltypes.ScalarModifierE

	// Derived cache, refreshed by rebuildFromDraft.
	ast       canonicaltypes.PrimitiveAstNodeI
	canonical string
	valid     bool
}

// State is the host-owned UI state of one primitive editor: the formula
// bar's text buffer and parse-error headline, the form's disclosure, and the
// embedded summary chip's own state. The zero value is a collapsed editor
// whose bar mirrors the model on the first frame. It must live at a stable
// address for as long as the editor is drawn — the bar's TextEdit binds to
// the buffer across frames (ADR-0267 W10).
type State struct {
	// barBuf is the formula-bar backing string; barErr is the last parse
	// error headline (empty when the bar parses). barFor is the canonical
	// the buffer was last synchronised to: when the model's canonical moves
	// away from it — a host SetCanonical, a form edit — the bar follows.
	barBuf string
	barErr string
	barFor string

	// formOpen drives the structured-form disclosure: false (the default)
	// keeps the editor a single bar row with just the inline toggle; true
	// reveals the grammar controls below it.
	formOpen bool

	summary canonicaltypesummary.State
}

// BarError reports the formula-bar parse-error headline, or "" when the bar
// parses. A non-empty result means the editor is showing an unparseable
// in-progress entry, so [Model.Canonical] / [Model.Node] / [Model.Valid] are
// stale (they hold the last type that parsed, since a parse failure keeps the
// draft) — a consumer gating on the edited type should treat a non-empty
// BarError as "not a usable type right now".
func (st *State) BarError() string { return st.barErr }

// FormOpen reports whether the structured form is disclosed.
func (st *State) FormOpen() bool { return st.formOpen }

// SetFormOpen discloses or collapses the structured form from code.
func (st *State) SetFormOpen(open bool) { st.formOpen = open }

// syncBar makes the bar follow the model when the canonical changed behind
// it (a host seed, a form edit). While the user is mid-edit on an unparseable
// entry the canonical has not moved, so the buffer is left alone.
func (st *State) syncBar(m *Model) {
	if st.barFor != m.canonical {
		st.barBuf = m.canonical
		st.barErr = ""
		st.barFor = m.canonical
	}
}

// NewModel returns an editor seeded with a friendly default (`u32`).
func NewModel() (m *Model) {
	m = &Model{
		base:  byte(canonicaltypes.BaseTypeMachineNumericUnsigned),
		width: 32,
	}
	m.rebuildFromDraft()
	return
}

// Canonical returns the current canonical-string form of the edited type.
func (m *Model) Canonical() string { return m.canonical }

// Valid reports whether the current type passes [canonicaltypes.AstNodeI.IsValid].
func (m *Model) Valid() bool { return m.valid }

// Node returns the current primitive AST node. It is constructed even when the
// type is invalid (e.g. a fixed-width string with width 0), so pair it with
// [Model.Valid] before relying on it.
func (m *Model) Node() canonicaltypes.PrimitiveAstNodeI { return m.ast }

// SetCanonical seeds the editor from a canonical string. A parse failure is a
// no-op so the editor keeps its current value; pass a single primitive (groups
// and signatures are out of scope for this editor). The bar follows on the
// next Render.
func (m *Model) SetCanonical(s string) {
	n, err := parsePrimitive(s)
	if err != nil {
		return
	}
	m.nodeToDraft(n)
	m.rebuildFromDraft()
}

// rebuildFromDraft reconstructs the AST node from the draft and refreshes the
// derived cache (ast, canonical, valid).
func (m *Model) rebuildFromDraft() {
	n := m.draftToNode()
	m.ast = n
	m.canonical = n.String()
	m.valid = n.IsValid()
}

// draftToNode builds the concrete primitive node for the current family,
// reading only the fields that apply to it. It always returns a node (even an
// invalid one) so the canonical readout and validity dot stay live.
func (m *Model) draftToNode() canonicaltypes.PrimitiveAstNodeI {
	switch familyOf(m.base) {
	case familyString:
		n := canonicaltypes.StringAstNode{
			BaseType:       canonicaltypes.BaseTypeStringE(m.base),
			ScalarModifier: m.scalarMod,
		}
		// bool carries no width; otherwise a fixed-width string pins the width.
		if canonicaltypes.BaseTypeStringE(m.base) != canonicaltypes.BaseTypeStringBool && m.fixedWidth {
			n.WidthModifier = canonicaltypes.WidthModifierFixed
			n.Width = canonicaltypes.Width(m.width)
		}
		return n
	case familyTemporal:
		return canonicaltypes.TemporalTypeAstNode{
			BaseType:       canonicaltypes.BaseTypeTemporalE(m.base),
			Width:          canonicaltypes.Width(m.width),
			ScalarModifier: m.scalarMod,
		}
	case familyNetwork:
		n := canonicaltypes.NetworkTypeAstNode{
			BaseType:       canonicaltypes.BaseTypeNetworkE(m.base),
			ScalarModifier: m.scalarMod,
		}
		if m.cidr {
			n.CIDRModifier = canonicaltypes.CIDRModifierVariable
		}
		return n
	default: // familyNumeric
		return canonicaltypes.MachineNumericTypeAstNode{
			BaseType:          canonicaltypes.BaseTypeMachineNumericE(m.base),
			Width:             canonicaltypes.Width(m.width),
			ByteOrderModifier: m.byteOrder,
			ScalarModifier:    m.scalarMod,
		}
	}
}

// nodeToDraft loads the draft fields from a parsed primitive node, clearing
// modifiers that do not apply so a re-canonicalise produces exactly the parsed
// type.
func (m *Model) nodeToDraft(n canonicaltypes.PrimitiveAstNodeI) {
	m.fixedWidth = false
	m.width = 0
	m.byteOrder = canonicaltypes.ByteOrderModifierNone
	m.cidr = false
	m.scalarMod = canonicaltypes.ScalarModifierNone
	switch t := n.(type) {
	case canonicaltypes.StringAstNode:
		m.base = byte(t.BaseType)
		m.fixedWidth = t.WidthModifier == canonicaltypes.WidthModifierFixed
		m.width = uint16(t.Width)
		m.scalarMod = t.ScalarModifier
	case canonicaltypes.MachineNumericTypeAstNode:
		m.base = byte(t.BaseType)
		m.width = uint16(t.Width)
		m.byteOrder = t.ByteOrderModifier
		m.scalarMod = t.ScalarModifier
	case canonicaltypes.TemporalTypeAstNode:
		m.base = byte(t.BaseType)
		m.width = uint16(t.Width)
		m.scalarMod = t.ScalarModifier
	case canonicaltypes.NetworkTypeAstNode:
		m.base = byte(t.BaseType)
		m.cidr = t.CIDRModifier == canonicaltypes.CIDRModifierVariable
		m.scalarMod = t.ScalarModifier
	}
}

// pkgParser is reused across parse calls (the parser resets its lexer per
// call). egui rendering is single-threaded; the mutex guards against a stray
// off-thread caller corrupting the shared antlr state. Mirrors
// canonicaltypesummary's parser handling.
var (
	pkgParser   = canonicaltypes.NewParser()
	pkgParserMu sync.Mutex
)

// parsePrimitive parses a single primitive canonical type (no groups).
func parsePrimitive(s string) (canonicaltypes.PrimitiveAstNodeI, error) {
	pkgParserMu.Lock()
	defer pkgParserMu.Unlock()
	return pkgParser.ParsePrimitiveTypeAst(s)
}

// familyOf derives the primitive family from a base rune.
func familyOf(base byte) familyE {
	switch base {
	case byte(canonicaltypes.BaseTypeStringUtf8), byte(canonicaltypes.BaseTypeStringBytes), byte(canonicaltypes.BaseTypeStringBool):
		return familyString
	case byte(canonicaltypes.BaseTypeTemporalUtcDatetime), byte(canonicaltypes.BaseTypeTemporalZonedDatetime), byte(canonicaltypes.BaseTypeTemporalZonedTime):
		return familyTemporal
	case byte(canonicaltypes.BaseTypeNetworkIPv4), byte(canonicaltypes.BaseTypeNetworkIPv6):
		return familyNetwork
	default: // u / i / f
		return familyNumeric
	}
}

// familyDefaultBase is the base a family snaps to when first selected.
func familyDefaultBase(f familyE) byte {
	switch f {
	case familyString:
		return byte(canonicaltypes.BaseTypeStringUtf8)
	case familyTemporal:
		return byte(canonicaltypes.BaseTypeTemporalUtcDatetime)
	case familyNetwork:
		return byte(canonicaltypes.BaseTypeNetworkIPv4)
	default:
		return byte(canonicaltypes.BaseTypeMachineNumericUnsigned)
	}
}

// defaultWidth is the bit width a width-bearing family snaps to when it would
// otherwise be zero (e.g. after switching from a network type, which has none).
func defaultWidth(f familyE) uint16 {
	if f == familyTemporal {
		return 64
	}
	return 32
}

// clampWidth keeps a form-entered width in a sane range; arbitrary widths are
// still reachable through the formula bar.
func clampWidth(w uint64) uint16 {
	switch {
	case w < 1:
		return 1
	case w > 4096:
		return 4096
	default:
		return uint16(w)
	}
}

// firstLine returns the trimmed first line of a (possibly multi-line) error.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
