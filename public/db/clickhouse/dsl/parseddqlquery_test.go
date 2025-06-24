package dsl

import (
	"testing"

	"github.com/stergiotis/boxer/public/parsing/antlr4utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIterateAll(t *testing.T) {
	_, tree, err := parseSql("SELECT 1 FROM db.tbl;", nil, nil)
	require.NoError(t, err)
	i := 0
	for range antlr4utils.IterateAll(tree) {
		i++
	}
	assert.Equal(t, 26, i)
	i = 0
	for range antlr4utils.IterateAll(tree) {
		if i > 2 {
			break
		}
		i++
	}
	assert.Equal(t, 3, i)
}
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
func TestParsedDqlQueryTdd01(t *testing.T) {
	dql := NewParsedDqlQuery()
	dql.Reset()

	err := dql.ParseFromString("SELECT a,b EXCEPT c FROM tbl")
	require.NoError(t, err)
	require.Equal(t, "SELECTa,bEXCEPTcFROMtbl<EOF>", dql.GetInputParseTree().GetText())

	err = dql.ParseFromString("SELECT a,b EXCEPT c,d FROM tbl")
	require.NoError(t, err)
	require.Equal(t, "SELECTa,bEXCEPTc,dFROMtbl<EOF>", dql.GetInputParseTree().GetText())

	err = dql.ParseFromString("SELECT a,b EXCEPT FROM tbl")
	require.Error(t, err)

	err = dql.ParseFromString("SELECT a,b EXCEPT columns('a.*') FROM tbl")
	require.NoError(t, err)
	require.Equal(t, "SELECTa,bEXCEPTcolumns('a.*')FROMtbl<EOF>", dql.GetInputParseTree().GetText())

	err = dql.ParseFromString("SELECT columns('a.*') FROM tbl")
	require.NoError(t, err)
	require.Equal(t, "SELECTcolumns('a.*')FROMtbl<EOF>", dql.GetInputParseTree().GetText())

	err = dql.ParseFromString("SELECT u,v FROM tbl WHERE a > 1\n  // a comment\n AND b > 1")
	require.NoError(t, err)
	require.Equal(t, "SELECTu,vFROMtblWHEREa>1ANDb>1<EOF>", dql.GetInputParseTree().GetText())
}
