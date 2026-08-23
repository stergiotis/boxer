package lwextract_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwextract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/readback"
)

var general = lwextract.Lanes{Value: "val", Ident: "lr", Card: "lrcard", Length: "len"}
var oneMembership = lwextract.Lanes{Value: "val", Ident: "lr", Length: "len"}

// TestValueForms pins the four renderings apart. The general forms are the
// strings the read-back generator emitted before this package existed, and
// its goldens pin that they still are; what is pinned here is which shape
// gets which form.
func TestValueForms(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  lwextract.Request
		want string
	}{
		{
			name: "scalar, general",
			req:  lwextract.Request{Lanes: general, Shape: lwextract.ShapeScalar, Membership: "42"},
			want: "LW_VALUE_BY_TAG_EQUAL(val, lr, 42, LW_RAGGED_PARENT_IDS(lrcard))",
		},
		{
			name: "scalar, one membership per attribute",
			req:  lwextract.Request{Lanes: oneMembership, Shape: lwextract.ShapeScalar, Membership: "42"},
			want: "val[indexOf(lr, 42)]",
		},
		{
			name: "list, general",
			req:  lwextract.Request{Lanes: general, Shape: lwextract.ShapeList, Membership: "42"},
			want: "LW_LIST_BY_TAG_EQUAL(val, len, lr, 42, LW_RAGGED_PARENT_IDS(lrcard))",
		},
		{
			name: "list, one membership per attribute",
			req:  lwextract.Request{Lanes: oneMembership, Shape: lwextract.ShapeList, Membership: "42"},
			want: "arraySlice(val, LW_LU_VAL_IDX_TO_MEMB_IDX_BEGIN_INCL(len)[indexOf(lr, 42)], len[indexOf(lr, 42)])",
		},
		{
			name: "unit indexes the located list",
			req:  lwextract.Request{Lanes: general, Shape: lwextract.ShapeList, Membership: "42", Unit: true},
			want: "LW_LIST_BY_TAG_EQUAL(val, len, lr, 42, LW_RAGGED_PARENT_IDS(lrcard))[1]",
		},
		{
			name: "unit is a no-op on a scalar",
			req:  lwextract.Request{Lanes: general, Shape: lwextract.ShapeScalar, Membership: "42", Unit: true},
			want: "LW_VALUE_BY_TAG_EQUAL(val, lr, 42, LW_RAGGED_PARENT_IDS(lrcard))",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := lwextract.Value(tc.req)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestValueRejectsIncompleteRequests covers the lanes a caller can forget.
// A list without its element-count lane is the dangerous one: it would
// otherwise render a call with a missing argument that fails at the server,
// far from the code that built it.
func TestValueRejectsIncompleteRequests(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  lwextract.Request
	}{
		{"no value lane", lwextract.Request{Lanes: lwextract.Lanes{Ident: "lr"}, Shape: lwextract.ShapeScalar, Membership: "1"}},
		{"no identity lane", lwextract.Request{Lanes: lwextract.Lanes{Value: "val"}, Shape: lwextract.ShapeScalar, Membership: "1"}},
		{"no membership", lwextract.Request{Lanes: general, Shape: lwextract.ShapeScalar}},
		{"list without length", lwextract.Request{Lanes: lwextract.Lanes{Value: "val", Ident: "lr"}, Shape: lwextract.ShapeList, Membership: "1"}},
		{"no shape", lwextract.Request{Lanes: general, Membership: "1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := lwextract.Value(tc.req)
			require.Error(t, err)
		})
	}
}

// TestPredicates pins the two builtins-only forms. They are what makes the
// WHERE side work with no UDF installed, so they must not acquire a helper
// call by accident.
func TestPredicates(t *testing.T) {
	require.Equal(t, "has(lr, 42)", lwextract.Present(general, "42"))
	require.Equal(t, "countEqual(lr, 42)", lwextract.CountEqual(general, "42"))
	require.Equal(t, "if(has(lr, 42), x, NULL)", lwextract.NullWhenAbsent("x", general, "42"))
	for _, expr := range []string{
		lwextract.Present(general, "42"),
		lwextract.CountEqual(general, "42"),
	} {
		require.NotContains(t, expr, "LW_", "the predicate forms must stay builtins-only")
	}
}

func runClickHouseLocal(t *testing.T, script string) string {
	t.Helper()
	cmd, err := extbin.ClickHouseLocal.Command(t.Context(), extbin.Opts{}, "--multiquery", "--output-format", "TSV")
	if err != nil {
		t.Skipf("clickhouse not on PATH, skipping: %v", err)
	}
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("clickhouse local failed: %v\nstderr:\n%s", err, stderr.String())
	}
	return stdout.String()
}

// TestFastPathEqualsGeneralForm is the claim the fast path rests on
// (ADR-0181 §SD3, closing ADR-0066's open item): when every attribute
// carries exactly one membership, LW_RAGGED_PARENT_IDS is the identity
// permutation, so dropping it changes nothing.
//
// Proved by running both forms over the same fixture rather than by
// arguing about the arithmetic — which is how the inherited BEGIN_INCL bug
// was found in the first place. The all-ones cardinality column is the
// shape a schema WITHOUT a cardinality column states structurally; the
// column exists in the fixture only so the general form has something to
// take.
func TestFastPathEqualsGeneralForm(t *testing.T) {
	scalarGeneral, err := lwextract.Value(lwextract.Request{Lanes: general, Shape: lwextract.ShapeScalar, Membership: "tag"})
	require.NoError(t, err)
	scalarFast, err := lwextract.Value(lwextract.Request{Lanes: oneMembership, Shape: lwextract.ShapeScalar, Membership: "tag"})
	require.NoError(t, err)
	listGeneral, err := lwextract.Value(lwextract.Request{Lanes: general, Shape: lwextract.ShapeList, Membership: "tag"})
	require.NoError(t, err)
	listFast, err := lwextract.Value(lwextract.Request{Lanes: oneMembership, Shape: lwextract.ShapeList, Membership: "tag"})
	require.NoError(t, err)

	// Three attributes, one membership each; the list lane is flattened and
	// partitioned by len, including an empty attribute and a miss.
	const fixture = `
WITH
    [10, 20, 30]                AS val_scalar,
    [100, 200, 300, 400]        AS val_flat,
    [7, 8, 9]                   AS lr,
    [1, 1, 1]                   AS lrcard,
    [2, 0, 2]                   AS len
SELECT %s FROM (SELECT 1) `

	for _, tc := range []struct {
		name             string
		general, fast    string
		valueLane, cases string
	}{
		{name: "scalar", general: scalarGeneral, fast: scalarFast, valueLane: "val_scalar"},
		{name: "list", general: listGeneral, fast: listFast, valueLane: "val_flat"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var checks []string
			// Every membership in the lane, plus one that is absent.
			for _, tag := range []string{"7", "8", "9", "999"} {
				g := strings.ReplaceAll(strings.ReplaceAll(tc.general, "val", tc.valueLane), "tag", tag)
				f := strings.ReplaceAll(strings.ReplaceAll(tc.fast, "val", tc.valueLane), "tag", tag)
				checks = append(checks, "if(("+g+") = ("+f+"), '', 'tag "+tag+": '||toString("+g+")||' != '||toString("+f+"))")
			}
			script := readback.HelperUDFsSQL() + "\n" +
				strings.Replace(fixture, "%s", strings.Join(checks, ", "), 1)
			out := strings.TrimSpace(strings.ReplaceAll(runClickHouseLocal(t, script), "\t", ""))
			require.Empty(t, out, "the fast form disagrees with the general form")
		})
	}
}

// mixed is a mixed channel's lanes: the parameter lane rides beside the
// identity lane and is counted by the same cardinality column.
var mixed = lwextract.Lanes{Value: "val", Ident: "lmv", Card: "lmvcard", Length: "len", Param: "mvhp"}

// TestMixedSingularReadRequiresAParameter is the contract that separates the
// two questions. On a mixed channel the identity is shared by design — the
// parameter lane exists because several attributes carry the same one — so a
// singular read without a parameter would return an arbitrary member of that
// set and present it as the answer.
func TestMixedSingularReadRequiresAParameter(t *testing.T) {
	_, err := lwextract.Value(lwextract.Request{Lanes: mixed, Shape: lwextract.ShapeScalar, Membership: "'m'"})
	require.ErrorContains(t, err, "identity AND parameter")

	_, err = lwextract.Value(lwextract.Request{
		Lanes: mixed, Shape: lwextract.ShapeScalar, Membership: "'m'", Params: "''", ParamsGiven: true,
	})
	require.NoError(t, err, "the empty parameter is a real value, not an omission")

	// The plural question is well posed without one.
	_, err = lwextract.Selection(lwextract.Request{Lanes: mixed, Membership: "'m'"})
	require.NoError(t, err)

	// A parameter on a channel that has no parameter lane is a mistake about
	// the schema, and is refused rather than ignored.
	_, err = lwextract.Selection(lwextract.Request{Lanes: general, Membership: "'m'", Params: "'x'", ParamsGiven: true})
	require.ErrorContains(t, err, "no parameter lane")
}

// TestSelectorsAreCoIndexed pins the property the selector pair exists for:
// element k of one names the same occurrence as element k of the other, so
// the two pass as two arguments to one lambda.
func TestSelectorsAreCoIndexed(t *testing.T) {
	sel, err := lwextract.Selection(lwextract.Request{Lanes: general, Membership: "42"})
	require.NoError(t, err)
	attrs, err := lwextract.SelectionAttrs(lwextract.Request{Lanes: general, Membership: "42"})
	require.NoError(t, err)
	require.Contains(t, attrs, sel, "the attribute selector gathers the position selector, so the two cannot drift")
	require.Contains(t, attrs, "LW_CO_GATHER(LW_RAGGED_PARENT_IDS(lrcard)")

	// One membership per attribute makes the position the attribute index,
	// so the map is dropped rather than emitted as an identity permutation.
	fastAttrs, err := lwextract.SelectionAttrs(lwextract.Request{Lanes: oneMembership, Membership: "42"})
	require.NoError(t, err)
	require.NotContains(t, fastAttrs, "LW_CO_GATHER")
}

// TestMixedFormsAgainstClickHouse runs the mixed arm rather than arguing
// about it, the way TestFastPathEqualsGeneralForm does — the ragged fixture
// is what makes it a real test: attribute 2 carries two memberships, so a
// membership position is not an attribute index for anything after it.
//
// The oracle is hand-decoded from the SoA layout, not computed by a second
// expression from this package, so the check cannot agree with itself.
func TestMixedFormsAgainstClickHouse(t *testing.T) {
	// val:     ['a','b','c']                                (3 attributes)
	// lmv:     ['/t/_','/t/_','/a/_','/host']                (4 memberships)
	// mvhp:    ['0000','0001','0000','']
	// lmvcard: [1, 2, 1]  -> attribute 2 owns positions 2 and 3
	const fixture = `
WITH
    ['a', 'b', 'c']                             AS val,
    ['/t/_', '/t/_', '/a/_', '/host']           AS lmv,
    ['0000', '0001', '0000', '']                AS mvhp,
    [1, 2, 1]                                   AS lmvcard,
    [1, 1, 1]                                   AS len
SELECT %s FROM (SELECT 1) `

	scalar := mixed
	scalar.Length = ""

	type check struct {
		name string
		expr string
		want string
	}
	var checks []check
	add := func(name string, expr string, want string) {
		checks = append(checks, check{name: name, expr: expr, want: want})
	}

	for _, tc := range []struct{ params, want string }{
		{"'0000'", "a"}, // the pair on attribute 1
		{"'0001'", "b"}, // the SAME identity on attribute 2 — the whole point
		{"'9999'", ""},  // absent pair: the type default, never an error
	} {
		v, err := lwextract.Value(lwextract.Request{
			Lanes: scalar, Shape: lwextract.ShapeScalar, Membership: "'/t/_'", Params: tc.params, ParamsGiven: true,
		})
		require.NoError(t, err)
		add("value "+tc.params, v, tc.want)
	}

	// The guard must answer the PAIR, or a NULL form reports a hit on a row
	// carrying the identity under some other parameter.
	add("present hit", lwextract.PresentFor(lwextract.Request{
		Lanes: scalar, Membership: "'/t/_'", Params: "'0001'", ParamsGiven: true,
	}), "1")
	add("present miss", lwextract.PresentFor(lwextract.Request{
		Lanes: scalar, Membership: "'/t/_'", Params: "'9999'", ParamsGiven: true,
	}), "0")

	sel, err := lwextract.Selection(lwextract.Request{Lanes: mixed, Membership: "'/t/_'"})
	require.NoError(t, err)
	add("selection", sel, "[1,2]")

	attrs, err := lwextract.SelectionAttrs(lwextract.Request{Lanes: mixed, Membership: "'/t/_'"})
	require.NoError(t, err)
	add("selection attrs", attrs, "[1,2]")

	// Gathering both axes through the co-indexed pair is the composition the
	// selectors exist for.
	add("gather both", "arrayStringConcat(arrayMap((p, a) -> concat(mvhp[p], '=', val[a]), "+sel+", "+attrs+"), ' ')", "0000=a 0001=b")

	// A narrowed selection, and one that selects nothing.
	selPair, err := lwextract.Selection(lwextract.Request{Lanes: mixed, Membership: "'/t/_'", Params: "'0001'", ParamsGiven: true})
	require.NoError(t, err)
	add("selection pair", selPair, "[2]")
	selMiss, err := lwextract.Selection(lwextract.Request{Lanes: mixed, Membership: "'/nope'"})
	require.NoError(t, err)
	add("selection miss", selMiss, "[]")

	exprs := make([]string, 0, len(checks))
	for _, c := range checks {
		exprs = append(exprs, "toString("+c.expr+")")
	}
	script := readback.HelperUDFsSQL() + "\n" + strings.Replace(fixture, "%s", strings.Join(exprs, ", "), 1)
	out := runClickHouseLocal(t, script)
	got := strings.Split(strings.TrimRight(out, "\n"), "\t")
	require.Len(t, got, len(checks))
	for i, c := range checks {
		require.Equal(t, c.want, got[i], "%s: %s", c.name, c.expr)
	}
}
