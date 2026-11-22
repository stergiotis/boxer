//go:build integration

package chstore

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore/chstoretest"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/ipcexec"
)

// writeMixedRows writes rows of several kinds through s, across two months,
// with every column family in use: text, strings, symbols, numbers, bytes
// holding bytes that are not text, booleans, times and an expiry.
func writeMixedRows(t *testing.T, s *Store) (n int) {
	t.Helper()
	ctx := context.Background()
	november := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)
	october := november.AddDate(0, -1, 0)
	for i, at := range []time.Time{october, october.Add(time.Hour), november, november.Add(time.Hour)} {
		_, err := s.WriteLog(ctx, factsstore.LogRow{
			AppId: "loader", Level: "info", Caller: "c.go:1", Service: "svc", Message: "row with every field", Error: "an error", Stack: "a\nstack", Ts: at, InstanceKey: uint64(i),
			Fields: []factsstore.LogField{
				{Name: "s", Kind: factsstore.LogFieldKindString, Str: "text"},
				{Name: "i", Kind: factsstore.LogFieldKindInt, Int: -7},
				{Name: "u", Kind: factsstore.LogFieldKindUint, Uint: 7},
				{Name: "f", Kind: factsstore.LogFieldKindFloat, Float: 1.5},
				{Name: "b", Kind: factsstore.LogFieldKindBool, Bool: true},
				{Name: "y", Kind: factsstore.LogFieldKindBytes, Bytes: []byte{0x00, 0xff, 0xfe, 0x80}},
				{Name: "t", Kind: factsstore.LogFieldKindTime, Time: at},
			},
		})
		require.NoError(t, err)
		_, err = s.WriteLaunch(ctx, factsstore.LaunchRow{TargetAppId: "loader", CallerAppId: app.AppIdT("caller"), RunId: "run", TileKey: uint64(i), Config: []byte{0xc3, 0x28, 0x00}, ConfigKind: "k", ResolvedFromKind: true, Ts: at})
		require.NoError(t, err)
		_, err = s.WriteGrant(ctx, factsstore.GrantRow{AppId: "loader", Pattern: "a.>", Reason: "because", Sticky: true, ExpiresAt: at.Add(24 * time.Hour), Ts: at})
		require.NoError(t, err)
		n += 3
	}
	return
}

// sameRows requires that a and b hold the same rows, every column.
func sameRows(t *testing.T, s *Store, a string, b string) {
	t.Helper()
	for _, dir := range [2][2]string{{a, b}, {b, a}} {
		assert.Equal(t, "0", scalar(t, s, "SELECT count() FROM (SELECT * FROM "+dir[0]+" EXCEPT SELECT * FROM "+dir[1]+")"), "rows of %s that %s lacks", dir[0], dir[1])
	}
}

// Rows of a facts table moved aside are loaded into the facts table through
// the executor and arrive unchanged in every column, bytes that are not
// text included; a load into a window that holds rows is refused, a load
// onto itself too, and a window loads its rows only.
func TestServer_Load_FromATable(t *testing.T) {
	cfg := serverScratch(t, "load")
	s := serverStore(t, cfg)
	ctx := context.Background()
	require.NoError(t, s.SetupTable(ctx, ""))
	rows := writeMixedRows(t, s)
	before := cfg.Database + ".facts_before"
	facts := cfg.Database + ".facts"
	require.NoError(t, s.exec.Exec(ctx, "RENAME TABLE "+facts+" TO "+before))
	require.NoError(t, s.SetupTable(ctx, ""))

	// Part by part: October first.
	november := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	var progress []uint64
	report, err := LoadFacts(ctx, s.exec, cfg, LoadOptions{FromTable: before, Until: november, BlockRows: 4,
		Progress: func(r LoadReport) { progress = append(progress, r.Rows) }})
	require.NoError(t, err)
	assert.EqualValues(t, rows/2, report.Rows)
	assert.EqualValues(t, 2, report.Batches, "six rows in blocks of four")
	assert.Equal(t, []uint64{4, 6}, progress)
	assert.EqualValues(t, 0, report.Before)
	assert.EqualValues(t, rows/2, report.After)

	_, err = LoadFacts(ctx, s.exec, cfg, LoadOptions{FromTable: before, Until: november})
	require.ErrorContains(t, err, "already hold rows", "the same window again would land twice")
	_, err = LoadFacts(ctx, s.exec, cfg, LoadOptions{FromTable: facts})
	require.ErrorContains(t, err, "onto itself")
	_, err = LoadFacts(ctx, s.exec, cfg, LoadOptions{})
	require.Error(t, err)

	report, err = LoadFacts(ctx, s.exec, cfg, LoadOptions{FromTable: before, Since: november})
	require.NoError(t, err)
	assert.EqualValues(t, rows/2, report.Rows)
	sameRows(t, s, before, facts)
	assert.Equal(t, "2", scalar(t, s, "SELECT uniqExact(partition_id) FROM system.parts WHERE active AND database = '"+cfg.Database+"' AND table = 'facts'"))
}

// Under an insert transform the loaded rows go where every write goes: to
// the deployment's table, in its shape, and read back through its view
// they are the rows that were loaded (ADR-0402).
func TestServer_Load_ThroughATransform(t *testing.T) {
	cfg := serverScratch(t, "loadtr")
	s := serverStore(t, cfg)
	ctx := context.Background()
	require.NoError(t, s.SetupTable(ctx, ""))
	rows := writeMixedRows(t, s)
	before := cfg.Database + ".facts_before"
	facts := cfg.Database + ".facts"
	require.NoError(t, s.exec.Exec(ctx, "RENAME TABLE "+facts+" TO "+before))

	contract, err := SchemaContract(cfg)
	require.NoError(t, err)
	carrier := chstoretest.NewCarrier(contract.Columns, cfg.Database+".carrier", facts, "one")
	for _, stmt := range append(carrier.SetupStatements(), carrier.ViewStatement(nil, nil)) {
		require.NoError(t, s.exec.Exec(ctx, stmt))
	}
	// Another tenant's rows in the carrier are not facts and do not count.
	require.NoError(t, s.exec.Exec(ctx, "INSERT INTO "+carrier.Table+" (tenant) VALUES ('another')"))
	unregister := recordstore.RegisterInsertTransform(facts, carrier.Transform())
	defer unregister()

	report, err := LoadFacts(ctx, s.exec, cfg, LoadOptions{FromTable: before})
	require.NoError(t, err)
	assert.EqualValues(t, rows, report.Rows)
	assert.Equal(t, facts, report.WrittenTo)
	assert.EqualValues(t, rows, report.After)
	assert.Equal(t, "View", scalar(t, s, "SELECT engine FROM system.tables WHERE database = '"+cfg.Database+"' AND name = 'facts'"))
	assert.Equal(t, "13", scalar(t, s, "SELECT count() FROM "+carrier.Table))
	sameRows(t, s, before, facts)

	// A transform that drops rows is found by the count afterwards.
	unregister()
	dropping := recordstore.InsertTransformFunc(func(table string, records []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
		target, out, terr := carrier.Transform().TransformInsert(table, records)
		for i, r := range out {
			out[i] = r.NewSlice(0, r.NumRows()-1)
			r.Release()
		}
		return target, out, terr
	})
	unregister2 := recordstore.RegisterInsertTransform(facts, dropping)
	defer unregister2()
	_, err = LoadFacts(ctx, s.exec, cfg, LoadOptions{FromTable: before, Append: true})
	require.ErrorContains(t, err, "does not show the rows that were inserted")
}

// A capture written without a server — the stores' batches in an Arrow IPC
// stream — is loaded into the facts table and holds what the stores wrote.
func TestServer_Load_FromAStream(t *testing.T) {
	cfg := serverScratch(t, "loadipc")
	s := serverStore(t, cfg)
	ctx := context.Background()
	require.NoError(t, s.SetupTable(ctx, ""))
	records := logBatch(t, s, 5, "captured-")

	var capture bytes.Buffer
	w := ipcexec.NewStreamExecutor(&capture, records[0].Schema(), nil)
	require.NoError(t, w.InsertArrow(ctx, cfg.Database+".facts", records))
	require.NoError(t, w.Close())

	report, err := LoadFacts(ctx, s.exec, cfg, LoadOptions{FromStream: &capture})
	require.NoError(t, err)
	assert.EqualValues(t, 5, report.Rows)
	assert.EqualValues(t, 5, report.After)
	got, err := s.LogRows(ctx, factsstore.LogRowsFilter{MessageContains: "captured-"})
	require.NoError(t, err)
	assert.Len(t, got, 5)
}
