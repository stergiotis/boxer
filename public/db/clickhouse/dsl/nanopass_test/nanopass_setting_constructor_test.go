package nanopass_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
)

// An array written as a setting's value becomes the array() call, and
// `array` lexes as a keyword: the canonical grammar reads the call only
// with its name quoted, as it does in an expression. A tuple's and a
// plain function's name are identifiers and stay bare.
func TestCanonicalSettingConstructorsParseCanonically(t *testing.T) {
	pipeline := passes.CanonicalizeFull(16)
	for in, want := range map[string]string{
		"SELECT 1 SETTINGS x = [1, 2]":      `SELECT 1 SETTINGS x = "array"(1, 2)`,
		"SELECT 1 SETTINGS x = []":          `SELECT 1 SETTINGS x = "array"()`,
		"SELECT 1 SETTINGS x = array(1, 2)": `SELECT 1 SETTINGS x = "array"(1, 2)`,
		"SELECT 1 SETTINGS x = [[1], [2]]":  `SELECT 1 SETTINGS x = "array"("array"(1), "array"(2))`,
		"SELECT 1 SETTINGS x = (1, 'a')":    `SELECT 1 SETTINGS x = tuple(1, 'a')`,
		"SELECT 1 SETTINGS x = map('a', 1)": `SELECT 1 SETTINGS x = map('a', 1)`,
		"SET x = [1,2]; SELECT 1":           `SET x = "array"(1, 2); SELECT 1`,
		"SELECT [1, 2] SETTINGS x = 1":      `SELECT "array"(1, 2) SETTINGS x = 1`,
		"SELECT 1 SETTINGS x = (1, [2, 3])": `SELECT 1 SETTINGS x = tuple(1, "array"(2, 3))`,
	} {
		out, err := pipeline.Run(in)
		require.NoError(t, err, in)
		require.Equal(t, want, out, in)
		_, err = nanopass.ParseCanonical(out)
		require.NoError(t, err, "the canonical grammar reads %q", out)
		again, err := pipeline.Run(out)
		require.NoError(t, err, out)
		require.Equal(t, out, again, "idempotent: %q", in)
	}
}
