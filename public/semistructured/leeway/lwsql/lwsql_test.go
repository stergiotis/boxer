package lwsql

import (
	"iter"
	"slices"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stretchr/testify/require"
)

const testTable = "mytable"

// buildKnownNames constructs a small leeway table and returns its generated
// physical column names — the same names a real ClickHouse table would carry:
//   - plain id (backbone → section "id")
//   - string:value       — single-value section
//   - timeRange:value    — single-value section (multi-word)
//   - symbol:value, :ref — a two-value-column section (shipped addSymbol shape)
func buildKnownNames(t *testing.T) []string {
	t.Helper()
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	manip.SetTableName(testTable)
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
	hints := encodingaspects.EncodeAspectsMustValidate(encodingaspects.AspectLightGeneralCompression)
	const memb = common.MembershipSpecMixedLowCardVerbatimHighCardParameters
	manip.MergeTaggedValueColumn("string", "value", ctabb.S, hints, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, memb, "", "")
	manip.MergeTaggedValueColumn("timeRange", "value", ctabb.U64, hints, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, memb, "", "")
	manip.MergeTaggedValueColumn("symbol", "value", ctabb.S, hints, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, memb, "", "")
	manip.MergeTaggedValueColumn("symbol", "ref", ctabb.U64, hints, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, memb, "", "")
	tbl, err := manip.BuildTableDesc()
	require.NoError(t, err)

	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tbl, tech))
	phys := make([]common.PhysicalColumnDesc, 0, 32)
	for cc, cp := range ir.IterateColumnProps() {
		phys, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, phys, common.TableRowConfigMultiAttributesPerRow)
		require.NoError(t, err)
	}
	names := make([]string, 0, len(phys))
	for _, p := range phys {
		names = append(names, strings.Join(p.NameComponents, ""))
	}
	require.NotEmpty(t, names)
	return names
}

func newTestResolver(names []string) *Resolver {
	return NewResolver(passes.NewStaticSchemaProvider(map[string][]string{testTable: names}))
}

func TestResolver_ColonHandles(t *testing.T) {
	names := buildKnownNames(t)
	r := newTestResolver(names)
	ok := func(handle string) []string {
		t.Helper()
		res := r.Resolve("", testTable, handle)
		require.Equalf(t, passes.ResolveOK, res.Kind, "handle %q should resolve", handle)
		require.NotEmptyf(t, res.Physical, "handle %q", handle)
		for _, p := range res.Physical {
			require.Containsf(t, names, p, "resolved %q not among the table's columns", p)
		}
		return res.Physical
	}
	kind := func(handle string) passes.ResolveKind {
		return r.Resolve("", testTable, handle).Kind
	}

	// section:column, style-folded
	require.Equal(t, ok("string:value"), ok("String:Value"))
	require.Len(t, ok("string:value"), 1)

	// two value columns in one section, distinct
	require.NotEqual(t, ok("symbol:value")[0], ok("symbol:ref")[0])

	// :* expands to all the section's value columns
	require.Len(t, ok("symbol:*"), 2)
	require.Len(t, ok("string:*"), 1)

	// plain/backbone section
	ok("id:id")

	// colon-always: a bare identifier is never a handle
	require.Equal(t, passes.ResolveNotAHandle, kind("symbol"))
	require.Equal(t, passes.ResolveNotAHandle, kind("value"))
	// a physical name typed verbatim (many colons) is not a handle — it must
	// pass through untouched, not warn as an unknown section
	require.Equal(t, passes.ResolveNotAHandle, kind("tv:symbol:value:val:s:124::I:0::data"))

	// known section, unknown column → candidates
	res := r.Resolve("", testTable, "symbol:nope")
	require.Equal(t, passes.ResolveUnknownColumn, res.Kind)
	require.Contains(t, res.Candidates, "value")
	require.Contains(t, res.Candidates, "ref")

	// unknown section
	require.Equal(t, passes.ResolveUnknownSection, kind("nope:x"))
	// (that support columns resolve via section:column — never false-warn — is
	// covered by TestBuildLabels_RoundTrip, which resolves every labelled column.)
}

func TestResolver_NonLeewayTable(t *testing.T) {
	r := NewResolver(passes.NewStaticSchemaProvider(map[string][]string{
		"plain": {"user_id", "amount", "created_at"},
	}))
	require.Equal(t, passes.ResolveNotAHandle, r.Resolve("", "plain", "foo:bar").Kind)
	require.Equal(t, passes.ResolveNotAHandle, r.Resolve("", "unknown", "foo:bar").Kind)
}

func TestBuildLabels_RoundTrip(t *testing.T) {
	names := buildKnownNames(t)
	labels := BuildLabels(names)
	require.NotEmpty(t, labels)

	r := newTestResolver(names)
	// Every label is `section:column` and round-trips: resolving it yields
	// exactly the physical column it labels.
	for phys, label := range labels {
		require.Containsf(t, label, ":", "label %q should be section:column", label)
		res := r.Resolve("", testTable, label)
		require.Equalf(t, passes.ResolveOK, res.Kind, "label %q should resolve", label)
		require.Equalf(t, []string{phys}, res.Physical, "label %q should round-trip", label)
	}

	// Spot-check specific forms (single-word, case-insensitive).
	lower := make(map[string]struct{}, len(labels))
	for _, l := range labels {
		lower[strings.ToLower(l)] = struct{}{}
	}
	require.Contains(t, lower, "symbol:value")
	require.Contains(t, lower, "symbol:ref")
	require.Contains(t, lower, "id:id")
}

// TestDefaultConditionSectionIsAValidName is the check NewResolver used to make
// at runtime, and panic on. Keeping it here is what lets that constructor be
// infallible: an edit to DefaultConditionSection that a leeway name cannot
// carry fails this test instead of crashing a host at its wiring site.
func TestDefaultConditionSectionIsAValidName(t *testing.T) {
	name, err := naming.MakeStylableName(fold(DefaultConditionSection))
	require.NoErrorf(t, err, "DefaultConditionSection %q is not a valid leeway name", DefaultConditionSection)
	require.Equal(t, name, defaultConditionSection,
		"NewResolver's condition section must be the validated fold of DefaultConditionSection")

	// And the two constructors agree, so a host that names the default
	// explicitly gets the same Resolver as one that takes it.
	explicit, err := NewResolverWithConditionSection(nil, DefaultConditionSection)
	require.NoError(t, err)
	require.Equal(t, explicit.conditionSection, NewResolver(nil).conditionSection)
}

// flakyProvider fails its first probe the way an unreachable server or a timed
// out system.columns query does, then answers normally.
type flakyProvider struct {
	calls int
	names []string
}

func (inst *flakyProvider) GetColumns(dbName string, tableName string) (columns iter.Seq[string], nColumns int, found bool) {
	inst.calls++
	if inst.calls == 1 {
		return nil, 0, false
	}
	return slices.Values(inst.names), len(inst.names), true
}

// TestResolver_TransientProbeFailureIsNotCached guards a bug where a provider
// that could not answer was cached as "not leeway" with no expiry: one probe
// timeout stopped every handle on that table from resolving for the rest of the
// process's life, and the provider was never consulted again to notice the
// server had come back.
func TestResolver_TransientProbeFailureIsNotCached(t *testing.T) {
	p := &flakyProvider{names: buildKnownNames(t)}
	r := NewResolver(p)

	// First probe fails: no resolution, and nothing learned about the table.
	require.Equal(t, passes.ResolveNotAHandle, r.Resolve("", testTable, "symbol:*").Kind)
	require.Equal(t, 1, p.calls)

	// Second attempt must re-probe rather than answer from a cached negative.
	res := r.Resolve("", testTable, "symbol:*")
	require.Equal(t, passes.ResolveOK, res.Kind, "a transient probe failure must not be cached")
	require.Len(t, res.Physical, 2)
	require.Equal(t, 2, p.calls)

	// And the successful index IS cached — no third probe.
	require.Equal(t, passes.ResolveOK, r.Resolve("", testTable, "symbol:*").Kind)
	require.Equal(t, 2, p.calls, "a resolved index should be cached")
}

// TestResolver_NonLeewayVerdictIsCached is the other half: a table the provider
// answered for, whose columns are not leeway-shaped, is a stable fact and must
// be probed once — the property the transient-failure fix must not give up.
func TestResolver_NonLeewayVerdictIsCached(t *testing.T) {
	p := &countingProvider{names: []string{"user_id", "amount", "created_at"}}
	r := NewResolver(p)
	for range 3 {
		require.Equal(t, passes.ResolveNotAHandle, r.Resolve("", "plain", "foo:bar").Kind)
	}
	require.Equal(t, 1, p.calls, "a non-leeway table should be probed once, not per Resolve")
}

type countingProvider struct {
	calls int
	names []string
}

func (inst *countingProvider) GetColumns(dbName string, tableName string) (columns iter.Seq[string], nColumns int, found bool) {
	inst.calls++
	return slices.Values(inst.names), len(inst.names), true
}

// TestResolver_IsHandleSyntax pins the gate the no-catalog-source diagnostic
// hangs off: it is the colon-always rule alone, with no table consulted.
func TestResolver_IsHandleSyntax(t *testing.T) {
	r := NewResolver(passes.NewStaticSchemaProvider(nil))
	require.True(t, r.IsHandleSyntax("symbol:value"))
	require.True(t, r.IsHandleSyntax("id:*"))
	require.False(t, r.IsHandleSyntax("symbol"), "a bare identifier is ordinary SQL")
	require.False(t, r.IsHandleSyntax("tv:symbol:value:val:s:124::I:0::data"), "a physical name is not a handle")
}
