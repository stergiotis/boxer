package inprocbus

import (
	"testing"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// TestClient_RemoveCap guards the cap-revocation path the fs Powerbox uses to
// drop a handle's fs.handle.{uuid}.> grant on close: the removed pattern must
// stop matching, unrelated caps must survive, and a repeat removal is a no-op.
func TestClient_RemoveCap(t *testing.T) {
	inst := NewInst(zerolog.Nop())
	c := inst.NewClient("test.app", []app.SubjectFilter{
		{Pattern: "fs.handle.abc.>", Direction: app.CapDirectionBoth, Reason: "granted"},
		{Pattern: "ch.query.boxer", Direction: app.CapDirectionPub, Reason: "unrelated"},
	})

	if !c.canPublish("fs.handle.abc.read") {
		t.Fatal("publish should be allowed before removal")
	}

	if removed := c.RemoveCap("fs.handle.abc.>"); removed != 1 {
		t.Fatalf("RemoveCap returned %d, want 1", removed)
	}
	if c.canPublish("fs.handle.abc.read") {
		t.Fatal("publish should be denied after the handle cap is revoked")
	}
	if c.canSubscribe("fs.handle.abc.event") {
		t.Fatal("subscribe should be denied after the handle cap is revoked")
	}

	if !c.canPublish("ch.query.boxer") {
		t.Fatal("unrelated cap must survive RemoveCap")
	}

	if again := c.RemoveCap("fs.handle.abc.>"); again != 0 {
		t.Fatalf("second RemoveCap returned %d, want 0 (idempotent)", again)
	}
}
