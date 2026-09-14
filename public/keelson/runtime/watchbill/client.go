package watchbill

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/watchbillreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/watchbillrequest"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Client is the queue as an app sees it (ADR-0234 §SD1): five verbs over
// a bus client that holds [ClientCaps], answered by whichever worker in
// the process serves `watchbill.job.*`. It is River's Client on the bus:
// Enqueue is Insert, and the rest carry their names. The zero value is
// unusable; take one from [NewClient] with the bus the host minted for
// the app (its MountContextI.Bus()).
type Client struct {
	bus app.BusI
	// Timeout bounds one request; zero is the bus's default.
	Timeout time.Duration
}

// NewClient wraps bus.
func NewClient(bus app.BusI) (inst *Client) {
	inst = &Client{bus: bus}
	return
}

// Enqueue writes req as a queued job and returns its id and the row as
// written. The worker rings its own doorbell, so nothing else is owed.
// OwnerAppId and RequesterRun on req are ignored: the worker attributes
// both from the bus envelope and its own run.
func (inst *Client) Enqueue(req Request) (job watchbillstore.Job, err error) {
	if req.Kind == "" {
		return job, eh.Errorf("watchbill: enqueue without a kind")
	}
	wire := watchbillrequest.WatchbillRequest{
		Op: watchbillrequest.OpEnqueue, Id: req.ID, Kind: req.Kind, Subject: req.Subject, Queue: req.Queue,
		Priority: req.Priority, MaxAttempts: req.MaxAttempts, Backoff: req.Backoff,
		BackoffBaseMs: uint64(req.BackoffBase / time.Millisecond), TimeoutMs: uint64(req.Timeout / time.Millisecond),
		ArgsKind: req.ArgsKind, Args: req.Args,
	}
	if !req.RunAfter.IsZero() {
		wire.RunAfterMs = req.RunAfter.UnixMilli()
	}
	r, err := inst.call(wire)
	if err != nil {
		return
	}
	if !r.Ok {
		return job, eh.Errorf("%s", r.Reason)
	}
	jobs := JobsOf(r)
	if len(jobs) != 1 {
		return job, eh.Errorf("watchbill: enqueue reply carries %d jobs", len(jobs))
	}
	job = jobs[0]
	return
}

// Cancel asks for the job to stop, with a note for the event. ok is false
// when the job is in a state a cancel does not apply to, or does not
// exist; err is a transport or store failure.
func (inst *Client) Cancel(id string, note string) (ok bool, err error) {
	return inst.verb(watchbillrequest.OpCancel, id, note)
}

// Retry puts a finished job back in the queue with its attempts reset.
func (inst *Client) Retry(id string, note string) (ok bool, err error) {
	return inst.verb(watchbillrequest.OpRetry, id, note)
}

func (inst *Client) verb(op string, id string, note string) (ok bool, err error) {
	if id == "" {
		return false, eh.Errorf("watchbill: %s without a job id", op)
	}
	r, err := inst.call(watchbillrequest.WatchbillRequest{Op: op, Id: id, Note: note})
	if err != nil {
		return
	}
	ok = r.Ok
	return
}

// Get reads one job.
func (inst *Client) Get(id string) (job watchbillstore.Job, found bool, err error) {
	if id == "" {
		return job, false, eh.Errorf("watchbill: get without a job id")
	}
	r, err := inst.call(watchbillrequest.WatchbillRequest{Op: watchbillrequest.OpGet, Id: id})
	if err != nil {
		return
	}
	jobs := JobsOf(r)
	if !r.Ok || len(jobs) == 0 {
		return job, false, nil
	}
	return jobs[0], true, nil
}

// List reads the jobs in any of states and any of kinds (empty: all), at
// most limit (zero: the worker's bound), newest request first.
func (inst *Client) List(states []string, kinds []string, limit int) (jobs []watchbillstore.Job, err error) {
	r, err := inst.call(watchbillrequest.WatchbillRequest{Op: watchbillrequest.OpList, States: states, Kinds: kinds, Limit: uint32(max(limit, 0))})
	if err != nil {
		return
	}
	if !r.Ok {
		return nil, eh.Errorf("%s", r.Reason)
	}
	jobs = JobsOf(r)
	return
}

func (inst *Client) call(req watchbillrequest.WatchbillRequest) (r watchbillreply.WatchbillReply, err error) {
	if inst == nil || inst.bus == nil {
		return r, eh.Errorf("watchbill: client without a bus")
	}
	req.At = time.Now().UTC()
	payload, err := buscodec.Encode(req)
	if err != nil {
		return r, eh.Errorf("encode request: %w", err)
	}
	raw, err := inst.bus.RequestWithTimeout(SubjectJob(req.Op), payload, inst.Timeout)
	if err != nil {
		return r, eb.Build().Str("op", req.Op).Errorf("watchbill request: %w", err)
	}
	if r, err = buscodec.Decode[watchbillreply.WatchbillReply](raw); err != nil {
		return r, eh.Errorf("decode reply: %w", err)
	}
	return
}
