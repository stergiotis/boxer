package lwsql

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// auditFixtureNames mints a small two-channel section: a scalar value lane,
// an array value lane with its len support, and a repeating low-card-ref
// membership with its card lane.
func auditFixtureNames(t *testing.T) (v1, va, lenLane, lr, lrcard string) {
	t.Helper()
	c, err := NewComposer(DefaultTableSegments())
	require.NoError(t, err)
	v1, err = c.TaggedValueColumn("sym", "v-one", "s", nil)
	require.NoError(t, err)
	va, err = c.TaggedValueColumn("sym", "v-arr", "u64h", nil)
	require.NoError(t, err)
	lenLane, err = c.SupportColumn("sym", "len")
	require.NoError(t, err)
	lr, err = c.MembershipColumn("sym", "low-card-ref")
	require.NoError(t, err)
	lrcard, err = c.SupportColumn("sym", "lrcard")
	require.NoError(t, err)
	return
}

func TestAuditQueries_Shapes(t *testing.T) {
	v1, va, lenLane, lr, lrcard := auditFixtureNames(t)
	names := []string{"id:mycol:u64:::0:", v1, va, lenLane, lr, lrcard}
	queries, err := AuditQueries("t", names)
	require.NoError(t, err)

	byName := make(map[string]string, len(queries))
	for _, q := range queries {
		byName[q.Name] = q.SQL
	}
	require.Contains(t, byName, "sym: co-length")
	require.Contains(t, byName, "sym: ragged-sum")
	require.Contains(t, byName, "sym: positivity")

	// I-lanes: scalar value, len, lrcard — the flattened lanes must not
	// appear in the co-length equality.
	co := byName["sym: co-length"]
	require.Contains(t, co, `length("`+v1+`")`)
	require.Contains(t, co, `length("`+lenLane+`")`)
	require.NotContains(t, co, `length("`+va+`")`)
	require.NotContains(t, co, `length("`+lr+`")`)

	rs := byName["sym: ragged-sum"]
	require.Contains(t, rs, `arraySum("`+lenLane+`") = length("`+va+`")`)
	require.Contains(t, rs, `arraySum("`+lrcard+`") = length("`+lr+`")`)

	// The plain column is not audited.
	for _, q := range queries {
		require.NotContains(t, q.SQL, "id:mycol")
	}
}

// TestAuditQueries_NonRepeatingMembershipIsAnInstanceLane pins the fast-path
// dual: without a `<role>card` lane the membership lane sits on the instance
// axis and joins the co-length equality instead of the ragged sums.
func TestAuditQueries_NonRepeatingMembershipIsAnInstanceLane(t *testing.T) {
	v1, _, _, lr, _ := auditFixtureNames(t)
	queries, err := AuditQueries("t", []string{v1, lr})
	require.NoError(t, err)
	require.Len(t, queries, 1)
	require.Equal(t, "sym: co-length", queries[0].Name)
	require.Contains(t, queries[0].SQL, `length("`+lr+`")`)
}

// TestAuditQueries_UnderscoreSeparator: a dumped ('_'-separated) table's
// names classify through the same sniff the resolver uses.
func TestAuditQueries_UnderscoreSeparator(t *testing.T) {
	seg := DefaultTableSegments()
	seg.Separator = "_"
	c, err := NewComposer(seg)
	require.NoError(t, err)
	v, err := c.TaggedValueColumn("sym", "v", "s", nil)
	require.NoError(t, err)
	m, err := c.MembershipColumn("sym", "low-card-ref")
	require.NoError(t, err)
	queries, err := AuditQueries("t", []string{v, m})
	require.NoError(t, err)
	require.Len(t, queries, 1)
	require.Contains(t, queries[0].SQL, `length("`+m+`")`)
}

// runAuditLocal executes DDL+INSERT+audit statements through
// clickhouse-local, returning one output line per audit query.
func runAuditLocal(t *testing.T, script string) []string {
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
		t.Fatalf("clickhouse-local failed: %v\nstderr:\n%s\nscript:\n%s", err, stderr.String(), script)
	}
	return strings.Fields(stdout.String())
}

func auditScript(t *testing.T, names []string, rows string) string {
	t.Helper()
	cols := make([]string, 0, len(names))
	for _, n := range names {
		typ := "Array(UInt64)"
		if strings.HasPrefix(n, "tv:sym:v-one:") {
			typ = "Array(String)"
		}
		cols = append(cols, `"`+n+`" `+typ)
	}
	queries, err := AuditQueries("t", names)
	require.NoError(t, err)
	var b strings.Builder
	b.WriteString("CREATE TABLE t (" + strings.Join(cols, ", ") + ") ENGINE = Memory;\n")
	b.WriteString("INSERT INTO t VALUES " + rows + ";\n")
	for _, q := range queries {
		b.WriteString(q.SQL + ";\n")
	}
	return b.String()
}

// TestAuditQueries_CleanFixtureIsGreen: a conforming row yields zero
// violations from every audit query (clickhouse-local).
func TestAuditQueries_CleanFixtureIsGreen(t *testing.T) {
	v1, va, lenLane, lr, lrcard := auditFixtureNames(t)
	names := []string{v1, va, lenLane, lr, lrcard}
	// Two instances; array lengths [2,1] → 3 flattened values; membership
	// cards [1,2] → 3 flattened membership ids.
	rows := "(['a','b'], [1,2,3], [2,1], [10,20,21], [1,2])"
	out := runAuditLocal(t, auditScript(t, names, rows))
	require.NotEmpty(t, out)
	for _, v := range out {
		require.Equal(t, "0", v, "audit must be green on a conforming fixture")
	}
}

// TestAuditQueries_CorruptionsGoRed: each invariant class trips on its own
// corruption (clickhouse-local).
func TestAuditQueries_CorruptionsGoRed(t *testing.T) {
	v1, va, lenLane, lr, lrcard := auditFixtureNames(t)
	names := []string{v1, va, lenLane, lr, lrcard}
	queries, err := AuditQueries("t", names)
	require.NoError(t, err)

	cases := []struct {
		name string
		rows string
		red  string // audit query name that must report a violation
	}{
		{"co-length", "(['a'], [1,2,3], [2,1], [10,20,21], [1,2])", "sym: co-length"},
		{"ragged-sum", "(['a','b'], [1,2], [2,1], [10,20,21], [1,2])", "sym: ragged-sum"},
		{"positivity", "(['a','b'], [1,2,3], [0,3], [10,20,21], [1,2])", "sym: positivity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runAuditLocal(t, auditScript(t, names, tc.rows))
			require.Len(t, out, len(queries))
			for i, q := range queries {
				if q.Name == tc.red {
					require.NotEqual(t, "0", out[i], "corruption must trip %s", q.Name)
				}
			}
		})
	}
}
