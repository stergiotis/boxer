package fieldview

// tree.go is the hierarchy half of the viewer (ADR-0176 M3), the port from a
// recursive CollapsingHeader to the native tree widget.
//
// The reshape a fixed-height row forces. A leaf used to be two lines — name
// and kind on one, the value wrapping underneath — and a container was a
// header whose body held its children. A tree row is one line, so a field is
// now one row across two columns: name and kind in the outline, value beside
// it. Long values truncate with the full text on hover where they used to
// wrap. What that costs is a long JSON value no longer readable in place;
// what it buys is that values line up down the pane, that a collapsed subtree
// costs nothing to draw, and that a thousand-field object is virtualised
// rather than built in full every frame.

import (
	"strconv"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
)

// fieldNode is the per-node metadata carried beside the columnar tree.
type fieldNode struct {
	// kind is the short label shown after the name, and value the formatted
	// leaf value. Both are computed during the build, where the Renderer's
	// ShowKind and BytesMax settings are in scope.
	kind  string
	value string
}

// State is the caller-owned view state: which containers are open, and the
// scratch the per-frame rebuild reuses. The zero value is usable and starts
// every container at the Renderer's [Renderer.DefaultOpen].
//
// It is separate from the Renderer because the Renderer is a value whose
// fluent setters return copies — configuration that is safe to share, where
// view state is not. One State belongs to one place a field list is shown.
//
// Expansion is the tree widget's own, filed under the key column below. There
// is no second map here mirroring it: the widget's [tree.State] is the store,
// and this type only feeds it the identity to file under.
type State struct {
	st      tree.State
	labels  []string
	parents []int32
	nodes   []fieldNode
	// keys identify the nodes across rebuilds, as slash-joined index chains
	// ("0/2/1"), one per node. Names are not usable — an Array's children are
	// all named by position and an Object's keys are not guaranteed unique —
	// and plain node indices are not either, since a container closing
	// renumbers everything below it.
	keys []string
}

// build flattens the field forest into the State's scratch, in slice order,
// depth first. Every field becomes a node whether or not it is a container,
// so the row sequence reads the way the recursive renderer's output did.
func (inst Renderer) build(s *State, fields []Field) {
	labels := s.labels[:0]
	parents := s.parents[:0]
	nodes := s.nodes[:0]
	keys := s.keys[:0]

	var walk func(parent int32, path string, fs []Field)
	walk = func(parent int32, path string, fs []Field) {
		for i := range fs {
			f := &fs[i]
			p := path + strconv.Itoa(i)
			n := int32(len(labels))
			labels = append(labels, f.Name)
			parents = append(parents, parent)
			keys = append(keys, p)
			var node fieldNode
			if inst.showKind {
				node.kind = kindName(f.Kind)
			}
			if f.IsContainer() {
				// A container's own "value" is how many children it holds,
				// which is what the CollapsingHeader title carried and what
				// makes a collapsed row still worth reading.
				node.value = strconv.Itoa(len(f.Children)) + " items"
			} else {
				node.value = formatField(*f, inst.bytesMax)
			}
			nodes = append(nodes, node)
			if f.IsContainer() {
				walk(n, p+"/", f.Children)
			}
		}
	}
	walk(-1, "", fields)

	s.labels, s.parents, s.nodes, s.keys = labels, parents, nodes, keys
}

// tree is the columnar input, borrowed: valid until the next build. The key
// column is what makes the widget's expansion survive one.
func (s *State) tree() tree.Tree {
	return tree.Tree{Labels: s.labels, Parents: s.parents, Keys: s.keys}
}
