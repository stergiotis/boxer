package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/apps/sqlapplet"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

const viewReceiverId app.AppIdT = "test.receiver"

type viewReceiver struct{ view *sqlapplet.BundleView }

var viewReceiverOps = func() (s *appops.Set[*viewReceiver, struct{}]) {
	s = appops.NewSet(func(*viewReceiver) struct{} { return struct{}{} })
	sqlapplet.BundleViewOps(s, func(r *viewReceiver) map[string]*sqlapplet.BundleView {
		return map[string]*sqlapplet.BundleView{"items": r.view}
	})
	return
}()

// The ADR-0288 §SD8 lane: a receiver window shows a bundle in an
// operable view, and a coordinator drives it through the receiver's catalog
// — the model's JSON, with the bundle_view argument, reaching play's own
// handler in the view, under play's agent limits.
func TestAnAgentDrivesAnOperableBundleView(t *testing.T) {
	ctx := context.Background()
	prevEndpoint := introspect.LocalQueryEndpoint()
	introspect.SetLocalQueryEndpoint(e2eQueryEndpoint)
	t.Cleanup(func() { introspect.SetLocalQueryEndpoint(prevEndpoint) })

	manifest := app.Manifest{Id: viewReceiverId, Display: "Receiver", Summary: "shows a bundle", Surface: app.SurfaceWindowed,
		Topics: []app.TopicT{app.AllTopics[0]}, Operations: viewReceiverOps.Catalog(), Caps: sqlapplet.BundleViewCaps}
	r := newRigWith(t, func(cfg *Config) {
		cfg.Coordinators = []string{"test.coordinator"}
		require.NoError(t, cfg.Registry.RegisterFactory(manifest, func() (app.AppI, error) { return nil, nil }))
	})
	svc, err := adhocdata.NewService(adhocdata.Config{Bus: r.bus, Registry: introspect.NewRegistry(), Dir: t.TempDir(), Log: zerolog.Nop()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(ctx) })
	svc.SetDispatcher(r.svc)
	publisher := r.bus.NewClient("test.producer", []app.SubjectFilter{{Pattern: "adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"}})
	_, err = play.PublishBundle(publisher, play.BundleSpec{Alias: "counts", Sql: "SELECT * FROM keelson('result')", Tabs: []string{"table"},
		Datasets: []adhocdata.BundleDatasetInput{{LocalName: "result", ArrowIPCStream: e2eStream(t, 1, 2, 3)}}})
	require.NoError(t, err)

	const key = 30
	viewBus := r.bus.NewClient(viewReceiverId, sqlapplet.BundleViewCaps)
	viewBus.SetInstanceKey(key)
	rcv := &viewReceiver{view: sqlapplet.NewBundleView("counts", sqlapplet.BundleViewConfig{Bus: viewBus, Log: zerolog.Nop(),
		StampAppId: string(viewReceiverId) + "#items", InstanceKey: key, Operable: true})}
	t.Cleanup(rcv.view.Close)
	e := opengine.New(viewReceiverOps.Catalog(), viewReceiverOps.Bind(rcv))
	r.host.mu.Lock()
	r.host.engines[key] = e
	if r.host.apps == nil {
		r.host.apps = map[uint64]app.AppIdT{}
	}
	r.host.apps[key] = viewReceiverId
	r.host.mu.Unlock()
	e.SetListener(func(entry opengine.LogEntry) { r.svc.Listener()(key, entry) })
	frameUntil := func(cond func() bool) {
		t.Helper()
		require.Eventually(t, func() bool {
			r.host.frameWith(key, rcv.view.Sync)
			return cond()
		}, 5*time.Second, 5*time.Millisecond)
	}
	frameUntil(func() bool { return rcv.view.Inner() != nil && len(rcv.view.Inner().DatasetBindingsForTest()) == 1 })

	got := make(chan Grant, 1)
	go func() {
		g, gErr := r.cli.Request(ctx, GrantRequest{Plan: "read the bundle the receiver shows", Conversation: "conv-op",
			Entries: []GrantEntry{{Instance: key, Mode: ModeAct}}})
		assert.NoError(t, gErr)
		got <- g
	}()
	r.person(true, nil)
	g := <-got
	call := func(k string, op string, args string) Outcome {
		t.Helper()
		out, cErr := r.cli.Call(ctx, CallRequest{Handle: g.Handle, Instance: key, Operation: op, Args: args, Key: k, Turn: "turn-1"})
		require.NoError(t, cErr)
		return out
	}

	out := call("list", "bundle_list_views", `{}`)
	require.Equal(t, "completed", out.Phase, out.Reason)
	out = call("state", "bundle_get_state", `{"bundle_view":"items"}`)
	require.Equal(t, "completed", out.Phase, out.Reason)
	out = call("chart", "bundle_get_chart", `{"bundle_view":"items"}`)
	assert.NotEqual(t, "completed", out.Phase)
	assert.Contains(t, out.Reason, "chart pane", "a pane the bundle does not show is unavailable, with the reason")
	out = call("sql", "bundle_set_sql", `{"bundle_view":"items","sql":"SELECT 1"}`)
	assert.Contains(t, out.Reason, "no such operation", "the document is the publisher's")

	// A run is play's: refused until the grant names the bundle, then
	// queued for the view's next frame.
	out = call("run1", "bundle_run", `{"bundle_view":"items"}`)
	require.Contains(t, []string{"accepted", "applied", "refused"}, out.Phase, out.Reason)
	if out.Phase == "accepted" {
		require.Eventually(t, func() bool {
			r.host.frameWith(key, rcv.view.Sync)
			st, _ := r.cli.Status(ctx, g.Handle, "run1", 0)
			return st.Final()
		}, 5*time.Second, 5*time.Millisecond)
		out, _ = r.cli.Status(ctx, g.Handle, "run1", 0)
	}
	assert.Equal(t, "refused", out.Phase)
	assert.True(t, strings.Contains(out.Reason, "keelson-bundle:counts"), out.Reason)

	widened := make(chan struct{})
	go func() {
		_, wErr := r.cli.Request(ctx, GrantRequest{Handle: g.Handle, Plan: "run it", Destinations: []string{"keelson-bundle:counts"}})
		assert.NoError(t, wErr)
		close(widened)
	}()
	r.person(true, nil)
	<-widened
	out = call("run2", "bundle_run", `{"bundle_view":"items"}`)
	if out.Phase == "accepted" {
		require.Eventually(t, func() bool {
			r.host.frameWith(key, rcv.view.Sync)
			st, _ := r.cli.Status(ctx, g.Handle, "run2", 0)
			return st.Final()
		}, 5*time.Second, 5*time.Millisecond)
		out, _ = r.cli.Status(ctx, g.Handle, "run2", 0)
	}
	assert.Contains(t, []string{"applied", "rendered"}, out.Phase, out.Reason)
}

func e2eStream(t *testing.T, vals ...int64) []byte {
	t.Helper()
	rec := e2eInts(t, vals...)
	defer rec.Release()
	stream, err := adhocdata.EncodeRecord(rec)
	require.NoError(t, err)
	return stream
}
