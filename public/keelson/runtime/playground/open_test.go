package playground_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchrequest"
	"github.com/stergiotis/boxer/public/keelson/runtime/playground"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// fakeBus captures the one Request Open makes and answers it.
type fakeBus struct {
	subject string
	payload []byte
	reply   launchreply.LaunchReply
}

var _ app.BusI = (*fakeBus)(nil)

func (f *fakeBus) Publish(subject string, payload []byte) (err error) { return }
func (f *fakeBus) Subscribe(subject string, handler app.MsgHandlerFunc) (unsubscribe func(), err error) {
	return
}
func (f *fakeBus) RequestWithTimeout(subject string, payload []byte, _ time.Duration) ([]byte, error) {
	return f.Request(subject, payload)
}
func (f *fakeBus) Request(subject string, payload []byte) (reply []byte, err error) {
	f.subject, f.payload = subject, payload
	return buscodec.Encode(f.reply)
}

// TestOpenNamesTheKindAndNoApp pins the handover's wire form: the request
// carries the kind and the config and no target, so the host resolves it
// (ADR-0361 §SD2, §SD8).
func TestOpenNamesTheKindAndNoApp(t *testing.T) {
	bus := &fakeBus{reply: launchreply.LaunchReply{WindowKey: 9}}

	key, err := playground.Open(bus, playground.Launch{Sql: "SELECT 1", AutoRun: true, Endpoint: playground.EndpointIntrospection})
	require.NoError(t, err)
	assert.Equal(t, uint64(9), key)
	assert.Equal(t, windowhost.OpenSubject, bus.subject)

	req, err := buscodec.Decode[launchrequest.LaunchRequest](bus.payload)
	require.NoError(t, err)
	assert.Empty(t, req.TargetAppId)
	assert.Equal(t, playground.Kind, req.ConfigKind)
	sent, err := buscodec.Decode[playground.Launch](req.Config)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1", sent.Sql)
	assert.True(t, sent.AutoRun)
	assert.Equal(t, playground.EndpointIntrospection, sent.Endpoint)
}

// TestOpenSurfacesARefusal turns a refusal reply into an error.
func TestOpenSurfacesARefusal(t *testing.T) {
	bus := &fakeBus{reply: launchreply.LaunchReply{Reason: "no app in this host takes launch kind"}}
	_, err := playground.Open(bus, playground.Launch{Sql: "SELECT 1"})
	assert.ErrorContains(t, err, "open refused: no app in this host takes launch kind")
}
