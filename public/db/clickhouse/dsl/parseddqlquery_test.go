package dsl

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestParsedDqlQuery_Smoke(t *testing.T) {
	dql := NewParsedDqlQuery()
	dql.Reset()
	err := dql.ParseFromString("SELECT 1")
	require.NoError(t, err)
	hl := NewSyntaxHighlighter(func(node antlr.Tree) (before string, after string) {
		before = "|"
		after = "|"
		return
	})
	var s string
	s, err = hl.Highlight(dql.GetInputSql(), dql.GetInputParseTree())
	require.NoError(t, err)
	require.Equal(t, strings.ReplaceAll(s, "|", ""), "SELECT 1")
}
