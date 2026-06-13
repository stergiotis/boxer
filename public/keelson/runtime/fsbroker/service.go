// Package fsbroker is the runtime's file-system Powerbox per ADR-0026 §SD7.
// Apps that want to read a file publish to fs.dialog.read; the broker
// queues a pending request that the host's file picker resolves on user
// selection. On resolve the broker mints an opaque handle uuid, augments
// the requesting client's caps to include fs.handle.{uuid}.> and replies
// with the handle subject prefix. The app then publishes to
// fs.handle.{uuid}.read to actually fetch the file content — it never
// sees a path.
//
// M2.6 ships the service + a programmatic Resolve API for tests and for
// the M2.6b egui picker bridge. The bridge calls Pending to learn what
// dialogs are active, drives the picker widget, and feeds the selection
// back via Resolve / Cancel.
package fsbroker

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Subject taxonomy implemented by this service (ADR-0026 §SD3).
const (
	SubjectDialogRead   = "fs.dialog.read"
	SubjectDialogWrite  = "fs.dialog.write"
	SubjectDialogBundle = "fs.dialog.bundle"
	SubjectDialogWatch  = "fs.dialog.watch"
	HandleSubjectPrefix = "fs.handle." // followed by {uuid}.{op}
	// HandleEventOp is the trailing token for the streaming-event subject
	// the broker publishes on once a watch is active:
	// fs.handle.{uuid}.event. Apps subscribe to this subject to receive
	// WatchEvent payloads.
	HandleEventOp = "event"
)

// ServiceAppId is the synthetic AppId the broker registers under.
const ServiceAppId app.AppIdT = "runtime.fs"

// DefaultMaxReadBytes caps a single fs.handle.{uuid}.read response. The
// whole file is buffered into memory (and again as the bus payload), so an
// app granted a handle to a multi-gigabyte file — or an unbounded special
// file like /dev/zero — would otherwise drive the process OOM. Hosts that
// genuinely need larger single-shot reads raise it via SetMaxReadBytes;
// streaming reads are a separate follow-up.
const DefaultMaxReadBytes int64 = 64 << 20

// HandleModeE encodes whether a granted handle permits read, write, or
// directory enumeration. Single-op semantics for M2.6.
type HandleModeE uint8

const (
	HandleModeUnspecified HandleModeE = 0
	HandleModeRead        HandleModeE = 1
	HandleModeWrite       HandleModeE = 2
	HandleModeBundle      HandleModeE = 3
	// HandleModeWatch marks a handle as eligible for fs.handle.{uuid}.watch
	// streaming notifications. Read/write are not permitted on watch handles
	// (and vice versa); the picker selects the directory to observe.
	HandleModeWatch HandleModeE = 4
)

// DialogReply is the payload returned on every fs.dialog.{op} request,
// wire-encoded via buscodec (CBOR canonical). On approval
// HandleSubjectPrefix carries fs.handle.{uuid} and the app's caps
// already cover fs.handle.{uuid}.> at the moment the reply lands.
type DialogReply struct {
	Granted             bool   `json:"granted"`
	HandleSubjectPrefix string `json:"handleSubjectPrefix,omitempty"`
	Reason              string `json:"reason,omitempty"`
}

// PendingRequest describes one dialog awaiting user selection. The host's
// picker bridge inspects the Op + AppId to render an appropriate UI and
// then calls Service.Resolve(Id, path) with the user's choice.
type PendingRequest struct {
	Id    string
	Op    string
	AppId app.AppIdT
}

// handle stores a resolved file grant. Path is never exposed back to the
// app — the app addresses the file via fs.handle.{uuid}.{op} only.
type handle struct {
	uuid    string
	path    string
	mode    HandleModeE
	appId   app.AppIdT
	created time.Time
}

// pendingEntry tracks an in-flight dialog. replySubject is the inbox the
// requesting app's Request is waiting on.
type pendingEntry struct {
	id           string
	op           string
	appId        app.AppIdT
	replySubject string
	created      time.Time
}

// Service subscribes to fs.> and dispatches dialog opens, handle ops, and
// handle close to either a pending queue (dialogs) or local syscalls
// (handles).
type Service struct {
	inst      *inprocbus.Inst
	log       zerolog.Logger
	busClient *inprocbus.Client
	unsub     func()

	mu           sync.Mutex
	handles      map[string]*handle
	pending      map[string]*pendingEntry
	watches      map[string]*activeWatch
	maxReadBytes int64
}

// SetMaxReadBytes overrides DefaultMaxReadBytes for single-shot handle
// reads. A non-positive value restores the default. Concurrent-safe.
func (inst *Service) SetMaxReadBytes(n int64) {
	inst.mu.Lock()
	if n <= 0 {
		n = DefaultMaxReadBytes
	}
	inst.maxReadBytes = n
	inst.mu.Unlock()
}

// NewService constructs and subscribes the service.
func NewService(inst *inprocbus.Inst, log zerolog.Logger) (s *Service, err error) {
	if inst == nil {
		err = eh.Errorf("fsbroker: nil inst")
		return
	}
	s = &Service{
		inst:         inst,
		log:          log.With().Str("app", string(ServiceAppId)).Logger(),
		handles:      make(map[string]*handle),
		pending:      make(map[string]*pendingEntry),
		watches:      make(map[string]*activeWatch),
		maxReadBytes: DefaultMaxReadBytes,
	}
	s.busClient = inst.NewClient(ServiceAppId, []app.SubjectFilter{
		{Pattern: "fs.>", Direction: app.CapDirectionBoth, Reason: "fs Powerbox serves all fs subjects"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "fs replies to inboxes"},
	})
	s.unsub, err = s.busClient.Subscribe("fs.>", s.handleRequest)
	if err != nil {
		err = eh.Errorf("fsbroker: subscribe: %w", err)
		return
	}
	return
}

// Close releases the subscription and tears down every live watch. Watches
// stopped this way emit their final WatchEventClosed (where the backend
// supports it) before their event channel closes; consumers reading the
// stream when Close returns may still drain a small number of buffered
// events.
func (inst *Service) Close() {
	inst.mu.Lock()
	live := inst.watches
	inst.watches = make(map[string]*activeWatch)
	inst.mu.Unlock()
	for _, w := range live {
		w.backend.Stop()
	}
	if inst.unsub != nil {
		inst.unsub()
		inst.unsub = nil
	}
}

// Pending returns the set of currently-pending dialog requests in
// insertion-time order. The host UI bridge calls this each frame to learn
// what to draw.
func (inst *Service) Pending() (out []PendingRequest) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	out = make([]PendingRequest, 0, len(inst.pending))
	for _, p := range inst.pending {
		out = append(out, PendingRequest{Id: p.id, Op: p.op, AppId: p.appId})
	}
	return
}

// Resolve completes a pending dialog with the user's chosen path. Mints a
// fresh handle uuid, registers (uuid, path, mode), grants
// fs.handle.{uuid}.> to the requesting client, and replies on the request
// inbox. Returns an error when no such pending request exists or the
// app's client cannot be found.
func (inst *Service) Resolve(reqId string, path string) (handleUuid string, err error) {
	inst.mu.Lock()
	p, ok := inst.pending[reqId]
	if !ok {
		inst.mu.Unlock()
		err = eh.Errorf("fsbroker: no pending request %q", reqId)
		return
	}
	delete(inst.pending, reqId)
	mode := modeFor(p.op)
	handleUuid = mintHandleUuid(p.appId, path)
	inst.handles[handleUuid] = &handle{
		uuid:    handleUuid,
		path:    path,
		mode:    mode,
		appId:   p.appId,
		created: time.Now(),
	}
	inst.mu.Unlock()

	client, ok := inst.inst.ClientByAppId(p.appId)
	if ok {
		dir := app.CapDirectionPub
		if mode == HandleModeWatch {
			// Watch handles also need Sub on .event so the app can
			// subscribe to the broker-published event stream. Read /
			// write / bundle stay Pub-only — they're request-reply.
			dir = app.CapDirectionBoth
		}
		client.AddCap(app.SubjectFilter{
			Pattern:   HandleSubjectPrefix + handleUuid + ".>",
			Direction: dir,
			Reason:    "granted via fs.dialog." + p.op,
		})
	} else {
		inst.log.Warn().Str("appId", string(p.appId)).Msg("fsbroker: resolve: no client to grant handle cap")
	}
	err = inst.replyDialog(p.replySubject, DialogReply{
		Granted:             true,
		HandleSubjectPrefix: HandleSubjectPrefix + handleUuid,
	})
	return
}

// Cancel completes a pending dialog with a denial reply. Tests for "user
// pressed Cancel" should call this rather than Resolve.
func (inst *Service) Cancel(reqId string) (err error) {
	inst.mu.Lock()
	p, ok := inst.pending[reqId]
	if !ok {
		inst.mu.Unlock()
		err = eh.Errorf("fsbroker: no pending request %q", reqId)
		return
	}
	delete(inst.pending, reqId)
	inst.mu.Unlock()
	err = inst.replyDialog(p.replySubject, DialogReply{Granted: false, Reason: "user cancelled"})
	return
}

func (inst *Service) handleRequest(msg *app.Msg) {
	// Skip self-published broadcasts (e.g. fs.handle.{uuid}.event payloads
	// emitted by the watch pump). The broker's own fs.> subscription would
	// otherwise loop back into dispatch.
	if msg.Sender == ServiceAppId {
		return
	}
	if msg.Reply == "" {
		inst.log.Warn().Str("subject", msg.Subject).Msg("fsbroker: request without reply")
		return
	}
	switch {
	case msg.Subject == SubjectDialogRead:
		inst.queuePending(msg, "read")
	case msg.Subject == SubjectDialogWrite:
		inst.queuePending(msg, "write")
	case msg.Subject == SubjectDialogBundle:
		inst.queuePending(msg, "bundle")
	case msg.Subject == SubjectDialogWatch:
		inst.queuePending(msg, "watch")
	case strings.HasPrefix(msg.Subject, HandleSubjectPrefix):
		inst.handleHandleOp(msg)
	default:
		inst.replyError(msg.Reply, "unknown fs subject: "+msg.Subject)
	}
}

func (inst *Service) queuePending(msg *app.Msg, op string) {
	reqId := mintRequestId(msg.Sender, op)
	inst.mu.Lock()
	inst.pending[reqId] = &pendingEntry{
		id:           reqId,
		op:           op,
		appId:        msg.Sender,
		replySubject: msg.Reply,
		created:      time.Now(),
	}
	inst.mu.Unlock()
	inst.log.Info().Str("reqId", reqId).Str("op", op).Str("from", string(msg.Sender)).
		Msg("fsbroker: dialog pending")
}

func (inst *Service) handleHandleOp(msg *app.Msg) {
	parts := strings.Split(msg.Subject, ".")
	if len(parts) != 4 || parts[0] != "fs" || parts[1] != "handle" {
		inst.replyError(msg.Reply, "malformed handle subject: "+msg.Subject)
		return
	}
	uuid := parts[2]
	op := parts[3]
	inst.mu.Lock()
	h, ok := inst.handles[uuid]
	inst.mu.Unlock()
	if !ok {
		inst.replyError(msg.Reply, "unknown handle: "+uuid)
		return
	}
	switch op {
	case "read":
		inst.handleRead(msg.Reply, h)
	case "close":
		inst.handleClose(msg.Reply, uuid)
	case "watch":
		inst.handleWatch(msg, h)
	case "unwatch":
		inst.handleUnwatch(msg.Reply, uuid)
	default:
		inst.replyError(msg.Reply, "unsupported handle op: "+op)
	}
}

func (inst *Service) handleRead(reply string, h *handle) {
	if h.mode != HandleModeRead {
		inst.replyError(reply, "handle not opened for read")
		return
	}
	inst.mu.Lock()
	max := inst.maxReadBytes
	inst.mu.Unlock()
	if max <= 0 {
		max = DefaultMaxReadBytes
	}
	f, err := os.Open(h.path)
	if err != nil {
		inst.replyError(reply, "open: "+err.Error())
		return
	}
	defer f.Close()
	buf := &bytes.Buffer{}
	// Read at most max+1 bytes. Hitting the extra byte means the file
	// exceeds the cap, so we refuse rather than buffer an unbounded payload
	// into memory (and then again as the bus message).
	n, err := io.Copy(buf, io.LimitReader(f, max+1))
	if err != nil {
		inst.replyError(reply, "read: "+err.Error())
		return
	}
	if n > max {
		inst.replyError(reply, fmt.Sprintf("file exceeds max read size (%d bytes)", max))
		return
	}
	_ = inst.busClient.Publish(reply, buf.Bytes())
}

func (inst *Service) handleClose(reply string, uuid string) {
	inst.mu.Lock()
	h := inst.handles[uuid]
	w, hadWatch := inst.watches[uuid]
	if hadWatch {
		delete(inst.watches, uuid)
	}
	delete(inst.handles, uuid)
	inst.mu.Unlock()
	if hadWatch {
		w.backend.Stop()
	}
	// Revoke the fs.handle.{uuid}.> cap granted at Resolve so the closed
	// handle's subject stops matching and the app's cap set doesn't grow
	// without bound across a long session.
	if h != nil {
		inst.revokeHandleCap(h.appId, uuid)
	}
	_ = inst.busClient.Publish(reply, nil)
}

// revokeHandleCap strips the per-handle cap from the owning app's bus
// client. No-op when the client is gone. Mirrors the AddCap performed in
// Resolve.
func (inst *Service) revokeHandleCap(appId app.AppIdT, uuid string) {
	client, ok := inst.inst.ClientByAppId(appId)
	if !ok {
		return
	}
	client.RemoveCap(HandleSubjectPrefix + uuid + ".>")
}

func (inst *Service) replyError(replySubject, reason string) {
	_ = inst.replyDialog(replySubject, DialogReply{Granted: false, Reason: reason})
}

func (inst *Service) replyDialog(replySubject string, r DialogReply) (err error) {
	payload, err := MarshalDialogReply(r)
	if err != nil {
		return
	}
	err = inst.busClient.Publish(replySubject, payload)
	return
}

func (inst *Service) replyWatch(replySubject string, r WatchReply) (err error) {
	payload, err := MarshalWatchReply(r)
	if err != nil {
		return
	}
	err = inst.busClient.Publish(replySubject, payload)
	return
}

func modeFor(op string) (m HandleModeE) {
	switch op {
	case "read":
		m = HandleModeRead
	case "write":
		m = HandleModeWrite
	case "bundle":
		m = HandleModeBundle
	case "watch":
		m = HandleModeWatch
	default:
		m = HandleModeUnspecified
	}
	return
}

// mintRequestId is a per-dialog id (not the handle uuid). Uses blake3 over
// (sender, op, time) for uniqueness within a process.
func mintRequestId(sender app.AppIdT, op string) (id string) {
	h := blake3.New(8, nil)
	_, _ = h.Write([]byte(sender))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(op))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	id = hex.EncodeToString(h.Sum(nil))
	return
}

// mintHandleUuid is stable across the (appId, path) tuple within a session
// — re-resolving the same path for the same app yields the same uuid, so
// the app's prior cap covers the new handle.
func mintHandleUuid(appId app.AppIdT, path string) (uuid string) {
	h := blake3.New(8, nil)
	_, _ = h.Write([]byte(appId))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(path))
	uuid = hex.EncodeToString(h.Sum(nil))
	return
}

// handleWatch starts a streaming watch on the handle's path. Rejected when
// the handle was not minted via fs.dialog.watch (HandleModeWatch). On
// success the broker replies with a WatchReply naming the event subject
// and spawns a pump goroutine that publishes each backend event to
// fs.handle.{uuid}.event. Idempotent at the handle level — a second watch
// request for an already-watching handle replies Started=false.
func (inst *Service) handleWatch(msg *app.Msg, h *handle) {
	if h.mode != HandleModeWatch {
		inst.replyError(msg.Reply, "handle not opened for watch")
		return
	}
	req, err := UnmarshalWatchRequest(msg.Payload)
	if err != nil {
		inst.replyError(msg.Reply, "watch: malformed request: "+err.Error())
		return
	}
	inst.mu.Lock()
	_, already := inst.watches[h.uuid]
	inst.mu.Unlock()
	if already {
		_ = inst.replyWatch(msg.Reply, WatchReply{
			Started: false,
			Reason:  "watch already active",
		})
		return
	}
	backend, backendName, err := pickBackend(h.path, req)
	if err != nil {
		inst.replyError(msg.Reply, "watch: "+err.Error())
		return
	}
	err = backend.Start()
	if err != nil {
		inst.replyError(msg.Reply, "watch: start: "+err.Error())
		return
	}
	w := &activeWatch{
		uuid:    h.uuid,
		backend: backend,
	}
	inst.mu.Lock()
	inst.watches[h.uuid] = w
	inst.mu.Unlock()
	go inst.pumpWatch(w)
	eventSubject := HandleSubjectPrefix + h.uuid + "." + HandleEventOp
	err = inst.replyWatch(msg.Reply, WatchReply{
		Started:      true,
		EventSubject: eventSubject,
		Backend:      backendName,
	})
	if err != nil {
		inst.log.Warn().Err(err).Str("uuid", h.uuid).Msg("fsbroker: watch reply failed")
	}
}

// handleUnwatch stops an active watch but keeps the handle alive — a
// subsequent fs.handle.{uuid}.watch may restart streaming. Always replies
// OK; unknown uuid is silently treated as already-stopped.
func (inst *Service) handleUnwatch(reply string, uuid string) {
	inst.mu.Lock()
	w, ok := inst.watches[uuid]
	if ok {
		delete(inst.watches, uuid)
	}
	inst.mu.Unlock()
	if ok {
		w.backend.Stop()
	}
	_ = inst.busClient.Publish(reply, nil)
}

// pumpWatch reads from the backend's event channel and publishes each
// event onto fs.handle.{uuid}.event. Exits when the backend's channel
// closes (Stop / root-vanished). Bus publishes are synchronous; a slow
// subscriber stalls the pump and may force the backend's bounded queue to
// emit a synthetic WatchEventOverflow.
func (inst *Service) pumpWatch(w *activeWatch) {
	eventSubject := HandleSubjectPrefix + w.uuid + "." + HandleEventOp
	for ev := range w.backend.Events() {
		payload, err := MarshalWatchEvent(ev)
		if err != nil {
			inst.log.Warn().Err(err).Str("uuid", w.uuid).Msg("fsbroker: watch event marshal")
			continue
		}
		err = inst.busClient.Publish(eventSubject, payload)
		if err != nil {
			inst.log.Warn().Err(err).Str("subject", eventSubject).Msg("fsbroker: watch event publish")
		}
	}
	// Backend stream closed (Stop or root-vanished). Drop the watch entry
	// if it's still listed; handleClose / handleUnwatch / Close may have
	// already removed it.
	inst.mu.Lock()
	if cur, ok := inst.watches[w.uuid]; ok && cur == w {
		delete(inst.watches, w.uuid)
	}
	inst.mu.Unlock()
}
