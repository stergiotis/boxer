package browserhost

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// startServe runs Serve on a temporary bundle and returns its base URL, the
// ClickHouse stand-in's request log, and a channel that closes when Serve
// returns.
func startServe(t *testing.T, exitOnReport bool) (base string, chSeen *[]string, done <-chan error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><title>tab</title>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "m.wasm"), []byte{0, 'a', 's', 'm'}, 0o644); err != nil {
		t.Fatal(err)
	}
	seen := []string{}
	ch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.RequestURI()+" host="+r.Host+" body="+string(b))
		_, _ = io.WriteString(w, "2\n")
	}))
	t.Cleanup(ch.Close)
	addrCh := make(chan net.Addr, 1)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	go func() {
		errCh <- Serve(ctx, ServeConfig{
			Dir:          dir,
			Listen:       "127.0.0.1:0",
			ChURL:        ch.URL + "/",
			ExitOnReport: exitOnReport,
			OnListen:     func(a net.Addr) { addrCh <- a },
		}, zerolog.Nop())
	}()
	select {
	case a := <-addrCh:
		base = "http://" + a.String()
	case err := <-errCh:
		t.Fatalf("serve returned before listening: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not listen")
	}
	return base, &seen, errCh
}

func get(t *testing.T, url string) (status int, ctype string, body string) {
	t.Helper()
	resp, err := http.Get(url) //nolint:gosec // test URL
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(b)
}

func TestServeFilesAndTypes(t *testing.T) {
	base, _, _ := startServe(t, false)
	status, ctype, body := get(t, base+"/index.html?worker=x")
	if status != http.StatusOK || !strings.HasPrefix(ctype, "text/html") || !strings.Contains(body, "tab") {
		t.Fatalf("index: %d %q %q", status, ctype, body)
	}
	status, ctype, _ = get(t, base+"/m.wasm")
	if status != http.StatusOK || ctype != "application/wasm" {
		t.Fatalf("wasm: %d %q", status, ctype)
	}
	status, _, _ = get(t, base+"/missing.mjs")
	if status != http.StatusNotFound {
		t.Fatalf("missing: %d", status)
	}
}

func TestServeProxiesClickHouseSameOrigin(t *testing.T) {
	base, seen, _ := startServe(t, false)
	resp, err := http.Post(base+"/ch/?query=SELECT+1%2B1", "text/plain", strings.NewReader("")) //nolint:gosec // test URL
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(b) != "2\n" {
		t.Fatalf("proxy: %d %q", resp.StatusCode, b)
	}
	if len(*seen) != 1 || !strings.HasPrefix((*seen)[0], "POST /?query=SELECT+1%2B1 host=127.0.0.1") {
		t.Fatalf("upstream saw %v", *seen)
	}
	status, _, _ := get(t, base+"/ch/ping")
	if status != http.StatusOK || !strings.HasPrefix((*seen)[1], "GET /ping ") {
		t.Fatalf("path: %d %v", status, *seen)
	}
}

func TestServeReportEndsWhenAsked(t *testing.T) {
	base, _, done := startServe(t, true)
	resp, err := http.Post(base+"/log", "text/plain", strings.NewReader("a line")) //nolint:gosec // test URL
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("log: %v %v", err, resp)
	}
	_ = resp.Body.Close()
	resp, err = http.Post(base+"/report", "text/plain", strings.NewReader("ARM {}")) //nolint:gosec // test URL
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("report: %v %v", err, resp)
	}
	_ = resp.Body.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not end after the report")
	}
}
