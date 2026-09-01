package singledecl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/readback"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
	"github.com/stretchr/testify/require"
)

// The fixture data both variants write, verbatim: two entities, three tags
// attributes carrying exactly one lv membership each (one also an additive
// hr membership), and one mixed-channel addr attribute per entity.

// writeSdecl writes the fixture data through the DECLARED table's builder —
// the lv and lmv cardinality lanes do not exist, the arity is enforced.
func writeSdecl(t *testing.T) arrow.RecordBatch {
	t.Helper()
	b := NewInEntitySdecl(memory.NewGoAllocator(), 4)
	b.BeginEntity().SetId(1)
	tags := b.GetSectionTags()
	tags.BeginAttribute("a").AddMembershipLowCardVerbatim([]byte("x")).AddMembershipHighCardRef(7).EndAttribute().
		BeginAttribute("b").AddMembershipLowCardVerbatim([]byte("y")).EndAttribute().EndSection()
	b.GetSectionAddr().BeginAttribute("p").AddMembershipMixedLowCardVerbatim([]byte("m1"), []byte("0001")).EndAttribute().EndSection()
	require.NoError(t, b.CommitEntity())
	b.BeginEntity().SetId(2)
	b.GetSectionTags().BeginAttribute("c").AddMembershipLowCardVerbatim([]byte("x")).EndAttribute().EndSection()
	b.GetSectionAddr().BeginAttribute("q").AddMembershipMixedLowCardVerbatim([]byte("m2"), []byte("0002")).EndAttribute().EndSection()
	require.NoError(t, b.CommitEntity())
	recs, err := b.TransferRecords(nil)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	return recs[0]
}

// writeSflat writes the SAME data through the undeclared twin — its
// cardinality lanes fill with the ones the declaration replaces.
func writeSflat(t *testing.T) arrow.RecordBatch {
	t.Helper()
	b := NewInEntitySflat(memory.NewGoAllocator(), 4)
	b.BeginEntity().SetId(1)
	tags := b.GetSectionTags()
	tags.BeginAttribute("a").AddMembershipLowCardVerbatim([]byte("x")).AddMembershipHighCardRef(7).EndAttribute().
		BeginAttribute("b").AddMembershipLowCardVerbatim([]byte("y")).EndAttribute().EndSection()
	b.GetSectionAddr().BeginAttribute("p").AddMembershipMixedLowCardVerbatim([]byte("m1"), []byte("0001")).EndAttribute().EndSection()
	require.NoError(t, b.CommitEntity())
	b.BeginEntity().SetId(2)
	b.GetSectionTags().BeginAttribute("c").AddMembershipLowCardVerbatim([]byte("x")).EndAttribute().EndSection()
	b.GetSectionAddr().BeginAttribute("q").AddMembershipMixedLowCardVerbatim([]byte("m2"), []byte("0002")).EndAttribute().EndSection()
	require.NoError(t, b.CommitEntity())
	recs, err := b.TransferRecords(nil)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	return recs[0]
}

// TestWriteEnforcesSingleMembership: the declared arity is a write-time
// contract — zero or two memberships on a declared channel fail the commit,
// exactly one passes; the undeclared twin accepts the ragged shapes.
func TestWriteEnforcesSingleMembership(t *testing.T) {
	fresh := func() *InEntitySdecl { return NewInEntitySdecl(memory.NewGoAllocator(), 1) }

	b := fresh()
	b.BeginEntity().SetId(1)
	b.GetSectionTags().BeginAttribute("a").EndAttribute().EndSection()
	err := b.CommitEntity()
	require.ErrorIs(t, err, runtime.ErrSingleMembershipViolated, "zero memberships on a declared channel")

	b = fresh()
	b.BeginEntity().SetId(1)
	b.GetSectionTags().BeginAttribute("a").
		AddMembershipLowCardVerbatim([]byte("x")).
		AddMembershipLowCardVerbatim([]byte("y")).
		EndAttribute().EndSection()
	err = b.CommitEntity()
	require.ErrorIs(t, err, runtime.ErrSingleMembershipViolated, "two memberships on a declared channel")

	b = fresh()
	b.BeginEntity().SetId(1)
	b.GetSectionTags().BeginAttribute("a").AddMembershipLowCardVerbatim([]byte("x")).EndAttribute().EndSection()
	require.NoError(t, b.CommitEntity(), "exactly one membership is the contract")

	// The mixed channel counts its (identity, params) pair once.
	b = fresh()
	b.BeginEntity().SetId(1)
	b.GetSectionAddr().BeginAttribute("p").EndAttribute().EndSection()
	err = b.CommitEntity()
	require.ErrorIs(t, err, runtime.ErrSingleMembershipViolated, "the mixed channel is declared too")

	// The undeclared twin accepts what the declaration refuses.
	f := NewInEntitySflat(memory.NewGoAllocator(), 1)
	f.BeginEntity().SetId(1)
	f.GetSectionTags().BeginAttribute("a").EndAttribute().EndSection()
	require.NoError(t, f.CommitEntity())
}

func collectBytes(t *testing.T, s func(func([]byte) bool)) (out []string) {
	t.Helper()
	for v := range s {
		out = append(out, string(v))
	}
	return
}

// TestReadAccessIdentityAccel: the generated membership packs load the
// declared channels' accel from the membership column itself (the identity
// permutation) and answer per-attribute reads exactly as the carded twin.
func TestReadAccessIdentityAccel(t *testing.T) {
	rec := writeSdecl(t)
	defer rec.Release()

	tags := NewMembershipPackSdeclTagsTags()
	require.NoError(t, tags.LoadFromRecord(rec))
	defer tags.Release()
	require.Equal(t, []string{"x"}, collectBytes(t, tags.GetMembValueLowCardVerbatim(0, 0)))
	require.Equal(t, []string{"y"}, collectBytes(t, tags.GetMembValueLowCardVerbatim(0, 1)))
	require.Equal(t, []string{"x"}, collectBytes(t, tags.GetMembValueLowCardVerbatim(1, 0)))
	var hr []uint64
	for v := range tags.GetMembValueHighCardRef(0, 0) {
		hr = append(hr, v)
	}
	require.Equal(t, []uint64{7}, hr, "the ragged hr channel keeps its carded accel")
	require.Empty(t, collectU64(t, tags.GetMembValueHighCardRef(0, 1)))

	addr := NewMembershipPackSdeclAddrAddr()
	require.NoError(t, addr.LoadFromRecord(rec))
	defer addr.Release()
	var pairs [][2]string
	for id, params := range addr.GetMembValueLowCardVerbatimHighCardParams(0, 0) {
		pairs = append(pairs, [2]string{string(id), string(params)})
	}
	require.Equal(t, [][2]string{{"m1", "0001"}}, pairs)
}

func collectU64(t *testing.T, s func(func(uint64) bool)) (out []uint64) {
	t.Helper()
	for v := range s {
		out = append(out, v)
	}
	return
}

// TagX is the read-back plan: the tags attribute carrying membership "x".
type TagX struct {
	_  struct{} `kind:"tagX"`
	ID uint64   `lw:",id"`
	X  string   `lw:"x,tags,lowCardVerbatim"`
}

func artefactsFor(t *testing.T, declared bool) readback.Artefacts {
	t.Helper()
	manip, err := GetSchemaInManipulator(declared)
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&td, clickhouse.NewTechnologySpecificCodeGenerator()))
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	info := readback.NewInformationRetrieval(conv)
	require.NoError(t, info.LoadTable(ir, common.TableRowConfigMultiAttributesPerRow))
	plan, err := marshallreflect.PlanFor[TagX]()
	require.NoError(t, err)
	g := readback.NewGenerator(info, readback.NewLookupResolver(marshallreflect.MapLookup(nil)))
	art, err := g.Generate(plan)
	require.NoError(t, err)
	return art
}

func writeArrowFile(t *testing.T, rec arrow.RecordBatch) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "batch.arrow")
	f, err := os.Create(path)
	require.NoError(t, err)
	w, err := ipc.NewFileWriter(f, ipc.WithSchema(rec.Schema()), ipc.WithAllocator(memory.NewGoAllocator()))
	require.NoError(t, err)
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())
	require.NoError(t, f.Close())
	return path
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

// TestReadbackFastPathAgreesWithGeneralForm is the server-truth oracle
// (readback invariant I5): the declared schema's artefacts take the bare
// indexOf fast form, the undeclared twin's take the general helper form, and
// over identical data both answer identically.
func TestReadbackFastPathAgreesWithGeneralForm(t *testing.T) {
	fast := artefactsFor(t, true)
	general := artefactsFor(t, false)

	require.Contains(t, fast.Projection, "indexOf(", "the declaration licenses the fast form")
	require.NotContains(t, fast.Projection, "LW_RAGGED_PARENT_IDS", "the fast form drops the position→attribute map")
	require.Contains(t, general.Projection, "LW_RAGGED_PARENT_IDS", "the undeclared twin keeps the general form")

	recD := writeSdecl(t)
	defer recD.Release()
	recF := writeSflat(t)
	defer recF.Release()
	fileD := writeArrowFile(t, recD)
	fileF := writeArrowFile(t, recF)

	outD := runClickHouseLocal(t, readback.HelperUDFsSQL()+"\nSELECT "+fast.Projection+" FROM file('"+fileD+"', 'Arrow');")
	outF := runClickHouseLocal(t, readback.HelperUDFsSQL()+"\nSELECT "+general.Projection+" FROM file('"+fileF+"', 'Arrow');")
	require.Equal(t, outF, outD, "fast and general form must answer identically over identical data")
	require.NotEmpty(t, strings.TrimSpace(outD))
}

// TestAuthoringSurfaceLicensesFastForm: LW_GET over the declared table
// expands to the fast form — the SQL road recovers the declaration from the
// use-aspects the physical column names encode — while the undeclared twin
// keeps the general form.
func TestAuthoringSurfaceLicensesFastForm(t *testing.T) {
	fields := func(schema *arrow.Schema) (names []string) {
		for _, f := range schema.Fields() {
			names = append(names, f.Name)
		}
		return
	}
	r := lwsql.NewResolver(passes.NewStaticSchemaProvider(map[string][]string{
		"sdecl": fields(CreateSchemaSdecl()),
		"sflat": fields(CreateSchemaSflat()),
	}))
	pass := constructsql.ExtractExpandPass(r, "")

	out, err := pass.Run("SELECT LW_GET('tags', 'x', 'chan:low-card-verbatim') FROM sdecl")
	require.NoError(t, err)
	require.Contains(t, out, "indexOf(")
	require.NotContains(t, out, "LW_RAGGED_PARENT_IDS")

	out, err = pass.Run("SELECT LW_GET('addr', 'm1', 'param:0001') FROM sdecl")
	require.NoError(t, err)
	require.Contains(t, out, "arrayFirstIndex(")
	require.NotContains(t, out, "LW_RAGGED_PARENT_IDS")

	out, err = pass.Run("SELECT LW_GET('tags', 'x', 'chan:low-card-verbatim') FROM sflat")
	require.NoError(t, err)
	require.Contains(t, out, "LW_VALUE_BY_TAG_EQUAL(", "the undeclared twin keeps the general form")
}

// TestDiscoveryRoundTripsDeclaration: the physical column names alone carry
// the declaration (the use-aspects segment), so schema discovery
// reconstructs it and a re-derived IR omits the same cardinality lanes —
// the datacatalog / play card path sees the declared layout, not an
// invented one.
func TestDiscoveryRoundTripsDeclaration(t *testing.T) {
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	var names []string
	for _, f := range CreateSchemaSdecl().Fields() {
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
	require.Equal(t, common.MembershipSpecLowCardVerbatim, bySection["tags"])
	require.Equal(t, common.MembershipSpecMixedLowCardVerbatimHighCardParameters, bySection["addr"])

	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tbl, clickhouse.NewTechnologySpecificCodeGenerator()))
	require.Equal(t, len(names), ir.TotalLength(), "the re-derived IR must not invent the omitted cardinality lanes")
}

// TestMarshallReflectRoundTrip is the reflect oracle: rows written through
// the generated builder by marshallreflect read back field-for-field through
// the shared reflect read path, whose membership resolution rides the same
// generated readers (and thereby the identity accel) as every consumer.
func TestMarshallReflectRoundTrip(t *testing.T) {
	rows := []TagX{{ID: 1, X: "a"}, {ID: 2, X: "c"}}
	b := NewInEntitySdecl(memory.NewGoAllocator(), 4)
	require.NoError(t, marshallreflect.Marshal(b, rows, marshallreflect.MapLookup(nil)))
	recs, err := b.TransferRecords(nil)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	rec := recs[0]
	defer rec.Release()

	tags := NewReadAccessSdeclTaggedTags()
	require.NoError(t, tags.LoadFromRecord(rec))
	defer tags.Release()
	readers := marshallreflect.NewSectionReaders(int(rec.NumRows())).
		PlainColumn("id", rec.Column(0)).
		Section("tags", tags.Attributes, tags.Memberships)
	var got []TagX
	require.NoError(t, marshallreflect.Unmarshal(readers, &got, marshallreflect.MapLookup(nil)))
	require.Equal(t, rows, got)
}
