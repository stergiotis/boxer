package clipboardbroker_test

import (
	"errors"
	"testing"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// writerCaps is the minimal cap set a consumer needs to issue a copy: Pub on
// clipboard.write. The reply lands on an ephemeral _INBOX.* that bypasses cap
// checks, so no Sub cap is required — matching every fs.dialog.* consumer.
func writerCaps() []app.SubjectFilter {
	return []app.SubjectFilter{
		{Pattern: clipboardbroker.SubjectWrite, Direction: app.CapDirectionPub, Reason: "test: copy"},
	}
}

func newServedBus(t *testing.T) (bus *inprocbus.Inst, svc *clipboardbroker.Service) {
	t.Helper()
	bus = inprocbus.NewInst(zerolog.Nop())
	svc, err := clipboardbroker.NewService(bus, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	t.Cleanup(svc.Close)
	return bus, svc
}

// TestWriteRequestEnqueuesAndAcks exercises the cold half end to end: a capable
// app's Request returns (proving the broker acked) and the text lands in the
// drain queue exactly once.
func TestWriteRequestEnqueuesAndAcks(t *testing.T) {
	bus, svc := newServedBus(t)
	cli := bus.NewClient("test.app", writerCaps())

	reply, err := cli.Request(clipboardbroker.SubjectWrite, []byte("hello clipboard"))
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if len(reply) != 0 {
		t.Fatalf("ack should be empty, got %q", reply)
	}

	got := svc.DrainPending()
	if len(got) != 1 || got[0] != "hello clipboard" {
		t.Fatalf("DrainPending = %#v, want [\"hello clipboard\"]", got)
	}

	// A second drain with nothing enqueued is empty — the queue resets.
	if again := svc.DrainPending(); again != nil {
		t.Fatalf("second DrainPending = %#v, want nil", again)
	}
}

// TestDrainPreservesArrivalOrder pins that the queue is FIFO so the host emits
// copy opcodes in the order the user clicked (last one wins on the clipboard
// within a frame, but order is the contract).
func TestDrainPreservesArrivalOrder(t *testing.T) {
	bus, svc := newServedBus(t)
	cli := bus.NewClient("test.app", writerCaps())

	for _, s := range []string{"one", "two", "three"} {
		if _, err := cli.Request(clipboardbroker.SubjectWrite, []byte(s)); err != nil {
			t.Fatalf("Request(%q): %v", s, err)
		}
	}

	got := svc.DrainPending()
	want := []string{"one", "two", "three"}
	if len(got) != len(want) {
		t.Fatalf("DrainPending len = %d, want %d (%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("DrainPending[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestWriteWithoutCapDenied pins the hygiene contract: an app that did not
// declare clipboard.write is refused at the bus, and nothing is enqueued. This
// is the audited-denial path (§SD5 / Update 2026-05-30).
func TestWriteWithoutCapDenied(t *testing.T) {
	bus, svc := newServedBus(t)
	cli := bus.NewClient("uncapped.app", nil)

	_, err := cli.Request(clipboardbroker.SubjectWrite, []byte("nope"))
	if !errors.Is(err, inprocbus.ErrPermissionViolation) {
		t.Fatalf("Request err = %v, want ErrPermissionViolation", err)
	}
	if got := svc.DrainPending(); got != nil {
		t.Fatalf("DrainPending = %#v, want nil (denied copy must not enqueue)", got)
	}
}

// TestNilBusRejected guards the constructor's nil check.
func TestNilBusRejected(t *testing.T) {
	if _, err := clipboardbroker.NewService(nil, zerolog.Nop()); err == nil {
		t.Fatal("NewService(nil) should error")
	}
}
