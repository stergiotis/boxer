package play

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/apps/sqlappletcreator/appletcreatecfg"
	tallylaunch "github.com/stergiotis/boxer/apps/tally/launchcfg"
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
	assert.Contains(t, inst.openPlayErr, "open refused", "the panel shows the message; windowhost's reason is now a field")
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
	assert.Contains(t, inst.saveAppletErr, "open refused", "the panel shows the message; windowhost's reason is now a field")
}

// Open in tally (ADR-0222 §SD6) — tally's own "Open in play" mirrored. The
// offer is gated on the buffer naming a lading macro, because that is what
// makes its rows describe files a browser can show.
func TestOfferOpenInTallyGatesOnALadingBuffer(t *testing.T) {
	inst := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 4), "", nil)
	// No bus: nothing to open a window through, so nothing is offered even
	// for a buffer that would otherwise qualify.
	assert.False(t, inst.offerOpenInTally("SELECT path FROM fs(0xF5F5019800020001, 12345)"))

	inst = newOpenTestApp(t, &fakeOpenBus{})
	assert.True(t, inst.offerOpenInTally("SELECT path FROM fs(0xF5F5019800020001, 12345)"))
	// A mount id that is not a tagged id is not a lading reference — the
	// macro would refuse it at expansion, so there is nothing to browse.
	assert.False(t, inst.offerOpenInTally("SELECT path FROM fs(1, 2)"))
	assert.True(t, inst.offerOpenInTally("SELECT * FROM fssnap('*')"))
	assert.False(t, inst.offerOpenInTally("SELECT 1"))
	assert.False(t, inst.offerOpenInTally("SELECT * FROM boxer.facts"))
	// A dataset lives on this process's introspection plane and tally reads
	// the ClickHouse server, so a buffer naming one has no endpoint that
	// could answer it.
	assert.False(t, inst.offerOpenInTally("SELECT * FROM keelson('h')"))
}

// The answer is memoised on the buffer — the toolbar asks once a frame and
// answering parses — but it still tracks what the buffer became.
func TestOfferOpenInTallyMemoisesOnTheBuffer(t *testing.T) {
	inst := newOpenTestApp(t, &fakeOpenBus{})
	require.True(t, inst.offerOpenInTally("SELECT path FROM fs(0xF5F5019800020001, 12345)"))
	assert.Equal(t, "SELECT path FROM fs(0xF5F5019800020001, 12345)", inst.tallyOfferSql)
	assert.False(t, inst.offerOpenInTally("SELECT 1"))
	assert.True(t, inst.offerOpenInTally("SELECT path FROM fs(0xF5F5019800020001, 12345)"))
}

func TestRequestOpenTally_ComposesRequestNamingTally(t *testing.T) {
	bus := &fakeOpenBus{reply: launchreply.LaunchReply{WindowKey: 11}}
	inst := newOpenTestApp(t, bus)

	cfg := tallylaunch.TallyLaunch{
		Sql:      "SELECT path FROM fs(0xF5F5019800020001, 12345)",
		SqlLabel: "from play",
		Tab:      tallylaunch.TabResults,
		Target:   "A",
	}
	inst.requestOpenTally(cfg)

	inst.openTallyMu.Lock()
	defer inst.openTallyMu.Unlock()
	assert.Empty(t, inst.openTallyErr)
	assert.False(t, inst.openTallyBusy)
	assert.Equal(t, windowhost.OpenSubject, bus.gotSubject)

	req, err := buscodec.Decode[launchrequest.LaunchRequest](bus.gotPayload)
	require.NoError(t, err)
	assert.Equal(t, tallylaunch.AppId, req.TargetAppId)
	assert.Equal(t, tallylaunch.Kind, req.ConfigKind)
	sent, err := buscodec.Decode[tallylaunch.TallyLaunch](req.Config)
	require.NoError(t, err)
	assert.Equal(t, cfg.Sql, sent.Sql)
	assert.Equal(t, cfg.SqlLabel, sent.SqlLabel)
	assert.Equal(t, tallylaunch.TabResults, sent.Tab)
}

// A buffer naming an ad-hoc dataset is NOT rewritten to handle form on this
// path, unlike the playground hand-off: tally reads the server, where a
// handle does not resolve either. The offer gate is what keeps such a buffer
// away from here; the op does not paper over it.
func TestRequestOpenTally_DoesNotRewriteDatasetAliases(t *testing.T) {
	bus := &fakeOpenBus{reply: launchreply.LaunchReply{WindowKey: 12}}
	client := NewClient(ClientConfig{URL: "http://example.invalid"}, nil)
	inst := NewPlayApp(client, newLiveQueryGraph(client, memory.NewGoAllocator(), 4), "", nil)
	inst.SetCapabilities(bus, nil, zerolog.Nop())
	require.NoError(t, inst.BindDataset("items", "adhoc_deadbeef01234567"))

	inst.requestOpenTally(tallylaunch.TallyLaunch{Sql: "SELECT * FROM keelson('items')"})

	req, err := buscodec.Decode[launchrequest.LaunchRequest](bus.gotPayload)
	require.NoError(t, err)
	sent, err := buscodec.Decode[tallylaunch.TallyLaunch](req.Config)
	require.NoError(t, err)
	assert.Contains(t, sent.Sql, "keelson('items')")
}

func TestOpenTallyWithoutABusIsAnError(t *testing.T) {
	inst := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 4), "", nil)
	err := inst.openTally(tallylaunch.TallyLaunch{Sql: "SELECT path FROM fs(0xF5F5019800020001, 12345)"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no bus wired")
}
