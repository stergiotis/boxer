package play

import "github.com/stergiotis/boxer/public/observability/eh"

// Ad-hoc dataset delivery ops (ADR-0134 §SD4/§SD5). An embedder that hosts
// a play instance drives it to bind ephemeral dataset handles to stable
// aliases, push signal values, and re-run under Live. Like the other
// delivery ops (play_delivery.go) these run on the render thread: they
// write instance state read by the next frame.

// SetSignal publishes value under signal name through the same emit path
// panels use (play_graph.go): last-write-wins within a frame, visible from
// the next frame's snapshot. Supported value kinds are the signal-store
// encodable set (string, integers, float64, bool); others are dropped with
// a warning by the emitter.
func (inst *PlayApp) SetSignal(name string, value any) {
	inst.sigEmit.Emit(SignalID(name), value)
}

// RequestRun schedules a run of the main buffer after the current frame —
// the same one-shot the Run button and the Live auto-run path use.
func (inst *PlayApp) RequestRun() {
	inst.requestRun = true
}

// BindDataset binds a stable alias — as written in the buffer,
// keelson('<alias>') — to an ephemeral dataset handle, so the client
// rewrites the alias to the handle before the request leaves play
// (ADR-0134 §SD4). Both must be bare identifiers. The binding is instance
// state, applied by the embedder between construction and mount.
func (inst *PlayApp) BindDataset(alias, handle string) error {
	if !validDatasetIdentifier(alias) {
		return eh.Errorf("play: invalid dataset alias %q", alias)
	}
	if !validDatasetIdentifier(handle) {
		return eh.Errorf("play: invalid dataset handle %q", handle)
	}
	if inst.client == nil {
		return eh.Errorf("play: BindDataset needs a client")
	}
	inst.client.bindDataset(alias, handle)
	return nil
}

// NotifyDatasetRevision signals that a bound dataset was republished at a
// new revision. Under Live main it triggers the ordinary auto-run path so
// the applet re-queries the fresh data; an explicit Run always works
// regardless. This is the instance-level realization of ADR-0134 §SD5
// freshness (a trigger, not a graph signal). The alias identifies which
// dataset changed and is advisory today — the trigger re-runs the whole
// main buffer.
func (inst *PlayApp) NotifyDatasetRevision(alias string, revision uint64) {
	if inst.liveMain {
		inst.requestRun = true
	}
}

// validDatasetIdentifier reports whether s is a bare identifier
// ([A-Za-z_][A-Za-z0-9_]*, ≤64 bytes) — the same shape the handle and
// alias must satisfy on the capability side.
func validDatasetIdentifier(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		ok := ch == '_' ||
			(ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') ||
			(i > 0 && ch >= '0' && ch <= '9')
		if !ok {
			return false
		}
	}
	return true
}
