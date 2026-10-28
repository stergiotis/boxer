// Package lane runs agent-caused watchbill jobs in a process of their own
// (ADR-0344 §SD4). The host's enqueue service routes an attested
// agent-caused job onto the reserved queue (§SD3); the coordinator here
// watches that queue and, for each queued row, starts one child of the
// running executable with `watchbill run --job <id>`, bounded by a pool
// of slots. The child is a run: it claims the row by id, runs the job
// under the agentic entry frame, writes the row's transitions and its own
// coverage, and exits. The coordinator keeps the host's side: the keelson
// task the window watches, the notices for a failed or discarded job, the
// changed announcement, and the child's lines in the host's log.
//
// Coordination is the job table's and the heartbeat's, as everywhere in
// watchbill: the coordinator needs no bus to be correct. A child that dies
// leaves a running row whose heartbeat goes stale; the sweep requeues or
// abandons it, and a requeued row is seen again here.
package lane

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/procpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// AppId is the coordinator's identity on the bus.
const AppId app.AppIdT = "runtime.watchbill.lane"

// Caps is the coordinator's bus capability set: hear the changed
// announcements (an enqueue onto the lane is one), announce the rows it
// settles on a child's behalf, and produce the tasks the window watches.
func Caps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: watchbill.SubjectChanged, Direction: app.CapDirectionSub, Reason: "watchbill lane: hear a row land on the lane"},
		{Pattern: watchbill.SubjectChanged, Direction: app.CapDirectionPub, Reason: "watchbill lane: announce a row a child settled"},
	}
	caps = append(caps, task.ProducerCaps()...)
	return
}

// Config parameterises a [Coordinator].
type Config struct {
	// Store is the job table; required.
	Store watchbill.StoreI
	// Launcher starts a child for a job; required.
	Launcher LauncherI
	// RunId is the host's run, the owner run of the tasks it spawns.
	RunId string
	// Bus is where tasks are reported and rows announced; nil does neither.
	Bus app.BusI
	// Notices is told a failed or discarded job, the way the in-process
	// worker tells it (ADR-0323 §SD7); nil tells nothing. SetNotices
	// installs one after Start, since the host builds the attention
	// service after the worker.
	Notices watchbill.NoticesI
	// MaxChildren bounds the children alive at once; zero is
	// watchbill.LaneChildren's.
	MaxChildren uint8
	// Poll is the lane read interval when no announcement arrives; zero is
	// the worker's poll default.
	Poll time.Duration
	// KillGrace is the time a child has after SIGTERM on Stop; zero is
	// watchbill.LaneKillGrace's.
	KillGrace time.Duration
	// ReadLimit bounds one lane read; zero is 64.
	ReadLimit int
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Log is the coordinator's logger; zero writes nowhere.
	Log zerolog.Logger
}

// slot is what the pool counts: a child's place. It is ready the moment
// it is made; the coordinator starts the process itself once it holds
// the slot, so MaxConcurrent bounds live children through their whole
// teardown (ADR-0285 §SD1).
type slot struct {
	release procpool.ReleaseFunc
	once    sync.Once
}

func (inst *slot) Close() (err error) {
	inst.once.Do(func() {
		if inst.release != nil {
			inst.release()
		}
	})
	return
}

type slotSpawner struct{}

func (slotSpawner) Spawn(_ context.Context, release procpool.ReleaseFunc) (w *slot, err error) {
	return &slot{release: release}, nil
}

// run is one child in flight.
type run struct {
	job    watchbillstore.Job
	child  ChildI
	slot   *slot
	handle task.HandleI
	cancel context.CancelFunc
}

// Coordinator drains the lane into children.
type Coordinator struct {
	cfg  Config
	now  func() time.Time
	log  zerolog.Logger
	pool *procpool.Pool[*slot]

	mu      sync.Mutex
	running map[string]*run
	wake    chan struct{}
	unsub   func()
	cancel  context.CancelFunc
	done    chan struct{}
	wg      sync.WaitGroup
	// Spawned counts children started, for tests and the status line.
	spawned uint64
	notices atomic.Pointer[noticesRef]
}

type noticesRef struct{ n watchbill.NoticesI }

// SetNotices installs the listener told of a failed or discarded job a
// child ran; nil removes it.
func (inst *Coordinator) SetNotices(n watchbill.NoticesI) {
	if n == nil {
		inst.notices.Store(nil)
		return
	}
	inst.notices.Store(&noticesRef{n: n})
}

// New validates cfg and builds the coordinator; Start runs it.
func New(cfg Config) (inst *Coordinator, err error) {
	if cfg.Store == nil {
		return nil, eh.Errorf("lane: Config.Store is required")
	}
	if cfg.Launcher == nil {
		return nil, eh.Errorf("lane: Config.Launcher is required")
	}
	if cfg.MaxChildren == 0 {
		n := watchbill.LaneChildren.Get()
		if n < 1 || n > 255 {
			return nil, eb.Build().Int64("children", n).Errorf("lane: KEELSON_WATCHBILL_LANE_CHILDREN out of range")
		}
		cfg.MaxChildren = uint8(n)
	}
	if cfg.Poll <= 0 {
		cfg.Poll = watchbill.Poll.Get()
	}
	if cfg.KillGrace <= 0 {
		cfg.KillGrace = watchbill.LaneKillGrace.Get()
	}
	if cfg.ReadLimit <= 0 {
		cfg.ReadLimit = 64
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	pool, err := procpool.New(procpool.Config{MaxConcurrent: cfg.MaxChildren}, slotSpawner{}, cfg.Log)
	if err != nil {
		return nil, eh.Errorf("lane: pool: %w", err)
	}
	inst = &Coordinator{cfg: cfg, now: cfg.Now, log: cfg.Log, pool: pool, running: map[string]*run{}, wake: make(chan struct{}, 1)}
	inst.SetNotices(cfg.Notices)
	return
}

// Start begins watching the lane; Stop ends it.
func (inst *Coordinator) Start(ctx context.Context) (err error) {
	if inst.cfg.Bus != nil {
		inst.unsub, err = inst.cfg.Bus.Subscribe(watchbill.SubjectChanged, func(*app.Msg) { inst.Wake() })
		if err != nil {
			return eh.Errorf("lane: subscribe changed: %w", err)
		}
	}
	ctx, inst.cancel = context.WithCancel(ctx)
	inst.done = make(chan struct{})
	go inst.loop(ctx)
	inst.log.Info().Uint8("children", inst.cfg.MaxChildren).Dur("poll", inst.cfg.Poll).Msg("watchbill lane: coordinator started; agent-caused jobs run in children of this executable")
	return
}

// Wake asks for a lane read now.
func (inst *Coordinator) Wake() {
	select {
	case inst.wake <- struct{}{}:
	default:
	}
}

// Stop ends the loop and the children: SIGTERM to each group, then
// SIGKILL after the grace. Their rows stay running for the sweep, since a
// killed child is a dead run to every other worker.
func (inst *Coordinator) Stop() {
	if inst.cancel == nil {
		return
	}
	if inst.unsub != nil {
		inst.unsub()
	}
	inst.cancel()
	<-inst.done
	inst.mu.Lock()
	children := make([]*run, 0, len(inst.running))
	for _, r := range inst.running {
		children = append(children, r)
	}
	inst.mu.Unlock()
	for _, r := range children {
		r.child.Terminate()
	}
	for _, r := range children {
		waitOrKill(r.child, inst.cfg.KillGrace)
	}
	inst.wg.Wait()
	sctx, cancel := context.WithTimeout(context.Background(), inst.cfg.KillGrace+time.Second)
	defer cancel()
	_ = inst.pool.Stop(sctx)
}

// Running lists the job ids with a child in flight, sorted by nothing.
func (inst *Coordinator) Running() (ids []string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for id := range inst.running {
		ids = append(ids, id)
	}
	return
}

// Spawned is how many children the coordinator has started.
func (inst *Coordinator) Spawned() (n uint64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.spawned
}

func (inst *Coordinator) loop(ctx context.Context) {
	defer close(inst.done)
	ticker := time.NewTicker(inst.cfg.Poll)
	defer ticker.Stop()
	for {
		if err := inst.Tick(ctx); err != nil && ctx.Err() == nil {
			inst.log.Warn().Err(err).Msg("watchbill lane: read")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-inst.wake:
		}
	}
}

// Tick is one pass: read the lane's queued, due rows and start a child
// for each that has none, while a slot is free. Exposed so a test drives
// it without a clock.
func (inst *Coordinator) Tick(ctx context.Context) (err error) {
	now := inst.now().UTC()
	jobs, err := inst.cfg.Store.List(ctx, watchbillstore.ListFilter{
		States: []string{watchbillstore.StateQueued}, Queues: []string{watchbillstore.QueueLane},
	}, inst.cfg.ReadLimit)
	if err != nil {
		return eh.Errorf("lane read: %w", err)
	}
	for _, job := range jobs {
		if job.RunAfter.After(now) {
			continue
		}
		inst.mu.Lock()
		_, busy := inst.running[job.ID]
		inst.mu.Unlock()
		if busy {
			continue
		}
		st := inst.pool.Stats()
		if st.Stopped || st.Live+st.PendingSpawns >= int(inst.cfg.MaxChildren) {
			return
		}
		if serr := inst.spawn(ctx, job); serr != nil {
			inst.log.Warn().Err(serr).Str("job", job.ID).Msg("watchbill lane: child not started; the row stays queued for the next read")
		}
	}
	return
}

// spawn takes a slot, reports the task, and starts the child for job.
func (inst *Coordinator) spawn(ctx context.Context, job watchbillstore.Job) (err error) {
	s, err := inst.pool.Acquire(ctx)
	if err != nil {
		return eh.Errorf("slot: %w", err)
	}
	handle := inst.spawnTask(ctx, job)
	child, err := inst.cfg.Launcher.Start(ctx, job)
	if err != nil {
		_ = handle.Error(err, "child not started")
		_ = s.Close()
		return
	}
	rctx, cancel := context.WithCancel(ctx)
	r := &run{job: job, child: child, slot: s, handle: handle, cancel: cancel}
	inst.mu.Lock()
	inst.running[job.ID] = r
	inst.spawned++
	inst.mu.Unlock()
	handle.Note("running in " + child.Describe())
	inst.log.Info().Str("job", job.ID).Str("kind", job.Kind).Str("task", job.Task).Str("child", child.Describe()).Msg("watchbill lane: child started")
	inst.wg.Add(2)
	go inst.relayCancel(rctx, r)
	go inst.await(r)
	return
}

// spawnTask reports the child's job as a keelson task of the host's,
// since the child has no bus to report on (ADR-0344 §SD4, E1); without a
// bus it is a handle that reports to no one.
func (inst *Coordinator) spawnTask(ctx context.Context, job watchbillstore.Job) (h task.HandleI) {
	if inst.cfg.Bus == nil {
		return noopHandle{ctx: ctx, id: task.TaskIdT(job.ID)}
	}
	logger := inst.log
	h, err := task.Spawn(ctx, inst.cfg.Bus, task.SpawnOpts{
		Id: task.TaskIdT(job.ID), Kind: job.Kind, Title: job.Kind + " " + job.Subject,
		OwnerAppId: app.AppIdT(job.OwnerAppId), OwnerRunId: inst.cfg.RunId,
		Cancellable: true, Logger: &logger,
	})
	if err != nil {
		inst.log.Warn().Err(err).Str("job", job.ID).Msg("watchbill lane: task not spawned; the job runs unwatched")
		return noopHandle{ctx: ctx, id: task.TaskIdT(job.ID)}
	}
	return h
}

// relayCancel turns a cancel of the host-side task into a cancel request
// on the row, which the child honours at its poll.
func (inst *Coordinator) relayCancel(ctx context.Context, r *run) {
	defer inst.wg.Done()
	select {
	case <-r.child.Done():
		return
	case <-ctx.Done():
		return
	case <-r.handle.Ctx().Done():
	}
	// The task's context also ends when the coordinator stops, since the
	// task was spawned under its context; a stop writes no cancel — the
	// child is signalled and its row is left running for the sweep.
	if ctx.Err() != nil {
		return
	}
	select {
	case <-r.child.Done():
		return
	default:
	}
	cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if ok, err := watchbill.RequestCancel(cctx, inst.cfg.Store, r.job.ID, string(AppId), "cancelled through the task", inst.now()); err != nil {
		inst.log.Warn().Err(err).Str("job", r.job.ID).Msg("watchbill lane: cancel not written")
	} else if ok {
		inst.log.Info().Str("job", r.job.ID).Msg("watchbill lane: cancel requested for a child's job")
	}
}

// await waits for the child, then settles the host's side from the row:
// the task's terminal state, the notices, the announcement.
func (inst *Coordinator) await(r *run) {
	defer inst.wg.Done()
	<-r.child.Done()
	r.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	row, found, err := inst.cfg.Store.Get(ctx, r.job.ID)
	exitErr := r.child.Err()
	ev := inst.log.Info()
	if exitErr != nil {
		ev = inst.log.Warn().Err(exitErr)
	}
	switch {
	case err != nil:
		ev.Str("job", r.job.ID).Msg("watchbill lane: child exited; the row could not be read")
		_ = r.handle.Error(err, "row unreadable after the child exited")
	case !found:
		ev.Str("job", r.job.ID).Msg("watchbill lane: child exited; the row is gone")
		_ = r.handle.Error(eh.Errorf("job row gone"), "row gone")
	default:
		ev.Str("job", r.job.ID).Str("state", row.State).Str("child", r.child.Describe()).Msg("watchbill lane: child exited")
		inst.settleTask(r.handle, row, exitErr)
		if row.State == watchbillstore.StateFailed || row.State == watchbillstore.StateDiscarded {
			if ref := inst.notices.Load(); ref != nil && ref.n != nil {
				ref.n.JobEnded(inst.now(), row, row.State, row.LastError)
			}
		}
	}
	if inst.cfg.Bus != nil {
		if perr := inst.cfg.Bus.Publish(watchbill.SubjectChanged, []byte(r.job.ID)); perr != nil {
			inst.log.Debug().Err(perr).Str("job", r.job.ID).Msg("watchbill lane: changed not announced")
		}
	}
	inst.mu.Lock()
	delete(inst.running, r.job.ID)
	inst.mu.Unlock()
	_ = r.slot.Close()
}

// settleTask ends the host-side task from the row the child left.
func (inst *Coordinator) settleTask(h task.HandleI, row watchbillstore.Job, exitErr error) {
	switch row.State {
	case watchbillstore.StateSucceeded:
		_ = h.Done(nil)
	case watchbillstore.StateRunning:
		// The child died with the claim: the sweep will requeue or
		// abandon the row once its heartbeat is stale.
		reason := "child exited without settling the row"
		if exitErr == nil {
			exitErr = errors.New(reason)
		}
		_ = h.Error(exitErr, reason)
	case watchbillstore.StateQueued:
		// A lost claim, or a failed attempt already requeued.
		_ = h.Error(eh.Errorf("job requeued: %s", row.LastError), "requeued")
	default:
		msg := row.LastError
		if msg == "" {
			msg = row.State
		}
		_ = h.Error(eb.Build().Str("state", row.State).Errorf("%s", msg), row.State)
	}
}

// noopHandle reports to no one; the job's context is the child's lifetime.
type noopHandle struct {
	ctx context.Context
	id  task.TaskIdT
}

func (inst noopHandle) Id() (id task.TaskIdT)            { return inst.id }
func (inst noopHandle) Ctx() (ctx context.Context)       { return inst.ctx }
func (inst noopHandle) Cancelled() (b bool)              { return inst.ctx.Err() != nil }
func (inst noopHandle) Report(task.ProgressReport)       {}
func (inst noopHandle) Note(string)                      {}
func (inst noopHandle) Done([]byte) (err error)          { return }
func (inst noopHandle) Error(error, string) (rerr error) { return }
