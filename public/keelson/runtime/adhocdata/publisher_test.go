package adhocdata

import (
	"bytes"
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// TestPublisherReusesItsHandle pins ADR-0240 §SD6's publisher: one alias,
// one handle across republishes, the generation up once per success, the
// keep-after-close flag carried, and Retract forgetting the handle.
func TestPublisherReusesItsHandle(t *testing.T) {
	logger := testLogger(t)
	bus := inprocbus.NewInst(logger)
	svc, err := NewService(Config{Bus: bus, Registry: introspect.NewRegistry(), Dir: t.TempDir(), Log: logger})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	client := bus.NewClient("test.app", []app.SubjectFilter{{Pattern: "adhoc.>", Direction: app.CapDirectionBoth}})
	client.SetInstanceKey(1)

	pub := NewPublisher("series", true)
	assert.Equal(t, "series", pub.Alias())
	assert.Empty(t, pub.Handle())
	_, gen, perr := pub.Last()
	assert.Zero(t, gen)
	assert.NoError(t, perr)

	first, err := pub.Publish(client, int64Stream(t, false, 1))
	require.NoError(t, err)
	second, err := pub.Publish(client, int64Stream(t, false, 2, 3))
	require.NoError(t, err)
	assert.Equal(t, first.Handle, second.Handle, "a republish rides the held handle")
	assert.Equal(t, uint64(2), second.Revision)
	assert.Equal(t, 1, svc.LiveCount(), "no second dataset was minted")
	last, gen, perr := pub.Last()
	assert.Equal(t, second, last)
	assert.Equal(t, uint64(2), gen)
	assert.NoError(t, perr)

	svc.mu.RLock()
	kept := svc.live[first.Handle].keepAfterClose
	svc.mu.RUnlock()
	assert.True(t, kept, "keep-after-close reached the service")

	_, err = pub.Publish(client, []byte("not arrow"))
	require.Error(t, err)
	_, gen, perr = pub.Last()
	assert.Equal(t, uint64(2), gen, "a failed publish does not advance the generation")
	assert.Error(t, perr)
	assert.Equal(t, first.Handle, pub.Handle(), "and keeps the handle")

	require.NoError(t, pub.Retract(client))
	assert.Empty(t, pub.Handle())
	assert.Equal(t, 0, svc.LiveCount())
	require.NoError(t, pub.Retract(client), "retracting nothing is fine")

	_, err = pub.Publish(nil, int64Stream(t, false, 1))
	require.Error(t, err, "no bus, no publish")
}

func TestEncodeRecordRoundTrips(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	rb.Field(0).(*array.Int64Builder).AppendValues([]int64{7, 8}, nil)
	rec := rb.NewRecordBatch()
	defer rec.Release()

	stream, err := EncodeRecord(rec)
	require.NoError(t, err)
	rdr, err := ipc.NewReader(bytes.NewReader(stream))
	require.NoError(t, err)
	defer rdr.Release()
	require.True(t, rdr.Next())
	assert.Equal(t, int64(2), rdr.RecordBatch().NumRows())

	// An empty batch still carries its schema: zero rows is an answer.
	empty := rb.NewRecordBatch()
	defer empty.Release()
	stream, err = EncodeRecord(empty)
	require.NoError(t, err)
	rdr2, err := ipc.NewReader(bytes.NewReader(stream))
	require.NoError(t, err)
	defer rdr2.Release()
	assert.True(t, rdr2.Schema().Equal(schema))
}
