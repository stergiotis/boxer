// Package layoutcheck reads the tree sidecar a scene capture writes
// (`<capture>.tree.jsonl`) and reports where its layout breaks a generic
// invariant: a control past the side of the viewport or of its window, two
// controls over each other, a control too small to hit, text the host drew
// elided (ADR-0363 M1).
//
// The checker knows no app. What an app means to do that looks like a
// violation is an [Exception] recorded in the scene document, never a rule
// here.
//
// What the tree can and cannot say shapes every rule. egui fills a node's
// bounds from the widget's response rect, unclipped, and marks nothing hidden
// for being scrolled away or clipped by a panel; a node's text is the text it
// was given, not what was drawn. So a row a scroll area has moved below the
// fold and a control a panel has clipped look the same, and an elided label
// carries its full text. The rules below are drawn around those two facts.
package layoutcheck

import (
	"bufio"
	"encoding/json/v2"
	"io"
	"iter"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/carrierclient"
)

// Node is one line of a tree sidecar ([carrierclient.WriteTreeJSONL]). Depth
// counts the node's ancestors that are themselves in the sidecar, not its
// depth in the AccessKit tree, which is mostly unnamed containers.
type Node struct {
	ID    uint64  `json:"id"`
	Role  string  `json:"role"`
	Name  string  `json:"name"`
	Value string  `json:"value"`
	CX    float32 `json:"cx"`
	CY    float32 `json:"cy"`
	X     float32 `json:"x"`
	Y     float32 `json:"y"`
	W     float32 `json:"w"`
	H     float32 `json:"h"`
	Flags uint32  `json:"flags"`
	Depth int     `json:"depth"`
}

// IsDisabled reports the node's disabled flag.
func (inst Node) IsDisabled() bool {
	return inst.Flags&carrierclient.FlagDisabled != 0
}

// hasBounds is false for a node that was never laid out: the host reports a
// zero rect for it rather than dropping it.
func (inst Node) hasBounds() bool {
	return inst.W > 0 && inst.H > 0
}

// Label names the node the way a trace anchors it: role, then the name, or
// the value for a node that has none, then the id.
func (inst Node) Label() string {
	text := inst.Name
	if text == "" && inst.Value != "" {
		text = "=" + inst.Value
	}
	if len([]rune(text)) > 48 {
		text = string([]rune(text)[:48]) + "…"
	}
	return inst.Role + " " + strconv.Quote(text) + " #" + strconv.FormatUint(inst.ID, 10)
}

func (inst Node) treeNode() *carrierclient.TreeNode {
	return &carrierclient.TreeNode{Id: inst.ID, Role: inst.Role, Name: inst.Name, Value: inst.Value, Flags: inst.Flags}
}

// ReadTree decodes a tree sidecar.
func ReadTree(r io.Reader) (nodes []Node, err error) {
	sc := bufio.NewScanner(r)
	// A text edit's value is its whole buffer, on one line.
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		b := sc.Bytes()
		if len(strings.TrimSpace(string(b))) == 0 {
			continue
		}
		var n Node
		if err = json.Unmarshal(b, &n); err != nil {
			return nil, eb.Build().Int("line", line).Errorf("unable to decode a tree sidecar line: %w", err)
		}
		nodes = append(nodes, n)
	}
	if err = sc.Err(); err != nil {
		return nil, eh.Errorf("unable to read the tree sidecar: %w", err)
	}
	return nodes, nil
}

// InvariantE names one layout invariant. The spelling is what a scene's
// exception gives.
type InvariantE string

const (
	// InvariantOutside: an interactive node reaches past the left or right
	// edge of the viewport, or of the egui window it is in.
	InvariantOutside InvariantE = "outside"
	// InvariantOverlap: two enabled interactive siblings cover each other.
	// Advisory (Blocking is false): tree bounds are not clipped by scrolling,
	// so content scrolled under a fixed control, or drawn inside an egui
	// Scene, overlaps in the tree where it does not on screen.
	InvariantOverlap InvariantE = "overlap"
	// InvariantHitSize: an interactive node is narrower or shorter than the
	// minimum hit size.
	InvariantHitSize InvariantE = "hit-size"
	// InvariantClipped: the host drew a node's text elided with "…" where
	// the node's own text is longer. Needs the capture's svg sidecar.
	InvariantClipped InvariantE = "clipped"
)

var AllInvariants = []InvariantE{InvariantOutside, InvariantOverlap, InvariantHitSize, InvariantClipped}

// Blocking reports whether a finding of this invariant fails a scene. An
// advisory one is reported beside the result and fails nothing.
func (inst InvariantE) Blocking() bool {
	return inst != InvariantOverlap
}

const (
	// DefaultMinHit is the smallest width and height, in logical points, an
	// interactive node may have: egui's `icon_width`, the size it gives its
	// own smallest controls (a window's close button, a check box's box).
	// Under it is smaller than anything egui draws as a control. egui's
	// standard row (`interact_size.y`) is 18; a dense table row is shorter,
	// and a floor of 16 fails it in every scene that shows one. WCAG 2.5.8 asks for 24 with an exception for well-spaced targets, and
	// the checker does not model spacing. A scene that wants a stricter
	// floor is a Go test over [Check] with Options.MinHit.
	DefaultMinHit float32 = 14
	// DefaultTolerance absorbs sub-point rounding: egui snaps rects to the
	// pixel grid, and adjacent controls in a segmented row share an edge.
	DefaultTolerance float32 = 1
)

// Options parameterise [Check].
type Options struct {
	// ViewportW and ViewportH are the window in logical points. Zero skips
	// InvariantOutside.
	ViewportW float32
	ViewportH float32
	// MinHit is the minimum width and height. Zero means DefaultMinHit.
	MinHit float32
	// Tolerance is how far a node may cross an edge, or two nodes overlap in
	// each axis, before it counts. Zero means DefaultTolerance.
	Tolerance float32
}

// Finding is one broken invariant.
type Finding struct {
	Invariant InvariantE
	Node      Node
	// Other is the second node of an overlap.
	Other *Node
	// Detail says by how much, in the finding's own terms.
	Detail string
}

func (inst Finding) String() string {
	s := string(inst.Invariant) + "  " + inst.Node.Label()
	if inst.Other != nil {
		s += "  ×  " + inst.Other.Label()
	}
	if inst.Detail != "" {
		s += "  (" + inst.Detail + ")"
	}
	return s
}

// interactiveRoles are the AccessKit roles a person points at or types into,
// in the spelling the tree uses. The set is what the host emits
// (`role_name` in the Rust host) for egui's widgets and the menu, tab and
// switch roles AccessKit defines for them. `splitter` is left out: the host
// emits it for a window's resize edges, which are 6 points thick and lie
// across the window's border by design.
var interactiveRoles = map[string]bool{
	"button": true, "default_button": true, "toggle_button": true,
	"check_box": true, "radio_button": true, "switch": true,
	"link": true, "combo_box": true, "editable_combo_box": true,
	"menu_item": true, "menu_item_check_box": true, "menu_item_radio": true,
	"tab": true, "slider": true, "spin_button": true, "color_well": true,
	"disclosure_triangle": true,
}

// IsInteractiveRole reports whether a role is one the invariants apply to.
func IsInteractiveRole(role string) bool {
	return interactiveRoles[role] || carrierclient.IsEditableRole(role)
}

// Check applies InvariantOutside, InvariantOverlap and InvariantHitSize to
// one sidecar's nodes. Findings come in the sidecar's order, outside and
// hit-size before the overlaps a node takes part in.
func Check(nodes []Node, opts Options) (findings []Finding) {
	minHit := opts.MinHit
	if minHit <= 0 {
		minHit = DefaultMinHit
	}
	tol := opts.Tolerance
	if tol <= 0 {
		tol = DefaultTolerance
	}
	parent := parents(nodes)
	for i, n := range nodes {
		if !IsInteractiveRole(n.Role) || !n.hasBounds() {
			continue
		}
		// Only the horizontal edges, of the viewport and of the egui window
		// the node is in: a scroll area lays out its whole content and the
		// tree carries it unclipped, so a row below the fold is
		// indistinguishable from a control that does not fit. Rows scroll
		// vertically; a toolbar or a status bar that overflows a narrow
		// window runs out sideways, and the window clips it.
		span := "x " + pt(n.X) + "…" + pt(n.X+n.W)
		w := enclosingWindow(nodes, parent, i)
		switch {
		case opts.ViewportW > 0 && max(-n.X, n.X+n.W-opts.ViewportW) > tol:
			findings = append(findings, Finding{Invariant: InvariantOutside, Node: n,
				Detail: span + " in a viewport " + pt(opts.ViewportW) + " wide"})
		case w >= 0 && max(nodes[w].X-n.X, n.X+n.W-nodes[w].X-nodes[w].W) > tol:
			win := nodes[w]
			findings = append(findings, Finding{Invariant: InvariantOutside, Node: n,
				Detail: span + " in window " + strconv.Quote(strings.TrimSpace(win.Name)) + " at x " + pt(win.X) + "…" + pt(win.X+win.W)})
		}
		if n.W < minHit || n.H < minHit {
			findings = append(findings, Finding{Invariant: InvariantHitSize, Node: n,
				Detail: pt(n.W) + "×" + pt(n.H) + " under " + pt(minHit)})
		}
	}
	for i, j := range overlappingSiblings(nodes, parent, tol) {
		a, b := nodes[i], nodes[j]
		findings = append(findings, Finding{Invariant: InvariantOverlap, Node: a, Other: &b,
			Detail: pt(min(a.X+a.W, b.X+b.W)-max(a.X, b.X)) + "×" + pt(min(a.Y+a.H, b.Y+b.H)-max(a.Y, b.Y))})
	}
	return findings
}

// parents returns each node's parent index in the sidecar, -1 for a node at
// depth 0. The sidecar lists nodes depth-first, so a node's parent is the
// nearest earlier node one level up.
func parents(nodes []Node) (parent []int) {
	parent = make([]int, len(nodes))
	// stack[d] is the index of the latest node at depth d.
	stack := make([]int, 0, 16)
	for i, n := range nodes {
		d := max(n.Depth, 0)
		if d > len(stack) {
			// A gap in the depths (a malformed sidecar): treat the node as a
			// child of the latest node seen.
			d = len(stack)
		}
		stack = append(stack[:d], i)
		parent[i] = -1
		if d > 0 {
			parent[i] = stack[d-1]
		}
	}
	return parent
}

// enclosingWindow is the index of the nearest `window` ancestor of node i,
// or -1.
func enclosingWindow(nodes []Node, parent []int, i int) int {
	for p := parent[i]; p >= 0; p = parent[p] {
		if nodes[p].Role == "window" && nodes[p].hasBounds() {
			return p
		}
	}
	return -1
}

// overlappingSiblings yields index pairs of enabled interactive siblings
// whose rects intersect by more than tol in both axes. Siblings share a
// parent in the sidecar.
func overlappingSiblings(nodes []Node, parent []int, tol float32) iter.Seq2[int, int] {
	return func(yield func(int, int) bool) {
		groups := make(map[int][]int, 16)
		order := make([]int, 0, 16)
		for i, n := range nodes {
			if !IsInteractiveRole(n.Role) || !n.hasBounds() || n.IsDisabled() {
				continue
			}
			p := parent[i]
			if _, ok := groups[p]; !ok {
				order = append(order, p)
			}
			groups[p] = append(groups[p], i)
		}
		for _, p := range order {
			g := groups[p]
			for x := range g {
				for y := x + 1; y < len(g); y++ {
					a, b := nodes[g[x]], nodes[g[y]]
					w := min(a.X+a.W, b.X+b.W) - max(a.X, b.X)
					h := min(a.Y+a.H, b.Y+b.H) - max(a.Y, b.Y)
					// Two nodes with the same rect are one control the tree
					// lists twice (a table cell and the widget in it), not
					// two controls drawn over each other.
					if sameRect(a, b, tol) {
						continue
					}
					if w > tol && h > tol {
						if !yield(g[x], g[y]) {
							return
						}
					}
				}
			}
		}
	}
}

func sameRect(a, b Node, tol float32) bool {
	near := func(x, y float32) bool { return max(x-y, y-x) <= tol }
	return near(a.X, b.X) && near(a.Y, b.Y) && near(a.W, b.W) && near(a.H, b.H)
}

// pt prints a length to a tenth of a point, which is finer than egui lays
// anything out.
func pt(v float32) string {
	return strings.TrimSuffix(strconv.FormatFloat(float64(v), 'f', 1, 32), ".0")
}
