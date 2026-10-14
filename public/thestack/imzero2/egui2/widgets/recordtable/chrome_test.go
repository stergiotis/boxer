package recordtable

import (
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
	"github.com/stretchr/testify/assert"
)

// Note sees the wire id of every cell button, one per button: a link cell has
// no button and a header no cell.
func TestChromeNotesCellWireIds(t *testing.T) {
	t.Cleanup(scenetest.Install())
	var noted []uint64
	ch := Chrome{Ids: c.NewWidgetIdStack(), Note: func(wire uint64) { noted = append(noted, wire) }}
	ch.SelectableCell(5, 2, Cell{Text: "a"})
	ch.SelectableCell(6, 2, Cell{Text: "b", Link: "https://example.com"})
	ch.HeaderCell(5, 2, func() {})
	ch.SelectableCell(7, 2, Cell{Text: "c"})
	c.CurrentApplicationState.StateManager.ScriptReset()
	if assert.Len(t, noted, 2) {
		assert.NotEqual(t, noted[0], noted[1], "two cells, two ids")
	}
}

// The drag floor is the content floor plus the inset on both sides.
func TestDragMinWidthCountsBothInsets(t *testing.T) {
	for _, d := range []uint8{0, 1, 2} {
		assert.Equal(t, MinContentWidth+2*CellPadX(densityOf(d)), DragMinWidth(densityOf(d)))
	}
}
