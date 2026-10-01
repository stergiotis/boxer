package windowhost

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The window host's side of the app operations contract (ADR-0269 §SD4):
// one opengine.Engine per window whose app serves a catalog, driven at fixed
// points of the window's frame, the subscription on the operation subjects,
// and what the dispatcher asks of the host directly — status, cancel, expiry,
// attachment and capture.

// opsRepaintIntervalSecs bounds how long a queued command waits for a frame
// while a task is attached or a command is queued (ADR-0269 §SD4).
const opsRepaintIntervalSecs = 0.1

// captureTimeout bounds how long a capture waits for its file.
const captureTimeout = 5 * time.Second

// startOps creates the window's engine after Mount, when the registered
// manifest keeps a catalog and the instance serves it.
func (w *window) startOps(logger zerolog.Logger) {
	if w.opsTried {
		return
	}
	w.opsTried = true
	if w.manifest.Operations == nil {
		return
	}
	oa, ok := w.appInst.(app.OperationsAppI)
	if !ok {
		logger.Warn().Str("id", string(w.manifest.Id)).
			Msg("windowhost: the manifest declares operations but the instance does not implement app.OperationsAppI")
		return
	}
	h := oa.Operations()
	if h == nil {
		return
	}
	eng := opengine.New(w.manifest.Operations, h)
	w.ops.Store(eng)
	w.frameCtx.SetOperationsGesture(eng.Gesture)
}

// beginOps runs the engine's command stage: after the previous frame's
// write-back, before the app's Frame.
func (w *window) beginOps() {
	if eng := w.ops.Load(); eng != nil {
		eng.BeginFrame()
		eng.ApplyQueued()
		eng.TakeSnapshot()
	}
}

// endOps attributes what the app's Frame changed.
func (w *window) endOps() {
	if eng := w.ops.Load(); eng != nil {
		eng.EndFrame()
	}
}

// opsBusy reports whether any window's engine wants frames soon.
func opsBusy(windows []*window) (busy bool) {
	for _, w := range windows {
		if eng := w.ops.Load(); eng != nil && eng.Busy() {
			return true
		}
	}
	return
}

// engineOf returns the engine of the window keyed key whose app's subject
// alias is alias; alias "" matches any app.
func (inst *Inst) engineOf(alias string, key uint64) (eng *opengine.Engine, w *window) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, cand := range inst.windows {
		if uint64(cand.key) != key || cand.closeReq {
			continue
		}
		if alias != "" && cand.manifest.Id.SubjectAlias() != alias {
			return
		}
		return cand.ops.Load(), cand
	}
	return
}

// OpsInstances lists the open windows for the dispatcher.
func (inst *Inst) OpsInstances() (out []opwire.InstanceInfo) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, w := range inst.windows {
		if w.closeReq {
			continue
		}
		out = append(out, opwire.InstanceInfo{
			App: w.manifest.Id, Alias: w.manifest.Id.SubjectAlias(), Key: uint64(w.key),
			Title: w.manifest.WindowTitle(), Ops: w.ops.Load() != nil,
		})
	}
	return
}

// OpsRenderGoroutine is the id of the goroutine that last ran Frame; zero
// before the first frame.
func (inst *Inst) OpsRenderGoroutine() (id uint64) { return inst.renderGoroutine.Load() }

// OpsStatus returns a call's outcome by call id.
func (inst *Inst) OpsStatus(key uint64, callId string) (out opwire.Outcome, ok bool) {
	if eng, _ := inst.engineOf("", key); eng != nil {
		out, ok = eng.Status(callId)
	}
	return
}

// OpsCancel withdraws a queued command.
func (inst *Inst) OpsCancel(key uint64, callId string) (out opwire.Outcome, ok bool) {
	if eng, _ := inst.engineOf("", key); eng != nil {
		out, ok = eng.Cancel(callId)
	}
	return
}

// OpsExpire ends queued commands of a window; every one when ids is empty.
func (inst *Inst) OpsExpire(key uint64, ids []string, reason string) {
	if eng, _ := inst.engineOf("", key); eng != nil {
		eng.Expire(ids, reason)
	}
}

// OpsAttach counts a task attached to a window, or detached from it.
func (inst *Inst) OpsAttach(key uint64, attached bool) (ok bool) {
	eng, _ := inst.engineOf("", key)
	if eng == nil {
		return
	}
	eng.SetAttached(attached)
	ok = true
	return
}

// OpsLog returns a window's command log.
func (inst *Inst) OpsLog(key uint64) (entries []opengine.LogEntry, ok bool) {
	eng, _ := inst.engineOf("", key)
	if eng == nil {
		return
	}
	entries, ok = eng.Log(), true
	return
}

// --- capture -------------------------------------------------------------

type captureJob struct {
	id        string
	key       WindowKeyT
	path      string
	requested time.Time
	exported  bool
	status    opwire.CaptureStatus
}

type captures struct {
	mu   sync.Mutex
	dir  string
	jobs map[string]*captureJob
	// pending are jobs whose export the next Frame queues.
	pending []*captureJob
}

// OpsCapture queues a capture of a window's content as SVG; the export runs
// in the next frame and Status follows the file. Only an open window can be
// captured (ADR-0269 §SD11).
func (inst *Inst) OpsCapture(key uint64) (job string, err error) {
	if _, w := inst.engineOf("", key); w == nil {
		err = eh.Errorf("windowhost: no open window by that key")
		return
	}
	inst.caps.mu.Lock()
	defer inst.caps.mu.Unlock()
	if inst.caps.dir == "" {
		inst.caps.dir, err = os.MkdirTemp("", "boxer-agent-captures-")
		if err != nil {
			err = eh.Errorf("windowhost: capture directory: %w", err)
			return
		}
		inst.caps.jobs = make(map[string]*captureJob)
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	job = "cap-" + hex.EncodeToString(b[:])
	j := &captureJob{id: job, key: WindowKeyT(key), path: filepath.Join(inst.caps.dir, job+".svg"), requested: time.Now(),
		status: opwire.CaptureStatus{Phase: opwire.PhaseRunning, MediaType: "image/svg+xml"}}
	inst.caps.jobs[job] = j
	inst.caps.pending = append(inst.caps.pending, j)
	return
}

// OpsCaptureStatus reports a capture by job id.
func (inst *Inst) OpsCaptureStatus(job string) (st opwire.CaptureStatus, ok bool) {
	inst.caps.mu.Lock()
	defer inst.caps.mu.Unlock()
	j, ok := inst.caps.jobs[job]
	if !ok {
		return
	}
	if j.status.Phase == opwire.PhaseRunning && j.exported {
		if fi, err := os.Stat(j.path); err == nil && fi.Size() > 0 {
			j.status = opwire.CaptureStatus{Phase: opwire.PhaseCompleted, Path: j.path, MediaType: "image/svg+xml", Bytes: fi.Size()}
		} else if time.Since(j.requested) > captureTimeout {
			j.status = opwire.CaptureStatus{Phase: opwire.PhaseFailed, Reason: "the export did not arrive in time"}
		}
	}
	st = j.status
	return
}

// runCaptures queues the pending exports on the render goroutine, after
// the windows drew, so the export captures what the person sees. ids is the
// host's stack, at the state the windows were emitted under.
func (inst *Inst) runCaptures(ids *c.WidgetIdStack, open map[WindowKeyT]bool) {
	inst.caps.mu.Lock()
	pending := inst.caps.pending
	inst.caps.pending = nil
	inst.caps.mu.Unlock()
	for i, j := range pending {
		if !open[j.key] {
			inst.caps.mu.Lock()
			j.status = opwire.CaptureStatus{Phase: opwire.PhaseRefused, Reason: "the window is not open"}
			inst.caps.mu.Unlock()
			continue
		}
		if i > 0 {
			// The export slot holds one request per pass; the rest wait.
			inst.caps.mu.Lock()
			inst.caps.pending = append(inst.caps.pending, pending[i:]...)
			inst.caps.mu.Unlock()
			return
		}
		ids.PrepareStr("window-" + strconv.FormatUint(uint64(j.key), 10))
		h := widgethandle.Make(ids.Derive())
		c.ExportSvgWindow(h, j.path, false, 0, 0x1e1e1eff)
		inst.caps.mu.Lock()
		j.exported = true
		inst.caps.mu.Unlock()
	}
}

// --- the operation subjects ------------------------------------------------

// OpsService answers the operation subjects app.{alias}.{instance}.op.{name}
// for every window of the host. It accepts a call only from the dispatcher's
// identity (ADR-0269 §SD3).
type OpsService struct {
	host      *Inst
	busClient *inprocbus.Client
	unsub     func()
	log       zerolog.Logger
	closeOnce sync.Once
}

// OpsServiceCaps is what the window host's operation service holds.
func OpsServiceCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: opwire.Pattern, Direction: app.CapDirectionSub, Reason: "windowhost: serve operation calls"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "windowhost: reply to inboxes"},
	}
	return
}

// NewOpsService subscribes the window host on the operation subjects. The
// caller MUST invoke Close.
func NewOpsService(bus *inprocbus.Inst, host *Inst, log zerolog.Logger) (s *OpsService, err error) {
	if bus == nil || host == nil {
		err = eh.Errorf("windowhost: ops service needs a bus and a host")
		return
	}
	s = &OpsService{host: host, log: log.With().Str("app", string(opwire.HostOpsAppId)).Logger()}
	s.busClient = bus.NewClient(opwire.HostOpsAppId, OpsServiceCaps())
	s.unsub, err = s.busClient.Subscribe(opwire.Pattern, s.handle)
	if err != nil {
		_ = s.busClient.Close()
		err = eh.Errorf("windowhost: ops subscribe: %w", err)
		return nil, err
	}
	return
}

// Close unsubscribes.
func (inst *OpsService) Close() {
	inst.closeOnce.Do(func() {
		if inst.unsub != nil {
			inst.unsub()
		}
		_ = inst.busClient.Close()
	})
}

func (inst *OpsService) handle(msg *app.Msg) {
	if msg.Sender != opwire.DispatcherAppId {
		inst.log.Warn().Str("sender", string(msg.Sender)).Str("subject", msg.Subject).
			Msg("windowhost: an operation call from someone other than the dispatcher; dropped")
		return
	}
	if msg.Reply == "" {
		return
	}
	rep := opwire.CallReply{V: opwire.WireVersion}
	alias, key, op, ok := opwire.ParseSubject(msg.Subject)
	if !ok {
		rep.Outcome = opwire.Outcome{Phase: opwire.PhaseRefused, Reason: "not an operation subject"}
		inst.reply(msg.Reply, rep)
		return
	}
	eng, w := inst.host.engineOf(alias, key)
	switch {
	case w == nil:
		rep.Outcome = opwire.Outcome{Phase: opwire.PhaseRefused, Reason: "no open window of that app by that key"}
	case eng == nil:
		rep.Outcome = opwire.Outcome{Phase: opwire.PhaseRefused, Reason: "the window serves no catalog"}
	default:
		req, err := buscodec.Decode[opwire.CallRequest](msg.Payload)
		if err != nil {
			rep.Outcome = opwire.Outcome{Phase: opwire.PhaseFailed, Reason: "undecodable call"}
			break
		}
		rep.Outcome = eng.Submit(op, req)
	}
	inst.reply(msg.Reply, rep)
}

func (inst *OpsService) reply(subject string, v opwire.CallReply) {
	if err := buscodec.Reply(inst.busClient.Publish, subject, v); err != nil {
		inst.log.Warn().Err(err).Msg("windowhost: ops reply failed")
	}
}
