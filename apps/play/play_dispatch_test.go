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
