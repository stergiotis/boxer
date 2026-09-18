package carrierclient

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scene is a window holding a button, a label whose text sits in the value
// slot, and an unnamed container between them — the shape egui emits.
func scene() *TreeSnapshot {
	root := &TreeNode{Id: 1, Role: "window", Name: "play", Children: []uint64{2}}
	box := &TreeNode{Id: 2, Role: "generic_container", Parent: 1, Children: []uint64{3, 4, 5}}
	run := &TreeNode{Id: 3, Role: "button", Name: "Run", Parent: 2, X: 600, Y: 400, W: 80, H: 24, Flags: FlagDisabled}
	rows := &TreeNode{Id: 4, Role: "label", Value: "3 Rows", Parent: 2}
	off := &TreeNode{Id: 5, Role: "check_box", Name: "Wrap", Parent: 2, Flags: FlagHidden}
	// Snapshot order is unspecified on the wire; the view must not depend on it.
	return &TreeSnapshot{Nodes: []*TreeNode{rows, off, run, root, box}, Pass: 42}
}

func ids(v TreeView) (out []uint64) {
	for _, n := range v.Nodes {
		out = append(out, n.GetId())
	}
	return out
}

func TestSelectNodesWalksDepthFirstAndSkipsContainers(t *testing.T) {
	v := SelectNodes(scene(), TreeFilter{})
	assert.Equal(t, []uint64{1, 3, 4}, ids(v))
	// The unnamed container is not printed, so it does not indent its children.
	assert.Equal(t, []int{0, 1, 1}, v.Depth)
	assert.Equal(t, uint64(42), v.Pass)
}

func TestSelectNodesTextMatchesNameOrValueIgnoringCase(t *testing.T) {
	assert.Equal(t, []uint64{4}, ids(SelectNodes(scene(), TreeFilter{Text: "rows"})))
	assert.Equal(t, []uint64{3}, ids(SelectNodes(scene(), TreeFilter{Text: "RUN"})))
	assert.Empty(t, SelectNodes(scene(), TreeFilter{Text: "absent"}).Nodes)
}

func TestSelectNodesRoleIgnoresCaseAndUnderscores(t *testing.T) {
	for _, role := range []string{"check_box", "CheckBox", "checkbox"} {
		v := SelectNodes(scene(), TreeFilter{Role: role, Hidden: true})
		assert.Equal(t, []uint64{5}, ids(v), role)
	}
}

func TestSelectNodesHiddenIsOptIn(t *testing.T) {
	assert.NotContains(t, ids(SelectNodes(scene(), TreeFilter{})), uint64(5))
	assert.Contains(t, ids(SelectNodes(scene(), TreeFilter{Hidden: true})), uint64(5))
}

func TestSelectNodesUnder(t *testing.T) {
	assert.Equal(t, []uint64{3, 4}, ids(SelectNodes(scene(), TreeFilter{Under: 2})))
	assert.Empty(t, SelectNodes(scene(), TreeFilter{Under: 999}).Nodes)
}

func TestSelectNodesLimitKeepsTheTotal(t *testing.T) {
	v := SelectNodes(scene(), TreeFilter{Limit: 1})
	assert.Len(t, v.Nodes, 1)
	assert.Equal(t, 3, v.Total)

	var buf bytes.Buffer
	require.NoError(t, WriteTree(&buf, v))
	assert.Contains(t, buf.String(), "nodes=1 of 3")
}

func TestSelectNodesSurvivesACycle(t *testing.T) {
	a := &TreeNode{Id: 1, Role: "window", Name: "a", Children: []uint64{2}}
	b := &TreeNode{Id: 2, Role: "button", Name: "b", Parent: 1, Children: []uint64{1}}
	assert.Equal(t, []uint64{1, 2}, ids(SelectNodes(&TreeSnapshot{Nodes: []*TreeNode{a, b}}, TreeFilter{})))
}

func TestFormatNodeIsWhatAStepTakes(t *testing.T) {
	s := scene()
	assert.Equal(t, `button "Run" #3 @640,412 [disabled]`, FormatNode(s.Nodes[2]))
	assert.Equal(t, `label ="3 Rows" #4 @0,0`, FormatNode(s.Nodes[0]))

	// An id with the top bit set prints unsigned, the way a step's "id" reads it.
	big := &TreeNode{Id: 1<<63 + 5, Role: "button", Name: "x"}
	assert.Contains(t, FormatNode(big), "#9223372036854775813")
}

func TestFormatNodeClipsLongTextOnARuneBoundary(t *testing.T) {
	n := &TreeNode{Id: 1, Role: "text_input", Value: strings.Repeat("ä", maxTreeText+10)}
	line := FormatNode(n)
	assert.Contains(t, line, strings.Repeat("ä", maxTreeText)+"…")
	assert.NotContains(t, line, strings.Repeat("ä", maxTreeText+1))
}

func TestWriteTreeIndentsByPrintedAncestors(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, WriteTree(&buf, SelectNodes(scene(), TreeFilter{})))
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 4)
	assert.Equal(t, "# tree pass=42 nodes=3", lines[0])
	assert.True(t, strings.HasPrefix(lines[1], `window "play"`))
	assert.True(t, strings.HasPrefix(lines[2], `  button "Run"`))
}

func TestSelectNodesHidesTextRunsUnlessAskedFor(t *testing.T) {
	label := &TreeNode{Id: 1, Role: "label", Value: "idle", Children: []uint64{2}}
	run := &TreeNode{Id: 2, Role: "text_run", Value: "idle", Parent: 1}
	s := &TreeSnapshot{Nodes: []*TreeNode{label, run}}
	assert.Equal(t, []uint64{1}, ids(SelectNodes(s, TreeFilter{})))
	assert.Equal(t, []uint64{2}, ids(SelectNodes(s, TreeFilter{Role: "text_run"})))
}

func TestFormatNodeQuotesAsJSON(t *testing.T) {
	// An icon-font codepoint, a no-break space, a quote and an astral
	// non-printing rune: the quoted form has to parse back as JSON to the
	// same string, because it is pasted into a step.
	name := "bus \ue182\u00a0\"x\" \U000E0001 · ä"
	line := FormatNode(&TreeNode{Id: 1, Role: "button", Name: name})
	quoted := line[len("button "):strings.LastIndex(line, " #")]
	assert.Contains(t, quoted, `\ue182`)
	assert.Contains(t, quoted, "· ä")
	var back string
	require.NoError(t, json.Unmarshal([]byte(quoted), &back))
	assert.Equal(t, name, back)
}
