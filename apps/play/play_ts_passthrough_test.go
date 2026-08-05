package play

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// play_ts_passthrough_test.go is ADR-0163's pass-through pin: the rewrite
// pipeline play runs before executing anything must leave a `ts*` call
// recognisable.
//
// It is worth pinning because the coupling is invisible from either side.
// CanonicalizeFull exists to normalise SQL for a SERVER, and knows nothing
// about a vocabulary play executes itself; recognition reads a parse and
// cannot see that a pass has been at it. A pass that quoted, case-folded or
// re-associated a call would break the feature with no test failing anywhere
// near either change — this is that test.

// The pipeline as play registers it (play_passes.go), applied here directly
// rather than through the registry so the pin does not depend on global
// registration order.
func tsCanonicalize(t *testing.T, sql string) string {
	t.Helper()
	out, err := passes.CanonicalizeFull(100).Run(sql)
	require.NoError(t, err)
	return out
}

func TestCanonicalizeLeavesClientCallsRecognisable(t *testing.T) {
	for _, tc := range []struct {
		name, sql string
		wantArgs  []string
	}{
		{
			name:     "literal argument",
			sql:      "WITH b AS (SELECT 1 AS t, 1 AS v), c AS (SELECT tsProfile(t, v, 64) FROM b) SELECT 1",
			wantArgs: []string{"t", "v", "64"},
		},
		{
			name:     "param slot argument",
			sql:      "WITH b AS (SELECT 1 AS t, 1 AS v), c AS (SELECT tsAnomalyScores(t, v, {win:UInt32}) FROM b) SELECT 1",
			wantArgs: []string{"t", "v", "{win:UInt32}"},
		},
		{
			name:     "four arguments",
			sql:      "WITH b AS (SELECT 1 AS t, 1 AS v), c AS (SELECT tsAnomalySpans(t, v, 64, 3) FROM b) SELECT 1",
			wantArgs: []string{"t", "v", "64", "3"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			canonical := tsCanonicalize(t, tc.sql)

			res, err := splitGraph(canonical)
			require.NoError(t, err, "the canonical form must still split")
			node, ok := findSplitNode(res, "c")
			require.True(t, ok)
			require.NotNil(t, node.Client, "canonicalisation must not hide the call:\n%s", canonical)
			assert.Equal(t, tc.wantArgs, node.Client.Args,
				"arguments survive canonicalisation:\n%s", canonical)
			assert.Equal(t, NodeID("b"), node.Client.Input)
		})
	}
}

// The name survives EXACTLY. CanonicalizeIdentifiers quotes identifiers and
// CanonicalizeKeywordCase upper-cases keywords; neither may touch the call's
// name, because the registry match is case-exact and a folded name would stop
// resolving.
func TestCanonicalizePreservesClientCallNameCase(t *testing.T) {
	for _, spec := range tsFuncs {
		if !spec.Shipped {
			continue
		}
		sql := "WITH b AS (SELECT 1 AS t, 1 AS v), c AS (SELECT " + tsSignature(spec) + " FROM b) SELECT 1"
		// tsSignature spells the declared parameter names, which parse as
		// plain identifiers — good enough to carry the name through.
		canonical := tsCanonicalize(t, sql)
		assert.True(t, strings.Contains(canonical, spec.Name) ||
			strings.Contains(canonical, `"`+spec.Name+`"`),
			"%s must survive canonicalisation verbatim (quoting is fine, folding is not):\n%s",
			spec.Name, canonical)
	}
}

// The terminal-leaf check must survive it too: canonicalisation rewrites the
// sink, and the error it raises is the one that names the fix.
func TestCanonicalizeKeepsTerminalLeafEnforcement(t *testing.T) {
	canonical := tsCanonicalize(t,
		"WITH b AS (SELECT 1 AS t, 1 AS v), c AS (SELECT tsProfile(t, v, 8) FROM b) SELECT * FROM c")
	_, err := splitGraph(canonical)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "computed client-side")
}
