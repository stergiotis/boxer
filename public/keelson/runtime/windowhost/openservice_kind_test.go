package windowhost

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchrequest"
)

// The tests in this file are ADR-0361's verification of resolution by
// kind: a fixture app taking a kind stands in for a playground. The rule is
// over kinds, so the test kind is any registered one.

func byKindReq() launchrequest.LaunchRequest {
	return launchrequest.LaunchRequest{At: time.Now().UTC(), ConfigKind: testCfgKind, Config: testCfgBytes}
}

func registerTakers(t *testing.T, reg *app.Registry, ids ...app.AppIdT) (apps []*launchApp) {
	t.Helper()
	for _, id := range ids {
		la := &launchApp{manifest: mkLaunchManifest(id, testCfgKind)}
		require.NoError(t, reg.Register(la))
		apps = append(apps, la)
	}
	return
}

func TestOpenByKind_ReachesTheSoleTaker(t *testing.T) {
	reg := app.NewRegistry()
	require.NoError(t, reg.Register(&counterApp{manifest: mkManifest("test.plain")}))
	require.NoError(t, reg.Register(&launchApp{manifest: mkLaunchManifest("test.other", "someOtherKind")}))
	takers := registerTakers(t, reg, "test.taker")
	fx := mkOpenServiceFixture(t, reg)

	rep := fx.request(t, byKindReq())
	require.Empty(t, rep.Reason)
	require.NotZero(t, rep.WindowKey)
	require.Equal(t, 1, fx.host.Len())

	fx.host.mu.Lock()
	w := fx.host.windows[0]
	fx.host.mu.Unlock()
	assert.Equal(t, app.AppIdT("test.taker"), w.manifest.Id)
	require.NoError(t, w.appInst.Mount(w.mountCtx))
	assert.Equal(t, testCfgBytes, takers[0].gotCfg)

	// The row names the app that opened and that it was resolved from the
	// kind (ADR-0361 §SD2): the audit row names the app, not the request's
	// absence of one.
	launches := fx.facts.Launches()
	require.Len(t, launches, 1)
	assert.Equal(t, app.AppIdT("test.taker"), launches[0].TargetAppId)
	assert.True(t, launches[0].ResolvedFromKind)
	assert.Equal(t, testCfgKind, launches[0].ConfigKind)
	assert.Equal(t, testCfgBytes, launches[0].Config)
	assert.Equal(t, testCallerAppId, launches[0].CallerAppId)
	assert.Equal(t, rep.WindowKey, launches[0].TileKey)
}

func TestOpenByKind_RefusedWithNoTaker(t *testing.T) {
	reg := app.NewRegistry()
	require.NoError(t, reg.Register(&launchApp{manifest: mkLaunchManifest("test.other", "someOtherKind")}))
	fx := mkOpenServiceFixture(t, reg)

	rep := fx.request(t, byKindReq())
	assert.Zero(t, rep.WindowKey)
	assert.Contains(t, rep.Reason, "no app in this host takes launch kind")
	assert.Contains(t, rep.Reason, testCfgKind)
	assert.Equal(t, 0, fx.host.Len())
	assert.Empty(t, fx.facts.Launches())
}

func TestOpenByKind_RefusedWithTwoUndesignatedNamingThem(t *testing.T) {
	reg := app.NewRegistry()
	registerTakers(t, reg, "test.taker.one", "test.taker.two")
	fx := mkOpenServiceFixture(t, reg)

	rep := fx.request(t, byKindReq())
	assert.Zero(t, rep.WindowKey)
	assert.Contains(t, rep.Reason, "none is designated")
	assert.Contains(t, rep.Reason, "test.taker.one")
	assert.Contains(t, rep.Reason, "test.taker.two")
	assert.Equal(t, 0, fx.host.Len(), "picking one by registration order would be a load-order selection")
	assert.Empty(t, fx.facts.Launches())
}

func TestOpenByKind_ReachesTheDesignatedTaker(t *testing.T) {
	reg := app.NewRegistry()
	registerTakers(t, reg, "test.taker.one", "test.taker.two")
	require.NoError(t, reg.DesignateForKind(testCfgKind, "test.taker.two"))
	fx := mkOpenServiceFixture(t, reg)

	rep := fx.request(t, byKindReq())
	require.Empty(t, rep.Reason)
	require.Equal(t, 1, fx.host.Len())

	launches := fx.facts.Launches()
	require.Len(t, launches, 1)
	assert.Equal(t, app.AppIdT("test.taker.two"), launches[0].TargetAppId)
	assert.True(t, launches[0].ResolvedFromKind)
}

func TestOpenByKind_SkipsATakerTheLaunchLimitRemoved(t *testing.T) {
	reg := app.NewRegistry()
	registerTakers(t, reg, "test.taker.one", "test.taker.two")
	// The limit removes one of two takers, so one candidate remains and no
	// designation is needed.
	reg.LimitLaunches([]app.AppIdT{"test.taker.two"})
	fx := mkOpenServiceFixture(t, reg)

	rep := fx.request(t, byKindReq())
	require.Empty(t, rep.Reason)
	launches := fx.facts.Launches()
	require.Len(t, launches, 1)
	assert.Equal(t, app.AppIdT("test.taker.two"), launches[0].TargetAppId)
}

func TestOpenByKind_LimitRemovingEveryTakerIsNoTaker(t *testing.T) {
	reg := app.NewRegistry()
	registerTakers(t, reg, "test.taker.one")
	reg.LimitLaunches(nil)
	fx := mkOpenServiceFixture(t, reg)

	rep := fx.request(t, byKindReq())
	assert.Zero(t, rep.WindowKey)
	assert.Contains(t, rep.Reason, "no app in this host takes launch kind")
}

func TestOpenByKind_DesignationNotAmongCandidatesIsRefused(t *testing.T) {
	reg := app.NewRegistry()
	registerTakers(t, reg, "test.taker.one", "test.taker.two", "test.taker.three")
	require.NoError(t, reg.DesignateForKind(testCfgKind, "test.taker.one"))
	// The limit removes the designated app but leaves two: a choice with no
	// valid designation, which the rule refuses rather than picks from.
	reg.LimitLaunches([]app.AppIdT{"test.taker.two", "test.taker.three"})
	fx := mkOpenServiceFixture(t, reg)

	rep := fx.request(t, byKindReq())
	assert.Zero(t, rep.WindowKey)
	assert.Contains(t, rep.Reason, "not launchable here")
	assert.Contains(t, rep.Reason, "test.taker.one")
	assert.Contains(t, rep.Reason, "test.taker.two")
	assert.Contains(t, rep.Reason, "test.taker.three")
	assert.Empty(t, fx.facts.Launches())
}

// A designation only chooses among several; one remaining candidate is the
// target even when the designated app is not among them.
func TestOpenByKind_OneCandidateIgnoresAnUnreachableDesignation(t *testing.T) {
	reg := app.NewRegistry()
	registerTakers(t, reg, "test.taker.one", "test.taker.two")
	require.NoError(t, reg.DesignateForKind(testCfgKind, "test.taker.one"))
	reg.LimitLaunches([]app.AppIdT{"test.taker.two"})
	fx := mkOpenServiceFixture(t, reg)

	rep := fx.request(t, byKindReq())
	require.Empty(t, rep.Reason)
	launches := fx.facts.Launches()
	require.Len(t, launches, 1)
	assert.Equal(t, app.AppIdT("test.taker.two"), launches[0].TargetAppId)
}

// A resolved open runs the by-id path: the probe, the size cap and the
// config delivery are the same (ADR-0361 §SD2).
func TestOpenByKind_RunsTheByIdChecks(t *testing.T) {
	reg := app.NewRegistry()
	registerTakers(t, reg, "test.taker")
	fx := mkOpenServiceFixture(t, reg)

	cases := []struct {
		name   string
		config []byte
		marker string
	}{
		{"garbage", []byte("junk"), "refused"},
		{"oversize", make([]byte, maxLaunchConfigBytes+1), "size cap"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := fx.request(t, launchrequest.LaunchRequest{At: time.Now().UTC(), ConfigKind: testCfgKind, Config: tc.config})
			assert.Zero(t, rep.WindowKey)
			assert.Contains(t, rep.Reason, tc.marker)
		})
	}
	assert.Equal(t, 0, fx.host.Len())
	assert.Empty(t, fx.facts.Launches())
}

// An open naming an app stays what it was: no resolution, no resolved row,
// and a kind mismatch is still the by-id refusal.
func TestOpenById_IsNotResolved(t *testing.T) {
	reg := app.NewRegistry()
	registerTakers(t, reg, "test.taker.one", "test.taker.two")
	fx := mkOpenServiceFixture(t, reg)

	rep := fx.request(t, launchrequest.LaunchRequest{At: time.Now().UTC(), TargetAppId: "test.taker.one", ConfigKind: testCfgKind, Config: testCfgBytes})
	require.Empty(t, rep.Reason)
	launches := fx.facts.Launches()
	require.Len(t, launches, 1)
	assert.Equal(t, app.AppIdT("test.taker.one"), launches[0].TargetAppId)
	assert.False(t, launches[0].ResolvedFromKind)
}

// The client half: RequestOpenKind names no target and the service resolves
// it; a refusal comes back as a RefusedError whose message carries the
// host's reason, as RequestOpen's do.
func TestRequestOpenKind_RoundTripAndRefusal(t *testing.T) {
	reg := app.NewRegistry()
	registerTakers(t, reg, "test.taker")
	fx := mkOpenServiceFixture(t, reg)

	key, err := RequestOpenKind(fx.caller, testCfgKind, testCfgBytes)
	require.NoError(t, err)
	assert.NotZero(t, key)
	require.Len(t, fx.facts.Launches(), 1)
	assert.Equal(t, app.AppIdT("test.taker"), fx.facts.Launches()[0].TargetAppId)

	_, err = RequestOpenKind(fx.caller, "", nil)
	assert.ErrorContains(t, err, "no config kind", "an open by kind needs a kind")

	_, err = RequestOpenKind(fx.caller, "someOtherKind", testCfgBytes)
	assert.ErrorContains(t, err, "open refused: no app in this host takes launch kind \"someOtherKind\"", "a refusal returns the host's reason, not a timeout")
	var refused *RefusedError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, "open", refused.Act)
	assert.Len(t, fx.facts.Launches(), 1, "a refusal writes no launch fact")
}

// The text a caller shows is the host's reason once, under one prefix: a
// by-id open of an app the host does not know reads "windowhost: open
// refused: app not registered", and an open by kind with two undesignated
// takers names them.
func TestRefusalTextCarriesTheReasonOnce(t *testing.T) {
	reg := app.NewRegistry()
	registerTakers(t, reg, "test.taker.a", "test.taker.b")
	fx := mkOpenServiceFixture(t, reg)

	_, err := RequestOpen(fx.caller, "test.nobody", "", nil)
	require.Error(t, err)
	assert.Equal(t, "windowhost: open refused: app not registered", err.Error())

	_, err = RequestOpenKind(fx.caller, testCfgKind, testCfgBytes)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "windowhost: open refused: several apps take launch kind")
	assert.Contains(t, err.Error(), "test.taker.a")
	assert.Contains(t, err.Error(), "test.taker.b")
	assert.NotContains(t, err.Error(), "refused: windowhost:", "the prefix appears once")
}
