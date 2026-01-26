package aliases

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractAliases(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		want      [][]string
		wantError bool
	}{
		{
			name: "End-to-End",
			sql: `
			SELECT 1 /* a comment */ AS ` + "`a1`" + `,
			 myFunc(2,/* comment */ 3) as "a2", -4
			a3, -5 "a4", -6 ` + "`a5`" + `, myFunc3() FROM (SELECT (2+myFunc2() as a6) AS a0,c0 FROM db.tbl);`,
			want: [][]string{
				{"", "a1"},
				{"myFunc", "a2"},
				{"", "a3"},
				{"", "a4"},
				{"", "a5"},
				{"", "a0"},
				{"", "a6"},
			},
			wantError: false,
		},
		{
			name: "Standard Mix",
			sql:  `SELECT 1 AS a1, myFunc(2) as "a2", a3, -5 "a4" FROM table`,
			want: [][]string{
				{"", "a1"},
				{"myFunc", "a2"},
				{"", "a4"},
			},
			wantError: false,
		},
		{
			name: "Quoted Functions and Aliases",
			sql:  `SELECT "my.Func"(1) AS ` + "`weird`name`" + ``,
			want: [][]string{
				{"my.Func", "weird`name"}, // Function names can technically be quoted identifiers
			},
			wantError: true,
		},
		{
			name: "Escape Sequences in Alias",
			sql:  `SELECT 1 AS "hack\"ed"`,
			want: [][]string{
				{"", `hack"ed`},
			},
			wantError: false,
		},
		{
			name: "Doubled Quotes Escape",
			sql:  `SELECT 1 AS "hack""ed"`,
			want: [][]string{
				{"", `hack"ed`},
			},
			wantError: false,
		},
		{
			name:      "Implicit Alias via Column Identifier",
			sql:       `SELECT col_name FROM tbl`,
			want:      nil,
			wantError: false,
		},
		{
			name: "Complex Nested Function",
			sql:  `SELECT 1 + myFunc() as res`,
			// Root expr is Plus operator, not Function, so FunctionName should be empty
			want: [][]string{
				{"", "res"},
			},
			wantError: false,
		},
		{
			name: "Subquery Extraction",
			sql:  `SELECT (SELECT innerFunc() AS innerAlias) AS outerAlias`,
			// Expect walker to hit inner first or last depending on depth-first traversal
			// We check if both exist in output.
			want: [][]string{
				{"", "outerAlias"},
				{"innerFunc", "innerAlias"},
			},
			wantError: false,
		},
		{
			name: "Comments and Evasion",
			sql:  `SELECT /**/ 1 /**/ AS /**/ a1 -- comment`,
			want: [][]string{
				{"", "a1"},
			},
			wantError: false,
		},
		{
			name:      "Syntax Error Injection",
			sql:       `SELECT 1 AS a1 WHERE ((((`, // Unclosed parenthesis
			want:      nil,
			wantError: true,
		},
		{
			name: "Empty String Alias",
			sql:  `SELECT 1 AS ""`,
			want: [][]string{
				{"", ""},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := dsl.NewParsedDqlQuery()
			p.SetRecoverFromParseErrors(false)
			err := p.ParseFromString(tt.sql)
			if err != nil {
				if !tt.wantError {
					require.NoError(t, err, tt.sql)
				}
			}
			tree := p.GetInputParseTree()
			if tree == nil {
				require.EqualValues(t, tt.want, tree)
			} else {
				var got [][]string
				for f, a := range IterateAllAliases(p.GetInputParseTree()) {
					got = append(got, []string{f, a})
				}
				assert.EqualValues(t, tt.want, got, tt.name)
			}
		})
	}
}
