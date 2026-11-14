package play

import (
	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookview"
)

// Play's panels as the renderers of a notebook's presentation cells
// (ADR-0371 §SD4). A `sql chart` cell is drawn by the panel
// play draws a chart tab with: the cell's own relation is the panel's
// `main` channel and each member of the cell's WITH list is the channel of
// its name, which is how play names a panel's further relations already.
// The timeline's `events` channel is the one play fills from the buffer's
// own result under another name, so a cell's own relation is offered there
// as well.
//
// Only the panels whose PanelI face is a driver alone are registered. Table,
// detail, schema, cards and chat read their PlayApp, and the vector field
// and the projection need a source a stored record is not.

// notebookPanels builds each registered panel on a caller's id stack, with
// no client, no source and no delivery callback.
var notebookPanels = map[PanelID]func(ids *c.WidgetIdStack) PanelI{
	"chart":      func(ids *c.WidgetIdStack) PanelI { return chartPanel{driver: NewChartDriver(ids)} },
	"dist":       func(ids *c.WidgetIdStack) PanelI { return distPanel{driver: NewDistDriver(ids)} },
	"icicle":     func(ids *c.WidgetIdStack) PanelI { return iciclePanel{driver: NewIcicleDriver(ids)} },
	"treemap":    func(ids *c.WidgetIdStack) PanelI { return treemapPanel{driver: newTreemapDriver(ids)} },
	"world":      func(ids *c.WidgetIdStack) PanelI { return worldPanel{driver: NewWorldDriver(ids)} },
	"bowtie":     func(ids *c.WidgetIdStack) PanelI { return bowtiePanel{driver: NewBowtieDriver(ids)} },
	"riskmatrix": func(ids *c.WidgetIdStack) PanelI { return riskMatrixPanel{driver: NewRiskMatrixDriver(ids)} },
	"tolerance":  func(ids *c.WidgetIdStack) PanelI { return tolerancePanel{driver: NewToleranceDriver(ids)} },
	"files":      func(ids *c.WidgetIdStack) PanelI { return filesPanel{driver: newFilesDriver(ids)} },
	"graphview":  func(ids *c.WidgetIdStack) PanelI { return graphviewPanel{driver: NewGraphviewDriver(ids, nil)} },
	"network":    func(ids *c.WidgetIdStack) PanelI { return layeredGraphPanel{driver: NewNetworkDriver(ids, nil)} },
	"kanban":     func(ids *c.WidgetIdStack) PanelI { return kanbanPanel{driver: NewKanbanDriver(ids, nil)} },
	"sankey":     func(ids *c.WidgetIdStack) PanelI { return sankeyPanel{driver: NewSankeyDriver(ids, nil)} },
	"series":     func(ids *c.WidgetIdStack) PanelI { return seriesPanel{driver: NewSeriesDriver(ids, nil)} },
	"timeline": func(ids *c.WidgetIdStack) PanelI {
		return timelinePanel{driver: NewTimelineDriver(ids, nil, nil, nil)}
	},
}

func init() {
	for id, build := range notebookPanels {
		notebookview.RegisterRenderer(string(id), func(ids *c.WidgetIdStack) notebookview.CellRenderer {
			panel := build(ids)
			return func(res *mdnotebook.Result) (reason string) {
				return renderNotebookCell(panel, res)
			}
		})
	}
}

// noSignals is the signal environment of a sealed cell: nothing is
// selected and nothing changes.
type noSignals struct{}

func (noSignals) Get(SignalID) (p env.Param, ok bool) { return }
func (noSignals) Revision() uint64                    { return 0 }

// dropSignals is where a sealed cell's panel writes: nowhere. A click that
// would select a row in play selects nothing here.
type dropSignals struct{}

func (dropSignals) Emit(SignalID, any) {}

// renderNotebookCell offers a cell's relations to the panel's channels and
// draws it through play's own dispatch (dispatchPanel). The reason of a
// required channel that is missing or of the wrong shape is returned and
// nothing is drawn; an optional channel that refused its member is drawn
// without, and said so in a line above the panel.
func renderNotebookCell(panel PanelI, res *mdnotebook.Result) (reason string) {
	inputs := make(map[ChannelID]channelInput, 2+len(res.Members))
	offer := func(ch ChannelID, rec arrow.RecordBatch) {
		if rec != nil {
			inputs[ch] = channelInput{rec: rec, schema: rec.Schema(), sig: noSignals{}}
		}
	}
	offer(chMain, res.Main)
	// As the Timeline tab binds the buffer's own result (renderTimelineTab);
	// a member named events takes its place.
	offer(chEvents, res.Main)
	for _, m := range res.Members {
		offer(ChannelID(m.Name), m.Record)
	}
	return dispatchPanel(panel, inputs, dropSignals{})
}
