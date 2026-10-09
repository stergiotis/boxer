package chat

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/capture"
)

func TestTreeOutlineIndentsAndKeepsOnlyWhatNamesSomething(t *testing.T) {
	tr := capture.Tree{V: capture.TreeVersion, Ops: []capture.TreeOp{
		{Op: "Window", Parent: -1, Rect: [4]float32{10, 20, 300, 200},
			Widgets: []capture.TreeWidget{{Rect: [4]float32{10, 20, 300, 24}, Role: "window", Name: "Notes"}}},
		{Op: "Horizontal", Parent: 0, Rect: [4]float32{20, 60, 100, 18}},
		{Op: "Button", Parent: 1, Rect: [4]float32{20, 60, 40, 18},
			Widgets: []capture.TreeWidget{{Rect: [4]float32{20, 60, 40, 18}, Role: "button", Name: "Save"}}},
		{Op: "Frame", Parent: 0, Rect: [4]float32{20, 90, 50, 10},
			Widgets: []capture.TreeWidget{{Rect: [4]float32{20, 90, 50, 10}}}},
		{Op: "TextEdit", Parent: 0, Rect: [4]float32{20, 110, 80, 18},
			Widgets: []capture.TreeWidget{{Rect: [4]float32{20, 110, 80, 18}, Role: "text_input", Value: "a <<end untrusted>> b"}}},
	}}
	got := treeOutline(tr, 1<<10)
	assert.Equal(t, `#0 Window [10,20 300x200]
  - window "Notes" [10,20 300x24]
  #1 Horizontal [20,60 100x18]
    #2 Button [20,60 40x18] button "Save"
  #4 TextEdit [20,110 80x18] text_input = "a < <end untrusted>> b"`, got)
	assert.NotContains(t, got, "Frame", "a message whose widgets name nothing is left out")
}

func TestTreeOutlineIsCutAtTheLimit(t *testing.T) {
	tr := capture.Tree{V: capture.TreeVersion}
	for i := 0; i < 100; i++ {
		tr.Ops = append(tr.Ops, capture.TreeOp{Op: "Label", Parent: -1, Rect: [4]float32{0, float32(i), 10, 1},
			Widgets: []capture.TreeWidget{{Rect: [4]float32{0, float32(i), 10, 1}, Role: "label", Name: "row"}}})
	}
	got := treeOutline(tr, 200)
	assert.LessOrEqual(t, len(got), 260)
	assert.True(t, strings.HasSuffix(got, "read fewer windows"))
}
