package passes_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammarenv"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A SET prelude on its own line is split off before the body passes run, so
// its setting names are spelled through the environment: bare, and quoted
// when the name lexes as a keyword, as in the body. The parameter lines and
// every value stay as written, so the environment a later Extract reads from
// the canonical form binds the same parameters to the same text.
func TestCanonicalizeFull_SetPrelude(t *testing.T) {
	pipeline := passes.CanonicalizeFull(16)
	for _, tc := range []struct{ sql, want string }{
		{"SET In= '';\nSELECT{A:A}", "SET \"In\" = '';\nSELECT{A:A}"},
		{"SET limit = 1;\nSELECT 1", "SET \"limit\" = 1;\nSELECT 1"},
		{"SET true = 1;\nSELECT 1", "SET \"true\" = 1;\nSELECT 1"},
		{"SET max_threads = 1;\nSELECT 1", "SET max_threads = 1;\nSELECT 1"},
		{"SET `limit` = 1;\nSELECT 1", "SET `limit` = 1;\nSELECT 1"},
		{"SET \"limit\" = 1;\nSELECT 1", "SET \"limit\" = 1;\nSELECT 1"},
		{
			"set param_ids = [1,2];\nSET final = 1;\nSELECT a FROM t WHERE has({ids:Array(UInt8)}, a)",
			"SET \"final\" = 1;\nSET param_ids = [1,2];\nSELECT \"a\" FROM \"t\" WHERE \"has\"({ids:Array(UInt8)}, \"a\")",
		},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			out, err := pipeline.Run(tc.sql)
			require.NoError(t, err)
			assert.Equal(t, tc.want, out)
			_, err = nanopass.ParseCanonical(out)
			require.NoError(t, err, "the canonical form parses under grammar2: %s", out)
			again, err := pipeline.Run(out)
			require.NoError(t, err)
			assert.Equal(t, out, again)

			before, _, err := grammarenv.Extract(tc.sql)
			require.NoError(t, err)
			after, _, err := grammarenv.Extract(out)
			require.NoError(t, err)
			assert.Equal(t, before.Params, after.Params)
		})
	}
}

// In the body a setting named true or false is a name too, quoted like a
// keyword; grammar2 takes a setting name only as an identifier.
func TestCanonicalizeFull_BooleanSettingName(t *testing.T) {
	pipeline := passes.CanonicalizeFull(16)
	for _, tc := range []struct{ sql, want string }{
		{"SET true = 1; SELECT 1", `SET "true" = 1; SELECT 1`},
		{"SELECT 1 SETTINGS false = 0", `SELECT 1 SETTINGS "false" = 0`},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			out, err := pipeline.Run(tc.sql)
			require.NoError(t, err)
			assert.Equal(t, tc.want, out)
			_, err = nanopass.ParseCanonical(out)
			require.NoError(t, err)
		})
	}
}

// A prelude name that binds a `{name:Type}` slot without the param_ prefix is
// a parameter, matched to the slot by its spelling; it is not quoted.
func TestCanonicalizeIdentifiers_SlotBindingNameStays(t *testing.T) {
	out, err := passes.CanonicalizeIdentifiers.Run("SET x = 1;\nSELECT {x:UInt8}")
	require.NoError(t, err)
	assert.Equal(t, "SET x = 1;\nSELECT {x:UInt8}", out)
}
