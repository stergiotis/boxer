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

// ProbeSeq derives a per-instance register slot — an r21 probe seq, an r9
// measure id — from the CURRENT id scope and a role, for a widget that owns a
// scope (ADR-0267 W7). Call it inside the widget's root IdScope: the slot is
// then unique per host scope, so two instances whose hosts chose the same
// scope key under different parents cannot share one, which the stack-free
// [ProbeSeq] cannot guarantee. Salted per role so one instance can hold
// several slots, and with a salt of its own so it cannot collide with a
// child widget id prepared from the same string under the same scope.
//
// Side-effect free: the prepare/derive state is untouched, and the call
// allocates nothing. Like every seq, the slot is shared between
// [CapturePaneSize] and [CaptureUiRect], so one seq means one kind of rect.
func (inst *WidgetIdStack) ProbeSeq(role string) (seq uint64) {
	return ensureNotZeroId(inst.peekIdFromStack() ^ makeHighEntropy(hashLabelToId(role)^probeRoleSalt))
}

// probeRoleSalt separates a probe slot from a child widget id prepared from
// the same role string under the same scope.
const probeRoleSalt uint64 = 0x9e3779b97f4a7c15
