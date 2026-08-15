package play

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// play_gloss.go is ADR-0186's per-column resolution: which gloss, if any,
// shows each column of a result, from where, and whether the column's value
// kind is one the gloss accepts. Resolved once per schema (the same pointer
// cache colLabels and colWidths use) and read by both Table grids per cell.
//
// M1 resolves the explicit `<label>@<media type>` alias only; the rule route
// (spec line, directive, affinities — §SD3) joins in M2 and adds sources here.

// glossSourceAlias names the explicit-alias source in hover text.
const glossSourceAlias = "alias"

// glossColumn is one column's resolution.
type glossColumn struct {
	// label is the header label a declaration supplies (`t` of `t@gloss/…`);
	// empty for an undeclared column, whose header keeps its physical name.
	label string
	// mediaType is the resolved gloss, empty when the column is plain.
	mediaType string
	// inst renders the cells; nil when plain or refused.
	inst gloss.InstanceI
	// source says where the binding came from (glossSourceAlias in M1).
	source string
	// reason is why a declaration could not be honoured — an unknown type, a
	// bad parameter — shown on hover; the cells then render plain.
	reason string
	// rowOK / elemOK are the gloss's Accepts verdicts on the whole column's
	// value kind (the per-DB-row grid) and on a list column's element kind
	// (the per-attribute grid explodes items). A refusal keeps the cells plain
	// and carries the reason on hover.
	rowOK      bool
	rowReason  string
	elemOK     bool
	elemReason string
}

// glossed reports whether the per-row grid renders this column through its
// gloss.
func (inst *glossColumn) glossed() bool { return inst.inst != nil && inst.rowOK }

// glossedElem reports the same for a list column's items.
func (inst *glossColumn) glossedElem() bool { return inst.inst != nil && inst.elemOK }

// glossResolution caches one schema's resolution.
type glossResolution struct {
	forSchema *arrow.Schema
	cols      []glossColumn
}

// glossColumns resolves (or returns the cached resolution of) schema.
func (inst *PlayApp) glossColumns(schema *arrow.Schema) []glossColumn {
	if schema == nil {
		return nil
	}
	if inst.glossRes.forSchema == schema && len(inst.glossRes.cols) == schema.NumFields() {
		return inst.glossRes.cols
	}
	cols := make([]glossColumn, schema.NumFields())
	for i := range cols {
		field := schema.Field(i)
		cols[i] = inst.resolveGlossColumn(field)
	}
	inst.glossRes = glossResolution{forSchema: schema, cols: cols}
	return cols
}

// resolveGlossColumn is the per-column resolution: the explicit alias in
// M1. It also settles the Accepts verdicts once, so the per-cell path is a
// field read.
func (inst *PlayApp) resolveGlossColumn(field arrow.Field) (gc glossColumn) {
	d, declared := inst.glossCatalog().ParseColumn(field.Name)
	if !declared {
		return gc
	}
	gc.label = d.Label
	gc.mediaType = d.MediaType
	gc.source = glossSourceAlias
	if d.Reason != "" {
		gc.reason = d.Reason
		return gc
	}
	gc.inst = d.Instance
	gc.rowOK, gc.rowReason = d.Instance.Accepts(gloss.KindOfArrow(field.Type))
	gc.elemOK, gc.elemReason = d.Instance.Accepts(gloss.KindOfArrow(listElemType(field.Type)))
	return gc
}

// glossCell renders one grid cell: the gloss's inline face when the column
// resolves, the kind is accepted and the raw toggle is off; else the plain
// rendering. arr is the column (per-row grid) or a list column's values
// (per-attribute grid, elem=true).
func (inst *PlayApp) glossCell(gc *glossColumn, arr arrow.Array, row int64, elem bool) (text string, tone gloss.ToneE) {
	ok := gc != nil && !inst.tableOpts.rawCells
	if ok {
		if elem {
			ok = gc.glossedElem()
		} else {
			ok = gc.glossed()
		}
	}
	if !ok {
		return gloss.FormatArrowElem(arr, row), gloss.ToneNeutral
	}
	cell := gloss.ArrowCell{Arr: arr, Row: int(row)}
	if cell.IsNull() {
		return "", gloss.ToneNeutral
	}
	face := gc.inst.Inline(cell)
	return face.Text, face.Tone
}

// glossText is the text-backed variant for a grid that already holds the
// cell as a string (the per-attribute grid's driver-marshalled values):
// the inline face over a TextCell of the given kind, or the text unchanged.
func (inst *PlayApp) glossText(gc *glossColumn, text string, kind gloss.ValueKindE) string {
	if gc == nil || inst.tableOpts.rawCells || !gc.glossedElem() || text == "" {
		return text
	}
	return gc.inst.Inline(gloss.TextCell{S: text, K: kind}).Text
}

// glossHover is the header hover's account of a column's resolution: what
// glosses it and from where, or why a declaration was refused. Empty for a
// plain, undeclared column.
func glossHover(gc *glossColumn) string {
	if gc == nil || gc.mediaType == "" {
		return ""
	}
	switch {
	case gc.reason != "":
		return "gloss " + gc.mediaType + " refused: " + gc.reason
	case gc.inst != nil && !gc.rowOK:
		return "gloss " + gc.mediaType + " (" + gc.source + ") — not applied: " + gc.rowReason
	default:
		return "glossed as " + gc.mediaType + " (" + gc.source + ")"
	}
}

// toneColor maps an inline face's tone onto the design system's semantic
// palette (ADR-0031). Neutral is "no colour": the cell keeps the text style
// it would have had, rather than a neutral grey that would read as disabled.
func toneColor(t gloss.ToneE) (col color.Color, ok bool) {
	var def styletokens.RGBA8
	switch t {
	case gloss.ToneInfo:
		def = styletokens.InfoDefault
	case gloss.ToneSuccess:
		def = styletokens.SuccessDefault
	case gloss.ToneWarning:
		def = styletokens.WarningDefault
	case gloss.ToneError:
		def = styletokens.ErrorDefault
	case gloss.ToneAccent:
		def = styletokens.AccentDefault
	default:
		return
	}
	return color.Hex(def.AsHex()), true
}

// anyGlossed reports whether at least one column of schema resolves to a
// gloss (accepted or not) — the condition for showing the raw toggle.
func (inst *PlayApp) anyGlossed(schema *arrow.Schema) bool {
	for i := range inst.glossColumns(schema) {
		if inst.glossRes.cols[i].inst != nil {
			return true
		}
	}
	return false
}

// renderGlossControl draws the raw-cells toggle in the Table tab's toolbar
// row when the result has a glossed column.
func (inst *PlayApp) renderGlossControl(schema *arrow.Schema) {
	if !inst.anyGlossed(schema) {
		return
	}
	for range c.HoverText("Show every cell as its plain value, ignoring the glosses (ADR-0186) — for this session.").KeepIter() {
		c.Checkbox(inst.ids.PrepareStr("table-raw-cells"),
			inst.tableOpts.rawCells, "Raw cells").
			SendRespVal(&inst.tableOpts.rawCells)
	}
}
