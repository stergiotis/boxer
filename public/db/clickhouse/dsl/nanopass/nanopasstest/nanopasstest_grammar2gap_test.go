package nanopasstest_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/nanopasstest"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGrammar2KnownGaps holds one statement per shape Grammar2Gap forgives.
// Each canonicalises, its canonical form is refused by grammar2, and
// Grammar2Gap names the gap. When grammar2 gains the production, the
// statement's canonical form parses and its entry fails here: drop the entry,
// and the shape from Grammar2Gap, so the fuzz targets check it again.
func TestGrammar2KnownGaps(t *testing.T) {
	gaps := []string{
		"SELECT * FROM numbers(1 + 1)",
		"SELECT * FROM numbers(toUInt64(1 + 1))",
		"SELECT * FROM numbers({n:UInt64})",
		"SELECT 0 FROM f((0))",
		"SELECT 1 FROM numbers(1 AS a)",
		"SELECT {id:UInt64}",
		"SET a = 0; SELECT {Id:a}",
	}
	pipeline := passes.CanonicalizeFull(16)
	for _, sql := range gaps {
		t.Run(sql, func(t *testing.T) {
			canonical, err := pipeline.Run(sql)
			require.NoError(t, err)
			_, err = nanopass.ParseCanonical(canonical)
			require.Error(t, err, "grammar2 takes %q now: remove the entry and its shape from Grammar2Gap", canonical)
			gap, ok := nanopasstest.Grammar2Gap(canonical)
			assert.True(t, ok, "%q", canonical)
			assert.NotEmpty(t, gap)
		})
	}
}

// TestGrammar2GapRefusesOtherDefects: a canonical form refused for a reason
// other than a gap, alone or beside one, is not forgiven.
func TestGrammar2GapRefusesOtherDefects(t *testing.T) {
	for _, text := range []string{
		`SELECT "a" ? "b" : "c"`,
		`SELECT "a" ? "b" : "c" FROM "numbers"(1 + 1)`,
		`SELECT "numbers"(1 + 1)`,
		`SELECT "a" FROM "t"`, // grammar2 takes it: not a gap either
	} {
		_, ok := nanopasstest.Grammar2Gap(text)
		assert.False(t, ok, "%q", text)
	}
}
