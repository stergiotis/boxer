//go:build llm_generated_opus47

package widgets

import (
	"fmt"
	"math/rand/v2"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// =============================================================================
// DEMO: snarl node-editor — calculator dataflow (ADR-0021 M2)
// =============================================================================
//
// Three node kinds:
//   Number (kind=1, no inputs, 1 output) — value editable via a slider
//                                          rendered inside the node body
//                                          (deferred-block re-entry).
//   Add    (kind=2, 2 inputs, 1 output) — emits a + b each frame.
//   Sink   (kind=3, 1 input, no outputs) — displays the input value via a
//                                          Label inside the node body.
//
// Round-trip model (SD6 default — "Go authoritative, Rust persistence
// opt-in"):
//   1. Demo body drains FetchSnarlEvents from the previous frame.
//   2. NodeMoved   → updates posX/posY in nodes.
//   3. Connection* → updates edges.
//   4. Topological eval over the DAG fills .computed on each node.
//   5. Topology pushed via SnarlNode / SnarlPin / SnarlConnection.
//   6. Editor built with deferred-block bodies for Number (slider) and
//      Sink (label). .Send() flushes the editor opcode.
//
// Pin labels carry kind=1 → all pins coloured the same in the default
// PinInfo::circle render. Multi-typed pins are SD8 future work.

const (
	snarlKindNumber = uint32(1)
	snarlKindAdd    = uint32(2)
	snarlKindSink   = uint32(3)
)

type snarlDemoNode struct {
	id         uint64
	kind       uint32
	posX, posY float32
	title      string
	value      float64 // Number nodes: editable. Add/Sink: ignored.
	computed   float64 // Last frame's eval result (per output port 0).
}

type snarlDemoEdge struct {
	srcNode uint64
	srcPort uint32
	dstNode uint64
	dstPort uint32
}

// snarlDemoState is the per-app-instance state for the snarl demo.
// Each open gallery window owns its own DAG, event log, persist-flag
// and node-id counter so two windows can edit independent calculator
// graphs side-by-side.
type snarlDemoState struct {
	nodes            map[uint64]*snarlDemoNode
	edges            []snarlDemoEdge
	eventLog         []string
	persistPositions bool
	wireStyleIdx     uint8 // 0 = Bezier5 default
	nextId           uint64
}

func newSnarlDemoState() (st *snarlDemoState) {
	st = &snarlDemoState{
		nodes:  map[uint64]*snarlDemoNode{},
		nextId: 5,
	}
	// Pre-populated: (n1=3) + (n2=5) → result.
	st.nodes[1] = &snarlDemoNode{id: 1, kind: snarlKindNumber, posX: 30, posY: 40, title: "n1", value: 3.0}
	st.nodes[2] = &snarlDemoNode{id: 2, kind: snarlKindNumber, posX: 30, posY: 180, title: "n2", value: 5.0}
	st.nodes[3] = &snarlDemoNode{id: 3, kind: snarlKindAdd, posX: 220, posY: 110, title: "add"}
	st.nodes[4] = &snarlDemoNode{id: 4, kind: snarlKindSink, posX: 410, posY: 110, title: "result"}
	st.edges = []snarlDemoEdge{
		{1, 0, 3, 0},
		{2, 0, 3, 1},
		{3, 0, 4, 0},
	}
	return
}

func init() {
	registry.Register(registry.Demo{
		Name:        "snarl",
		Category:    "Graphics & canvas",
		Title:       icons.IconGitMerge + " snarl (node editor)",
		Stage:       [2]float32{1024, 700},
		Flags:       registry.DemoFlagNeedsLargeArea | registry.DemoFlagNonDeterministic, // math/rand-seeded initial node positions + values
		Kind:        registry.DemoKindMixed,
		Description: "Node-graph editor (egui-snarl, ADR-0021): three-kind calculator dataflow — Number (slider in body) → Add → Sink (computed value in body). Drag nodes to move (NodeMoved round-trips through Go), drag pin → pin to connect, right-click an output pin to drop wires; topology is fully Go-authoritative by default with an opt-in Rust-persistence flag (SD6).",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = newSnarlDemoState()
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoSnarl(ids, state.(*snarlDemoState))
		},
		SourceFunc: demoSnarl,
	})
}

// snarlPinCount returns the (numInputs, numOutputs) for a kind.
// Centralised so the topology push and the viewer-side trait both agree.
func snarlPinCount(kind uint32) (uint32, uint32) {
	switch kind {
	case snarlKindNumber:
		return 0, 1
	case snarlKindAdd:
		return 2, 1
	case snarlKindSink:
		return 1, 0
	}
	return 0, 0
}

// evalDataflow runs a single Kahn topological pass and writes each
// node's output value back to .computed. Cycles silently leave dependent
// nodes at their previous .computed (the inDeg never reaches 0).
func (st *snarlDemoState) evalDataflow() {
	incoming := make(map[uint64][]snarlDemoEdge, len(st.nodes))
	inDeg := make(map[uint64]int, len(st.nodes))
	for id := range st.nodes {
		inDeg[id] = 0
	}
	for _, e := range st.edges {
		incoming[e.dstNode] = append(incoming[e.dstNode], e)
		inDeg[e.dstNode]++
	}
	outVal := map[[2]uint64]float64{}

	ready := make([]uint64, 0, len(st.nodes))
	for id, d := range inDeg {
		if d == 0 {
			ready = append(ready, id)
		}
	}
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		n := st.nodes[id]
		var v float64
		switch n.kind {
		case snarlKindNumber:
			v = n.value
		case snarlKindAdd:
			var a, b float64
			for _, e := range incoming[id] {
				key := [2]uint64{e.srcNode, uint64(e.srcPort)}
				if e.dstPort == 0 {
					a = outVal[key]
				} else if e.dstPort == 1 {
					b = outVal[key]
				}
			}
			v = a + b
		case snarlKindSink:
			for _, e := range incoming[id] {
				if e.dstPort == 0 {
					v = outVal[[2]uint64{e.srcNode, uint64(e.srcPort)}]
				}
			}
		}
		n.computed = v
		if n.kind != snarlKindSink {
			outVal[[2]uint64{id, 0}] = v
		}
		for _, e := range st.edges {
			if e.srcNode != id {
				continue
			}
			inDeg[e.dstNode]--
			if inDeg[e.dstNode] == 0 {
				ready = append(ready, e.dstNode)
			}
		}
	}
}

func (st *snarlDemoState) addNode(kind uint32) {
	id := st.nextId
	st.nextId++
	var title string
	var value float64
	switch kind {
	case snarlKindNumber:
		title = fmt.Sprintf("n%d", id)
		value = float64(rand.IntN(10)) - 5
	case snarlKindAdd:
		title = "add"
	case snarlKindSink:
		title = "result"
	}
	st.nodes[id] = &snarlDemoNode{
		id:    id,
		kind:  kind,
		posX:  100 + float32(rand.IntN(300)),
		posY:  100 + float32(rand.IntN(200)),
		title: title,
		value: value,
	}
}

// drainEvents consumes the previous frame's events and updates Go's
// authoritative model. NodeMoved is the round-trip half of SD6 default
// mode; Connection* are the only path through which the user can edit
// the topology since this demo doesn't expose a context-menu remove.
func (st *snarlDemoState) drainEvents() {
	for _, ev := range c.FetchSnarlEvents() {
		var line string
		switch ev.Kind {
		case c.SnarlEventKindNodeMoved:
			if n, ok := st.nodes[ev.NodeId]; ok {
				n.posX, n.posY = ev.X, ev.Y
			}
			line = fmt.Sprintf("NodeMoved id=%d -> (%.0f, %.0f)", ev.NodeId, ev.X, ev.Y)
		case c.SnarlEventKindConnectionAdded:
			already := false
			for _, e := range st.edges {
				if e.srcNode == ev.NodeId && e.srcPort == ev.PortA &&
					e.dstNode == ev.NodeIdB && e.dstPort == ev.PortB {
					already = true
					break
				}
			}
			if !already {
				st.edges = append(st.edges, snarlDemoEdge{
					srcNode: ev.NodeId, srcPort: ev.PortA,
					dstNode: ev.NodeIdB, dstPort: ev.PortB,
				})
			}
			line = fmt.Sprintf("ConnectionAdded %d.%d → %d.%d",
				ev.NodeId, ev.PortA, ev.NodeIdB, ev.PortB)
		case c.SnarlEventKindConnectionRemoved:
			kept := st.edges[:0]
			for _, e := range st.edges {
				if e.srcNode == ev.NodeId && e.srcPort == ev.PortA &&
					e.dstNode == ev.NodeIdB && e.dstPort == ev.PortB {
					continue
				}
				kept = append(kept, e)
			}
			st.edges = kept
			line = fmt.Sprintf("ConnectionRemoved %d.%d → %d.%d",
				ev.NodeId, ev.PortA, ev.NodeIdB, ev.PortB)
		}
		if line != "" {
			st.eventLog = append(st.eventLog, line)
		}
	}
	const keep = 10
	if len(st.eventLog) > keep {
		st.eventLog = st.eventLog[len(st.eventLog)-keep:]
	}
}

func demoSnarl(ids *c.WidgetIdStack, st *snarlDemoState) {
	c.Label("Calculator dataflow — drag nodes to move; drag pin → pin to connect; right-click an output pin to drop wires.").Send()

	c.Checkbox(ids.PrepareStr("snarl-persist"), st.persistPositions,
		"PersistPositions (Rust-authoritative layout — drags don't round-trip through Go)").
		SendRespVal(&st.persistPositions)

	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("snarl-add-number"),
			c.Atoms().Text("+ Number").Keep()).SendResp().HasPrimaryClicked() {
			st.addNode(snarlKindNumber)
		}
		if c.Button(ids.PrepareStr("snarl-add-add"),
			c.Atoms().Text("+ Add").Keep()).SendResp().HasPrimaryClicked() {
			st.addNode(snarlKindAdd)
		}
		if c.Button(ids.PrepareStr("snarl-add-sink"),
			c.Atoms().Text("+ Sink").Keep()).SendResp().HasPrimaryClicked() {
			st.addNode(snarlKindSink)
		}
	}

	st.evalDataflow()

	// Push topology — every frame. The Rust side reconciles against its
	// retained Snarl<u64> by full diff (SD7).
	for _, n := range st.nodes {
		ni, no := snarlPinCount(n.kind)
		c.SnarlNode(n.id, n.posX, n.posY, n.kind, n.title).
			NumInputs(ni).NumOutputs(no).Send()
		// Pin metadata. Side: 0=input, 1=output. Kind=1 colours every
		// pin the same in the default PinInfo::circle render — this
		// demo doesn't yet exercise typed pin polymorphism (SD8).
		switch n.kind {
		case snarlKindNumber:
			c.SnarlPin(n.id, 1, 0, "value", 1).Send()
		case snarlKindAdd:
			c.SnarlPin(n.id, 0, 0, "a", 1).Send()
			c.SnarlPin(n.id, 0, 1, "b", 1).Send()
			c.SnarlPin(n.id, 1, 0, "sum", 1).Send()
		case snarlKindSink:
			c.SnarlPin(n.id, 0, 0, "in", 1).Send()
		}
	}
	for _, e := range st.edges {
		c.SnarlConnection(e.srcNode, e.srcPort, e.dstNode, e.dstPort).Send()
	}

	// Build editor + deferred-block node bodies. Sliders bind via stable
	// heap pointers (the map values are *snarlDemoNode, so &n.value is
	// stable across frames).
	ed := c.SnarlEditor(ids.PrepareStr("snarl-editor")).
		Height(420).
		PersistPositions(st.persistPositions).
		WireStyle(st.wireStyleIdx).
		CrispMagnifiedText(true)
	for _, n := range st.nodes {
		switch n.kind {
		case snarlKindNumber:
			ed = ed.BeginNodeBody(n.id)
			c.SliderF64(ids.PrepareSeq(n.id*1000+1), n.value, -10.0, 10.0).
				Text("value").
				SendRespVal(&n.value)
			ed = ed.EndNodeBody()
		case snarlKindSink:
			ed = ed.BeginNodeBody(n.id)
			c.Label(fmt.Sprintf("= %.2f", n.computed)).Send()
			ed = ed.EndNodeBody()
		}
	}
	ed.Send()

	// Drain events from THIS frame's editor render. Must come AFTER
	// ed.Send() so the SendIntermediate inside FetchSnarlEvents flushes
	// the editor opcode (which fills snarl_events_pending) before the
	// fetch reads it back. If drained earlier in the body, the
	// PrepareNextFrame opcode that prefixes every Go frame would clear
	// the events captured at the end of the previous frame's editor
	// render — visible symptom: drag fires no NodeMoved events Go-side,
	// reconcile re-anchors nodes to their start positions, drop_outputs
	// silently re-creates wires.
	st.drainEvents()

	c.Separator().Send()
	c.Label(fmt.Sprintf("nodes: %d   edges: %d   events (last %d):",
		len(st.nodes), len(st.edges), len(st.eventLog))).Send()
	for _, line := range st.eventLog {
		c.Label("  " + line).Send()
	}
}
