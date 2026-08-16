//go:build integration

package play

// The precision oracle for ADR-0190's completion engine: every candidate it
// offers, spliced back into the buffer it was offered for, must be a statement
// the server accepts.
//
// This is SD1 made mechanical. A unit test can pin that the engine offers what
// a registry holds; only a server can say whether what the registry holds is
// what a query may actually name. The two halves it checks are the two ways
// the claim can be false: an offered spelling the server refuses, and a
// spelling the server accepts that the engine did not offer.
//
// LIMIT 0 throughout — the question is whether the statement ANALYSES, and
// reading rows would make the test depend on what the endpoint happens to hold.
//
// The splice ships through BuildStatement, not raw: LW_COMPONENT is a CLIENT
// macro, so the buffer a user writes is not the body the server sees. Sending
// the raw text would test the wrong thing — it would ask whether ClickHouse has
// a function called LW_COMPONENT, which it never does.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlcomplete"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	passregdefaults "github.com/stergiotis/boxer/public/keelson/data/passreg/defaults"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// liveCompletionRig is play's own wiring — the same registries a running
// process has — over a client pointed at the live endpoint.
type liveCompletionRig struct {
	engine *sqlcomplete.Engine
	client *Client
	reg    *componentsql.Registry
}

func newLiveCompletionRig(t *testing.T) *liveCompletionRig {
	t.Helper()
	componentsql.Default.Reset()
	t.Cleanup(componentsql.Default.Reset)
	require.NoError(t, RegisterComponents(componentsql.Default))

	// The host's own pass wiring, so a spliced buffer reaches the server the
	// way a typed one does. Best-effort registration, as at boot: the standard
	// set may already be there from another test in this binary.
	_ = passregdefaults.RegisterDefaults()
	_ = RegisterPasses(passreg.Default)

	vocab := sqlvocab.NewRegistry()
	require.NoError(t, RegisterVocabulary(vocab))

	app := tabsTestApp()
	app.client = NewClient(ClientConfig{URL: liveClickHouseURL(t)}, nil)
	app.completion.catalog = newCatalogProbe(app.client)
	return &liveCompletionRig{
		engine: &sqlcomplete.Engine{
			Vocab:            vocab,
			Providers:        app.completionProviders(),
			NamedTupleAccess: true,
		},
		client: app.client,
		reg:    componentsql.Default,
	}
}

// completeAtLive runs one request over the caret-marked buffer.
func (inst *liveCompletionRig) completeAtLive(t *testing.T, buf string) (string, sqlcomplete.Result) {
	t.Helper()
	i := strings.Index(buf, "|")
	require.GreaterOrEqualf(t, i, 0, "case %q has no caret marker", buf)
	sql := buf[:i] + buf[i+1:]
	return sql, inst.engine.Complete(sqlcomplete.Request{
		Site: highlight.SiteAtIn(sql, i), Statement: sql, Caret: i,
	})
}

// analyses ships one statement and reports whether the server accepted it.
func (inst *liveCompletionRig) analyses(t *testing.T, sql string) error {
	t.Helper()
	body, params := inst.client.BuildStatement(sql)
	rec, _, _, err := clientExecutor{client: inst.client, opts: newExecOptions("completion-oracle")}.
		execute(context.Background(), compiledNode{SQL: body, Params: params}, memory.NewGoAllocator())
	if rec != nil {
		rec.Release()
	}
	return err
}

// TestLiveEveryOfferedComponentKindRuns splices each offered kind into the
// buffer it was offered for and checks the server accepts the result.
func TestLiveEveryOfferedComponentKindRuns(t *testing.T) {
	rig := newLiveCompletionRig(t)
	head := `SELECT LW_COMPONENT('`
	_, res := rig.completeAtLive(t, head+`|`)
	require.Empty(t, res.Silent)
	require.NotEmpty(t, res.Items)

	for _, it := range res.Items {
		t.Run(it.Text, func(t *testing.T) {
			sql := head + it.Text + `') FROM boxer.facts LIMIT 0`
			assert.NoErrorf(t, rig.analyses(t, sql), "an offered kind must be a kind a query may name")
		})
	}
}

// TestLiveEveryOfferedFieldRuns is the same for the field position, per kind:
// the fields offered for `tupleElement(LW_COMPONENT(k), '…')` must all read.
func TestLiveEveryOfferedFieldRuns(t *testing.T) {
	rig := newLiveCompletionRig(t)
	kinds := rig.reg.Kinds()
	require.NotEmpty(t, kinds)

	for _, kind := range kinds {
		head := `SELECT tupleElement(LW_COMPONENT('` + kind + `'), '`
		_, res := rig.completeAtLive(t, head+`|`)
		require.Emptyf(t, res.Silent, "%s: %s", kind, res.Silent)
		require.NotEmptyf(t, res.Items, "%s offers no fields", kind)

		for _, it := range res.Items {
			t.Run(kind+"/"+it.Text, func(t *testing.T) {
				sql := head + it.Text + `') FROM boxer.facts LIMIT 0`
				assert.NoErrorf(t, rig.analyses(t, sql), "an offered field must be a field a query may read")
			})
		}
	}
}

// TestLiveOfferedFieldsAreExactlyTheKinds is the other half of the claim: the
// offered set equals the kind's own slot list — no more, and no less.
//
// The "no less" direction is what a spliced-execution check alone cannot see:
// an engine offering a strict subset would pass every execution and still be
// hiding half the component.
func TestLiveOfferedFieldsAreExactlyTheKinds(t *testing.T) {
	rig := newLiveCompletionRig(t)
	for _, kind := range rig.reg.Kinds() {
		t.Run(kind, func(t *testing.T) {
			b, ok := rig.reg.Lookup(kind)
			require.True(t, ok)
			elems, err := b.Elements()
			require.NoError(t, err)
			want := make([]string, len(elems))
			for i := range elems {
				want[i] = elems[i].Name
			}

			_, res := rig.completeAtLive(t,
				`SELECT tupleElement(LW_COMPONENT('`+kind+`'), '|`)
			got := make([]string, len(res.Items))
			for i := range res.Items {
				got[i] = res.Items[i].Text
			}
			assert.Equal(t, want, got)
		})
	}
}

// TestLiveDotFormRunsWhereTupleElementDoes pins ADR-0190 §SD11 against the
// server: the spelling grammar1 learned means the same thing the canonical
// form does, all the way through play's pass pipeline.
func TestLiveDotFormRunsWhereTupleElementDoes(t *testing.T) {
	rig := newLiveCompletionRig(t)
	kind := "SysMem"
	b, ok := rig.reg.Lookup(kind)
	require.True(t, ok)
	elems, err := b.Elements()
	require.NoError(t, err)
	require.NotEmpty(t, elems)
	field := elems[len(elems)-1].Name

	dot := `SELECT LW_COMPONENT('` + kind + `').` + field + ` FROM boxer.facts LIMIT 0`
	fn := `SELECT tupleElement(LW_COMPONENT('` + kind + `'), '` + field + `') FROM boxer.facts LIMIT 0`
	assert.NoError(t, rig.analyses(t, fn), "the canonical form must run")
	assert.NoError(t, rig.analyses(t, dot), "the dot form must run wherever the canonical one does")
}

// TestLiveEveryOfferedIntrospectionTableRuns covers the third in-process
// domain that names something a query addresses.
func TestLiveEveryOfferedIntrospectionTableRuns(t *testing.T) {
	rig := newLiveCompletionRig(t)
	_, res := rig.completeAtLive(t, `SELECT * FROM keelson('|`)
	if len(res.Items) == 0 {
		t.Skipf("no introspection catalogue in this binary: %s", res.Silent)
	}
	for _, it := range res.Items {
		t.Run(it.Text, func(t *testing.T) {
			sql := `SELECT * FROM keelson('` + it.Text + `') LIMIT 0`
			assert.NoErrorf(t, rig.analyses(t, sql), "an offered introspection table must be readable")
		})
	}
}

// TestLiveCatalogueProbesAnswer walks the endpoint-dependent providers, so a
// query whose column list or spelling changed on the server side is caught
// here rather than as a pane that quietly says "waiting".
func TestLiveCatalogueProbesAnswer(t *testing.T) {
	rig := newLiveCompletionRig(t)
	cat := rig.engine.Providers.Catalog
	cases := []struct {
		name string
		call func() ([]sqlcomplete.Item, bool)
	}{
		{"databases", cat.Databases},
		{"tables", func() ([]sqlcomplete.Item, bool) { return cat.Tables("system") }},
		{"columns", func() ([]sqlcomplete.Item, bool) { return cat.Columns("system.columns") }},
		{"settings", cat.Settings},
		{"type families", cat.TypeNames},
		{"time zones", cat.TimeZones},
		{"formats", cat.Formats},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// The lanes are level-triggered and asynchronous: the first demand
			// starts the query, and the answer lands on a later one once the
			// worker has come back. So this polls on wall-clock rather than
			// spinning — a tight loop re-asks a thousand times inside the
			// round trip and learns nothing.
			var items []sqlcomplete.Item
			var ready bool
			deadline := time.Now().Add(30 * time.Second)
			for time.Now().Before(deadline) {
				items, ready = c.call()
				if ready {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			require.Truef(t, ready, "%s never answered", c.name)
			assert.NotEmptyf(t, items, "%s answered empty", c.name)
		})
	}
}

// The component family's two names are what the pane offers a kind for; if the
// roster grew a third the domains above would not cover it.
func TestLiveComponentFamilyIsTheTwoNames(t *testing.T) {
	names := make([]string, 0, 2)
	for _, f := range constructsql.ComponentFunctions() {
		names = append(names, f.Name)
	}
	assert.ElementsMatch(t, []string{constructsql.NameComponent, constructsql.NameComponentFilter}, names)
}
