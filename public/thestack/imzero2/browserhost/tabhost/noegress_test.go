package tabhost

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// NoEgress lets through the page's own origin and nothing else (ADR-0299 §SD2,
// proposed): not another host, not another scheme or port on the same host,
// and nothing at all when the worker passed no base.
func TestNoEgressAllowsOnlyThePagesOrigin(t *testing.T) {
	passed := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK}, nil
	})
	get := func(rt http.RoundTripper, u string) (err error) {
		req, err := http.NewRequest(http.MethodGet, u, nil)
		require.NoError(t, err)
		_, err = rt.RoundTrip(req)
		return
	}
	rt := newNoEgress("https://example.github.io/boxer/demo/", passed)
	require.NoError(t, get(rt, "https://example.github.io/boxer/demo/applets/x.md"))
	require.NoError(t, get(rt, "https://example.github.io/other/path"))
	require.Error(t, get(rt, "https://tile.openstreetmap.org/0/0/0.png"))
	require.Error(t, get(rt, "http://example.github.io/boxer/demo/applets/x.md"))
	require.Error(t, get(rt, "https://example.github.io:8443/boxer/demo/applets/x.md"))

	for _, base := range []string{"", "not a url", "file:///srv/demo/"} {
		require.Error(t, get(newNoEgress(base, passed), "https://example.github.io/x"), base)
	}
}
