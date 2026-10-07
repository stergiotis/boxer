package sqlapplet

import (
	"reflect"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

// opsReceiver is a receiver whose catalog offers its bundle views.
type opsReceiver struct {
	views map[string]*BundleView
}

type opsReceiverSnap struct{}

var receiverOps = func() (s *appops.Set[*opsReceiver, opsReceiverSnap]) {
	s = appops.NewSet(func(*opsReceiver) opsReceiverSnap { return opsReceiverSnap{} })
	BundleViewOps(s, func(r *opsReceiver) map[string]*BundleView { return r.views })
	return
}()

// Every bundle_ operation is play's of the same name: same version, class,
// effect, result and prose, its resources prefixed, its arguments play's
// with the view in front (ADR-0288 (proposed) §SD8).
func TestBundleViewOpsArePlaysOperations(t *testing.T) {
	catalog := receiverOps.Catalog()
	require.NoError(t, catalog.Validate())
	ops := play.OperableOperations()
	require.NotEmpty(t, ops)
	for _, o := range ops {
		got, ok := catalog.Lookup(BundleOpPrefix + o.Spec.Name)
		require.True(t, ok, o.Spec.Name)
		assert.Equal(t, o.Spec.Version, got.Version, o.Spec.Name)
		assert.Equal(t, o.Spec.Class, got.Class, o.Spec.Name)
		assert.Equal(t, o.Spec.Effect, got.Effect, o.Spec.Name)
		assert.Equal(t, o.Spec.Result, got.Result, o.Spec.Name)
		assert.Equal(t, o.Spec.Follows, got.Follows, o.Spec.Name)
		assert.Equal(t, o.Spec.Untrusted, got.Untrusted, o.Spec.Name)
		assert.Equal(t, prefixed(o.Spec.Reads), got.Reads, o.Spec.Name)
		assert.Equal(t, prefixed(o.Spec.Writes), got.Writes, o.Spec.Name)
		require.Equal(t, "BundleView", got.Args.Field(0).Name, o.Spec.Name)
		if o.Spec.Args != nil {
			require.Equal(t, o.Spec.Args.NumField()+1, got.Args.NumField(), o.Spec.Name)
			for i := range o.Spec.Args.NumField() {
				assert.Equal(t, o.Spec.Args.Field(i).Name, got.Args.Field(i+1).Name, o.Spec.Name)
				assert.Equal(t, o.Spec.Args.Field(i).Type, got.Args.Field(i+1).Type, o.Spec.Name)
			}
		}
	}
	_, ok := catalog.Lookup(BundleOpPrefix + "set_sql")
	assert.False(t, ok, "the document is the publisher's")
	_, ok = catalog.Lookup(BundleOpPrefix + "publish_result")
	assert.False(t, ok)
}

func encodeView(t *testing.T, view string) []byte {
	t.Helper()
	b, err := buscodec.Encode(struct{ BundleView string }{BundleView: view})
	require.NoError(t, err)
	return b
}

// An operable view serves play's operations through the receiver: a read of
// its state, a pane it does not show reported unavailable, and a run held to
// play's agent limits under the bundle's name.
func TestAnOperableViewServesPlaysOperations(t *testing.T) {
	_, publisher, viewBus := viewRig(t)
	publishView(t, publisher, "SELECT * FROM keelson('result')", "result")
	v := NewBundleView("counts", BundleViewConfig{Bus: viewBus, Log: zerolog.Nop(), StampAppId: "test.receiver#counts", Operable: true})
	t.Cleanup(v.Close)
	plain := NewBundleView("counts", BundleViewConfig{Bus: viewBus, Log: zerolog.Nop(), StampAppId: "test.receiver#plain"})
	t.Cleanup(plain.Close)
	syncUntil(t, v, func() bool { return v.Inner() != nil && len(v.Inner().DatasetBindingsForTest()) == 1 })

	r := &opsReceiver{views: map[string]*BundleView{"main": v, "plain": plain}}
	h := receiverOps.Bind(r)
	snap := h.Snapshot()
	raw, err := snap.Query(opListBundleViews, nil)
	require.NoError(t, err)
	list, err := buscodec.Decode[BundleViewList](raw)
	require.NoError(t, err)
	require.Len(t, list.Views, 1, "a plain view is not offered")
	assert.Equal(t, "main", list.Views[0].View)
	assert.Equal(t, "counts", list.Views[0].Bundle)
	assert.Contains(t, list.Views[0].Panes, "table")
	assert.NotContains(t, list.Views[0].Panes, "chart")

	_, err = snap.Query(BundleOpPrefix+"get_state", nil)
	require.NoError(t, err, "with one view, the view may be left out")
	ok, reason := snap.Available(BundleOpPrefix + "get_chart")
	assert.False(t, ok)
	assert.Contains(t, reason, "chart pane")
	_, err = snap.Query(BundleOpPrefix+"get_chart", encodeView(t, "main"))
	assert.ErrorContains(t, err, "shows the chart pane", "the refusal says which pane is missing")
	_, err = snap.Query(BundleOpPrefix+"get_state", encodeView(t, "nope"))
	assert.ErrorContains(t, err, "no operable bundle view named nope")

	obo := app.OnBehalfOf{Task: "t", Epoch: 1, Call: "t-1"}
	_, err = h.ApplyCommand(app.OperationCall{Writer: "task:t", OnBehalfOf: &obo}, BundleOpPrefix+"run", encodeView(t, "main"))
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal, "a run is play's, under play's agent limits")
	assert.Contains(t, refusal.Destinations, play.DestinationKeelsonBundle("counts"), "the grant is asked for the bundle, never the local name")
	obo.Destinations = []string{play.DestinationKeelsonBundle("counts")}
	_, err = h.ApplyCommand(app.OperationCall{Writer: "task:t", OnBehalfOf: &obo}, BundleOpPrefix+"run", encodeView(t, "main"))
	require.NoError(t, err)

	r.views["second"] = NewBundleView("counts", BundleViewConfig{Bus: viewBus, Log: zerolog.Nop(), StampAppId: "test.receiver#second", Operable: true})
	t.Cleanup(r.views["second"].Close)
	_, err = h.ApplyCommand(app.OperationCall{Writer: "task:t", OnBehalfOf: &obo}, BundleOpPrefix+"run", nil)
	assert.ErrorContains(t, err, "several bundle views")
	_, err = h.ApplyCommand(app.OperationCall{Writer: "task:t", OnBehalfOf: &obo}, BundleOpPrefix+"run", encodeView(t, "second"))
	assert.ErrorContains(t, err, "waits", "a view that has not applied its bundle says what it waits for")
}

func TestWithViewArgKeepsPlaysFields(t *testing.T) {
	type args struct {
		Name  string `desc:"x"`
		Value string
	}
	got := withViewArg(reflect.TypeFor[args]())
	require.Equal(t, 3, got.NumField())
	assert.Equal(t, "BundleView", got.Field(0).Name)
	assert.Equal(t, `desc:"x"`, string(got.Field(1).Tag))
	assert.Equal(t, 1, withViewArg(nil).NumField())
}
