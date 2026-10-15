//go:build !(js || wasip1)

package goccyengine

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
)

func laidOutDot(t *testing.T, m layeredgraph.GraphModel) string {
	t.Helper()
	e, err := New(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })
	dot, err := e.renderLaidOutDot(context.Background(), m, layeredgraph.LayoutOpts{})
	require.NoError(t, err)
	return string(dot)
}

// Edge notation reaches Graphviz only on the edges that use it, so a graph
// that uses none of it is emitted exactly as before.
func TestNotationIsEmittedOnlyWhenUsed(t *testing.T) {
	nodes := []layeredgraph.Node{{ID: "a"}, {ID: "b"}}

	plain := laidOutDot(t, layeredgraph.GraphModel{Nodes: nodes,
		Edges: []layeredgraph.Edge{{From: "a", To: "b", Label: "x"}, {From: "b", To: "a"}}})
	for _, attr := range []string{"dir", "arrowhead", "style", "headlabel", "taillabel"} {
		// Whole attribute names: rankdir must not count as dir.
		re := regexp.MustCompile(`(^|[\s\[,])` + attr + `=`)
		assert.False(t, re.MatchString(plain), "a plain graph carries no %s attribute", attr)
	}

	marked := laidOutDot(t, layeredgraph.GraphModel{Nodes: nodes, Edges: []layeredgraph.Edge{
		{From: "a", To: "b", Head: layeredgraph.EdgeHeadNone},
		{From: "b", To: "a", Head: layeredgraph.EdgeHeadOpen, Dashed: true, HeadLabel: "h", TailLabel: "t"},
	}})
	assert.True(t, strings.Contains(marked, "dir=none"), "undirected edge emits dir=none")
	assert.True(t, strings.Contains(marked, "arrowhead=empty"), "open head emits arrowhead=empty")
	assert.True(t, strings.Contains(marked, "style=dashed"), "dashed edge emits style=dashed")
	assert.True(t, strings.Contains(marked, "headlabel=h"), "head label is emitted")
	assert.True(t, strings.Contains(marked, "taillabel=t"), "tail label is emitted")
}

// The record label stacks its fields under both rank axes — braces for
// top-bottom ranks, a flat list for left-right ones — and escapes every
// character the record parser would otherwise read.
func TestRecordLabel(t *testing.T) {
	n := layeredgraph.Node{ID: "n", Subtitle: "s t", Rows: []layeredgraph.Row{{ID: "x", Text: `a|b {c} <d> \e`}, {ID: "y", Text: "z"}}}
	for dir, want := range map[layeredgraph.RankDir]string{
		layeredgraph.RankDirTopBottom: `{Title\ \ \ s\ t|<r0> a\|b\ \{c\}\ \<d\>\ \\e|<r1> z}`,
		layeredgraph.RankDirBottomTop: `{Title\ \ \ s\ t|<r0> a\|b\ \{c\}\ \<d\>\ \\e|<r1> z}`,
		layeredgraph.RankDirLeftRight: `Title\ \ \ s\ t|<r0> a\|b\ \{c\}\ \<d\>\ \\e|<r1> z`,
		layeredgraph.RankDirRightLeft: `Title\ \ \ s\ t|<r0> a\|b\ \{c\}\ \<d\>\ \\e|<r1> z`,
	} {
		got, err := recordLabel(n, "Title ", dir)
		require.NoError(t, err)
		assert.Equal(t, want, got, "rank dir %d", dir)
	}
}

// A graph without row nodes emits no record.
func TestRecordIsEmittedOnlyForRowNodes(t *testing.T) {
	plain := laidOutDot(t, layeredgraph.GraphModel{Nodes: []layeredgraph.Node{{ID: "a"}, {ID: "b", Shape: layeredgraph.NodeShapeDiamond}}})
	assert.NotContains(t, plain, "record")
	rows := laidOutDot(t, layeredgraph.GraphModel{Nodes: []layeredgraph.Node{{ID: "a", Rows: []layeredgraph.Row{{ID: "r", Text: "r"}}}}})
	assert.Contains(t, rows, "shape=record")
	assert.Contains(t, rows, "rects=")
}
