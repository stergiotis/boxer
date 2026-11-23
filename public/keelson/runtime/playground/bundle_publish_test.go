package playground_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/playground"
)

// oneCall attests one call of one task, from whichever window sends it.
type oneCall struct{ app.OnBehalfOf }

func (inst oneCall) CallContext(task string, epoch uint64, call string, sender app.AppIdT, senderInstance uint64) (cc app.CallContext, ok bool, reason string) {
	if task != inst.Task || call != inst.Call {
		return cc, false, "the task has no call by that id"
	}
	return app.CallContext{Task: task, Epoch: epoch, Call: call, App: sender, Instance: senderInstance}, true, ""
}

func (inst oneCall) AllowDestination(string, uint64, string) (ok bool, reason string) {
	return false, "not granted"
}

// A producer that publishes rows it computed from sealed data says so, and
// the dataset service then holds the bundle as derived: the task the
// publish was work of does not read it back as its own clear publication.
func TestPublishBundleCarriesWhatTheRowsDeriveFrom(t *testing.T) {
	prev := playground.SetAppletDocParser(func(path string, src []byte) (doc playground.AppletDoc, err error) {
		doc.Datasets = []string{"result"}
		_, rest, _ := strings.Cut(string(src), "```sql\n")
		doc.Sql, _, _ = strings.Cut(rest, "\n```")
		doc.Introspection = true
		return
	})
	t.Cleanup(func() { playground.SetAppletDocParser(prev) })
	bus := inprocbus.NewInst(zerolog.Nop())
	svc, err := adhocdata.NewService(adhocdata.Config{Bus: bus, Registry: introspect.NewRegistry(), Dir: t.TempDir(), Log: zerolog.Nop()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	obo := app.OnBehalfOf{Task: "t", Epoch: 1, Call: "t-1"}
	svc.SetDispatcher(oneCall{obo})
	producer := bus.NewClient("test.producer", []app.SubjectFilter{{Pattern: "runtime.adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"}})

	schema := arrow.NewSchema([]arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64}}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	rb.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2}, nil)
	rec := rb.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())

	spec := playground.BundleSpec{Alias: "computed", OnBehalfOf: &obo,
		Datasets: []adhocdata.BundleDatasetInput{{LocalName: "result", ArrowIPCStream: buf.Bytes()}}}
	_, err = playground.PublishBundle(t.Context(), producer, spec)
	require.NoError(t, err)
	got, err := adhocdata.ReadAll(t.Context(), producer, "computed__result", &obo)
	require.NoError(t, err)
	assert.False(t, got.OwnClear, "a producer that states nothing publishes sealed data, whatever tier its call declared")

	spec.FromNothingSealed = true
	_, err = playground.PublishBundle(t.Context(), producer, spec)
	require.NoError(t, err)
	got, err = adhocdata.ReadAll(t.Context(), producer, "computed__result", &obo)
	require.NoError(t, err)
	assert.True(t, got.OwnClear, "the producer asserted its rows derive from nothing sealed: the task's own clear publication")
}
