package play

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"
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
	lensEstimate // EXPLAIN ESTIMATE — tabular per-table parts/rows/marks
	lensIndexes  // EXPLAIN PLAN indexes=1 — the plan with per-read index usage
	lensLineage  // column-level lineage of the SELECT list — local, like statement
)

// remote reports whether the lens needs a server round-trip — everything but
// the two local derivations (statement, lineage).
func (l flowLens) remote() bool { return l != lensStatement && l != lensLineage }

func (l flowLens) String() string {
	switch l {
	case lensAST:
		return "ast"
	case lensPlan:
		return "plan"
	case lensPipeline:
		return "pipeline"
	case lensEstimate:
		return "estimate"
	case lensIndexes:
		return "indexes"
	case lensLineage:
		return "lineage"
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
	case lensEstimate:
		kind = "EXPLAIN ESTIMATE"
	case lensIndexes:
		kind = "EXPLAIN PLAN indexes = 1, json = 1"
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
// The document is [{"Plan": <node>}] with nested "Plans" children; with
// `indexes = 1` the ReadFrom* steps additionally carry an "Indexes" array.
type explainPlanNode struct {
	NodeType    string             `json:"Node Type"`
	NodeID      string             `json:"Node Id"`
	Description string             `json:"Description"`
	Plans       []explainPlanNode  `json:"Plans"`
	Indexes     []explainPlanIndex `json:"Indexes"`
}

// explainPlanIndex is one index-usage entry of an indexes=1 plan: what the
// index selected against what existed. Keys/Name/Condition appear per index
// kind (PrimaryKey carries Condition, a skip index carries Name, …).
type explainPlanIndex struct {
	Type             string   `json:"Type"`
	Name             string   `json:"Name"`
	Keys             []string `json:"Keys"`
	Condition        string   `json:"Condition"`
	InitialParts     int64    `json:"Initial Parts"`
	SelectedParts    int64    `json:"Selected Parts"`
	InitialGranules  int64    `json:"Initial Granules"`
	SelectedGranules int64    `json:"Selected Granules"`
}

// summary renders one index entry for a node's Detail — the selected/initial
// ratios are the payload (how much the index pruned).
func (ix explainPlanIndex) summary() string {
	var b strings.Builder
	b.WriteString(ix.Type)
	if ix.Name != "" {
		b.WriteString(" ")
		b.WriteString(ix.Name)
	}
	if len(ix.Keys) > 0 {
		b.WriteString(" keys(")
		b.WriteString(strings.Join(ix.Keys, ", "))
		b.WriteString(")")
	}
	if ix.Condition != "" && ix.Condition != "true" {
		b.WriteString(" cond ")
		b.WriteString(truncateRunes(ix.Condition, 48))
	}
	fmt.Fprintf(&b, " parts %d/%d granules %s/%s",
		ix.SelectedParts, ix.InitialParts,
		humanize.Comma(int64(ix.SelectedGranules)), humanize.Comma(int64(ix.InitialGranules)))
	return b.String()
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
		detail := n.Description
		for _, ix := range n.Indexes {
			if detail != "" {
				detail += " · "
			}
			detail += ix.summary()
		}
		if !a.addNode(flowNode{ID: id, Kind: kind,
			Label: truncateRunes(n.NodeType, flowLabelRunes), Detail: truncateRunes(detail, flowSnippetRunes)}) {
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

// parseExplainEstimate maps `EXPLAIN ESTIMATE` rows — tab-joined cells of
// (database, table, parts, rows, marks), one per MergeTree table the
// statement reads — onto the IR: each table is a source node whose detail
// carries the estimate, draining into one terminal so the drawing reads like
// the statement lens's sources→result. A statement reading no MergeTree
// tables estimates empty, which the panel reports as such.
func parseExplainEstimate(lines []string) flowGraph {
	a := newLensGraphAssembler()
	tables := make([]string, 0, len(lines))
	for _, ln := range lines {
		cells := strings.Split(ln, "\t")
		if len(cells) < 5 {
			continue
		}
		id := cells[0] + "." + cells[1]
		if _, dup := a.ids[id]; dup {
			continue
		}
		if !a.addNode(flowNode{ID: id, Kind: flowSourceTable,
			Label:  truncateRunes(id, flowLabelRunes),
			Detail: "parts " + cells[2] + " · rows " + cells[3] + " · marks " + cells[4]}) {
			break
		}
		tables = append(tables, id)
	}
	if len(tables) == 0 {
		return a.g
	}
	if a.addNode(flowNode{ID: "estimate", Kind: flowResult, Label: "read estimate"}) {
		for _, id := range tables {
			a.addEdge(id, "estimate")
		}
	}
	return a.g
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
	case lensPlan, lensIndexes:
		return parseExplainPlanJSON(strings.Join(lines, "\n"))
	case lensEstimate:
		return parseExplainEstimate(lines), nil
	}
	return flowGraph{}, nil
}
