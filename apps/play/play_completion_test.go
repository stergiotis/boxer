package play

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlcomplete"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sqleditor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// completionTestEngine builds the engine exactly as a running play does: the
// host's vocabulary registry and the host's in-process providers.
func completionTestEngine(t *testing.T) *sqlcomplete.Engine {
	t.Helper()
	componentsql.Default.Reset()
	t.Cleanup(componentsql.Default.Reset)
	require.NoError(t, RegisterComponents(componentsql.Default))

	app := tabsTestApp()
	return &sqlcomplete.Engine{
		Vocab:     testVocabRegistry(t),
		Providers: app.completionProviders(),
	}
}

func completeAt(t *testing.T, e *sqlcomplete.Engine, buf string) sqlcomplete.Result {
	t.Helper()
	i := strings.Index(buf, "|")
	require.GreaterOrEqualf(t, i, 0, "case %q has no caret marker", buf)
	sql := buf[:i] + buf[i+1:]
	return e.Complete(sqlcomplete.Request{
		Site: highlight.SiteAtIn(sql, i), Statement: sql, Caret: i,
	})
}

func completionTexts(res sqlcomplete.Result) (out []string) {
	out = make([]string, len(res.Items))
	for i := range res.Items {
		out[i] = res.Items[i].Text
	}
	return
}

// The driving cases through play's own wiring: the registries this build
// actually carries, not a fixture (ADR-0190 M1).
func TestCompletionAnswersTheDrivingCases(t *testing.T) {
	e := completionTestEngine(t)

	kinds := completeAt(t, e, `SELECT LW_COMPONENT('|`)
	require.Empty(t, kinds.Silent)
	assert.Contains(t, completionTexts(kinds), "SysMem")
	assert.Equal(t, sqlvocab.DomainComponentKind, kinds.Domain.Kind)

	prefix := completeAt(t, e, `SELECT LW_COMPONENT('Sys|`)
	assert.Equal(t, sqlcomplete.MatchPrefix, prefix.Match)
	assert.NotEmpty(t, prefix.Prefix)

	exact := completeAt(t, e, `SELECT LW_COMPONENT('SysMem|`)
	assert.Equal(t, sqlcomplete.MatchExact, exact.Match)
	it, ok := exact.ExactItem()
	require.True(t, ok)
	assert.Equal(t, "SysMem", it.Text)

	// The caret moved back inside a complete literal still reports that the
	// whole content resolves — the state §SD9 tints the editor for.
	inside := completeAt(t, e, `SELECT LW_COMPONENT('Sys|Mem')`)
	assert.Equal(t, sqlcomplete.MatchExact, inside.Match)

	fields := completeAt(t, e, `SELECT tupleElement(LW_COMPONENT('SysMem'), '|`)
	require.Empty(t, fields.Silent)
	names := completionTexts(fields)
	assert.Contains(t, names, "TotalBytes")
	assert.Contains(t, names, "Id")
	assert.NotContains(t, names, "LoadAvg1", "the sibling argument decides which kind's fields these are")
	for i := range fields.Items {
		assert.NotEmptyf(t, fields.Items[i].Type, "%s carries no type", fields.Items[i].Text)
	}

	// The introspection catalogue is registered by the host at boot, so a test
	// binary's is empty; what the pane must get right here is that the POSITION
	// resolves to the introspection-table domain rather than falling through to
	// the clause rule.
	tables := completeAt(t, e, `SELECT * FROM keelson('|`)
	assert.Equal(t, sqlvocab.DomainIntrospectionTable, tables.Domain.Kind)
	assert.Equal(t, "keelson", tables.Callee)

	glosses := completeAt(t, e, `SELECT gloss(x, '|`)
	require.Empty(t, glosses.Silent)
	assert.NotEmpty(t, completionTexts(glosses))
}

// The precision claim, in the form a test can hold: for every registered kind,
// the offered field set equals what the kind's Projection projects — no more,
// no less (ADR-0190's verification plan).
func TestCompletionOffersExactlyTheKindsFields(t *testing.T) {
	e := completionTestEngine(t)
	kinds := componentsql.Default.Kinds()
	require.NotEmpty(t, kinds)

	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			b, ok := componentsql.Default.Lookup(kind)
			require.True(t, ok)
			elems, err := b.Elements()
			require.NoError(t, err)
			want := make([]string, len(elems))
			for i := range elems {
				want[i] = elems[i].Name
			}

			res := completeAt(t, e, `SELECT tupleElement(LW_COMPONENT('`+kind+`'), '|`)
			require.Empty(t, res.Silent)
			assert.Equal(t, want, completionTexts(res))
		})
	}
}

// Where nothing in process can answer, the pane gets a sentence rather than an
// empty table (§SD1). These are the domains this build does not wire yet.
func TestCompletionIsSilentWithAReason(t *testing.T) {
	e := completionTestEngine(t)
	for _, buf := range []string{
		`SELECT LW_GET('|`,
		`SELECT LW_COMPONENT('Nonesuch'), tupleElement(m, '|`,
		`SELECT a FROM |`,
		`SELECT m.| FROM t`,
	} {
		t.Run(buf, func(t *testing.T) {
			res := completeAt(t, e, buf)
			assert.Empty(t, res.Items)
			assert.NotEmpty(t, res.Silent, "a silence with no reason is indistinguishable from a bug")
		})
	}
}

// The editor's half of the two-way report: an exact match becomes a resolved
// underline over the token, and nothing otherwise.
func TestResolvedTokenSection(t *testing.T) {
	app := tabsTestApp()
	app.sql = "SELECT LW_COMPONENT('SysMem')"

	app.completion.result = sqlcomplete.Result{
		Match:   sqlcomplete.MatchExact,
		Partial: highlight.Range{Start: 22, Stop: 28},
	}
	sec, ok := app.resolvedTokenSection()
	require.True(t, ok)
	assert.Equal(t, uint32(22), sec.Start)
	assert.Equal(t, uint32(28), sec.Stop)

	app.completion.result.Match = sqlcomplete.MatchPrefix
	_, ok = app.resolvedTokenSection()
	assert.False(t, ok, "a prefix match is not a claim that the token resolves")

	// A range left over from a longer buffer is dropped rather than clamped:
	// an underline in the wrong place is worse than none.
	app.completion.result = sqlcomplete.Result{
		Match:   sqlcomplete.MatchExact,
		Partial: highlight.Range{Start: 10, Stop: 9999},
	}
	_, ok = app.resolvedTokenSection()
	assert.False(t, ok)
}

// The pane is fed from the editor's Bind, so its answer and the tint always
// describe the same caret.
func TestRefreshCompletionUsesTheEditorsSite(t *testing.T) {
	componentsql.Default.Reset()
	t.Cleanup(componentsql.Default.Reset)
	require.NoError(t, RegisterComponents(componentsql.Default))
	require.NoError(t, RegisterVocabulary(sqlvocab.Default))
	t.Cleanup(sqlvocab.Default.Reset)

	app := tabsTestApp()
	ed := sqleditor.New()
	buf := "SELECT LW_COMPONENT('Sys"
	n := uint64(len([]rune(buf)))
	ed.SetCaretForTest(n)
	app.refreshCompletion(ed.Bind(sqleditor.Frame{IDSlot: "t", Value: &buf}))

	assert.Equal(t, "Sys", app.completion.typed)
	assert.True(t, app.completion.atEnd)
	assert.Contains(t, completionTexts(app.completion.result), "SysMem")
}
