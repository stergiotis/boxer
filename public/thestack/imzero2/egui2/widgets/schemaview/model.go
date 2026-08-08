package schemaview

import (
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
)

// selKind discriminates what the navigator selection points at, so the
// detail pane can render the matching shape.
type selKind uint8

const (
	selNone          selKind = iota
	selPlainColumn           // a plain value column (indexed by plainCol)
	selSection               // a tagged section as a whole (indexed by section)
	selSectionColumn         // a value column inside a tagged section (section + col)
)

// selection identifies the tree node whose detail the right pane shows.
// Indices reference the live TableDesc; they stay valid as long as the bound
// TableDesc is stable, which it is — the host swaps the whole binding via
// [Model.SetTable] when the fixture changes, and SetTable resets selection.
type selection struct {
	kind     selKind
	plainCol int // index into PlainValues* (selPlainColumn)
	section  int // index into TaggedValuesSections (selSection / selSectionColumn)
	col      int // index into a section's ValueColumn* (selSectionColumn)
}

// Model is the editable state of the inspector: the schema under view plus
// the navigator's selection, filter, and legend-popup flag. The schema is
// owned by the host (a [*common.TableDesc] handed in at construction); the
// widget mutates only sel, filter, and legendOpen.
type Model struct {
	Table  *common.TableDesc
	sel    selection
	filter string // case-insensitive substring; "" shows everything
	// legendOpen pins the tethered glyph-legend window (the "?" affordance in
	// the navigator header). The window's title-bar close writes back here via
	// an R10 databinding, so it stays a plain widget-owned bool.
	legendOpen bool

	// collapsed holds the sections the reader has closed, keyed by
	// [navNode.key]. Absent means open, which is what the CollapsingHeader
	// navigator's DefaultOpen(true) meant before the port.
	collapsed map[string]bool
	// navState is the tree widget's view state. It is rewritten from collapsed
	// and sel before every render ([Model.syncNav]) rather than being an
	// authority of its own, because it keys on node indices and the filter
	// renumbers those on every keystroke.
	navState tree.State
	// navLabels / navParents / navNodes are [Model.buildNav]'s scratch: the
	// hierarchy is rebuilt every frame — the filter can change on any of them —
	// so the slices are retained and refilled rather than reallocated.
	navLabels  []string
	navParents []int32
	navNodes   []navNode
}

// NewModel binds a schema and selects a sensible default node so the detail
// pane is populated on the first frame.
func NewModel(table *common.TableDesc) *Model {
	return &Model{Table: table, sel: defaultSelection(table)}
}

// SetTable rebinds the schema (used when the host swaps fixtures) and resets
// the selection, which indexed into the previous TableDesc.
func (m *Model) SetTable(table *common.TableDesc) {
	m.Table = table
	m.sel = defaultSelection(table)
}

// defaultSelection prefers the first plain column, then the first tagged
// section's first column, then the section itself.
func defaultSelection(t *common.TableDesc) selection {
	if t == nil {
		return selection{}
	}
	if len(t.PlainValuesNames) > 0 {
		return selection{kind: selPlainColumn, plainCol: 0}
	}
	if len(t.TaggedValuesSections) > 0 {
		if len(t.TaggedValuesSections[0].ValueColumnNames) > 0 {
			return selection{kind: selSectionColumn, section: 0, col: 0}
		}
		return selection{kind: selSection, section: 0}
	}
	return selection{}
}

// matches reports whether any of the supplied names contains the current
// filter (case-insensitive). An empty/blank filter matches everything.
func (m *Model) matches(names ...string) bool {
	f := strings.ToLower(strings.TrimSpace(m.filter))
	if f == "" {
		return true
	}
	for _, n := range names {
		if strings.Contains(strings.ToLower(n), f) {
			return true
		}
	}
	return false
}

// matchesSection reports whether a section is visible under the filter — by
// its own name or any of its column names. Filtering is per-section, not
// per-column: a matching section shows all its columns.
func (m *Model) matchesSection(sec *common.TaggedValuesSection) bool {
	names := make([]string, 0, len(sec.ValueColumnNames)+1)
	names = append(names, sec.Name.String())
	for _, n := range sec.ValueColumnNames {
		names = append(names, n.String())
	}
	return m.matches(names...)
}
