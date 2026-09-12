package csr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildUndirectedCollapsesAndSortsRows(t *testing.T) {
	// ids deliberately unsorted and sparse; edge 7-3 declared twice and once
	// reversed; one self-loop declared twice.
	src := []uint64{7, 3, 7, 42, 42, 3}
	dst := []uint64{3, 7, 3, 42, 42, 9}
	w := []float32{1, 2, 4, 8, 16, 32}
	g, err := BuildE(src, dst, w, Options{})
	require.NoError(t, err)
	require.Equal(t, []uint64{3, 7, 9, 42}, g.IDs())
	require.Equal(t, 4, g.NumVertices())
	require.EqualValues(t, 3, g.NumEdges()) // 3-7, 3-9, 42-42
	require.EqualValues(t, 3, g.Collapsed())
	require.EqualValues(t, 2, g.SelfLoops())
	require.False(t, g.IsDirected())
	s3, _ := g.Slot(3)
	s7, _ := g.Slot(7)
	s9, _ := g.Slot(9)
	s42, _ := g.Slot(42)
	require.Equal(t, []int32{s7, s9}, g.Out(s3))
	require.Equal(t, []int32{s3}, g.Out(s7))
	require.Equal(t, []int32{s42}, g.Out(s42))
	require.Equal(t, []float32{7, 32}, g.OutWeights(s3)) // 1+2+4 collapsed
	require.Equal(t, []float32{24}, g.OutWeights(s42))
	require.True(t, g.HasArc(s3, s7))
	require.False(t, g.HasArc(s9, s7))
	_, ok := g.Slot(5)
	require.False(t, ok)
}

func TestBuildDirectedKeepsDirection(t *testing.T) {
	g, err := BuildE([]uint64{1, 2, 2}, []uint64{2, 3, 3}, nil, Options{Directed: true})
	require.NoError(t, err)
	require.True(t, g.IsDirected())
	require.EqualValues(t, 2, g.NumEdges())
	require.EqualValues(t, 1, g.Collapsed())
	s1, _ := g.Slot(1)
	s2, _ := g.Slot(2)
	s3, _ := g.Slot(3)
	require.Equal(t, []int32{s2}, g.Out(s1))
	require.Empty(t, g.In(s1))
	require.Equal(t, []int32{s1}, g.In(s2))
	require.Equal(t, []int32{s2}, g.In(s3))
	require.Empty(t, g.Out(s3))
	require.Nil(t, g.OutWeights(s1))
}

func TestFingerprintDependsOnTopologyOnly(t *testing.T) {
	a, _ := BuildE([]uint64{1, 2}, []uint64{2, 3}, []float32{1, 1}, Options{})
	b, _ := BuildE([]uint64{2, 3}, []uint64{1, 2}, []float32{5, 9}, Options{}) // same edges, other order and weights
	c, _ := BuildE([]uint64{1, 2}, []uint64{2, 3}, nil, Options{Directed: true})
	d, _ := BuildE([]uint64{1, 1}, []uint64{2, 3}, nil, Options{}) // a star, not a path
	require.Equal(t, a.Fingerprint(), b.Fingerprint())
	require.NotEqual(t, a.Fingerprint(), c.Fingerprint())
	require.NotEqual(t, a.Fingerprint(), d.Fingerprint())
}

func TestBuildRejectsMismatchedColumns(t *testing.T) {
	_, err := BuildE([]uint64{1}, []uint64{1, 2}, nil, Options{})
	require.Error(t, err)
	_, err = BuildE([]uint64{1}, []uint64{2}, []float32{1, 2}, Options{})
	require.Error(t, err)
}

func TestEmptyGraph(t *testing.T) {
	g, err := BuildE(nil, nil, nil, Options{})
	require.NoError(t, err)
	require.Equal(t, 0, g.NumVertices())
	require.EqualValues(t, 0, g.NumEdges())
}
