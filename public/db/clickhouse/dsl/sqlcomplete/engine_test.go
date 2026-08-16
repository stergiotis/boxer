package sqlcomplete_test

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/chtype"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlcomplete"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const caret = "|"

func req(t *testing.T, buf string) sqlcomplete.Request {
	t.Helper()
	i := strings.Index(buf, caret)
	require.GreaterOrEqualf(t, i, 0, "case %q has no caret marker", buf)
	sql := buf[:i] + buf[i+len(caret):]
	return sqlcomplete.Request{Site: highlight.SiteAtIn(sql, i), Statement: sql, Caret: i}
}

func items(names ...string) []sqlcomplete.Item {
	out := make([]sqlcomplete.Item, len(names))
	for i, n := range names {
		out[i] = sqlcomplete.Item{Text: n, Source: "test"}
	}
	return out
}

func texts(res sqlcomplete.Result) (out []string) {
	out = make([]string, len(res.Items))
	for i := range res.Items {
		out[i] = res.Items[i].Text
	}
	return
}

// The two fixture kinds: one wide enough to prefix-match inside, one to prove
// the engine offers the kind's own fields and not another's.
var fixtureTypes = map[string]string{
	"SysMem": "Tuple(Id UInt64, Ts DateTime64(9, 'UTC'), TotalBytes UInt64, TotalPercent UInt8, FreeBytes UInt64)",
	"SysCPU": "Tuple(Id UInt64, LoadAvg1 Float32)",
}

func testEngine(t *testing.T) *sqlcomplete.Engine {
	t.Helper()
	r := sqlvocab.NewRegistry()
	require.NoError(t, r.Register(
		sqlvocab.Function{
			Name: "LW_COMPONENT", Where: sqlvocab.WhereClient, Family: "components", Available: true,
			Params: []sqlvocab.Param{sqlvocab.Lit("'Kind'", sqlvocab.DomainComponentKind)},
		},
		sqlvocab.Function{
			Name: "keelson", Where: sqlvocab.WhereClient, Family: "introspection", Available: true,
			Params: []sqlvocab.Param{sqlvocab.Lit("'table'", sqlvocab.DomainIntrospectionTable)},
		},
		sqlvocab.Function{
			Name: "LW_GET", Where: sqlvocab.WhereClient, Family: "extraction", Available: true,
			Params: []sqlvocab.Param{
				sqlvocab.Lit("'section'", sqlvocab.DomainSection),
				sqlvocab.Lit("'membership'|id", sqlvocab.DomainMembership),
				sqlvocab.Of("'chan:…'", sqlvocab.DomainExtractionToken, 0),
			},
		},
		sqlvocab.Function{
			Name: "gloss", Where: sqlvocab.WhereClient, Family: "glosses", Available: true,
			Params: []sqlvocab.Param{
				sqlvocab.Expr("expr"),
				sqlvocab.Lit("'gloss/…'", sqlvocab.DomainGloss),
				sqlvocab.Of("'key'", sqlvocab.DomainGlossKey, 1),
				sqlvocab.Expr("value…"),
			},
		},
	))

	return &sqlcomplete.Engine{
		Vocab: r,
		Providers: sqlcomplete.Providers{
			ComponentKinds: func() ([]sqlcomplete.Item, bool) { return items("SysCPU", "SysMem"), true },
			ComponentType: func(kind string) (chtype.Type, bool) {
				s, hit := fixtureTypes[kind]
				if !hit {
					return chtype.Type{}, false
				}
				ty, err := chtype.Parse(s)
				return ty, err == nil
			},
			IntrospectionTables: func() ([]sqlcomplete.Item, bool) {
				return items("lw_components", "packages"), true
			},
			Sections:         func() ([]sqlcomplete.Item, bool) { return items("symbol", "u64Array"), true },
			ExtractionTokens: func(string) ([]sqlcomplete.Item, bool) { return items("chan:low-card-ref"), true },
			Glosses:          func() ([]sqlcomplete.Item, bool) { return items("gloss/bytes", "gloss/duration"), true },
			GlossKeys: func(g string) ([]sqlcomplete.Item, bool) {
				if g == "gloss/bytes" {
					return items("unit", "base"), true
				}
				return nil, true
			},
			// Deliberately unanswered, to separate "not yet" from "empty".
			Memberships: func() ([]sqlcomplete.Item, bool) { return nil, false },
		},
	}
}

func TestCompleteDrivingCases(t *testing.T) {
	e := testEngine(t)
	cases := []struct {
		buf   string
		want  []string
		match sqlcomplete.MatchE
		exact string
	}{
		{
			buf:  `SELECT LW_COMPONENT('|`,
			want: []string{"SysCPU", "SysMem"}, match: sqlcomplete.MatchNone,
		},
		{
			buf:  `SELECT LW_COMPONENT('Sys|`,
			want: []string{"SysCPU", "SysMem"}, match: sqlcomplete.MatchPrefix,
		},
		{
			buf:  `SELECT LW_COMPONENT('SysMem|`,
			want: []string{"SysCPU", "SysMem"}, match: sqlcomplete.MatchExact, exact: "SysMem",
		},
		{
			// The caret moved back inside a complete literal: prefix filters
			// on what precedes it, exact reads the whole content.
			buf:  `SELECT LW_COMPONENT('Sys|Mem')`,
			want: []string{"SysCPU", "SysMem"}, match: sqlcomplete.MatchExact, exact: "SysMem",
		},
		{
			buf:   `SELECT tupleElement(LW_COMPONENT('SysMem'), '|`,
			want:  []string{"Id", "Ts", "TotalBytes", "TotalPercent", "FreeBytes"},
			match: sqlcomplete.MatchNone,
		},
		{
			buf:   `SELECT tupleElement(LW_COMPONENT('SysMem'), 'Tot|`,
			want:  []string{"Id", "Ts", "TotalBytes", "TotalPercent", "FreeBytes"},
			match: sqlcomplete.MatchPrefix,
		},
		{
			buf:   `SELECT tupleElement(LW_COMPONENT('SysMem'), 'TotalBytes|`,
			want:  []string{"Id", "Ts", "TotalBytes", "TotalPercent", "FreeBytes"},
			match: sqlcomplete.MatchExact, exact: "TotalBytes",
		},
		{
			// The other kind's fields, to prove the sibling is what decides.
			buf:  `SELECT tupleElement(LW_COMPONENT('SysCPU'), '|`,
			want: []string{"Id", "LoadAvg1"}, match: sqlcomplete.MatchNone,
		},
		{
			buf:  `SELECT keelson('|`,
			want: []string{"lw_components", "packages"}, match: sqlcomplete.MatchNone,
		},
		{
			buf:  `SELECT LW_GET('|`,
			want: []string{"symbol", "u64Array"}, match: sqlcomplete.MatchNone,
		},
		{
			// The repeating tail: the fourth argument still resolves.
			buf:  `SELECT LW_GET('symbol', 'm', 'chan:a', '|`,
			want: []string{"chan:low-card-ref"}, match: sqlcomplete.MatchNone,
		},
		{
			buf:  `SELECT gloss(x, '|`,
			want: []string{"gloss/bytes", "gloss/duration"}, match: sqlcomplete.MatchNone,
		},
		{
			// A ref-dependent domain reading the sibling's literal value.
			buf:  `SELECT gloss(x, 'gloss/bytes', '|`,
			want: []string{"unit", "base"}, match: sqlcomplete.MatchNone,
		},
	}
	for _, c := range cases {
		t.Run(c.buf, func(t *testing.T) {
			res := e.Complete(req(t, c.buf))
			assert.Empty(t, res.Silent, "expected an answer")
			assert.Equal(t, c.want, texts(res))
			assert.Equal(t, c.match, res.Match, "match state")
			if c.exact == "" {
				assert.Equal(t, -1, res.Exact)
				return
			}
			it, ok := res.ExactItem()
			require.True(t, ok)
			assert.Equal(t, c.exact, it.Text)
		})
	}
}

// Every path that offers nothing says why (§SD1).
func TestCompleteIsSilentWithAReason(t *testing.T) {
	e := testEngine(t)
	cases := []struct{ buf, contains string }{
		{`SELECT LW_GET('symbol', '|`, "waiting for the endpoint"},
		{`SELECT nosuchfn('|`, "no signature"},
		{`SELECT gloss(x, 'gloss/nope', '|`, "no gloss key is available"},
		{`SELECT LW_COMPONENT('SysMem'), |`, "nothing here answers expression"},
		{`SELECT CAST(x AS |`, "keyword-syntax call"},
		{`SELECT tupleElement(nosuch, '|`, "nothing here can type"},
		{`SELECT tupleElement(42, '|`, "has no named elements"},
		{`SELECT tupleElement(|`, "nothing here answers expression"},
		{`SELECT m.|`, "needs the statement's scope"},
		{`SELECT LW_COMPONENT('SysMem').|`, "not accepted by this build's pipeline yet"},
		{`SELECT a FROM |`, "nothing here answers table"},
		{`SELECT | FROM t`, "nothing here answers expression"},
		{`SELECT x SETTINGS |`, "nothing here answers setting"},
	}
	for _, c := range cases {
		t.Run(c.buf, func(t *testing.T) {
			res := e.Complete(req(t, c.buf))
			assert.Empty(t, res.Items)
			assert.Containsf(t, res.Silent, c.contains, "silence reason was %q", res.Silent)
		})
	}
}

// `tupleElement(m, '…')` on an argument the engine cannot type is silent, and
// stays silent — never an empty list that reads as "this tuple has no fields".
func TestUnknownTypeIsNotAnEmptyTuple(t *testing.T) {
	e := testEngine(t)
	res := e.Complete(req(t, `SELECT tupleElement(m, '|`))
	assert.Empty(t, res.Items)
	assert.Contains(t, res.Silent, "nothing here can type")
	assert.Equal(t, sqlvocab.DomainElementOf, res.Domain.Kind)
}

// SD11's gate: once the pipeline accepts the dot form, the call receiver is
// offered, and it is the same list tupleElement gives.
func TestNamedTupleAccessGate(t *testing.T) {
	e := testEngine(t)
	e.NamedTupleAccess = true
	res := e.Complete(req(t, `SELECT LW_COMPONENT('SysMem').Tot|`))
	require.Empty(t, res.Silent)
	assert.Equal(t, []string{"Id", "Ts", "TotalBytes", "TotalPercent", "FreeBytes"}, texts(res))
	assert.Equal(t, sqlcomplete.MatchPrefix, res.Match)
	assert.Equal(t, []int{2, 3}, res.Prefix, "TotalBytes and TotalPercent extend Tot")
}

// The Insert spelling carries quotes exactly when the position takes a literal
// and none has been opened.
func TestInsertSpelling(t *testing.T) {
	e := testEngine(t)
	inLit := e.Complete(req(t, `SELECT LW_COMPONENT('|`))
	require.NotEmpty(t, inLit.Items)
	assert.Equal(t, "SysCPU", inLit.Items[0].Insert)

	noQuote := e.Complete(req(t, `SELECT LW_COMPONENT(|`))
	require.NotEmpty(t, noQuote.Items)
	assert.Equal(t, "'SysCPU'", noQuote.Items[0].Insert)
}

// The elements carry their types, which is what the pane's type column shows
// and what tells a field from a field of the same name in another kind.
func TestElementsCarryTheirTypes(t *testing.T) {
	e := testEngine(t)
	res := e.Complete(req(t, `SELECT tupleElement(LW_COMPONENT('SysMem'), '|`))
	require.Len(t, res.Items, 5)
	assert.Equal(t, "Ts", res.Items[1].Text)
	assert.Equal(t, "DateTime64(9, 'UTC')", res.Items[1].Type)
	assert.Equal(t, sqlcomplete.ItemField, res.Items[1].Kind)
}

func TestCalleeAndOrdinalTravelWithTheResult(t *testing.T) {
	e := testEngine(t)
	res := e.Complete(req(t, `SELECT LW_GET('symbol', 'm', '|`))
	assert.Equal(t, "LW_GET", res.Callee)
	assert.Equal(t, 2, res.Ordinal)
}

// A curated built-in resolves without any roster declaring it.
func TestBuiltinSignaturesAnswer(t *testing.T) {
	e := testEngine(t)
	e.Providers.Catalog.TimeZones = func() ([]sqlcomplete.Item, bool) {
		return items("UTC", "Europe/Zurich"), true
	}
	res := e.Complete(req(t, `SELECT toDateTime(ts, 'UT|`))
	require.Empty(t, res.Silent)
	assert.Equal(t, []string{"UTC", "Europe/Zurich"}, texts(res))
	assert.Equal(t, sqlcomplete.MatchPrefix, res.Match)
}

// Every roster parameter with a closed domain must be answerable at its own
// ordinal — otherwise a declared domain is one no position can reach.
func TestEveryDeclaredOrdinalReaches(t *testing.T) {
	e := testEngine(t)
	for _, f := range e.Vocab.All() {
		for i, p := range f.Params {
			if p.Domain.Kind == sqlvocab.DomainExpr {
				continue
			}
			t.Run(f.Name+"/"+p.Name, func(t *testing.T) {
				args := make([]string, i+1)
				for j := 0; j < i; j++ {
					args[j] = fixtureArg(f.Params[j])
				}
				args[i] = "'"
				buf := "SELECT " + f.Name + "(" + strings.Join(args, ", ") + "|"
				res := e.Complete(req(t, buf))
				assert.Equalf(t, p.Domain.Kind, res.Domain.Kind, "%s reached the wrong domain", buf)
			})
		}
	}
}

// fixtureArg writes a plausible value for an earlier argument, so a
// ref-dependent domain further along has something to read.
func fixtureArg(p sqlvocab.Param) string {
	switch p.Domain.Kind {
	case sqlvocab.DomainComponentKind:
		return "'SysMem'"
	case sqlvocab.DomainGloss:
		return "'gloss/bytes'"
	case sqlvocab.DomainSection:
		return "'symbol'"
	case sqlvocab.DomainExpr:
		return "x"
	}
	return "'v'"
}

// Off-caret validation (§SD9): a complete literal in a closed in-process
// domain is reported resolved or not, and the token under the caret is never
// reported at all.
func TestValidate(t *testing.T) {
	e := testEngine(t)

	t.Run("a wrong kind and a right one", func(t *testing.T) {
		stmt := `SELECT LW_COMPONENT('SysMem'), LW_COMPONENT('Nonesuch')`
		got := e.Validate(stmt, nil, -1)
		require.Len(t, got, 2)
		assert.Equal(t, "SysMem", got[0].Text)
		assert.True(t, got[0].Resolved)
		assert.Equal(t, "Nonesuch", got[1].Text)
		assert.False(t, got[1].Resolved)
		assert.Equal(t, "LW_COMPONENT", got[1].Callee)
		assert.Equal(t, stmt[got[1].Range.Start:got[1].Range.Stop], "Nonesuch")
	})

	t.Run("a field of the sibling's kind", func(t *testing.T) {
		got := e.Validate(`SELECT tupleElement(LW_COMPONENT('SysMem'), 'Nope')`, nil, -1)
		require.Len(t, got, 2)
		assert.True(t, got[0].Resolved, "the kind itself")
		assert.False(t, got[1].Resolved, "SysMem has no field called Nope")
	})

	t.Run("the caret's own token is never reported", func(t *testing.T) {
		stmt := `SELECT LW_COMPONENT('Nonesuch')`
		inside := len(`SELECT LW_COMPONENT('Non`)
		assert.Empty(t, e.Validate(stmt, nil, inside))
		assert.Len(t, e.Validate(stmt, nil, 0), 1, "with the caret elsewhere it is reported")
	})

	t.Run("a domain that is not closed in process is left alone", func(t *testing.T) {
		// The time zone is an endpoint catalogue; a literal marked wrong
		// because the listing has not landed would be a false accusation.
		assert.Empty(t, e.Validate(`SELECT toDateTime(ts, 'Nonesuch/Zone')`, nil, -1))
	})

	t.Run("an unrelated literal is left alone", func(t *testing.T) {
		assert.Empty(t, e.Validate(`SELECT 'hello', concat('a', 'b')`, nil, -1))
	})

	t.Run("nothing to say about an empty statement", func(t *testing.T) {
		assert.Empty(t, e.Validate("", nil, -1))
	})
}

// Tab is shell-style: the unique match's suffix, or the longest common prefix
// of several, and nothing when they agree on no more (§SD10).
func TestTabCompletion(t *testing.T) {
	e := testEngine(t)

	t.Run("a unique match completes", func(t *testing.T) {
		res := e.Complete(req(t, `SELECT LW_COMPONENT('SysM|`))
		suffix, ok := res.TabCompletion("SysM")
		require.True(t, ok)
		assert.Equal(t, "em", suffix)
	})

	t.Run("several extend to what they agree on", func(t *testing.T) {
		res := e.Complete(req(t, `SELECT LW_COMPONENT('S|`))
		suffix, ok := res.TabCompletion("S")
		require.True(t, ok)
		assert.Equal(t, "ys", suffix, "SysCPU and SysMem agree on Sys")
	})

	t.Run("nothing more to agree on", func(t *testing.T) {
		res := e.Complete(req(t, `SELECT LW_COMPONENT('Sys|`))
		_, ok := res.TabCompletion("Sys")
		assert.False(t, ok)
	})

	t.Run("a fully typed candidate adds nothing", func(t *testing.T) {
		res := e.Complete(req(t, `SELECT LW_COMPONENT('SysMem|`))
		_, ok := res.TabCompletion("SysMem")
		assert.False(t, ok)
	})

	t.Run("no candidates at all", func(t *testing.T) {
		res := e.Complete(req(t, `SELECT nosuchfn('a|`))
		_, ok := res.TabCompletion("a")
		assert.False(t, ok)
	})

	t.Run("a field list completes the same way", func(t *testing.T) {
		res := e.Complete(req(t, `SELECT tupleElement(LW_COMPONENT('SysMem'), 'Tot|`))
		suffix, ok := res.TabCompletion("Tot")
		require.True(t, ok)
		assert.Equal(t, "al", suffix, "TotalBytes and TotalPercent agree on Total")
	})
}
