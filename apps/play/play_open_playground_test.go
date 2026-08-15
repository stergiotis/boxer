package play

import (
	"time"

	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/apps/sqlappletcreator/appletcreatecfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchrequest"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// fakeOpenBus captures the one Request the Open in Playground path makes
// and answers with a canned reply or transport error.
type fakeOpenBus struct {
	gotSubject string
	gotPayload []byte
	reply      launchreply.LaunchReply
	err        error
}

var _ app.BusI = (*fakeOpenBus)(nil)

func (f *fakeOpenBus) Publish(subject string, payload []byte) (err error) { return }
func (f *fakeOpenBus) Subscribe(subject string, handler app.MsgHandlerFunc) (unsubscribe func(), err error) {
	return
}

// RequestWithTimeout delegates: the fake answers instantly, so the wait
// never matters here.
func (f *fakeOpenBus) RequestWithTimeout(subject string, payload []byte, _ time.Duration) ([]byte, error) {
	return f.Request(subject, payload)
}

func (f *fakeOpenBus) Request(subject string, payload []byte) (reply []byte, err error) {
	f.gotSubject = subject
	f.gotPayload = payload
	if f.err != nil {
		err = f.err
		return
	}
	reply, err = buscodec.Encode(f.reply)
	return
}

func newOpenTestApp(t *testing.T, bus *fakeOpenBus) (inst *PlayApp) {
	t.Helper()
	inst = NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 4), "SELECT 1", nil)
	inst.SetCapabilities(bus, nil, zerolog.Nop())
	return
}

func TestRequestOpenPlayground_ComposesRequestAndSucceeds(t *testing.T) {
	bus := &fakeOpenBus{reply: launchreply.LaunchReply{WindowKey: 7}}
	inst := newOpenTestApp(t, bus)

	cfg := launchcfg.PlayLaunch{
		Sql:      "SELECT 41",
		AutoRun:  true,
		Live:     true,
		BandsSql: "SELECT 'b'",
	}
	inst.requestOpenPlayground(cfg)

	inst.openPlayMu.Lock()
	defer inst.openPlayMu.Unlock()
	assert.Empty(t, inst.openPlayErr)
	assert.False(t, inst.openPlayBusy)
	assert.Equal(t, windowhost.OpenSubject, bus.gotSubject)

	req, err := buscodec.Decode[launchrequest.LaunchRequest](bus.gotPayload)
	require.NoError(t, err)
	assert.Equal(t, string(AppId), req.TargetAppId)
	assert.Equal(t, launchcfg.Kind, req.ConfigKind)
	sent, err := buscodec.Decode[launchcfg.PlayLaunch](req.Config)
	require.NoError(t, err)
	assert.Equal(t, cfg.Sql, sent.Sql)
	assert.True(t, sent.AutoRun)
	assert.True(t, sent.Live)
	assert.Equal(t, cfg.BandsSql, sent.BandsSql)
}

// TestRequestOpenPlayground_ResolvesDatasetAliases pins the ad-hoc case:
// an embedder binds keelson('<alias>') to an ephemeral handle on this
// instance's client (ADR-0134 §SD4), and the opened window inherits no
// binding — so the launched buffer must carry the handle form or its
// query resolves nowhere.
func TestRequestOpenPlayground_ResolvesDatasetAliases(t *testing.T) {
	bus := &fakeOpenBus{reply: launchreply.LaunchReply{WindowKey: 3}}
	client := NewClient(ClientConfig{URL: "http://example.invalid"}, nil)
	inst := NewPlayApp(client, newLiveQueryGraph(client, memory.NewGoAllocator(), 4), "", nil)
	inst.SetCapabilities(bus, nil, zerolog.Nop())
	require.NoError(t, inst.BindDataset("items", "adhoc_deadbeef01234567"))

	inst.requestOpenPlayground(launchcfg.PlayLaunch{
		Sql:      "SELECT * FROM keelson('items') ORDER BY x",
		BandsSql: "SELECT * FROM keelson('items')",
	})

	inst.openPlayMu.Lock()
	require.Empty(t, inst.openPlayErr)
	inst.openPlayMu.Unlock()

	req, err := buscodec.Decode[launchrequest.LaunchRequest](bus.gotPayload)
	require.NoError(t, err)
	sent, err := buscodec.Decode[launchcfg.PlayLaunch](req.Config)
	require.NoError(t, err)
	assert.Contains(t, sent.Sql, "keelson('adhoc_deadbeef01234567')")
	assert.NotContains(t, sent.Sql, "'items'")
	assert.Contains(t, sent.BandsSql, "keelson('adhoc_deadbeef01234567')")
}

// An instance with no bindings sends the buffer through untouched — the
// ordinary non-embedded path.
func TestRequestOpenPlayground_UnboundBufferUnchanged(t *testing.T) {
	bus := &fakeOpenBus{reply: launchreply.LaunchReply{WindowKey: 4}}
	inst := newOpenTestApp(t, bus)

	const sql = "SELECT * FROM keelson('items')"
	inst.requestOpenPlayground(launchcfg.PlayLaunch{Sql: sql})

	req, err := buscodec.Decode[launchrequest.LaunchRequest](bus.gotPayload)
	require.NoError(t, err)
	sent, err := buscodec.Decode[launchcfg.PlayLaunch](req.Config)
	require.NoError(t, err)
	assert.Equal(t, sql, sent.Sql)
}

func TestRequestOpenPlayground_RefusalSurfaces(t *testing.T) {
	bus := &fakeOpenBus{reply: launchreply.LaunchReply{Reason: "app accepts no launch config"}}
	inst := newOpenTestApp(t, bus)

	inst.requestOpenPlayground(launchcfg.PlayLaunch{Sql: "SELECT 1"})

	inst.openPlayMu.Lock()
	defer inst.openPlayMu.Unlock()
	assert.Contains(t, inst.openPlayErr, "refused")
	assert.Contains(t, inst.openPlayErr, "accepts no launch config")
}

func TestRequestOpenPlayground_TransportErrorSurfaces(t *testing.T) {
	// The un-wired-handler shape: the request times out or is denied;
	// the button must surface it, not hang or hide (ADR-0135 §SD1).
	bus := &fakeOpenBus{err: eh.Errorf("bus request: timeout")}
	inst := newOpenTestApp(t, bus)

	inst.requestOpenPlayground(launchcfg.PlayLaunch{Sql: "SELECT 1"})

	inst.openPlayMu.Lock()
	defer inst.openPlayMu.Unlock()
	assert.Contains(t, inst.openPlayErr, "timeout")
	assert.False(t, inst.openPlayBusy)
}

func TestRequestSaveApplet_ComposesRequestAndSucceeds(t *testing.T) {
	bus := &fakeOpenBus{reply: launchreply.LaunchReply{WindowKey: 9}}
	inst := newOpenTestApp(t, bus)

	inst.requestSaveApplet(appletcreatecfg.AppletCreate{
		Sql:      "SELECT 7",
		Endpoint: appletcreatecfg.EndpointIntrospection,
	})

	inst.saveAppletMu.Lock()
	defer inst.saveAppletMu.Unlock()
	assert.Empty(t, inst.saveAppletErr)
	assert.False(t, inst.saveAppletBusy)
	assert.Equal(t, windowhost.OpenSubject, bus.gotSubject)

	req, err := buscodec.Decode[launchrequest.LaunchRequest](bus.gotPayload)
	require.NoError(t, err)
	assert.Equal(t, appletcreatecfg.AppId, req.TargetAppId)
	assert.Equal(t, appletcreatecfg.Kind, req.ConfigKind)
	sent, err := buscodec.Decode[appletcreatecfg.AppletCreate](req.Config)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 7", sent.Sql)
	assert.Equal(t, appletcreatecfg.EndpointIntrospection, sent.Endpoint)
}

func TestRequestSaveApplet_RefusalSurfaces(t *testing.T) {
	bus := &fakeOpenBus{reply: launchreply.LaunchReply{Reason: "app accepts no launch config"}}
	inst := newOpenTestApp(t, bus)

	inst.requestSaveApplet(appletcreatecfg.AppletCreate{Sql: "SELECT 1"})

	inst.saveAppletMu.Lock()
	defer inst.saveAppletMu.Unlock()
	assert.Contains(t, inst.saveAppletErr, "refused")
	assert.Contains(t, inst.saveAppletErr, "accepts no launch config")
}
