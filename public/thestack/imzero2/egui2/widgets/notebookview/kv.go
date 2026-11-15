package notebookview

import (
	"strconv"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/glossview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// RendererKV is the built-in renderer that draws a relation as labelled
// values: each row a block, each column a line of its label and its value.
// A value is shown through the gloss its column declares, in the gloss's
// tone; a NULL is left out; a `gloss/url` value is a link, reported in
// [Events.Links] when the host claims it. It is for the few facts of one
// thing, where a grid's header row and column widths say nothing.
const RendererKV = "kv"

// kvMaxRows bounds the blocks a `kv` cell draws: past it the relation is a
// table's.
const kvMaxRows = 24

// field is one column of a `kv` cell's relation.
type field struct {
	label string
	arr   arrow.Array
	// instance is the column's gloss, nil for a column that declares none
	// or one the catalog does not hold for its values.
	instance gloss.InstanceI
	// link says the column's values are URLs.
	link bool
}

func fieldsOf(rec arrow.RecordBatch, cat *gloss.Catalog) (fields []field) {
	fields = make([]field, 0, rec.NumCols())
	for i, f := range rec.Schema().Fields() {
		fl := field{label: f.Name, arr: rec.Column(i)}
		if d, declared := cat.ParseColumn(f.Name); declared {
			fl.label = d.Label
			if d.Instance != nil {
				if ok, _ := d.Instance.Accepts(gloss.KindOfArrow(fl.arr.DataType())); ok {
					fl.instance = d.Instance
					fl.link = d.MediaType == gloss.MediaTypeURL
				}
			}
		}
		fields = append(fields, fl)
	}
	return
}

// renderFields draws a `kv` cell.
func (inst *View) renderFields(cv *cellView) {
	rows := int(cv.result.Main.NumRows())
	if rows == 0 {
		weakLabel("no rows")
		return
	}
	shown := min(rows, kvMaxRows)
	for range c.ScrollArea().Vscroll(true).MaxHeight(inst.resultMaxHeight(cv)).AutoShrink(false, true).KeepIter() {
		for row := range shown {
			if row > 0 {
				c.Separator().Send()
			}
			for range c.IdScope(inst.ids.PrepareSeq(uint64(row))) {
				for range c.Grid(inst.ids.PrepareStr("fields")).NumColumns(2).KeepIter() {
					for i := range cv.fields {
						fl := &cv.fields[i]
						cell := gloss.ArrowCell{Arr: fl.arr, Row: row}
						if cell.IsNull() {
							continue
						}
						weakLabel(fl.label)
						inst.fieldValue(i, fl, cell)
						c.EndRow()
					}
				}
			}
		}
		if rows > shown {
			weakLabel("+" + strconv.Itoa(rows-shown) + " more rows")
		}
	}
}

// fieldValue draws one value: its gloss's face, a link for a URL, the
// plain text otherwise.
func (inst *View) fieldValue(i int, fl *field, cell gloss.ArrowCell) {
	face := gloss.Inline{Text: cell.Text()}
	if fl.instance != nil {
		face = fl.instance.Inline(cell)
	}
	if !fl.link {
		glossview.Face(face, "", glossview.Style{})
		return
	}
	url, _ := cell.Raw()
	if !inst.claims(url) || isVal(url) {
		glossview.Face(face, url, glossview.Style{})
		return
	}
	// A link the host takes: drawn here, since the host's hyperlink would
	// open it.
	fg := color.Hex(styletokens.InfoDefault.AsHex())
	if c.Button(inst.ids.PrepareSeq(uint64(i)), c.Atoms().BeginRichTextColored(fg, color.Transparent, face.Text).End().Keep()).
		Frame(false).SendResp().HasPrimaryClicked() {
		inst.events.Links = append(inst.events.Links, markdown.LinkClick{Label: face.Text, URL: url})
	}
}
