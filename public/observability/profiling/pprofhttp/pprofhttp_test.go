package pprofhttp

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cli "github.com/urfave/cli/v3"
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

// A failed bind is logged and the command runs on: an address another
// process already holds must not stop the host that asked for it second.
func TestListenAddressFailedBindDoesNotEndTheCommand(t *testing.T) {
	run := func(args ...string) error {
		cmd := &cli.Command{Name: "x", Flags: Flags, Action: func(context.Context, *cli.Command) error { return nil }}
		return cmd.Run(t.Context(), append([]string{"x"}, args...))
	}
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("hold a port: %v", err)
	}
	defer held.Close()
	if err = run("--pprofHttpListenAddress", held.Addr().String()); err != nil {
		t.Fatalf("address in use: want the command to run on, got %v", err)
	}
	if err = run("--pprofHttpListenAddress", "no-port"); err != nil {
		t.Fatalf("malformed address: want the command to run on, got %v", err)
	}
	if err = run("--pprofHttpListenAddress", "127.0.0.1:0"); err != nil {
		t.Fatalf("loopback bind: %v", err)
	}
}
