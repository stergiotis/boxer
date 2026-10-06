package play

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func TestLookupDocsCatalogEntry(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	spec, ok := m.Operations.Lookup(opLookupDocs)
	require.True(t, ok)
	assert.Equal(t, app.OperationClassExternalRead, spec.Class)
	assert.Equal(t, app.OperationEffectNone, spec.Effect)
	assert.True(t, spec.Untrusted, "user-defined bodies are authored on the endpoint")
	assert.True(t, spec.Agents)
	assert.Empty(t, spec.Reads)
	assert.Empty(t, spec.Writes)
}

// docsWindow is a window whose documentation source answers from ep.
func docsWindow(t *testing.T, ep *stubEndpoint) (*PlayLauncher, app.OperationsHandlerI) {
	t.Helper()
	l, h := opsLauncher(t)
	l.inner.client = NewClient(ClientConfig{URL: ep.srv.URL}, ep.srv.Client())
	src := NewClickHouseDocsSource(l.inner.client)
	t.Cleanup(src.Close)
	l.inner.docs = newDocsDriver(src)
	return l, h
}

// lookup_docs reads one name through its own request: an agent needs the
// endpoint in the grant, a kind picks among several, a long body is cut
// with the sections it lost named, and no name reads what the pane shows.
func TestLookupDocsReadsANameUnderTheGrant(t *testing.T) {
	long := "Takes an array.\n\n```sql\nSELECT arrayFold((acc, x) -> acc + x, [1, 2, 3], 0)\n```\n\n" +
		strings.Repeat("filler text ", 700) + "\n\n## Arguments\n\nmore\n\n## Returned value\n\nmore"
	ep := newStubEndpoint(t, stringStreamBytes(t, []string{"name", "type", "description", "source"}, [][]string{
		{"arrayFold", "Function", long, "src/Functions/array/arrayFold.cpp"},
		{"arrayFold", "Combinator", "the other kind", ""},
	}))
	l, h := docsWindow(t, ep)

	var refusal *app.OperationRefusal
	agent := app.OperationCall{Writer: "task:t", OnBehalfOf: &app.OnBehalfOf{Task: "t", Epoch: 1}}
	_, _, err := h.Snapshot().ExternalRead(agent, opLookupDocs, mustEncode(t, DocsArgs{Name: "arrayFold"}))
	require.ErrorAs(t, err, &refusal)
	assert.Equal(t, []string{ep.destination()}, refusal.Destinations)
	bodies, _ := ep.sent()
	assert.Empty(t, bodies, "nothing is sent for a lookup the grant does not cover")

	agent.OnBehalfOf.Destinations = []string{ep.destination()}
	out := externalReadOp[DocsReading](t, h, agent, opLookupDocs, DocsArgs{Name: "arrayFold"})
	assert.True(t, out.Found)
	assert.Equal(t, []string{"Function", "Combinator"}, out.Kinds)
	assert.Equal(t, "Function", out.Kind, "the first kind when none is named")
	assert.Equal(t, "src/Functions/array/arrayFold.cpp", out.Source)
	require.Len(t, out.Sql, 1)
	assert.Contains(t, out.Sql[0], "arrayFold((acc, x)")
	assert.True(t, out.Truncated)
	assert.LessOrEqual(t, len(out.Body), refMaxText)
	assert.Equal(t, []string{"Arguments", "Returned value"}, out.CutSections)
	_, params := ep.sent()
	require.Len(t, params, 1)
	assert.Contains(t, params[0], "param_n=arrayFold")

	other := externalReadOp[DocsReading](t, h, agent, opLookupDocs, DocsArgs{Name: "arrayFold", Kind: "combinator"})
	assert.Equal(t, "Combinator", other.Kind)
	assert.Equal(t, "the other kind", other.Body)
	_, _, err = h.Snapshot().ExternalRead(agent, opLookupDocs, mustEncode(t, DocsArgs{Name: "arrayFold", Kind: "Setting"}))
	require.ErrorAs(t, err, &refusal)
	assert.Contains(t, refusal.Reason, "Function, Combinator")

	// No name: what the pane shows, which does not move.
	_, _, err = h.Snapshot().ExternalRead(agent, opLookupDocs, nil)
	require.ErrorAs(t, err, &refusal, "the pane shows nothing yet")
	l.inner.docsPane.shown, l.inner.docsPane.shownKind = "arrayFold", "Combinator"
	pane := externalReadOp[DocsReading](t, h, agent, opLookupDocs, DocsArgs{})
	assert.True(t, pane.FromPane)
	assert.True(t, pane.PaneFollows)
	assert.Equal(t, "Combinator", pane.Kind, "the kind the pane shows")
	assert.Equal(t, "arrayFold", l.inner.docsPane.shown)
}

// A name the source does not document is a result, not an error.
func TestLookupDocsNothingFoundIsAResult(t *testing.T) {
	ep := newStubEndpoint(t, stringStreamBytes(t, []string{"name", "type", "description", "source"}, nil))
	_, h := docsWindow(t, ep)
	out := externalReadOp[DocsReading](t, h, app.OperationCall{Writer: "person"}, opLookupDocs, DocsArgs{Name: "noSuchFunction"})
	assert.False(t, out.Found)
	assert.Equal(t, "noSuchFunction", out.Name)
}

// docsOnlyPolls is a source that cannot answer at once.
type docsOnlyPolls struct{ DocsSourceI }

// A source without DocsLookupNowI leaves the operation refused.
func TestLookupDocsNeedsASourceThatAnswersAtOnce(t *testing.T) {
	l, h := opsLauncher(t)
	l.inner.docs = newDocsDriver(docsOnlyPolls{})
	var refusal *app.OperationRefusal
	_, _, err := h.Snapshot().ExternalRead(app.OperationCall{Writer: "person"}, opLookupDocs, mustEncode(t, DocsArgs{Name: "count"}))
	require.ErrorAs(t, err, &refusal)
	assert.Contains(t, refusal.Reason, "does not answer agents")
}
