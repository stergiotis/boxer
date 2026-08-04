package bindings

import "hash/fnv"

// Layout probes answer "how much room do I have here?" for a caller that
// intends to size something to its pane.
//
// Use these, not [CaptureAvailableSize]. That op writes r18, a SINGLE
// process-wide scalar that the frame's LAST capture wins, so two panels reading
// it size each other — a bug that presents as one pane inexplicably tracking
// another's width, and that no amount of care at one call site can prevent.
// The seq-keyed form below has one slot per caller and cannot be taken over.
//
// One-frame lag, like every capture/fetch pair: a capture this frame is
// readable on the next.

// CapturePaneSize arms this Ui's available-rect probe under `seq` and returns
// what the same seq reported LAST frame. ok is false until a capture has
// landed — on the first frame, and again on the frame a hidden tab comes back,
// since a seq that did not capture is absent from the drain rather than zero.
// Callers that would flash at a fallback size should hold the last good answer
// across frames.
//
// Call it BEFORE placing the content it is meant to size: the rect is the space
// left for the NEXT widget, so a probe emitted afterwards reports what remains
// AFTER that content — which is how a widget ends up sizing itself against its
// own output, and ratcheting.
//
// `seq` must be stable across frames and unique to the caller. Derive it from
// whatever already identifies the instance — [ProbeSeq] over a scope key, or a
// package salt through the instance's own id stack — and note that the slot is
// shared with [CaptureUiRect], so one seq means one kind of rect.
func CapturePaneSize(seq uint64) (w, h float32, ok bool) {
	CaptureUiAvailableRect(seq)
	r, found := CurrentApplicationState.StateManager.GetUiRect(seq)
	if !found {
		return 0, 0, false
	}
	return r.MaxX - r.MinX, r.MaxY - r.MinY, true
}

// ProbeSeq derives a stable per-instance register slot — an r21 probe seq, an
// r9 measure id — from a widget's scope key and a role, for the common case of
// an instance identified by a string. Salted per role so one instance can hold
// several slots, and per package so it cannot collide with a caller hashing the
// same scope key for its own purposes.
func ProbeSeq(scopeKey, role string) (seq uint64) {
	h := fnv.New64a()
	_, _ = h.Write([]byte("egui2-probe#"))
	_, _ = h.Write([]byte(role))
	_, _ = h.Write([]byte("#"))
	_, _ = h.Write([]byte(scopeKey))
	return h.Sum64()
}
