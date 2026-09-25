package leewaywidgets

import (
	"math"
	"slices"
	"strings"
)

// HierarchyNode is one node of a hierarchy built from a leeway batch: a path
// segment, the weight beneath it and its children, heaviest first.
type HierarchyNode struct {
	Name     string
	Path     string
	Weight   float64
	Children []*HierarchyNode
}

// Hierarchy is a leeway batch projected onto a tree (ADR-0257, proposed,
// §SD1): each entity is a leaf whose path is its label split on a separator —
// a natural key like "research/vision/datasets" — and whose weight is the sum
// of its values in the charted section (ChartModel), or 1 when counting.
type Hierarchy struct {
	Root *HierarchyNode
	// Leaves counts the entities placed; Skipped those with no positive
	// weight, which a size-by-value picture cannot draw.
	Leaves  int
	Skipped int
	// Depth is the deepest level reached, the root's children being 1.
	Depth int
}

// BuildHierarchy projects a chart model onto a tree. maxDepth folds anything
// deeper into its ancestor at that depth, so a deep path still counts toward
// the levels drawn; zero means no limit.
func BuildHierarchy(m *ChartModel, sep string, byValue bool, maxDepth int) (h Hierarchy) {
	h.Root = &HierarchyNode{Name: "", Path: ""}
	for e, label := range m.Categories {
		w := 1.0
		if byValue {
			w = 0
			for s := range m.Values {
				if v := m.Values[s][e]; !math.IsNaN(v) {
					w += v
				}
			}
			if !(w > 0) {
				h.Skipped++
				continue
			}
		}
		segs := splitPath(label, sep)
		if maxDepth > 0 && len(segs) > maxDepth {
			segs = segs[:maxDepth]
		}
		node := h.Root
		node.Weight += w
		for i, seg := range segs {
			child := findChild(node, seg)
			if child == nil {
				child = &HierarchyNode{Name: seg, Path: strings.Join(segs[:i+1], sep)}
				node.Children = append(node.Children, child)
			}
			child.Weight += w
			node = child
		}
		h.Depth = max(h.Depth, len(segs))
		h.Leaves++
	}
	sortTree(h.Root)
	return h
}

func splitPath(label string, sep string) (segs []string) {
	for _, s := range strings.Split(label, sep) {
		if s = strings.TrimSpace(s); s != "" {
			segs = append(segs, s)
		}
	}
	if len(segs) == 0 {
		segs = []string{label}
	}
	return segs
}

func findChild(n *HierarchyNode, name string) *HierarchyNode {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// sortTree orders children heaviest first, ties by name, so a drawing does not
// depend on the batch's row order.
func sortTree(n *HierarchyNode) {
	slices.SortStableFunc(n.Children, func(a, b *HierarchyNode) int {
		switch {
		case a.Weight > b.Weight:
			return -1
		case a.Weight < b.Weight:
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	for _, c := range n.Children {
		sortTree(c)
	}
}

// Flow is one link of a hierarchy drawn as flows: parent to child, carrying
// the child's weight.
type Flow struct {
	From, To string
	Weight   float64
}

// Flows lists the hierarchy's parent-to-child links below the root, level by
// level — the input a sankey of the hierarchy is drawn from.
func (inst Hierarchy) Flows() (fs []Flow) {
	level := inst.Root.Children
	for len(level) > 0 {
		var next []*HierarchyNode
		for _, n := range level {
			for _, c := range n.Children {
				fs = append(fs, Flow{From: n.Path, To: c.Path, Weight: c.Weight})
				next = append(next, c)
			}
		}
		level = next
	}
	return fs
}
