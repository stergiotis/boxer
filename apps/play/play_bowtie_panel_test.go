package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/bowtie"
)

// bowtieRow is one row of a test result; step < 0 is NULL.
type bowtieRow struct {
	side, path string
	step       int64
	barrier    string
	status     string
	evidence   string
}

// bowtieRec builds a result carrying the full column contract.
func bowtieRec(t *testing.T, top string, rows []bowtieRow) arrow.RecordBatch {
	t.Helper()
	alloc := memory.NewGoAllocator()
	str := func(get func(r bowtieRow) string) arrow.Array {
		b := array.NewStringBuilder(alloc)
		defer b.Release()
		for _, r := range rows {
			b.Append(get(r))
		}
		return b.NewStringArray()
	}
	sb := array.NewUint8Builder(alloc)
	defer sb.Release()
	for _, r := range rows {
		if r.step < 0 {
			sb.AppendNull()
		} else {
			sb.Append(uint8(r.step))
		}
	}
	fields := []arrow.Field{strField("top_event"), strField("side"), strField("path"),
		{Name: "step", Type: arrow.PrimitiveTypes.Uint8, Nullable: true},
		strField("barrier"), strField("status"), strField("evidence")}
	cols := []arrow.Array{
		str(func(bowtieRow) string { return top }),
		str(func(r bowtieRow) string { return r.side }),
		str(func(r bowtieRow) string { return r.path }),
		sb.NewUint8Array(),
		str(func(r bowtieRow) string { return r.barrier }),
		str(func(r bowtieRow) string { return r.status }),
		str(func(r bowtieRow) string { return r.evidence }),
	}
	return array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, int64(len(rows)))
}

func TestBowtieAcceptContract(t *testing.T) {
	p := bowtiePanel{driver: NewBowtieDriver(nil)}
	full := bowtieRec(t, "loss", nil).Schema()
	claim, reason := p.AcceptForChannel(chMain, full, nil)
	require.Empty(t, reason)
	k := claim.(bowtieClaim)
	assert.Equal(t, 3, k.stepCol)
	assert.Equal(t, 6, k.evidenceCol)

	// The three naming columns are required, and the reject says which.
	_, reason = p.AcceptForChannel(chMain, arrow.NewSchema([]arrow.Field{strField("path")}, nil), nil)
	assert.Contains(t, reason, "`top_event`")
	assert.Contains(t, reason, "`side`")
	assert.NotContains(t, reason, "`path`")

	// Paths alone are a bow-tie with no barriers yet.
	_, reason = p.AcceptForChannel(chMain, arrow.NewSchema([]arrow.Field{strField("top_event"), strField("side"), strField("path")}, nil), nil)
	assert.Empty(t, reason)

	// A barrier needs its order and its status beside it.
	_, reason = p.AcceptForChannel(chMain, arrow.NewSchema([]arrow.Field{strField("top_event"), strField("side"), strField("path"),
		strField("barrier"), strField("status")}, nil), nil)
	assert.Contains(t, reason, "`step`")
	_, reason = p.AcceptForChannel(chMain, arrow.NewSchema([]arrow.Field{strField("top_event"), strField("side"), strField("path"),
		strField("barrier"), u64Field("step")}, nil), nil)
	assert.Contains(t, reason, "`status`")

	// A step is an order, so it is an integer.
	_, reason = p.AcceptForChannel(chMain, arrow.NewSchema([]arrow.Field{strField("top_event"), strField("side"), strField("path"),
		strField("barrier"), f64Field("step"), strField("status")}, nil), nil)
	assert.Contains(t, reason, "integer")
}

func TestBowtieFoldAndRowIdentity(t *testing.T) {
	rec := bowtieRec(t, "loss", []bowtieRow{
		{side: "prevention", path: "cause", step: 2, barrier: "second", status: "failed", evidence: "red team"},
		{side: "prevention", path: "cause", step: 1, barrier: "first", status: "effective"},
		{side: "Mitigation", path: "effect", step: -1},
	})
	k, reason := resolveBowtieColumns(rec.Schema())
	require.Empty(t, reason)
	m, reject, truncated := foldBowtie(rec, k)
	require.Empty(t, reject)
	assert.Zero(t, truncated)
	require.Len(t, m.Threats, 1)
	require.Len(t, m.Threats[0].Barriers, 2)
	assert.Equal(t, "first", m.Threats[0].Barriers[0].Name, "barriers by step, not by row")
	assert.Equal(t, bowtie.StatusFailed, m.Threats[0].Barriers[1].Status)
	require.Len(t, m.Consequences, 1)
	assert.Empty(t, m.Consequences[0].Barriers, "a NULL step names a path with no barrier")

	// A click maps back to the row the element came from.
	assert.Equal(t, int64(0), bowtieRowOf(&m, bowtie.Element{Kind: bowtie.ElementBarrier, Side: bowtie.SidePrevention, Path: 0, Barrier: 1}))
	assert.Equal(t, int64(1), bowtieRowOf(&m, bowtie.Element{Kind: bowtie.ElementBarrier, Side: bowtie.SidePrevention, Path: 0, Barrier: 0}))
	assert.Equal(t, int64(2), bowtieRowOf(&m, bowtie.Element{Kind: bowtie.ElementPath, Side: bowtie.SideMitigation, Path: 0, Barrier: -1}))
	assert.Equal(t, int64(-1), bowtieRowOf(&m, bowtie.Element{Kind: bowtie.ElementTopEvent, Path: -1, Barrier: -1}))
}

func TestBowtieFoldRejectsByRow(t *testing.T) {
	cases := map[string]struct {
		rows []bowtieRow
		want string
	}{
		"side":     {[]bowtieRow{{side: "left", path: "p", step: -1}}, "Row 1: `side`"},
		"status":   {[]bowtieRow{{side: "prevention", path: "p", step: 1, barrier: "b", status: "green"}}, "Row 1: `status`"},
		"parallel": {[]bowtieRow{{side: "prevention", path: "p", step: 1, barrier: "a"}, {side: "prevention", path: "p", step: 1, barrier: "b"}}, "not one bow-tie"},
	}
	for name, tc := range cases {
		rec := bowtieRec(t, "loss", tc.rows)
		k, reason := resolveBowtieColumns(rec.Schema())
		require.Empty(t, reason, name)
		_, reject, _ := foldBowtie(rec, k)
		assert.Contains(t, reject, tc.want, name)
	}
}

func TestBowtieStatusLine(t *testing.T) {
	d := NewBowtieDriver(nil)
	rec := bowtieRec(t, "loss", []bowtieRow{
		{side: "prevention", path: "a", step: 1, barrier: "x", status: "failed"},
		{side: "prevention", path: "a", step: 2, barrier: "y", status: ""},
		{side: "mitigation", path: "b", step: 1, barrier: "z", status: "effective"},
	})
	k, _ := resolveBowtieColumns(rec.Schema())
	d.rebuild(rec, rec.Schema(), k)
	line, reject := d.paneStatus()
	assert.Empty(t, reject)
	assert.Equal(t, "1 threat · 1 consequence · 3 barriers · 1 failed · 1 untested", line)
}
