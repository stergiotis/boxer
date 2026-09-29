package schemaview

import (
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/canonicaltypesummary"
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
// TableDesc is stable, which it is — a different Input.Table pointer resets
// the selection ([State.bind]).
type selection struct {
	kind     selKind
	plainCol int // index into PlainValues* (selPlainColumn)
	section  int // index into TaggedValuesSections (selSection / selSectionColumn)
	col      int // index into a section's ValueColumn* (selSectionColumn)
}

// State is the host-owned UI state of one inspector (ADR-0267 W9): the
// navigator's selection, filter and legend-popup flag, the detail pane's
// canonical-type summary state, and the tree widget's view state. Its zero
// value is usable; keep one per inspector across frames. The schema itself
// is data and arrives on Input.Table every frame; a different Table pointer
// resets the selection, which indexed into the previous one, and keeps the
// filter.
type State struct {
	// table is the TableDesc sel indexes; see bind.
	table  *common.TableDesc
	sel    selection
	filter string // case-insensitive substring; "" shows everything
	// legendOpen pins the tethered glyph-legend window (the "?" affordance in
	// the navigator header). The window's title-bar close writes back here via
	// an R10 databinding, so it stays a plain widget-owned bool.
	legendOpen bool
	// colType is the detail pane's canonical-type summary state (the tethered
	// inspector of whichever column is selected).
	colType canonicaltypesummary.State

	// navState is the tree widget's view state, and the authority for which
	// sections are open. It survives the rebuild every filter keystroke
	// triggers because the hierarchy carries [navNode.key] as its key column,
	// so the widget files expansion under the section rather than under an
	// index the next keystroke reassigns. Its selection is projected from sel,
	// which is the richer thing the detail pane reads ([view.syncNav]).
	navState tree.State
	// navLabels / navParents / navKeys / navNodes are [view.buildNav]'s
	// scratch: the hierarchy is rebuilt every frame — the filter can change on
	// any of them — so the slices are retained and refilled rather than
	// reallocated.
	//
	// navKeys is the identity column the widget files expansion under:
	// "plain:<item-type>" for a plain grouping, "sec:<name>" or
	// "co:<group>:<name>" for a tagged section, and the parent's key plus
	// ":<column>" for a column beneath one. Stable across a filter keystroke,
	// which node indices are not — that is the whole reason it exists.
	navLabels  []string
	navParents []int32
	navKeys    []string
	navNodes   []navNode
}

// view is one frame's pairing of the host's schema with its State: the
// receiver every render and navigation helper works on.
type view struct {
	Table *common.TableDesc
	*State
}

// bind points the State at table, resetting the selection when the table is
// a different one from last frame (it indexed into the previous TableDesc)
// and selecting a sensible default node so the detail pane is populated.
func (st *State) bind(table *common.TableDesc) {
	if st.table == table {
		return
	}
	st.table = table
	st.sel = defaultSelection(table)
}

// newView binds st to table for one frame.
func newView(table *common.TableDesc, st *State) *view {
	st.bind(table)
	return &view{Table: table, State: st}
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
func (m *view) matches(names ...string) bool {
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
func (m *view) matchesSection(sec *common.TaggedValuesSection) bool {
	names := make([]string, 0, len(sec.ValueColumnNames)+1)
	names = append(names, sec.Name.String())
	for _, n := range sec.ValueColumnNames {
		names = append(names, n.String())
	}
	return m.matches(names...)
}
