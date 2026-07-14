package fsbroker

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/dialogreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/watchevent"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/watchreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/watchrequest"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// MarshalDialogReply serialises a DialogReply via the canonical bus
// codec. The codec wire form (dialogreply.DialogReply) lives in
// keelson/runtime/codec/dialogreply; this helper handles the
// translation so callers keep using the broker's native shape.
func MarshalDialogReply(r DialogReply) (b []byte, err error) {
	wire := dialogreply.DialogReply{
		Approved:            r.Granted,
		HandleSubjectPrefix: r.HandleSubjectPrefix,
		Reason:              r.Reason,
	}
	b, err = buscodec.Encode(wire)
	if err != nil {
		err = eh.Errorf("fsbroker: marshal dialog reply: %w", err)
	}
	return
}

// UnmarshalDialogReply is the inverse of MarshalDialogReply.
func UnmarshalDialogReply(b []byte) (r DialogReply, err error) {
	var wire dialogreply.DialogReply
	wire, err = buscodec.Decode[dialogreply.DialogReply](b)
	if err != nil {
		err = eh.Errorf("fsbroker: unmarshal dialog reply: %w", err)
		return
	}
	r = DialogReply{
		Granted:             wire.Approved,
		HandleSubjectPrefix: wire.HandleSubjectPrefix,
		Reason:              wire.Reason,
	}
	return
}

// DialogRequest is the OPTIONAL payload an app may publish on an
// fs.dialog.{op} open to carry advisory hints for the picker. Today it holds
// only a suggested filename for the "Save as" dialog (fs.dialog.write). A nil
// or empty payload means "no hints", so callers that publish nil to open a
// dialog stay wire-compatible.
//
// Unlike the DialogReply / Watch* payloads — leeway-coded per-type codecs,
// shaped for replay into the runtime.facts store — this ephemeral control-plane
// hint rides the buscodec DEFAULT (canonical CBOR) path. buscodec routes
// unregistered types there by design (ADR-0036; the leeway migration is
// "opt-in per payload type"), so no codec/dialogrequest package or vdd
// vocabulary term is added for a value that never enters the facts store. Field
// keys therefore come from the `cbor:` tag, not a leeway membership.
type DialogRequest struct {
	// SuggestedName is the filename the picker pre-fills in ModeSave ("Save
	// as"); non-save dialogs ignore it. Advisory only — the user may overwrite
	// it, and the broker NEVER derives the write path from it (the resolved
	// path comes from the picker selection). A basename is expected; the
	// picker reduces any directory component away.
	SuggestedName string `cbor:"suggestedName"`
}

// MarshalDialogRequest serialises a DialogRequest via the canonical bus codec
// (the default CBOR path — DialogRequest registers no per-type codec).
func MarshalDialogRequest(r DialogRequest) (b []byte, err error) {
	b, err = buscodec.Encode(r)
	if err != nil {
		err = eh.Errorf("fsbroker: marshal dialog request: %w", err)
	}
	return
}

// UnmarshalDialogRequest is the inverse of MarshalDialogRequest. An empty byte
// slice yields a zero DialogRequest without error, so a nil-payload dialog open
// (no hints) stays wire-compatible — mirrors UnmarshalWatchRequest.
func UnmarshalDialogRequest(b []byte) (r DialogRequest, err error) {
	if len(b) == 0 {
		return
	}
	r, err = buscodec.Decode[DialogRequest](b)
	if err != nil {
		err = eh.Errorf("fsbroker: unmarshal dialog request: %w", err)
	}
	return
}

// MarshalWatchRequest serialises a WatchRequest. Empty payload (zero-
// length b) is also a valid watch request, so callers may publish nil
// when defaults suffice; UnmarshalWatchRequest tolerates that on the
// receiver side.
func MarshalWatchRequest(r WatchRequest) (b []byte, err error) {
	wire := watchrequest.WatchRequest{
		PollFallback:   r.PollFallback,
		PollIntervalMs: r.PollIntervalMs,
		Recursive:      r.Recursive,
	}
	b, err = buscodec.Encode(wire)
	if err != nil {
		err = eh.Errorf("fsbroker: marshal watch request: %w", err)
	}
	return
}

// UnmarshalWatchRequest is the inverse of MarshalWatchRequest. An empty
// byte slice yields a zero WatchRequest without error — matches the
// existing wire contract.
func UnmarshalWatchRequest(b []byte) (r WatchRequest, err error) {
	if len(b) == 0 {
		return
	}
	var wire watchrequest.WatchRequest
	wire, err = buscodec.Decode[watchrequest.WatchRequest](b)
	if err != nil {
		err = eh.Errorf("fsbroker: unmarshal watch request: %w", err)
		return
	}
	r = WatchRequest{
		PollFallback:   wire.PollFallback,
		PollIntervalMs: wire.PollIntervalMs,
		Recursive:      wire.Recursive,
	}
	return
}

// MarshalWatchReply serialises a WatchReply.
func MarshalWatchReply(r WatchReply) (b []byte, err error) {
	wire := watchreply.WatchReply{
		Started:      r.Started,
		EventSubject: r.EventSubject,
		Backend:      r.Backend,
		Reason:       r.Reason,
	}
	b, err = buscodec.Encode(wire)
	if err != nil {
		err = eh.Errorf("fsbroker: marshal watch reply: %w", err)
	}
	return
}

// UnmarshalWatchReply is the inverse of MarshalWatchReply.
func UnmarshalWatchReply(b []byte) (r WatchReply, err error) {
	var wire watchreply.WatchReply
	wire, err = buscodec.Decode[watchreply.WatchReply](b)
	if err != nil {
		err = eh.Errorf("fsbroker: unmarshal watch reply: %w", err)
		return
	}
	r = WatchReply{
		Started:      wire.Started,
		EventSubject: wire.EventSubject,
		Backend:      wire.Backend,
		Reason:       wire.Reason,
	}
	return
}

// MarshalWatchEvent serialises a WatchEvent for publication on
// fs.handle.{uuid}.event.
func MarshalWatchEvent(e WatchEvent) (b []byte, err error) {
	wire := watchevent.WatchEvent{
		At:     time.Unix(0, e.Ts).UTC(),
		Kind:   e.Kind.String(),
		Name:   e.Name,
		Cookie: e.Cookie,
	}
	b, err = buscodec.Encode(wire)
	if err != nil {
		err = eh.Errorf("fsbroker: marshal watch event: %w", err)
	}
	return
}

// UnmarshalWatchEvent is the inverse of MarshalWatchEvent.
func UnmarshalWatchEvent(b []byte) (e WatchEvent, err error) {
	var wire watchevent.WatchEvent
	wire, err = buscodec.Decode[watchevent.WatchEvent](b)
	if err != nil {
		err = eh.Errorf("fsbroker: unmarshal watch event: %w", err)
		return
	}
	e = WatchEvent{
		Kind:   ParseWatchEventKind(wire.Kind),
		Name:   wire.Name,
		Cookie: wire.Cookie,
		Ts:     wire.At.UnixNano(),
	}
	return
}
