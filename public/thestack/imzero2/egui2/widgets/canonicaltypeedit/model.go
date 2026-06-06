// Package canonicaltypeedit is an editor widget for a single primitive leeway
// canonical type ([canonicaltypes]). It is the editor half of ADR-0067.
//
// The editor presents two synchronised views of one type, kept consistent by
// the bidirectional discipline in ADR-0067 §SD2:
//
//   - A formula bar — a free-text [c.TextEdit] holding the canonical string.
//   - A structured form whose controls mirror the grammar productions
//     (family → base → family-specific modifiers → scalar shape), so invalid
//     *shapes* are unrepresentable from the form (ADR-0067 §SD3).
//
// The single source of truth is a flat draft (the unexported fields of
// [Model]). Each frame, at most one side can have been edited (egui edits one
// widget per frame), so [Model.Render] applies a simple edge-ownership rule:
// a bar edit re-parses into the draft (keeping the buffer on a parse failure
// so mid-typing survives); a form edit re-canonicalises the bar. The editor
// also embeds the level-1 chip of [canonicaltypesummary] over the live value,
// so the same anchor can pop the full tethered inspector (ADR-0067 §SD4).
//
// Scope is a single primitive; groups and signatures are deferred (ADR-0067).
// Callers own the [Model]: construct with [NewModel], render each frame, and
// read the result with [Model.Canonical] / [Model.Node] / [Model.Valid].
package canonicaltypeedit

import (
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
)

// familyE is the primitive family, derived from the base rune.
type familyE uint8

const (
	familyString familyE = iota
	familyNumeric
	familyTemporal
	familyNetwork
)

// Model is the caller-owned editable state for one primitive canonical type.
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

	// Bidirectional bar state: barBuf is the formula-bar backing string;
	// barErr is the last parse error headline (empty when the bar parses).
	barBuf string
	barErr string

	// Derived cache, refreshed by rebuildFromDraft.
	ast       canonicaltypes.PrimitiveAstNodeI
	canonical string
	valid     bool
}

// NewModel returns an editor seeded with a friendly default (`u32`).
func NewModel() (m *Model) {
	m = &Model{
		base:  byte(canonicaltypes.BaseTypeMachineNumericUnsigned),
		width: 32,
	}
	m.rebuildFromDraft()
	m.barBuf = m.canonical
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
// and signatures are out of scope for this editor).
func (m *Model) SetCanonical(s string) {
	n, err := parsePrimitive(s)
	if err != nil {
		return
	}
	m.nodeToDraft(n)
	m.rebuildFromDraft()
	m.barBuf = m.canonical
	m.barErr = ""
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
