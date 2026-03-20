//go:build llm_generated_opus46

package card

import (
	"fmt"
	"html"
	"image/color"
	"io"
	"strings"

	"github.com/dim13/colormap"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
)

var _ streamreadaccess.SinkI = (*HtmlCardEmitter)(nil)

// HtmlCardEmitter renders Leeway entities using an "attribute card" layout.
// Implements StructuredOutput2I.
//
// Names are emitted via StylableName.String() — the IR is assumed to carry
// the desired naming style already.
type HtmlCardEmitter struct {
	w       io.Writer
	palette color.Palette

	entityIdx   int
	sectionIdx  int
	sectionName string
	colNames    []naming.StylableName
	colTypes    []canonicaltypes.PrimitiveAstNodeI
	nAttrs      int
	accentColor string

	// Current column state
	currentColName naming.StylableName
	currentColType canonicaltypes.PrimitiveAstNodeI
	cellBuf        strings.Builder
	inCollection   bool
	collType       int // 1=array, 2=set
	itemIdx        int

	// Tag accumulation (flushed at EndTaggedValue)
	tagBuf strings.Builder

	err error
}

type ColorPaletteE int

const (
	HtmlPaletteInferno ColorPaletteE = iota
	ColorPaletteViridis
	ColorPaletteMagma
	ColorPalettePlasma
)

func NewHtmlCardEmitter(w io.Writer, palette ColorPaletteE) (inst *HtmlCardEmitter) {
	var pal color.Palette
	switch palette {
	case ColorPaletteViridis:
		pal = colormap.Viridis
	case ColorPaletteMagma:
		pal = colormap.Magma
	case ColorPalettePlasma:
		pal = colormap.Plasma
	default:
		pal = colormap.Inferno
	}
	inst = &HtmlCardEmitter{
		w:       w,
		palette: pal,
	}
	return
}

func (inst *HtmlCardEmitter) sectionColor(idx int) (hexColor string) {
	n := len(inst.palette)
	if n == 0 {
		return "#888888"
	}
	lo := n / 5
	hi := n * 4 / 5
	span := hi - lo
	if span <= 0 {
		span = 1
	}
	pos := lo + (idx*37)%span
	if pos >= n {
		pos = pos % n
	}
	r, g, b, _ := inst.palette[pos].RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

func canonicalTypeStr(ct canonicaltypes.PrimitiveAstNodeI) string {
	if ct != nil {
		return ct.String()
	}
	return ""
}

func (inst *HtmlCardEmitter) write(s string) {
	if inst.err != nil {
		return
	}
	_, err := io.WriteString(inst.w, s)
	if err != nil {
		inst.err = err
	}
}

func (inst *HtmlCardEmitter) writef(format string, args ...any) {
	if inst.err != nil {
		return
	}
	_, err := fmt.Fprintf(inst.w, format, args...)
	if err != nil {
		inst.err = err
	}
}

// --- Batch ---

func (inst *HtmlCardEmitter) BeginBatch() {
	inst.entityIdx = 0
	inst.sectionIdx = 0
	inst.write(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Leeway Record View</title>
<style>
:root {
  --bg: #0d0d11;
  --bg-entity: #131318;
  --bg-card: #1a1a22;
  --bg-card-hover: #1f1f2a;
  --bg-kv-alt: rgba(255,255,255,0.02);
  --fg: #c8c8d4;
  --fg-dim: #70708a;
  --fg-key: #9090aa;
  --fg-val: #e8e8f4;
  --fg-bright: #f4f4ff;
  --fg-type: #585870;
  --border: #252535;
  --border-light: #1e1e2c;
  --tag-bg: rgba(255,255,255,0.04);
  --tag-border: #303045;
  --mono: 'JetBrains Mono','Fira Code','Cascadia Code','SF Mono','Consolas',monospace;
  --sans: 'Inter','Segoe UI',system-ui,sans-serif;
  --radius: 6px;
  --card-width: 280px;
}
*{margin:0;padding:0;box-sizing:border-box;}
body{
  background:var(--bg);color:var(--fg);
  font-family:var(--sans);font-size:13px;line-height:1.5;
  padding:12px 16px;
}
.batch{max-width:1600px;margin:0 auto;}

/* Entity accordion */
.entity{
  background:var(--bg-entity);
  border:1px solid var(--border);
  border-radius:8px;
  margin-bottom:10px;
}
.entity>summary{
  padding:8px 14px;cursor:pointer;
  font-weight:600;font-size:13px;color:var(--fg-bright);
  list-style:none;user-select:none;
  display:flex;align-items:center;gap:6px;
}
.entity>summary::before{
  content:'▶';font-size:9px;
  transition:transform .15s;display:inline-block;width:12px;
  color:var(--fg-dim);
}
.entity[open]>summary::before{transform:rotate(90deg);}
.entity-body{padding:6px 10px 10px;display:flex;flex-wrap:wrap;gap:6px;align-items:flex-start;}

/* Section group */
.sec-group{margin-bottom:2px;flex:0 1 auto;min-width:200px;}
.sec-label{
  font-family:var(--mono);font-size:11px;font-weight:600;
  color:var(--fg-dim);
  padding:3px 0 4px;
  display:flex;align-items:center;gap:6px;
  letter-spacing:0.3px;
}
.sec-dot{
  width:7px;height:7px;border-radius:50%;flex-shrink:0;
}
.sec-count{
  font-weight:400;opacity:0.5;font-size:10px;
}
.sec-kind{
  font-weight:400;opacity:0.4;font-size:9px;
  text-transform:uppercase;letter-spacing:0.5px;
}

/* Attribute card grid */
.attr-grid{
  display:flex;flex-wrap:wrap;gap:6px;
  padding:2px 0;
  align-items:flex-start;
}

/* Single attribute card */
.attr-card{
  background:var(--bg-card);
  border:1px solid var(--border);
  border-radius:var(--radius);
  min-width:180px;
  flex:0 1 var(--card-width);
  overflow:hidden;
  transition:background .1s;
}
.attr-card:hover{background:var(--bg-card-hover);}
.attr-card-accent{
  display:flex;align-items:center;gap:6px;
  padding:3px 8px;
  font-family:var(--mono);font-size:10px;
  font-weight:600;letter-spacing:0.3px;
  color:rgba(255,255,255,0.85);
}

/* Key-value rows inside card */
.kv-table{width:100%;border-collapse:collapse;}
.kv-table tr:nth-child(even){background:var(--bg-kv-alt);}
.kv-table td{
  padding:2px 8px;
  vertical-align:top;
  font-family:var(--mono);font-size:11px;
}
.kv-key{
  color:var(--fg-key);white-space:nowrap;
  width:1%;padding-right:4px;
}
.kv-type{
  color:var(--fg-type);font-size:10px;
  font-weight:400;margin-left:2px;
}
.kv-val{
  color:var(--fg-val);
  word-break:break-all;
  max-width:220px;
}

/* Collections inside value cells */
.coll{padding:0;margin:0;}
.coll-item{display:block;padding:0 0 1px;}
.coll-item::before{color:var(--fg-dim);margin-right:3px;font-size:10px;}
.coll-item.arr::before{content:attr(data-idx)']';}
.coll-item.set::before{content:'•';}

/* Tag footer */
.attr-tags{
  padding:3px 6px 4px;
  border-top:1px solid var(--border-light);
  display:flex;flex-wrap:wrap;gap:3px;
}
.tag{
  display:inline-flex;align-items:center;gap:3px;
  padding:1px 5px;border-radius:3px;
  font-family:var(--mono);font-size:10px;
  background:var(--tag-bg);
  border:1px solid var(--tag-border);
  color:var(--fg-dim);white-space:nowrap;
}
.tag-t{opacity:0.5;}

/* Co-section group */
.co-group{
  border:1px dashed var(--border);
  border-radius:var(--radius);
  padding:4px 6px;margin-bottom:2px;
  flex:0 1 auto;min-width:200px;
}
.co-group-label{
  font-size:10px;color:var(--fg-dim);
  text-transform:uppercase;letter-spacing:0.5px;
  padding:0 2px 3px;
}

/* Wide cards for sections with many columns */
.attr-card.wide{
  flex-basis:100%;min-width:100%;
}
</style>
</head>
<body>
<div class="batch">
`)
}

func (inst *HtmlCardEmitter) EndBatch() (err error) {
	inst.write("</div>\n</body>\n</html>\n")
	return inst.err
}

// --- Entity ---

func (inst *HtmlCardEmitter) BeginEntity() {
	inst.writef("<details class=\"entity\" open>\n<summary>Entity %d</summary>\n<div class=\"entity-body\">\n", inst.entityIdx)
	inst.entityIdx++
	inst.sectionIdx = 0
}

func (inst *HtmlCardEmitter) EndEntity() (err error) {
	inst.write("</div>\n</details>\n")
	return inst.err
}

// --- Plain section ---

func (inst *HtmlCardEmitter) BeginPlainSection(itemType common.PlainItemTypeE, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	inst.sectionName = itemType.String()
	inst.colNames = valueNames
	inst.colTypes = valueCanonicalTypes
	inst.nAttrs = nAttrs
	inst.accentColor = inst.sectionColor(inst.sectionIdx)
	inst.sectionIdx++

	if nAttrs == 0 {
		return
	}

	inst.write("<div class=\"sec-group\">\n")
	inst.writef("<div class=\"sec-label\"><span class=\"sec-dot\" style=\"background:%s\"></span>%s <span class=\"sec-kind\">plain</span></div>\n",
		inst.accentColor, html.EscapeString(inst.sectionName))
	inst.write("<div class=\"attr-grid\">\n")
}

func (inst *HtmlCardEmitter) EndPlainSection() (err error) {
	if inst.nAttrs == 0 {
		return inst.err
	}
	inst.write("</div>\n</div>\n") // attr-grid + sec-group
	return inst.err
}

// --- Plain value ---

func (inst *HtmlCardEmitter) BeginPlainValue() {
	wideClass := ""
	if len(inst.colNames) > 3 {
		wideClass = " wide"
	}
	inst.writef("<div class=\"attr-card%s\">\n", wideClass)
	//inst.writef("<div class=\"attr-card-accent\" style=\"background:%s\">%s</div>\n",
	//	inst.accentColor, html.EscapeString(inst.sectionName))
	inst.writef("<div class=\"attr-card-accent\" style=\"background:%s\"></div>\n",
		inst.accentColor)
	inst.write("<table class=\"kv-table\">\n")
}

func (inst *HtmlCardEmitter) EndPlainValue() (err error) {
	inst.write("</table>\n")
	inst.write("</div>\n") // attr-card (no tag footer for plain values)
	return inst.err
}

// --- Tagged sections scope ---

func (inst *HtmlCardEmitter) BeginTaggedSections() {
	// No structural HTML needed — plain and tagged sections both flow in entity-body
}

func (inst *HtmlCardEmitter) EndTaggedSections() (err error) {
	return inst.err
}

// --- Co-section group ---

func (inst *HtmlCardEmitter) BeginCoSectionGroup(name naming.Key) {
	inst.writef("<div class=\"co-group\">\n<div class=\"co-group-label\">co: %s</div>\n", html.EscapeString(string(name)))
}

func (inst *HtmlCardEmitter) EndCoSectionGroup() (err error) {
	inst.write("</div>\n")
	return inst.err
}

// --- Section ---

func (inst *HtmlCardEmitter) BeginSection(name naming.StylableName, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	inst.sectionName = name.String()
	inst.colNames = valueNames
	inst.colTypes = valueCanonicalTypes
	inst.nAttrs = nAttrs
	inst.accentColor = inst.sectionColor(inst.sectionIdx)
	inst.sectionIdx++

	if nAttrs == 0 {
		return
	}

	inst.write("<div class=\"sec-group\">\n")
	inst.writef("<div class=\"sec-label\"><span class=\"sec-dot\" style=\"background:%s\"></span>%s <span class=\"sec-count\">(%d)</span></div>\n",
		inst.accentColor, html.EscapeString(inst.sectionName), nAttrs)
	inst.write("<div class=\"attr-grid\">\n")
}

func (inst *HtmlCardEmitter) EndSection() (err error) {
	if inst.nAttrs == 0 {
		return inst.err
	}
	inst.write("</div>\n</div>\n")
	return inst.err
}

// --- Tagged value (attribute card) ---

func (inst *HtmlCardEmitter) BeginTaggedValue() {
	wideClass := ""
	if len(inst.colNames) > 3 {
		wideClass = " wide"
	}
	inst.writef("<div class=\"attr-card%s\">\n", wideClass)
	//inst.writef("<div class=\"attr-card-accent\" style=\"background:%s\">%s</div>\n",
	//	inst.accentColor, html.EscapeString(inst.sectionName))
	inst.writef("<div class=\"attr-card-accent\" style=\"background:%s\"></div>\n",
		inst.accentColor)
	inst.write("<table class=\"kv-table\">\n")
	inst.tagBuf.Reset()
}

func (inst *HtmlCardEmitter) EndTaggedValue() (err error) {
	inst.write("</table>\n")
	if inst.tagBuf.Len() > 0 {
		inst.write("<div class=\"attr-tags\">")
		inst.write(inst.tagBuf.String())
		inst.write("</div>\n")
	}
	inst.write("</div>\n")
	return inst.err
}

// --- Column ---

func (inst *HtmlCardEmitter) BeginColumn(colAddr streamreadaccess.PhysicalColumnAddr, name naming.StylableName, canonicalType canonicaltypes.PrimitiveAstNodeI) {
	inst.currentColName = name
	inst.currentColType = canonicalType
	inst.cellBuf.Reset()
	inst.inCollection = false
}

func (inst *HtmlCardEmitter) EndColumn() {
	inst.writef("<tr><td class=\"kv-key\">%s<span class=\"kv-type\"> %s</span></td><td class=\"kv-val\">%s</td></tr>\n",
		html.EscapeString(inst.currentColName.String()),
		html.EscapeString(canonicalTypeStr(inst.currentColType)),
		inst.cellBuf.String())
	inst.inCollection = false
}

// --- Scalar ---

func (inst *HtmlCardEmitter) BeginScalarValue() {
	inst.inCollection = false
}

func (inst *HtmlCardEmitter) EndScalarValue() (err error) {
	return inst.err
}

// --- Array ---

func (inst *HtmlCardEmitter) BeginHomogenousArrayValue(card int) {
	inst.inCollection = true
	inst.collType = 1
	inst.cellBuf.WriteString("<div class=\"coll\">")
}

func (inst *HtmlCardEmitter) EndHomogenousArrayValue() {
	inst.cellBuf.WriteString("</div>")
	inst.inCollection = false
}

// --- Set ---

func (inst *HtmlCardEmitter) BeginSetValue(card int) {
	inst.inCollection = true
	inst.collType = 2
	inst.cellBuf.WriteString("<div class=\"coll\">")
}

func (inst *HtmlCardEmitter) EndSetValue() {
	inst.cellBuf.WriteString("</div>")
	inst.inCollection = false
}

// --- Value item ---

func (inst *HtmlCardEmitter) BeginValueItem(index int) {
	inst.itemIdx = index
	switch inst.collType {
	case 1:
		fmt.Fprintf(&inst.cellBuf, "<span class=\"coll-item arr\" data-idx=\"[%d\">", index)
	case 2:
		inst.cellBuf.WriteString("<span class=\"coll-item set\">")
	}
}

func (inst *HtmlCardEmitter) EndValueItem() {
	inst.cellBuf.WriteString("</span>")
}

// --- Write ---

func (inst *HtmlCardEmitter) Write(p []byte) (n int, err error) {
	return inst.WriteString(string(p))
}

func (inst *HtmlCardEmitter) WriteString(s string) (n int, err error) {
	n = len(s)
	inst.cellBuf.WriteString(html.EscapeString(s))
	return
}

// --- Tags (accumulated into tagBuf, flushed at EndTaggedValue) ---

func (inst *HtmlCardEmitter) BeginTags(nTags int) {}
func (inst *HtmlCardEmitter) EndTags()            {}

func (inst *HtmlCardEmitter) AddMembershipRef(lowCard bool, ref uint64, humanReadableRef string) {
	c := "H"
	if lowCard {
		c = "L"
	}
	fmt.Fprintf(&inst.tagBuf, "<span class=\"tag\"><span class=\"tag-t\">ref(%s)</span> %s</span>", c, html.EscapeString(humanReadableRef))
}

func (inst *HtmlCardEmitter) AddMembershipVerbatim(lowCard bool, verbatim string, humanReadableVerbatim string) {
	c := "H"
	if lowCard {
		c = "L"
	}
	fmt.Fprintf(&inst.tagBuf, "<span class=\"tag\"><span class=\"tag-t\">v(%s)</span> %s</span>", c, html.EscapeString(humanReadableVerbatim))
}

func (inst *HtmlCardEmitter) AddMembershipRefParametrized(lowCard bool, ref uint64, humanReadableRef string, params string, humanReadableParams string) {
	c := "H"
	if lowCard {
		c = "L"
	}
	if humanReadableParams != "" {
		fmt.Fprintf(&inst.tagBuf, "<span class=\"tag\"><span class=\"tag-t\">rp(%s)</span> %s<span class=\"tag-t\">(%s)</span></span>",
			c, html.EscapeString(humanReadableRef), html.EscapeString(humanReadableParams))
	} else {
		fmt.Fprintf(&inst.tagBuf, "<span class=\"tag\"><span class=\"tag-t\">rp(%s)</span> %s</span>", c, html.EscapeString(humanReadableRef))
	}
}

func (inst *HtmlCardEmitter) AddMembershipMixedLowCardRefHighCardParam(ref uint64, humanReadableRef string, params string, humanReadableParams string) {
	if humanReadableParams != "" {
		fmt.Fprintf(&inst.tagBuf, "<span class=\"tag\"><span class=\"tag-t\">mr</span> %s<span class=\"tag-t\">(%s)</span></span>",
			html.EscapeString(humanReadableRef), html.EscapeString(humanReadableParams))
	} else {
		fmt.Fprintf(&inst.tagBuf, "<span class=\"tag\"><span class=\"tag-t\">mr</span> %s</span>", html.EscapeString(humanReadableRef))
	}
}

func (inst *HtmlCardEmitter) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, humanReadableVerbatim string, params string, humanReadableParams string) {
	if humanReadableParams != "" {
		fmt.Fprintf(&inst.tagBuf, "<span class=\"tag\"><span class=\"tag-t\">mv</span> %s<span class=\"tag-t\">(%s)</span></span>",
			html.EscapeString(humanReadableVerbatim), html.EscapeString(humanReadableParams))
	} else {
		fmt.Fprintf(&inst.tagBuf, "<span class=\"tag\"><span class=\"tag-t\">mv</span> %s</span>", html.EscapeString(humanReadableVerbatim))
	}
}
