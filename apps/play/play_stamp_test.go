package play

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/queryrunfacts"
)

func TestComposeLogCommentRoundTrips(t *testing.T) {
	c := NewClient(ClientConfig{URL: "http://localhost:8123/"}, nil)
	c.SetStampIdentity("run-abc", "github.com/stergiotis/boxer/apps/play")
	opts := newExecOptions("main")
	lc := c.composeLogComment("SELECT 1 -- authored", "SELECT 1 FORMAT ArrowStream",
		map[string]string{"param_a": "1"}, map[string]string{"param_b": "2"}, opts)
	require.NotEmpty(t, lc)

	st, ok := queryrunfacts.ParseStamp(lc)
	require.True(t, ok, "the capture side must parse the produced stamp")
	require.Equal(t, "run-abc", st.RunId)
	require.Equal(t, "github.com/stergiotis/boxer/apps/play", st.App)
	require.Equal(t, "main", st.Lane)
	require.Len(t, st.AuthoredFp, 16)
	require.Len(t, st.SentFp, 16)
	require.Len(t, st.ChainFp, 16)
	require.Len(t, st.EnvFp, 16)
	require.NotEqual(t, st.AuthoredFp, st.SentFp, "authored and sent texts differ here")
}

func TestStampFingerprintStability(t *testing.T) {
	require.Equal(t, stampFp("SELECT 1"), stampFp("SELECT 1"))
	require.NotEqual(t, stampFp("SELECT 1"), stampFp("SELECT 2"))
	require.Len(t, stampFp(""), 16)
}

func TestEnvFingerprint(t *testing.T) {
	require.Empty(t, envFingerprint(nil, nil), "no binding → no fingerprint")

	a := envFingerprint(map[string]string{"param_x": "1", "param_y": "2"}, nil)
	b := envFingerprint(map[string]string{"param_y": "2", "param_x": "1"}, nil)
	require.Equal(t, a, b, "map order must not move the fingerprint")

	// A SET-bound constant shadows a same-named signal — the fingerprint
	// follows what actually rides the URL.
	shadowed := envFingerprint(map[string]string{"param_x": "set"}, map[string]string{"param_x": "sig"})
	require.Equal(t, envFingerprint(map[string]string{"param_x": "set"}, nil), shadowed)
	require.NotEqual(t, envFingerprint(nil, map[string]string{"param_x": "sig"}), shadowed)
}

func TestChainFingerprintTracksRegime(t *testing.T) {
	c1 := NewClient(ClientConfig{URL: "http://localhost:8123/"}, nil)
	c2 := NewClient(ClientConfig{URL: "http://localhost:8123/"}, nil)
	require.Equal(t, c1.chainFingerprint(), c2.chainFingerprint(),
		"same pass regime → same chain fingerprint")

	before := c1.chainFingerprint()
	c1.SetExposeConditions(true)
	require.NotEqual(t, before, c1.chainFingerprint(),
		"the ADR-0121 rewrite toggle is part of the regime")
}

func TestComposeProbeLogComment(t *testing.T) {
	c := NewClient(ClientConfig{URL: "http://localhost:8123/"}, nil)
	require.Empty(t, c.composeProbeLogComment(nil), "no identity, no lane → no stamp")

	c.SetStampIdentity("run-1", "app-1")
	lc := c.composeProbeLogComment(newExecOptions("diagnostics"))
	st, ok := queryrunfacts.ParseStamp(lc)
	require.True(t, ok)
	require.Equal(t, "run-1", st.RunId)
	require.Equal(t, "app-1", st.App)
	require.Equal(t, "diagnostics", st.Lane)
	require.Empty(t, st.AuthoredFp, "a probe is not an executed definition")
	require.Empty(t, st.EnvFp)
}

// TestExecuteArrowStreamCarriesStamp pins the wire: the stamp rides the
// URL as the log_comment parameter alongside query_id, exactly like a
// param — endpoints that don't know it ignore it.
func TestExecuteArrowStreamCarriesStamp(t *testing.T) {
	var gotLogComment, gotQueryID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLogComment = r.URL.Query().Get("log_comment")
		gotQueryID = r.URL.Query().Get("query_id")
		w.WriteHeader(http.StatusOK) // body is not a valid ArrowStream; the decode error is irrelevant here
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{URL: srv.URL}, nil)
	c.SetStampIdentity("run-wire", "app-wire")
	opts := newExecOptions("map")
	_, _, _, _ = c.ExecuteArrowStream(context.Background(), "SELECT 1", memory.NewGoAllocator(), opts, nil)

	require.Equal(t, opts.QueryID, gotQueryID)
	st, ok := queryrunfacts.ParseStamp(gotLogComment)
	require.True(t, ok, "log_comment must carry a parseable stamp, got %q", gotLogComment)
	require.Equal(t, "run-wire", st.RunId)
	require.Equal(t, "app-wire", st.App)
	require.Equal(t, "map", st.Lane)
	require.NotEmpty(t, st.AuthoredFp)
	require.NotEmpty(t, st.SentFp)
	require.NotEmpty(t, st.ChainFp)
}
