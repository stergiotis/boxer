package play

import (
	"github.com/apache/arrow-go/v18/arrow"
)

// play_tab_marks.go implements the ADR-0097 2026-07-27 Update: the dock strip
// carries the graph. A tab title gains at most one mark for what the graph
// already knows about that pane — whether it can draw the result it would be
// handed (SD6's AcceptForChannel) and whether it writes a signal the current
// split reads (a write-back edge, the relation the system graph draws) — and
// the mark composes with 6c's binding decoration: "Table · by_kind ●".
//
// The ladder below is pure and UI-free. Two properties are load-bearing:
//
//   - It runs for EVERY registered tab every frame, hidden tabs included, so
//     it must stay cheap. AcceptForChannel is a schema scan by contract (SD6);
//     nothing here adds more than a walk over the split's nodes.
//   - It must never DEMAND (SD2). A channel filled by a named split node is
//     judged structurally — is the node in the split — rather than by asking
//     its lane, which would execute a query every frame for a hidden tab.

// tabMarkE is the single mark a tab title carries. One mark, one slot: the
// strip is a menu the eye scans, and a second mark per tab buys detail nobody
// reads at the cost of width every tab pays.
type tabMarkE uint8

const (
	tabMarkNone        tabMarkE = iota
	tabMarkShapeReject          // this pane cannot draw the result's shape
	tabMarkBlocked              // it writes a name the query reads that nothing has filled
	tabMarkDrives               // it writes a name the query reads
	tabMarkNotice               // chrome with something to report
)

// glyph is the mark's character on the strip, or "" for no mark.
//
// ASCII, for two reasons found by looking at the running app. `×` sits one
// space from the dock's own per-tab close glyph, where a second ✕-shape reads
// as a second close button. And the app's SVG export — its own toolbar button,
// and the scripted-screenshot path — does not carry non-ASCII glyphs through
// reliably: `●` and `✓` render live but come out as tofu in an exported
// capture, so a non-ASCII mark would be missing from exactly the artifacts
// that get shared. (The font itself is fine: `●` paints correctly in the Graph
// view. `→` is the one the endpoint switcher records as genuinely absent.)
//
// Blocked and notice deliberately share `!`: both mean "attention here", the
// strip has room for one attention class, and no tab can hold both (a notice
// is chrome's, and chrome declares no writes).
func (inst tabMarkE) glyph() (g string) {
	switch inst {
	case tabMarkShapeReject:
		g = "-"
	case tabMarkBlocked, tabMarkNotice:
		g = "!"
	case tabMarkDrives:
		g = "*"
	}
	return
}

// tabVerdict is everything one tab's mark derives from, passed in rather than
// read off PlayApp so the ladder is testable on its own.
type tabVerdict struct {
	// schema is what a frame-fed channel would be offered: the tab's own
	// (possibly bound) result schema. nil means nothing has landed, and the
	// shape verdict is then unknown rather than negative — before the first
	// Run every panel rejects, and a strip of rejects says nothing.
	schema *arrow.Schema
	// split is the last Run's recovered node graph — the source of both the
	// structural channel verdict and the names the query reads.
	split splitResult
	sig   SignalEnvI
	// bound are the names the buffer's SET prelude pins. A pinned name
	// shadows the signal at execution (slice-5 D1), so a pane writing it does
	// not drive the query.
	bound map[string]string
	// notice marks chrome that has something to report this frame.
	notice bool
}

// tabMark is the mark ladder, in precedence order: a pane that cannot draw
// this result is not a control over it either, so the shape verdict outranks
// the signal marks; an unfilled name outranks a filled one because it names
// the gesture that unblocks a refused Run.
func tabMark(spec *TabSpec, in tabVerdict) (mark tabMarkE) {
	if spec.ShapeContract && spec.Panel != nil && shapeRejects(spec.Panel, in) {
		return tabMarkShapeReject
	}
	drives, blocked := signalRelation(spec, in)
	switch {
	case blocked:
		mark = tabMarkBlocked
	case drives:
		mark = tabMarkDrives
	case in.notice:
		mark = tabMarkNotice
	}
	return
}

// shapeRejects reports that a panel's required channels cannot be filled from
// what this frame would offer them. Only a POSITIVE rejection is reported: an
// unknown offer (no result yet, no split, a channel this mapping does not
// model, or a split node whose columns are unknown without executing) yields
// false, so the strip stays silent rather than guessing. "Does not reject" is
// therefore not a promise that the pane will render.
func shapeRejects(p PanelI, in tabVerdict) (rejects bool) {
	for _, ch := range p.Channels() {
		if !ch.Required {
			continue // an optional channel that cannot be filled is simply absent
		}
		if node, fromSplit := splitFedChannel(ch.ID); fromSplit {
			if len(in.split.Nodes) == 0 {
				continue // nothing has been split yet — unknown, not absent
			}
			if _, ok := findSplitNode(in.split, node); !ok {
				return true // the CTE this channel needs is not in the buffer
			}
			continue // present, but its shape needs an execution to know (SD2)
		}
		if !frameFedChannel(ch.ID) || in.schema == nil {
			continue
		}
		if _, reason := p.AcceptForChannel(ch.ID, in.schema, in.sig); reason != "" {
			return true
		}
	}
	return
}

// splitFedChannel maps a channel to the split node that fills it BY NAME — the
// Network's two CTEs and the Kanban's lane inventory. Their verdict is
// structural because asking the lane would execute the query (SD2).
//
// The Timeline's bands channel is deliberately absent: it is panel-authored
// (the bands editor's own SQL, not a node of the user's split) and optional,
// so it never reaches a required-channel verdict.
func splitFedChannel(ch ChannelID) (node NodeID, ok bool) {
	switch ch {
	case chEdges:
		return networkEdgesNodeID, true
	case chVertices:
		return networkVerticesNodeID, true
	case chLanes:
		return kanbanLanesNodeID, true
	}
	return
}

// frameFedChannel reports the channels filled from the tab's own frame view —
// the active result, or a bound node's (6c).
func frameFedChannel(ch ChannelID) bool { return ch == chMain || ch == chEvents }

// signalRelation reports whether the tab writes a name some split node reads
// (drives) and, of those, whether one is unfilled (blocked) — the same
// condition the Run gate refuses on, attributed to the pane that can fix it.
//
// A SET-bound name counts for neither: the constant shadows the signal at
// execution (D1), so writing it moves nothing until the SET goes.
func signalRelation(spec *TabSpec, in tabVerdict) (drives, blocked bool) {
	if len(spec.Writes) == 0 {
		return
	}
	reads := splitReads(in.split)
	if len(reads) == 0 {
		return
	}
	for _, w := range declaredWrites(spec) {
		name := string(w)
		if !reads[name] {
			continue
		}
		if _, pinned := in.bound[name]; pinned {
			continue
		}
		drives = true
		if signalDefaultsEmpty(name) {
			continue // a reserved String signal never blocks a Run
		}
		if in.sig != nil {
			if _, held := in.sig.Get(name); held {
				continue
			}
		}
		blocked = true
	}
	return
}

// splitReads is the union of the names the split's nodes read — the signal
// edges of the recovered graph, SET-bound names included (the caller filters
// those, as the system-graph drawing does).
func splitReads(split splitResult) (out map[string]bool) {
	for i := range split.Nodes {
		for _, r := range split.Nodes[i].Reads {
			if out == nil {
				out = make(map[string]bool, 4)
			}
			out[string(r)] = true
		}
	}
	return
}

// declaredWrites expands a tab's declared writes with the companions the
// dispatcher stamps on its behalf: a pane that publishes `selection` also
// publishes `selection_node` and, on a leeway result, `selection_id`
// (selectionStamper), so a query cross-filtering on `{selection_id:UInt64}` is
// driven by every selecting pane and not just by the one that declared it.
func declaredWrites(spec *TabSpec) (out []SignalID) {
	out = spec.Writes
	for _, w := range spec.Writes {
		if w != signalSelection {
			continue
		}
		out = append(append([]SignalID(nil), out...), signalSelectionNode, signalSelectionID)
		break
	}
	return
}

// tabTitle is one tab's dock title this frame: 6c's binding decoration plus at
// most one mark. Dock identity is the DockID, so a varying title is safe.
func (inst *PlayApp) tabTitle(spec *TabSpec, f *TabFrame) (title string) {
	title = inst.boundTabTitle(spec)
	if g := tabMark(spec, inst.tabVerdictFor(spec, f)).glyph(); g != "" {
		title += " " + g
	}
	return
}

// tabVerdictFor gathers the frame facts the ladder judges. The schema is the
// TAB's, not the active result's: a bound pane is marked for what it actually
// renders (6c).
func (inst *PlayApp) tabVerdictFor(spec *TabSpec, f *TabFrame) (v tabVerdict) {
	v = tabVerdict{
		split:  inst.currentSplit,
		sig:    inst.frameSig,
		bound:  inst.paramSyncedValues,
		notice: inst.tabNotice(spec, f),
	}
	if f != nil {
		v.schema = f.Schema
	}
	return
}

// tabNotice reports chrome with something to say. Diagnostics is its only
// holder today: a failed run, a truncated result, or a standing emit drop —
// three facts already computed this frame.
//
// Deliberately NOT included: a skipped pre-execute pass. Its trace is
// demand-driven on purpose (rewriteTraceFor memoises a full client-side
// rewrite, and a session with neither the Passes nor the Diagnostics tab open
// never pays for it), and reading it from the title path would force that cost
// on every session — the same class of hazard as demanding a lane, in a
// different resource. It joins the notice if the trace ever becomes a
// per-frame product.
func (inst *PlayApp) tabNotice(spec *TabSpec, f *TabFrame) (notice bool) {
	if spec.ID != "diagnostics" {
		return
	}
	if f != nil && f.Err != nil {
		return true
	}
	if inst.activeTruncation() != "" {
		return true
	}
	return inst.graph != nil && inst.graph.hasEmitDrops()
}
