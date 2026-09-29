package schemaview

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// TestRenderHeadless renders one frame without a host: the widget must not
// panic, must refuse a nil State, and a State must follow a swapped table by
// resetting its selection while keeping its filter.
func TestRenderHeadless(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	var st State
	if res := Render(Input{Ids: ids, ScopeKey: "t", Table: detailFixture(), State: &st}); res.Err != nil {
		t.Fatalf("render: %v", res.Err)
	}
	if st.sel.kind == selNone {
		t.Fatal("the first frame selects a default node")
	}
	if Render(Input{Ids: ids, ScopeKey: "t", Table: detailFixture()}).Err == nil {
		t.Fatal("a nil State must be refused")
	}
	// A different table resets the selection and keeps the filter.
	st.filter = "cel"
	st.sel = selection{kind: selSection, section: 1}
	other := detailFixture()
	Render(Input{Ids: ids, ScopeKey: "t", Table: other, State: &st})
	if st.sel != defaultSelection(other) || st.filter != "cel" {
		t.Fatalf("after a table swap sel=%+v filter=%q", st.sel, st.filter)
	}
}

// detailFixture is fixture() with the parallel aspect slices the detail pane
// indexes, which the navigator tests never touch.
func detailFixture() (t *common.TableDesc) {
	t = fixture()
	t.PlainValuesEncodingHints = make([]encodingaspects.AspectSet, len(t.PlainValuesNames))
	t.PlainValuesValueSemantics = make([]valueaspects.AspectSet, len(t.PlainValuesNames))
	return
}
