package play

import (
	"github.com/apache/arrow-go/v18/arrow"
)

// play_timeline_panel.go is slice 2 of ADR-0097: the Timeline as the first
// PanelI — an observer bound to the `main` node. Accept is the existing
// resolveContract (Mode/Reject) lifted to the typed accept/reject negotiation;
// Render delegates to TimelineDriver. Selection is published as a param
// mutation through SignalEmitterI into the signal store (slice 5b).
// See doc/adr/0097-play-reactive-query-graph.md.

// mainNodeID is the canonical id of the shared result node — today's single
// QueryStore result, i.e. the degenerate single-node graph of slice 1.
const mainNodeID NodeID = "main"

// signalSelection is the param a panel writes to publish the selected row
// (ADR-0097 SD8: selection is just a panel-written signal, shared by name).
const signalSelection SignalID = "selection"

// bandsNodeID is the id of the Timeline's bands node — the bands-editor SQL run
// on the driver's bands lane (4b). It is the chBands channel's fixed source
// (option a): a panel-authored node, not one of the split-graph nodes.
const bandsNodeID NodeID = "timeline-bands"

// timelinePanel adapts the Timeline to PanelI. It is a thin value over the
// existing *TimelineDriver, constructed per frame in renderTimelineTab.
type timelinePanel struct {
	driver *TimelineDriver
}

func (inst timelinePanel) ID() PanelID { return "timeline" }

// Channels: the required "events" channel (foreground marks) + the optional
// "bands" channel (background shaded ranges), filled by the bands node (4b-2).
func (inst timelinePanel) Channels() []ChannelSpec {
	return []ChannelSpec{
		{ID: chEvents, Required: true, Label: "events"},
		{ID: chBands, Required: false, Label: "bands"},
	}
}

// AcceptForChannel lifts resolveContract for the events channel (Mode/Reject);
// for chBands it accepts any bands-node result — the _tl_band_* contract is
// validated in mapBandsRecord (setBands), which reports column errors via the
// bands status line rather than as a channel reject. Pure (schema-only).
func (inst timelinePanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	if ch == chBands {
		if schema == nil {
			reason = "no bands result"
			return
		}
		claim = true
		return
	}
	ct := resolveContract(schema)
	if ct.Mode == timelineModeNone {
		reason = ct.Reject
		return
	}
	claim = ct
	return
}

// Render sets the bands (mapped by the driver) before the events render — the
// band producer reads inst.bands during renderContract — then paints the events.
func (inst timelinePanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	if b, ok := filled[chBands]; ok {
		inst.driver.setBands(b.Rec)
	} else {
		inst.driver.clearBands()
	}
	ev := filled[chEvents]
	ct, ok := ev.Claim.(timelineContract)
	if !ok {
		return
	}
	inst.driver.renderContract(ev.Rec, ct, emit)
}
