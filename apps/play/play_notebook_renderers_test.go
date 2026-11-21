package play

import (
	"io/fs"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook/fixture"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookview"
)

func fixtureRecords(t *testing.T) map[string]arrow.RecordBatch {
	t.Helper()
	text, err := fs.ReadFile(fixture.FS(), fixture.Name)
	require.NoError(t, err)
	nb, err := mdnotebook.Read(text)
	require.NoError(t, err)
	ld := mdnotebook.Load(nb, fixture.FS())
	t.Cleanup(ld.Release)
	require.Empty(t, ld.Problems)
	return ld.Records
}

// Every panel registered as a notebook renderer is one the notebook view
// knows by its panel id.
func TestNotebookRenderersAreRegisteredUnderTheirPanelIds(t *testing.T) {
	registered := notebookview.RegisteredRenderers()
	for id := range notebookPanels {
		assert.Contains(t, registered, string(id))
	}
}

// A sealed cell's relations reach a panel as play's own dispatch hands
// them over: the cell's relation as `main`, a WITH member as the channel of
// its name. Two cells of one panel are two panels.
func TestNotebookCellsAreDrawnByPlaysPanels(t *testing.T) {
	messages, zero, reset := egui2test.InstallCounting()
	t.Cleanup(reset)
	records := fixtureRecords(t)
	draw := func(panel PanelI, statement string) (reason string, painted int) {
		res, err := mdnotebook.Evaluate(statement, records)
		require.NoError(t, err, statement)
		defer res.Release()
		defer c.CurrentApplicationState.StateManager.ScriptReset()
		zero()
		reason = renderNotebookCell(panel, res)
		return reason, messages()
	}
	ids := c.NewWidgetIdStack()

	chart := notebookPanels["chart"](ids)
	reason, painted := draw(chart, "SELECT t AS x, region AS series, p99 FROM p99_by_hour")
	assert.Empty(t, reason)
	assert.Positive(t, painted)
	reason, painted = draw(notebookPanels["chart"](ids), "SELECT t AS x, region AS series, p99 FROM p99_by_hour LIMIT 24")
	assert.Empty(t, reason, "a second cell of the same panel")
	assert.Positive(t, painted)

	// A result the panel does not read is refused with the panel's own
	// reason, and nothing is drawn.
	reason, painted = draw(chart, "SELECT region FROM p99_by_hour")
	assert.NotEmpty(t, reason)
	assert.Zero(t, painted)

	graph := notebookPanels["graphview"](ids)
	reason, painted = draw(graph, "WITH edges AS (SELECT source, target FROM calls), graph_opts AS (SELECT 'hierarchical' AS layout) SELECT * FROM edges")
	assert.Empty(t, reason)
	assert.Positive(t, painted)
	reason, _ = draw(graph, "SELECT source, target FROM calls")
	assert.NotEmpty(t, reason, "the graph reads the member named edges, not the cell's own relation")
	// A member an optional channel refuses is drawn without, not in place
	// of the panel.
	reason, painted = draw(notebookPanels["graphview"](ids), "WITH edges AS (SELECT source, target FROM calls), vertices AS (SELECT calls FROM calls) SELECT * FROM edges")
	assert.Empty(t, reason)
	assert.Positive(t, painted)

	// The timeline reads the cell's own relation as its events, as the
	// Timeline tab reads the buffer's own result.
	timeline := notebookPanels["timeline"](ids)
	reason, painted = draw(timeline, "SELECT t AS _tl_time FROM p99_by_hour")
	assert.Empty(t, reason)
	assert.Positive(t, painted)
	reason, _ = draw(timeline, "SELECT region FROM p99_by_hour")
	assert.NotEmpty(t, reason)
}

// Every registered panel is built without a play instance and answers a
// result it does not read with a reason, not a panic.
func TestNotebookPanelsRefuseWhatTheyDoNotRead(t *testing.T) {
	t.Cleanup(egui2test.Install())
	records := fixtureRecords(t)
	res, err := mdnotebook.Evaluate("SELECT samples FROM stats", records)
	require.NoError(t, err)
	defer res.Release()
	for id, build := range notebookPanels {
		panel := build(c.NewWidgetIdStack())
		assert.NotPanics(t, func() {
			defer c.CurrentApplicationState.StateManager.ScriptReset()
			renderNotebookCell(panel, res)
		}, string(id))
	}
}
