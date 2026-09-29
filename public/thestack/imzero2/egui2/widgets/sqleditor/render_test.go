package sqleditor

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlcomplete"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// TestEditorRenderHeadless renders one frame of the editor without a host:
// Bind then Render, the result returned from the frame, and nothing drawn
// before a Bind or without ids.
func TestEditorRenderHeadless(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	ed := New(ids, "t")
	buf := "SELECT 1;\nSELECT 2"
	ed.SetCaretForTest(3)
	bound := ed.Bind(Frame{Value: &buf, Rows: 4})
	res := ed.Render(Decoration{})
	require.Equal(t, bound, res, "Render returns what Bind published")
	require.True(t, res.Ok)
	require.Equal(t, 2, res.Total)

	// A residual view keeps its own identity under the editor's scope.
	mirror := "SELECT 2"
	ed.Bind(Frame{View: "residual", Value: &mirror, Offset: len("SELECT 1;\n"), Canonical: buf})
	require.Equal(t, buf, ed.Render(Decoration{}).Buffer)

	require.Equal(t, Result{}, New(ids, "u").Render(Decoration{}), "unbound: nothing")
	require.Equal(t, Result{}, New(nil, "v").Render(Decoration{}), "no ids: nothing drawn, zero result")
}

// TestFieldRenderHeadless renders a field and a multi-line field once.
func TestFieldRenderHeadless(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	f := NewField(ids, "t")
	v := "a = 1"
	require.False(t, f.Render(FieldFrame{Value: &v, Hint: "predicate", Width: 200}).Changed)
	v2 := "line one\nline two"
	f.Render(FieldFrame{Value: &v2, Rows: 1})
	require.Equal(t, "line one line two", v2, "a single-line field folds line breaks")
	require.Equal(t, FieldResult{}, NewField(nil, "u").Render(FieldFrame{Value: &v}), "no ids: nothing drawn")
}

// TestRenderPaneHeadless renders the completion pane once, and refuses a
// missing State.
func TestRenderPaneHeadless(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	var st PaneState
	res := sqlcomplete.Result{
		Items:  []sqlcomplete.Item{{Text: "SysMem", Insert: "SysMem"}, {Text: "SysCPU", Insert: "SysCPU"}},
		Prefix: []int{0},
	}
	out := RenderPane(PaneInput{Ids: ids, ScopeKey: "p", State: &st, Result: res, Typed: "Sys", CaretAtPartialEnd: true, Heading: "kind"})
	require.NoError(t, out.Err)
	require.False(t, out.Accepted)
	require.Len(t, st.rows, 2, "a small domain shows whole")
	require.ErrorIs(t, RenderPane(PaneInput{Ids: ids, Result: res}).Err, ErrPaneNeedsState)
}
