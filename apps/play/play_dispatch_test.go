package play

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/runid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingResolver captures what it was asked about, so a test can assert
// that two issuers resolved from the same SQL rather than merely landing on
// the same endpoint by luck.
type recordingResolver struct {
	mu        sync.Mutex
	residuals []string
	decisions []dispatchDecision
	answer    func(residual string, base string) dispatchDecision
}

var _ endpointResolverI = (*recordingResolver)(nil)

func (inst *recordingResolver) resolve(residual string, base string, _ string) (dec dispatchDecision) {
	dec = dispatchDecision{targetURL: base, class: dispatchClassManual, reason: "recorded"}
	if inst.answer != nil {
		dec = inst.answer(residual, base)
	}
	inst.mu.Lock()
	inst.residuals = append(inst.residuals, residual)
	inst.decisions = append(inst.decisions, dec)
	inst.mu.Unlock()
	return
}

func (inst *recordingResolver) seen() (residuals []string, decisions []dispatchDecision) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return append([]string(nil), inst.residuals...), append([]dispatchDecision(nil), inst.decisions...)
}

// okServer answers every request with 200 and body, recording how many it saw.
func okServer(t *testing.T, body []byte) (srv *httptest.Server, hits *int) {
	t.Helper()
	n := 0
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n++
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

// TestNewExecOptionsMintsAValidRunId ties play's lane minting to the
// identity contract: the id it produces must be one every consumer accepts,
// including the poller, which rejects rather than escapes.
func TestNewExecOptionsMintsAValidRunId(t *testing.T) {
	opts := newExecOptions("main")
	require.NotEmpty(t, opts.QueryID)
	assert.True(t, runid.Valid(opts.QueryID), "id=%q", opts.QueryID)
	assert.Contains(t, opts.QueryID, runid.HostToken(), "the host component is what makes it safe on a shared channel")
	assert.Equal(t, "main", opts.Label, "the label rides separately; nothing parses it back out")

	// A lane label that is not a literal — a bound graph node carries the
	// node's id — must not be able to smuggle anything through.
	rough := newExecOptions("bound-node'; DROP--")
	assert.True(t, runid.Valid(rough.QueryID), "id=%q", rough.QueryID)
	assert.NotContains(t, rough.QueryID, "'")
}

func TestDispatchClassNames(t *testing.T) {
	seen := make(map[string]struct{}, len(allDispatchClasses))
	for _, cl := range allDispatchClasses {
		name := cl.String()
		require.NotEmpty(t, name)
		_, dup := seen[name]
		assert.False(t, dup, "class names must be distinct, %q repeats", name)
		seen[name] = struct{}{}
	}
	assert.Equal(t, "unresolved", dispatchClassE(200).String())
}

// TestDispatchStaticResolverIsThePinnedEndpoint pins the frozen behavior the
// seam was installed under: with no resolver, every statement goes where the
// endpoint switcher points, and SetURL moves it.
func TestDispatchStaticResolverIsThePinnedEndpoint(t *testing.T) {
	c := NewClient(ClientConfig{URL: "http://first.invalid"}, nil)

	dec := c.Dispatch("SELECT 1", "")
	assert.Equal(t, "http://first.invalid", dec.targetURL)
	assert.Equal(t, dispatchClassManual, dec.class)
	assert.NotEmpty(t, dec.reason, "a decision always carries a reason")

	c.SetURL("http://second.invalid")
	assert.Equal(t, "http://second.invalid", c.Dispatch("SELECT 1", "").targetURL)

	// A nil resolver restores the static one.
	c.SetResolver(&recordingResolver{answer: func(_ string, _ string) dispatchDecision {
		return dispatchDecision{targetURL: "http://third.invalid", class: dispatchClassIntrospection}
	}})
	assert.Equal(t, "http://third.invalid", c.Dispatch("SELECT 1", "").targetURL)
	c.SetResolver(nil)
	assert.Equal(t, "http://second.invalid", c.Dispatch("SELECT 1", "").targetURL)
}

// TestDispatchZeroDecisionIsRejected is what makes the parameter required in
// substance and not just in signature: a caller that forgets to resolve gets
// a loud failure, never a silent fallback to some ambient endpoint.
func TestDispatchZeroDecisionIsRejected(t *testing.T) {
	srv, hits := okServer(t, emptyArrowStream(t))
	c := NewClient(ClientConfig{URL: srv.URL}, srv.Client())

	_, _, _, err := c.ExecuteArrowStream(context.Background(), "SELECT 1", memory.NewGoAllocator(), nil, nil, dispatchDecision{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "names no endpoint")

	err = c.ProbeStatement(context.Background(), "SELECT 1", nil, nil, dispatchDecision{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "names no endpoint")

	assert.Zero(t, *hits, "an unresolved run must not reach a server")
}

// TestDispatchRefusalStopsTheRun covers the decision shape a policy resolver
// needs when no endpoint may serve a statement: the reason reaches the user
// and nothing is sent.
func TestDispatchRefusalStopsTheRun(t *testing.T) {
	srv, hits := okServer(t, emptyArrowStream(t))
	c := NewClient(ClientConfig{URL: srv.URL}, srv.Client())
	c.SetResolver(&recordingResolver{answer: func(_ string, _ string) dispatchDecision {
		return dispatchDecision{class: dispatchClassRefused, reason: "names both planes"}
	}})

	dec := c.Dispatch("SELECT 1", "")
	_, _, _, err := c.ExecuteArrowStream(context.Background(), "SELECT 1", memory.NewGoAllocator(), nil, nil, dec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "names both planes", "the refusal reason must reach the user")
	assert.Zero(t, *hits, "a refused run must not reach a server")
}

// TestDispatchLastDecisionFeedsTheToolbar covers what the Auto label reads:
// nothing before the first resolution, then the decision that actually
// carried the last run.
func TestDispatchLastDecisionFeedsTheToolbar(t *testing.T) {
	c := NewClient(ClientConfig{URL: "http://base.invalid"}, nil)
	_, ok := c.LastDecision()
	assert.False(t, ok, "no decision before anything ran")

	want := c.Dispatch("SELECT 1", "")
	got, ok := c.LastDecision()
	require.True(t, ok)
	assert.Equal(t, want, got)
	assert.Contains(t, got.describe(), "http://base.invalid")
	assert.Contains(t, got.describe(), got.reason)
}

func TestDispatchDescribeNamesRefusal(t *testing.T) {
	dec := dispatchDecision{class: dispatchClassRefused, reason: "names both planes"}
	assert.Contains(t, dec.describe(), "refused")
	assert.Contains(t, dec.describe(), "names both planes")
}

// TestRunsOnIntrospectionFollowsTheResolver is the applet-stamping rule. The
// old probe compared the client's URL against the local endpoint, which under
// Auto answers about the pinned base rather than about where the buffer runs
// — so a keelson-only buffer would be stamped for the default target and come
// back somewhere its bare keelson('…') dialect does not work.
func TestRunsOnIntrospectionFollowsTheResolver(t *testing.T) {
	const local = "http://127.0.0.1:9998/query"
	prev := introspect.LocalQueryEndpoint()
	introspect.SetLocalQueryEndpoint(local)
	t.Cleanup(func() { introspect.SetLocalQueryEndpoint(prev) })

	cl := NewClient(ClientConfig{URL: "http://base.invalid"}, nil)
	app := &PlayApp{client: cl}

	// Auto off: the pinned base is the target, so nothing is stamped even for
	// a keelson-only buffer.
	assert.False(t, app.runsOnIntrospection("SELECT * FROM keelson('env')"))

	// Auto on: the same buffer now really does run on the introspection
	// plane, and the stamp must follow.
	cl.SetResolver(autoResolver)
	assert.True(t, app.runsOnIntrospection("SELECT * FROM keelson('env')"))
	assert.False(t, app.runsOnIntrospection("SELECT * FROM db.events"),
		"a plain buffer still runs on the pinned base")

	// Pinned directly at the introspection endpoint, Auto off: the old probe's
	// case, which must keep answering the same way.
	cl.SetResolver(nil)
	cl.SetURL(local)
	assert.True(t, app.runsOnIntrospection("SELECT * FROM db.events"))
}

func TestRunsOnIntrospectionWithoutAPublishedEndpoint(t *testing.T) {
	prev := introspect.LocalQueryEndpoint()
	introspect.SetLocalQueryEndpoint("")
	t.Cleanup(func() { introspect.SetLocalQueryEndpoint(prev) })

	cl := NewClient(ClientConfig{URL: "http://base.invalid"}, nil)
	cl.SetResolver(autoResolver)
	app := &PlayApp{client: cl}
	assert.False(t, app.runsOnIntrospection("SELECT * FROM keelson('env')"))

	assert.False(t, (&PlayApp{}).runsOnIntrospection("SELECT 1"), "nil client is not a crash")
}

// TestDispatchProbeAndRunShareOneDecision is the divergence guard. The
// diagnostics probe wraps the residual in EXPLAIN AST, and that wrapper
// parses differently and names none of the residual's tables — so a probe
// that resolved from its own text could be answered about a different
// server than the run it claims to describe, and its verdict would stop
// meaning anything. Both must resolve from the same residual.
func TestDispatchProbeAndRunShareOneDecision(t *testing.T) {
	srv, _ := okServer(t, emptyArrowStream(t))
	c := NewClient(ClientConfig{URL: srv.URL}, srv.Client())
	rec := &recordingResolver{}
	c.SetResolver(rec)

	// A buffer whose EXPLAIN-wrapped form would resolve differently under any
	// policy that looks at the statement: the macro is invisible through the
	// wrapper, which Grammar1 cannot parse at all.
	const buffer = "SELECT name FROM keelson('env')"

	runDec := c.Dispatch(buffer, "")

	d := NewDiagnosticsDriver(c)
	defer d.close()
	d.noteParse(buffer, errors.New("grammar1: forced probe"))
	require.True(t, strings.HasPrefix(d.probeNode.SQL, diagProbePrefix), "probe wraps the residual")

	_, _, _, err := probeExecutor{client: c}.execute(context.Background(), d.probeNode, memory.NewGoAllocator())
	require.NoError(t, err)

	residuals, decisions := rec.seen()
	require.Len(t, residuals, 2, "one resolution for the run, one for the probe")
	assert.Equal(t, residuals[0], residuals[1],
		"probe and run must resolve from the same SQL, not from the EXPLAIN wrapper")
	assert.NotContains(t, residuals[1], "EXPLAIN", "the probe must not resolve from its own wrapper")
	assert.Equal(t, runDec, decisions[1], "probe and run must consume the same decision")
}

// TestDispatchRecordsTheResolvedMember covers what R11 needs and boxer's own
// endpoints never exercise: a resolver with members to choose between says
// WHICH one it picked, and a cancellation is addressed there rather than at
// the placement.
func TestDispatchRecordsTheResolvedMember(t *testing.T) {
	// The roster is the site's, not boxer's — a test is the only place one
	// appears in this repository.
	placement := []string{"http://a.invalid:8123", "http://b.invalid:8123", "http://c.invalid:8123"}
	c := NewClient(ClientConfig{URL: "http://cluster.invalid"}, nil)
	c.SetResolver(&recordingResolver{answer: func(_ string, base string) dispatchDecision {
		member, _ := queryengine.SelectMember(placement, "generation-3")
		return dispatchDecision{
			targetURL: base,
			class:     dispatchClassManual,
			reason:    "cluster placement, generation-pinned member",
			member:    member,
		}
	}})

	dec := c.Dispatch("SELECT 1", "generation-3")
	require.NotEmpty(t, dec.member)
	assert.Contains(t, placement, dec.member)

	target, err := dec.killTarget()
	require.NoError(t, err)
	assert.Equal(t, dec.member, target,
		"a kill reaches only the member that ran the query, never the placement")
	assert.Contains(t, dec.describe(), dec.member, "an audit of the run wants the member too (R12)")
}

// TestDispatchWithoutMembersKillsAtTheTarget is boxer's own case: no
// placement, so the target IS the member and nothing extra is displayed.
func TestDispatchWithoutMembersKillsAtTheTarget(t *testing.T) {
	c := NewClient(ClientConfig{URL: "http://only.invalid"}, nil)
	dec := c.Dispatch("SELECT 1", "")
	assert.Empty(t, dec.member, "boxer's endpoints have no members to choose between")

	target, err := dec.killTarget()
	require.NoError(t, err)
	assert.Equal(t, "http://only.invalid", target)
	assert.NotContains(t, dec.describe(), "()", "nothing to name means nothing shown")
}

// TestDispatchRefusedHasNoKillTarget: a run that never happened cannot be
// cancelled, and asking says so rather than answering with the base.
func TestDispatchRefusedHasNoKillTarget(t *testing.T) {
	dec := dispatchDecision{class: dispatchClassRefused, reason: "names both planes"}
	_, err := dec.killTarget()
	assert.Error(t, err)
}

// withSealedPlane publishes a fake local introspection plane whose named
// datasets are sealed, and restores the process globals afterwards.
func withSealedPlane(t *testing.T, endpoint string, sealed ...string) {
	t.Helper()
	prevEndpoint := introspect.LocalQueryEndpoint()
	introspect.SetLocalQueryEndpoint(endpoint)
	introspect.SetLocalSealedPredicate(func(name string) (yes bool) {
		for _, s := range sealed {
			if s == name {
				return true
			}
		}
		return
	})
	t.Cleanup(func() {
		introspect.SetLocalQueryEndpoint(prevEndpoint)
		introspect.SetLocalSealedPredicate(nil)
	})
}

// TestConfineRefusesSealedDataOffThePlane is ADR-0145 §SD4's resolver-side
// wall. The static resolver answers with the pinned endpoint and knows
// nothing about sealed data — which is the point: the wall sits ABOVE every
// resolver, so a router cannot route around it (R2).
func TestConfineRefusesSealedDataOffThePlane(t *testing.T) {
	withSealedPlane(t, "http://127.0.0.1:9/query", "adhoc_secret")
	c := NewClient(ClientConfig{URL: "http://elsewhere.invalid"}, nil)

	dec := c.Dispatch("SELECT * FROM keelson('adhoc_secret')", "")
	assert.Equal(t, dispatchClassRefused, dec.class)
	assert.Equal(t, queryengine.SensitivityConfined, dec.sensitivity)
	assert.Contains(t, dec.reason, "adhoc_secret", "the reason names the evidence")
	assert.Contains(t, dec.reason, "must not leave this box")
	assert.Contains(t, dec.reason, "elsewhere.invalid", "and names where it would have gone")

	_, err := dec.target()
	require.Error(t, err, "a refused decision cannot be dispatched")
}

// TestConfineAllowsSealedDataOnThePlane: the one endpoint exempt is this
// process's own plane, and the exemption is by identity — the same string
// the plane published.
func TestConfineAllowsSealedDataOnThePlane(t *testing.T) {
	const plane = "http://127.0.0.1:9/query"
	withSealedPlane(t, plane, "adhoc_secret")
	c := NewClient(ClientConfig{URL: plane}, nil)

	dec := c.Dispatch("SELECT * FROM keelson('adhoc_secret')", "")
	require.NotEqual(t, dispatchClassRefused, dec.class, "reason=%q", dec.reason)
	assert.Equal(t, queryengine.SensitivityConfined, dec.sensitivity,
		"still confined — allowed is not the same as ordinary")
	assert.Contains(t, dec.describe(), "confined", "an audit of the run records it (R12)")
}

// TestConfineLeavesOrdinaryRunsAlone: a query naming an unsealed keelson
// table is not confined, and the wall does not touch it.
func TestConfineLeavesOrdinaryRunsAlone(t *testing.T) {
	withSealedPlane(t, "http://127.0.0.1:9/query", "adhoc_secret")
	c := NewClient(ClientConfig{URL: "http://elsewhere.invalid"}, nil)

	dec := c.Dispatch("SELECT * FROM keelson('env')", "")
	assert.NotEqual(t, dispatchClassRefused, dec.class)
	assert.Equal(t, queryengine.SensitivityOrdinary, dec.sensitivity)
	assert.NotContains(t, dec.describe(), "confined")
}

// TestConfineWithNoLocalPlane: with no plane running there is no sealed data
// to confine, so nothing is refused for confinement.
func TestConfineWithNoLocalPlane(t *testing.T) {
	prev := introspect.LocalQueryEndpoint()
	introspect.SetLocalQueryEndpoint("")
	introspect.SetLocalSealedPredicate(nil)
	t.Cleanup(func() { introspect.SetLocalQueryEndpoint(prev) })

	c := NewClient(ClientConfig{URL: "http://elsewhere.invalid"}, nil)
	dec := c.Dispatch("SELECT * FROM keelson('adhoc_secret')", "")
	assert.Equal(t, queryengine.SensitivityOrdinary, dec.sensitivity)
	assert.NotEqual(t, dispatchClassRefused, dec.class)
}

// TestConfinePreservesAnExistingRefusal: a decision already refused for
// another reason keeps that reason rather than having it overwritten.
func TestConfinePreservesAnExistingRefusal(t *testing.T) {
	withSealedPlane(t, "http://127.0.0.1:9/query", "adhoc_secret")
	c := NewClient(ClientConfig{URL: "http://elsewhere.invalid"}, nil)
	in := dispatchDecision{class: dispatchClassRefused, reason: "names both planes"}
	out := c.confine("SELECT * FROM keelson('adhoc_secret')", in)
	assert.Equal(t, "names both planes", out.reason)
}

// TestConfinedRunIsRefusedByTheEngineEvenWhenPlaced is why ADR-0145 §SD4
// has two refusals rather than one. This decision is what a site resolver
// could hand back — confined, yet placed somewhere that is not this
// process's plane — with the wall above the resolver bypassed. The engine
// refuses on its own account, and nothing reaches the server.
func TestConfinedRunIsRefusedByTheEngineEvenWhenPlaced(t *testing.T) {
	withSealedPlane(t, "http://127.0.0.1:9/query", "adhoc_secret")
	srv, hits := okServer(t, emptyArrowStream(t))
	c := NewClient(ClientConfig{URL: srv.URL}, srv.Client())

	dec := dispatchDecision{
		targetURL:   srv.URL,
		class:       dispatchClassManual,
		reason:      "a site resolver placed it here",
		sensitivity: queryengine.SensitivityConfined,
	}
	_, _, _, err := c.ExecuteArrowStream(context.Background(), "SELECT * FROM keelson('adhoc_secret')",
		memory.NewGoAllocator(), nil, nil, dec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "confined")
	assert.Zero(t, *hits, "the run must not reach a server that was never cleared for it")

	// The probe issuer shares the decision, so it is refused for the same
	// reason rather than quietly attesting about a server the run never met.
	err = c.ProbeStatement(context.Background(), "SELECT 1", nil, nil, dec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "confined")
}

// TestSealedNamesSeesBothSpellings closes a syntactic hole the wall must not
// have. keelson('x') and x are interchangeable in the introspection dialect
// (ADR-0094 §SD4), so labelling only the macro form would have exempted a
// run from confinement purely because of how it was written.
func TestSealedNamesSeesBothSpellings(t *testing.T) {
	withSealedPlane(t, "http://127.0.0.1:9/query", "adhoc_secret")

	assert.Equal(t, []string{"adhoc_secret"}, sealedNames("SELECT * FROM keelson('adhoc_secret')"))
	assert.Equal(t, []string{"adhoc_secret"}, sealedNames("SELECT * FROM adhoc_secret"),
		"the bare spelling names the same sealed data")
	assert.Equal(t, []string{"adhoc_secret"},
		sealedNames("SELECT * FROM adhoc_secret JOIN keelson('adhoc_secret') USING (v)"),
		"named twice is still one dataset")
	assert.Empty(t, sealedNames("SELECT * FROM ordinary_table"))
	assert.Empty(t, sealedNames("NOT SQL (("), "unparseable names nothing")
}

// TestConfineRefusesABareSealedReference is the same hole seen through the
// wall rather than the derivation.
func TestConfineRefusesABareSealedReference(t *testing.T) {
	withSealedPlane(t, "http://127.0.0.1:9/query", "adhoc_secret")
	c := NewClient(ClientConfig{URL: "http://elsewhere.invalid"}, nil)

	dec := c.Dispatch("SELECT * FROM adhoc_secret", "")
	assert.Equal(t, dispatchClassRefused, dec.class)
	assert.Equal(t, queryengine.SensitivityConfined, dec.sensitivity)
}

// TestZeroClientIsConservativeNotFatal: a Client built as a literal has no
// prover, and several tests in this package build one. Nothing may panic,
// and nothing may count as proven.
func TestZeroClientIsConservativeNotFatal(t *testing.T) {
	withSealedPlane(t, "http://127.0.0.1:9/query", "adhoc_secret")
	c := &Client{}

	dec := c.confine("SELECT * FROM keelson('adhoc_secret')",
		dispatchDecision{targetURL: "http://x.invalid", class: dispatchClassManual})
	assert.Equal(t, dispatchClassRefused, dec.class, "no prover means nothing is proven")

	assert.False(t, c.reach.isProven("http://x.invalid"))
	c.reach.record("http://x.invalid")
	assert.False(t, c.reach.isProven("http://x.invalid"), "recording into nothing records nothing")
	_, ok := c.reach.begin("http://x.invalid")
	assert.False(t, ok)
	_, err := c.reach.prove(context.Background(), "http://x.invalid",
		func(ctx context.Context, endpoint string, sql string) (e error) { return })
	assert.Error(t, err)
}
