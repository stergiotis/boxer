//go:build llm_generated_opus46

package passes_test

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// extractedFixture runs ExtractLiterals over `sql` and returns the resulting
// (sets, body) so individual tests can simulate downstream passes by
// rewriting the body and prepending the SETs.
func extractedFixture(t *testing.T, sql string) (sets []string, body string) {
	t.Helper()
	cfg := passes.NewExtractLiteralsConfig(0)
	cfg.SetMinINListSize(0)
	out, err := passes.ExtractLiterals(cfg)(sql)
	require.NoError(t, err)
	sets, _, body = passes.ParseExtractedQuery(out, "")
	return
}

func joinSets(sets []string, body string) string {
	var sb strings.Builder
	for _, s := range sets {
		sb.WriteString(s)
		sb.WriteString(";\n")
	}
	sb.WriteString(body)
	return sb.String()
}

// --- No-ops ---

func TestPruneUnreferencedParams_NoSets(t *testing.T) {
	got, err := passes.PruneUnreferencedParams("")("SELECT 1 FROM t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1 FROM t", got)
}

func TestPruneUnreferencedParams_AllReferenced(t *testing.T) {
	sets, body := extractedFixture(t, "SELECT a FROM t WHERE id = 42 AND name = 'longvalue'")
	require.Len(t, sets, 2)
	input := joinSets(sets, body)

	got, err := passes.PruneUnreferencedParams("")(input)
	require.NoError(t, err)
	// Both slots still in body → both SETs kept; output equals input.
	assert.Equal(t, input, got)
}

// --- Pruning ---

func TestPruneUnreferencedParams_DropsAllWhenBodyHasNoSlots(t *testing.T) {
	sets, _ := extractedFixture(t, "SELECT 12345, 67890 FROM t")
	require.Len(t, sets, 2)
	// Simulate a downstream pass that folded both slots away.
	input := joinSets(sets, "SELECT 99 FROM t")

	got, err := passes.PruneUnreferencedParams("")(input)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 99 FROM t", got)
	assert.NotContains(t, got, "SET ")
}

func TestPruneUnreferencedParams_DropsOnlyUnreferenced(t *testing.T) {
	sets, body := extractedFixture(t, "SELECT a FROM t WHERE id = 42 AND name = 'longvalue'")
	require.Len(t, sets, 2)

	// Find the param name corresponding to the integer literal (context "eq", value 42)
	// and remove its slot from the body, leaving the string slot intact.
	var keptName, droppedName string
	for _, set := range sets {
		if strings.Contains(set, "= 42") {
			droppedName = setName(set)
		} else {
			keptName = setName(set)
		}
	}
	require.NotEmpty(t, keptName)
	require.NotEmpty(t, droppedName)

	// Replace the dropped slot with a real literal (simulate eval folding it in).
	prunedBody := replaceSlot(body, droppedName, "42")
	require.NotContains(t, prunedBody, "{"+droppedName+":")
	require.Contains(t, prunedBody, "{"+keptName+":")

	input := joinSets(sets, prunedBody)
	got, err := passes.PruneUnreferencedParams("")(input)
	require.NoError(t, err)

	assert.NotContains(t, got, droppedName)
	assert.Contains(t, got, keptName)
	assert.Contains(t, got, prunedBody)
}

// --- Preservation of unrelated SETs ---

func TestPruneUnreferencedParams_PreservesRegularSets(t *testing.T) {
	sets, _ := extractedFixture(t, "SELECT a FROM t WHERE name = 'longvalue'")
	require.Len(t, sets, 1)

	// Body has no slot → extracted SET should be pruned, but a session-level
	// SET on a non-matching name must survive.
	input := sets[0] + ";\nSET max_threads = 4;\nSELECT a FROM t"
	got, err := passes.PruneUnreferencedParams("")(input)
	require.NoError(t, err)

	assert.NotContains(t, got, sets[0])
	assert.Contains(t, got, "SET max_threads = 4")
	assert.Contains(t, got, "SELECT a FROM t")
}

// --- CST-aware scan: only ParamSlotContext counts as a reference ---

// Bare-name occurrences in comments must not count as references.
func TestPruneUnreferencedParams_BareNameInCommentIsNotReference(t *testing.T) {
	sets, _ := extractedFixture(t, "SELECT 12345 FROM t")
	require.Len(t, sets, 1)
	name := setName(sets[0])

	body := "SELECT 99 /* mentions " + name + " in a comment */ FROM t"
	input := joinSets(sets, body)

	got, err := passes.PruneUnreferencedParams("")(input)
	require.NoError(t, err)
	assert.NotContains(t, got, "SET "+name+" = ")
	assert.Contains(t, got, body)
}

// The textual scan was vulnerable to this; the CST scan must reject it.
func TestPruneUnreferencedParams_BareNameInStringLiteralIsNotReference(t *testing.T) {
	sets, _ := extractedFixture(t, "SELECT 12345 FROM t")
	require.Len(t, sets, 1)
	name := setName(sets[0])

	// The param name appears inside a string literal, formatted exactly like
	// a slot would be: `{name: Int64}`. A textual scan would falsely keep
	// the SET; a CST scan must drop it.
	body := "SELECT 'pseudo-slot {" + name + ": Int64}' FROM t"
	input := joinSets(sets, body)

	got, err := passes.PruneUnreferencedParams("")(input)
	require.NoError(t, err)
	assert.NotContains(t, got, "SET "+name+" = ")
	assert.Contains(t, got, body)
}

// --- Bare-name references (CTE-injected style) ---

// After InjectParamsAsCTE rewrites a slot into a bare identifier (`{name:T}` →
// `name` with a `WITH value AS name` clause), prune must still recognise the
// name as referenced.
func TestPruneUnreferencedParams_BareIdentifierCountsAsReference(t *testing.T) {
	sets, _ := extractedFixture(t, "SELECT a FROM t WHERE id = 42")
	require.Len(t, sets, 1)
	name := setName(sets[0])

	// Hand-crafted CTE-style body: WITH … AS name … name as bare ref.
	body := "WITH 42 AS " + name + " SELECT a FROM t WHERE id = " + name
	input := joinSets(sets, body)

	got, err := passes.PruneUnreferencedParams("")(input)
	require.NoError(t, err)
	assert.Contains(t, got, "SET "+name+" = ")
	assert.Contains(t, got, body)
}

// CTE-injected with one slot remaining and one folded: the folded one (whose
// name appears nowhere in the body) must still be pruned.
func TestPruneUnreferencedParams_PrunesAfterPartialCTEInjection(t *testing.T) {
	sets, body := extractedFixture(t, "SELECT a FROM t WHERE id = 42 AND name = 'longvalue'")
	require.Len(t, sets, 2)

	var injectedName, droppedName string
	for _, set := range sets {
		if strings.Contains(set, "= 42") {
			injectedName = setName(set)
		} else {
			droppedName = setName(set)
		}
	}
	require.NotEmpty(t, injectedName)
	require.NotEmpty(t, droppedName)

	// Replace one slot with a bare ref (CTE-injected) and fold the other away.
	cteBody := strings.ReplaceAll(body, "{"+injectedName+": Int64}", injectedName)
	cteBody = "WITH 42 AS " + injectedName + " " + replaceSlot(cteBody, droppedName, "'inlined'")

	input := joinSets(sets, cteBody)
	got, err := passes.PruneUnreferencedParams("")(input)
	require.NoError(t, err)
	assert.Contains(t, got, "SET "+injectedName+" = ")
	assert.NotContains(t, got, "SET "+droppedName+" = ")
}

// --- Strict metadata validation ---

// An identifier whose text starts with the prefix but does not carry valid
// CBOR-encoded metadata (a hand-crafted column or a typo) must NOT enter the
// referenced set — the body-scan predicate is symmetric with the SET-admit
// predicate used by ParseExtractedQuery.
func TestPruneUnreferencedParams_RejectsInvalidMetadataIdentifiers(t *testing.T) {
	sets, _ := extractedFixture(t, "SELECT 12345 FROM t")
	require.Len(t, sets, 1)
	realName := setName(sets[0])

	// Body has no slot or valid bare ref for realName, but does have an
	// identifier with the prefix and invalid metadata suffix.
	bogus := passes.ParamPrefixExtracted + "_eq_zz" // "zz" is not valid hex
	body := "SELECT a FROM t WHERE a = " + bogus
	input := joinSets(sets, body)

	got, err := passes.PruneUnreferencedParams("")(input)
	require.NoError(t, err)
	assert.NotContains(t, got, "SET "+realName+" = ")
	// Bogus identifier survives in the body untouched (we don't rewrite it).
	assert.Contains(t, got, bogus)
}

// --- Idempotency ---

func TestPruneUnreferencedParams_Idempotent(t *testing.T) {
	sets, _ := extractedFixture(t, "SELECT 12345 FROM t")
	require.Len(t, sets, 1)
	input := joinSets(sets, "SELECT 99 FROM t")

	once, err := passes.PruneUnreferencedParams("")(input)
	require.NoError(t, err)
	twice, err := passes.PruneUnreferencedParams("")(once)
	require.NoError(t, err)
	assert.Equal(t, once, twice)
}

// --- Helpers ---

func setName(setLine string) string {
	line := strings.TrimPrefix(setLine, "SET ")
	eq := strings.Index(line, " = ")
	if eq < 0 {
		return ""
	}
	return strings.TrimSpace(line[:eq])
}

// replaceSlot rewrites every `{name: Type}` slot occurrence in body with the
// given SQL literal text — simulating a downstream pass folding the slot.
func replaceSlot(body string, name string, replacement string) string {
	out := body
	prefix := "{" + name + ":"
	for {
		idx := strings.Index(out, prefix)
		if idx < 0 {
			return out
		}
		end := strings.Index(out[idx:], "}")
		if end < 0 {
			return out
		}
		out = out[:idx] + replacement + out[idx+end+1:]
	}
}
