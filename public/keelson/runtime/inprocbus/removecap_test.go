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

// TestNewClient_OwnsItsCaps guards two windows of one app, which are handed
// the same manifest slice: a grant added to one, or revoked from it, must
// leave the other's list and the manifest's as they were. The slice has
// room to grow, as a manifest built by appending has; without a copy the
// second grant is written where the first lies.
func TestNewClient_OwnsItsCaps(t *testing.T) {
	inst := NewInst(zerolog.Nop())
	manifest := make([]app.SubjectFilter, 0, 8)
	manifest = append(manifest,
		app.SubjectFilter{Pattern: "fs.dialog.read", Direction: app.CapDirectionPub, Reason: "the picker"},
		app.SubjectFilter{Pattern: "ch.query.boxer", Direction: app.CapDirectionPub, Reason: "unrelated"},
	)
	a := inst.NewClient("test.app", manifest)
	b := inst.NewClient("test.app", manifest)

	a.AddCap(app.SubjectFilter{Pattern: "fs.handle.aaa.>", Direction: app.CapDirectionBoth})
	b.AddCap(app.SubjectFilter{Pattern: "fs.handle.bbb.>", Direction: app.CapDirectionBoth})
	if !a.canPublish("fs.handle.aaa.read") {
		t.Fatal("the first window lost its grant to the second's")
	}
	if a.canPublish("fs.handle.bbb.read") || b.canPublish("fs.handle.aaa.read") {
		t.Fatal("a window holds the other's grant")
	}

	if removed := a.RemoveCap("fs.dialog.read"); removed != 1 {
		t.Fatalf("RemoveCap returned %d, want 1", removed)
	}
	if !b.canPublish("fs.dialog.read") {
		t.Fatal("revoking from one window revoked from the other")
	}
	if manifest[0].Pattern != "fs.dialog.read" || manifest[1].Pattern != "ch.query.boxer" || len(manifest) != 2 {
		t.Fatalf("the caller's slice was changed: %+v", manifest)
	}
	if spare := manifest[:3][2]; spare.Pattern != "" {
		t.Fatalf("a grant was written into the caller's slice: %+v", spare)
	}
}
