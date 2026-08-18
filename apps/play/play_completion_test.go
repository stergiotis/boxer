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

// The off-caret half of the two-way report: a literal that does not resolve
// takes the error tone, one that does takes the resolved tone, and the caret's
// own token takes neither from this producer.
func TestCompletionFindingSections(t *testing.T) {
	componentsql.Default.Reset()
	t.Cleanup(componentsql.Default.Reset)
	require.NoError(t, RegisterComponents(componentsql.Default))

	app := tabsTestApp()
	app.completion.engine = &sqlcomplete.Engine{
		Vocab:     testVocabRegistry(t),
		Providers: app.completionProviders(),
	}
	app.sql = `SELECT LW_COMPONENT('SysMem'), LW_COMPONENT('Nonesuch')`
	app.completion.findings = app.completion.engine.Validate(app.sql, nil, -1)
	require.Len(t, app.completion.findings, 2)

	secs := app.completionFindingSections()
	require.Len(t, secs, 2)
	assert.Equal(t, sqleditor.ToneResolved, secs[0].Color)
	assert.Equal(t, styleErrorTone, secs[1].Color)
	assert.Equal(t, "SysMem", app.sql[secs[0].Start:secs[0].Stop])
	assert.Equal(t, "Nonesuch", app.sql[secs[1].Start:secs[1].Stop])

	// A finding whose range no longer indexes the buffer is dropped, not
	// clamped: an underline in the wrong place is worse than none.
	app.sql = "SELECT 1"
	assert.Empty(t, app.completionFindingSections())
}

// The Tab gate (ADR-0190 §SD10): the key is only taken when it would insert
// something, so Tab means a tab character the rest of the time.
func TestCompletionWantsTab(t *testing.T) {
	app := tabsTestApp()
	st := &app.completion

	assert.False(t, app.completionWantsTab(), "nothing to complete")

	st.result = sqlcomplete.Result{
		Items:  []sqlcomplete.Item{{Text: "SysMem", Insert: "SysMem"}, {Text: "SysCPU", Insert: "SysCPU"}},
		Prefix: []int{0, 1},
		Exact:  -1,
	}
	st.typed = "S"
	st.atEnd = true
	assert.True(t, app.completionWantsTab(), "the two agree on Sys")

	st.typed = "Sys"
	assert.False(t, app.completionWantsTab(), "they agree on nothing more")

	st.typed = "S"
	st.atEnd = false
	assert.False(t, app.completionWantsTab(), "a suffix insert needs the caret at the end")
}

// TestCompareFoldMatchesLoweringBothSides pins the claim compareFold is written
// on: that it answers what a bytewise comparison of the two lowered strings
// would, without building them. The oracle is that comparison, spelled out.
func TestCompareFoldMatchesLoweringBothSides(t *testing.T) {
	corpus := []string{
		"", "a", "A", "aa", "aA", "Ab", "ab", "abc", "ABC", "aBc",
		"substring", "SUBSTRING", "substr", "toString", "TOSTRING",
		"LW_GET", "lw_get", "Lw_Get", "_x", "x_", "x0", "X0", "x1",
		// Past ASCII, where the rune path takes over. The pairs differ in
		// case, in length, and at the byte where they leave ASCII.
		"é", "É", "éa", "Éb", "über", "ÜBER", "Straße", "STRASSE",
		"日本語", "a日", "A日", "aé", "Aé", "z", "Z", "{", "[",
	}
	for _, a := range corpus {
		for _, b := range corpus {
			want := strings.Compare(strings.ToLower(a), strings.ToLower(b))
			assert.Equalf(t, want, compareFold(a, b), "compareFold(%q, %q)", a, b)
		}
	}
}

// TestCompareFoldThenExactIsATotalOrder is the property vocabProbe's container
// depends on: distinct spellings never compare equal, so keying on this
// comparator cannot merge two functions the endpoint reports separately.
func TestCompareFoldThenExactIsATotalOrder(t *testing.T) {
	names := []string{"substring", "SUBSTRING", "Substring", "substr", "é", "É"}
	for _, a := range names {
		for _, b := range names {
			c := compareFoldThenExact(a, b)
			if a == b {
				assert.Equalf(t, 0, c, "%q against itself", a)
				continue
			}
			assert.NotEqualf(t, 0, c, "distinct names %q and %q must not tie", a, b)
			assert.Equalf(t, -c, compareFoldThenExact(b, a), "antisymmetry for %q, %q", a, b)
		}
	}
}

// TestSortCompletionItemsKeepsOneCaseInsensitiveRun is the ordering the pane
// shows: names group by their folded spelling rather than splitting into an
// upper-case run and a lower-case one.
func TestSortCompletionItemsKeepsOneCaseInsensitiveRun(t *testing.T) {
	items := []sqlcomplete.Item{
		{Text: "beta"}, {Text: "Alpha"}, {Text: "ALPHA"}, {Text: "gamma"}, {Text: "alpha"},
	}
	sortCompletionItems(items)
	got := make([]string, 0, len(items))
	for _, it := range items {
		got = append(got, it.Text)
	}
	assert.Equal(t, []string{"ALPHA", "Alpha", "alpha", "beta", "gamma"}, got)
}

// TestRefreshCompletionMemoIsPerRequest covers the memo's two obligations: an
// unchanged frame must not recompute, and a moved caret must not be served the
// previous frame's answer.
func TestRefreshCompletionMemoIsPerRequest(t *testing.T) {
	componentsql.Default.Reset()
	t.Cleanup(componentsql.Default.Reset)
	require.NoError(t, RegisterComponents(componentsql.Default))
	require.NoError(t, RegisterVocabulary(sqlvocab.Default))
	t.Cleanup(sqlvocab.Default.Reset)

	app := tabsTestApp()
	ed := sqleditor.New()
	buf := "SELECT LW_COMPONENT('Sys"
	ed.SetCaretForTest(uint64(len([]rune(buf))))
	app.refreshCompletion(ed.Bind(sqleditor.Frame{IDSlot: "t", Value: &buf}))
	require.True(t, app.completion.resultValid)
	first := app.completion.result
	key := app.completion.resultKey

	// Same buffer, same caret: the key is unchanged, so the answer is the one
	// already computed rather than an equal one computed again.
	app.refreshCompletion(ed.Bind(sqleditor.Frame{IDSlot: "t", Value: &buf}))
	assert.Equal(t, key, app.completion.resultKey)
	assert.Equal(t, completionTexts(first), completionTexts(app.completion.result))

	// A caret one byte back is a different site — a stale hit here would be
	// the pane describing a caret the editor has left.
	ed.SetCaretForTest(uint64(len([]rune(buf))) - 1)
	app.refreshCompletion(ed.Bind(sqleditor.Frame{IDSlot: "t", Value: &buf}))
	assert.NotEqual(t, key, app.completion.resultKey)
	assert.Equal(t, "Sy", app.completion.typed)
}
