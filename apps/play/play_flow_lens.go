package play

import (
	"encoding/json"
	"strconv"
	"strings"
)

// play_flow_lens.go is the ADR-0153 lens layer: ClickHouse EXPLAIN outputs
// parsed into the same flowGraph IR the static statement lens produces. The
// server sees `SELECT * FROM (EXPLAIN <kind> <stmt>)` — the subquery form
// keeps the outer statement an ordinary SELECT, so FORMAT, URL params and
// endpoint dispatch all behave exactly as for any other node (verified against
// ClickHouse 26.7: params substitute inside the explained statement). A SET
// prelude cannot ride inside the parens, so the wrapper re-lifts it in front.
//
// Parsers are pure line/JSON transforms — no CST types, no UI — and each maps
// one output dialect:
//   - EXPLAIN AST: one-space-per-level indentation.
//   - EXPLAIN PLAN json=1: a recursive {"Node Type", "Plans": […]} document.
//   - EXPLAIN PIPELINE: two-space indentation with "(PlanStep)" group-marker
//     lines that label the processors following them.
// Edges point child→parent throughout: leaves (ReadFrom*, literals, source
// transforms) sit where the statement lens puts its sources, the root where it
// puts the result.

// flowLens selects what the Flow tab derives its graph from.
type flowLens uint8

const (
	lensStatement flowLens = iota // static grammar1 derivation (ADR-0153 §SD2)
	lensAST
	lensPlan
	lensPipeline
)

// remote reports whether the lens needs a server round-trip (everything but
// the static statement lens).
func (l flowLens) remote() bool { return l != lensStatement }

func (l flowLens) String() string {
	switch l {
	case lensAST:
		return "ast"
	case lensPlan:
		return "plan"
	case lensPipeline:
		return "pipeline"
	}
	return "statement"
}

// explainWrap returns a remote lens's wire-body wrap for
// ExecOptions.WrapStatement: the lane compiles and routes the PLAIN fused
// statement — placement resolves from it, the pre-execute rewrites apply to
// it, SET preludes are harvested off it — and the transport wraps the
// residual at the last moment. Index structure and schema are endpoint-local,
// so the EXPLAIN must interrogate the endpoint the statement itself routes
// to; wrapping earlier would also hide the statement from the rewrites
// (grammar1 cannot parse the wrapper, so every pass would skip). nil for the
// static statement lens.
func explainWrap(l flowLens) func(string) string {
	var kind string
	switch l {
	case lensAST:
		kind = "EXPLAIN AST"
	case lensPlan:
		kind = "EXPLAIN PLAN json = 1"
	case lensPipeline:
		kind = "EXPLAIN PIPELINE"
	default:
		return nil
	}
	return func(residual string) string {
		return "SELECT * FROM (" + kind + " " + strings.TrimRight(strings.TrimSpace(residual), ";") + ")"
	}
}

// explainUnsupportedByEndpoint recognises the failure class "this endpoint's
// SQL surface has no EXPLAIN" — today that is the in-process introspection
// plane, whose keelsonsql parser rejects the wrapper with its own error
// namespace. Routing is working as designed when this fires (the statement
// itself runs on that endpoint); only the lens has nothing to ask, so the
// panel says that in plain language instead of relaying a parse error.
func explainUnsupportedByEndpoint(err error) bool {
	return err != nil && strings.Contains(err.Error(), "keelsonsql:")
}

// lensGraphAssembler accumulates a flowGraph under the shared caps, with the
// same both-endpoints edge guard as the statement builder.
type lensGraphAssembler struct {
	g   flowGraph
	ids map[string]struct{}
}

func newLensGraphAssembler() *lensGraphAssembler {
	return &lensGraphAssembler{ids: make(map[string]struct{}, 32)}
}

func (a *lensGraphAssembler) addNode(n flowNode) bool {
	if len(a.g.Nodes) >= flowMaxNodes {
		a.g.Capped = true
		return false
	}
	a.ids[n.ID] = struct{}{}
	a.g.Nodes = append(a.g.Nodes, n)
	return true
}

func (a *lensGraphAssembler) addEdge(from, to string) {
	if from == "" || to == "" {
		return
	}
	if _, ok := a.ids[from]; !ok {
		a.g.Capped = true
		return
	}
	if _, ok := a.ids[to]; !ok {
		a.g.Capped = true
		return
	}
	a.g.Edges = append(a.g.Edges, flowEdge{From: from, To: to})
}

// indentNode is one line of an indentation-shaped EXPLAIN output.
type indentNode struct {
	depth  int
	label  string
	detail string
}

// buildIndentGraph folds a depth-ordered line sequence into a tree by the
// nearest-shallower-ancestor rule and emits child→parent edges. IDs are
// positional ("e0", "e0.1", …) so identical output yields identical graphs.
func buildIndentGraph(nodes []indentNode) flowGraph {
	a := newLensGraphAssembler()
	type frame struct {
		depth int
		id    string
		kids  int
	}
	stack := make([]frame, 0, 8)
	roots := 0
	for _, n := range nodes {
		for len(stack) > 0 && stack[len(stack)-1].depth >= n.depth {
			stack = stack[:len(stack)-1]
		}
		var id, parent string
		if len(stack) == 0 {
			id = "e" + strconv.Itoa(roots)
			roots++
		} else {
			top := &stack[len(stack)-1]
			id = top.id + "." + strconv.Itoa(top.kids)
			top.kids++
			parent = top.id
		}
		kind := flowOp
		if strings.HasPrefix(n.label, "ReadFrom") {
			kind = flowSourceTable
		}
		if !a.addNode(flowNode{ID: id, Kind: kind,
			Label: truncateRunes(n.label, flowLabelRunes), Detail: truncateRunes(n.detail, flowSnippetRunes)}) {
			break
		}
		a.addEdge(id, parent)
		stack = append(stack, frame{depth: n.depth, id: id})
	}
	return a.g
}

// parseExplainAST maps `EXPLAIN AST` lines (one space per level) onto the IR.
func parseExplainAST(lines []string) flowGraph {
	nodes := make([]indentNode, 0, len(lines))
	for _, ln := range lines {
		trimmed := strings.TrimLeft(ln, " ")
		if trimmed == "" {
			continue
		}
		nodes = append(nodes, indentNode{depth: len(ln) - len(trimmed), label: trimmed})
	}
	return buildIndentGraph(nodes)
}

// parseExplainPipeline maps `EXPLAIN PIPELINE` lines (two spaces per level)
// onto the IR. "(PlanStep)" group-marker lines are not processors: they label
// the processors that follow them, so they fold into those nodes' Detail.
func parseExplainPipeline(lines []string) flowGraph {
	nodes := make([]indentNode, 0, len(lines))
	group := ""
	for _, ln := range lines {
		trimmed := strings.TrimLeft(ln, " ")
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "(") && strings.HasSuffix(trimmed, ")") {
			group = strings.Trim(trimmed, "()")
			continue
		}
		nodes = append(nodes, indentNode{depth: (len(ln) - len(trimmed)) / 2, label: trimmed, detail: group})
	}
	return buildIndentGraph(nodes)
}

// explainPlanNode is the shape of one step in `EXPLAIN PLAN json = 1` output.
// The document is [{"Plan": <node>}] with nested "Plans" children.
type explainPlanNode struct {
	NodeType    string            `json:"Node Type"`
	NodeID      string            `json:"Node Id"`
	Description string            `json:"Description"`
	Plans       []explainPlanNode `json:"Plans"`
}

// parseExplainPlanJSON maps `EXPLAIN PLAN json = 1` output onto the IR. IDs
// prefer the server's "Node Id" (unique per plan) with a positional fallback.
func parseExplainPlanJSON(doc string) (flowGraph, error) {
	var root []struct {
		Plan explainPlanNode `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(doc), &root); err != nil {
		return flowGraph{}, err
	}
	a := newLensGraphAssembler()
	var walk func(n explainPlanNode, path, parent string)
	walk = func(n explainPlanNode, path, parent string) {
		id := n.NodeID
		if id == "" {
			id = path
		}
		kind := flowOp
		if strings.HasPrefix(n.NodeType, "ReadFrom") {
			kind = flowSourceTable
		}
		if !a.addNode(flowNode{ID: id, Kind: kind,
			Label: truncateRunes(n.NodeType, flowLabelRunes), Detail: truncateRunes(n.Description, flowSnippetRunes)}) {
			return
		}
		a.addEdge(id, parent)
		for i := range n.Plans {
			walk(n.Plans[i], path+"."+strconv.Itoa(i), id)
		}
	}
	for i := range root {
		walk(root[i].Plan, "e"+strconv.Itoa(i), "")
	}
	return a.g, nil
}

// parseLensRecord dispatches a lens's result lines to its parser. PLAN's
// json=1 output may arrive as one row or as split lines — joining is total
// either way.
func parseLensRecord(l flowLens, lines []string) (flowGraph, error) {
	switch l {
	case lensAST:
		return parseExplainAST(lines), nil
	case lensPipeline:
		return parseExplainPipeline(lines), nil
	case lensPlan:
		return parseExplainPlanJSON(strings.Join(lines, "\n"))
	}
	return flowGraph{}, nil
}
