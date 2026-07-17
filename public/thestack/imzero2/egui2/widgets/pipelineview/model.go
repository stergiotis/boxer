// Package pipelineview models and lays out schematic data-processing
// pipelines: a dominant left-to-right spine of stages (`a | b | c | d`) with
// side channels leaving each stage at sides fixed by port class — stderr
// below, configuration above, written artifacts hanging as leaves
// (ADR-0119). The primary input is the series/parallel structure the caller
// already knows, not a flat edge list; layout is grid recursion over that
// tree, not graph-structure recovery.
//
// The package is UI-free. This file declares the model; layout.go turns it
// into positioned geometry (painter space: points, top-left origin, y-down);
// ./view — the only half that imports the egui2 bindings — paints the
// geometry. The split mirrors the layeredgraph widget (ADR-0069), and
// ToGraphModel keeps the two models mechanically convertible.
package pipelineview

import (
	"fmt"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
)

// PortClass fixes the side a port occupies on its stage box. The set is
// closed by design (ADR-0119 SD1): extending it is an ADR update, not a
// widget option.
type PortClass uint8

const (
	// PortPrimary is the spine flow: it enters a stage on the west edge and
	// leaves on the east edge. Stages do not declare primary ports — every
	// stage has the two implicit anchors, referenced by a Ref with Port == "".
	PortPrimary PortClass = iota
	// PortDiagnostic is a south-side output (stderr and friends).
	PortDiagnostic
	// PortConfig is a north-side input (configuration read by the stage).
	PortConfig
	// PortArtifact is a south-side output for durable results (files
	// written); artifact pins sit east of diagnostic pins on the south edge.
	PortArtifact
)

// EndpointKind selects the glyph an endpoint is drawn with.
type EndpointKind uint8

const (
	// EndpointFile is a document glyph (dog-eared rectangle).
	EndpointFile EndpointKind = iota
	// EndpointStore is a cylinder glyph.
	EndpointStore
	// EndpointStream is a parallelogram glyph (the flowchart I/O shape).
	EndpointStream
	// EndpointNull is a small "discard" glyph (circle with a slash).
	EndpointNull
)

// Port declares a named side port on a stage. Declaration order is display
// order along the stage edge (diagnostics west of artifacts on the south
// edge; see layout.go).
type Port struct {
	Name  string
	Class PortClass
}

// Stage is one processing step on the spine.
type Stage struct {
	ID    string // unique across stages AND endpoints; sense-region key
	Label string // drawn text; empty means the ID is used
	Ports []Port // named side ports; the primary west/east anchors are implicit
}

// Group composes children in series (the spine order) or in parallel
// (stacked branches). The zero value of Par means series, so
// Group{Children: …} reads as a plain pipeline segment.
type Group struct {
	Par      bool
	Children []Element
}

// Element is a node of the series/parallel stage tree: a Stage or a Group.
type Element interface{ isElement() }

func (Stage) isElement() {}
func (Group) isElement() {}

// Endpoint is a terminal artifact outside the spine: a file written or read,
// a store loaded, a stream, a discard.
type Endpoint struct {
	ID    string // unique across stages AND endpoints
	Label string // drawn text; empty means the ID is used
	// Sublabel is an optional detail line drawn smaller under Label (a URL,
	// a database, a path); the endpoint box grows to fit it.
	Sublabel string
	Kind     EndpointKind
}

// Ref names one end of an explicit edge: exactly one of Stage or Endpoint is
// set. Port names a declared side port on Stage; empty Port on a stage ref
// means the implicit primary anchor (west when the ref is a target, east
// when it is a source). Endpoint refs carry no port.
type Ref struct {
	Stage    string
	Port     string
	Endpoint string
}

// IsEndpoint reports whether the ref names an endpoint.
func (r Ref) IsEndpoint() bool { return r.Endpoint != "" }

// Key returns the referenced node id (stage or endpoint).
func (r Ref) Key() string {
	if r.IsEndpoint() {
		return r.Endpoint
	}
	return r.Stage
}

// Edge is one explicit arc. Spine edges between consecutive tree elements
// are implied by the tree and are NOT listed here; explicit edges cover side
// channels (stage port ↔ endpoint), axial endpoints (primary ↔ endpoint:
// a `> file` sink or a `< file` source), and stage-to-stage primary edges
// (a forward skip, or a feedback edge when the target is not later than the
// source).
type Edge struct {
	From, To Ref
	Label    string
	// Volume is an optional flow quantity reserved for the deferred volume
	// overlay (ADR-0119 SD5); layout passes it through untouched and the v1
	// renderer ignores it. 0 = unknown.
	Volume float64
}

// Pipeline is the model handed to Compute: the stage tree, the endpoints,
// and the explicit edges.
type Pipeline struct {
	Root      Element
	Endpoints []Endpoint
	Edges     []Edge
}

// walkStages visits every stage in tree order.
func walkStages(el Element, visit func(st Stage)) {
	switch v := el.(type) {
	case Stage:
		visit(v)
	case Group:
		for _, ch := range v.Children {
			walkStages(ch, visit)
		}
	}
}

// entryStages returns the stage IDs an edge into el connects to (the heads
// of every parallel branch); exitStages the tails. Both preserve tree order.
func entryStages(el Element) (ids []string) {
	switch v := el.(type) {
	case Stage:
		return []string{v.ID}
	case Group:
		if len(v.Children) == 0 {
			return nil
		}
		if !v.Par {
			return entryStages(v.Children[0])
		}
		for _, ch := range v.Children {
			ids = append(ids, entryStages(ch)...)
		}
	}
	return
}

func exitStages(el Element) (ids []string) {
	switch v := el.(type) {
	case Stage:
		return []string{v.ID}
	case Group:
		if len(v.Children) == 0 {
			return nil
		}
		if !v.Par {
			return exitStages(v.Children[len(v.Children)-1])
		}
		for _, ch := range v.Children {
			ids = append(ids, exitStages(ch)...)
		}
	}
	return
}

// impliedSpineEdges lists the (from, to) stage pairs the tree implies:
// within every series group, the exits of each element connect to the
// entries of the next. Order is deterministic (tree order).
func impliedSpineEdges(el Element) (pairs [][2]string) {
	g, ok := el.(Group)
	if !ok {
		return nil
	}
	for _, ch := range g.Children {
		pairs = append(pairs, impliedSpineEdges(ch)...)
	}
	if !g.Par {
		for i := 0; i+1 < len(g.Children); i++ {
			for _, from := range exitStages(g.Children[i]) {
				for _, to := range entryStages(g.Children[i+1]) {
					pairs = append(pairs, [2]string{from, to})
				}
			}
		}
	}
	return
}

// Validate checks the model invariants Compute relies on: non-empty unique
// ids (stages and endpoints share one namespace), non-empty groups, refs
// that resolve, and the port-direction rules — a named source port is an
// output class (Diagnostic, Artifact), a named target port is an input class
// (Config). Stage-to-stage edges on NAMED ports (artifact feeding a later
// stage's config directly) are rejected in v1: model them through a shared
// endpoint. Endpoint-to-endpoint edges are rejected.
func (p Pipeline) Validate() error {
	if p.Root == nil {
		return fmt.Errorf("pipelineview: nil Root")
	}
	stages := make(map[string]Stage, 16)
	var walkErr error
	var walkGroups func(el Element)
	walkGroups = func(el Element) {
		g, ok := el.(Group)
		if !ok {
			return
		}
		if len(g.Children) == 0 && walkErr == nil {
			walkErr = fmt.Errorf("pipelineview: empty group")
		}
		for _, ch := range g.Children {
			walkGroups(ch)
		}
	}
	walkGroups(p.Root)
	if walkErr != nil {
		return walkErr
	}
	var dupErr error
	walkStages(p.Root, func(st Stage) {
		if dupErr != nil {
			return
		}
		if st.ID == "" {
			dupErr = fmt.Errorf("pipelineview: stage with empty ID")
			return
		}
		if _, dup := stages[st.ID]; dup {
			dupErr = fmt.Errorf("pipelineview: duplicate stage id %q", st.ID)
			return
		}
		stages[st.ID] = st
	})
	if dupErr != nil {
		return dupErr
	}
	if len(stages) == 0 {
		return fmt.Errorf("pipelineview: no stages")
	}
	endpoints := make(map[string]Endpoint, len(p.Endpoints))
	for _, ep := range p.Endpoints {
		if ep.ID == "" {
			return fmt.Errorf("pipelineview: endpoint with empty ID")
		}
		if _, dup := stages[ep.ID]; dup {
			return fmt.Errorf("pipelineview: endpoint id collides with stage id %q", ep.ID)
		}
		if _, dup := endpoints[ep.ID]; dup {
			return fmt.Errorf("pipelineview: duplicate endpoint id %q", ep.ID)
		}
		endpoints[ep.ID] = ep
	}
	portClass := func(st Stage, name string) (PortClass, bool) {
		for _, po := range st.Ports {
			if po.Name == name {
				return po.Class, true
			}
		}
		return 0, false
	}
	checkRef := func(r Ref, source bool) error {
		switch {
		case r.Stage != "" && r.Endpoint != "":
			return fmt.Errorf("pipelineview: ref names both stage %q and endpoint %q", r.Stage, r.Endpoint)
		case r.Endpoint != "":
			if r.Port != "" {
				return fmt.Errorf("pipelineview: endpoint ref %q carries a port", r.Endpoint)
			}
			if _, ok := endpoints[r.Endpoint]; !ok {
				return fmt.Errorf("pipelineview: unknown endpoint %q", r.Endpoint)
			}
		case r.Stage != "":
			st, ok := stages[r.Stage]
			if !ok {
				return fmt.Errorf("pipelineview: unknown stage %q", r.Stage)
			}
			if r.Port == "" {
				return nil // implicit primary anchor
			}
			cl, ok := portClass(st, r.Port)
			if !ok {
				return fmt.Errorf("pipelineview: stage %q has no port %q", r.Stage, r.Port)
			}
			if source && cl != PortDiagnostic && cl != PortArtifact {
				return fmt.Errorf("pipelineview: port %q.%q is not an output class", r.Stage, r.Port)
			}
			if !source && cl != PortConfig {
				return fmt.Errorf("pipelineview: port %q.%q is not an input class", r.Stage, r.Port)
			}
		default:
			return fmt.Errorf("pipelineview: empty ref")
		}
		return nil
	}
	for _, e := range p.Edges {
		if err := checkRef(e.From, true); err != nil {
			return err
		}
		if err := checkRef(e.To, false); err != nil {
			return err
		}
		if e.From.IsEndpoint() && e.To.IsEndpoint() {
			return fmt.Errorf("pipelineview: endpoint-to-endpoint edge %q -> %q", e.From.Endpoint, e.To.Endpoint)
		}
		if !e.From.IsEndpoint() && !e.To.IsEndpoint() && (e.From.Port != "" || e.To.Port != "") {
			return fmt.Errorf("pipelineview: stage-to-stage edge on named ports (%q.%q -> %q.%q) is not supported; route it through an endpoint",
				e.From.Stage, e.From.Port, e.To.Stage, e.To.Port)
		}
	}
	return nil
}

// ToGraphModel converts the pipeline into the layeredgraph model — the
// documented handoff for graphs that outgrow the spine idiom (ADR-0119 SD3):
// stages become boxes, endpoints become ellipses, implied spine edges and
// explicit edges become plain edges. Ports (and their classes) flatten away;
// call Validate first if the model is untrusted.
func (p Pipeline) ToGraphModel() layeredgraph.GraphModel {
	var m layeredgraph.GraphModel
	walkStages(p.Root, func(st Stage) {
		m.Nodes = append(m.Nodes, layeredgraph.Node{ID: st.ID, Label: labelOr(st.Label, st.ID), Shape: layeredgraph.NodeShapeBox})
	})
	for _, ep := range p.Endpoints {
		m.Nodes = append(m.Nodes, layeredgraph.Node{ID: ep.ID, Label: labelOr(ep.Label, ep.ID), Shape: layeredgraph.NodeShapeEllipse})
	}
	for _, pr := range impliedSpineEdges(p.Root) {
		m.Edges = append(m.Edges, layeredgraph.Edge{From: pr[0], To: pr[1]})
	}
	for _, e := range p.Edges {
		m.Edges = append(m.Edges, layeredgraph.Edge{From: e.From.Key(), To: e.To.Key(), Label: e.Label})
	}
	return m
}

func labelOr(label, id string) string {
	if label == "" {
		return id
	}
	return label
}
