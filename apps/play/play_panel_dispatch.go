package play

import (
	"github.com/apache/arrow-go/v18/arrow"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

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
	result ResultID // the ResultID of rec; zero for a synthetic input
}

// dispatchPanel runs the channel negotiation (SD6): for each declared channel it
// offers the matching input to AcceptForChannel. A required channel that rejects
// yields its reason (the empty-state) and the panel does NOT render — the caller
// paints the reason in its own style. When every required channel is claimed,
// Render is called with the filled map. Returns the first unmet required
// channel's reason, or "" when the panel rendered.
//
// An optional channel that was offered a real result and refused it is drawn
// without, and said so in one line above the pane: a `vertices` CTE missing its
// `id` is a decoration the author wrote and is not getting, and until this line
// existed nothing on screen told them.
func dispatchPanel(p PanelI, inputs map[ChannelID]channelInput, emit SignalEmitterI) (reject string) {
	// Stamp the panel's identity onto an unstamped store emitter so its
	// writes carry provenance for the Signals chrome (slice 5e).
	if ge, isGraph := emit.(graphEmitter); isGraph && ge.writer == "" {
		emit = ge.as(string(p.ID()))
	}
	// Selection coherence across differently-bound panels (slice 6c): the
	// panel's selection writes are stamped with the node its primary channel
	// renders (plus the row's leeway id when the result carries one), and
	// its reads see `selection` only when the cursor indexes that node.
	if specs := p.Channels(); len(specs) > 0 {
		prim := inputs[specs[0].ID]
		emit = selectionStamper{inner: emit, node: prim.node, rec: prim.rec}
	}
	filled, refused, reject := negotiateChannels(p, inputs)
	if reject != "" {
		return reject
	}
	for _, r := range refused {
		for rt := range c.RichTextLabel(r.String()) {
			rt.Small().Weak()
		}
	}
	p.Render(filled, emit)
	return ""
}

// optionalRefusal is an optional channel that was offered a result and
// refused it: Label is the channel's (the CTE name, for the named-CTE
// channels), Reason the panel's own text.
type optionalRefusal struct {
	Label  string
	Reason string
}

func (inst optionalRefusal) String() string {
	return "`" + inst.Label + "` not used: " + inst.Reason
}

// negotiateChannels is the pure half of dispatchPanel. reject is the first
// required channel's reason; refused lists the optional channels that rejected
// an input carrying a schema. An optional channel offered nothing — no such
// CTE in the buffer, or its lane still loading — is simply absent, which is the
// ordinary case and not worth a line.
func negotiateChannels(p PanelI, inputs map[ChannelID]channelInput) (filled map[ChannelID]ChannelResult, refused []optionalRefusal, reject string) {
	filled = make(map[ChannelID]ChannelResult, len(inputs))
	for _, spec := range p.Channels() {
		in := inputs[spec.ID]
		sig := in.sig
		if sig != nil {
			sig = nodeScopedSelection{SignalEnvI: sig, node: in.node}
		}
		claim, reason := p.AcceptForChannel(spec.ID, in.schema, sig)
		if reason != "" {
			if spec.Required {
				return nil, nil, reason
			}
			if in.schema != nil {
				refused = append(refused, optionalRefusal{Label: spec.Label, Reason: reason})
			}
			continue
		}
		filled[spec.ID] = ChannelResult{Node: in.node, Rec: in.rec, Claim: claim, Result: in.result}
	}
	return
}

// activeNodeID is the graph node whose result currently feeds the result panels:
// the observed node (3d), or the sink (main) when none is observed.
func (inst *PlayApp) activeNodeID() NodeID {
	if inst.observedNode != "" {
		return inst.observedNode
	}
	return mainNodeID
}
