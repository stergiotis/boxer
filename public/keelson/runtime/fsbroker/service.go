// Package fsbroker is the runtime's file-system Powerbox per ADR-0026 §SD7.
// Apps that want to read a file publish to fs.dialog.read; the broker
// queues a pending request that the host's file picker resolves on user
// selection. On resolve the broker mints an opaque handle uuid, augments
// the requesting client's caps to include fs.handle.{uuid}.> and replies
// with the handle subject prefix. The app then publishes to
// fs.handle.{uuid}.read to actually fetch the file content — it never
// sees a path.
//
// Beside the dialogs and their handles, each app has a data area it reaches
// by file name over fs.appdata.{op}, for records it keeps for itself (see
// appdata.go).
//
// M2.6 ships the service + a programmatic Resolve API for tests and for
// the M2.6b egui picker bridge. The bridge calls Pending to learn what
// dialogs are active, drives the picker widget, and feeds the selection
// back via Resolve / Cancel.
package fsbroker

import (
	"bytes"
	"cmp"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
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

// DialogTimeout and HandleOpTimeout are what a CLIENT should pass to
// [app.BusI.RequestWithTimeout] for the two kinds of request this service
// answers. They live here, beside the subjects, because the thing that decides
// them is a property of the subject rather than of the caller: only this
// package knows which of its requests waits on a person.
//
// A `fs.dialog.*` request is not answered until somebody has found a folder,
// typed a name and pressed Save. Under the transport default — five seconds,
// the order of magnitude a NATS client uses — every dialog flow fails while
// its picker is still on screen and the app reports a timeout for a dialog the
// user is looking at. That is not hypothetical; it is what driving the desktop
// host did to mdedit's Save, and every consumer here had the same defect
// (ADR-0026, 2026-08-08).
//
// Finite rather than unbounded, so a picker nobody ever answers releases the
// caller's goroutine instead of leaking it for the life of the window. Ten
// minutes is well past the point where a dialog is still a live gesture.
//
// The handle ops are the opposite case — a filesystem call and a bus hop, no
// human anywhere — so they get a bound generous for slow storage and far below
// the dialog's, because a wedged filesystem should not look like a slow
// reader.
const (
	DialogTimeout   = 10 * time.Minute
	HandleOpTimeout = 30 * time.Second
)

// MaxInflightOps bounds the handle and appdata ops running at once. The
// in-process bus runs a responder inline on the requester's goroutine, so
// the broker hands each such op to a goroutine of its own; without that, a
// read that blocks in the filesystem would hold the requester past
// HandleOpTimeout. An op wedged in the filesystem keeps its slot, so the
// bound is what stops a hung mount from accumulating goroutines: past it,
// a request is refused at once instead of queued.
const MaxInflightOps = 64

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
	// streaming notifications on a DIRECTORY the picker selected. Read/write
	// are not permitted on watch handles. The reverse is narrower: a
	// HandleModeRead handle may also watch — its own file, filtered
	// broker-side (see handleWatch) — while write/bundle handles cannot.
	HandleModeWatch HandleModeE = 4
)

// DialogReply is the payload returned on every fs.dialog.{op} request,
// wire-encoded via buscodec (CBOR canonical). On approval
// HandleSubjectPrefix carries fs.handle.{uuid} and the app's caps
// already cover fs.handle.{uuid}.> at the moment the reply lands.
type DialogReply struct {
	Granted             bool   `json:"granted"`
	HandleSubjectPrefix string `json:"handleSubjectPrefix,omitempty"`
	// DisplayName is the BASENAME of the resolved path — the one fact about
	// the file's identity the Powerbox reveals, so an editor can title what
	// it has open. The path itself stays inside the broker (ADR-0026 §SD7):
	// the name says which file among the ones the user picked, never where
	// anything lives. Empty on denial.
	DisplayName string `json:"displayName,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// PendingRequest describes one dialog awaiting user selection. The host's
// picker bridge inspects the Op + AppId to render an appropriate UI and
// then calls Service.Resolve(Id, path) with the user's choice.
type PendingRequest struct {
	Id    string
	Op    string
	AppId app.AppIdT
	// SuggestedName is the optional filename the requesting app asked the
	// picker to pre-fill (from the DialogRequest payload). Meaningful only for
	// the "Save as" dialog (op "write"); empty otherwise. The bridge feeds it
	// to filepicker.WithDefaultFilename — the user may overwrite it, and the
	// broker never derives the resolved path from it.
	SuggestedName string
}

// handle stores a resolved file grant. Path is never exposed back to the
// app — the app addresses the file via fs.handle.{uuid}.{op} only.
type handle struct {
	uuid        string
	path        string
	mode        HandleModeE
	appId       app.AppIdT
	instanceKey uint64 // the window the dialog came from; 0 when unattributed
	created     time.Time
}

// pendingEntry tracks an in-flight dialog. replySubject is the inbox the
// requesting app's Request is waiting on; suggestedName is the optional
// picker pre-fill decoded from the DialogRequest payload. instanceKey is the
// requesting window (zero when unattributed), so its closing can drop the
// entry; seq orders entries by arrival.
type pendingEntry struct {
	id            string
	op            string
	appId         app.AppIdT
	instanceKey   uint64
	replySubject  string
	suggestedName string
	created       time.Time
	seq           uint64
}

// Service subscribes to fs.> and dispatches dialog opens, handle ops, and
// handle close to either a pending queue (dialogs) or local syscalls
// (handles).
type Service struct {
	inst        *inprocbus.Inst
	log         zerolog.Logger
	busClient   *inprocbus.Client
	unsub       func()
	unsubClosed func()

	mu           sync.Mutex
	handles      map[string]*handle
	pending      map[string]*pendingEntry
	pendingSeq   uint64
	watches      map[string]*activeWatch
	maxReadBytes int64
	appDataRoot  string

	// opSlots holds one token per handle or appdata op in flight
	// (MaxInflightOps).
	opSlots chan struct{}
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
		appDataRoot:  defaultAppDataRoot(),
		opSlots:      make(chan struct{}, MaxInflightOps),
	}
	s.busClient = inst.NewClient(ServiceAppId, []app.SubjectFilter{
		{Pattern: "fs.>", Direction: app.CapDirectionBoth, Reason: "fs Powerbox serves all fs subjects"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "fs replies to inboxes"},
		{Pattern: app.SubjectInstanceClosed, Direction: app.CapDirectionSub, Reason: "fs drops the pending dialogs of a closed instance"},
	})
	s.unsub, err = s.busClient.Subscribe("fs.>", s.handleRequest)
	if err != nil {
		err = eh.Errorf("fsbroker: subscribe: %w", err)
		return
	}
	s.unsubClosed, err = s.busClient.Subscribe(app.SubjectInstanceClosed, s.handleInstanceClosed)
	if err != nil {
		s.unsub()
		s.unsub = nil
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
	if inst.unsubClosed != nil {
		inst.unsubClosed()
		inst.unsubClosed = nil
	}
}

// Pending returns the set of currently-pending dialog requests in
// insertion-time order. The host UI bridge calls this each frame to learn
// what to draw. A dialog older than DialogTimeout is dropped here: its
// requester has stopped waiting, and a picker for it would mint a grant no
// one receives.
func (inst *Service) Pending() (out []PendingRequest) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.pruneExpiredLocked(time.Now())
	entries := make([]*pendingEntry, 0, len(inst.pending))
	for _, p := range inst.pending {
		entries = append(entries, p)
	}
	slices.SortFunc(entries, func(a, b *pendingEntry) int { return cmp.Compare(a.seq, b.seq) })
	out = make([]PendingRequest, 0, len(entries))
	for _, p := range entries {
		out = append(out, PendingRequest{Id: p.id, Op: p.op, AppId: p.appId, SuggestedName: p.suggestedName})
	}
	return
}

// pruneExpiredLocked drops pending dialogs whose requester has timed out.
// Caller holds inst.mu.
func (inst *Service) pruneExpiredLocked(now time.Time) {
	for id, p := range inst.pending {
		if now.Sub(p.created) > DialogTimeout {
			delete(inst.pending, id)
		}
	}
}

// handleInstanceClosed drops the pending dialogs of a window that closed
// (ADR-0240 §SD5): nobody is waiting on their reply inbox any more. The
// envelope's sender is the closed client's own identity.
func (inst *Service) handleInstanceClosed(msg *app.Msg) {
	if msg.SenderInstance == 0 {
		return
	}
	inst.mu.Lock()
	for id, p := range inst.pending {
		if p.appId == msg.Sender && p.instanceKey == msg.SenderInstance {
			delete(inst.pending, id)
		}
	}
	inst.mu.Unlock()
}

// Resolve completes a pending dialog with the user's chosen path. Mints a
// fresh handle uuid, registers (uuid, path, mode), grants
// fs.handle.{uuid}.> to the requesting client, and replies on the request
// inbox. Returns an error when no such pending request exists or the
// app's client cannot be found.
func (inst *Service) Resolve(reqId string, path string) (handleUuid string, err error) {
	inst.mu.Lock()
	inst.pruneExpiredLocked(time.Now())
	p, ok := inst.pending[reqId]
	if !ok {
		inst.mu.Unlock()
		err = eb.Build().Str("reqId", reqId).Errorf("fsbroker: no pending request")
		return
	}
	handleUuid, err = mintHandleUuid()
	if err != nil {
		inst.mu.Unlock()
		return
	}
	delete(inst.pending, reqId)
	mode := modeFor(p.op)
	inst.handles[handleUuid] = &handle{
		uuid:        handleUuid,
		path:        path,
		mode:        mode,
		appId:       p.appId,
		instanceKey: p.instanceKey,
		created:     time.Now(),
	}
	inst.mu.Unlock()

	// The cap goes to the window that opened the dialog, not to whichever
	// window of the app is newest.
	client, ok := inst.inst.ClientByInstance(p.appId, p.instanceKey)
	if ok {
		dir := app.CapDirectionPub
		if mode == HandleModeWatch {
			// Watch handles also need Sub on .event so the app can
			// subscribe to the broker-published event stream. Write /
			// bundle stay Pub-only — they're request-reply.
			dir = app.CapDirectionBoth
		}
		client.AddCap(app.SubjectFilter{
			Pattern:   HandleSubjectPrefix + handleUuid + ".>",
			Direction: dir,
			Reason:    "granted via fs.dialog." + p.op,
		})
		if mode == HandleModeRead {
			// A read handle may watch its own file (handleWatch accepts
			// it), so the app also needs Sub — on exactly the event
			// subject, not the whole prefix: the request-reply ops stay
			// Pub through the wildcard above, and widening it to Both
			// would let the app eavesdrop its own request stream for
			// nothing.
			client.AddCap(app.SubjectFilter{
				Pattern:   HandleSubjectPrefix + handleUuid + "." + HandleEventOp,
				Direction: app.CapDirectionSub,
				Reason:    "watch events for the file granted via fs.dialog." + p.op,
			})
		}
	} else {
		inst.log.Warn().Str("appId", string(p.appId)).Uint64("instanceKey", p.instanceKey).Msg("fsbroker: resolve: no client to grant handle cap")
	}
	err = inst.replyDialog(p.replySubject, DialogReply{
		Granted:             true,
		HandleSubjectPrefix: HandleSubjectPrefix + handleUuid,
		// The broker's own truth, not an echo of the app's SuggestedName —
		// the user may have typed something else in the picker.
		DisplayName: filepath.Base(path),
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
		err = eb.Build().Str("reqId", reqId).Errorf("fsbroker: no pending request")
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
		inst.dispatchOp(msg, inst.handleHandleOp)
	case strings.HasPrefix(msg.Subject, SubjectAppDataPrefix):
		inst.dispatchOp(msg, inst.handleAppData)
	default:
		inst.replyError(msg.Reply, "unknown fs subject: "+msg.Subject)
	}
}

// dispatchOp runs a handle or appdata op on its own goroutine, so the
// requester's timeout bounds its wait however long the filesystem call
// takes (MaxInflightOps). Each op still replies exactly once, and one
// requester's ops stay ordered because each waits for its reply.
func (inst *Service) dispatchOp(msg *app.Msg, op func(*app.Msg)) {
	select {
	case inst.opSlots <- struct{}{}:
	default:
		inst.replyError(msg.Reply, "fsbroker: too many operations in flight")
		return
	}
	go func() {
		defer func() { <-inst.opSlots }()
		op(msg)
	}()
}

func (inst *Service) queuePending(msg *app.Msg, op string) {
	reqId := mintRequestId(msg.Sender, op)
	// Optional advisory hints (currently just a suggested filename the "Save
	// as" dialog pre-fills). A nil / empty payload carries no hints; a
	// malformed one degrades to "no hint" rather than failing the dialog — the
	// hint is advisory and the user still confirms the path in the picker.
	var suggestedName string
	if req, derr := UnmarshalDialogRequest(msg.Payload); derr == nil {
		suggestedName = req.SuggestedName
	} else {
		inst.log.Debug().Err(derr).Str("op", op).Msg("fsbroker: ignoring malformed dialog request hint")
	}
	inst.mu.Lock()
	inst.pendingSeq++
	inst.pending[reqId] = &pendingEntry{
		id:            reqId,
		op:            op,
		appId:         msg.Sender,
		instanceKey:   msg.SenderInstance,
		replySubject:  msg.Reply,
		suggestedName: suggestedName,
		created:       time.Now(),
		seq:           inst.pendingSeq,
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
	// The bus cap check is not the only gate: a handle answers only the app
	// it was granted to, so a manifest declaring a broad fs.handle.> cannot
	// reach another app's grant. Refused as unknown, so the reply does not
	// confirm that the handle exists.
	if ok && msg.Sender != h.appId {
		ok = false
	}
	if !ok {
		inst.replyError(msg.Reply, "unknown handle: "+uuid)
		return
	}
	switch op {
	case "read":
		inst.handleRead(msg.Reply, h)
	case "write":
		inst.handleWrite(msg, h)
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
	defer func() { _ = f.Close() }()
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

// handleWrite persists the request payload to the handle's path. Rejected
// unless the handle was minted via fs.dialog.write (HandleModeWrite) — a
// read-mode handle can never be turned into a write. The payload is written
// whole by replaceHandleFile (temporary file, fsync, rename): it already sits
// in memory as the inbound bus message, so there is no incremental streaming
// or additional size cap to impose beyond what the bus itself already did when
// it delivered the message. On success the broker replies DialogReply{Granted:
// true}; a mode mismatch or filesystem error replies DialogReply{Granted:
// false, Reason:...} through replyError — the same shape every other handle op
// uses — so the app gets an explicit positive or negative acknowledgement it
// can surface to the user (a "Save" needs to report whether the bytes landed).
func (inst *Service) handleWrite(msg *app.Msg, h *handle) {
	if h.mode != HandleModeWrite {
		inst.replyError(msg.Reply, "handle not opened for write")
		return
	}
	err := replaceHandleFile(h.path, msg.Payload)
	if err != nil {
		inst.replyError(msg.Reply, "write: "+err.Error())
		return
	}
	_ = inst.replyDialog(msg.Reply, DialogReply{Granted: true})
}

// replaceHandleFile writes data over path without ever truncating the
// original: the bytes go to a temporary file beside the target, which is
// synced and renamed over it, so a write that fails partway (ENOSPC, EIO, a
// kill) leaves the user's previous document whole. A symlinked target is
// resolved first, so the link survives and the file it names is replaced. The
// target's permission bits carry over; a new file gets 0o644. A target that
// is not a regular file (a device, a FIFO, a dangling link) cannot be renamed
// over meaningfully and is written in place as before.
func replaceHandleFile(path string, data []byte) (err error) {
	target := path
	if resolved, rerr := filepath.EvalSymlinks(path); rerr == nil {
		target = resolved
	}
	perm := os.FileMode(0o644)
	fi, serr := os.Lstat(target)
	switch {
	case serr == nil && !fi.Mode().IsRegular():
		err = os.WriteFile(path, data, 0o644)
		return
	case serr == nil:
		perm = fi.Mode().Perm()
	case !os.IsNotExist(serr):
		err = serr
		return
	}
	dir := filepath.Dir(target)
	var f *os.File
	f, err = os.CreateTemp(dir, "."+filepath.Base(target)+".*.tmp")
	if err != nil {
		return
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Chmod(perm)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, target)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return
	}
	syncDir(dir)
	return
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
		inst.revokeHandleCap(h.appId, h.instanceKey, uuid)
	}
	_ = inst.busClient.Publish(reply, nil)
}

// revokeHandleCap strips the per-handle caps from the bus client of the
// window the handle was granted to. No-op when that client is gone. Mirrors the AddCaps performed in
// Resolve — the wildcard, and the read-handle event Sub (RemoveCap is
// idempotent by pattern, so a handle that never had the second loses
// nothing).
func (inst *Service) revokeHandleCap(appId app.AppIdT, instanceKey uint64, uuid string) {
	client, ok := inst.inst.ClientByInstance(appId, instanceKey)
	if !ok {
		return
	}
	client.RemoveCap(HandleSubjectPrefix + uuid + ".>")
	client.RemoveCap(HandleSubjectPrefix + uuid + "." + HandleEventOp)
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

// mintHandleUuid draws a fresh uuid for every grant. Each Resolve is its own
// grant with its own handle entry and cap, so two opens of one file (two
// editor tabs, a read beside a save) close independently: a uuid derived from
// (appId, path, op) made them share one entry, and closing either revoked
// both, watch included. Random rather than derived also keeps the subject
// unguessable to an app that does not hold the grant.
func mintHandleUuid() (uuid string, err error) {
	var b [16]byte
	_, err = rand.Read(b[:])
	if err != nil {
		err = eh.Errorf("fsbroker: mint handle uuid: %w", err)
		return
	}
	uuid = hex.EncodeToString(b[:])
	return
}

// handleWatch starts a streaming watch on the handle's path. Two modes may
// watch: a HandleModeWatch handle (minted via fs.dialog.watch — the user
// picked a DIRECTORY to observe) watches its path as granted, and a
// HandleModeRead handle watches ITS OWN FILE — the seam that lets an editor
// follow the document it has open. The file case routes through
// fileWatchBackend: the parent directory is watched and the stream is
// filtered to the one file broker-side, because the app holds no path and a
// read grant must not leak sibling names. On success the broker replies with
// a WatchReply naming the event subject and spawns a pump goroutine that
// publishes each backend event to fs.handle.{uuid}.event. Idempotent at the
// handle level — a second watch request for an already-watching handle
// replies Started=false.
func (inst *Service) handleWatch(msg *app.Msg, h *handle) {
	if h.mode != HandleModeWatch && h.mode != HandleModeRead {
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
	var backend watcherBackendI
	var backendName string
	if h.mode == HandleModeRead {
		backend, backendName, err = newFileWatchBackend(h.path, req)
	} else {
		backend, backendName, err = pickBackend(h.path, req)
	}
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
	// Re-check under the insert lock: a concurrent watch on the same handle,
	// or a close, may have landed while the backend was being built. A
	// backend no map holds would never be stopped, so the loser stops its own.
	inst.mu.Lock()
	_, already = inst.watches[h.uuid]
	cur, live := inst.handles[h.uuid]
	if !already && live && cur == h {
		inst.watches[h.uuid] = w
	}
	inst.mu.Unlock()
	if already || !live || cur != h {
		// Drain as the pump would, so a backend whose forwarder blocks on
		// send (fileWatchBackend) can still close its stream.
		backend.Stop()
		go func() {
			for range backend.Events() {
			}
		}()
		reason := "watch already active"
		if !already {
			reason = "handle closed"
		}
		_ = inst.replyWatch(msg.Reply, WatchReply{Started: false, Reason: reason})
		return
	}
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
