package play

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func TestGetCanonicalCatalogEntry(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	spec, ok := m.Operations.Lookup(opGetCanonical)
	require.True(t, ok)
	assert.Equal(t, app.OperationClassQuery, spec.Class)
	assert.Equal(t, app.OperationEffectNone, spec.Effect)
	assert.True(t, spec.Untrusted, "the items are the data's")
	assert.True(t, spec.Agents)
	assert.Equal(t, []string{opsResResult, opsResSignals, opsResPanes}, spec.Reads)
}

// get_canonical reads the selected row's canonical forms: the digests
// get_detail reports, and the items they were taken over in diagnostic
// notation with the pane's position comments.
func TestGetCanonicalReadsALeewayRow(t *testing.T) {
	l, h := opsLauncher(t)
	rec := factsRecord(t)
	rec.Retain()
	landMain(l, rec)

	_, err := h.Snapshot().Query(opGetCanonical, nil)
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal, "no selection yet")

	l.inner.graph.setSignalRawFrom(signalSelection, "1", signalWriterApp)
	out := queryOp[CanonicalReading](t, h, opGetCanonical, nil)
	detail := queryOp[DetailReading](t, h, opGetDetail, nil)
	require.NotNil(t, detail.Identity)
	assert.Equal(t, int64(1), out.Row)
	assert.Equal(t, detail.Identity.Canonform, out.Canonform, "one row, one content digest")
	assert.Equal(t, detail.Identity.Canonwire, out.Canonwire)
	assert.True(t, out.Canonical)
	assert.NotEmpty(t, out.FormPin)
	assert.Contains(t, out.CanonwireItem, "/ version /")
	assert.Contains(t, out.CanonwireItem, "/ tagged /")
	assert.Contains(t, out.CanonwireItem, "notes.md", "the wire item is the row's content")
	assert.Contains(t, out.CanonformItems, "/ memberships /")
	assert.Contains(t, out.CanonformItems, "/ leaf digests /")
	assert.False(t, out.ItemsCut)

	wire := queryOp[CanonicalReading](t, h, opGetCanonical, CanonicalArgs{Items: "canonwire"})
	assert.Equal(t, out.CanonwireItem, wire.CanonwireItem)
	assert.Empty(t, wire.CanonformItems)

	_, err = h.Snapshot().Query(opGetCanonical, mustEncode(t, CanonicalArgs{Items: "cbor"}))
	require.ErrorAs(t, err, &refusal)
}

func TestGetCanonicalRefusesAResultThatIsNotLeewayShaped(t *testing.T) {
	l, h := opsLauncher(t)
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{{Name: "city", Type: arrow.BinaryTypes.String}}, nil)
	b := array.NewStringBuilder(mem)
	b.Append("Basel")
	a := b.NewArray()
	b.Release()
	rec := array.NewRecordBatch(schema, []arrow.Array{a}, 1)
	a.Release()
	landMain(l, rec)

	row := int64(0)
	_, err := h.Snapshot().Query(opGetCanonical, mustEncode(t, CanonicalArgs{Row: &row}))
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
	assert.Contains(t, refusal.Reason, "not leeway-shaped")
}

func TestCutAtLineKeepsWholeLines(t *testing.T) {
	s := "[\n  1,\n  2,\n  3\n]"
	out, cut := cutAtLine(s, 100)
	assert.Equal(t, s, out)
	assert.False(t, cut)
	out, cut = cutAtLine(s, 12)
	assert.True(t, cut)
	assert.True(t, strings.HasSuffix(out, "\n…"))
	assert.Equal(t, "[\n  1,", strings.TrimSuffix(out, "\n…"), "cut back to a line break")
}
