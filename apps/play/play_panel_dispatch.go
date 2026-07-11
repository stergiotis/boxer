package play

import "github.com/apache/arrow-go/v18/arrow"

// play_panel_dispatch.go is slice 4a of ADR-0097: the channel negotiation that
// drives a PanelI. A panel declares typed input channels (Channels); the
// dispatcher offers each a candidate node result, runs the per-channel
// accept/reject (AcceptForChannel), and renders only when every required channel
// is filled. Single-channel panels (Table/Projection/Detail/Timeline-events) are
// the degenerate case; the bands channel (slice 4b) is the first multi-channel use.

// channelInput is a candidate node result offered to a panel channel. schema may
// be nil (no result yet); rec is what Render draws when the channel is claimed.
type channelInput struct {
	node   NodeID
	rec    arrow.RecordBatch
	schema *arrow.Schema
	sig    SignalEnvI
}

// dispatchPanel runs the channel negotiation (SD6): for each declared channel it
// offers the matching input to AcceptForChannel. A required channel that rejects
// yields its reason (the empty-state) and the panel does NOT render — the caller
// paints the reason in its own style. When every required channel is claimed,
// Render is called with the filled map. Returns the first unmet required
// channel's reason, or "" when the panel rendered.
func dispatchPanel(p PanelI, inputs map[ChannelID]channelInput, emit SignalEmitterI) (reject string) {
	// Stamp the panel's identity onto an unstamped store emitter so its
	// writes carry provenance for the Signals chrome (slice 5e).
	if ge, isGraph := emit.(graphEmitter); isGraph && ge.writer == "" {
		emit = ge.as(string(p.ID()))
	}
	filled := make(map[ChannelID]ChannelResult, len(inputs))
	for _, spec := range p.Channels() {
		in := inputs[spec.ID]
		claim, reason := p.AcceptForChannel(spec.ID, in.schema, in.sig)
		if reason != "" {
			if spec.Required {
				return reason
			}
			continue // an optional channel that can't be filled is simply absent
		}
		filled[spec.ID] = ChannelResult{Node: in.node, Rec: in.rec, Claim: claim}
	}
	p.Render(filled, emit)
	return ""
}

// activeNodeID is the graph node whose result currently feeds the result panels:
// the observed node (3d), or the sink (main) when none is observed.
func (inst *PlayApp) activeNodeID() NodeID {
	if inst.observedNode != "" {
		return inst.observedNode
	}
	return mainNodeID
}
