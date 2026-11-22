package chstore

import (
	"context"
	"iter"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/db/clickhouse/jackstay"
	"github.com/stergiotis/boxer/public/identity/identgen/leased/memalloc"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema/dml"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// RoundTripReferenceTable is where [RoundTrip] keeps the rows as written,
// untransformed, to compare the read relation against.
const RoundTripReferenceTable = "roundtrip_reference.facts"

// RoundTripOptions parameterizes [RoundTrip].
type RoundTripOptions struct {
	// Transform is the deployment's; nil takes the one registered for
	// boxer.facts.
	Transform recordstore.InsertTransformI
	// ReadTable is the relation the rows are read back through, database-
	// qualified; empty takes factsschema.ReadTable().
	ReadTable string
}

// RoundTripMismatch is one facts column whose values differ between the
// rows written and the rows read back: Rows rows, counted from both sides.
type RoundTripMismatch struct {
	Column string
	Rows   uint64
}

// RoundTripReport is what [RoundTrip] found.
type RoundTripReport struct {
	// Kinds are the kinds a row was written for.
	Kinds []string
	// Stores are the stores over the facts table that contributed rows of
	// their own components (recordstore.RegisterSampleRows): the ones the
	// binary links. StoreRows is how many rows they wrote.
	Stores    []string
	StoreRows uint64
	// Written is how many rows were written; Read how many the read
	// relation returned.
	Written uint64
	Read    uint64
	// Target is the table the transform inserted into; ReadTable the
	// relation read back.
	Target    string
	ReadTable string
	// Columns is the read relation judged by its columns (ADR-0402 §SD4):
	// a facts column it lacks or returns under another type.
	Columns recordstore.SchemaVerdict
	// Mismatches are the columns whose values did not survive.
	Mismatches []RoundTripMismatch
}

// Ok reports a round trip in which every row and every column survived.
func (inst RoundTripReport) Ok() bool {
	return inst.Columns.IsOk() && len(inst.Mismatches) == 0 && inst.Written == inst.Read
}

// RoundTrip checks that a deployment's write transform and its read
// relation agree (ADR-0402 §SD6): it writes one row of every kind the
// runtime vocabulary registers through the transform, reads the rows back
// through the read relation, and compares every facts column by row id.
//
// exec must reach a scratch server, never one holding real facts: the
// caller has created the transform's target table and the read relation on
// it, and RoundTrip creates [RoundTripReferenceTable] there for the rows as
// written. `boxer schema roundtrip` runs it on clickhouse-local in a fresh
// directory. Its inserts run under recordstore.WithTransformApplied, so an
// exec that consults the registry sends them as they are: the reference rows
// untransformed, the transformed rows once.
//
// The rows of the kinds this store writes come from its own encoders with
// every optional field set. Every other kind gets a synthetic row: the
// kind's membership on the symbol section and one attribute in each section
// of the facts table, with a low-cardinality, a mixed and a high-cardinality
// membership where the section has them. Beside those, every generated
// store over the facts table that the binary links writes one entity per
// component of its own, under its own memberships
// (recordstore.RegisterSampleRows), so a transform that treats rows
// differently by a membership a store writes meets that membership here.
// Those rows hold sample values, not ones their writers would produce.
func RoundTrip(ctx context.Context, exec recordstore.ExecutorI, opts RoundTripOptions) (report RoundTripReport, err error) {
	t := opts.Transform
	if t == nil {
		var ok bool
		if t, ok = recordstore.RegisteredInsertTransform(factsschema.QualifiedTableName); !ok {
			err = eh.Errorf("chstore: round trip: no insert transform is registered for boxer.facts and none was given")
			return
		}
	}
	report.ReadTable = opts.ReadTable
	if report.ReadTable == "" {
		if report.ReadTable, err = factsschema.ReadTable(); err != nil {
			err = eh.Errorf("chstore: round trip: %w", err)
			return
		}
	}
	if err = recordstore.CheckTableRef(report.ReadTable); err != nil {
		err = eh.Errorf("chstore: round trip: read table: %w", err)
		return
	}
	refDb, refTable := recordstore.SplitTableRef(RoundTripReferenceTable)
	refCfg := Config{Database: refDb, Table: refTable}
	stmts, err := tableStatements(refCfg, "")
	if err != nil {
		err = eh.Errorf("chstore: round trip: %w", err)
		return
	}
	for _, s := range stmts {
		if err = exec.Exec(ctx, s); err != nil {
			err = eh.Errorf("chstore: round trip: create the reference table: %w", err)
			return
		}
	}

	alloc := memory.NewGoAllocator()
	records, kinds, err := roundTripRows(ctx, alloc)
	defer func() {
		for _, r := range records {
			r.Release()
		}
	}()
	if err != nil {
		return
	}
	report.Kinds = kinds
	// The rows the generated stores over the facts table write, under the
	// memberships their own readers select by.
	for _, w := range recordstore.SampleRowWriters(factsschema.QualifiedTableName) {
		sink := &sampleSink{}
		werr := w.Write(ctx, sink)
		records = append(records, sink.records...)
		if werr != nil {
			err = eb.Build().Str("store", w.Name).Errorf("chstore: round trip: the store's sample rows: %w", werr)
			return
		}
		report.Stores = append(report.Stores, w.Name)
		for _, r := range sink.records {
			report.StoreRows += uint64(r.NumRows())
		}
	}
	for _, r := range records {
		report.Written += uint64(r.NumRows())
	}
	ictx := recordstore.WithTransformApplied(ctx)
	if err = exec.InsertArrow(ictx, RoundTripReferenceTable, records); err != nil {
		err = eh.Errorf("chstore: round trip: insert the reference rows: %w", err)
		return
	}
	target, out, release, err := recordstore.TransformInsert(t, factsschema.QualifiedTableName, records)
	if err != nil {
		err = eh.Errorf("chstore: round trip: %w", err)
		return
	}
	report.Target = target
	err = exec.InsertArrow(ictx, target, out)
	release()
	if err != nil {
		err = eb.Build().Str("target", target).Errorf("chstore: round trip: insert the transformed rows: %w", err)
		return
	}

	contract, err := SchemaContract(refCfg)
	if err != nil {
		return
	}
	contract.Table, contract.Stamp, contract.Layout, contract.LayoutRecipe, contract.ColumnsOnly = report.ReadTable, "", recordstore.TableLayout{}, "", true
	report.Columns = recordstore.JudgeTable(ctx, exec, contract)
	if report.Columns.Verdict == recordstore.SchemaVerdictAbsent || report.Columns.Verdict == recordstore.SchemaVerdictUnchecked {
		err = eb.Build().Str("readTable", report.ReadTable).Str("summary", report.Columns.Summary()).Errorf("chstore: round trip: the read relation cannot be read")
		return
	}
	if report.Read, err = roundTripCount(ctx, exec, report.ReadTable); err != nil {
		return
	}
	report.Mismatches, err = roundTripCompare(ctx, exec, contract.Columns, report.Columns.Missing, report.ReadTable)
	return
}

// roundTripCount is the number of rows in table.
func roundTripCount(ctx context.Context, exec recordstore.ExecutorI, table string) (n uint64, err error) {
	err = roundTripRowsOf(ctx, exec, "SELECT toString(count()) AS n FROM "+table, func(vals []string) {
		n, _ = strconv.ParseUint(vals[0], 10, 64)
	})
	return
}

// roundTripCompare compares every facts column the read relation returns
// against the reference rows, joined by row id, in both directions.
func roundTripCompare(ctx context.Context, exec recordstore.ExecutorI, cols []recordstore.ColumnSpec, missing []string, readTable string) (mismatches []RoundTripMismatch, err error) {
	idCol := ""
	for _, c := range cols {
		if strings.HasPrefix(c.Name, "id:id:") {
			idCol = c.Name
			break
		}
	}
	if idCol == "" {
		err = eh.Errorf("chstore: round trip: the facts columns name no row id")
		return
	}
	if slices.Contains(missing, idCol) {
		err = eb.Build().Str("column", idCol).Errorf("chstore: round trip: the read relation lacks the row id, so no row can be compared")
		return
	}
	id := jackstay.QuoteIdent(idCol)
	parts := make([]string, 0, 2*len(cols))
	for _, c := range cols {
		if c.Name == idCol || slices.Contains(missing, c.Name) {
			continue
		}
		q := jackstay.QuoteIdent(c.Name)
		name := jackstay.QuoteString(c.Name)
		for _, dir := range [2][2]string{{RoundTripReferenceTable, readTable}, {readTable, RoundTripReferenceTable}} {
			parts = append(parts, "SELECT "+name+" AS c, count() AS k FROM (SELECT "+id+", "+q+" FROM "+dir[0]+" EXCEPT SELECT "+id+", "+q+" FROM "+dir[1]+")")
		}
	}
	if len(parts) == 0 {
		return
	}
	sql := "SELECT c, toString(sum(k)) AS n FROM (" + strings.Join(parts, " UNION ALL ") + ") GROUP BY c HAVING sum(k) > 0 ORDER BY c"
	err = roundTripRowsOf(ctx, exec, sql, func(vals []string) {
		n, _ := strconv.ParseUint(vals[1], 10, 64)
		mismatches = append(mismatches, RoundTripMismatch{Column: vals[0], Rows: n})
	})
	return
}

// roundTripRowsOf runs sql and hands each row's columns, as text, to fn.
func roundTripRowsOf(ctx context.Context, exec recordstore.ExecutorI, sql string, fn func(vals []string)) (err error) {
	sql += " SETTINGS output_format_arrow_string_as_string=1, output_format_arrow_low_cardinality_as_dictionary=0"
	for rec, qerr := range exec.QueryArrow(ctx, sql) {
		if qerr != nil {
			return eh.Errorf("chstore: round trip: query: %w", qerr)
		}
		vals := make([]string, rec.NumCols())
		for i := range int(rec.NumRows()) {
			for j := range vals {
				vals[j], _ = stringAt(rec.Column(j), i)
			}
			fn(vals)
		}
		rec.Release()
	}
	return
}

// captureSink is an executor that keeps the batches inserted into it, for
// encoding rows without a server.
type captureSink struct {
	records []arrow.RecordBatch
}

func (inst *captureSink) Exec(context.Context, string) error {
	return eh.Errorf("chstore: the capture sink runs no statement")
}

func (inst *captureSink) QueryArrow(context.Context, string) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) {
		yield(nil, eh.Errorf("chstore: the capture sink answers no query"))
	}
}

func (inst *captureSink) InsertArrow(_ context.Context, _ string, records []arrow.RecordBatch) error {
	for _, r := range records {
		r.Retain()
	}
	inst.records = append(inst.records, records...)
	return nil
}

// sampleSink is the executor a store writes its sample rows into: it keeps
// the batches, takes any statement as done and answers any query with no
// rows, as a table nothing was written to would.
type sampleSink struct {
	records []arrow.RecordBatch
}

var _ recordstore.InsertSettingsI = (*sampleSink)(nil)

func (inst *sampleSink) Exec(context.Context, string) error { return nil }

func (inst *sampleSink) QueryArrow(context.Context, string) iter.Seq2[arrow.RecordBatch, error] {
	return func(func(arrow.RecordBatch, error) bool) {}
}

func (inst *sampleSink) InsertArrow(_ context.Context, _ string, records []arrow.RecordBatch) error {
	for _, r := range records {
		r.Retain()
	}
	inst.records = append(inst.records, records...)
	return nil
}

func (inst *sampleSink) InsertArrowWith(ctx context.Context, table string, records []arrow.RecordBatch, _ recordstore.InsertSettings) (bool, error) {
	return true, inst.InsertArrow(ctx, table, records)
}

// roundTripTs is the time every round-trip row carries.
var roundTripTs = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

// roundTripRows encodes one row of every kind of the runtime vocabulary:
// the hand-written kinds through this store's encoders, every other one
// synthetic. The caller releases the records.
func roundTripRows(ctx context.Context, alloc memory.Allocator) (records []arrow.RecordBatch, kinds []string, err error) {
	sink := &captureSink{}
	st := &Store{cfg: Config{Database: factsschema.DatabaseName, Table: factsschema.TableName, RunId: "roundtrip-run"}, allocator: alloc, ship: sink}
	if err = st.ids.init(nil, st.leaseLayout(), RowIds{Allocator: memalloc.NewAllocator()}); err != nil {
		err = eh.Errorf("chstore: round trip: %w", err)
		return
	}
	defer func() {
		records = sink.records
	}()
	written := map[uint64]bool{}
	ts := roundTripTs
	steps := []struct {
		kind  uint64
		write func() error
	}{
		{vocab.MembKindGrant.GetId().Value(), func() (werr error) {
			_, werr = st.WriteGrant(ctx, factsstore.GrantRow{AppId: "roundtrip.app", InstanceKey: 7, Pattern: "runtime.>", Direction: app.CapDirectionBoth,
				Reason: "round trip", Sticky: true, GrantedVia: "operator", Ts: ts, ExpiresAt: ts.Add(time.Hour)})
			return
		}},
		{vocab.MembKindLog.GetId().Value(), func() (werr error) {
			_, werr = st.WriteLog(ctx, factsstore.LogRow{AppId: "roundtrip.app", InstanceKey: 7, Level: "warn", Message: "round trip", Caller: "roundtrip.go:1",
				Error: "an error", Stack: "a stack", Service: "roundtrip", Ts: ts, Fields: []factsstore.LogField{
					{Name: "s", Kind: factsstore.LogFieldKindString, Str: "text"},
					{Name: "i", Kind: factsstore.LogFieldKindInt, Int: -3},
					{Name: "u", Kind: factsstore.LogFieldKindUint, Uint: 3},
					{Name: "f", Kind: factsstore.LogFieldKindFloat, Float: 1.5},
					{Name: "b", Kind: factsstore.LogFieldKindBool, Bool: true},
					{Name: "y", Kind: factsstore.LogFieldKindBytes, Bytes: []byte{0, 1, 2}},
					{Name: "t", Kind: factsstore.LogFieldKindTime, Time: ts},
				}})
			return
		}},
		{vocab.MembKindRuntimeRun.GetId().Value(), func() (werr error) {
			_, werr = st.WriteRuntimeStart(ctx, factsstore.RuntimeStartRow{RunId: "roundtrip-run", Hostname: "host", Pid: 42, GoVersion: "go", VcsRevision: "rev",
				VcsModified: true, VcsBuildInfo: "info", ModulePath: "module", BuildId: "build", Driver: "driver", DriverJob: "job", DriverAttempt: 2,
				DriverJobKind: "kind", Task: "task", TaskEpoch: 3, Call: "call", Ts: ts})
			return
		}},
		{vocab.MembKindRuntimeHeartbeat.GetId().Value(), func() (werr error) {
			_, werr = st.WriteRuntimeHeartbeat(ctx, factsstore.HeartbeatRow{RunId: "roundtrip-run", Ts: ts})
			return
		}},
		{vocab.MembKindAppLifecycle.GetId().Value(), func() (werr error) {
			_, werr = st.WriteAppLifecycle(ctx, factsstore.AppLifecycleRow{RunId: "roundtrip-run", AppId: "roundtrip.app", TileKey: 7,
				Phase: factsstore.AppLifecyclePhaseStopped, StopReason: "closed", Ts: ts})
			return
		}},
		{vocab.MembKindLaunch.GetId().Value(), func() (werr error) {
			_, werr = st.WriteLaunch(ctx, factsstore.LaunchRow{RunId: "roundtrip-run", CallerAppId: "roundtrip.caller", TargetAppId: "roundtrip.app",
				TileKey: 7, ConfigKind: "kind", Config: []byte{1, 2, 3}, ResolvedFromKind: true, Ts: ts})
			return
		}},
	}
	for _, s := range steps {
		if err = s.write(); err != nil {
			err = eh.Errorf("chstore: round trip: encode: %w", err)
			return
		}
		written[s.kind] = true
	}

	type kindOf struct {
		name string
		id   uint64
	}
	var all []kindOf
	for name, k := range vocab.NkRegistry.IterateAll() {
		if strings.HasPrefix(string(name), "runtime-kind-") {
			all = append(all, kindOf{name: string(name), id: k.GetId().Value()})
		}
	}
	slices.SortFunc(all, func(a, b kindOf) int { return strings.Compare(a.name, b.name) })
	ent := dml.NewInEntityFacts(alloc, len(all))
	synthetic := 0
	for _, k := range all {
		kinds = append(kinds, k.name)
		if written[k.id] {
			continue
		}
		var id uint64
		if id, err = st.mintId(ctx); err != nil {
			return
		}
		synthesizeRow(ent, id, k.name, k.id)
		if err = ent.CommitEntity(); err != nil {
			err = eb.Build().Str("kind", k.name).Errorf("chstore: round trip: encode: %w", err)
			return
		}
		synthetic++
	}
	if synthetic > 0 {
		var recs []arrow.RecordBatch
		if recs, err = ent.TransferRecords(nil); err != nil {
			err = eh.Errorf("chstore: round trip: transfer records: %w", err)
			return
		}
		sink.records = append(sink.records, recs...)
	}
	return
}

// synthesizeRow writes one row of kind into ent: the kind on the symbol
// section with the origin's slots, and one attribute in every section, each
// carrying the kind as a low-cardinality membership and, where the section
// has them, a mixed and a high-cardinality one.
func synthesizeRow(ent *dml.InEntityFacts, id uint64, name string, kind uint64) {
	const hr = uint64(4242)
	p := []byte("param")
	ent.BeginEntity().SetId(id, naturalKeyFor("roundtrip", app.AppIdT(name), nil, nil)).SetTimestamp(roundTripTs).SetLifecycle(roundTripTs.Add(time.Hour))

	sym := ent.GetSectionSymbol()
	sym.BeginAttribute(name).AddMembershipLowCardRef(kind).EndAttribute()
	sym.BeginAttribute("roundtrip-run").AddMembershipLowCardRef(vocab.MembRuntimeRun.GetId().Value()).EndAttribute()
	sym.BeginAttribute("roundtrip.app").AddMembershipLowCardRef(vocab.MembRuntimeApp.GetId().Value()).EndAttribute()
	sym.BeginAttribute("symbol").AddMembershipMixedLowCardRef(kind, p).AddMembershipHighCardRef(hr).EndAttribute()
	sym.EndSection()

	ent.GetSectionSymbolArray().BeginAttributeSingle("symbol-array").AddMembershipLowCardRef(kind).AddMembershipMixedLowCardRef(kind, p).AddMembershipHighCardRef(hr).EndAttribute().EndSection()
	ent.GetSectionStringArray().BeginAttributeSingle("string").AddMembershipLowCardRef(kind).AddMembershipMixedLowCardRef(kind, p).AddMembershipHighCardRef(hr).EndAttribute().EndSection()
	ent.GetSectionTextArray().BeginAttributeSingle("text").AddMembershipLowCardRef(kind).AddMembershipMixedLowCardRef(kind, p).AddMembershipHighCardRef(hr).EndAttribute().EndSection()
	ent.GetSectionBlobArray().BeginAttributeSingle([]byte{0, 1, 2}).AddMembershipLowCardRef(kind).AddMembershipMixedLowCardRef(kind, p).AddMembershipHighCardRef(hr).EndAttribute().EndSection()
	ent.GetSectionBool().BeginAttribute(true).AddMembershipLowCardRef(kind).AddMembershipMixedLowCardRef(kind, p).AddMembershipHighCardRef(hr).EndAttribute().EndSection()
	ent.GetSectionF32Array().BeginAttributeSingle(1.5).AddMembershipLowCardRef(kind).AddMembershipMixedLowCardRef(kind, p).AddMembershipHighCardRef(hr).EndAttribute().EndSection()
	ent.GetSectionF64Array().BeginAttributeSingle(2.5).AddMembershipLowCardRef(kind).AddMembershipMixedLowCardRef(kind, p).AddMembershipHighCardRef(hr).EndAttribute().EndSection()
	ent.GetSectionI8Array().BeginAttributeSingle(-8).AddMembershipLowCardRef(kind).AddMembershipMixedLowCardRef(kind, p).AddMembershipHighCardRef(hr).EndAttribute().EndSection()
	ent.GetSectionI16Array().BeginAttributeSingle(-16).AddMembershipLowCardRef(kind).AddMembershipMixedLowCardRef(kind, p).AddMembershipHighCardRef(hr).EndAttribute().EndSection()
	ent.GetSectionI32Array().BeginAttributeSingle(-32).AddMembershipLowCardRef(kind).AddMembershipMixedLowCardRef(kind, p).AddMembershipHighCardRef(hr).EndAttribute().EndSection()
	ent.GetSectionI64Array().BeginAttributeSingle(-64).AddMembershipLowCardRef(kind).AddMembershipMixedLowCardRef(kind, p).AddMembershipHighCardRef(hr).EndAttribute().EndSection()
	ent.GetSectionU8Array().BeginAttributeSingle(8).AddMembershipLowCardRef(kind).AddMembershipMixedLowCardRef(kind, p).AddMembershipHighCardRef(hr).EndAttribute().EndSection()
	ent.GetSectionU16Array().BeginAttributeSingle(16).AddMembershipLowCardRef(kind).AddMembershipMixedLowCardRef(kind, p).AddMembershipHighCardRef(hr).EndAttribute().EndSection()
	ent.GetSectionU32Array().BeginAttributeSingle(32).AddMembershipLowCardRef(kind).AddMembershipMixedLowCardRef(kind, p).AddMembershipHighCardRef(hr).EndAttribute().EndSection()
	ent.GetSectionU64Array().BeginAttributeSingle(64).AddMembershipLowCardRef(kind).AddMembershipMixedLowCardRef(kind, p).AddMembershipHighCardRef(hr).EndAttribute().EndSection()
	ent.GetSectionU32Set().BeginAttribute().AddToContainer(1).AddToContainer(2).AddMembershipLowCardRef(kind).AddMembershipMixedLowCardRef(kind, p).AddMembershipHighCardRef(hr).EndAttribute().EndSection()
	ent.GetSectionU64Set().BeginAttribute().AddToContainer(3).AddToContainer(4).AddMembershipLowCardRef(kind).AddMembershipMixedLowCardRef(kind, p).AddMembershipHighCardRef(hr).EndAttribute().EndSection()
	ent.GetSectionU32Range().BeginAttribute(5, 9).AddMembershipLowCardRef(kind).AddMembershipMixedLowCardRef(kind, p).AddMembershipHighCardRef(hr).EndAttribute().EndSection()
	ent.GetSectionTimeArray().BeginAttributeSingle(roundTripTs).AddMembershipLowCardRef(kind).AddMembershipMixedLowCardRef(kind, p).AddMembershipHighCardRef(hr).EndAttribute().EndSection()
	ent.GetSectionForeignKey().BeginAttribute(id).AddMembershipLowCardRef(kind).EndAttribute().EndSection()
}
