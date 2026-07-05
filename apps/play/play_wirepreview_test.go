package play

import (
	"strings"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	passregdefaults "github.com/stergiotis/boxer/public/keelson/data/passreg/defaults"
)

// newTestClientWithStandardSet builds a Client whose pass registry carries
// the real standard set (passreg/defaults), injected so the process-global
// passreg.Default stays untouched.
func newTestClientWithStandardSet(t *testing.T) *Client {
	t.Helper()
	cl := NewClient(ClientConfig{URL: "http://127.0.0.1:1"}, nil)
	reg := passreg.NewRegistry()
	if err := passregdefaults.RegisterStandard(reg); err != nil {
		t.Fatalf("RegisterStandard: %v", err)
	}
	cl.passes = reg
	return cl
}

// TestBuildStatementShipsWireForm pins the shared builder the execution
// path and the Preview tab's "as sent" view both use: params harvested to
// the URL channel, pre-execute passes applied, FORMAT rewritten.
func TestBuildStatementShipsWireForm(t *testing.T) {
	cl := newTestClientWithStandardSet(t)
	body, params := cl.BuildStatement(`SET param_id = 7; SELECT LW_ID_BODY({id:UInt64})`)
	if got, want := params["param_id"], "7"; got != want {
		t.Errorf("param_id = %q, want %q", got, want)
	}
	if strings.Contains(body, "LW_ID_") {
		t.Errorf("macro not expanded: %q", body)
	}
	if !strings.Contains(body, "FORMAT ArrowStream") {
		t.Errorf("FORMAT rewrite missing: %q", body)
	}
	if strings.Contains(body, "SET param_id") {
		t.Errorf("harvested SET leaked into body: %q", body)
	}
}

func TestUpdateWirePreviewComputesAndDebounces(t *testing.T) {
	cl := newTestClientWithStandardSet(t)
	app := &PlayApp{client: cl, previewAsSent: true, sql: `SELECT LW_ID_IS_VALID(id) FROM t`}

	// Zero lastEditAt → debounce window long elapsed → computes.
	app.updateWirePreview()
	if app.wireBody == "" || strings.Contains(app.wireBody, "LW_ID_") {
		t.Fatalf("wire preview not computed/expanded: %q", app.wireBody)
	}
	if !strings.Contains(app.wireBody, "FORMAT ArrowStream") {
		t.Errorf("wire preview missing FORMAT: %q", app.wireBody)
	}

	// A fresh edit inside the debounce window must NOT recompute.
	prev := app.wireBody
	app.sql = `SELECT 1`
	app.lastEditAt = time.Now()
	app.updateWirePreview()
	if app.wireBody != prev {
		t.Error("recomputed within the debounce window")
	}

	// Once the window elapses, the new buffer lands.
	app.lastEditAt = time.Now().Add(-2 * previewDebounce)
	app.updateWirePreview()
	if !strings.Contains(app.wireBody, "SELECT 1") {
		t.Errorf("wire preview stale after debounce: %q", app.wireBody)
	}
}

func TestUpdateWirePreviewGuards(t *testing.T) {
	cl := newTestClientWithStandardSet(t)

	// Toggle off → never computes (BuildStatement re-parses; hidden view
	// must cost nothing).
	off := &PlayApp{client: cl, sql: `SELECT 1`}
	off.updateWirePreview()
	if off.wireBody != "" {
		t.Errorf("computed while toggle off: %q", off.wireBody)
	}

	// Nil client (tests, legacy CLI) → no panic, no compute.
	noClient := &PlayApp{previewAsSent: true, sql: `SELECT 1`}
	noClient.updateWirePreview()
	if noClient.wireBody != "" {
		t.Errorf("computed without a client: %q", noClient.wireBody)
	}

	// Blank buffer while on → empty view state, params cleared.
	blank := &PlayApp{client: cl, previewAsSent: true, sql: "   "}
	blank.wireParams = map[string]string{"stale": "1"}
	blank.updateWirePreview()
	if blank.wireBody != "" || blank.wireParams != nil {
		t.Errorf("blank buffer must clear the wire view, got body=%q params=%v", blank.wireBody, blank.wireParams)
	}
}
