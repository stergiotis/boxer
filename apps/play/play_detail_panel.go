package play

import (
	"strconv"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// play_detail_panel.go is slice 2 of ADR-0097: the Detail tab as a PanelI
// observer of the `main` node and the CONSUMER of the `selection` signal that
// the Timeline (and Table/Projection) publish — closing the producer/consumer
// loop (SD8). Accept reads the selected row from the signal env; Render draws the
// leeway/ad-hoc card for it. Detail emits nothing.

// detailClaim is Detail's resolved render target: the schema Accept validated and
// the selected row it read from the selection signal.
type detailClaim struct {
	schema *arrow.Schema
	row    int64
}

// detailPanel adapts the Detail tab to PanelI. It holds the app for the
// renderDetailPane machinery (card driver, id stack, density).
type detailPanel struct {
	app *PlayApp
}

func (inst detailPanel) ID() PanelID { return "detail" }

// Channels: one required "main" channel — the row whose detail is shown.
func (inst detailPanel) Channels() []ChannelSpec {
	return []ChannelSpec{{ID: chMain, Required: true, Label: "row detail"}}
}

// AcceptForChannel gates on a result schema and a valid selection read from the
// signal env: no schema → "run a query"; no selected row → "select a row". Detail
// renders any schema — leeway card vs ad-hoc grouping is decided inside
// renderDetailPane, so there is no schema-shape rejection (unlike the Timeline).
func (inst detailPanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	if schema == nil {
		reason = "Run a query, then select a row to see its detail."
		return
	}
	row, ok := readSelection(sig)
	if !ok || row < 0 {
		reason = "Select a row in the Table tab to see its detail."
		return
	}
	claim = detailClaim{schema: schema, row: row}
	return
}

// Render draws the detail card for the claimed row. emit is unused — Detail is a
// pure consumer. A row past the result's end (a stale selection across a shrunk
// or empty result) falls back to the "select a row" empty-state.
func (inst detailPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	main := filled[chMain]
	dc, ok := main.Claim.(detailClaim)
	if !ok {
		return
	}
	if dc.row < 0 || dc.row >= main.Rec.NumRows() {
		for rt := range c.RichTextLabel("Select a row in the Table tab to see its detail.") {
			rt.Small().Weak()
		}
		return
	}
	inst.app.renderDetailPane(main.Rec, dc.schema, dc.row)
}

// selectionParam encodes a selected row index as the `selection` signal's value —
// an Int64 param, the same shape a SQL param would carry (ADR-0097 SD8).
func selectionParam(row int64) env.Param {
	return env.Param{Name: signalSelection, Type: "Int64", Raw: strconv.FormatInt(row, 10)}
}

// readSelection decodes the selected row from a signal env, or (-1, false) when
// the selection signal is absent or unparseable.
func readSelection(sig SignalEnvI) (row int64, ok bool) {
	p, found := sig.Get(signalSelection)
	if !found {
		return -1, false
	}
	r, err := strconv.ParseInt(p.Raw, 10, 64)
	if err != nil {
		return -1, false
	}
	return r, true
}

// playSignals is the strangler signal env: it exposes PlayApp.selectedRow as the
// `selection` signal, so consumers (Detail) read selection through the signal
// vocabulary while producers (Timeline's emitter, Table/Projection) still write
// selectedRow. When the runtime owns the signal store, this bridge and
// selectedRowEmitter retire together and the panels are unchanged.
type playSignals struct {
	selectedRow int64
}

func (inst playSignals) Get(id SignalID) (param env.Param, ok bool) {
	if id == signalSelection {
		return selectionParam(inst.selectedRow), true
	}
	return env.Param{}, false
}

func (inst playSignals) Revision() uint64 { return 0 }
