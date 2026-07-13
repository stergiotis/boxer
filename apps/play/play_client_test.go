package play

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	passregdefaults "github.com/stergiotis/boxer/public/keelson/data/passreg/defaults"
)

// emptyArrowStream produces a minimal Arrow IPC byte stream so that the
// ipc.Reader handshake in ExecuteArrowStream succeeds.
func emptyArrowStream(t *testing.T) []byte {
	t.Helper()
	alloc := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{}, nil)
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema), ipc.WithAllocator(alloc))
	rec := array.NewRecord(schema, nil, 0)
	defer rec.Release()
	if err := w.Write(rec); err != nil {
		t.Fatalf("ipc write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("ipc close: %v", err)
	}
	return buf.Bytes()
}

func TestExecuteArrowStreamSendsParamsOnURLWhenPresent(t *testing.T) {
	body := emptyArrowStream(t)

	var (
		gotMethod      string
		gotContentType string
		gotURLParams   url.Values
		gotBody        []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotURLParams = r.URL.Query()
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{URL: srv.URL}, nil)
	rdr, closer, _, err := c.ExecuteArrowStream(
		context.Background(),
		`SET param_a = 1; SET param_b = 'hello world'; SELECT {param_a : UInt64}`,
		memory.NewGoAllocator(),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("ExecuteArrowStream: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })
	t.Cleanup(rdr.Release)

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if !strings.HasPrefix(gotContentType, "text/plain") {
		t.Errorf("content-type = %q, want text/plain", gotContentType)
	}
	if got, want := gotURLParams.Get("param_a"), "1"; got != want {
		t.Errorf("URL param_a = %q, want %q", got, want)
	}
	if got, want := gotURLParams.Get("param_b"), "hello world"; got != want {
		t.Errorf("URL param_b = %q, want %q", got, want)
	}
	bs := string(gotBody)
	for _, want := range []string{"SELECT", "{param_a", "FORMAT ArrowStream"} {
		if !strings.Contains(bs, want) {
			t.Errorf("body missing %q: %q", want, bs)
		}
	}
	if strings.Contains(bs, "SET ") {
		t.Errorf("body still contains harvested SET: %q", bs)
	}
}

func TestExecuteArrowStreamPlainPostWhenNoParams(t *testing.T) {
	body := emptyArrowStream(t)

	var (
		gotContentType string
		gotBody        []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{URL: srv.URL}, nil)
	rdr, closer, _, err := c.ExecuteArrowStream(context.Background(), `SELECT 1`, memory.NewGoAllocator(), nil, nil)
	if err != nil {
		t.Fatalf("ExecuteArrowStream: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })
	t.Cleanup(rdr.Release)

	if !strings.HasPrefix(gotContentType, "text/plain") {
		t.Errorf("content-type = %q, want text/plain", gotContentType)
	}
	if !strings.Contains(string(gotBody), "SELECT 1") {
		t.Errorf("body = %q", gotBody)
	}
	if !strings.Contains(string(gotBody), "FORMAT ArrowStream") {
		t.Errorf("body missing FORMAT clause: %q", gotBody)
	}
}

// ExecOptions ride the URL: a stable query_id plus replace_running_query=1 —
// the server half of SD5 supersession (context cancel alone does not kill a
// read-only ClickHouse query; review finding). nil opts adds neither.
func TestExecuteArrowStreamSendsQueryIDAndReplace(t *testing.T) {
	body := emptyArrowStream(t)
	var gotParams url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotParams = r.URL.Query()
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	c := NewClient(ClientConfig{URL: srv.URL}, nil)

	rdr, closer, _, err := c.ExecuteArrowStream(context.Background(), `SELECT 1`,
		memory.NewGoAllocator(), &ExecOptions{QueryID: "play-test-1", ReplaceRunningQuery: true}, nil)
	if err != nil {
		t.Fatalf("ExecuteArrowStream: %v", err)
	}
	rdr.Release()
	_ = closer.Close()
	if got := gotParams.Get("query_id"); got != "play-test-1" {
		t.Errorf("query_id = %q, want play-test-1", got)
	}
	if got := gotParams.Get("replace_running_query"); got != "1" {
		t.Errorf("replace_running_query = %q, want 1", got)
	}

	rdr, closer, _, err = c.ExecuteArrowStream(context.Background(), `SELECT 1`,
		memory.NewGoAllocator(), nil, nil)
	if err != nil {
		t.Fatalf("ExecuteArrowStream: %v", err)
	}
	rdr.Release()
	_ = closer.Close()
	if gotParams.Has("query_id") || gotParams.Has("replace_running_query") {
		t.Errorf("nil opts must add no query settings, got %v", gotParams)
	}
}

// TestSetURLRoutesToNewTarget: the endpoint switcher (ADR-0094 §SD6) repoints
// the client at runtime, and ExecuteArrowStream reads the live target — the
// request must hit the new endpoint, not the original. Empty is ignored.
func TestSetURLRoutesToNewTarget(t *testing.T) {
	body := emptyArrowStream(t)
	var hitA, hitB bool
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hitA = true
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srvA.Close)
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hitB = true
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srvB.Close)

	c := NewClient(ClientConfig{URL: srvA.URL}, nil)
	if got := c.URL(); got != srvA.URL {
		t.Fatalf("initial URL() = %q, want %q", got, srvA.URL)
	}
	c.SetURL("") // ignored
	if got := c.URL(); got != srvA.URL {
		t.Fatalf("empty SetURL changed target to %q", got)
	}
	c.SetURL(srvB.URL)
	if got := c.URL(); got != srvB.URL {
		t.Fatalf("URL() after SetURL = %q, want %q", got, srvB.URL)
	}

	rdr, closer, _, err := c.ExecuteArrowStream(context.Background(), `SELECT 1`, memory.NewGoAllocator(), nil, nil)
	if err != nil {
		t.Fatalf("ExecuteArrowStream: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })
	t.Cleanup(rdr.Release)

	if hitA {
		t.Error("request hit the old target after SetURL")
	}
	if !hitB {
		t.Error("request did not hit the new target after SetURL")
	}
}

// TestExecuteArrowStreamAppliesPreExecutePasses: a pass registered at
// passreg.StagePreExecute rewrites the shipped statement (ADR-0108 §SD6),
// composing with the FORMAT rewrite. The client's registry is injected so
// the process-global passreg.Default stays untouched.
func TestExecuteArrowStreamAppliesPreExecutePasses(t *testing.T) {
	body := emptyArrowStream(t)

	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{URL: srv.URL}, nil)
	reg := passreg.NewRegistry()
	if err := reg.Register(passreg.Entry{
		Pass: nanopass.LiftBodyPass("TestRewrite", func(sql string) (string, error) {
			return strings.Replace(sql, "SELECT 1", "SELECT 2", 1), nil
		}, nanopass.PassProperties{Reads: nanopass.RegionBody, Writes: nanopass.RegionBody}),
		Stage: passreg.StagePreExecute,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c.passes = reg

	rdr, closer, _, err := c.ExecuteArrowStream(context.Background(), `SELECT 1`, memory.NewGoAllocator(), nil, nil)
	if err != nil {
		t.Fatalf("ExecuteArrowStream: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })
	t.Cleanup(rdr.Release)

	bs := string(gotBody)
	if !strings.Contains(bs, "SELECT 2") {
		t.Errorf("pre-execute pass not applied, body: %q", bs)
	}
	if strings.Contains(bs, "SELECT 1") {
		t.Errorf("original statement leaked to the wire: %q", bs)
	}
	if !strings.Contains(bs, "FORMAT ArrowStream") {
		t.Errorf("FORMAT rewrite lost after pre-execute pass: %q", bs)
	}
}

// TestExecuteArrowStreamFailingPreExecutePassFallsBack: a broken registered
// pass must not block execution — the SQL from before it ships instead.
func TestExecuteArrowStreamFailingPreExecutePassFallsBack(t *testing.T) {
	body := emptyArrowStream(t)

	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{URL: srv.URL}, nil)
	reg := passreg.NewRegistry()
	if err := reg.Register(passreg.Entry{
		Pass: nanopass.LiftBodyPass("Broken", func(string) (string, error) {
			return "", errors.New("boom")
		}, nanopass.PassProperties{Reads: nanopass.RegionBody, Writes: nanopass.RegionBody}),
		Stage: passreg.StagePreExecute,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c.passes = reg

	rdr, closer, _, err := c.ExecuteArrowStream(context.Background(), `SELECT 1`, memory.NewGoAllocator(), nil, nil)
	if err != nil {
		t.Fatalf("ExecuteArrowStream: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })
	t.Cleanup(rdr.Release)

	if !strings.Contains(string(gotBody), "SELECT 1") {
		t.Errorf("fallback SQL missing from body: %q", string(gotBody))
	}
}

// stubColumnResolver rewrites exactly the handle "sec:col" → "phys_col" and
// treats everything else as ordinary SQL. It stands in for the leeway
// system.columns-backed resolver so the wiring can be exercised without a
// server.
type stubColumnResolver struct{}

func (stubColumnResolver) Resolve(dbName, tableName, handle string) passes.ResolveResult {
	if handle == "sec:col" {
		return passes.ResolveResult{Kind: passes.ResolveOK, Physical: []string{"phys_col"}}
	}
	return passes.ResolveResult{Kind: passes.ResolveNotAHandle}
}

// TestExecuteArrowStreamRealisesLateBoundFactory drives the ADR-0108 §SD7
// wiring end to end: with the standard set (which registers ResolveColumnNames
// as a late-bound factory, ADR-0116 §SD6) in the client's registry and a
// ColumnResolverI as the client's binding, ApplyBestEffortBound realises the
// factory and the friendly handle is rewritten on the wire. This is exactly
// the seam installLeewayNameResolution uses — it sets passBinding to the live
// resolver. With the binding cleared, the same factory declines and the handle
// ships verbatim, proving the rewrite came from the binding and not the entry
// set.
func TestExecuteArrowStreamRealisesLateBoundFactory(t *testing.T) {
	body := emptyArrowStream(t)

	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{URL: srv.URL}, nil)
	reg := passreg.NewRegistry()
	if err := passregdefaults.RegisterStandard(reg); err != nil {
		t.Fatalf("RegisterStandard: %v", err)
	}
	c.passes = reg
	c.passBinding = stubColumnResolver{}

	rdr, closer, _, err := c.ExecuteArrowStream(context.Background(), "SELECT `sec:col` FROM t", memory.NewGoAllocator(), nil, nil)
	if err != nil {
		t.Fatalf("ExecuteArrowStream: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })
	t.Cleanup(rdr.Release)

	bs := string(gotBody)
	if !strings.Contains(bs, "phys_col") {
		t.Errorf("late-bound factory not realised; handle not resolved, body: %q", bs)
	}
	if strings.Contains(bs, "sec:col") {
		t.Errorf("friendly handle leaked to the wire: %q", bs)
	}

	gotBody = nil
	c.passBinding = nil
	rdr2, closer2, _, err := c.ExecuteArrowStream(context.Background(), "SELECT `sec:col` FROM t", memory.NewGoAllocator(), nil, nil)
	if err != nil {
		t.Fatalf("ExecuteArrowStream (unbound): %v", err)
	}
	t.Cleanup(func() { _ = closer2.Close() })
	t.Cleanup(rdr2.Release)
	if !strings.Contains(string(gotBody), "sec:col") {
		t.Errorf("with no binding the handle must ship verbatim, body: %q", string(gotBody))
	}
}

// TestExecuteArrowStreamSurfacesClickHouseErrorBody: an invalid query comes
// back as a non-2xx whose body carries ClickHouse's real diagnostic (the "help
// text"). ExecuteArrowStream must fold that body into err.Error() — not bury it
// in a structured field only — because the play UI renders err.Error() in the
// Table tab and status bar. Without this the user sees a bare "clickhouse http
// error" and none of the actual cause.
func TestExecuteArrowStreamSurfacesClickHouseErrorBody(t *testing.T) {
	const chErr = "Code: 47. DB::Exception: Unknown expression identifier 'nope' in scope SELECT nope. (UNKNOWN_IDENTIFIER) (version 24.8.1.1 (official build))"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		// ClickHouse terminates the diagnostic with a newline; the client must
		// still surface it verbatim (trimmed).
		_, _ = io.WriteString(w, chErr+"\n")
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{URL: srv.URL}, nil)
	rdr, closer, _, err := c.ExecuteArrowStream(context.Background(), `SELECT nope`, memory.NewGoAllocator(), nil, nil)
	if rdr != nil {
		t.Cleanup(rdr.Release)
	}
	if closer != nil {
		t.Cleanup(func() { _ = closer.Close() })
	}
	if err == nil {
		t.Fatalf("expected an error for a non-200 response, got nil")
	}
	got := err.Error()
	if !strings.Contains(got, chErr) {
		t.Errorf("error message missing ClickHouse body:\n got: %q\n want substring: %q", got, chErr)
	}
	if !strings.Contains(got, "400") {
		t.Errorf("error message missing status code 400: %q", got)
	}
}

// TestExecuteArrowStreamErrorWithEmptyBody: a non-2xx with no body must still
// produce a legible error (status + a placeholder), never a dangling
// "clickhouse http 500: " with a trailing colon and nothing after it.
func TestExecuteArrowStreamErrorWithEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{URL: srv.URL}, nil)
	_, _, _, err := c.ExecuteArrowStream(context.Background(), `SELECT 1`, memory.NewGoAllocator(), nil, nil)
	if err == nil {
		t.Fatalf("expected an error for a 500 response, got nil")
	}
	got := err.Error()
	if !strings.Contains(got, "500") {
		t.Errorf("error message missing status code 500: %q", got)
	}
	if !strings.Contains(got, "empty response body") {
		t.Errorf("empty-body error should note the empty body: %q", got)
	}
}

// TestExecuteArrowStreamExpandsLwIdMacrosViaStandardSet drives the real
// standard pass set (passreg/defaults) through the client: LW_ID_* macros
// leave expanded, param slots survive the ExtractParams→expand ordering,
// and a wrong-arity macro falls back to verbatim SQL (identsql→play
// wiring, ADR-0106 §SD5 via ADR-0108 §SD6).
func TestExecuteArrowStreamExpandsLwIdMacrosViaStandardSet(t *testing.T) {
	body := emptyArrowStream(t)

	var gotBody []byte
	var gotURLParams url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotURLParams = r.URL.Query()
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{URL: srv.URL}, nil)
	reg := passreg.NewRegistry()
	if err := passregdefaults.RegisterStandard(reg); err != nil {
		t.Fatalf("RegisterStandard: %v", err)
	}
	c.passes = reg

	run := func(sql string) string {
		t.Helper()
		rdr, closer, _, err := c.ExecuteArrowStream(context.Background(), sql, memory.NewGoAllocator(), nil, nil)
		if err != nil {
			t.Fatalf("ExecuteArrowStream(%q): %v", sql, err)
		}
		t.Cleanup(func() { _ = closer.Close() })
		t.Cleanup(rdr.Release)
		return string(gotBody)
	}

	// (1) plain macro call: expanded, no LW_ID_ on the wire, still parseable.
	bs := run(`SELECT LW_ID_BODY(id) FROM t`)
	if strings.Contains(bs, "LW_ID_") {
		t.Errorf("macro not expanded on the wire: %q", bs)
	}
	if _, err := nanopass.Parse(bs); err != nil {
		t.Errorf("expanded wire SQL does not parse: %v (%q)", err, bs)
	}

	// (2) param slot survives: ExtractParams runs first, expansion sees the
	// slot, and the value still rides the URL.
	bs = run(`SET param_id = 7; SELECT LW_ID_BODY({id:UInt64})`)
	if strings.Contains(bs, "LW_ID_") {
		t.Errorf("macro around a param slot not expanded: %q", bs)
	}
	if !strings.Contains(bs, "{id") {
		t.Errorf("param slot lost during expansion: %q", bs)
	}
	if got, want := gotURLParams.Get("param_id"), "7"; got != want {
		t.Errorf("URL param_id = %q, want %q", got, want)
	}

	// (3) wrong arity is an ExpandPass error: best-effort falls back to the
	// unexpanded SQL so the server reports the real problem to the user.
	bs = run(`SELECT LW_ID_BODY(a, b) FROM t`)
	if !strings.Contains(bs, "LW_ID_BODY(a, b)") {
		t.Errorf("wrong-arity macro must ship verbatim, got: %q", bs)
	}
}
