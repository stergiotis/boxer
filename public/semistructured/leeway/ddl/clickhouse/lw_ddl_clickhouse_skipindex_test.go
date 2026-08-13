package clickhouse_test

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stretchr/testify/require"
)

func TestDeriveSkipIndexes_Policy(t *testing.T) {
	ir, _ := composeFixture(t)

	// Default policy: one bloom_filter per membership lane.
	specs, err := clickhouse.DeriveSkipIndexes(ir, clickhouse.DefaultSkipIndexPolicy())
	require.NoError(t, err)
	require.Len(t, specs, 1)
	require.Equal(t, common.ColumnRoleLowCardRef, specs[0].Ref.Role)
	require.Equal(t, "bloom_filter(0.01)", specs[0].Type)
	require.EqualValues(t, 4, specs[0].Granularity)

	// Value blooms add the scalar string value lane; set(N) doubles the
	// membership lane with a suffixed name.
	policy := clickhouse.DefaultSkipIndexPolicy()
	policy.ValueStringBloom = true
	policy.MembershipSet = 100
	specs, err = clickhouse.DeriveSkipIndexes(ir, policy)
	require.NoError(t, err)
	require.Len(t, specs, 3)
	var setName string
	kinds := make(map[string]int, 3)
	for _, s := range specs {
		kinds[s.Type]++
		if strings.HasPrefix(s.Type, "set(") {
			setName = s.Name
		}
	}
	require.Equal(t, map[string]int{"bloom_filter(0.01)": 2, "set(100)": 1}, kinds)
	require.True(t, strings.HasSuffix(setName, "_set"), "set index needs a distinct name, got %q", setName)

	// Zero fp rate falls back to the bare form.
	policy = clickhouse.SkipIndexPolicy{MembershipBloom: true}
	specs, err = clickhouse.DeriveSkipIndexes(ir, policy)
	require.NoError(t, err)
	require.Equal(t, "bloom_filter", specs[0].Type)
}

// TestDeriveSkipIndexes_ZeroMatchesIsEmpty: derivation is an optimisation —
// a schema with no matching lanes yields no specs and no error; interactive
// surfaces that want a loud no-match check the count (the CLI flag does).
func TestDeriveSkipIndexes_ZeroMatchesIsEmpty(t *testing.T) {
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	manip.SetTableName("plainonly")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&td, clickhouse.NewTechnologySpecificCodeGenerator()))

	specs, err := clickhouse.DeriveSkipIndexes(ir, clickhouse.DefaultSkipIndexPolicy())
	require.NoError(t, err)
	require.Empty(t, specs)
}

// TestComposeCreateTable_DuplicateIndexNameIsGenerationError: an explicit
// spec colliding with a derived one must fail at generation time, not as
// ClickHouse's ILLEGAL_INDEX at deploy.
func TestComposeCreateTable_DuplicateIndexNameIsGenerationError(t *testing.T) {
	ir, conv := composeFixture(t)
	policy := clickhouse.DefaultSkipIndexPolicy()
	_, err := clickhouse.ComposeCreateTable("tdup", ir, common.TableRowConfigMultiAttributesPerRow, conv, clickhouse.TableOptions{
		Engine: "MergeTree()",
		Indexes: []clickhouse.IndexSpec{{
			Ref:  clickhouse.ColumnRef{Section: "symbol", Role: common.ColumnRoleLowCardRef},
			Type: "minmax",
		}},
		SkipIndexes: &policy,
	})
	require.ErrorContains(t, err, "duplicate index name")
}

func TestComposeCreateTable_SkipIndexPolicy(t *testing.T) {
	ir, conv := composeFixture(t)
	policy := clickhouse.DefaultSkipIndexPolicy()
	sql, err := clickhouse.ComposeCreateTable("tidx", ir, common.TableRowConfigMultiAttributesPerRow, conv, clickhouse.TableOptions{
		Engine:      "MergeTree()",
		OrderBy:     []clickhouse.ColumnRef{{PlainItem: common.PlainItemTypeEntityId}},
		SkipIndexes: &policy,
	})
	require.NoError(t, err)
	require.Contains(t, sql, `INDEX idx_section_symbol_role_lr "tv:symbol:lr:`)
	require.Contains(t, sql, "TYPE bloom_filter(0.01) GRANULARITY 4")
}

// TestSkipIndexes_PruneGranules executes the SD4 acceptance criterion the way
// ADR-0066's matrix was verified: clickhouse-local, EXPLAIN indexes = 1, a
// `has` guard over a bloom_filter-indexed membership lane. The skip stages
// chain, so the assertion compares the first stage's denominator (all
// selected granules) against the last stage's numerator (granules left after
// every skip index ran).
func TestSkipIndexes_PruneGranules(t *testing.T) {
	bin, err := exec.LookPath("clickhouse-local")
	if err != nil {
		t.Skipf("clickhouse-local not on PATH, skipping: %v", err)
	}

	ir, conv := composeFixture(t)
	policy := clickhouse.DefaultSkipIndexPolicy()
	ddlSQL, err := clickhouse.ComposeCreateTable("tprune", ir, common.TableRowConfigMultiAttributesPerRow, conv, clickhouse.TableOptions{
		Engine:      "MergeTree()",
		OrderBy:     []clickhouse.ColumnRef{{PlainItem: common.PlainItemTypeEntityId}},
		SkipIndexes: &policy,
	})
	require.NoError(t, err)

	// Physical lane names, in IR order (id, ts, value, lr, lrcard).
	names := fixturePhysicalNames(t, ir, conv)
	require.Len(t, names, 5)
	quoted := make([]string, len(names))
	var membLane string
	for i, n := range names {
		quoted[i] = `"` + n + `"`
		if strings.Contains(n, ":lr:lr:") {
			membLane = quoted[i]
		}
	}
	require.NotEmpty(t, membLane, "membership lane not found in %v", names)

	// One distinct membership id per 8192-row granule, so the needle 3 lives
	// in exactly one granule of ~25. The session setting mirrors what the
	// fixture's LowCardinality(UInt64) membership lane needs at CREATE time.
	script := "SET allow_suspicious_low_cardinality_types = 1;\n" + ddlSQL + ";\n" +
		"INSERT INTO tprune (" + strings.Join(quoted, ", ") + ") " +
		"SELECT number, toDateTime64(number, 3), ['v'], [intDiv(number, 8192)], [1] FROM numbers(200000);\n" +
		"EXPLAIN indexes = 1 SELECT count() FROM tprune WHERE has(" + membLane + ", 3);\n"

	cmd := exec.Command(bin, "--multiquery", "--output-format", "TSVRaw")
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "clickhouse-local failed:\n%s\nscript:\n%s", stderr.String(), script)

	explain := stdout.String()
	require.Contains(t, explain, "idx_section_symbol_role_lr", "the derived index must appear in the plan:\n%s", explain)

	// Collect the "Granules: n/m" stages in plan order.
	type stage struct{ num, den int }
	var stages []stage
	for line := range strings.SplitSeq(explain, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Granules:") {
			continue
		}
		frac := strings.TrimSpace(strings.TrimPrefix(line, "Granules:"))
		numS, denS, ok := strings.Cut(frac, "/")
		require.True(t, ok, "unparseable granule line %q", line)
		num, err := strconv.Atoi(strings.TrimSpace(numS))
		require.NoError(t, err)
		den, err := strconv.Atoi(strings.TrimSpace(denS))
		require.NoError(t, err)
		stages = append(stages, stage{num, den})
	}
	require.GreaterOrEqual(t, len(stages), 2, "expected primary + skip stages:\n%s", explain)
	first, last := stages[0], stages[len(stages)-1]
	require.Greater(t, first.den, 1, "fixture must span multiple granules:\n%s", explain)
	require.Less(t, last.num, first.den, "the has guard must prune granules through the derived index:\n%s", explain)
}

func fixturePhysicalNames(t *testing.T, ir *common.IntermediateTableRepresentation, conv common.NamingConventionI) []string {
	t.Helper()
	phys := make([]common.PhysicalColumnDesc, 0, 8)
	var err error
	for cc, cp := range ir.IterateColumnProps() {
		phys, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, phys, common.TableRowConfigMultiAttributesPerRow)
		require.NoError(t, err)
	}
	names := make([]string, 0, len(phys))
	for _, p := range phys {
		names = append(names, p.String())
	}
	return names
}
