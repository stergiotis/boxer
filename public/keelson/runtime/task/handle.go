//go:build llm_generated_opus47

package task

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskdone"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskerror"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskprogress"
	"github.com/stergiotis/boxer/public/keelson/runtime/task/estimator"
)

// HandleI is the producer-side contract returned by Spawn. Implementations
// are safe for concurrent use — a worker goroutine calling Report and a
// UI goroutine calling Note is the canonical pattern.
//
// Lifecycle: Created at Spawn → Report/Note may run any number of times
// → exactly one of Done/Error is called → handle is terminal. After
// Done/Error, further Report/Note/Done/Error calls return without
// publishing (idempotent), and Ctx().Done() is closed.
type HandleI interface {
	// Id returns the task identifier — same value as appears in subject
	// paths task.<id>.<verb>.
	Id() (id TaskIdT)

	// Ctx returns a context.Context that cancels when:
	//   - the parent context passed to Spawn cancels;
	//   - a task.<id>.cancel message arrives on the bus;
	//   - the handle reaches a terminal state (Done/Error).
	// Worker loops poll Ctx().Done() (or call Cancelled() as a
	// convenience) and exit promptly.
	Ctx() (ctx context.Context)

	// Cancelled is a shorthand for Ctx().Err() != nil.
	Cancelled() (b bool)

	// Report submits a progress sample. Publication is gated by the
	// estimator's humanized-change rule plus a 1 Hz heartbeat for
	// indeterminate tasks; callers should not rate-limit upstream.
	Report(p ProgressReport)

	// Note submits a text-only progress update. Same emission gate as
	// Report; useful for tasks whose progress is qualitative
	// ("connecting…", "negotiating tls…").
	Note(note string)

	// Done publishes the terminal-success message with an opaque result.
	// Pass nil result for tasks whose outcome is the side effect itself
	// (a file was written, a row was inserted). Idempotent.
	Done(result []byte) (err error)

	// Error publishes the terminal-failure message. err is encoded via
	// the boxer eh.MarshalError chain so errorview renders it directly;
	// reason is a short human label that surfaces in observer lists.
	// Idempotent.
	Error(err error, reason string) (rerr error)
}

// Handle is the concrete HandleI returned by Spawn. Exported because tests
// poke at internal state via package-private helpers; consumers should
// program against HandleI.
type Handle struct {
	id         TaskIdT
	kind       string
	ownerAppId app.AppIdT
	bus        app.BusI
	now        func() time.Time

	ctx               context.Context
	cancel            context.CancelFunc
	unsubscribeCancel func()

	// logger is the producer-side diagnostic logger. Pre-contextualised
	// with task_id; the upstream MountContextI.Tasks() also adds run_id
	// / app_id / instance_id. Zero value writes nowhere.
	logger zerolog.Logger

	mu          sync.Mutex
	est         *estimator.Inst
	lastHuman   string
	lastEmitMs  int64
	lastReport  *taskprogress.TaskProgress
	terminated  bool
	indetMode   bool
	heartbeatMs int64
}

var _ HandleI = (*Handle)(nil)

// DefaultIndeterminateHeartbeatMs is the minimum emit cadence for
// indeterminate-mode tasks (Total=0). A no-change Report call still
// publishes at this cadence so observers know the task is alive.
const DefaultIndeterminateHeartbeatMs int64 = 1_000

func (inst *Handle) Id() (id TaskIdT) {
	id = inst.id
	return
}

func (inst *Handle) Ctx() (ctx context.Context) {
	ctx = inst.ctx
	return
}

func (inst *Handle) Cancelled() (b bool) {
	b = inst.ctx.Err() != nil
	return
}

func (inst *Handle) Report(p ProgressReport) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.terminated {
		return
	}

	nowNs := inst.now().UnixNano()
	nowMs := nowNs / 1_000_000
	inst.est.Add(p.Current, nowMs)
	thr := inst.est.ThroughputPerSec()
	eta := inst.est.EtaMs(p.Current, p.Total)
	human := estimator.Humanize(p.Current, p.Total, estimator.UnitE(p.Unit), thr, eta)
	if p.Note != "" {
		human = human + " · " + p.Note
	}

	progress := taskprogress.TaskProgress{
		TaskId:           string(inst.id),
		Current:          p.Current,
		Total:            p.Total,
		Unit:             p.Unit.String(),
		ThroughputPerSec: thr,
		EtaMs:            eta,
		Note:             p.Note,
		AtNs:             nowNs,
	}
	inst.lastReport = &progress
	inst.indetMode = p.Total == 0

	// Emission gate: publish on humanized-change. Indeterminate-mode
	// tasks emit at least every heartbeatMs even when the visible
	// string would not change, so observers see "still alive".
	if human == inst.lastHuman {
		if !inst.indetMode {
			return
		}
		if nowMs-inst.lastEmitMs < inst.heartbeatMs {
			return
		}
	}

	inst.publishProgress(progress)
	inst.lastHuman = human
	inst.lastEmitMs = nowMs
}

func (inst *Handle) Note(note string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.terminated {
		return
	}
	nowNs := inst.now().UnixNano()
	nowMs := nowNs / 1_000_000
	progress := taskprogress.TaskProgress{
		TaskId: string(inst.id),
		Note:   note,
		AtNs:   nowNs,
	}
	if inst.lastReport != nil {
		progress.Current = inst.lastReport.Current
		progress.Total = inst.lastReport.Total
		progress.Unit = inst.lastReport.Unit
		progress.ThroughputPerSec = inst.lastReport.ThroughputPerSec
		progress.EtaMs = inst.lastReport.EtaMs
	}
	inst.lastReport = &progress

	human := note
	if human == inst.lastHuman {
		if nowMs-inst.lastEmitMs < inst.heartbeatMs {
			return
		}
	}
	inst.publishProgress(progress)
	inst.lastHuman = human
	inst.lastEmitMs = nowMs
}

func (inst *Handle) Done(result []byte) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.terminated {
		return
	}
	inst.flushPendingReportLocked()
	nowNs := inst.now().UnixNano()
	inst.logger.Debug().Int("resultBytes", len(result)).Msg("task done")
	d := taskdone.TaskDone{
		TaskId: string(inst.id),
		AtNs:   nowNs,
		Result: result,
	}
	var b []byte
	b, err = MarshalTaskDone(d)
	if err != nil {
		err = eh.Errorf("task: handle done: %w", err)
		inst.finishLocked()
		return
	}
	err = inst.bus.Publish(SubjectDone(inst.id), b)
	if err != nil {
		err = eh.Errorf("task: publish done: %w", err)
	}
	inst.finishLocked()
	return
}

func (inst *Handle) Error(taskErr error, reason string) (rerr error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.terminated {
		return
	}
	inst.flushPendingReportLocked()
	nowNs := inst.now().UnixNano()
	inst.logger.Warn().Err(taskErr).Str("reason", reason).Msg("task error")
	e := taskerror.TaskError{
		TaskId: string(inst.id),
		AtNs:   nowNs,
		Reason: reason,
	}
	if taskErr != nil {
		// Wire shape v1: plain-text rendering via FormatErrorWithStackS.
		// boxer's structured error chain is zerolog-CBOR-shaped (the
		// project's binary_log build tag swaps zerolog into CBOR mode),
		// so a JSON-on-the-wire variant would require either project-
		// wide CBOR consumers or a fragile encoder-mode toggle. Until
		// either lands the task primitive carries the multi-line text
		// rep — readable in the supervisor's audit row, displayed as
		// a collapsing block in the M4 taskmonitor widget.
		e.ErrorText = eh.FormatErrorWithStackS(taskErr)
		if e.Reason == "" {
			e.Reason = taskErr.Error()
		}
	}
	var b []byte
	b, rerr = MarshalTaskError(e)
	if rerr != nil {
		rerr = eh.Errorf("task: handle error: %w", rerr)
		inst.finishLocked()
		return
	}
	rerr = inst.bus.Publish(SubjectError(inst.id), b)
	if rerr != nil {
		rerr = eh.Errorf("task: publish error: %w", rerr)
	}
	inst.finishLocked()
	return
}

// flushPendingReportLocked publishes the most recent Report payload if it
// was held back by the emission gate. Called once on Done/Error so the
// final progress state is always visible to observers.
func (inst *Handle) flushPendingReportLocked() {
	if inst.lastReport == nil {
		return
	}
	lastMs := inst.lastReport.AtNs / 1_000_000
	if lastMs == inst.lastEmitMs {
		return
	}
	inst.publishProgress(*inst.lastReport)
	inst.lastEmitMs = lastMs
}

func (inst *Handle) publishProgress(progress taskprogress.TaskProgress) {
	b, err := MarshalTaskProgress(progress)
	if err != nil {
		return
	}
	_ = inst.bus.Publish(SubjectProgress(inst.id), b)
}

func (inst *Handle) finishLocked() {
	inst.terminated = true
	if inst.unsubscribeCancel != nil {
		inst.unsubscribeCancel()
		inst.unsubscribeCancel = nil
	}
	if inst.cancel != nil {
		inst.cancel()
	}
}
