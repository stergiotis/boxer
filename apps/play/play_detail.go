package play

import (
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/identity/identifier"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// tagNameForIdTagValue renders an entity id's tag value as a compact hex
// label (e.g. "tv:0x1a"). The playground queries arbitrary leeway tables
// whose id-tag values are domain-specific, and there is no general (non-VCS)
// tag-value name registry to resolve them against, so a hex label is the
// honest, schema-agnostic rendering. (Earlier scaffolding reached for
// vdd.VcsTagValueRegistry, but that only names VCS-domain tags —
// codeLine/gitHash — never the drone/cyber/sensor entities shown here.)
func tagNameForIdTagValue(tv uint64) string {
	return fmt.Sprintf("tv:0x%x", tv)
}

// extractTaggedId pulls the plain "id:id:…" u64 column value from a record row.
func extractTaggedId(rec arrow.RecordBatch, row int64) (identifier.TaggedId, bool) {
	schema := rec.Schema()
	for i := 0; i < schema.NumFields(); i++ {
		name := schema.Field(i).Name
		if !strings.HasPrefix(name, "id:id:") {
			continue
		}
		col := rec.Column(i)
		if row < 0 || int(row) >= col.Len() || col.IsNull(int(row)) {
			return 0, false
		}
		switch a := col.(type) {
		case *array.Uint64:
			return identifier.TaggedId(a.Value(int(row))), true
		default:
			s := formatCell(rec, i, row)
			var u uint64
			_, err := fmt.Sscanf(s, "%d", &u)
			if err == nil {
				return identifier.TaggedId(u), true
			}
		}
	}
	return 0, false
}

// Field section names derived from the column-name prefix.
const (
	sectionPlain      = "plain"
	sectionForeignKey = "relations"
	sectionData       = "data"
	sectionRare       = "meta"
)

// sectionForColumn derives a UI grouping bucket from a column name.
// Spinnaker column names are leeway-encoded with a prefix like:
//
//	"id:id:…", "id:naturalKey:…"                           → plain
//	"tv:foreignKey:value:…", "tv:foreignKey:lr:…"          → relations
//	"tv:text:…", "tv:string:…", "tv:symbol:…",
//	"tv:u64:…", "tv:u32:…", … (integer/float widths)       → data
//	"tv:time:…", "tv:bool:…", "tv:u32Range:…"              → meta
func sectionForColumn(name string) string {
	switch {
	case strings.HasPrefix(name, "id:"):
		return sectionPlain
	case strings.HasPrefix(name, "tv:foreignKey:"):
		return sectionForeignKey
	case strings.HasPrefix(name, "tv:time:"),
		strings.HasPrefix(name, "tv:bool:"),
		strings.HasPrefix(name, "tv:u32Range:"):
		return sectionRare
	case strings.HasPrefix(name, "tv:"):
		return sectionData
	}
	return sectionData
}

// shortColumnLabel strips leeway encoding aspects so a column name can be
// shown compactly in a detail card (e.g.
// "tv:foreignKey:value:val:u64:g:1d0DV72:0:0::" → "foreignKey.value").
func shortColumnLabel(name string) string {
	if strings.HasPrefix(name, "id:") {
		// "id:id:..." → "id"; "id:naturalKey:..." → "naturalKey".
		parts := strings.SplitN(name, ":", 3)
		if len(parts) >= 2 {
			return parts[1]
		}
		return name
	}
	if strings.HasPrefix(name, "tv:") {
		parts := strings.SplitN(name, ":", 4)
		if len(parts) >= 3 {
			return parts[1] + "." + parts[2]
		}
	}
	return name
}

// DetailContentFunc renders the body of the Detail panel for one selected row,
// below the header (row position + entity identity) PlayApp always draws. It
// runs inside the pane's Vertical scope; rec/schema/row identify the row. A
// library re-using PlayApp installs one with SetDetailContent to replace the
// built-in leeway-card / ad-hoc body with a domain-specific view — or to wrap
// it, by calling RenderDefaultDetailContent and then appending its own widgets.
type DetailContentFunc func(rec arrow.RecordBatch, schema *arrow.Schema, row int64)

// SetDetailContent overrides the Detail panel's body renderer. Passing nil
// restores the built-in body (RenderDefaultDetailContent). The header PlayApp
// draws above the body is unaffected.
func (inst *PlayApp) SetDetailContent(fn DetailContentFunc) {
	inst.detailContent = fn
}

// renderDetailPane renders the right-hand detail card for the selected row: a
// fixed header (row position + entity identity) followed by the pluggable body.
// The body is inst.detailContent when a re-user installed one, else the built-in
// RenderDefaultDetailContent.
func (inst *PlayApp) renderDetailPane(rec arrow.RecordBatch, schema *arrow.Schema, row int64) {
	for range c.Vertical().KeepIter() {
		entityLabel, natKey := entityHeader(rec, row)

		c.Label(fmt.Sprintf("detail · row %d / %d",
			row+1, rec.NumRows())).Send()
		c.Separator().Horizontal().Send()

		for range c.Horizontal().KeepIter() {
			for rt := range c.RichTextLabel(entityLabel) {
				rt.Strong()
			}
			if natKey != "" {
				c.Label("·").Send()
				c.Label(natKey).Truncate().Send()
			}
		}

		if inst.detailContent != nil {
			inst.detailContent(rec, schema, row)
		} else {
			inst.RenderDefaultDetailContent(rec, schema, row)
		}
	}
}

// RenderDefaultDetailContent is the built-in Detail body: the leeway card stack
// when the schema is leeway-shaped (co-sections, real tags, per-type
// formatters), else a prefix-grouped ad-hoc section layout for arbitrary SQL
// results. Exported so a custom DetailContentFunc can delegate to it and append
// its own widgets.
//
// The leeway card view (Table2CardEmitter) renders into an
// egui_extras::TableBuilder that owns its own ScrollArea, so it must NOT be
// wrapped in an outer ScrollArea: that hands the table unbounded available
// height and egui_extras then crops its tail rows. The driver emits the plain
// section first and the tagged / co-sections after it, so the cropped rows are
// exactly the tagged sections — leaving "only plain value sections" visible.
// Render the card directly in the bounded dock tab, matching the leewaywidgets
// demo's renderActiveView. The ad-hoc fallback has no self-scrolling widget, so
// it keeps an explicit ScrollArea.
func (inst *PlayApp) RenderDefaultDetailContent(rec arrow.RecordBatch, schema *arrow.Schema, row int64) {
	switch {
	case inst.cards != nil && inst.cards.EnsureFor(schema):
		if err := inst.cards.Render(rec, row); err != nil {
			c.Label(fmt.Sprintf("card render error: %s", err)).Wrap().Send()
		}
	default:
		for range c.ScrollArea().Vscroll(true).Hscroll(true).KeepIter() {
			inst.renderAdHocDetail(rec, schema, row)
		}
	}
}

// renderAdHocDetail is the fallback path for non-leeway schemas: it groups
// columns by prefix into pinned / relations / data / meta sections.
func (inst *PlayApp) renderAdHocDetail(rec arrow.RecordBatch, schema *arrow.Schema, row int64) {
	inst.renderDetailSection(rec, schema, row, sectionPlain, "pinned")
	inst.renderDetailSection(rec, schema, row, sectionForeignKey, "relations")
	inst.renderDetailSection(rec, schema, row, sectionData, "data")
	inst.renderDetailSection(rec, schema, row, sectionRare, "meta")
}

// entityHeader resolves the row's entity type + natural key for the card head.
func entityHeader(rec arrow.RecordBatch, row int64) (typeLabel string, natKey string) {
	if id, ok := extractTaggedId(rec, row); ok {
		tv := uint64(id.GetTag().GetValue())
		name := tagNameForIdTagValue(tv)
		typeLabel = fmt.Sprintf("[%s]", name)
	} else {
		typeLabel = "[?]"
	}
	schema := rec.Schema()
	for i := 0; i < schema.NumFields(); i++ {
		if strings.HasPrefix(schema.Field(i).Name, "id:naturalKey:") {
			natKey = formatCell(rec, i, row)
			break
		}
	}
	return
}

// renderDetailSection prints all non-empty columns whose sectionForColumn
// matches `section`. Skipped columns save vertical space in the card.
func (inst *PlayApp) renderDetailSection(rec arrow.RecordBatch, schema *arrow.Schema, row int64, section, heading string) {
	rowsShown := 0
	for i := 0; i < schema.NumFields(); i++ {
		name := schema.Field(i).Name
		if sectionForColumn(name) != section {
			continue
		}
		val := formatCell(rec, i, row)
		if val == "" || val == "[len=0]" {
			continue
		}
		if rowsShown == 0 {
			c.Separator().Horizontal().Send()
			for rt := range c.RichTextLabel(strings.ToUpper(heading)) {
				rt.Small().Weak()
			}
		}
		rowsShown++
		for range c.Horizontal().KeepIter() {
			for rt := range c.RichTextLabel(shortColumnLabel(name)) {
				rt.Weak()
			}
			c.Label(val).Truncate().Send()
		}
	}
}
