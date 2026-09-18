package carrierclient

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// treeview.go renders a tree snapshot for a reader that pays per character —
// an agent deciding what to anchor its next step on. [Resolve] answers "which
// one node"; this answers "what is there", which is a different question: a
// filter that matches nothing, or forty nodes, is a result rather than an
// error.
//
// The output is lines, not JSON. Every field a follow-up step needs is on the
// line in the spelling the trace takes it in — the id unsigned, the role
// lower-snake — so nothing has to be converted between reading and acting.

// TreeFilter selects the nodes of a snapshot worth printing. The zero value
// selects every visible node that carries a name or a value.
type TreeFilter struct {
	// Under restricts the view to one node and its descendants.
	Under uint64
	// Text matches a substring of the name *or* the value, ignoring case. One
	// field rather than two because the reader rarely knows which slot a
	// widget put its text in: egui names a button and leaves a label's name
	// empty. [Locator] keeps the two apart for the opposite reason — there, a
	// match that widened silently could make an existing anchor ambiguous.
	Text string
	// Role matches the AccessKit role, ignoring case and underscores, so
	// "CheckBox", "checkbox" and "check_box" all select check boxes.
	Role string
	// Hidden includes nodes that are laid out but not on screen.
	Hidden bool
	// Limit caps the nodes returned; 0 means [DefaultTreeLimit].
	Limit int
}

const (
	// DefaultTreeLimit bounds an unfiltered view. A busy scene holds several
	// hundred named nodes, and a reader that wanted all of them can say so.
	DefaultTreeLimit = 200
	// maxTreeText is where a printed name or value is cut. A text edit's value
	// is its whole buffer; the view is for finding the widget, not reading it.
	maxTreeText = 160
)

// TreeView is the outcome of [SelectNodes].
type TreeView struct {
	// Nodes are the matches in depth-first order, so siblings stay together
	// and a window's content follows the window.
	Nodes []*TreeNode
	// Depth is each node's indentation: the number of *printed* ancestors it
	// has, not its depth in the snapshot, which is mostly unnamed containers.
	Depth []int
	// Total is how many nodes matched before Limit cut the list.
	Total int
	// Pass is the host's frame counter for the snapshot.
	Pass uint64
}

func normaliseRole(role string) string {
	return strings.ToLower(strings.ReplaceAll(role, "_", ""))
}

func (inst TreeFilter) matches(n *TreeNode) bool {
	if !inst.Hidden && n.GetFlags()&FlagHidden != 0 {
		return false
	}
	// Unnamed, valueless nodes are layout containers; a locator cannot
	// address them and listing them buries the ones it can.
	if n.GetName() == "" && n.GetValue() == "" {
		return false
	}
	role := normaliseRole(n.GetRole())
	want := normaliseRole(inst.Role)
	// egui hangs a `text_run` under most labels, carrying the label's own text
	// again. It is no use as an anchor — see [Locator] on the ambiguity it
	// causes — and listing it nearly doubles the view, so it shows only to a
	// reader who asks for the role by name.
	if role == "textrun" && want != "textrun" {
		return false
	}
	if want != "" && role != want {
		return false
	}
	if inst.Text != "" {
		needle := strings.ToLower(inst.Text)
		if !strings.Contains(strings.ToLower(n.GetName()), needle) &&
			!strings.Contains(strings.ToLower(n.GetValue()), needle) {
			return false
		}
	}
	return true
}

// SelectNodes walks a snapshot depth-first and returns the nodes the filter
// keeps. An Under id that is not in the snapshot yields an empty view.
func SelectNodes(snap *TreeSnapshot, f TreeFilter) (view TreeView) {
	view.Pass = snap.GetPass()
	limit := f.Limit
	if limit <= 0 {
		limit = DefaultTreeLimit
	}
	byID := make(map[uint64]*TreeNode, len(snap.GetNodes()))
	for _, n := range snap.GetNodes() {
		byID[n.GetId()] = n
	}
	var roots []*TreeNode
	if f.Under != 0 {
		if n := byID[f.Under]; n != nil {
			roots = append(roots, n)
		}
	} else {
		for _, n := range snap.GetNodes() {
			if n.GetParent() == 0 || byID[n.GetParent()] == nil {
				roots = append(roots, n)
			}
		}
	}
	// seen guards the walk against a cycle or a node listed under two parents:
	// the snapshot is wire data, and a view that never returns is worse than
	// one that prints a node once.
	seen := make(map[uint64]struct{}, len(byID))
	var walk func(n *TreeNode, depth int)
	walk = func(n *TreeNode, depth int) {
		if _, dup := seen[n.GetId()]; dup {
			return
		}
		seen[n.GetId()] = struct{}{}
		if f.matches(n) {
			view.Total++
			if len(view.Nodes) < limit {
				view.Nodes = append(view.Nodes, n)
				view.Depth = append(view.Depth, depth)
			}
			depth++
		}
		for _, id := range n.GetChildren() {
			if c := byID[id]; c != nil {
				walk(c, depth)
			}
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	return view
}

// quoteTreeText quotes a name or value as a JSON string, so it can be pasted
// into a step as it stands. Runes that do not print — the private-use
// codepoints an icon font is drawn from, a no-break space — are escaped rather
// than emitted raw, which keeps them visible and keeps them JSON.
func quoteTreeText(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range clipTreeText(s) {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case unicode.IsPrint(r):
			b.WriteRune(r)
		case r > 0xFFFF:
			hi, lo := utf16.EncodeRune(r)
			b.WriteString(fmt.Sprintf(`\u%04x\u%04x`, hi, lo))
		default:
			b.WriteString(fmt.Sprintf(`\u%04x`, r))
		}
	}
	b.WriteByte('"')
	return b.String()
}

func clipTreeText(s string) string {
	runes := 0
	for i := range s {
		if runes == maxTreeText {
			return s[:i] + "…"
		}
		runes++
	}
	return s
}

// FormatNode renders one node as a line.
//
//	button "Run" #1234 @640,412 [disabled]
//	label ="3 rows" #5678 @700,440
//
// The name is quoted bare and the value after `=`, mirroring how a step tells
// `name` from `value`. `#` is the id as a trace takes it and `@` the bounds
// centre in logical points, which is where a `pointer` click or a coordinate
// step would land.
func FormatNode(n *TreeNode) string {
	var b strings.Builder
	b.WriteString(n.GetRole())
	if n.GetName() != "" {
		b.WriteString(" " + quoteTreeText(n.GetName()))
	}
	if n.GetValue() != "" {
		b.WriteString(" =" + quoteTreeText(n.GetValue()))
	}
	b.WriteString(" #" + strconv.FormatUint(n.GetId(), 10))
	x, y := Center(n)
	b.WriteString(" @" + strconv.Itoa(int(x+0.5)) + "," + strconv.Itoa(int(y+0.5)))
	var flags []string
	for _, f := range []struct {
		bit  uint32
		name string
	}{{FlagDisabled, "disabled"}, {FlagHidden, "hidden"}, {FlagFocused, "focused"}, {FlagSelected, "selected"}} {
		if n.GetFlags()&f.bit != 0 {
			flags = append(flags, f.name)
		}
	}
	if len(flags) > 0 {
		b.WriteString(" [" + strings.Join(flags, ",") + "]")
	}
	return b.String()
}

// WriteTree prints a view: a header naming the pass and the counts, then one
// indented line per node. A view cut by its limit says so, because a reader
// that takes a truncated list for the whole scene anchors on the wrong thing.
func WriteTree(w io.Writer, view TreeView) (err error) {
	var b strings.Builder
	b.WriteString("# tree pass=" + strconv.FormatUint(view.Pass, 10) +
		" nodes=" + strconv.Itoa(len(view.Nodes)))
	if view.Total > len(view.Nodes) {
		b.WriteString(" of " + strconv.Itoa(view.Total) + " — narrow the filter or raise the limit")
	}
	b.WriteString("\n")
	for i, n := range view.Nodes {
		b.WriteString(strings.Repeat("  ", view.Depth[i]))
		b.WriteString(FormatNode(n))
		b.WriteString("\n")
	}
	_, err = io.WriteString(w, b.String())
	return err
}

// nodeRecord is one line of [WriteTreeJSONL]. Field names are the ones a step
// uses where the two overlap.
type nodeRecord struct {
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

// WriteTreeJSONL prints a view as one JSON object per node, for a program
// rather than a reader: nothing is clipped or rounded, and there is no header
// line to skip. The id is a JSON number, as a step takes it, and exceeds 2^53
// — decode it into a uint64, not a float.
func WriteTreeJSONL(w io.Writer, view TreeView) (err error) {
	for i, n := range view.Nodes {
		cx, cy := Center(n)
		var b []byte
		b, err = json.Marshal(nodeRecord{
			ID: n.GetId(), Role: n.GetRole(), Name: n.GetName(), Value: n.GetValue(),
			CX: cx, CY: cy, X: n.GetX(), Y: n.GetY(), W: n.GetW(), H: n.GetH(),
			Flags: n.GetFlags(), Depth: view.Depth[i],
		})
		if err != nil {
			return eh.Errorf("unable to encode a tree node: %w", err)
		}
		if _, err = w.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	return nil
}
