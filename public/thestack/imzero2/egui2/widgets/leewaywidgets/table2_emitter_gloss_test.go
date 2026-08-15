package leewaywidgets

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetCellGloss drives the value protocol by hand — a scalar column and a
// two-item collection — and checks the installed gloss rewrites each value's
// text (per item, never the ", " joiner) and is keyed by the column's Arrow
// index. Removing it restores the identity.
func TestSetCellGloss(t *testing.T) {
	e := NewTable2CardEmitter(nil, ColorPaletteViridis, nil)
	e.colNames = []string{"temperature", "reading"}
	e.colHidden = []bool{false, false}
	e.currentRow = &table2UnifiedRow{kind: rowKindData}
	e.columnIdx = -1

	e.SetCellGloss(func(arrowIdx int, text string) string {
		if arrowIdx == 7 {
			return text + " °C"
		}
		return text
	})

	// Scalar in Arrow column 7: glossed.
	e.BeginColumn(streamreadaccess.PhysicalColumnAddr{Index: 7}, "temperature", nil, valueaspects.EmptyAspectSet)
	e.BeginScalarValue()
	_, _ = e.WriteString("21.5")
	require.NoError(t, e.EndScalarValue())
	e.EndColumn()

	// Two items in Arrow column 9: not this gloss's column, untouched.
	e.BeginColumn(streamreadaccess.PhysicalColumnAddr{Index: 9}, "reading", nil, valueaspects.EmptyAspectSet)
	e.BeginHomogenousArrayValue(2)
	e.BeginValueItem(0)
	_, _ = e.WriteString("a")
	e.EndValueItem()
	e.BeginValueItem(1)
	_, _ = e.WriteString("b")
	e.EndValueItem()
	e.EndHomogenousArrayValue()
	e.EndColumn()

	require.Len(t, e.currentRow.valuePairs, 2)
	assert.Equal(t, table2NamedValue{name: "temperature", value: "21.5 °C"}, e.currentRow.valuePairs[0])
	assert.Equal(t, table2NamedValue{name: "reading", value: "a, b"}, e.currentRow.valuePairs[1])

	// Items are glossed one at a time, and the joiner is never handed over.
	var seen []string
	e.SetCellGloss(func(_ int, text string) string { seen = append(seen, text); return "[" + text + "]" })
	e.currentRow = &table2UnifiedRow{kind: rowKindData}
	e.columnIdx = -1
	e.BeginColumn(streamreadaccess.PhysicalColumnAddr{Index: 7}, "temperature", nil, valueaspects.EmptyAspectSet)
	e.BeginSetValue(2)
	e.BeginValueItem(0)
	_, _ = e.WriteString("x")
	e.EndValueItem()
	e.BeginValueItem(1)
	_, _ = e.WriteString("y")
	e.EndValueItem()
	e.EndSetValue()
	e.EndColumn()
	assert.Equal(t, []string{"x", "y"}, seen)
	assert.Equal(t, "[x], [y]", e.currentRow.valuePairs[0].value)

	// Cleared: identity again.
	e.SetCellGloss(nil)
	e.currentRow = &table2UnifiedRow{kind: rowKindData}
	e.columnIdx = -1
	e.BeginColumn(streamreadaccess.PhysicalColumnAddr{Index: 7}, "temperature", nil, valueaspects.EmptyAspectSet)
	e.BeginScalarValue()
	_, _ = e.WriteString("21.5")
	require.NoError(t, e.EndScalarValue())
	e.EndColumn()
	assert.Equal(t, "21.5", e.currentRow.valuePairs[0].value)
}

// TestSetCellBlock drives the same protocol with a block seam installed: the
// seam sees each value's text before the gloss rewrites it, a claim attaches
// to the pair per scalar or item, the inline text still goes through the
// gloss (SectionDigests read it), and the row grows by what the blocks ask
// for — clamped, with a caption only when the row has more than one pair.
func TestSetCellBlock(t *testing.T) {
	e := NewTable2CardEmitter(nil, ColorPaletteViridis, nil)
	e.colNames = []string{"body", "size"}
	e.colHidden = []bool{false, false}
	e.currentRow = &table2UnifiedRow{kind: rowKindData}
	e.columnIdx = -1

	var blockSaw []string
	e.SetCellGloss(func(_ int, text string) string { return "first line of " + text })
	e.SetCellBlock(func(arrowIdx int, text string) (CellBlock, bool) {
		blockSaw = append(blockSaw, text)
		if arrowIdx != 3 {
			return CellBlock{}, false
		}
		return CellBlock{Render: func() {}, Height: 100}, true
	})

	// Column 3, two items: both claimed.
	e.BeginColumn(streamreadaccess.PhysicalColumnAddr{Index: 3}, "body", nil, valueaspects.EmptyAspectSet)
	e.BeginHomogenousArrayValue(2)
	e.BeginValueItem(0)
	_, _ = e.WriteString("# doc one")
	e.EndValueItem()
	e.BeginValueItem(1)
	_, _ = e.WriteString("# doc two")
	e.EndValueItem()
	e.EndHomogenousArrayValue()
	e.EndColumn()
	// Column 5, a scalar: offered, declined, text-only.
	e.BeginColumn(streamreadaccess.PhysicalColumnAddr{Index: 5}, "size", nil, valueaspects.EmptyAspectSet)
	e.BeginScalarValue()
	_, _ = e.WriteString("12")
	require.NoError(t, e.EndScalarValue())
	e.EndColumn()

	assert.Equal(t, []string{"# doc one", "# doc two", "12"}, blockSaw, "the block seam sees the unrewritten text, per item")
	require.Len(t, e.currentRow.valuePairs, 2)
	body, size := e.currentRow.valuePairs[0], e.currentRow.valuePairs[1]
	assert.Len(t, body.blocks, 2)
	assert.Equal(t, "first line of # doc one, first line of # doc two", body.value, "the inline face is still there for the digests")
	assert.Nil(t, size.blocks)
	assert.Equal(t, "first line of 12", size.value)

	// Row height: one inline line, then a caption (two pairs) and two
	// clamped blocks with their gaps.
	inline, blocksH := valuesCellExtent(e.currentRow.valuePairs)
	assert.Equal(t, 1, inline)
	assert.InDelta(t, table2BlockCaptionHeight+2*(100+table2BlockGap), blocksH, 0.01)
	assert.InDelta(t, table2RowHeightSingle+blocksH, rowHeight(e.currentRow), 0.01)

	// A lone block pair: no caption, and the request is clamped both ways.
	lone := &table2UnifiedRow{kind: rowKindData, valuePairs: []table2NamedValue{{name: "body", blocks: []CellBlock{{Render: func() {}, Height: 5}}}}}
	assert.InDelta(t, table2RowHeightSingle+table2BlockGap, rowHeight(lone), 0.01, "a block asks for less than a line: one line")
	lone.valuePairs[0].blocks[0].Height = 10_000
	assert.InDelta(t, table2BlockMaxHeight+table2BlockGap, rowHeight(lone), 0.01, "…or for more than the maximum: the maximum")

	// A row without blocks keeps the old heuristic exactly.
	plain := &table2UnifiedRow{kind: rowKindData, valuePairs: []table2NamedValue{{name: "a", value: "1"}, {name: "b", value: "2"}, {name: "c", value: "3"}}}
	assert.InDelta(t, table2RowHeightDouble, rowHeight(plain), 0.01)

	// Cleared: nothing is offered.
	e.SetCellBlock(nil)
	blockSaw = nil
	e.currentRow = &table2UnifiedRow{kind: rowKindData}
	e.columnIdx = -1
	e.BeginColumn(streamreadaccess.PhysicalColumnAddr{Index: 3}, "body", nil, valueaspects.EmptyAspectSet)
	e.BeginScalarValue()
	_, _ = e.WriteString("x")
	require.NoError(t, e.EndScalarValue())
	e.EndColumn()
	assert.Nil(t, blockSaw)
	assert.Nil(t, e.currentRow.valuePairs[0].blocks)
}
