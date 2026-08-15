package sysmfacts_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const componentRegenEnvVar = "BOXER_SYSMFACTS_COMPONENT_REGEN"

const componentGolden = "testdata/component-expansion.golden"

// goldenKinds are the three shapes worth pinning whole, not all thirteen.
//
// The artefacts themselves are already committed — they are in
// sysmetrics_store.out.go, so a re-aspected section or a re-keyed vocabulary
// shows in that file's diff without a golden here. Pinning every kind would
// add ~70 KB restating it. What this golden adds instead is the *pass over
// the real artefacts*: that the projection is emitted and the conformance
// filter reaches the WHERE, for the three storage shapes this vocabulary has.
var goldenKinds = []string{
	"SysMem",      // scalars on array sections — the plainest shape
	"SysNet",      // the M4 per-item tables, column-major across parallel lists
	"SysTopology", // the M6 adjacency list, the one shape parallel arrays cannot hold
}

func componentSource(t *testing.T) constructsql.ComponentSourceI {
	t.Helper()
	r := componentsql.NewRegistry()
	require.NoError(t, r.Register(sysmfacts.SysmetricsComponentSQL))
	return r
}

func expandComponent(t *testing.T, sql string) (out string) {
	t.Helper()
	out, err := constructsql.ComponentExpandPass(componentSource(t), "boxer").Run(sql)
	require.NoError(t, err)
	return
}

// TestComponentExpansionMatchesTheGolden pins what a component read ships
// (ADR-0189 M4). The pass is tested against stand-in artefacts in its own
// package; this is the same pass over the artefacts a real component
// definition generated.
func TestComponentExpansionMatchesTheGolden(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("# The sysmetrics component read surface, expanded (ADR-0189).\n")
	sb.WriteString("#\n")
	sb.WriteString("# Each block is one authored component read and what the client-side\n")
	sb.WriteString("# LW_COMPONENT pass ships. The WHERE is not in the authored form: it is\n")
	sb.WriteString("# the kind's conformance filter, injected, without which the projection\n")
	sb.WriteString("# would be a first-match read rather than an exact one (ADR-0066).\n")
	sb.WriteString("#\n")
	sb.WriteString("# Three kinds, not thirteen: the artefacts are already committed in\n")
	sb.WriteString("# sysmetrics_store.out.go, so this pins the pass over them rather than\n")
	sb.WriteString("# restating them.\n")
	sb.WriteString("#\n")
	sb.WriteString("# Regenerate with " + componentRegenEnvVar + "=1 go test ./public/keelson/runtime/sysmfacts/...\n")

	for _, kind := range goldenKinds {
		authored := "SELECT LW_COMPONENT('" + kind + "') AS c FROM boxer.facts"
		sb.WriteString("\n## " + kind + "\n")
		sb.WriteString("-- authored\n" + authored + "\n")
		sb.WriteString("-- expanded\n" + expandComponent(t, authored) + "\n")
	}
	got := sb.String()

	if os.Getenv(componentRegenEnvVar) != "" {
		require.NoError(t, os.MkdirAll(filepath.Dir(componentGolden), 0o755))
		require.NoError(t, os.WriteFile(componentGolden, []byte(got), 0o644))
		t.Skip("golden rewritten; unset " + componentRegenEnvVar + " to compare against it")
	}

	want, err := os.ReadFile(componentGolden)
	require.NoError(t, err, "read the golden; regenerate with "+componentRegenEnvVar+"=1")
	assert.Equal(t, string(want), got,
		"a component read moved; check whether the artefacts changed or the pass did")
}

// Every kind the store publishes expands. The golden speaks for three of
// them; this is what says the other ten are not quietly broken — a kind whose
// projection or filter failed to generate would be registrable and
// unexpandable, and nothing else would notice.
func TestEveryPublishedKindExpands(t *testing.T) {
	kinds := make([]string, 0, len(sysmfacts.SysmetricsComponentSQL.Kinds))
	for kind := range sysmfacts.SysmetricsComponentSQL.Kinds {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	require.Len(t, kinds, 13)

	for _, kind := range kinds {
		out := expandComponent(t, "SELECT LW_COMPONENT('"+kind+"') AS c FROM boxer.facts")

		assert.NotContainsf(t, out, "LW_COMPONENT", "%s: a call survived expansion", kind)
		assert.Containsf(t, out, "CAST(tuple(", "%s: no named-tuple projection", kind)
		assert.Containsf(t, out, " WHERE ", "%s: no filter was injected", kind)
		// The validator half is what makes the read exact rather than
		// first-match, so its presence is the property worth asserting per
		// kind rather than the filter's mere existence.
		assert.Containsf(t, out, "countEqual(", "%s: the injected filter carries no validator", kind)
	}
}

// The filter injected for a kind is the one the store's own Scan verb uses.
// Two read paths over one definition disagreeing is the failure this whole
// surface exists to prevent, and it is cheap to check without a server.
func TestInjectedFilterIsTheStoresOwnFilter(t *testing.T) {
	for _, kind := range goldenKinds {
		out := expandComponent(t, "SELECT LW_COMPONENT('"+kind+"') FROM boxer.facts")
		assert.Containsf(t, out, sysmfacts.SysmetricsComponentSQL.Kinds[kind].Filter,
			"%s: the injected predicate is not the published Filter", kind)
	}
}
