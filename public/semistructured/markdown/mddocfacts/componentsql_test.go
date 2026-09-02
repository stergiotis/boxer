package mddocfacts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mddocfacts"
)

const componentRegenEnvVar = "BOXER_MDDOCFACTS_COMPONENT_REGEN"

const componentGolden = "testdata/component-expansion.golden"

func componentSource(t *testing.T) constructsql.ComponentSourceI {
	t.Helper()
	r := componentsql.NewRegistry()
	require.NoError(t, r.Register(mddocfacts.MddocComponentSQL))
	return r
}

func expandComponent(t *testing.T, sql string) (out string) {
	t.Helper()
	out, err := constructsql.ComponentExpandPass(componentSource(t), "boxer").Run(sql)
	require.NoError(t, err)
	return
}

// componentKinds is every kind the store publishes, in the order the golden
// lists them. A kind added to the store and not here is a kind the golden
// cannot speak for — TestEveryKindIsInTheGolden says so.
var componentKinds = []string{"MdDoc", "MdHeading", "MdCodeBlock", "MdLink", "MdEmphasis", "MdTag"}

func TestEveryKindIsInTheGolden(t *testing.T) {
	var published []string
	for kind := range mddocfacts.MddocComponentSQL.Kinds {
		published = append(published, kind)
	}
	assert.ElementsMatch(t, componentKinds, published)
}

// TestComponentExpansionMatchesTheGolden pins what each component read ships
// (ADR-0189 M4, the sysmfacts pattern): the projection is emitted and the
// kind's conformance filter reaches the WHERE — without it the read would be
// a first-match rather than an exact one (ADR-0066). The MdDoc block is the
// read mdedit's send-to-play launch query stands on; the item kinds are what
// the markdown how-to's graph, backlink and tag queries stand on.
func TestComponentExpansionMatchesTheGolden(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("# The mddoc component read surface, expanded (ADR-0189).\n")
	sb.WriteString("#\n")
	sb.WriteString("# One authored component read per kind and what the client-side\n")
	sb.WriteString("# LW_COMPONENT pass ships. The WHERE is the kind's conformance filter, injected.\n")
	sb.WriteString("#\n")
	sb.WriteString("# Regenerate with " + componentRegenEnvVar + "=1 go test ./public/semistructured/markdown/mddocfacts/...\n")

	for _, kind := range componentKinds {
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
	require.NoError(t, err)
	assert.Equal(t, string(want), got)
}

// TestLaunchShapedReadExpands pins the exact SHAPE mdedit's launch query
// uses: the tupleElement projection over the component, an id filter beside
// the injected conformance filter. The gloss(...) macro is play-side and
// expands in a later pass, so it is not part of this one's input.
func TestLaunchShapedReadExpands(t *testing.T) {
	authored := "SELECT tupleElement(LW_COMPONENT('MdDoc'), 'Content') FROM boxer.facts WHERE \"id:id\" = 42 LIMIT 1"
	out := expandComponent(t, authored)
	assert.NotContains(t, out, "LW_COMPONENT", "the macro must be fully expanded")
	assert.Contains(t, out, "Content String", "the projection carries the content column")
	assert.Contains(t, out, "\"id:id\" = 42", "the caller's filter survives beside the conformance filter")
}
