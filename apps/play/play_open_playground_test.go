package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/play/launchcfg"
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
	inst = NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 4), "SELECT 1")
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
