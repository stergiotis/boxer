// Package clipboardbroker is the runtime's clipboard Powerbox per ADR-0026
// (Update 2026-05-30). It is the first capability whose mechanism lives on the
// egui draw path rather than in Go: the only way to reach the viewport
// clipboard is egui's Context::copy_text, which runs on the frame thread, off
// the bus. The broker therefore splits the copy into a cold half and a hot
// half.
//
// Cold half (this package): an app publishes a request to clipboard.write with
// the raw UTF-8 bytes to copy as the payload. The broker enqueues the text and
// replies with an empty ack. The copy is a request/reply — not a fire-and-
// forget publish — specifically so the bus AuditSink records it: the sink fires
// in inprocbus Client.Request, never in Publish (matching NATS core, where a
// bare publish has no acked lifecycle). A Publish-based copy would be invisible
// to the audit trail, defeating the reason the clipboard is a capability.
//
// Hot half (the host frame loop): once per frame the host calls DrainPending
// and emits a CopyTextToClipboard egui opcode for each drained string inside
// the active Ui scope. See windowhost for the bridge; it mirrors how
// fsbroker/pickerbridge couples the off-frame dialog queue to the on-frame
// file picker.
//
// Write-only at v1. Read/paste (egui delivers paste as an event, not a
// synchronous pull) is a separate follow-up.
package clipboardbroker

import (
	"sync"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// SubjectWrite is the request subject an app publishes to in order to copy text
// to the viewport clipboard (ADR-0026 §SD3, Update 2026-05-30). The request
// payload is the raw UTF-8 text; the reply is an empty ack.
const SubjectWrite = "clipboard.write"

// ServiceAppId is the synthetic AppId the broker registers under, mirroring
// fsbroker.ServiceAppId ("runtime.fs").
const ServiceAppId app.AppIdT = "runtime.clipboard"

// maxPending bounds the off-frame queue so a pathological copy loop with no
// host draining it (e.g. a headless run where no windowhost exists) cannot grow
// memory without limit. On overflow the oldest entries are dropped — the most
// recent copy is the one the user wants on the clipboard, and within a single
// frame the host emits opcodes in order so the last drained string wins anyway.
// Drops are logged, never silent (ADR-0026 audit posture).
const maxPending = 256

// Service subscribes to clipboard.write, accumulates requested text off-frame,
// and acks each request so the requester's Request returns and is audited. The
// host frame loop drains the accumulated text via DrainPending and emits the
// egui copy opcode for each entry.
type Service struct {
	inst      *inprocbus.Inst
	log       zerolog.Logger
	busClient *inprocbus.Client
	unsub     func()

	mu      sync.Mutex
	pending []string
}

// NewService constructs and subscribes the service. The returned Service holds
// a bus client scoped to ServiceAppId with just the two caps it needs: receive
// on clipboard.write and reply on inboxes.
func NewService(inst *inprocbus.Inst, log zerolog.Logger) (s *Service, err error) {
	if inst == nil {
		err = eh.Errorf("clipboardbroker: nil inst")
		return
	}
	s = &Service{
		inst: inst,
		log:  log.With().Str("app", string(ServiceAppId)).Logger(),
	}
	s.busClient = inst.NewClient(ServiceAppId, []app.SubjectFilter{
		{Pattern: SubjectWrite, Direction: app.CapDirectionBoth, Reason: "clipboard Powerbox serves clipboard.write"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "clipboard acks replies to inboxes"},
	})
	s.unsub, err = s.busClient.Subscribe(SubjectWrite, s.handleWrite)
	if err != nil {
		err = eh.Errorf("clipboardbroker: subscribe: %w", err)
		return
	}
	return
}

// Close releases the bus subscription. Idempotent.
func (inst *Service) Close() {
	if inst.unsub != nil {
		inst.unsub()
		inst.unsub = nil
	}
}

// DrainPending returns and clears the accumulated copy requests in arrival
// order, then resets the queue. The host frame loop calls this once per frame;
// each returned string becomes one CopyTextToClipboard egui opcode emitted
// inside the active Ui scope. Returns nil when nothing is pending.
func (inst *Service) DrainPending() (texts []string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if len(inst.pending) == 0 {
		return
	}
	texts = inst.pending
	inst.pending = nil
	return
}

// handleWrite enqueues one copy request and acks it. Synchronous-dispatch
// safe: inprocbus calls this on the requester's goroutine after releasing the
// bus lock, and the requester's inbox channel is buffered (cap 1), so acking
// inline cannot deadlock. The service mutex is released before the ack publish
// so no lock is held across a re-entrant bus call.
func (inst *Service) handleWrite(msg *app.Msg) {
	// Defensive self-skip: acks go to _INBOX.* not clipboard.write, but a
	// future broadcast on the served subject would otherwise loop back in.
	// Mirrors fsbroker's guard.
	if msg.Sender == ServiceAppId {
		return
	}

	inst.mu.Lock()
	inst.pending = append(inst.pending, string(msg.Payload))
	dropped := 0
	if len(inst.pending) > maxPending {
		dropped = len(inst.pending) - maxPending
		inst.pending = inst.pending[dropped:]
	}
	depth := len(inst.pending)
	inst.mu.Unlock()

	if dropped > 0 {
		inst.log.Warn().Int("dropped", dropped).Int("max", maxPending).
			Msg("clipboardbroker: pending queue full; dropped oldest copies (no host drain?)")
	}
	inst.log.Info().Str("from", string(msg.Sender)).Int("bytes", len(msg.Payload)).Int("depth", depth).
		Msg("clipboardbroker: copy enqueued")

	if msg.Reply != "" {
		// Empty ack: the requester's Request returns (and is audited) on
		// receipt; the payload carries no information beyond "received".
		if err := inst.busClient.Publish(msg.Reply, nil); err != nil {
			inst.log.Error().Err(err).Msg("clipboardbroker: ack publish failed")
		}
	}
}
