package leewaywidgets

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixture is one entity whose string sections reference nothing in the
// batch: one node, no linking section, no edges.
func TestGraphSinkOnTheFixture(t *testing.T) {
	s := NewGraphSink()
	RunFixture(s)
	m := s.Model()
	require.Len(t, m.Nodes, 1)
	assert.Equal(t, "acme/widgets/blue", m.Nodes[0])
	assert.Empty(t, m.Section, "no section resolves to an entity")
	assert.Empty(t, m.Edges)
}

// EndBatch picks the section whose values resolve most often, by label or id.
func TestGraphSinkResolvesTheLinkingSection(t *testing.T) {
	s := NewGraphSink()
	s.BeginBatch()
	s.model.Nodes = []string{"a", "b", "c"}
	s.model.Groups = []string{"", "", ""}
	s.ids = []string{"1", "2", "3"}
	s.refs["note"] = []graphRef{{from: 0, value: "hello"}}
	s.refs["link"] = []graphRef{{from: 0, value: "b", kind: "calls"}, {from: 1, value: "3"}, {from: 2, value: "zz"}}
	s.order = []string{"note", "link"}
	s.kinds = [][]sectionKind{{{"link", "calls"}, {"role", "service"}}, nil, {{"role", "db"}}}
	require.NoError(t, s.EndBatch())
	m := s.Model()
	assert.Equal(t, "link", m.Section)
	assert.Equal(t, []GraphEdge{{From: 0, To: 1, Kind: "calls"}, {From: 1, To: 2}}, m.Edges, "by label and by id")
	assert.Equal(t, 1, m.Unresolved)
	assert.Equal(t, []string{"service", "", "db"}, m.Groups, "groups skip the linking section")
}
