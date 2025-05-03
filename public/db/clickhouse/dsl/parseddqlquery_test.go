package dsl

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParsedDqlQuery_Smoke(t *testing.T) {
	dql := NewParsedDqlQuery()

	dql.Reset()
	err := dql.ParseFromString("SELECT 1")
	require.Equal(t, "SELECT1<EOF>", dql.GetInputParseTree().GetText())
	require.NoError(t, err)

	dql.Reset()
	dql.SetRecoverFromParseErrors(false)
	err = dql.ParseFromString("SELECT 1 2")
	require.Error(t, err)

	dql.Reset()
	dql.SetRecoverFromParseErrors(true)
	err = dql.ParseFromString("SELECT 1 2")
	require.Equal(t, "SELECT12<EOF>", dql.GetInputParseTree().GetText())
	require.NoError(t, err)
}
