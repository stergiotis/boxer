package play

import (
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/leewaywidgets"
)

// play_gloss.go is ADR-0186's per-column resolution: which gloss, if any,
// shows each column of a result, from where, and whether the column's value
// kind is one the gloss accepts. Resolved once per (schema, directive set) —
// the schema-pointer cache colLabels and colWidths use, plus the buffer's
// `-- play: gloss` lines, re-scanned per frame like the enum hints — and read
// by both Table grids per cell.
//
// Precedence per column (§SD3): the explicit `<label>@<media type>` alias;
// else the first matching directive rule, in buffer order; else the first
// matching affinity, in catalog order; else plain. An aliased column is never
// offered to the rules.

// glossSourceAlias names the explicit-alias source in hover text.
const glossSourceAlias = "alias"

// glossMarkerPrefix is the directive keyword, matched case-insensitively after
// the `--` the way ADR-0124's `play: enum ` is: `-- play: gloss <token> <regex>`.
// A bare `-- play: gloss` is a directive too — an incomplete one, reported.
const glossMarkerPrefix = "play: gloss"

// glossDirective is one scanned directive line: the media-type token (the
// first whitespace-delimited word, compact `;k=v` form) and the pattern (the
// rest of the line, trimmed).
type glossDirective struct {
	line    int // 1-based buffer line, for the note
	token   string
	pattern string
}

// scanGlossDirectives reads every `-- play: gloss` line of the buffer, in
// order. Lines are returned even when incomplete — the compiler names what is
// wrong with them, so a typo is a note rather than a rule that silently
// applies to nothing.
func scanGlossDirectives(sql string) (ds []glossDirective) {
	lineNo := 0
	for line := range strings.SplitSeq(sql, "\n") {
		lineNo++
		ln := strings.TrimSpace(line)
		if !strings.HasPrefix(ln, "--") {
			continue
		}
		ln = strings.TrimSpace(strings.TrimPrefix(ln, "--"))
		lower := strings.ToLower(ln)
		if !strings.HasPrefix(lower, glossMarkerPrefix) {
			continue
		}
		if len(ln) > len(glossMarkerPrefix) && ln[len(glossMarkerPrefix)] != ' ' && ln[len(glossMarkerPrefix)] != '\t' {
			continue // `-- play: glossary` is somebody else's line
		}
		rest := strings.TrimSpace(ln[len(glossMarkerPrefix):])
		token, pattern, _ := strings.Cut(rest, " ")
		ds = append(ds, glossDirective{line: lineNo, token: strings.TrimSpace(token), pattern: strings.TrimSpace(pattern)})
	}
	return ds
}

// directivesKey is the cache key of a directive set: the lines themselves.
func directivesKey(ds []glossDirective) string {
	if len(ds) == 0 {
		return ""
	}
	var b strings.Builder
	for _, d := range ds {
		b.WriteString(strconv.Itoa(d.line))
		b.WriteByte(':')
		b.WriteString(d.token)
		b.WriteByte(' ')
		b.WriteString(d.pattern)
		b.WriteByte('\n')
	}
	return b.String()
}

// glossColumn is one column's resolution.
type glossColumn struct {
	// label is the header label a declaration supplies (`t` of `t@gloss/…`);
	// empty for an undeclared column, whose header keeps its physical name.
	label string
	// mediaType is the resolved gloss, empty when the column is plain; params
	// its bound parameters (a directive's or an alias's).
	mediaType string
	params    map[string]string
	// inst renders the cells; nil when plain or refused.
	inst gloss.InstanceI
	// source says where the binding came from: glossSourceAlias, or the
	// rule's provenance and pattern (`directive line 3: name:.*temp`,
	// `affinity: \bsem:secret\b`).
	source string
	// specLine is the column's written-out form the rules matched against
	// (ADR-0186 §SD3), shown on hover.
	specLine string
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

// glossResolution caches one (schema, directive set) resolution.
type glossResolution struct {
	forSchema  *arrow.Schema
	directives string // directivesKey of the buffer lines the rules came from
	rules      []gloss.Rule
	notes      []string // directive lines that did not compile, with why
	cols       []glossColumn
}

// glossColumns resolves (or returns the cached resolution of) schema against
// the buffer's current directives.
func (inst *PlayApp) glossColumns(schema *arrow.Schema) []glossColumn {
	if schema == nil {
		return nil
	}
	ds := scanGlossDirectives(inst.sql)
	key := directivesKey(ds)
	if inst.glossRes.forSchema == schema && inst.glossRes.directives == key && len(inst.glossRes.cols) == schema.NumFields() {
		return inst.glossRes.cols
	}
	cat := inst.glossCatalog()
	res := glossResolution{forSchema: schema, directives: key}
	for _, d := range ds {
		r, err := cat.CompileRule(d.token, d.pattern, "directive line "+strconv.Itoa(d.line))
		if err != nil {
			res.notes = append(res.notes, "-- play: gloss, line "+strconv.Itoa(d.line)+": "+err.Error())
			continue
		}
		res.rules = append(res.rules, r)
	}
	// Directive rules first, affinities after: list order is precedence.
	rules := append(res.rules, cat.AffinityRules()...)

	names := make([]string, schema.NumFields())
	for i := range names {
		names[i] = schema.Field(i).Name
	}
	specs := lwsql.SpecLines(names)
	res.cols = make([]glossColumn, schema.NumFields())
	for i := range res.cols {
		field := schema.Field(i)
		res.cols[i] = inst.resolveGlossColumn(field, specs[i]+" arrow:"+field.Type.String(), rules)
	}
	inst.glossRes = res
	return res.cols
}

// glossNotes returns the current resolution's directive notes.
func (inst *PlayApp) glossNotes() []string { return inst.glossRes.notes }

// resolveGlossColumn is the per-column resolution: alias, else first
// matching rule, else plain. It also settles the Accepts verdicts once, so
// the per-cell path is a field read.
func (inst *PlayApp) resolveGlossColumn(field arrow.Field, specLine string, rules []gloss.Rule) (gc glossColumn) {
	gc.specLine = specLine
	if d, declared := inst.glossCatalog().ParseColumn(field.Name); declared {
		gc.label = d.Label
		gc.mediaType = d.MediaType
		gc.params = d.Params
		gc.source = glossSourceAlias
		if d.Reason != "" {
			gc.reason = d.Reason
			return gc
		}
		gc.inst = d.Instance
	} else if r, ok := gloss.MatchFirst(rules, specLine); ok {
		gc.mediaType = r.MediaType
		gc.params = r.Params
		gc.source = r.Source + ": " + r.Pattern
		gc.inst = r.Instance
	} else {
		return gc
	}
	gc.rowOK, gc.rowReason = gc.inst.Accepts(gloss.KindOfArrow(field.Type))
	gc.elemOK, gc.elemReason = gc.inst.Accepts(gloss.KindOfArrow(listElemType(field.Type)))
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

// glossLink returns the URL a gloss/url cell opens — the value itself,
// trimmed — when the column is glossed as gloss/url, accepted, and the raw
// toggle is off; empty for every other cell. The grids render such a cell as
// a hyperlink instead of a selectable button.
func (inst *PlayApp) glossLink(gc *glossColumn, arr arrow.Array, row int64) string {
	if gc == nil || inst.tableOpts.rawCells || gc.mediaType != gloss.MediaTypeURL || !gc.glossed() {
		return ""
	}
	raw, ok := gloss.ArrowCell{Arr: arr, Row: int(row)}.Raw()
	if !ok {
		return ""
	}
	return strings.TrimSpace(raw)
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

// glossHover is the header hover's account of a column: what glosses it and
// from where, or why a declaration was refused, then the spec line the rules
// saw. Empty for a plain column with no spec line to show.
func glossHover(gc *glossColumn) string {
	if gc == nil {
		return ""
	}
	var head string
	token := gloss.CompactMediaType(gc.mediaType, gc.params)
	switch {
	case gc.mediaType == "":
	case gc.reason != "":
		head = "gloss " + token + " refused: " + gc.reason
	case gc.inst != nil && !gc.rowOK:
		head = "gloss " + token + " (" + gc.source + ") — not applied: " + gc.rowReason
	default:
		head = "glossed as " + token + " (" + gc.source + ")"
	}
	if gc.specLine == "" {
		return head
	}
	if head == "" {
		return "spec: " + gc.specLine
	}
	return head + " — spec: " + gc.specLine
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
// row when the result has a glossed column, and the directive notes — a
// `-- play: gloss` line that did not compile, with why — beside it.
func (inst *PlayApp) renderGlossControl(schema *arrow.Schema) {
	glossed := inst.anyGlossed(schema)
	notes := inst.glossNotes()
	if !glossed && len(notes) == 0 {
		return
	}
	if glossed {
		for range c.HoverText("Show every cell as its plain value, ignoring the glosses (ADR-0186) — for this session.").KeepIter() {
			c.Checkbox(inst.ids.PrepareStr("table-raw-cells"),
				inst.tableOpts.rawCells, "Raw cells").
				SendRespVal(&inst.tableOpts.rawCells)
		}
	}
	for _, n := range notes {
		for rt := range c.RichTextLabel(n) {
			rt.Small().Weak()
		}
	}
}

// declaration is the column's resolution as the Detail pane's renderRichCell
// consumes it: a caption (the alias label, else the caller's), the media type
// and instance, or the reason nothing renders. ok is false for a plain
// column.
func (inst *glossColumn) declaration(caption string) (d gloss.Declaration, ok bool) {
	if inst.mediaType == "" {
		return d, false
	}
	if inst.label != "" {
		caption = inst.label
	}
	return gloss.Declaration{
		Label:     caption,
		MediaType: gloss.CompactMediaType(inst.mediaType, inst.params),
		Params:    inst.params,
		Instance:  inst.inst,
		Reason:    inst.reason,
	}, true
}

// cardCellGloss is the leeway card's per-value gloss (Table2CardEmitter's
// SetCellGloss seam): the same resolution the grids use, applied to the
// marshalled text of each value with the column's element kind. Nil when
// nothing in the schema is glossed or the raw toggle is on, so the emitter
// skips the rewrite entirely.
func (inst *PlayApp) cardCellGloss(schema *arrow.Schema) leewaywidgets.CellGlossFunc {
	if inst.tableOpts.rawCells || !inst.anyGlossed(schema) {
		return nil
	}
	cols := inst.glossColumns(schema)
	return func(arrowIdx int, text string) string {
		if arrowIdx < 0 || arrowIdx >= len(cols) {
			return text
		}
		return inst.glossText(&cols[arrowIdx], text, gloss.KindOfArrow(listElemType(schema.Field(arrowIdx).Type)))
	}
}
