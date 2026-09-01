package singledecl

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/readback"
	"github.com/stretchr/testify/require"
)

// striple is the facts11'-shaped section: two declared-single primaries (lv,
// mvhp) beside two carded channels (lr, hv) over a list value lane. The
// fixture data is the bronze shape — lr never written — with one multi-hv
// attribute, one hv-less attribute whose value list is present but EMPTY,
// and a second entity sharing the first's lv and address.

func writeStriple(t *testing.T) arrow.RecordBatch {
	t.Helper()
	b := NewInEntityStriple(memory.NewGoAllocator(), 4)
	b.BeginEntity().SetId(1)
	b.GetSectionFacts().
		BeginAttribute().AddToContainer("v1").AddToContainer("v2").
		AddMembershipLowCardVerbatim([]byte("name")).
		AddMembershipMixedLowCardVerbatim([]byte("path1"), []byte("0000")).
		AddMembershipHighCardVerbatim([]byte("e1")).
		AddMembershipHighCardVerbatim([]byte("e2")).
		EndAttribute().
		BeginAttribute(). // zero AddToContainer calls: present but empty
		AddMembershipLowCardVerbatim([]byte("empty")).
		AddMembershipMixedLowCardVerbatim([]byte("path2"), []byte("0001")).
		EndAttribute().EndSection()
	require.NoError(t, b.CommitEntity())
	b.BeginEntity().SetId(2)
	b.GetSectionFacts().
		BeginAttribute().AddToContainer("w").
		AddMembershipLowCardVerbatim([]byte("name")).
		AddMembershipMixedLowCardVerbatim([]byte("path1"), []byte("0000")).
		AddMembershipHighCardVerbatim([]byte("e1")).
		EndAttribute().EndSection()
	require.NoError(t, b.CommitEntity())
	recs, err := b.TransferRecords(nil)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	return recs[0]
}

// TestTripleWriteEnforcesPerChannel: arity is enforced per declared channel —
// each primary needs exactly one membership independently, while the carded
// channels accept zero and several on the same attribute.
func TestTripleWriteEnforcesPerChannel(t *testing.T) {
	fresh := func() *InEntityStriple { return NewInEntityStriple(memory.NewGoAllocator(), 1) }

	b := fresh()
	b.BeginEntity().SetId(1)
	b.GetSectionFacts().BeginAttribute().AddToContainer("v").
		AddMembershipMixedLowCardVerbatim([]byte("p"), []byte("0000")).
		EndAttribute().EndSection()
	require.ErrorIs(t, b.CommitEntity(), runtime.ErrSingleMembershipViolated, "zero lv memberships")

	b = fresh()
	b.BeginEntity().SetId(1)
	b.GetSectionFacts().BeginAttribute().AddToContainer("v").
		AddMembershipLowCardVerbatim([]byte("a")).
		AddMembershipLowCardVerbatim([]byte("b")).
		AddMembershipMixedLowCardVerbatim([]byte("p"), []byte("0000")).
		EndAttribute().EndSection()
	require.ErrorIs(t, b.CommitEntity(), runtime.ErrSingleMembershipViolated, "two lv memberships")

	b = fresh()
	b.BeginEntity().SetId(1)
	b.GetSectionFacts().BeginAttribute().AddToContainer("v").
		AddMembershipLowCardVerbatim([]byte("a")).
		EndAttribute().EndSection()
	require.ErrorIs(t, b.CommitEntity(), runtime.ErrSingleMembershipViolated, "zero mvhp memberships")

	b = fresh()
	b.BeginEntity().SetId(1)
	b.GetSectionFacts().BeginAttribute().AddToContainer("v").
		AddMembershipLowCardVerbatim([]byte("a")).
		AddMembershipMixedLowCardVerbatim([]byte("p"), []byte("0000")).
		AddMembershipHighCardVerbatim([]byte("e1")).
		AddMembershipHighCardVerbatim([]byte("e2")).
		EndAttribute().EndSection()
	require.NoError(t, b.CommitEntity(), "one of each primary; hv and lr are unconstrained (two hv, zero lr)")
}

func collectStrings(t *testing.T, s func(func(string) bool)) (out []string) {
	t.Helper()
	for v := range s {
		out = append(out, v)
	}
	return
}

// TestTripleReadAccess: the pack answers all four channels per attribute —
// the declared primaries off the identity accel, the carded channels off
// their card lanes (hv ragged, lr all-empty) — and the value reader keeps a
// present-but-empty list distinct from an absent attribute.
func TestTripleReadAccess(t *testing.T) {
	rec := writeStriple(t)
	defer rec.Release()

	pack := NewMembershipPackStripleFactsFacts()
	require.NoError(t, pack.LoadFromRecord(rec))
	defer pack.Release()

	require.Equal(t, []string{"name"}, collectBytes(t, pack.GetMembValueLowCardVerbatim(0, 0)))
	require.Equal(t, []string{"empty"}, collectBytes(t, pack.GetMembValueLowCardVerbatim(0, 1)))
	require.Equal(t, []string{"name"}, collectBytes(t, pack.GetMembValueLowCardVerbatim(1, 0)))

	require.Equal(t, []string{"e1", "e2"}, collectBytes(t, pack.GetMembValueHighCardVerbatim(0, 0)), "hv is ragged: two annotations")
	require.Empty(t, collectBytes(t, pack.GetMembValueHighCardVerbatim(0, 1)), "hv is ragged: zero annotations")
	require.Equal(t, []string{"e1"}, collectBytes(t, pack.GetMembValueHighCardVerbatim(1, 0)))

	require.Empty(t, collectU64(t, pack.GetMembValueLowCardRef(0, 0)), "the reserved lr lane stays empty in bronze")
	require.Empty(t, collectU64(t, pack.GetMembValueLowCardRef(0, 1)))
	require.Empty(t, collectU64(t, pack.GetMembValueLowCardRef(1, 0)))

	var pairs [][2]string
	for id, params := range pack.GetMembValueLowCardVerbatimHighCardParams(0, 0) {
		pairs = append(pairs, [2]string{string(id), string(params)})
	}
	require.Equal(t, [][2]string{{"path1", "0000"}}, pairs)

	attrs := NewReadAccessStripleTaggedFactsAttributes()
	require.NoError(t, attrs.LoadFromRecord(rec))
	defer attrs.Release()
	require.EqualValues(t, 2, attrs.GetNumberOfAttributes(0), "the empty-list attribute exists")
	require.Equal(t, []string{"v1", "v2"}, collectStrings(t, attrs.GetAttrValueValue(0, 0)))
	require.Empty(t, collectStrings(t, attrs.GetAttrValueValue(0, 1)), "present but empty")
	require.EqualValues(t, 1, attrs.GetNumberOfAttributes(1))
}

// TestTripleAuthoringSurface: on a four-channel section the declared
// primaries expand to the fast forms and the carded channels keep the
// general form — per channel, within one section.
func TestTripleAuthoringSurface(t *testing.T) {
	var names []string
	for _, f := range CreateSchemaStriple().Fields() {
		names = append(names, f.Name)
	}
	r := lwsql.NewResolver(passes.NewStaticSchemaProvider(map[string][]string{"striple": names}))
	pass := constructsql.ExtractExpandPass(r, "")

	out, err := pass.Run("SELECT LW_GET_LIST('facts', 'name', 'chan:low-card-verbatim') FROM striple")
	require.NoError(t, err)
	require.Contains(t, out, "indexOf(", "declared lv takes the fast locator")
	require.Contains(t, out, "arraySlice(", "list value read off the length lane directly")
	require.NotContains(t, out, "LW_RAGGED_PARENT_IDS")

	out, err = pass.Run("SELECT LW_GET_LIST('facts', 'path1', 'chan:low-card-verbatim-high-card-params', 'param:0000') FROM striple")
	require.NoError(t, err)
	require.Contains(t, out, "arrayFirstIndex(", "declared mvhp locates by the pair")
	require.NotContains(t, out, "LW_RAGGED_PARENT_IDS")

	out, err = pass.Run("SELECT LW_GET_LIST('facts', 'e1', 'chan:high-card-verbatim') FROM striple")
	require.NoError(t, err)
	require.Contains(t, out, "LW_RAGGED_PARENT_IDS", "carded hv keeps the general form")

	out, err = pass.Run("SELECT LW_GET_LIST('facts', 7, 'chan:low-card-ref') FROM striple")
	require.NoError(t, err)
	require.Contains(t, out, "LW_RAGGED_PARENT_IDS", "carded lr keeps the general form")
}

// TestTripleServerTruth runs the expanded reads over the written batch in
// clickhouse-local and pins the answers: the same values through the lv fast
// form, the mvhp pair locator and the hv general form; the empty-list
// attribute reads as [] like an absent one, and LW_SEL is what tells the two
// apart (a selector of length 1 vs 0).
func TestTripleServerTruth(t *testing.T) {
	rec := writeStriple(t)
	defer rec.Release()
	file := writeArrowFile(t, rec)

	var names []string
	for _, f := range CreateSchemaStriple().Fields() {
		names = append(names, f.Name)
	}
	r := lwsql.NewResolver(passes.NewStaticSchemaProvider(map[string][]string{"striple": names}))
	pass := constructsql.ExtractExpandPass(r, "")

	sql := "SELECT " +
		"LW_GET_LIST('facts', 'name', 'chan:low-card-verbatim'), " +
		"LW_GET_LIST('facts', 'path1', 'chan:low-card-verbatim-high-card-params', 'param:0000'), " +
		"LW_GET_LIST('facts', 'e1', 'chan:high-card-verbatim'), " +
		"LW_GET_LIST('facts', 'empty', 'chan:low-card-verbatim'), " +
		"length(LW_SEL('facts', 'empty', 'chan:low-card-verbatim')) " +
		"FROM striple ORDER BY \"id:id:u64:::0:\""
	expanded, err := pass.Run(sql)
	require.NoError(t, err)
	expanded = strings.Replace(expanded, "striple", "file('"+file+"', 'Arrow')", 1)

	out := runClickHouseLocal(t, readback.HelperUDFsSQL()+"\n"+expanded+";")
	require.Equal(t,
		"['v1','v2']\t['v1','v2']\t['v1','v2']\t[]\t1\n"+
			"['w']\t['w']\t['w']\t[]\t0\n",
		out)
}

// TestTripleDiscoveryRoundTrip: the four-channel section's physical names
// carry both single-membership declarations, and the re-derived IR keeps the
// carded channels' lanes while not inventing the omitted ones.
func TestTripleDiscoveryRoundTrip(t *testing.T) {
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	var names []string
	for _, f := range CreateSchemaStriple().Fields() {
		names = append(names, f.Name)
	}
	phys, err := conv.ParseColumns(names)
	require.NoError(t, err)
	tbl, trc, err := conv.DiscoverTableFromPhysicalColumns(phys)
	require.NoError(t, err)
	require.Equal(t, common.TableRowConfigMultiAttributesPerRow, trc)

	bySection := map[string]common.MembershipSpecE{}
	for _, sec := range tbl.TaggedValuesSections {
		bySection[string(sec.Name)] = common.SingleMembershipSpecs(sec.UseAspects)
	}
	require.Equal(t,
		common.MembershipSpecLowCardVerbatim|common.MembershipSpecMixedLowCardVerbatimHighCardParameters,
		bySection["facts"])

	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tbl, clickhouse.NewTechnologySpecificCodeGenerator()))
	require.Equal(t, len(names), ir.TotalLength(), "carded lanes kept, omitted lanes not invented")
}
