package play

import (
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
)

// The Table pane's row grouping (ADR-0336 M3): a spec the person sets from
// the picker on the pager row or an agent sets through set_table_options,
// held by column name so it survives a rerun of the same query; the
// recordtable.GroupState that holds the expansion; and the gestures the
// grid reports, routed through set_table_options as the sort is
// (ADR-0270 §SD6).

// tableGroupLevel is one level of the spec: a column, by physical name, and
// how its groups order among themselves.
type tableGroupLevel struct {
	column     string
	desc       bool
	nullsFirst bool
}

// tableGroupPathSep joins a group's keys in the path get_table prints and
// expand / collapse read.
const tableGroupPathSep = " / "

// tableGroupAll is the expand / collapse argument that means every group.
const tableGroupAll = "all"

// tableGroupingActive reports whether a spec is set.
func (inst *PlayApp) tableGroupingActive() bool { return len(inst.tableGroupSpec) > 0 }

// tableGroupLevelsFor resolves the spec against a schema: a level whose
// column the result lacks is dropped, so a result with other columns draws
// flat and the spec waits for one that has them.
func (inst *PlayApp) tableGroupLevelsFor(schema *arrow.Schema) (levels []recordtable.GroupLevel) {
	for _, lvl := range inst.tableGroupSpec {
		idx := schema.FieldIndices(lvl.column)
		if len(idx) == 0 {
			continue
		}
		levels = append(levels, recordtable.GroupLevel{
			Arrow: idx[0],
			Order: recordtable.GroupOrder{Desc: lvl.desc, NullsFirst: lvl.nullsFirst},
		})
	}
	return
}

// applyTableGrouping hands the grid's GroupState the spec as it resolves
// against the schema drawn, when that differs from what it holds: a
// SetLevels repartitions and resets nothing it need not, so it is made only
// on a change.
func (inst *PlayApp) applyTableGrouping(schema *arrow.Schema) {
	want := inst.tableGroupLevelsFor(schema)
	have := inst.tableGroups.Levels()
	same := len(want) == len(have)
	for i := 0; same && i < len(want); i++ {
		same = want[i].Arrow == have[i].Arrow && want[i].Order == have[i].Order
	}
	if !same {
		inst.tableGroups.SetLevels(want)
	}
}

// tableGroupPath prints a group's path: its keys from the first level down,
// joined; a null key prints as (null). It is what get_table lists and what
// expand / collapse take back.
func tableGroupPath(lay *recordtable.Layout, group int32) string {
	keys := lay.PathOf(group)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		switch {
		case k.Null:
			parts = append(parts, "(null)")
		case k.Label != "":
			parts = append(parts, k.Label)
		default:
			parts = append(parts, k.Value)
		}
	}
	return strings.Join(parts, tableGroupPathSep)
}

// tableGroupByPath finds the group a printed path names in the layout
// drawn, -1 when none. The root (the empty path) is not a group.
func tableGroupByPath(lay *recordtable.Layout, path string) int32 {
	path = strings.TrimSpace(path)
	if lay == nil || path == "" {
		return -1
	}
	for g := int32(1); int(g) < len(lay.Groups); g++ {
		if tableGroupPath(lay, g) == path {
			return g
		}
	}
	return -1
}

// tableGroupSpecWith is the spec with one level's direction turned around,
// as a grouped column's header click asks.
func tableGroupSpecWith(spec []tableGroupLevel, level int, desc bool) (out []TableGroupLevelArgs) {
	out = make([]TableGroupLevelArgs, 0, len(spec))
	for i, lvl := range spec {
		a := TableGroupLevelArgs{Column: lvl.column, Descending: lvl.desc, NullsFirst: lvl.nullsFirst}
		if i == level {
			a.Descending = desc
		}
		out = append(out, a)
	}
	return
}

// tableGroupSpecArgs is the spec as set_table_options takes it, with one
// level dropped (drop >= 0) or one appended (add != "").
func tableGroupSpecArgs(spec []tableGroupLevel, drop int, add string) (out []TableGroupLevelArgs) {
	out = make([]TableGroupLevelArgs, 0, len(spec)+1)
	for i, lvl := range spec {
		if i == drop {
			continue
		}
		out = append(out, TableGroupLevelArgs{Column: lvl.column, Descending: lvl.desc, NullsFirst: lvl.nullsFirst})
	}
	if add != "" {
		out = append(out, TableGroupLevelArgs{Column: add})
	}
	return
}

// renderTableGroupPicker draws the grouping controls on the pager row: a
// chip per level — its caption and direction; a click cycles it ascending,
// descending, off, as the sort's header does — a combo that adds a column,
// and expand / collapse while grouped. Every change goes through
// set_table_options. The combo's items are captioned "+ name" so a driver
// tells them from the column's header.
func (inst *PlayApp) renderTableGroupPicker(schema *arrow.Schema, visCols []int) {
	ids := inst.ids
	gap := styletokens.GapInline(inst.density)
	c.AddSpace(styletokens.GapPanels(inst.density))
	for rt := range c.RichTextLabel("Group by") {
		rt.Weak().Small()
	}
	grouped := make(map[string]bool, len(inst.tableGroupSpec))
	for i, lvl := range inst.tableGroupSpec {
		grouped[lvl.column] = true
		caption := lvl.column
		if idx := schema.FieldIndices(lvl.column); len(idx) > 0 {
			caption = inst.tableColumnCaption(schema, idx[0])
		}
		// The Phosphor carets, not the header's text triangles, so the chip's
		// name differs from the grouped header's for a driver.
		glyph := icons.PhCaretUp
		if lvl.desc {
			glyph = icons.PhCaretDown
		}
		for range c.IdScope(ids.PrepareSeq(uint64(i))) {
			hint := "click to reverse the groups of this level"
			if lvl.desc {
				hint = "click to stop grouping by " + caption
			}
			for range c.HoverText(hint).KeepIter() {
				if c.Button(ids.PrepareStr("table-group-level"), c.Atoms().Text(caption+" "+glyph).Keep()).Small().
					SendResp().HasPrimaryClicked() {
					in := SetTableOptionsArgs{GroupBy: tableGroupSpecWith(inst.tableGroupSpec, i, true)}
					if lvl.desc {
						in.GroupBy = tableGroupSpecArgs(inst.tableGroupSpec, i, "")
					}
					inst.requestTableOptions(in)
				}
			}
		}
		c.AddSpace(gap)
	}
	for range c.ComboBox(ids.PrepareStr("table-group-add"),
		c.WidgetText().Text("").Keep(),
		c.WidgetText().Text("+ column").Keep()).
		KeepIter() {
		for _, col := range visCols {
			name := schema.Field(col).Name
			if grouped[name] {
				continue
			}
			if c.Button(ids.PrepareSeq(uint64(0x6700+col)), c.Atoms().Text("+ "+inst.tableColumnCaption(schema, col)).Keep()).
				Frame(false).SendResp().HasPrimaryClicked() {
				inst.requestTableOptions(SetTableOptionsArgs{GroupBy: tableGroupSpecArgs(inst.tableGroupSpec, -1, name)})
			}
		}
	}
	if !inst.tableGroupingActive() {
		return
	}
	c.AddSpace(gap)
	all := tableGroupAll
	if c.Button(ids.PrepareStr("table-group-expand"), c.Atoms().Text("expand all").Keep()).Small().
		SendResp().HasPrimaryClicked() {
		inst.requestTableOptions(SetTableOptionsArgs{Expand: &all})
	}
	if c.Button(ids.PrepareStr("table-group-collapse"), c.Atoms().Text("collapse all").Keep()).Small().
		SendResp().HasPrimaryClicked() {
		inst.requestTableOptions(SetTableOptionsArgs{Collapse: &all})
	}
}

// tableColumnCaption is a column as its header captions it: the gloss
// label, else the leeway handle, else the physical name.
func (inst *PlayApp) tableColumnCaption(schema *arrow.Schema, col int) string {
	name := schema.Field(col).Name
	inst.ensureColLabels(schema)
	glossCols := inst.glossColumns(schema)
	if col < len(glossCols) && glossCols[col].label != "" {
		return glossCols[col].label
	}
	if label := inst.colLabels[name]; label != "" {
		return label
	}
	return name
}

// routeTableGroupGestures turns what the grid reported into the
// set_table_options each gesture is: a band or chevron click folds or
// unfolds its group, a grouped column's header reverses its level.
func (inst *PlayApp) routeTableGroupGestures(res recordtable.Result) {
	lay := inst.tableGroups.Layout()
	if g := res.GroupToggled; g >= 0 && int(g) < len(lay.Groups) {
		path := tableGroupPath(lay, g)
		if lay.Groups[g].Open {
			inst.requestTableOptions(SetTableOptionsArgs{Collapse: &path})
		} else {
			inst.requestTableOptions(SetTableOptionsArgs{Expand: &path})
		}
	}
	if lvl := res.GroupOrderClicked; lvl >= 0 && lvl < len(inst.tableGroupSpec) {
		inst.requestTableOptions(SetTableOptionsArgs{GroupBy: tableGroupSpecWith(inst.tableGroupSpec, lvl, !inst.tableGroupSpec[lvl].desc)})
	}
}

// followTableReveal serves a reveal the layout answered with a line off the
// page: the page turns to it, and the reveal is asked again so the grid
// scrolls there on the frame that draws that page.
func (inst *PlayApp) followTableReveal(pageStart, pageEnd int64) {
	lay := inst.tableGroups.Layout()
	line := lay.Revealed
	if line < 0 || (line >= pageStart && line < pageEnd) || line >= lay.Len() {
		return
	}
	inst.pager.GoToIndex(line)
	inst.tableGroups.Reveal(lay.Lines[line].Row)
}
