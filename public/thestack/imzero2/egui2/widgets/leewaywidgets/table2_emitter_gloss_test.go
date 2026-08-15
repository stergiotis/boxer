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
