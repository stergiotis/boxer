package play

import (
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// play_ts_collision.go warns when a name play executes itself is ALSO a
// function the server has (ADR-0163 §SD4). Inside play the buffer vocabulary
// wins — that is the decision, not a bug — but a user whose server carries a
// `tsProfile` UDF is entitled to know that the number on screen did not come
// from it.
//
// It is a warning and never a refusal. Refusing would break a buffer that
// worked yesterday because someone else created a UDF today, and the whole
// point of reserving the family was to make play's answer predictable.
//
// The probe cannot live in the classifier: splitGraph is pure — it parses and
// analyses, it does not execute — which is what lets the split run on every
// keystroke. So the shadowing question is asked HERE, by the chrome that
// displays a client node, against a set fetched once.

// tsCollisionQuery asks only about the reserved family. `ts%` is a prefix a
// server may legitimately populate, so the result is filtered again in Go
// against the same reserved-name rule recognition uses — asking the server to
// spell that rule in SQL would be two statements of one policy.
const tsCollisionQuery = "SELECT name FROM system.functions WHERE name LIKE 'ts%' ORDER BY name"

// tsCollisionTimeout bounds the probe. It is chrome: a server that is slow to
// answer must delay a caption, never a result.
const tsCollisionTimeout = 5 * time.Second

// tsCollisionProbe caches the server's `ts*` function names. The lane IS the
// cache — its memo key is the SQL, which never changes, so the query runs once
// per session and every later demand is served from the held result.
type tsCollisionProbe struct {
	lane  *nodeLane
	names map[string]bool
}

func newTsCollisionProbe(client *Client) (inst *tsCollisionProbe) {
	return &tsCollisionProbe{
		lane: newNodeLane(clientExecutor{client: client, opts: newExecOptions("ts-collision")},
			memory.NewGoAllocator(), tsCollisionTimeout),
	}
}

// shadows reports whether the server has a function of this name. It demands
// the probe on first ask and answers false until the result lands — an
// unanswered probe must read as "nothing known", never as a warning nobody
// can act on.
func (inst *tsCollisionProbe) shadows(name string) (hit bool) {
	if inst == nil || inst.lane == nil {
		return false
	}
	if inst.names == nil {
		view := inst.lane.demand(compiledNode{SQL: tsCollisionQuery})
		if view.rec == nil {
			return false
		}
		inst.names = make(map[string]bool, 4)
		col := view.rec.Column(0)
		for row := range int(view.rec.NumRows()) {
			n := col.ValueStr(row)
			if tsIsReservedName(n) {
				inst.names[n] = true
			}
		}
	}
	return inst.names[name]
}

func (inst *tsCollisionProbe) close() {
	if inst != nil && inst.lane != nil {
		inst.lane.close()
	}
}

// renderTsCollisionWarning paints the shadowing note for one client node, if
// the probe has an answer and the answer is yes.
func (inst *PlayApp) renderTsCollisionWarning(call *tsCall) {
	if !inst.tsCollisions.shadows(call.Spec.Name) {
		return
	}
	for rt := range c.RichTextLabel("this server also has a function called `" + call.Spec.Name +
		"`. Inside play the buffer vocabulary wins, so what you see is play's — the server's is " +
		"reachable from any other client, and from this one under a different name.") {
		rt.Small().Weak()
	}
}

// tsClientNodesOf lists the client calls of a split, for the panes that
// caption them. Order follows the split's own.
func tsClientNodesOf(split splitResult) (out []splitNode) {
	for i := range split.Nodes {
		if split.Nodes[i].Client != nil {
			out = append(out, split.Nodes[i])
		}
	}
	return
}

// tsEngineSummary is the one-line engine split for a buffer, for the places
// that report on the whole statement rather than on one node. Empty when the
// buffer holds no client call, so an ordinary session never sees it.
func tsEngineSummary(split splitResult) (line string) {
	nodes := tsClientNodesOf(split)
	if len(nodes) == 0 {
		return ""
	}
	names := make([]string, 0, len(nodes))
	for i := range nodes {
		names = append(names, string(nodes[i].ID)+" ("+nodes[i].Client.Spec.Name+")")
	}
	return "two engines: " + strings.Join(names, ", ") +
		" computed in play, everything else on ClickHouse"
}
