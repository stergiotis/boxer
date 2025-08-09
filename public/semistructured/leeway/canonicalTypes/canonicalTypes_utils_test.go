package canonicalTypes

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIterateGroupIndexedByOccurrence(t *testing.T) {
	p := NewParser()
	ast, err := p.ParsePrimitiveTypeOrGroupAst("u32-u64-u32-i64")
	require.NoError(t, err)
	s := &strings.Builder{}
	for i, n := range IterateGroupIndexedByOccurrence(ast, -1) {
		_, err = fmt.Fprintf(s, "%s(%d) ", n.String(), i)
		require.NoError(t, err)
	}
	require.Equal(t, "u32(0) u64(-1) u32(1) i64(-1) ", s.String())
}
func TestMergeGroup(t *testing.T) {
	p := NewParser()
	ast1, err := p.ParsePrimitiveTypeOrGroupAst("u32")
	require.NoError(t, err)
	ast2, err := p.ParsePrimitiveTypeOrGroupAst("u64-u32")
	require.NoError(t, err)
	ast3, err := p.ParsePrimitiveTypeOrGroupAst("i64")
	require.NoError(t, err)
	require.EqualValues(t, "u32-u64-u32-i64", MergeGroup(ast1, MergeGroup(ast2, ast3)).String())
}
