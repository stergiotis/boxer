package analysis_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
)

// securityOf classifies sql and returns its class and its witnesses' names,
// folded and sorted.
func securityOf(t *testing.T, sql string) (class analysis.QuerySecurityClassE, names []string) {
	t.Helper()
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err, sql)
	class, witnesses, err := analysis.ClassifyQuerySecurity(pr)
	require.NoError(t, err, sql)
	for _, w := range witnesses {
		names = append(names, strings.ToLower(w.Name))
	}
	slices.Sort(names)
	return
}

// ADR-0132 §SD5: the class is a property of the buffer, not of its
// spelling. Callers classify different forms — sqlapplet and Diagnostics the
// authored buffer, the agent limits the canonical residual — so a buffer and
// its canonical form must classify alike, with the same witnesses.
func TestSecurityClassSurvivesCanonicalisation(t *testing.T) {
	canonical := passes.CanonicalizeFull(16)
	for _, sql := range []string{
		`SELECT a FROM t WHERE b = 1`,
		`SELECT * FROM values('a String, b String', ('x', 'y'), ('z', 'w'))`,
		`SELECT * FROM values('a Array(UInt8)', [1, 2])`,
		`SELECT * FROM values('a Tuple(UInt8, String)', ((1, 'x')))`,
		`SELECT * FROM numbers(if(1 = 1, 3, 4))`,
		`SELECT * FROM merge(currentDatabase(), '^t')`,
		`SELECT * FROM url(concat('http://', 'h'), 'CSV')`,
		`SELECT * FROM values('x String', file('/etc/passwd'))`,
		`SELECT * FROM loop(numbers(3))`,
		`SELECT * FROM remote('h', numbers(10))`,
		`SELECT CAST(x AS String), [1, 2], (1, 'a') FROM t`,
		`SELECT x ? 1 : 2, CASE WHEN x = 1 THEN 'a' ELSE 'b' END FROM t`,
		`SELECT aiGenerate(note) FROM t`,
		`SELECT * FROM t WHERE id IN (SELECT id FROM s3('http://h/x', 'CSV'))`,
		`SET param_a = 1; SELECT {a:UInt64} FROM numbers(3)`,
		`SELECT * FROM view(SELECT (1, 'a') AS p FROM url('http://h/x', 'CSV'))`,
	} {
		canon, err := canonical.Run(sql)
		require.NoError(t, err, sql)
		rawClass, rawNames := securityOf(t, sql)
		canonClass, canonNames := securityOf(t, canon)
		assert.Equal(t, rawClass, canonClass, "%s\n→ %s", sql, canon)
		assert.Equal(t, rawNames, canonNames, "%s\n→ %s", sql, canon)
	}
}
