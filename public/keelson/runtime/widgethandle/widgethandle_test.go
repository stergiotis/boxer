package widgethandle

import (
	"testing"
)

func TestRoundTrip(t *testing.T) {
	ids := []uint64{0, 1, 42, 0xdeadbeef, 0xffffffffffffffff}
	for _, id := range ids {
		h := Make(id)
		got := h.Resolve()
		if got != id {
			t.Errorf("Make(%d).Resolve() = %d, want %d", id, got, id)
		}
	}
}

func TestDifferentIdsProduceDifferentHandles(t *testing.T) {
	h1 := Make(100)
	h2 := Make(200)
	if h1 == h2 {
		t.Error("different IDs produced the same handle")
	}
}

func TestHandleIsNotRawId(t *testing.T) {
	// Unless the secret happens to be 0 (astronomically unlikely), the
	// handle's numeric value should differ from the raw ID.
	id := uint64(12345)
	h := Make(id)
	if uint64(h) == id {
		t.Error("handle value equals raw ID; XOR obfuscation may not be working")
	}
}

func TestMakeZeroIsNotNoWidget(t *testing.T) {
	// Make(0) produces a handle that is secret^0 = secret, which is
	// (almost certainly) not the zero value.
	h := Make(0)
	if h == NoWidget {
		t.Error("Make(0) should not equal NoWidget unless secret is 0")
	}
}

func TestNoWidgetIsZero(t *testing.T) {
	if !NoWidget.IsZero() {
		t.Error("NoWidget.IsZero() should be true")
	}
}

func TestIsZero(t *testing.T) {
	h := Make(999)
	if h.IsZero() {
		t.Error("non-zero handle should not report IsZero")
	}
}

func TestSecretChangesAcrossRuns(t *testing.T) {
	// We cannot truly test cross-process secret rotation in a unit test,
	// but we can verify the secret is non-zero (which would defeat the
	// obfuscation).
	if secret == 0 {
		t.Error("secret should not be 0")
	}
}
