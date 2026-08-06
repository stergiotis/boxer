package datacatalog_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// TestRestorationRoundTrip is the load-bearing test of ADR-0170: the naming
// convention is claimed to be bidirectional, and the whole catalog rests on it.
// A fixture table, rendered to physical column names and classified back, must
// relate to the original as *equal* — under both separators, since a catalog
// run sees `:` on tables boxer wrote and `_` on tables restored from a dump.
//
// Silent regression of the naming grammar or the relation semantics turns this
// red.
func TestRestorationRoundTrip(t *testing.T) {
	ops, err := common.NewTableOperations()
	require.NoError(t, err)
	original := buildFixtureTable(t)
	for _, sep := range []string{":", "_"} {
		t.Run("sep="+sep, func(t *testing.T) {
			cl := datacatalog.Classify(physicalNames(t, &original, sep))
			require.NoError(t, cl.Err)
			require.Equal(t, datacatalog.KindLeeway, cl.Kind)
			rel, relErr := ops.Relate(&original, cl.Table)
			require.NoError(t, relErr)
			assert.Equal(t, common.TableRelationEqual, rel, "restored table must relate as equal")
			// And the derived keys agree, which is the stronger claim the pair
			// matrix actually consumes.
			origKeys, keyErr := datacatalog.AttrKeys(ops, &original)
			require.NoError(t, keyErr)
			restoredKeys, keyErr := datacatalog.AttrKeys(ops, cl.Table)
			require.NoError(t, keyErr)
			assert.Equal(t, origKeys, restoredKeys)
			assert.Equal(t, datacatalog.HashAttrKeys(origKeys), datacatalog.HashAttrKeys(restoredKeys))
		})
	}
}

func TestAttrKeys_ShapeAndOrder(t *testing.T) {
	ops, err := common.NewTableOperations()
	require.NoError(t, err)
	tbl := buildFixtureTable(t)
	keys, err := datacatalog.AttrKeys(ops, &tbl)
	require.NoError(t, err)
	require.NotEmpty(t, keys)
	assert.True(t, sort.StringsAreSorted(keys), "keys must be sorted: %v", keys)
	for _, k := range keys {
		assert.Truef(t,
			strings.HasPrefix(k, datacatalog.ScopePlain+"/") || strings.HasPrefix(k, datacatalog.ScopeTagged+"/"),
			"key %q carries neither scope", k)
		assert.Containsf(t, k, ":", "key %q carries no canonical type", k)
	}
	// The two scopes are both populated by the fixture, so a key format that
	// silently dropped one half would not pass.
	assert.Contains(t, keys, "plain/entity-id/id:bh")
	assert.Contains(t, keys, "tagged/metric/value:u64")
}

// Naming style must not reach the keys: containment is decided on normalized
// tables, so the hashes have to be decided there too or two tables Relate calls
// equal could carry different schema hashes.
func TestAttrKeys_NamingStyleInsensitive(t *testing.T) {
	ops, err := common.NewTableOperations()
	require.NoError(t, err)
	camel := buildOneSectionTable(t, "geoPoint", "latDegrees")
	spinal := buildOneSectionTable(t, "geo-point", "lat-degrees")
	camelKeys, err := datacatalog.AttrKeys(ops, &camel)
	require.NoError(t, err)
	spinalKeys, err := datacatalog.AttrKeys(ops, &spinal)
	require.NoError(t, err)
	assert.Equal(t, spinalKeys, camelKeys)
	assert.Equal(t, datacatalog.HashAttrKeys(spinalKeys), datacatalog.HashAttrKeys(camelKeys))
}

func TestHashAttrKeys(t *testing.T) {
	a := []string{"tagged/metric/value:u64"}
	b := []string{"tagged/metric/value:f64"}
	assert.Equal(t, datacatalog.HashAttrKeys(a), datacatalog.HashAttrKeys(a))
	assert.NotEqual(t, datacatalog.HashAttrKeys(a), datacatalog.HashAttrKeys(b))
	// The per-key terminator means a boundary cannot be forged by concatenation.
	assert.NotEqual(t,
		datacatalog.HashAttrKeys([]string{"ab", "c"}),
		datacatalog.HashAttrKeys([]string{"a", "bc"}))
}

func TestIntersectKeys(t *testing.T) {
	assert.Equal(t, []string{"b", "c"}, datacatalog.IntersectKeys([]string{"a", "b", "c"}, []string{"b", "c", "d"}))
	assert.Empty(t, datacatalog.IntersectKeys([]string{"a"}, []string{"b"}))
	assert.Empty(t, datacatalog.IntersectKeys(nil, []string{"b"}))
}

// The invariant the ADR states and the book chapters lean on: for an equal or
// subset pair the intersection is the contained side's whole key set, so the
// pair's shape_id equals that side's schema_hash — which is what lets a Sankey
// draw a shape node flowing into every table that contains it.
func TestRelatePairs_SubsetShapeIdIsContainedSideSchemaHash(t *testing.T) {
	ops, err := common.NewTableOperations()
	require.NoError(t, err)

	small := buildOneSectionTable(t, "metric", "value")
	large := buildTwoSectionTable(t)

	tables := []datacatalog.LeewayTable{
		mustLeewayTable(t, ops, datacatalog.TableRef{Database: "d", Name: "large"}, &large),
		mustLeewayTable(t, ops, datacatalog.TableRef{Database: "d", Name: "small"}, &small),
	}
	pairs, err := datacatalog.RelatePairs(ops, tables)
	require.NoError(t, err)
	require.Len(t, pairs, 1)

	p := pairs[0]
	// Sorted: "large" precedes "small", so A is the container and the relation
	// reads "A is a superset of B".
	assert.Equal(t, "large", p.A.Name)
	assert.Equal(t, "small", p.B.Name)
	assert.Equal(t, common.TableRelationSuperset, p.Relation)

	var smallHash uint64
	for _, lt := range tables {
		if lt.Ref.Name == "small" {
			smallHash = lt.SchemaHash
		}
	}
	assert.Equal(t, smallHash, p.ShapeId)
	assert.EqualValues(t, len(small.TaggedValuesSections[0].ValueColumnNames)+len(small.PlainValuesNames), p.NCommon)
	assert.Greater(t, p.Jaccard, float32(0))
	assert.Less(t, p.Jaccard, float32(1))
}

// Two tables that share nothing: disjoint, shape_id 0, jaccard 0. Stored rather
// than dropped — "these two share nothing" is an answer.
func TestRelatePairs_Disjoint(t *testing.T) {
	ops, err := common.NewTableOperations()
	require.NoError(t, err)
	// Nothing in common, backbone included — sharing the `id` column would make
	// these two overlap, which is the mistake this fixture is written to avoid.
	a := buildTable(t, "orderId", "metric", "value")
	b := buildTable(t, "shipmentId", "shipment", "weight")
	pairs, err := datacatalog.RelatePairs(ops, []datacatalog.LeewayTable{
		mustLeewayTable(t, ops, datacatalog.TableRef{Database: "d", Name: "a"}, &a),
		mustLeewayTable(t, ops, datacatalog.TableRef{Database: "d", Name: "b"}, &b),
	})
	require.NoError(t, err)
	require.Len(t, pairs, 1)
	assert.Equal(t, uint64(0), pairs[0].ShapeId)
	assert.EqualValues(t, 0, pairs[0].NCommon)
	assert.EqualValues(t, 0, pairs[0].Jaccard)
}

// Equal tables: the pair's shape_id is each side's schema_hash, and jaccard 1.
func TestRelatePairs_Equal(t *testing.T) {
	ops, err := common.NewTableOperations()
	require.NoError(t, err)
	a := buildOneSectionTable(t, "metric", "value")
	b := buildOneSectionTable(t, "metric", "value")
	tables := []datacatalog.LeewayTable{
		mustLeewayTable(t, ops, datacatalog.TableRef{Database: "d", Name: "a"}, &a),
		mustLeewayTable(t, ops, datacatalog.TableRef{Database: "d", Name: "b"}, &b),
	}
	pairs, err := datacatalog.RelatePairs(ops, tables)
	require.NoError(t, err)
	require.Len(t, pairs, 1)
	assert.Equal(t, common.TableRelationEqual, pairs[0].Relation)
	assert.Equal(t, tables[0].SchemaHash, pairs[0].ShapeId)
	assert.EqualValues(t, 1, pairs[0].Jaccard)
}

// Every unordered pair exactly once, ordered so A precedes B — the ORDER BY the
// catalog table declares.
func TestRelatePairs_CoversEachPairOnceInOrder(t *testing.T) {
	ops, err := common.NewTableOperations()
	require.NoError(t, err)
	tbl := buildOneSectionTable(t, "metric", "value")
	tables := make([]datacatalog.LeewayTable, 0, 4)
	for _, name := range []string{"d", "b", "a", "c"} {
		tables = append(tables, mustLeewayTable(t, ops, datacatalog.TableRef{Database: "db", Name: name}, &tbl))
	}
	pairs, err := datacatalog.RelatePairs(ops, tables)
	require.NoError(t, err)
	assert.Len(t, pairs, 6)
	seen := make(map[string]struct{}, len(pairs))
	for _, p := range pairs {
		assert.Negative(t, p.A.Compare(p.B), "pair is not ordered: %v %v", p.A, p.B)
		key := p.A.String() + "|" + p.B.String()
		_, dup := seen[key]
		assert.Falsef(t, dup, "pair %s seen twice", key)
		seen[key] = struct{}{}
	}
}

func TestRelatePairs_NoTables(t *testing.T) {
	ops, err := common.NewTableOperations()
	require.NoError(t, err)
	pairs, err := datacatalog.RelatePairs(ops, nil)
	require.NoError(t, err)
	assert.Empty(t, pairs)
}

func TestNewLeewayTable_RejectsOpaque(t *testing.T) {
	ops, err := common.NewTableOperations()
	require.NoError(t, err)
	_, err = datacatalog.NewLeewayTable(ops, datacatalog.TableRef{Database: "d", Name: "t"},
		datacatalog.Classify([]string{"ts", "value"}))
	assert.Error(t, err)
}

func TestTableRef_CompareAndString(t *testing.T) {
	a := datacatalog.TableRef{Database: "boxer", Name: "facts"}
	b := datacatalog.TableRef{Database: "boxer", Name: "tables_catalog"}
	c := datacatalog.TableRef{Database: "other", Name: "aaa"}
	assert.Equal(t, "boxer.facts", a.String())
	assert.Negative(t, a.Compare(b))
	assert.Negative(t, b.Compare(c))
	assert.Zero(t, a.Compare(a))
	assert.Positive(t, c.Compare(a))
}

func TestIsSystemDatabase(t *testing.T) {
	assert.True(t, datacatalog.IsSystemDatabase("system"))
	assert.True(t, datacatalog.IsSystemDatabase("INFORMATION_SCHEMA"))
	assert.True(t, datacatalog.IsSystemDatabase("information_schema"))
	assert.False(t, datacatalog.IsSystemDatabase("boxer"))
}

func mustLeewayTable(t *testing.T, ops *common.TableOperations, ref datacatalog.TableRef, tbl *common.TableDesc) (lt datacatalog.LeewayTable) {
	t.Helper()
	lt, err := datacatalog.NewLeewayTable(ops, ref, datacatalog.Classification{
		Kind:      datacatalog.KindLeeway,
		Table:     tbl,
		RowConfig: common.TableRowConfigMultiAttributesPerRow,
	})
	require.NoError(t, err)
	return
}

// buildOneSectionTable is the minimal leeway table: one backbone id and one
// tagged column, named by the caller so two fixtures can be made to overlap or
// not.
func buildOneSectionTable(t *testing.T, section string, column string) (tbl common.TableDesc) {
	t.Helper()
	return buildTable(t, "id", section, column)
}

func buildTable(t *testing.T, idName string, section string, column string) (tbl common.TableDesc) {
	t.Helper()
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	ctp := canonicaltypes.NewParser()
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, naming.StylableName(idName), ctp.MustParsePrimitiveTypeAst("bh"),
		encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet)
	manip.MergeTaggedValueColumn(naming.StylableName(section), naming.StylableName(column),
		ctp.MustParsePrimitiveTypeAst("u64"),
		encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet,
		common.MembershipSpecHighCardRef, naming.Key(""), naming.Key(""))
	tbl, err = manip.BuildTableDesc()
	require.NoError(t, err)
	return
}

// buildTwoSectionTable is buildOneSectionTable("metric", "value") plus a second
// section, so it strictly contains it.
func buildTwoSectionTable(t *testing.T) (tbl common.TableDesc) {
	t.Helper()
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	ctp := canonicaltypes.NewParser()
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "id", ctp.MustParsePrimitiveTypeAst("bh"),
		encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet)
	manip.MergeTaggedValueColumn("metric", "value", ctp.MustParsePrimitiveTypeAst("u64"),
		encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet,
		common.MembershipSpecHighCardRef, naming.Key(""), naming.Key(""))
	manip.MergeTaggedValueColumn("label", "text", ctp.MustParsePrimitiveTypeAst("s"),
		encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet,
		common.MembershipSpecHighCardRef, naming.Key(""), naming.Key(""))
	tbl, err = manip.BuildTableDesc()
	require.NoError(t, err)
	return
}
