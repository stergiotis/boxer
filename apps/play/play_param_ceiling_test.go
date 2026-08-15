package play

// The security ceiling a SQL-valued knob is judged against (ADR-0187
// §SD5, milestone M4): what a substitution may turn a classified
// query into, and what happens when it goes further than the document declared.

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stretchr/testify/require"
)

// The direction of the comparison is the whole rule, so it is pinned on its
// own: numerically smaller is stronger, so a refusal is substituted < ceiling.
func TestExprCeilingDirection(t *testing.T) {
	require.Less(t, analysis.QuerySecurityMutating, analysis.QuerySecurityReadEgress)
	require.Less(t, analysis.QuerySecurityReadEgress, analysis.QuerySecurityRead)
	require.Equal(t, analysis.QuerySecurityMutating, analysis.QuerySecurityClassE(0),
		"the zero value is the strongest class, so a zero ceiling refuses nothing")
}

func TestExprCeilingRefusal(t *testing.T) {
	const sql = "SELECT number FROM numbers(10) WHERE {cond:Expr}"
	cases := []struct {
		name    string
		sql     string
		values  map[string]string
		ceiling analysis.QuerySecurityClassE
		refuse  bool
		says    string
	}{
		{
			"no ceiling refuses nothing — this is play",
			sql, map[string]string{"cond": "number IN (SELECT h FROM url('http://x/y', 'CSV'))"},
			analysis.QuerySecurityMutating, false, "",
		},
		{
			"an ordinary predicate passes a read ceiling",
			sql, map[string]string{"cond": "number % 2 = 0"},
			analysis.QuerySecurityRead, false, "",
		},
		// The case this exists for: a frozen read-class applet, and a knob that
		// reaches outside the endpoint it promised.
		{
			"egress under a read ceiling is refused, and the knob is named",
			sql, map[string]string{"cond": "number IN (SELECT h FROM url('http://x/y', 'CSV'))"},
			analysis.QuerySecurityRead, true, "{cond}",
		},
		{
			"a remote read is egress too",
			sql, map[string]string{"cond": "number IN (SELECT n FROM remote('h:9000', d.t))"},
			analysis.QuerySecurityRead, true, "{cond}",
		},
		// A read-egress applet already reaches outside, so the same splice is
		// within what it declared.
		{
			"the same splice passes a read-egress ceiling",
			sql, map[string]string{"cond": "number IN (SELECT h FROM url('http://x/y', 'CSV'))"},
			analysis.QuerySecurityReadEgress, false, "",
		},
		{
			"nothing substituted is nothing to re-judge",
			"SELECT number FROM numbers(10)", map[string]string{"cond": "url('http://x/y', 'CSV')"},
			analysis.QuerySecurityRead, false, "",
		},
		// Conservative direction: a body nobody can classify must not run
		// against an applet that promised a class.
		{
			"an unparseable substitution is refused",
			sql, map[string]string{"cond": "))) not sql ((("},
			analysis.QuerySecurityRead, true, "refusing",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason := exprCeilingRefusal(tc.sql, tc.values, tc.ceiling)
			if !tc.refuse {
				require.Equal(t, "", reason)
				return
			}
			require.NotEqual(t, "", reason)
			require.Contains(t, reason, tc.says)
		})
	}
}

// The refusal names the knob that raised the class, which is what makes it
// actionable — "this applet is read-class" tells a reader nothing to do.
func TestExprCeilingRefusalNamesTheKnobAndTheWitness(t *testing.T) {
	reason := exprCeilingRefusal(
		"SELECT number FROM numbers(10) WHERE {cond:Expr}",
		map[string]string{"cond": "number IN (SELECT h FROM url('http://x/y', 'CSV'))"},
		analysis.QuerySecurityRead)
	require.Contains(t, reason, "{cond}")
	require.Contains(t, reason, "read-egress", "the class it would reach")
	require.Contains(t, reason, "url", "the witness that raised it")
	require.Contains(t, reason, "read", "and the class the applet declared")
}

// A witness the DOCUMENT carries is not the knob's doing, so the refusal
// reports it without blaming a field for it.
func TestExprCeilingRefusalDoesNotBlameAnInnocentKnob(t *testing.T) {
	reason := exprCeilingRefusal(
		"SELECT h FROM url('http://x/y', 'CSV') WHERE {cond:Expr}",
		map[string]string{"cond": "h != ''"},
		analysis.QuerySecurityRead)
	require.NotEqual(t, "", reason, "the body is egress, so a read ceiling still refuses")
	require.NotContains(t, reason, "{cond}", "but the knob did not put it there")
}

// The gate, through the app: an applet refuses the run and says why, and the
// same buffer in play runs.
func TestRunGateEnforcesTheCeiling(t *testing.T) {
	const sql = "-- play: expr cond = number IN (SELECT h FROM url('http://x/y', 'CSV'))\n" +
		"SELECT number FROM numbers(10) WHERE {cond:Expr}"

	applet := paneApp(t, sql)
	applet.client = NewClient(ClientConfig{URL: "http://localhost:8123/"}, nil)
	applet.SetSecurityCeiling(analysis.QuerySecurityRead)
	require.Contains(t, applet.exprCeilingRefusal(sql), "{cond}")

	// play sets no ceiling, so the same knob is reported and not enforced —
	// its editor already accepts arbitrary SQL.
	playground := paneApp(t, sql)
	playground.client = NewClient(ClientConfig{URL: "http://localhost:8123/"}, nil)
	require.Equal(t, "", playground.exprCeilingRefusal(sql))
}

// The Diagnostics class describes what runs. Without the substitution it would
// describe a template nobody executes.
func TestDiagnosticsClassifiesTheSubstitutedBody(t *testing.T) {
	const sql = "-- play: expr cond = number IN (SELECT h FROM url('http://x/y', 'CSV'))\n" +
		"SELECT number FROM numbers(10) WHERE {cond:Expr}"

	plain := &DiagnosticsDriver{}
	plain.armSecurityContext(sql, nil)
	require.True(t, plain.secKnown)
	require.Equal(t, analysis.QuerySecurityRead, plain.secClass,
		"unsubstituted, the placeholder hides the egress")

	cl := NewClient(ClientConfig{URL: "http://localhost:8123/"}, nil)
	subst := &DiagnosticsDriver{substitute: cl.ExprSubstituted}
	subst.armSecurityContext(sql, nil)
	require.True(t, subst.secKnown)
	require.Equal(t, analysis.QuerySecurityReadEgress, subst.secClass,
		"substituted, the badge describes the query that runs")
}
