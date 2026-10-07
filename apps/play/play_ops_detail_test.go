package play

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

func TestDetailAndGlossCatalogEntries(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	spec, ok := m.Operations.Lookup(opGetDetail)
	require.True(t, ok)
	assert.Equal(t, app.OperationClassQuery, spec.Class)
	assert.Equal(t, app.OperationEffectNone, spec.Effect)
	assert.True(t, spec.Untrusted, "every value is the data's")
	assert.True(t, spec.Agents)
	assert.Equal(t, []string{opsResResult, opsResSignals, opsResPanes}, spec.Reads)
	assert.Empty(t, spec.Writes)

	spec, ok = m.Operations.Lookup(opListGlosses)
	require.True(t, ok)
	assert.Equal(t, app.OperationClassQuery, spec.Class)
	assert.Equal(t, app.OperationEffectNone, spec.Effect)
	assert.False(t, spec.Untrusted, "the build's own text")
	assert.True(t, spec.Agents)
	assert.Empty(t, spec.Reads)
	assert.Empty(t, spec.Writes)
}

func landMain(l *PlayLauncher, rec arrow.RecordBatch) {
	l.inner.graph.mainLane.finish("SELECT …", nil, time.Now(), rec, rec.Schema(), rec.NumRows(), Summary{}, nil, runstream.Terminal{})
}

// get_detail reads a leeway row as the card does: its sections with values
// and memberships, its canonical identity and its temporal attributes; the
// selection picks the row unless one is named.
func TestGetDetailReadsALeewayRow(t *testing.T) {
	l, h := opsLauncher(t)
	rec := factsRecord(t)
	rec.Retain()
	landMain(l, rec)

	_, err := h.Snapshot().Query(opGetDetail, nil)
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal, "no selection yet")

	l.inner.graph.setSignalRawFrom(signalSelection, "1", signalWriterApp)
	out := queryOp[DetailReading](t, h, opGetDetail, nil)
	assert.Equal(t, int64(1), out.Row, "the selection's row")
	assert.Equal(t, int64(2), out.Rows)
	assert.True(t, out.Leeway)
	assert.Equal(t, "646f632d62", out.NaturalKey, "doc-b, as the pane shows bytes")
	require.NotEmpty(t, out.Attributes)
	tagged := false
	for _, a := range out.Attributes {
		tagged = tagged || !a.Plain
		assert.NotContains(t, a.Name, "_unidentified", "an attribute is named by what it is")
	}
	assert.True(t, tagged)
	assert.Equal(t, 3, out.Hidden, "the doc's machine-readable-only columns are counted, as the card hides them")
	for _, a := range out.Attributes {
		if a.Section == "u64-array" {
			assert.Equal(t, []string{"1"}, a.Values[0].Items, "a list-valued attribute reads as a list")
			assert.Contains(t, a.Handle, "LW_GET_LIST('u64-array', ")
		}
	}
	require.NotNil(t, out.Identity)
	assert.Empty(t, out.Identity.Error)
	assert.Len(t, out.Identity.Canonform, 64)
	assert.True(t, out.Identity.Canonical)
	require.NotEmpty(t, out.Temporal)
	assert.Contains(t, out.Temporal[0].Summary, "2023-11-14")

	row0 := int64(0)
	first := queryOp[DetailReading](t, h, opGetDetail, DetailArgs{Row: &row0})
	host := map[string]DetailAttribute{}
	for _, a := range first.Attributes {
		host[a.Name] = a
	}
	require.Contains(t, host, "sysm-mem-host", "a ref is named through the session's registries")
	assert.Equal(t, "host-a", host["sysm-mem-host"].Values[0].Value)
	assert.Equal(t, []string{"17179869184"}, host["sysm-mem-total-bytes"].Values[0].Items)
	assert.Equal(t, "host-a", host["natural-key"].Values[0].Value, "a bytes natural key reads as text")
	assert.Equal(t, "686f73742d61", first.NaturalKey)
	assert.NotEqual(t, out.Identity.Canonform, first.Identity.Canonform)

	far := int64(9)
	_, err = h.Snapshot().Query(opGetDetail, mustEncode(t, DetailArgs{Row: &far}))
	require.ErrorAs(t, err, &refusal)
	assert.Contains(t, refusal.Reason, "2 rows")
}

// A result that is not leeway-shaped reads as the pane's ad-hoc groups,
// non-empty columns only; an embedder's body is named.
func TestGetDetailReadsAnAdHocRow(t *testing.T) {
	l, h := opsLauncher(t)
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "city", Type: arrow.BinaryTypes.String},
		{Name: "empty", Type: arrow.BinaryTypes.String},
	}, nil)
	cb, eb := array.NewStringBuilder(mem), array.NewStringBuilder(mem)
	cb.Append("Basel")
	eb.Append("")
	ca, ea := cb.NewArray(), eb.NewArray()
	rec := array.NewRecordBatch(schema, []arrow.Array{ca, ea}, 1)
	cb.Release()
	eb.Release()
	ca.Release()
	ea.Release()
	landMain(l, rec)
	l.inner.SetDetailContent(func(arrow.RecordBatch, *arrow.Schema, int64) {})

	row := int64(0)
	out := queryOp[DetailReading](t, h, opGetDetail, DetailArgs{Row: &row})
	assert.False(t, out.Leeway)
	assert.True(t, out.Embedded)
	assert.Nil(t, out.Identity)
	require.Len(t, out.Attributes, 1, "the empty column is left out")
	assert.Equal(t, "data", out.Attributes[0].Section)
	assert.Equal(t, DetailValue{Column: "city", Value: "Basel"}, out.Attributes[0].Values[0])
}

// The byte bound keeps attributes in order and counts what it left out.
func TestBoundDetailAttributesCountsWhatItLeavesOut(t *testing.T) {
	big := DetailAttribute{Name: "a", Values: []DetailValue{{Column: "v", Value: string(make([]byte, 200))}}}
	out, more := boundDetailAttributes([]DetailAttribute{big, big, big}, 300)
	require.Len(t, out, 1)
	assert.Equal(t, 2, more)
}

// list_glosses lists the catalog with the spellings that declare each gloss
// and its closed parameter values; search narrows it.
func TestListGlossesListsTheCatalog(t *testing.T) {
	_, h := opsLauncher(t)
	all := queryOp[GlossCatalog](t, h, opListGlosses, nil)
	require.NotEmpty(t, all.Glosses)
	assert.NotEmpty(t, all.Precedence)
	var temp *GlossInfo
	for i := range all.Glosses {
		if all.Glosses[i].MediaType == "gloss/temperature" {
			temp = &all.Glosses[i]
		}
	}
	require.NotNil(t, temp)
	require.NotEmpty(t, temp.Params)
	assert.Equal(t, "unit", temp.Params[0].Name)
	assert.NotEmpty(t, temp.Params[0].Values, "a closed set")
	assert.Contains(t, temp.Alias, "AS label@gloss/temperature;unit=")
	assert.Contains(t, temp.Directive, "-- play: gloss gloss/temperature;unit=")
	assert.Contains(t, temp.Call, "gloss(expr")
	assert.NotEmpty(t, temp.Accepts)
	assert.NotEmpty(t, temp.Sample)

	some := queryOp[GlossCatalog](t, h, opListGlosses, GlossArgs{Search: "temperature"})
	require.NotEmpty(t, some.Glosses)
	assert.Less(t, len(some.Glosses), len(all.Glosses))
}

// describe_result reports each column's gloss as the window resolved it:
// applied, refused with why, or not applied to the column's values.
func TestDescribeResultReportsEachColumnsGloss(t *testing.T) {
	l, h := opsLauncher(t)
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "t@gloss/temperature;unit=C", Type: arrow.PrimitiveTypes.Float64},
		{Name: "x@gloss/temperatur;unit=C", Type: arrow.PrimitiveTypes.Float64},
		{Name: "w@gloss/temperature;unit=C", Type: arrow.BinaryTypes.String},
		{Name: "plain", Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	cols := make([]arrow.Array, 0, 4)
	for _, f := range schema.Fields() {
		b := array.NewBuilder(mem, f.Type)
		b.AppendNull()
		cols = append(cols, b.NewArray())
		b.Release()
	}
	rec := array.NewRecordBatch(schema, cols, 1)
	for _, c := range cols {
		c.Release()
	}
	landMain(l, rec)

	before := queryOp[ResultDescription](t, h, opDescribeResult, nil)
	assert.NotEmpty(t, before.GlossNote, "no pane has resolved this result's glosses")
	assert.Nil(t, before.Columns[0].Gloss)

	l.inner.glossColumns(schema) // as the Table pane's draw does
	out := queryOp[ResultDescription](t, h, opDescribeResult, nil)
	assert.Empty(t, out.GlossNote)
	require.NotNil(t, out.Columns[0].Gloss)
	assert.Equal(t, "applied", out.Columns[0].Gloss.Status)
	assert.Equal(t, "gloss/temperature;unit=C", out.Columns[0].Gloss.Token)
	assert.Equal(t, glossSourceAlias, out.Columns[0].Gloss.Source)
	require.NotNil(t, out.Columns[1].Gloss)
	assert.Equal(t, "refused", out.Columns[1].Gloss.Status)
	assert.NotEmpty(t, out.Columns[1].Gloss.Reason)
	require.NotNil(t, out.Columns[2].Gloss)
	assert.Equal(t, "not applied", out.Columns[2].Gloss.Status)
	assert.Nil(t, out.Columns[3].Gloss, "a plain column")
}
