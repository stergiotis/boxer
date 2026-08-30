package pprofhttp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The mux is hand-registered rather than inherited from
// http.DefaultServeMux, so nothing but this test says the paths a profiling
// client fetches are actually served. It also pins the negative half of the
// decision: a path registered on the default mux by some other package must
// not appear here.
func TestServeMuxServesPprofPathsAndNothingElse(t *testing.T) {
	http.HandleFunc("/not-pprof", func(w http.ResponseWriter, r *http.Request) {})

	srv := httptest.NewServer(NewServeMux())
	defer srv.Close()

	for _, tc := range []struct {
		path string
		want string
	}{
		{"/debug/pprof/", "Types of profiles available"},
		{"/debug/pprof/cmdline", ""},
		{"/debug/pprof/goroutine?debug=1", "goroutine profile"},
		{"/debug/pprof/symbol", "num_symbols: 1"},
	} {
		resp, err := srv.Client().Get(srv.URL + tc.path)
		if err != nil {
			t.Fatalf("get %s: %v", tc.path, err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatalf("read %s: %v", tc.path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d, want 200", tc.path, resp.StatusCode)
		}
		if tc.want != "" && !strings.Contains(string(body), tc.want) {
			t.Errorf("%s: body does not contain %q", tc.path, tc.want)
		}
	}

	resp, err := srv.Client().Get(srv.URL + "/not-pprof")
	if err != nil {
		t.Fatalf("get /not-pprof: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("/not-pprof: status %d, want 404 — the listener must not serve the default mux", resp.StatusCode)
	}
}
