package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormater(t *testing.T) {
	t.Skip("still necessary?")
	opts := FormaterOptions{
		BinaryPath:      "clickhouse format",
		Hilite:          false,
		KeepComments:    true,
		MaxLineLength:   0,
		AllowMultiQuery: true,
		Seed:            "",
		MaxParserDepth:  0,
		MaxQuerySize:    0,
		Obfuscate:       false,
		Oneline:         false,
		Timeout:         0,
	}
	formatter, err := NewFormater(opts)
	require.NoError(t, err)
	var sqlOut string
	sqlOut, err = formatter.FormatFromString(`SELECT a,b,c FROM /* comment */ tbl1;
select 1,2,3 fRom "tbl2" as uu;`)
	const should = "SELECT\n    a,\n    b,\n    c\nFROM tbl1\n;\n\nSELECT\n    1,\n    2,\n    3\nFROM tbl2 AS uu\n;\n\n"
	require.Equal(t, should, sqlOut)

	sqlOut, err = formatter.FormatFromString("SELECT;")
	require.Error(t, err)
	require.Equal(t, "", sqlOut)
}
func TestFormaterColors(t *testing.T) {
	t.Skip("still necessary?")
	opts := FormaterOptions{
		BinaryPath:      "clickhouse format",
		Hilite:          true,
		KeepComments:    true,
		MaxLineLength:   0,
		AllowMultiQuery: true,
		Seed:            "",
		MaxParserDepth:  0,
		MaxQuerySize:    0,
		Obfuscate:       false,
		Oneline:         false,
		Timeout:         0,
	}
	formatter, err := NewFormater(opts)
	require.NoError(t, err)
	var sqlOut string
	sqlOut, err = formatter.FormatFromString(`SELECT a,a+b,avg(c) AS "u-1", {p:UInt8} FROM /* comment */ tbl1;`)

	require.Equal(t, `〈keyword SELECT〉
    〈identifier a〉,
    〈identifier a〉〈operator  + 〉〈identifier b〉,
    〈function avg(〉〈identifier c〉〈function )〉〈keyword  AS 〈alias `+"`u-1`"+`〉,
    〈substitution {〈identifier p〈substitution :〈identifier UInt8〈substitution }〉〈keyword 
FROM〉 〈identifier tbl1〉
;

`, debugMarkupHiliteOutput(sqlOut))

	sqlOut, err = formatter.FormatFromString("SELECT 'abc', '" + hiliteFunction + "abc'")
	require.NoError(t, err)
	require.Equal(t, "\x1b[1mSELECT\x1b[0m\n    'abc',\n    '\x1b[0;33mabc'\n;\n\n", sqlOut)
}
