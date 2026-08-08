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
	// path identifies the node across frames as a slash-joined index chain
	// ("0/2/1"). Names are not usable as a key — an Array's children are all
	// named by position and an Object's keys are not guaranteed unique — and
	// node indices are not either, since a collapse renumbers everything
	// below it.
	path string
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
type State struct {
	// open holds only the containers whose state differs from the Renderer's
	// DefaultOpen, so the zero value needs no construction and switching
	// DefaultOpen still moves everything the reader has not touched.
	open map[string]bool

	st      tree.State
	labels  []string
	parents []int32
	nodes   []fieldNode
}

// isOpen reports whether the container at path is expanded, given the
// Renderer's default.
func (s *State) isOpen(path string, deflt bool) bool {
	if v, ok := s.open[path]; ok {
		return v
	}
	return deflt
}

// setOpen records a container's state, dropping the entry when it agrees with
// the default so the map stays bounded by what the reader actually changed.
func (s *State) setOpen(path string, open, deflt bool) {
	if open == deflt {
		delete(s.open, path)
		return
	}
	if s.open == nil {
		s.open = make(map[string]bool, 8)
	}
	s.open[path] = open
}

// build flattens the field forest into the State's scratch, in slice order,
// depth first. Every field becomes a node whether or not it is a container,
// so the row sequence reads the way the recursive renderer's output did.
func (inst Renderer) build(s *State, fields []Field) {
	labels := s.labels[:0]
	parents := s.parents[:0]
	nodes := s.nodes[:0]

	var walk func(parent int32, path string, fs []Field)
	walk = func(parent int32, path string, fs []Field) {
		for i := range fs {
			f := &fs[i]
			p := path + strconv.Itoa(i)
			n := int32(len(labels))
			labels = append(labels, f.Name)
			parents = append(parents, parent)
			node := fieldNode{path: p}
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

	s.labels, s.parents, s.nodes = labels, parents, nodes
}

// syncState projects the caller's own expansion onto the widget's, which keys
// on node indices — indices a collapse or a changed field list renumbers,
// where the index path does not.
func (inst Renderer) syncState(s *State) {
	for i := range s.nodes {
		s.st.SetExpanded(int32(i), s.isOpen(s.nodes[i].path, inst.defaultOpen))
	}
}

// applyResult writes a frame's toggle back onto the caller's state. There is
// nothing to do for a click: the viewer has no selection of its own, and the
// widget's is overwritten by the next syncState before it is ever drawn.
func (inst Renderer) applyResult(s *State, res tree.Result) {
	n := res.Toggled
	if n < 0 || int(n) >= len(s.nodes) {
		return
	}
	s.setOpen(s.nodes[n].path, s.st.IsExpanded(n), inst.defaultOpen)
}
