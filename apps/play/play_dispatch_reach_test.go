package play

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

// fakeProbe stands in for the plane's probe primitive: a nonce is fetched
// only if the runner actually asked for it, which is the whole property the
// real one has.
type fakeProbe struct {
	mu      sync.Mutex
	minted  int
	fetched map[string]bool
	mintErr error
}

func (inst *fakeProbe) MintProbe() (nonce string, url string, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.mintErr != nil {
		err = inst.mintErr
		return
	}
	inst.minted++
	nonce = "nonce" + string(rune('a'+inst.minted-1))
	url = "http://plane.invalid/probe/" + nonce
	return
}

func (inst *fakeProbe) CheckProbe(nonce string) (fetched bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	fetched = inst.fetched[nonce]
	delete(inst.fetched, nonce) // single-use, like the real one
	return
}

func (inst *fakeProbe) markFetched(nonce string) {
	inst.mu.Lock()
	if inst.fetched == nil {
		inst.fetched = map[string]bool{}
	}
	inst.fetched[nonce] = true
	inst.mu.Unlock()
}

func withProbe(t *testing.T, p introspect.LocalProbeI) {
	t.Helper()
	introspect.SetLocalProbe(p)
	t.Cleanup(func() { introspect.SetLocalProbe(nil) })
}

func TestReachProverRecordsAndExpires(t *testing.T) {
	t.Parallel()
	now := time.Now()
	pr := newReachProver()
	pr.now = func() time.Time { return now }

	assert.False(t, pr.isProven("http://a.invalid"), "nothing is proven until it is demonstrated")
	pr.record("http://a.invalid")
	assert.True(t, pr.isProven("http://a.invalid"))

	// A proof is a statement about a moment, so it expires.
	now = now.Add(reachProofTTL + time.Second)
	assert.False(t, pr.isProven("http://a.invalid"), "a stale proof is not a proof")
	assert.False(t, pr.isProven(""), "and an empty endpoint proves nothing")
}

// TestReachProveNeedsTheEngineToActuallyFetch is R3 in one test: the
// demonstration is the fetch. A runner that merely succeeds proves nothing.
func TestReachProveNeedsTheEngineToActuallyFetch(t *testing.T) {
	probe := &fakeProbe{}
	withProbe(t, probe)
	pr := newReachProver()

	// A runner that runs the statement but never reaches the URL — the
	// tunnelled-engine case, which must fail safe.
	proven, err := pr.prove(context.Background(), "http://elsewhere.invalid",
		func(ctx context.Context, endpoint string, sql string) (err error) { return })
	require.NoError(t, err)
	assert.False(t, proven, "running the statement is not the same as fetching the nonce")
	assert.False(t, pr.isProven("http://elsewhere.invalid"))

	// A runner that does fetch it.
	proven, err = pr.prove(context.Background(), "http://local.invalid",
		func(ctx context.Context, endpoint string, sql string) (err error) {
			// The nonce is in the statement, which is how the engine would
			// have got it.
			assert.Contains(t, sql, "/probe/")
			probe.markFetched("nonceb")
			return
		})
	require.NoError(t, err)
	assert.True(t, proven)
	assert.True(t, pr.isProven("http://local.invalid"))
}

// TestReachProveFailureIsNotAnError: an engine that cannot run the statement
// is "not proven", not an error to escalate. Only this process's own failure
// to mint is reported.
func TestReachProveFailureIsNotAnError(t *testing.T) {
	probe := &fakeProbe{}
	withProbe(t, probe)
	pr := newReachProver()

	proven, err := pr.prove(context.Background(), "http://broken.invalid",
		func(ctx context.Context, endpoint string, sql string) (err error) {
			return assert.AnError
		})
	require.NoError(t, err, "the engine's failure is the answer, not an error")
	assert.False(t, proven)
}

func TestReachProveWithoutAPlane(t *testing.T) {
	t.Parallel()
	introspect.SetLocalProbe(nil)
	pr := newReachProver()
	_, err := pr.prove(context.Background(), "http://a.invalid",
		func(ctx context.Context, endpoint string, sql string) (err error) { return })
	assert.Error(t, err, "there is nothing to prove reachability TO")
}

// TestReachProveIsDeduped: several lanes hitting the same refusal must
// produce one demonstration, not one each.
func TestReachProveIsDeduped(t *testing.T) {
	t.Parallel()
	pr := newReachProver()
	done, ok := pr.begin("http://a.invalid")
	require.True(t, ok)
	_, second := pr.begin("http://a.invalid")
	assert.False(t, second, "a probe already under way claims the endpoint")
	done()
	_, third := pr.begin("http://a.invalid")
	assert.True(t, third, "and releases it when finished")
}

// TestConfineAllowsAProvenEndpoint closes the §SD5 loop through the wall: an
// endpoint that is not this plane, but has demonstrated it can reach it, is
// no longer refused — and the engine's own gate agrees, because both read
// the same proof rather than each other.
func TestConfineAllowsAProvenEndpoint(t *testing.T) {
	withSealedPlane(t, "http://127.0.0.1:9/query", "adhoc_secret")
	const other = "http://sibling.invalid"
	c := NewClient(ClientConfig{URL: other}, nil)

	dec := c.Dispatch("SELECT * FROM keelson('adhoc_secret')", "")
	require.Equal(t, dispatchClassRefused, dec.class, "unproven is refused")
	assert.Contains(t, dec.reason, "has not demonstrated")

	c.reach.record(other)
	dec = c.Dispatch("SELECT * FROM keelson('adhoc_secret')", "")
	assert.NotEqual(t, dispatchClassRefused, dec.class, "a demonstration widens the wall")
	assert.Equal(t, queryengine.SensitivityConfined, dec.sensitivity,
		"still confined — proven is not the same as ordinary")

	eng, err := c.engineFor(dec)
	require.NoError(t, err)
	st, _, err := eng.Deliver(context.Background(), queryengine.Request{
		SQL: "SELECT 1", Sensitivity: queryengine.SensitivityConfined,
	})
	// The engine's gate opened too. A refusal would have been an ERROR from
	// Deliver — the caller's-fault channel; what comes back instead is a
	// stream whose terminal failed on the unreachable host, which is
	// ADR-0144's split between "rejected" and "ran and ended badly".
	require.NoError(t, err, "the confinement gate must not refuse a proven endpoint")
	defer func() { _ = st.Close() }()
	_, term, cErr := queryengine.Collect(st)
	require.NoError(t, cErr)
	assert.Equal(t, runstream.TerminalFailed, term.State)
	assert.NotContains(t, term.Err.Error(), "confined", "it failed on the host, not on the wall")
}
