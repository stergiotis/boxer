package lwextract_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

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
	bin, err := exec.LookPath("clickhouse-local")
	if err != nil {
		t.Skipf("clickhouse-local not on PATH, skipping: %v", err)
	}
	cmd := exec.Command(bin, "--multiquery", "--output-format", "TSV")
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("clickhouse-local failed: %v\nstderr:\n%s", err, stderr.String())
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
