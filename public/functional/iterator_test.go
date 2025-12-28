package functional

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMakeIterator2FromAlternatedValue(t *testing.T) {
	ks, vs := AppendSeqIter2(nil, nil, MakeIter2FromAlternatedValue("a", "1", "b", "2"))
	require.EqualValues(t, []string{"a", "b"}, ks)
	require.EqualValues(t, []string{"1", "2"}, vs)
	ks, vs = AppendSeqIter2(nil, nil, MakeIter2FromAlternatedValue("a", "1", "b", "2", "c"))
	require.EqualValues(t, []string{"a", "b"}, ks)
	require.EqualValues(t, []string{"1", "2"}, vs)
	ks, vs = AppendSeqIter2(nil, nil, MakeIter2FromAlternatedValue("a", "1"))
	require.EqualValues(t, []string{"a"}, ks)
	require.EqualValues(t, []string{"1"}, vs)
	ks, vs = AppendSeqIter2(nil, nil, MakeIter2FromAlternatedValue("a"))
	require.EqualValues(t, []string(nil), ks)
	require.EqualValues(t, []string(nil), vs)
}
