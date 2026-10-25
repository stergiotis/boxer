package mdoutline

import (
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdline"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
)

// Node is the per-heading metadata carried beside the columnar tree, one
// entry per node and indexed the same way. It holds the three things the
// tree widget has no way to know: what the row is filed under across
// rebuilds, where in the source it points, and how much it hides when
// closed.
type Node struct {
	// Slug is the anchor a preview scrolls to and the section a position is
	// reported as. Not unique on its own; see Model's keys.
	Slug string
	// Off is the heading's byte offset into the source, where a click sends
	// the caret. It points at the heading text, as [markdown.HeadingInfo]
	// reports it, not at the `#` marker.
	Off int
	// Descendants is how many headings sit under this one, at any depth.
	// Shown on a collapsed row, so closing a section says what it took away.
	Descendants int
}

// Model is the outline's hierarchy: the columnar input [tree.Render] takes,
// plus the metadata above. Retain it and refill it in place; the slices are
// rebuilt on every reparse, so reallocating them would be one allocation per
// edit for nothing.
type Model struct {
	labels  []string
	parents []int32
	nodes   []Node
	// keys identify the nodes across rebuilds, and are what the tree widget
	// files a collapse under. An outline is rebuilt from a fresh parse on
	// every edit that changes the text, so a node index, the only identity a
	// columnar input has on its own, means a different heading one keystroke
	// to the next, and inserting a section would hand its collapse state to
	// whichever heading slid into its slot.
	//
	// The slug is the stable part; the ordinal disambiguates two sections
	// that share a title.
	keys []string
	// seen counts slugs during a build, so the ordinal in a key is stable
	// for a given document. Retained and cleared rather than allocated per
	// build.
	seen map[string]int
}

// Build turns a document's headings into the hierarchy.
//
// Nesting comes from the heading levels and nothing else, using the rule a
// reader already applies: a heading belongs to the nearest heading above it
// with a smaller level. That handles the two shapes a hand-written document
// takes and a strict level-equals-depth reading does not: a level skipped
// (`#` straight to `###`, which nests under the `#`), and a document that
// never uses level 1 at all (its `##`s are roots). A forest is fine; the
// tree widget allows several roots, and inventing a virtual document root
// would state a containment the markdown does not.
//
// Headings with no slug are dropped: there is nothing to scroll to.
func (m *Model) Build(headings []markdown.HeadingInfo) {
	labels := m.labels[:0]
	parents := m.parents[:0]
	nodes := m.nodes[:0]
	keys := m.keys[:0]
	if m.seen == nil {
		m.seen = make(map[string]int, 32)
	}
	clear(m.seen)

	// The open ancestors, innermost last. Bounded by the six heading levels
	// in practice, but nothing enforces that, so it is a slice.
	type ancestor struct {
		level uint8
		node  int32
	}
	stack := make([]ancestor, 0, 8)

	for _, h := range headings {
		if h.Slug == "" {
			continue
		}
		for len(stack) > 0 && stack[len(stack)-1].level >= h.Level {
			stack = stack[:len(stack)-1]
		}
		parent := int32(-1)
		if len(stack) > 0 {
			parent = stack[len(stack)-1].node
		}
		label := h.Text
		if label == "" {
			label = h.Slug
		}
		ord := m.seen[h.Slug]
		m.seen[h.Slug] = ord + 1

		node := int32(len(labels))
		labels = append(labels, label)
		parents = append(parents, parent)
		keys = append(keys, h.Slug+"#"+itoa(ord))
		nodes = append(nodes, Node{Slug: h.Slug, Off: h.ByteOffset})
		stack = append(stack, ancestor{level: h.Level, node: node})
	}

	// Descendant counts, accumulated backwards. A parent always precedes its
	// children here, since headings arrive in document order and the stack
	// only ever points at one already emitted, so one reverse pass is enough.
	for i := len(nodes) - 1; i >= 0; i-- {
		if p := parents[i]; p >= 0 {
			nodes[p].Descendants += nodes[i].Descendants + 1
		}
	}

	m.labels, m.parents, m.nodes, m.keys = labels, parents, nodes, keys
}

// Len is the node count.
func (m *Model) Len() int { return len(m.nodes) }

// Node is the metadata of node i.
func (m *Model) Node(i int32) Node { return m.nodes[i] }

// Label is the heading text node i shows.
func (m *Model) Label(i int32) string { return m.labels[i] }

// Tree is the columnar input, borrowed. Valid until the next Build. The key
// column is what carries a collapse across one.
func (m *Model) Tree() tree.Tree {
	return tree.Tree{Labels: m.labels, Parents: m.parents, Keys: m.keys}
}

// NodeBySlug finds the first node with this slug, or -1. First rather than
// exact because a slug is what a position's section is reported as, and two
// sections can share one, in which case the outline highlights the earlier.
func (m *Model) NodeBySlug(slug string) (node int32) {
	if slug == "" {
		return -1
	}
	for i := range m.nodes {
		if m.nodes[i].Slug == slug {
			return int32(i)
		}
	}
	return -1
}

// Count is how many of the headings the outline would list: those with a
// slug.
func Count(headings []markdown.HeadingInfo) (n int) {
	for _, h := range headings {
		if h.Slug != "" {
			n++
		}
	}
	return
}

// SectionAt resolves a byte offset in src to the slug of the section
// containing it: the last heading starting at or before off. An offset above
// the first heading returns "", the document-level section, which is also
// the value [markdown.Input.ScrollToSection] treats as a no-op, so a document
// with no headings never scrolls.
//
// Heading offsets are normalised to their line start before comparing.
// [markdown.HeadingInfo.ByteOffset] points at the heading text, not at the
// `#` marker, so without the normalisation a caret resting on the marker
// resolves to the previous section, and a preview would jump backwards at
// the moment the reader clicked into a heading.
func SectionAt(src string, headings []markdown.HeadingInfo, off int) (slug string) {
	for _, h := range headings {
		if h.ByteOffset < 0 || h.Slug == "" {
			// A heading with no text lines (`##` alone) carries offset -1
			// and an empty slug. It names no section.
			continue
		}
		if mdline.LineStart(src, h.ByteOffset) > off {
			break // headings arrive in document order
		}
		slug = h.Slug
	}
	return
}

// itoa is strconv.Itoa for small non-negative ordinals, kept local so a key
// allocates nothing beyond its string.
func itoa(n int) (s string) {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 && i > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
