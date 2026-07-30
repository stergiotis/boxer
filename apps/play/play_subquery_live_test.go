//go:build integration

package play

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// play_subquery_live_test.go — the differential net under the hoist rules.
//
// The reparse test beside these (TestSubqueryCompositionReparses) proves a
// narrowed composition is SQL; it cannot prove it is the same query. The
// hoist rules encode facts about ClickHouse's CTE scoping — siblings bind by
// name regardless of order, an inner rebinding answers on the flat list,
// named queries and scalar aliases live in separate namespaces — and those
// are claims about the connected analyzer, not about the grammar. So this
// lane runs every composition next to its original statement and holds the
// pair to one contract:
//
//   - a unit reporting nothing Unresolved must compose a query the server
//     accepts, returning exactly what the original returns — each case's
//     outer query is a passthrough of the narrowed unit, so the two agree by
//     construction when the hoist preserved the unit's environment;
//   - a unit the server would reject must have said so first, in Unresolved.
//     Endpoint failures are allowed only where the editor marked them.

// chQueryTSV runs one statement over the ClickHouse HTTP interface and
// returns its TSV result with the trailing newline trimmed; a non-200
// response comes back as an error carrying the server's message.
func chQueryTSV(t *testing.T, url, sql string) (result string, err error) {
	t.Helper()
	resp, err := http.Post(url, "text/plain", strings.NewReader(sql))
	if err != nil {
		return
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	if resp.StatusCode != http.StatusOK {
		err = errors.New(strings.TrimSpace(string(raw)))
		return
	}
	result = strings.TrimRight(string(raw), "\n")
	return
}

func TestLiveSubqueryCompositionDifferential(t *testing.T) {
	url := liveClickHouseURL(t)
	cases := []struct {
		name   string
		marked string
	}{{
		name:   "an inner rebinding answers on the flat list",
		marked: "WITH t AS (SELECT 1 AS v), u AS (SELECT v FROM t) SELECT * FROM (WITH t AS (SELECT 2 AS v) SELECT |* FROM u)",
	}, {
		name:   "an inner rebinding, one level further down",
		marked: "WITH t AS (SELECT 1 AS v), u AS (SELECT v FROM t) SELECT * FROM (WITH t AS (SELECT 2 AS v) SELECT * FROM (SELECT |* FROM u))",
	}, {
		name:   "a body referencing a sibling defined after it",
		marked: "WITH a AS (SELECT * FROM |b), b AS (SELECT 1 AS one) SELECT * FROM a",
	}, {
		name:   "a scalar alias the unit rebinds itself",
		marked: "WITH 7 AS k SELECT * FROM (WITH 8 AS k SELECT |k)",
	}, {
		name:   "a named query and a scalar alias sharing a name",
		marked: "WITH t AS (SELECT 1 AS v) SELECT * FROM (WITH 7 AS t SELECT |t FROM t)",
	}, {
		name:   "a non-recursive rebinding reaches the outer name",
		marked: "WITH t AS (SELECT 1 AS v) SELECT * FROM (WITH t AS (SELECT v+1 AS v FROM |t) SELECT * FROM t)",
	}, {
		name:   "a recursive CTE referenced from outside its body",
		marked: "WITH RECURSIVE r AS (SELECT 1 AS n UNION ALL SELECT n+1 FROM r WHERE n < 3) SELECT * FROM (SELECT max(n) AS m FROM |r)",
	}, {
		name:   "two WITH levels flatten into one list",
		marked: "WITH a AS (SELECT 1 AS x) SELECT * FROM (WITH b AS (SELECT 2 AS y) SELECT * FROM (SELECT |* FROM a, b))",
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, caret := caretAt(t, tc.marked)
			unit, ok := pickSubquery(parseSubqueryUnits(text), caret)
			if !ok || unit.Root {
				t.Fatal("case does not narrow — it is not exercising the hoist")
			}
			if len(unit.Unresolved) > 0 {
				t.Fatalf("case unexpectedly marked unresolved: %v", unit.Unresolved)
			}
			want, err := chQueryTSV(t, url, text)
			if err != nil {
				t.Fatalf("the original statement does not run — the case is broken: %v", err)
			}
			composed := unit.compose(text)
			got, err := chQueryTSV(t, url, composed)
			if err != nil {
				t.Fatalf("the server rejected an unmarked composition:\n  %v\n  composed %q", err, composed)
			}
			if got != want {
				t.Errorf("the composition is a different query:\n got  %q\n want %q\n from %q", got, want, composed)
			}
		})
	}
}

// A correlated reference whose qualifier is rebound by a NESTED subquery of
// the unit: the nested bind does not enclose the reference, so the original
// resolves it against the OUTER alias and runs, while the narrowed unit is
// UNKNOWN_IDENTIFIER. The flat suppression missed exactly this shape (the
// review's scope-blindness finding) — the contract is that the editor marks
// it before the server rejects it. The rejection is asserted as a tripwire,
// like the self-reference test below.
func TestLiveSubqueryCorrelationIsMarkedBeforeTheServerRejects(t *testing.T) {
	url := liveClickHouseURL(t)
	const marked = "SELECT (SELECT max(z) FROM (SELECT number AS z FROM numbers(5) a) WHERE |a.k > 0) FROM (SELECT 3 AS k) a"
	text, caret := caretAt(t, marked)
	unit, ok := pickSubquery(parseSubqueryUnits(text), caret)
	if !ok || unit.Root {
		t.Fatal("case does not narrow")
	}
	if got, err := chQueryTSV(t, url, text); err != nil {
		t.Fatalf("the original statement does not run — the case is broken: %v", err)
	} else if got != "4" {
		t.Fatalf("original = %q, want 4 — the reference must bind the OUTER alias", got)
	}
	if len(unit.Unresolved) == 0 {
		t.Fatal("the correlation went unmarked — the narrowed run would fail at the endpoint with no warning")
	}
	if _, err := chQueryTSV(t, url, unit.compose(text)); err == nil {
		t.Error("the composition now runs — the correlation mark may be too strict for this server")
	}
}

// A recursive body's self-reference is the failure composition cannot
// repair, and the contract is that the editor says so before the server
// does. The server-side rejection is asserted too — not as contract, but as
// a tripwire: an analyzer that learns to accept the shape fails this test
// and flags the detection for relaxing.
func TestLiveSubquerySelfReferenceIsMarkedBeforeTheServerRejects(t *testing.T) {
	url := liveClickHouseURL(t)
	const marked = "WITH RECURSIVE r AS (SELECT 1 AS n UNION ALL SELECT n+1 FROM |r WHERE n < 3) SELECT max(n) AS m FROM r"
	text, caret := caretAt(t, marked)
	unit, ok := pickSubquery(parseSubqueryUnits(text), caret)
	if !ok || unit.Root {
		t.Fatal("case does not narrow")
	}
	if _, err := chQueryTSV(t, url, text); err != nil {
		t.Fatalf("the original statement does not run — the case is broken: %v", err)
	}
	if len(unit.Unresolved) == 0 {
		t.Fatal("the self-reference went unmarked — the narrowed run would fail at the endpoint with no warning")
	}
	if _, err := chQueryTSV(t, url, unit.compose(text)); err == nil {
		t.Error("the composition now runs — the self-reference mark may be too strict for this server")
	}
}
