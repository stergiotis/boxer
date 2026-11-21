//go:build integration

package chstore

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/jackstay"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// carrierOf is a deployment's storage in the shape these tests need: a
// table with another column set — every facts column renamed c<i>, and a
// tenant column the facts do not have — a transform into it, and the
// statements that create it and a view back in the facts columns. The view
// leaves out the facts column skip names, when skip is not empty.
func carrierOf(t *testing.T, skip string) (setup []string, tr recordstore.InsertTransformI) {
	t.Helper()
	c, err := SchemaContract(Config{Database: "boxer", Table: "facts"})
	require.NoError(t, err)
	carrierName := make(map[string]string, len(c.Columns))
	cols := make([]string, 0, len(c.Columns)+1)
	sel := make([]string, 0, len(c.Columns))
	cols = append(cols, "tenant String")
	for i, col := range c.Columns {
		n := "c" + strconv.Itoa(i)
		carrierName[col.Name] = n
		cols = append(cols, n+" "+col.Type)
		if col.Name != skip {
			sel = append(sel, n+" AS "+jackstay.QuoteIdent(col.Name))
		}
	}
	setup = []string{
		"CREATE DATABASE IF NOT EXISTS carrier",
		"SET allow_suspicious_low_cardinality_types = 1; CREATE TABLE carrier.rows (" + strings.Join(cols, ", ") + ") ENGINE = MergeTree ORDER BY tenant SETTINGS " + recordstore.DedupWindowSetting + " = 100",
		"CREATE VIEW carrier.facts_view AS SELECT " + strings.Join(sel, ", ") + " FROM carrier.rows WHERE tenant = 'one'",
	}
	alloc := memory.NewGoAllocator()
	tr = recordstore.InsertTransformFunc(func(table string, records []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
		out := make([]arrow.RecordBatch, 0, len(records))
		for _, r := range records {
			fields := []arrow.Field{{Name: "tenant", Type: arrow.BinaryTypes.String}}
			b := array.NewStringBuilder(alloc)
			for range int(r.NumRows()) {
				b.Append("one")
			}
			tenant := b.NewArray()
			b.Release()
			arrs := []arrow.Array{tenant}
			for i, f := range r.Schema().Fields() {
				f.Name = carrierName[f.Name]
				fields = append(fields, f)
				arrs = append(arrs, r.Column(i))
			}
			out = append(out, array.NewRecordBatch(arrow.NewSchema(fields, nil), arrs, r.NumRows()))
			tenant.Release()
		}
		return "carrier.rows", out, nil
	})
	return
}

// A transform into a table with another column set and a view back named
// as the read table pass the round trip over every registered kind
// (ADR-0402 §SD6).
func TestRoundTrip_TransformAndViewAgree_Local(t *testing.T) {
	exec := localExec(t)
	ctx := context.Background()
	setup, tr := carrierOf(t, "")
	for _, s := range setup {
		require.NoError(t, exec.Exec(ctx, s), s[:min(len(s), 60)])
	}
	report, err := RoundTrip(ctx, exec, RoundTripOptions{Transform: tr, ReadTable: "carrier.facts_view"})
	if err != nil {
		t.Log(eh.FormatErrorPlainS(err))
	}
	require.NoError(t, err)
	assert.True(t, report.Ok(), "%+v", report)
	assert.Equal(t, "carrier.rows", report.Target)
	assert.Equal(t, uint64(len(report.Kinds)), report.Written)
	assert.Equal(t, report.Written, report.Read)
	assert.Empty(t, report.Mismatches)
	assert.Equal(t, recordstore.SchemaVerdictOk, report.Columns.Verdict, report.Columns.Summary())
}

// A view that leaves a facts column out fails the round trip on its
// columns.
func TestRoundTrip_AViewMissingAColumnFails_Local(t *testing.T) {
	exec := localExec(t)
	ctx := context.Background()
	setup, tr := carrierOf(t, factsschema.TsColumn)
	for _, s := range setup {
		require.NoError(t, exec.Exec(ctx, s), s[:min(len(s), 60)])
	}
	report, err := RoundTrip(ctx, exec, RoundTripOptions{Transform: tr, ReadTable: "carrier.facts_view"})
	require.NoError(t, err)
	assert.False(t, report.Ok())
	assert.Equal(t, []string{factsschema.TsColumn}, report.Columns.Missing)
}

// A transform that loses a value fails the round trip on that column.
func TestRoundTrip_ALostValueIsAMismatch_Local(t *testing.T) {
	exec := localExec(t)
	ctx := context.Background()
	setup, tr := carrierOf(t, "")
	for _, s := range setup {
		require.NoError(t, exec.Exec(ctx, s), s[:min(len(s), 60)])
	}
	alloc := memory.NewGoAllocator()
	lossy := recordstore.InsertTransformFunc(func(table string, records []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
		target, out, err := tr.TransformInsert(table, records)
		if err != nil {
			return "", nil, err
		}
		// Zero the timestamp: the row survives, its time does not.
		for i, r := range out {
			idx := r.Schema().FieldIndices(carrierTsName(t, r))[0]
			cols := append([]arrow.Array(nil), r.Columns()...)
			zero := array.MakeArrayOfNull(alloc, r.Column(idx).DataType(), int(r.NumRows()))
			cols[idx] = zero
			fields := append([]arrow.Field(nil), r.Schema().Fields()...)
			fields[idx].Nullable = true
			out[i] = array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, r.NumRows())
			zero.Release()
			r.Release()
		}
		return target, out, nil
	})
	report, err := RoundTrip(ctx, exec, RoundTripOptions{Transform: lossy, ReadTable: "carrier.facts_view"})
	require.NoError(t, err)
	assert.False(t, report.Ok())
	require.NotEmpty(t, report.Mismatches)
	assert.Equal(t, factsschema.TsColumn, report.Mismatches[0].Column)
	assert.Equal(t, 2*report.Written, report.Mismatches[0].Rows, "every row differs, counted from both sides")
}

// carrierTsName is the carrier's name for the facts timestamp column in r.
func carrierTsName(t *testing.T, r arrow.RecordBatch) string {
	t.Helper()
	c, err := SchemaContract(Config{Database: "boxer", Table: "facts"})
	require.NoError(t, err)
	for i, col := range c.Columns {
		if col.Name == factsschema.TsColumn {
			return "c" + strconv.Itoa(i)
		}
	}
	t.Fatal("no timestamp column")
	return ""
}

// A re-sent batch lands once: the transform is pure, so the re-sent insert
// is the same block under the same token, and the carrier's window folds it
// (ADR-0402 §SD1).
func TestTransformedInsert_AResentBatchLandsOnce_Local(t *testing.T) {
	exec := localExec(t)
	ctx := context.Background()
	setup, tr := carrierOf(t, "")
	for _, s := range setup {
		require.NoError(t, exec.Exec(ctx, s), s[:min(len(s), 60)])
	}
	records, _, err := roundTripRows(ctx, memory.NewGoAllocator())
	require.NoError(t, err)
	defer func() {
		for _, r := range records {
			r.Release()
		}
	}()
	wrapped := recordstore.TransformExecutor(exec, factsschema.QualifiedTableName, tr)
	settings := recordstore.InsertSettings{}.WithDeduplicationToken("roundtrip-batch-1")
	for range 2 {
		_, err = wrapped.InsertArrowWith(ctx, factsschema.QualifiedTableName, records[:1], settings)
		require.NoError(t, err)
	}
	assert.Equal(t, strconv.FormatInt(records[0].NumRows(), 10), count(t, exec, "carrier.rows"))
}
