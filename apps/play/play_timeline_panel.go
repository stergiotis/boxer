package play

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
)

// play_timeline_panel.go is slice 2 of ADR-0097: the Timeline as the first
// PanelI — an observer bound to the `main` node. Accept is the existing
// resolveContract (Mode/Reject) lifted to the typed accept/reject negotiation;
// Render delegates to TimelineDriver. Selection is published as a param mutation
// through SignalEmitterI, bridged to PlayApp.selectedRow until the signal graph
// owns the sink. See doc/adr/0097-play-reactive-query-graph.md.

// mainNodeID is the canonical id of the shared result node — today's single
// QueryStore result, i.e. the degenerate single-node graph of slice 1.
const mainNodeID NodeID = "main"

// signalSelection is the param a panel writes to publish the selected row
// (ADR-0097 SD8: selection is just a panel-written signal, shared by name).
const signalSelection SignalID = "selection"

// timelinePanel adapts the Timeline to PanelI. It is a thin value over the
// existing *TimelineDriver, constructed per frame in renderTimelineTab.
type timelinePanel struct {
	driver *TimelineDriver
}

func (inst timelinePanel) ID() PanelID       { return "timeline" }
func (inst timelinePanel) BoundNode() NodeID { return mainNodeID }

// Accept lifts resolveContract: a renderable contract (Mode != None) becomes the
// claim; otherwise the contract's Reject is the empty-state reason. Pure — it
// reads no signals yet (the Timeline contract is schema-only).
func (inst timelinePanel) Accept(schema *arrow.Schema, sig SignalEnvI) (claim PanelClaim, reason string) {
	ct := resolveContract(schema)
	if ct.Mode == timelineModeNone {
		reason = ct.Reject
		return
	}
	claim = ct
	return
}

// Render delegates to the driver with the contract Accept already resolved.
func (inst timelinePanel) Render(rec arrow.RecordBatch, claim PanelClaim, emit SignalEmitterI) {
	ct, ok := claim.(timelineContract)
	if !ok {
		return
	}
	inst.driver.renderContract(rec, ct, emit)
}

// selectedRowEmitter bridges a panel's selection signal to PlayApp.selectedRow —
// the sink today's Detail/Table tabs still read. The strangler step before the
// signal graph owns the selection (ADR-0097 SD8): the panel already emits, only
// the sink is legacy.
type selectedRowEmitter struct {
	target *int64
}

func (inst selectedRowEmitter) Emit(id SignalID, value any) {
	if id != signalSelection || inst.target == nil {
		return
	}
	if r, ok := value.(int64); ok {
		*inst.target = r
	}
}

// emptySignals is a SignalEnvI with no bound signals — used where a panel's
// Accept reads none (the Timeline contract is schema-only). Replaced by the live
// signal env when the runtime drives the panel.
type emptySignals struct{}

func (emptySignals) Get(id SignalID) (param env.Param, ok bool) { return }
func (emptySignals) Revision() uint64                           { return 0 }
