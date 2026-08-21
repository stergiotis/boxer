package fsbrowser

import (
	"io/fs"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
)

// placeholderKeySuffix marks the stand-in child an unread directory carries so
// it shows a disclosure control before anyone has asked what is inside. It is
// a NUL, which no io/fs path contains, so the key cannot collide with a real
// child's.
const placeholderKeySuffix = "\x00"

// buildOutline assembles the tree under the current directory from the cached
// listings, reading a directory the first time it is found expanded and never
// before. The loop re-binds the tree state after every growth because
// expansion is keyed by path and a node's index is known only once it is
// appended; it terminates because every pass either loads a directory not
// loaded before or stops.
//
// nodes is parallel to the tree: nodes[i] is the entry at node i. A
// placeholder node has Ord -1 and an empty Path.
func (st *State) buildOutline(fsys fs.FS, showHidden bool) (t tree.Tree, nodes []Entry) {
	st.ensure()
	if st.loadedDir == nil {
		st.loadedDir = make(map[string]bool, 16)
	}
	t = tree.Tree{
		Labels:  st.outlineT.Labels[:0],
		Parents: st.outlineT.Parents[:0],
		Keys:    st.outlineT.Keys[:0],
	}
	nodes = st.nodes[:0]
	scratch := make([]Entry, 0, 64)
	add := func(parent int32, dir string) {
		l := st.read(fsys, dir)
		if l.err != nil {
			t.Labels = append(t.Labels, "Cannot read: "+l.err.Error())
			t.Parents = append(t.Parents, parent)
			t.Keys = append(t.Keys, dir+placeholderKeySuffix+"err")
			nodes = append(nodes, Entry{Name: l.err.Error(), Ord: -1, InfoErr: l.err})
			return
		}
		scratch = st.view(l, showHidden, scratch)
		for _, e := range scratch {
			t.Labels = append(t.Labels, e.Name)
			t.Parents = append(t.Parents, parent)
			t.Keys = append(t.Keys, e.Path)
			nodes = append(nodes, e)
		}
	}
	loaded := make(map[int32]bool, 32)
	add(-1, st.Dir())
	for {
		st.tree.Bind(t)
		grew := false
		for i := 0; i < len(nodes); i++ {
			e := nodes[i]
			if !e.IsDir || loaded[int32(i)] {
				continue
			}
			if !st.tree.IsExpanded(int32(i)) {
				continue
			}
			loaded[int32(i)] = true
			grew = true
			add(int32(i), e.Path)
		}
		if !grew {
			break
		}
	}
	// Every directory not read yet gets its stand-in child, so it opens.
	for i := 0; i < len(nodes); i++ {
		e := nodes[i]
		if !e.IsDir || loaded[int32(i)] {
			continue
		}
		t.Labels = append(t.Labels, "…")
		t.Parents = append(t.Parents, int32(i))
		t.Keys = append(t.Keys, e.Path+placeholderKeySuffix)
		nodes = append(nodes, Entry{Name: "…", Ord: -1})
	}
	st.outlineT, st.nodes = t, nodes
	return
}
