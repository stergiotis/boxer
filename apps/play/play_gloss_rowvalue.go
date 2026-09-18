package play

import (
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/hmi/gloss"
)

// play_gloss_rowvalue.go is ADR-0245 §SD2: row-value glosses. For a column
// whose gloss label is L, a text column named L_gloss is its companion — each
// row's value is a media-type token in the alias's spelling, and it outranks
// everything the column itself resolves to (alias, directive, rule set,
// affinity). A result that mixes kinds — a picture on one row, a recording on
// the next — says so per row, which no per-column binding can.
//
// ADR-0186 §SD1 binds a gloss once per column; this binds once per DISTINCT
// TOKEN per result, as a synthesized glossColumn, so everything downstream of
// a column resolution (inline faces, glossBlock, the artifact caches) takes
// it unchanged. The distinct tokens of one companion are capped: that is
// what bounds the cost of a column of garbage, which bind-once-per-column
// bounded by construction.

const (
	// rowGlossSuffix names a companion: `<label>_gloss`.
	rowGlossSuffix = "_gloss"
	// rowGlossMaxTokens caps the distinct tokens bound per companion column.
	rowGlossMaxTokens = 32
	// rowGlossSourcePrefix is the binding's provenance, as the hover and the
	// Glosses tab spell a source.
	rowGlossSourcePrefix = "row value: "
)

// isTextType reports whether a column can carry media-type tokens.
func isTextType(dt arrow.DataType) bool {
	switch dt.ID() {
	case arrow.STRING, arrow.LARGE_STRING, arrow.BINARY, arrow.LARGE_BINARY, arrow.STRING_VIEW, arrow.BINARY_VIEW:
		return true
	case arrow.DICTIONARY:
		return isTextType(dt.(*arrow.DictionaryType).ValueType)
	}
	return false
}

// rowGlossCompanions pairs each column with its companion, by gloss label:
// value column index → companion column index. A `<label>_gloss` column with
// no `<label>` beside it, or one that is not text, is an ordinary column.
// Schema-only, so a pane can run it in AcceptForChannel.
func rowGlossCompanions(schema *arrow.Schema) (companionOf map[int]int) {
	if schema == nil {
		return nil
	}
	var byLabel map[string]int
	for ci, f := range schema.Fields() {
		if !strings.HasSuffix(f.Name, rowGlossSuffix) || !isTextType(f.Type) {
			continue
		}
		if byLabel == nil {
			byLabel = make(map[string]int, schema.NumFields())
			for vi, vf := range schema.Fields() {
				if _, dup := byLabel[pathColumnLabel(vf.Name)]; !dup {
					byLabel[pathColumnLabel(vf.Name)] = vi
				}
			}
		}
		stem := strings.TrimSuffix(f.Name, rowGlossSuffix)
		if vi, ok := byLabel[stem]; ok && vi != ci {
			if companionOf == nil {
				companionOf = make(map[int]int, 2)
			}
			companionOf[vi] = ci
		}
	}
	return
}

// rowGlossBinder binds the tokens of one result's companions, once each.
type rowGlossBinder struct {
	catalog *gloss.Catalog
	// tokens is keyed by companion column, then by token.
	tokens map[int]map[string]*glossColumn
	// overflow is the resolution handed out past the cap, per companion.
	overflow map[int]*glossColumn
}

func newRowGlossBinder(catalog *gloss.Catalog) *rowGlossBinder {
	return &rowGlossBinder{catalog: catalog, tokens: make(map[int]map[string]*glossColumn, 2), overflow: make(map[int]*glossColumn, 1)}
}

// reset drops every binding — the result changed.
func (inst *rowGlossBinder) reset() {
	clear(inst.tokens)
	clear(inst.overflow)
}

// resolve returns the resolution of one cell: the row value's binding when
// the companion carries a token for this row, else base (the column's own
// resolution, which may be nil for a plain column).
//
// loud decides what a token without a slash means. Inside a reserved
// namespace (`card_*`) the intent is unambiguous, so anything that does not
// bind — `png`, `image/pgn`, `;unti=K` — is a resolution carrying the reason.
// Outside it ADR-0123 §SD2's slash gate applies per value: no slash is no
// declaration, silently, because a table may simply have columns named `lip`
// and `lip_gloss`.
func (inst *rowGlossBinder) resolve(rec arrow.RecordBatch, valueCol, companionCol int, row int64, base *glossColumn, loud bool) *glossColumn {
	cell := gloss.ArrowCell{Arr: rec.Column(companionCol), Row: int(row)}
	if cell.IsNull() {
		return base
	}
	token, ok := cell.Raw()
	if !ok {
		token = cell.Text()
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return base
	}
	if !loud && !strings.Contains(token, "/") {
		return base
	}
	byToken := inst.tokens[companionCol]
	if gc, bound := byToken[token]; bound {
		return gc
	}
	schema := rec.Schema()
	companion := schema.Field(companionCol).Name
	if len(byToken) >= rowGlossMaxTokens {
		gc, ok := inst.overflow[companionCol]
		if !ok {
			gc = &glossColumn{
				label:     pathColumnLabel(schema.Field(valueCol).Name),
				mediaType: "…",
				source:    rowGlossSourcePrefix + companion,
				reason:    fmt.Sprintf("`%s` carries more than %d distinct glosses; the rest render plain", companion, rowGlossMaxTokens),
			}
			inst.overflow[companionCol] = gc
		}
		return gc
	}
	if byToken == nil {
		byToken = make(map[string]*glossColumn, 4)
		inst.tokens[companionCol] = byToken
	}
	// The token aliases Arrow memory and is good for the frame; the map key
	// and everything the binding keeps are copies.
	token = strings.Clone(token)
	gc := inst.bind(schema.Field(valueCol), companion, token)
	byToken[token] = gc
	return gc
}

// bind is the once-per-token work: the catalog's own column parser over
// `<label>@<token>`, so a row value is refused in the words an alias is.
func (inst *rowGlossBinder) bind(field arrow.Field, companion string, token string) *glossColumn {
	label := pathColumnLabel(field.Name)
	gc := &glossColumn{label: label, source: rowGlossSourcePrefix + companion}
	d, declared := inst.catalog.ParseColumn(label + gloss.Sep + token)
	if !declared {
		gc.mediaType = token
		gc.reason = fmt.Sprintf("%q is not a media type (no slash) — e.g. image/png", token)
		return gc
	}
	gc.mediaType, gc.params = d.MediaType, d.Params
	if d.Reason != "" {
		gc.reason = d.Reason
		return gc
	}
	gc.inst = d.Instance
	gc.rowOK, gc.rowReason = gc.inst.Accepts(gloss.KindOfArrow(field.Type))
	gc.elemOK, gc.elemReason = gc.inst.Accepts(gloss.KindOfArrow(listElemType(field.Type)))
	return gc
}

// rowGlossState is the app's row-value resolution for the result on screen:
// the companions of its schema and the tokens bound so far. The Table grids,
// the ad-hoc Detail pane, the Chat bubble and the Cards pane share it
// (ADR-0245 §SD8 M6).
type rowGlossState struct {
	seen        bool
	forSchema   *arrow.Schema
	forResult   ResultID
	companionOf map[int]int
	companions  map[int]struct{}
	binder      *rowGlossBinder
}

// rowGlossSync returns the state for schema, rebuilt when the schema or the
// result changed. Nil when the schema pairs no column with a companion —
// nearly always, so the per-cell paths pay one pointer compare.
func (inst *PlayApp) rowGlossSync(schema *arrow.Schema) *rowGlossState {
	st := &inst.rowGlossSt
	if !st.seen || st.forSchema != schema || st.forResult != inst.frameResult {
		st.seen, st.forSchema, st.forResult = true, schema, inst.frameResult
		st.companionOf = rowGlossCompanions(schema)
		st.companions = nil
		if len(st.companionOf) > 0 {
			st.companions = make(map[int]struct{}, len(st.companionOf))
			for _, ci := range st.companionOf {
				st.companions[ci] = struct{}{}
			}
		}
		if st.binder == nil {
			st.binder = newRowGlossBinder(inst.glossCatalog())
		}
		st.binder.reset()
	}
	if len(st.companionOf) == 0 {
		return nil
	}
	return st
}

// rowGloss is one cell's resolution in the grids, Detail and Chat: the row
// value where the column has a companion carrying one, else the column's own
// (nil when cols does not reach col). Loud inside a reserved namespace,
// slash-gated outside it, as on a card.
func (inst *PlayApp) rowGloss(rec arrow.RecordBatch, cols []glossColumn, col int, row int64) *glossColumn {
	var base *glossColumn
	if col < len(cols) {
		base = &cols[col]
	}
	st := inst.rowGlossSync(rec.Schema())
	if st == nil || inst.tableOpts.rawCells {
		return base
	}
	comp, ok := st.companionOf[col]
	if !ok || row < 0 || row >= rec.NumRows() {
		return base
	}
	loud := strings.HasPrefix(pathColumnLabel(rec.Schema().Field(col).Name), cardgridPrefix)
	return st.binder.resolve(rec, col, comp, row, base, loud)
}

// isRowGlossCompanion reports whether col is some column's `<label>_gloss`.
func (inst *PlayApp) isRowGlossCompanion(schema *arrow.Schema, col int) bool {
	st := inst.rowGlossSync(schema)
	if st == nil {
		return false
	}
	_, ok := st.companions[col]
	return ok
}

// fromRowValue reports whether a resolution came from a companion column.
func (inst *glossColumn) fromRowValue() bool {
	return inst != nil && strings.HasPrefix(inst.source, rowGlossSourcePrefix)
}
