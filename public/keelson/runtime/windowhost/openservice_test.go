package windowhost

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchrequest"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

const testCallerAppId app.AppIdT = "test.caller"

// openServiceFixture wires bus + host + audit + service + a caller
// client holding the windowhost.open Pub cap — the caller-side shape
// M6's applet manifests declare.
type openServiceFixture struct {
	bus    *inprocbus.Inst
	host   *Inst
	svc    *OpenService
	facts  *factsstore.InMemoryFactsStore
	caller *inprocbus.Client
}

func mkOpenServiceFixture(t *testing.T, reg *app.Registry) (fx openServiceFixture) {
	t.Helper()
	fx.bus = inprocbus.NewInst(zerolog.Nop())
	fx.bus.SetRequestTimeout(2 * time.Second)
	fx.host = NewInst(reg, zerolog.Nop())
	fx.host.SetBus(fx.bus)
	fx.facts = factsstore.NewInMemoryFactsStore()
	fx.host.SetAudit("run-1", fx.facts)
	svc, err := NewOpenService(fx.bus, fx.host, zerolog.Nop())
	require.NoError(t, err)
	fx.svc = svc
	t.Cleanup(svc.Close)
	fx.caller = fx.bus.NewClient(testCallerAppId, []app.SubjectFilter{
		{Pattern: OpenSubject, Direction: app.CapDirectionPub},
	})
	return
}

func (fx openServiceFixture) request(t *testing.T, req launchrequest.LaunchRequest) (rep launchreply.LaunchReply) {
	t.Helper()
	payload, err := buscodec.Encode(req)
	require.NoError(t, err)
	replyBytes, err := fx.caller.Request(OpenSubject, payload)
	require.NoError(t, err, "a refusal must be a reply, not a timeout")
	rep, err = buscodec.Decode[launchreply.LaunchReply](replyBytes)
	require.NoError(t, err)
	return
}

func TestOpenService_OpenWithConfigEndToEnd(t *testing.T) {
	reg := app.NewRegistry()
	la := &launchApp{manifest: mkLaunchManifest("test.launch", testCfgKind)}
	require.NoError(t, reg.Register(la))
	fx := mkOpenServiceFixture(t, reg)

	rep := fx.request(t, launchrequest.LaunchRequest{
		At:          time.Now().UTC(),
		TargetAppId: "test.launch",
		ConfigKind:  testCfgKind,
		Config:      testCfgBytes,
	})
	require.Empty(t, rep.Reason)
	require.NotZero(t, rep.WindowKey)
	require.Equal(t, 1, fx.host.Len())

	// The window delivers the bytes at Mount.
	fx.host.mu.Lock()
	w := fx.host.windows[0]
	fx.host.mu.Unlock()
	require.EqualValues(t, rep.WindowKey, w.key)
	require.NoError(t, w.appInst.Mount(w.mountCtx))
	assert.Equal(t, testCfgBytes, la.gotCfg)

	// The accepted request is persisted beside the lifecycle row (§SD6),
	// caller attributed from the bus envelope.
	launches := fx.facts.Launches()
	require.Len(t, launches, 1)
	assert.Equal(t, testCallerAppId, launches[0].CallerAppId)
	assert.Equal(t, app.AppIdT("test.launch"), launches[0].TargetAppId)
	assert.Equal(t, rep.WindowKey, launches[0].TileKey)
	assert.Equal(t, testCfgKind, launches[0].ConfigKind)
	assert.Equal(t, testCfgBytes, launches[0].Config)
	assert.Equal(t, "run-1", launches[0].RunId)
	lifecycles := fx.facts.Lifecycles()
	require.Len(t, lifecycles, 1)
	assert.Equal(t, launches[0].TileKey, lifecycles[0].TileKey, "launch row joins its started row on TileKey")
}

func TestOpenService_PlainOpenEndToEnd(t *testing.T) {
	reg, _ := mkRegistryWithSingleton(t, "test.plain")
	fx := mkOpenServiceFixture(t, reg)

	rep := fx.request(t, launchrequest.LaunchRequest{
		At:          time.Now().UTC(),
		TargetAppId: "test.plain",
	})
	require.Empty(t, rep.Reason)
	require.NotZero(t, rep.WindowKey)
	require.Equal(t, 1, fx.host.Len())

	launches := fx.facts.Launches()
	require.Len(t, launches, 1)
	assert.Empty(t, launches[0].ConfigKind)
	assert.Nil(t, launches[0].Config)
}

func TestOpenService_RefusalsAreRepliesNotTimeouts(t *testing.T) {
	reg := app.NewRegistry()
	la := &launchApp{manifest: mkLaunchManifest("test.launch", testCfgKind)}
	require.NoError(t, reg.Register(la))
	plain := &counterApp{manifest: mkManifest("test.plain")}
	require.NoError(t, reg.Register(plain))
	fx := mkOpenServiceFixture(t, reg)

	cases := []struct {
		name   string
		req    launchrequest.LaunchRequest
		marker string
	}{
		{
			name:   "unknown app",
			req:    launchrequest.LaunchRequest{TargetAppId: "test.absent"},
			marker: "not registered",
		},
		{
			name:   "no-args app given args",
			req:    launchrequest.LaunchRequest{TargetAppId: "test.plain", ConfigKind: testCfgKind, Config: testCfgBytes},
			marker: "accepts no launch config",
		},
		{
			name:   "kind mismatch",
			req:    launchrequest.LaunchRequest{TargetAppId: "test.launch", ConfigKind: "someOtherKind", Config: testCfgBytes},
			marker: "does not match",
		},
		{
			name:   "garbage envelope",
			req:    launchrequest.LaunchRequest{TargetAppId: "test.launch", ConfigKind: testCfgKind, Config: []byte("junk")},
			marker: "refused",
		},
		{
			name:   "oversize",
			req:    launchrequest.LaunchRequest{TargetAppId: "test.launch", ConfigKind: testCfgKind, Config: make([]byte, maxLaunchConfigBytes+1)},
			marker: "size cap",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.req.At = time.Now().UTC()
			rep := fx.request(t, tc.req)
			assert.Zero(t, rep.WindowKey)
			assert.Contains(t, rep.Reason, tc.marker)
		})
	}
	assert.Equal(t, 0, fx.host.Len(), "no window opened by any refusal")
	assert.Empty(t, fx.facts.Launches(), "refusals write no launch fact")
}

func TestOpenService_UndecodableRequestIsRepliedTo(t *testing.T) {
	reg, _ := mkRegistryWithSingleton(t, "test.plain")
	fx := mkOpenServiceFixture(t, reg)

	replyBytes, err := fx.caller.Request(OpenSubject, []byte("not a launch request"))
	require.NoError(t, err)
	rep, err := buscodec.Decode[launchreply.LaunchReply](replyBytes)
	require.NoError(t, err)
	assert.Zero(t, rep.WindowKey)
	assert.Contains(t, rep.Reason, "decode")
	assert.Equal(t, 0, fx.host.Len())
}

func TestOpenService_PublishWithoutReplyIsDropped(t *testing.T) {
	reg, _ := mkRegistryWithSingleton(t, "test.plain")
	fx := mkOpenServiceFixture(t, reg)

	payload, err := buscodec.Encode(launchrequest.LaunchRequest{
		At:          time.Now().UTC(),
		TargetAppId: "test.plain",
	})
	require.NoError(t, err)
	require.NoError(t, fx.caller.Publish(OpenSubject, payload))
	assert.Equal(t, 0, fx.host.Len(), "fire-and-forget publish must not open windows")
}
