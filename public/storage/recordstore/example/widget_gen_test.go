package example

import (
	"testing"

	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// TestGenerateWidgetStore emits the pass-through reference store — the
// composite-id / routing / state-view backbone drives WidgetEnvelope. Run it to
// (re)generate:
//
//	go test -tags "$(cat tags)" -run TestGenerateWidgetStore ./public/storage/recordstore/example/
func TestGenerateWidgetStore(t *testing.T) {
	manip, err := GetWidgetSchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName: "example",
		StoreName:   "Widget",
		TableName:   "widget",
		Table:       td,
		RowConfig:   TableRowConfig,
		OutDir:      ".",
		ImportPath:  "github.com/stergiotis/boxer/public/storage/recordstore/example",
	}.Generate())
}
