package leewaywidgets

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/lwread"
)

func scalar(col string, idx int, raw, text string) lwread.Value {
	return lwread.Value{Column: col, ArrowIdx: idx, Items: []lwread.Item{{Raw: raw, Text: text}}}
}

func list(col string, idx int, items ...string) lwread.Value {
	v := lwread.Value{Column: col, ArrowIdx: idx, Shape: lwread.ShapeList}
	for _, it := range items {
		v.Items = append(v.Items, lwread.Item{Raw: it, Text: it})
	}
	return v
}

func oneRecord(attrs ...lwread.Attribute) *lwread.Model {
	return &lwread.Model{Records: []lwread.Record{{Attributes: attrs}}}
}

func dataRows(card *RecordCard) (rows []table2UnifiedRow) {
	for _, r := range card.unified {
		if r.kind == rowKindData {
			rows = append(rows, r)
		}
	}
	return
}

// The gloss rewrites each item, keyed by the column's Arrow index, from the
// driver's text; a value it leaves alone is drawn in the read model's
// spelling; the joiner is never handed over.
func TestRecordCardGlossSeam(t *testing.T) {
	card := NewRecordCard(nil, "", ColorPaletteViridis)
	named := &lwread.Membership{Name: "sensor", Text: "sensor"}
	m := oneRecord(lwread.Attribute{Section: "obs", Name: "sensor", Named: named, Values: []lwread.Value{
		scalar("temperature", 7, "21.5", "21.5"),
		list("reading", 9, "a", "b"),
		scalar("key", 11, "aG9zdA==", "host"),
	}})
	card.SetCellGloss(func(arrowIdx int, text string) string {
		if arrowIdx == 7 {
			return text + " °C"
		}
		return text
	})
	card.Prepare(m)
	rows := dataRows(card)
	require.Len(t, rows, 1)
	assert.Equal(t, []table2NamedValue{{name: "temperature", value: "21.5 °C"}, {name: "reading", value: "a, b"}, {name: "key", value: "host"}},
		rows[0].valuePairs, "an unglossed value reads in the read model's spelling")

	var seen []string
	card.SetCellGloss(func(_ int, text string) string { seen = append(seen, text); return "[" + text + "]" })
	card.Prepare(oneRecord(lwread.Attribute{Section: "obs", Values: []lwread.Value{list("v", 7, "x", "y"), scalar("key", 11, "aG9zdA==", "host")}}))
	assert.Equal(t, []string{"x", "y", "aG9zdA=="}, seen, "per item, from the driver's text")
	assert.Equal(t, "[x], [y]", dataRows(card)[0].valuePairs[0].value)

	card.SetCellGloss(nil)
	card.Prepare(m)
	assert.Equal(t, "21.5", dataRows(card)[0].valuePairs[0].value)
}

// The block seam sees each item's driver text before the gloss, a claim
// attaches to the pair per item, and the row grows by what the blocks ask
// for — clamped, with a caption only when the row has more than one pair.
func TestRecordCardBlockSeam(t *testing.T) {
	card := NewRecordCard(nil, "", ColorPaletteViridis)
	var blockSaw []string
	card.SetCellGloss(func(_ int, text string) string { return "first line of " + text })
	card.SetCellBlock(func(arrowIdx int, text string) (CellBlock, bool) {
		blockSaw = append(blockSaw, text)
		if arrowIdx != 3 {
			return CellBlock{}, false
		}
		return CellBlock{Render: func() {}, Height: 100}, true
	})
	card.Prepare(oneRecord(lwread.Attribute{Section: "doc", Values: []lwread.Value{list("body", 3, "# doc one", "# doc two"), scalar("size", 5, "12", "12")}}))
	assert.Equal(t, []string{"# doc one", "# doc two", "12"}, blockSaw)
	rows := dataRows(card)
	require.Len(t, rows, 1)
	body, size := rows[0].valuePairs[0], rows[0].valuePairs[1]
	assert.Len(t, body.blocks, 2)
	assert.Equal(t, "first line of # doc one, first line of # doc two", body.value, "the inline face is still there for the digests")
	assert.Nil(t, size.blocks)

	inline, blocksH := valuesCellExtent(rows[0].valuePairs)
	assert.Equal(t, 1, inline)
	assert.InDelta(t, table2BlockCaptionHeight+2*(100+table2BlockGap), blocksH, 0.01)
	assert.InDelta(t, table2RowHeightSingle+blocksH, rowHeight(&rows[0]), 0.01)

	lone := &table2UnifiedRow{kind: rowKindData, valuePairs: []table2NamedValue{{name: "body", blocks: []CellBlock{{Render: func() {}, Height: 5}}}}}
	assert.InDelta(t, table2RowHeightSingle+table2BlockGap, rowHeight(lone), 0.01, "a block asks for less than a line: one line")
	lone.valuePairs[0].blocks[0].Height = 10_000
	assert.InDelta(t, table2BlockMaxHeight+table2BlockGap, rowHeight(lone), 0.01, "…or for more than the maximum: the maximum")
	plain := &table2UnifiedRow{kind: rowKindData, valuePairs: []table2NamedValue{{name: "a", value: "1"}, {name: "b", value: "2"}, {name: "c", value: "3"}}}
	assert.InDelta(t, table2RowHeightDouble, rowHeight(plain), 0.01)
}

// A plain column is a row of its own, its name in the attribute cell, its
// bare value — and its block face — in the values cell: where a leeway
// entity's id sits, and so where gloss/taggedid's block face belongs.
func TestRecordCardPlainColumnsKeepTheirBlocks(t *testing.T) {
	card := NewRecordCard(nil, "", ColorPaletteViridis)
	card.SetCellBlock(func(arrowIdx int, _ string) (CellBlock, bool) {
		if arrowIdx != 0 {
			return CellBlock{}, false
		}
		return CellBlock{Render: func() {}, Height: 62}, true
	})
	card.Prepare(oneRecord(
		lwread.Attribute{Section: "entity-id", Plain: true, Name: "id", Values: []lwread.Value{scalar("id", 0, "12393906174523605050", "12393906174523605050")}},
		lwread.Attribute{Section: "entity-id", Plain: true, Name: "naturalKey", Values: []lwread.Value{scalar("naturalKey", 1, "YWJj", "abc")}},
	))
	require.Len(t, card.unified, 3, "one header, then one row per plain column")
	assert.Equal(t, rowKindSectionHeader, card.unified[0].kind)
	id, key := card.unified[1], card.unified[2]
	assert.Equal(t, "id", id.primary[0].display)
	assert.Equal(t, "", id.valuePairs[0].name, "the bare value")
	require.Len(t, id.valuePairs[0].blocks, 1)
	assert.Nil(t, key.valuePairs[0].blocks)
	assert.Equal(t, "abc", key.valuePairs[0].value)
	assert.InDelta(t, 62+table2BlockGap, rowHeight(&id), 0.01)
}

// Records are separated, each run of attributes in one section gets one
// header, an attribute's name and labels fill their columns, and a cut list
// says how much was cut.
func TestRecordCardLayout(t *testing.T) {
	card := NewRecordCard(nil, "", ColorPaletteViridis)
	cpu := &lwread.Membership{Name: "cpu", Text: "cpu"}
	m := &lwread.Model{Records: []lwread.Record{
		{Attributes: []lwread.Attribute{
			{Section: "num", Name: "cpu", Named: cpu, Labels: []lwread.Membership{{Text: "unit"}}, Values: []lwread.Value{scalar("value", 3, "41", "41")}},
			{Section: "num", Name: "num", Values: []lwread.Value{{Column: "value", ArrowIdx: 3, Shape: lwread.ShapeList, Items: []lwread.Item{{Raw: "1", Text: "1"}}, More: 4}}},
			{Section: "geo", CoGroup: "loc", Name: "geo", Values: []lwread.Value{scalar("lat", 4, "47", "47")}},
		}},
		{Attributes: []lwread.Attribute{{Section: "num", Name: "cpu", Named: cpu, Values: []lwread.Value{scalar("value", 3, "9", "9")}}}},
	}}
	card.Prepare(m)
	var kinds []rowKindE
	for _, r := range card.unified {
		kinds = append(kinds, r.kind)
	}
	assert.Equal(t, []rowKindE{rowKindSectionHeader, rowKindData, rowKindData, rowKindSectionHeader, rowKindData,
		rowKindEntitySep, rowKindSectionHeader, rowKindData}, kinds)
	assert.EqualValues(t, 2, card.nEntities)
	first := card.unified[1]
	assert.Equal(t, []table2Tag{{display: "cpu", detail: "verbatim"}}, first.primary)
	assert.Equal(t, "unit", first.secondary[0].display)
	assert.Empty(t, card.unified[2].primary, "no membership names it: the section column says what it is")
	assert.Equal(t, "1, … 4 more", card.unified[2].valuePairs[0].value)
	assert.Equal(t, sectionTypeCo, card.unified[4].sectionType)
	assert.Equal(t, "loc · geo", card.unified[4].sectionName)
}

// SectionDigests lifts each tagged row's name, labels and value pairs, and
// leaves plain rows out. The Detail timeline labels its flags with them.
func TestRecordCardSectionDigests(t *testing.T) {
	card := NewRecordCard(nil, "", ColorPaletteViridis)
	sensor := &lwread.Membership{Name: "sensorA", Text: "sensorA"}
	card.Prepare(oneRecord(
		lwread.Attribute{Section: "entity-timestamp", Plain: true, Name: "event-time", Values: []lwread.Value{scalar("ts", 0, "1969-12-31", "1969-12-31")}},
		lwread.Attribute{Section: "obs", Name: "sensorA", Named: sensor, Labels: []lwread.Membership{{Text: "region:eu"}},
			Values: []lwread.Value{scalar("seen", 1, "2025-12-06", "2025-12-06"), scalar("seq", 2, "42", "42")}},
	))
	digs := card.SectionDigests()
	require.Len(t, digs, 1)
	d := digs[0]
	assert.Equal(t, "obs", d.SectionName)
	assert.Equal(t, []string{"sensorA"}, d.Primary)
	assert.Equal(t, []string{"region:eu"}, d.Secondary)
	assert.Equal(t, []SectionValue{{Name: "seen", Value: "2025-12-06"}, {Name: "seq", Value: "42"}}, d.Values)
}
