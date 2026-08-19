package meshdemo_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	factsdml "github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/meshdemo"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/ra"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/readback"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/assignments"
	raruntime "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// The agent's rows: three hosts, written before HostLoad existed.
var agentRows = []meshdemo.FleetSample{
	{Id: 1, NaturalKey: []byte("box-a"), Kind: "fleetSample", Host: "box-a", Region: "eu-central", CpuPercent: 41, UptimeSeconds: 900},
	{Id: 2, NaturalKey: []byte("box-b"), Kind: "fleetSample", Host: "box-b", Region: "eu-central", CpuPercent: 7, UptimeSeconds: 86_400},
	{Id: 3, NaturalKey: []byte("box-c"), Kind: "fleetSample", Host: "box-c", Region: "us-east", CpuPercent: 96, UptimeSeconds: 120},
}

func registryLookup(t *testing.T) marshallreflect.MapLookup {
	t.Helper()
	ids, err := storegen.MembershipIds(meshdemo.NkRegistry)
	require.NoError(t, err)
	lookup, err := marshallreflect.NewRegistryLookup(ids)
	require.NoError(t, err)
	return lookup
}

// The ids the two domains use are equal, and nothing at the boundary compares
// them: the agent's store baked its map at generation time, and capacity
// planning resolves the same names at run time. Both went to the registry,
// which is the entire coordination mechanism (ADR-0183 D0/D1).
func TestBakedAndRuntimeIdsAgree(t *testing.T) {
	ids, err := storegen.MembershipIds(meshdemo.NkRegistry)
	require.NoError(t, err)

	baked := meshdemo.FleetMembershipIds["FleetSample"]
	require.NotEmpty(t, baked)
	for _, name := range []string{"meshHost", "meshCpuPercent"} {
		assert.Equal(t, baked[name], ids[name],
			"%s: the id the agent baked and the id a late reader resolves must be one number", name)
	}
	assert.Equal(t, meshdemo.MembHost.GetId().Value(), ids["meshHost"])
	assert.Equal(t, meshdemo.MembCpuPercent.GetId().Value(), ids["meshCpuPercent"])

	// The membership HostLoad names and nobody writes resolves too: a
	// vocabulary is not a list of what has been written.
	assert.Equal(t, meshdemo.MembDrainedAt.GetId().Value(), ids["meshDrainedAt"])
}

// The centerpiece, in memory: rows written by the agent's DTO decode into the
// component another domain formulated afterwards, with no artefact in common.
//
// The write side here deliberately uses the reflect path rather than the
// generated store, so this test shares no generated code between writer and
// reader at all — the ids come from the registry on both sides and that is
// the only thing connecting them.
func TestLateComponentDecodesRowsTheAgentWrote(t *testing.T) {
	lookup := registryLookup(t)

	dml := factsdml.NewInEntityFacts(memory.NewGoAllocator(), len(agentRows))
	require.NoError(t, marshallreflect.Marshal(dml, agentRows, lookup))
	recs, err := dml.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()

	readers, release := factsSectionReaders(t, recs[0])
	defer release()

	var got []meshdemo.HostLoad
	require.NoError(t, marshallreflect.Unmarshal(readers, &got, lookup))
	require.Len(t, got, len(agentRows))

	for i, want := range agentRows {
		assert.Equal(t, want.Host, got[i].Host, "row %d host", i)
		assert.Equal(t, want.CpuPercent, got[i].CpuPercent, "row %d cpu", i)
		// The slot nobody writes reads absent — a legal state, not a
		// decode failure. A component that required every slot could not
		// be formulated over data it did not commission.
		assert.False(t, got[i].DrainedAt.Has, "row %d drainedAt must read absent", i)
	}
}

// The same rows, located by SQL. Capacity planning builds its plan from the
// struct at run time, resolves ids from the registry, and generates the
// predicate that finds its component's rows — then decodes them through the
// read contract. No generated artefact for HostLoad exists anywhere.
//
// The predicate is the generated Filter, which is built-ins only by contract
// (ADR-0100 S2), so this runs against a bare clickhouse-local with no leeway
// helper pack installed.
func TestLateComponentFindsTheAgentsRowsBySql(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	ctx := context.Background()
	for _, stmt := range splitStatements(t, "facts_ddl_clickhouse.out.sql") {
		require.NoError(t, exec.Exec(ctx, stmt))
	}

	// --- domain A: the agent ingests through its generated store. ---
	store := meshdemo.NewFleetStore(exec, nil, meshdemo.FleetStoreConfig{})
	defer store.Close()
	require.NoError(t, store.IngestFleetSample(time.Unix(1_700_000_000, 0).UTC(), agentRows))
	n, err := store.Flush(ctx)
	require.NoError(t, err)
	require.Equal(t, len(agentRows), n)

	// --- a third domain writes rows that are none of HostLoad's business. ---
	//
	// Without these the test would pass over a table where every row happens
	// to be a fleet sample, which would say nothing about whether the
	// component's predicate selects anything. The table is shared; a reader
	// that cannot ignore its neighbours has not demonstrated the mesh.
	writeRegionNotes(t, ctx, exec, registryLookup(t))
	require.Equal(t, len(agentRows)+2, countRows(t, ctx, exec),
		"both domains' rows must actually be in the table, or the read below proves nothing")

	// --- domain B: a struct, a registry, and a plan built at run time. ---
	plan, err := marshallreflect.PlanFor[meshdemo.HostLoad]()
	require.NoError(t, err)
	ids, err := storegen.MembershipIds(meshdemo.NkRegistry)
	require.NoError(t, err)
	lookup, err := marshallreflect.NewRegistryLookup(ids)
	require.NoError(t, err)
	artefacts, err := readback.NewGenerator(factsIR(t), readback.NewLookupResolver(lookup)).Generate(plan)
	require.NoError(t, err)
	require.NotEmpty(t, artefacts.Filter)

	sql := "SELECT * FROM " + factsschema.DatabaseName + "." + factsschema.TableName +
		" WHERE " + artefacts.Filter +
		` ORDER BY "id:id:u64:47::0:" ASC` +
		" SETTINGS output_format_arrow_string_as_string=1, output_format_arrow_low_cardinality_as_dictionary=0"

	var got []meshdemo.HostLoad
	for rec, rerr := range exec.QueryArrow(ctx, sql) {
		require.NoError(t, rerr)
		readers, release := factsSectionReaders(t, rec)
		var batch []meshdemo.HostLoad
		uerr := marshallreflect.Unmarshal(readers, &batch, lookup)
		release()
		rec.Release()
		require.NoError(t, uerr)
		got = append(got, batch...)
	}

	require.Len(t, got, len(agentRows),
		"the predicate must find the agent's rows and leave the other domain's alone")
	for i, want := range agentRows {
		assert.Equal(t, want.Host, got[i].Host, "row %d host", i)
		assert.Equal(t, want.CpuPercent, got[i].CpuPercent, "row %d cpu", i)
		assert.False(t, got[i].DrainedAt.Has, "row %d drainedAt must read absent", i)
	}
}

// regionNote is a third domain's row: same table, same vocabulary, none of
// HostLoad's slots. It exists so the read above has something to not match.
type regionNote struct {
	_ struct{} `kind:"regionNote"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Region string `lw:"meshRegion,symbol"`
}

func writeRegionNotes(t *testing.T, ctx context.Context, exec *chexec.LocalExecutor, lookup marshallreflect.MapLookup) {
	t.Helper()
	rows := []regionNote{
		{Id: 90, NaturalKey: []byte("eu-central"), Ts: time.Unix(1_700_000_001, 0).UTC(), Region: "eu-central"},
		{Id: 91, NaturalKey: []byte("us-east"), Ts: time.Unix(1_700_000_002, 0).UTC(), Region: "us-east"},
	}
	dml := factsdml.NewInEntityFacts(memory.NewGoAllocator(), len(rows))
	require.NoError(t, marshallreflect.Marshal(dml, rows, lookup))
	recs, err := dml.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()
	require.NoError(t, exec.InsertArrow(ctx, factsschema.DatabaseName+"."+factsschema.TableName, recs))
}

// HostLoad never names the agent's kind marker, and the demonstration above
// works because of that rather than in spite of it: a row satisfies a
// component when it carries the component's slots. Gating on the marker would
// mean no row written before a component existed could ever satisfy it, which
// is the property this whole package is about.
func TestLateComponentClaimsNoKindMarker(t *testing.T) {
	plan, err := marshallreflect.PlanFor[meshdemo.HostLoad]()
	require.NoError(t, err)
	for _, f := range plan.Fields {
		assert.NotEqual(t, "meshKindFleetSample", f.LWMembership,
			"the late component must not depend on the writer having declared itself")
	}
}

// The demo vocabulary keeps a committed assignment table like every other
// version-controlled vocabulary — the discipline is the mechanism, so an
// example that skipped it would be demonstrating something else.
func TestMembershipAssignmentsMatchTheGolden(t *testing.T) {
	if os.Getenv(assignments.RegenEnvVar) != "" {
		require.NoError(t, assignments.WriteGoldenFile(".", meshdemo.NkRegistry))
		t.Skip("golden rewritten; unset " + assignments.RegenEnvVar + " to compare against it")
	}
	differences, err := assignments.CompareToGoldenFile(".", meshdemo.NkRegistry)
	require.NoError(t, err)
	assert.Empty(t, differences,
		"the vocabulary and its committed table disagree; a `!` line is a re-pointed id, not a new membership")
}

// factsSectionReaders binds the boxer.facts read-access readers HostLoad's
// sections need, in the shape marshallreflect.Unmarshal takes them.
func factsSectionReaders(t *testing.T, rec arrow.RecordBatch) (*marshallreflect.SectionReaders, func()) {
	t.Helper()
	idR := ra.NewReadAccessFactsPlainEntityIdAttributes()
	tsR := ra.NewReadAccessFactsPlainEntityTimestampAttributes()
	symbolR := ra.NewReadAccessFactsTaggedSymbol()
	u8R := ra.NewReadAccessFactsTaggedU8Array()
	u64R := ra.NewReadAccessFactsTaggedU64Array()
	for _, r := range []interface {
		LoadFromRecord(raruntime.RecordI) error
	}{idR, tsR, symbolR, u8R, u64R} {
		require.NoError(t, r.LoadFromRecord(rec))
	}
	readers := marshallreflect.NewSectionReaders(idR.ValueId.Len()).
		PlainColumn("id", idR.ValueId).
		PlainColumn("naturalKey", idR.ValueNaturalKey).
		PlainColumn("ts", tsR.ValueTs).
		Section("symbol", symbolR.GetAttributes(), symbolR.GetMemberships()).
		Section("u8Array", u8R.GetAttributes(), u8R.GetMemberships()).
		Section("u64Array", u64R.GetAttributes(), u64R.GetMemberships())
	return readers, func() {
		idR.Release()
		tsR.Release()
		symbolR.Release()
		u8R.Release()
		u64R.Release()
	}
}

// factsIR loads the boxer.facts schema the way a reader that only knows the
// table would: from the schema definition, not from anything the agent emitted.
func factsIR(t *testing.T) *readback.InformationRetrieval {
	t.Helper()
	manip, err := factsschema.GetSchemaInManipulator()
	require.NoError(t, err)
	tblDesc, err := manip.BuildTableDesc()
	require.NoError(t, err)
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tblDesc, clickhouse.NewTechnologySpecificCodeGenerator()))
	info := readback.NewInformationRetrieval(conv)
	require.NoError(t, info.LoadTable(ir, factsschema.TableRowConfig))
	return info
}

// countRows reads the table's row count, so a test that claims to have
// written rows can say so rather than assume it.
func countRows(t *testing.T, ctx context.Context, exec *chexec.LocalExecutor) (n int) {
	t.Helper()
	for rec, err := range exec.QueryArrow(ctx, "SELECT toUInt64(count()) AS n FROM "+factsschema.DatabaseName+"."+factsschema.TableName) {
		require.NoError(t, err)
		col, ok := rec.Column(0).(*array.Uint64)
		require.True(t, ok, "count column is %s", rec.Column(0).DataType())
		require.Positive(t, col.Len())
		n = int(col.Value(0))
		rec.Release()
	}
	return
}

func splitStatements(t *testing.T, path string) (stmts []string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	for s := range strings.SplitSeq(string(raw), ";") {
		if strings.TrimSpace(s) != "" {
			stmts = append(stmts, s)
		}
	}
	return
}
