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
	"crypto/rand"
	"encoding/hex"
	"sync"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/cell"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// SubjectWrite is the request subject an app publishes to in order to copy text
// to the viewport clipboard (ADR-0026 §SD3, Update 2026-05-30). The request
// payload is the raw UTF-8 text, or a labelled Payload (ADR-0317 §SD1);
// the reply is an empty ack for raw text and a CopyRecord for a payload.
const SubjectWrite = "clipboard.write"

// deferred: nothing in the tree asks the broker to resolve a handle yet
// (a paste inside boxer that restores the labels, ADR-0317 §SD3), so a
// handle on the outside clipboard is inert everywhere until a paste path
// calls Resolve.
//
// SubjectResolve answers a copy handle with its payload (ADR-0317 §SD3):
// the request is the handle's id, the reply the encoded payload, or empty
// for a handle the broker does not hold.
const SubjectResolve = "clipboard.resolve"

// PolicyModeE is how the clipboard policy applies on this host (ADR-0317
// §SD4, ADR-0307 §SD6).
type PolicyModeE uint8

const (
	// PolicyModeRecord records every copy and prevents none: the host the
	// person administers, and the compile-date behaviour.
	PolicyModeRecord PolicyModeE = iota
	// PolicyModeEnforce keeps labelled text above the free tier off the
	// viewport clipboard: it leaves as a handle. An org-managed host.
	PolicyModeEnforce
)

// Policy is the deployment's clipboard policy: the tier up to which
// plaintext may leave freely, the approval path per tier above it, and
// whether the host enforces or records.
type Policy struct {
	FreeTier agentrun.TierE
	Mode     PolicyModeE
	// Approvals is the cell's mapping (ADR-0312 §SD3): under enforcement,
	// a tier on the person's path leaves as a handle the person may
	// resolve inside boxer; every other path above the free tier is
	// refused. Nil is the default mapping.
	Approvals cell.ApprovalPolicy
}

// DefaultPolicy is the compile-date behaviour: every tier free, recorded.
func DefaultPolicy() (p Policy) { return Policy{FreeTier: agentrun.TierRed, Mode: PolicyModeRecord} }

// PolicyOf is the clipboard policy the cell declaration states (ADR-0317
// §SD4): the clipboard's clearance is the free tier, the approvals are
// the cell's, and an organisation-managed host enforces where one the
// person administers records.
func PolicyOf(decl cell.Declaration) (p Policy) {
	p = Policy{FreeTier: decl.Clearances.Clipboard, Mode: PolicyModeRecord, Approvals: decl.Approvals}
	if decl.Managed {
		p.Mode = PolicyModeEnforce
	}
	return
}

func (inst Policy) path(t agentrun.TierE) cell.ApprovalTierE {
	if inst.Approvals == nil {
		return cell.DefaultApprovalPolicy().Path(t)
	}
	return inst.Approvals.Path(t)
}

// maxHandles bounds the handles the broker keeps; the oldest go first.
const maxHandles = 256

// maxRecords bounds the broker's own record of copies; the oldest go
// first. The bus audit and the journal's copy entries are the durable
// record; this is the window's.
const maxRecords = 1024

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
	unsubRes  func()
	policy    Policy

	mu      sync.Mutex
	pending []string
	// handles are the copies by reference the broker holds, by id, and
	// handleOrder their ids oldest first.
	handles     map[string]Payload
	handleOrder []string
	// records are the copies the broker made, oldest first, bounded by
	// maxRecords: its own audit beside the bus's, since the bus record
	// names app, subject and bytes and not the label (ADR-0317 §SD1). A
	// raw copy is recorded with the zero label, so every forwarded text
	// has a record behind it (law L20).
	records []CopyRecord
}

// NewService constructs and subscribes the service. The returned Service holds
// a bus client scoped to ServiceAppId with just the two caps it needs: receive
// on clipboard.write and reply on inboxes.
func NewService(inst *inprocbus.Inst, log zerolog.Logger) (s *Service, err error) {
	return NewServiceWithPolicy(inst, log, DefaultPolicy())
}

// NewServiceWithPolicy is NewService under the deployment's clipboard
// policy.
func NewServiceWithPolicy(inst *inprocbus.Inst, log zerolog.Logger, policy Policy) (s *Service, err error) {
	if inst == nil {
		err = eh.Errorf("clipboardbroker: nil inst")
		return
	}
	s = &Service{
		inst:    inst,
		log:     log.With().Str("app", string(ServiceAppId)).Logger(),
		policy:  policy,
		handles: make(map[string]Payload),
	}
	s.busClient = inst.NewClient(ServiceAppId, []app.SubjectFilter{
		{Pattern: SubjectWrite, Direction: app.CapDirectionBoth, Reason: "clipboard Powerbox serves clipboard.write"},
		{Pattern: SubjectResolve, Direction: app.CapDirectionBoth, Reason: "clipboard Powerbox resolves copy handles (ADR-0317 §SD3)"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "clipboard acks replies to inboxes"},
	})
	s.unsub, err = s.busClient.Subscribe(SubjectWrite, s.handleWrite)
	if err != nil {
		err = eh.Errorf("clipboardbroker: subscribe: %w", err)
		return
	}
	s.unsubRes, err = s.busClient.Subscribe(SubjectResolve, s.handleResolve)
	if err != nil {
		s.unsub()
		err = eh.Errorf("clipboardbroker: subscribe resolve: %w", err)
		return
	}
	return
}

// Close releases the bus subscriptions. Idempotent.
func (inst *Service) Close() {
	if inst.unsub != nil {
		inst.unsub()
		inst.unsub = nil
	}
	if inst.unsubRes != nil {
		inst.unsubRes()
		inst.unsubRes = nil
	}
}

// Policy is the policy the service runs under.
func (inst *Service) Policy() (p Policy) { return inst.policy }

// Resolve is the payload behind a handle id the broker holds.
func (inst *Service) Resolve(id string) (p Payload, ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	p, ok = inst.handles[id]
	return
}

// Records are the copies the broker made, oldest first, raw ones with
// the zero label.
func (inst *Service) Records() (records []CopyRecord) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return append([]CopyRecord(nil), inst.records...)
}

// note keeps a record, bounded. The caller holds the mutex.
func (inst *Service) note(rec CopyRecord) {
	inst.records = append(inst.records, rec)
	if over := len(inst.records) - maxRecords; over > 0 {
		inst.records = append([]CopyRecord(nil), inst.records[over:]...)
	}
}

// handle keeps the payload behind a fresh random id, bounded. The id
// carries nothing of the content: the string on the outside clipboard
// must not confirm a guess of what it stands for. The caller holds the
// mutex.
func (inst *Service) handle(p Payload) (id string) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("clipboardbroker: the random source failed: " + err.Error())
	}
	id = hex.EncodeToString(b[:])
	inst.handles[id] = p
	inst.handleOrder = append(inst.handleOrder, id)
	if len(inst.handleOrder) > maxHandles {
		delete(inst.handles, inst.handleOrder[0])
		inst.handleOrder = inst.handleOrder[1:]
	}
	return
}

// decide is what the policy does with a labelled payload (ADR-0317 §SD3,
// §SD4). Content another model touched (an integrity level above this
// run's own model) copies by reference whatever the mode: a handle is
// how it comes back with its labels, not an exit decision. The tier is
// the exit decision: plaintext at or below the free tier; above it, on a
// host that only records, plaintext with a recorded exit; under
// enforcement a handle for a tier on the person's path and a refusal
// for every other path. Text that this run's own model wrote and that
// carries no tier above the free one copies plaintext with its record:
// an integrity label marks what comes back, not what goes out. A handle
// is not an approval; Approval is set only where one was given, and no
// path gives one here, since the person's approval of a plaintext exit
// is the deferred plugin's dialog (§SD2). The caller holds the mutex.
func (inst *Service) decide(p Payload) (text string, rec CopyRecord) {
	rec = CopyRecord{Digest: agentrun.ContentDigest(p.Text), Label: p.Label, Level: uint8(p.Label.Level), Tier: uint8(p.Label.Tier), Source: p.Source, Bytes: len(p.Text)}
	above := p.Label.Tier > inst.policy.FreeTier
	modelTouched := p.Label.Level > agentrun.LevelOwnModel
	switch {
	case modelTouched && (!above || inst.policy.Mode != PolicyModeEnforce):
		rec.Handle, rec.Recorded = HandlePrefix+inst.handle(p), above && inst.policy.Mode != PolicyModeEnforce
		return rec.Handle, rec
	case inst.policy.Mode != PolicyModeEnforce:
		rec.Plaintext, rec.Recorded = true, above
		return p.Text, rec
	case !above:
		rec.Plaintext = true
		return p.Text, rec
	}
	path := inst.policy.path(p.Label.Tier)
	if path != cell.ApprovalTierPerson {
		rec.Refused = true
		rec.Reason = p.Label.Tier.String() + " content does not leave by the clipboard (cleared " + inst.policy.FreeTier.String() + "): the approval path for it is " + path.String()
		return "", rec
	}
	rec.Handle = HandlePrefix + inst.handle(p)
	return rec.Handle, rec
}

// decideRaw is what the policy does with unlabelled text, the legacy
// form: on a host that records it goes as it is, recorded with the zero
// label. Under enforcement its provenance is unknown, and unknown is
// read as content another model may have touched (LevelOtherModel at
// the free tier), so it copies by reference like the rest; a caller that
// knows better labels its payload. The caller holds the mutex.
func (inst *Service) decideRaw(text string) (out string, rec CopyRecord) {
	rec = CopyRecord{Digest: agentrun.ContentDigest(text), Raw: true, Bytes: len(text)}
	if inst.policy.Mode != PolicyModeEnforce {
		rec.Plaintext = true
		return text, rec
	}
	p := Payload{Text: text, Label: agentrun.Label{Level: agentrun.LevelOtherModel}}
	rec.Level, rec.Label = uint8(p.Label.Level), p.Label
	rec.Handle = HandlePrefix + inst.handle(p)
	return rec.Handle, rec
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

	p, labelled, derr := DecodePayload(msg.Payload)
	if derr != nil {
		inst.log.Warn().Err(derr).Str("from", string(msg.Sender)).Msg("clipboardbroker: payload refused")
		if msg.Reply != "" {
			if err := buscodec.Reply(inst.busClient.Publish, msg.Reply, CopyRecord{Refused: true, Reason: "the payload does not decode: " + derr.Error()}); err != nil {
				inst.log.Error().Err(err).Msg("clipboardbroker: refusal publish failed")
			}
		}
		return
	}
	inst.mu.Lock()
	text := p.Text
	var rec CopyRecord
	if labelled {
		text, rec = inst.decide(p)
	} else {
		text, rec = inst.decideRaw(p.Text)
	}
	inst.note(rec)
	if rec.Refused {
		inst.mu.Unlock()
		inst.log.Warn().Str("from", string(msg.Sender)).Str("label", p.Label.String()).Str("source", p.Source).Str("reason", rec.Reason).Msg("clipboardbroker: copy refused")
		if msg.Reply != "" {
			if err := buscodec.Reply(inst.busClient.Publish, msg.Reply, rec); err != nil {
				inst.log.Error().Err(err).Msg("clipboardbroker: refusal publish failed")
			}
		}
		return
	}
	inst.pending = append(inst.pending, text)
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
	ev := inst.log.Info().Str("from", string(msg.Sender)).Int("bytes", len(msg.Payload)).Int("depth", depth)
	if labelled {
		ev = ev.Str("label", p.Label.String()).Str("source", p.Source).Str("digest", rec.Digest).Bool("plaintext", rec.Plaintext).Str("handle", rec.Handle)
	}
	ev.Msg("clipboardbroker: copy enqueued")

	if msg.Reply == "" {
		return
	}
	if !labelled {
		// Empty ack: the requester's Request returns (and is audited) on
		// receipt; the payload carries no information beyond "received".
		if err := inst.busClient.Publish(msg.Reply, nil); err != nil {
			inst.log.Error().Err(err).Msg("clipboardbroker: ack publish failed")
		}
		return
	}
	if err := buscodec.Reply(inst.busClient.Publish, msg.Reply, rec); err != nil {
		inst.log.Error().Err(err).Msg("clipboardbroker: ack publish failed")
	}
}

// handleResolve answers a handle id with its payload, or nothing.
func (inst *Service) handleResolve(msg *app.Msg) {
	if msg.Sender == ServiceAppId || msg.Reply == "" {
		return
	}
	p, ok := inst.Resolve(string(msg.Payload))
	var reply []byte
	if ok {
		if b, err := p.Encode(); err == nil {
			reply = b
		}
	}
	if err := inst.busClient.Publish(msg.Reply, reply); err != nil {
		inst.log.Error().Err(err).Msg("clipboardbroker: resolve reply failed")
	}
}
