package watchbill

import (
	"context"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/watchbillreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/watchbillrequest"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// The client protocol is served by the worker (ADR-0234 §SD1): the process
// that holds the store answers `watchbill.job.<op>` on the bus, so an app
// with a bus client and [ClientCaps] reaches the queue without a store
// handle. The five verbs are River's client verbs. Every answer is a
// watchbillreply.WatchbillReply: a refusal is Ok false with a Reason.
//
// Attribution is the envelope's: an enqueue records Msg.Sender as the
// job's owner app, and a cancel or retry names the sender in its event
// note. The acting run on every row is the worker's own, since it is the
// worker that writes.

// requestTimeout bounds one request's store work; a client waits the
// bus's default, and a store that stalls must not hold the handler.
const requestTimeout = 10 * time.Second

// serve subscribes the job requests when there is a bus.
func (inst *Worker) serve() (err error) {
	if inst.cfg.Bus == nil {
		return
	}
	inst.unsubReq, err = inst.cfg.Bus.Subscribe(SubjectJobAll, inst.handleRequest)
	if err != nil {
		err = eh.Errorf("subscribe job requests: %w", err)
	}
	return
}

func (inst *Worker) handleRequest(msg *app.Msg) {
	if msg.Reply == "" {
		inst.log.Warn().Str("subject", msg.Subject).Msg("watchbill: request without reply, dropping")
		return
	}
	op := strings.TrimPrefix(msg.Subject, SubjectJobPrefix)
	req, err := buscodec.Decode[watchbillrequest.WatchbillRequest](msg.Payload)
	if err != nil {
		inst.refuse(msg.Reply, "watchbill: malformed request: "+err.Error())
		return
	}
	if req.Op != "" && req.Op != op {
		inst.refuse(msg.Reply, "watchbill: op in subject ("+op+") and payload ("+req.Op+") disagree")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	var reply watchbillreply.WatchbillReply
	switch op {
	case watchbillrequest.OpEnqueue:
		reply, err = inst.opEnqueue(ctx, msg.Sender, req)
	case watchbillrequest.OpCancel:
		reply, err = inst.opVerb(ctx, msg.Sender, req, RequestCancel)
	case watchbillrequest.OpRetry:
		reply, err = inst.opVerb(ctx, msg.Sender, req, Retry)
	case watchbillrequest.OpGet:
		reply, err = inst.opGet(ctx, req)
	case watchbillrequest.OpList:
		reply, err = inst.opList(ctx, req)
	default:
		inst.refuse(msg.Reply, "watchbill: unknown op: "+op)
		return
	}
	if err != nil {
		inst.log.Warn().Err(err).Str("op", op).Str("sender", string(msg.Sender)).Msg("watchbill: request failed")
		inst.refuse(msg.Reply, err.Error())
		return
	}
	inst.reply(msg.Reply, reply)
}

// opEnqueue writes the row, rings its own doorbell and announces the row,
// so a bus client's enqueue is [Submit] with the announcement the worker
// owes for every row it flushes.
func (inst *Worker) opEnqueue(ctx context.Context, sender app.AppIdT, req watchbillrequest.WatchbillRequest) (reply watchbillreply.WatchbillReply, err error) {
	r := Request{
		Kind: req.Kind, Subject: req.Subject, Queue: req.Queue, Priority: req.Priority,
		MaxAttempts: req.MaxAttempts, Backoff: req.Backoff,
		BackoffBase: time.Duration(req.BackoffBaseMs) * time.Millisecond,
		Timeout:     time.Duration(req.TimeoutMs) * time.Millisecond,
		OwnerAppId:  sender, RequesterRun: inst.cfg.RunId,
		ArgsKind: req.ArgsKind, Args: req.Args, ID: req.Id,
	}
	if req.RunAfterMs > 0 {
		r.RunAfter = time.UnixMilli(req.RunAfterMs).UTC()
	}
	if r.Backoff != "" && r.Backoff != watchbillstore.BackoffNone && r.Backoff != watchbillstore.BackoffLinear && r.Backoff != watchbillstore.BackoffExponential {
		return reply, eh.Errorf("watchbill: unknown backoff class %q", r.Backoff) //boxer:lint disable=CS013 reason="an op handler's message becomes the reply's Reason string and is the only thing that crosses the bus; a CBOR field stays on the worker"
	}
	id, err := Enqueue(ctx, inst.cfg.Store, r)
	if err != nil {
		return
	}
	inst.Wake()
	inst.announce(id)
	job, found, err := inst.cfg.Store.Get(ctx, id)
	if err != nil {
		return
	}
	if !found {
		return reply, eh.Errorf("watchbill: enqueued row not read back")
	}
	reply = replyOf(inst.now(), true, "", []watchbillstore.Job{job})
	return
}

// opVerb is cancel or retry: the verb on the row, the sender in the
// note, a wake so a cancel of this worker's own job is honoured now, and
// the announcement after the event row.
func (inst *Worker) opVerb(ctx context.Context, sender app.AppIdT, req watchbillrequest.WatchbillRequest, verb func(context.Context, StoreI, string, string, string, time.Time) (bool, error)) (reply watchbillreply.WatchbillReply, err error) {
	if req.Id == "" {
		return reply, eh.Errorf("watchbill: the verb needs a job id")
	}
	note := "asked by " + string(sender)
	if req.Note != "" {
		note += ": " + req.Note
	}
	ok, err := verb(ctx, inst.cfg.Store, req.Id, inst.cfg.RunId, note, inst.now().UTC())
	if err != nil {
		return
	}
	job, found, err := inst.cfg.Store.Get(ctx, req.Id)
	if err != nil {
		return
	}
	if !found {
		return replyOf(inst.now(), false, "watchbill: no such job", nil), nil
	}
	if ok {
		inst.Wake()
		inst.announce(req.Id)
		return replyOf(inst.now(), true, "", []watchbillstore.Job{job}), nil
	}
	return replyOf(inst.now(), false, "watchbill: the job is "+job.State+", which the verb does not apply to", []watchbillstore.Job{job}), nil
}

func (inst *Worker) opGet(ctx context.Context, req watchbillrequest.WatchbillRequest) (reply watchbillreply.WatchbillReply, err error) {
	if req.Id == "" {
		return reply, eh.Errorf("watchbill: get needs a job id")
	}
	job, found, err := inst.cfg.Store.Get(ctx, req.Id)
	if err != nil {
		return
	}
	if !found {
		return replyOf(inst.now(), false, "watchbill: no such job", nil), nil
	}
	return replyOf(inst.now(), true, "", []watchbillstore.Job{job}), nil
}

func (inst *Worker) opList(ctx context.Context, req watchbillrequest.WatchbillRequest) (reply watchbillreply.WatchbillReply, err error) {
	jobs, err := inst.cfg.Store.List(ctx, watchbillstore.ListFilter{States: req.States, Kinds: req.Kinds}, int(req.Limit))
	if err != nil {
		return
	}
	return replyOf(inst.now(), true, "", jobs), nil
}

// announce publishes the changed subject for id; the row is already
// flushed, which is the order the subject promises.
func (inst *Worker) announce(id string) {
	if perr := inst.cfg.Bus.Publish(SubjectChanged, []byte(id)); perr != nil {
		inst.log.Debug().Err(perr).Str("id", id).Msg("watchbill: changed not announced")
	}
}

func (inst *Worker) refuse(replySubject string, reason string) {
	inst.reply(replySubject, replyOf(inst.now(), false, reason, nil))
}

func (inst *Worker) reply(replySubject string, r watchbillreply.WatchbillReply) {
	if err := buscodec.Reply(inst.cfg.Bus.Publish, replySubject, r); err != nil {
		inst.log.Warn().Err(err).Str("reply", replySubject).Msg("watchbill: publish reply")
	}
}

// replyOf lays jobs out as the reply's parallel columns.
func replyOf(at time.Time, ok bool, reason string, jobs []watchbillstore.Job) (r watchbillreply.WatchbillReply) {
	r = watchbillreply.WatchbillReply{At: at.UTC(), Ok: ok, Reason: reason}
	n := len(jobs)
	r.Id, r.Kind, r.Subject, r.Queue = make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	r.Priority, r.Attempt, r.MaxAttempts = make([]uint64, n), make([]uint64, n), make([]uint64, n)
	r.Backoff, r.BackoffBaseMs, r.TimeoutMs = make([]string, n), make([]uint64, n), make([]uint64, n)
	r.State, r.WorkerRun, r.LastError = make([]string, n), make([]string, n), make([]string, n)
	r.RunAfterMs, r.FinishedAtMs = make([]int64, n), make([]int64, n)
	r.OwnerAppId, r.RequesterRun, r.ArgsKind = make([]string, n), make([]string, n), make([]string, n)
	for i, j := range jobs {
		r.Id[i], r.Kind[i], r.Subject[i], r.Queue[i] = j.ID, j.Kind, j.Subject, j.Queue
		r.Priority[i], r.Attempt[i], r.MaxAttempts[i] = uint64(j.Priority), uint64(j.Attempt), uint64(j.MaxAttempts)
		r.Backoff[i], r.BackoffBaseMs[i], r.TimeoutMs[i] = j.Backoff, j.BackoffBaseMs, j.TimeoutMs
		r.State[i], r.WorkerRun[i], r.LastError[i] = j.State, j.WorkerRun, j.LastError
		r.RunAfterMs[i], r.FinishedAtMs[i] = unixMs(j.RunAfter), unixMs(j.FinishedAt)
		r.OwnerAppId[i], r.RequesterRun[i], r.ArgsKind[i] = j.OwnerAppId, j.RequesterRun, j.ArgsKind
	}
	return
}

func unixMs(t time.Time) (ms int64) {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// JobsOf is the inverse of the reply's layout: one [watchbillstore.Job]
// per entry, with the fields the wire carries (Args bytes do not travel;
// ArgsKind does).
func JobsOf(r watchbillreply.WatchbillReply) (jobs []watchbillstore.Job) {
	n := r.Len()
	jobs = make([]watchbillstore.Job, 0, n)
	at := func(s []string, i int) string {
		if i < len(s) {
			return s[i]
		}
		return ""
	}
	u := func(s []uint64, i int) uint64 {
		if i < len(s) {
			return s[i]
		}
		return 0
	}
	ms := func(s []int64, i int) time.Time {
		if i < len(s) && s[i] != 0 {
			return time.UnixMilli(s[i]).UTC()
		}
		return time.Time{}
	}
	for i := 0; i < n; i++ {
		jobs = append(jobs, watchbillstore.Job{
			ID: r.Id[i], Kind: at(r.Kind, i), Subject: at(r.Subject, i), Queue: at(r.Queue, i),
			Priority: uint32(u(r.Priority, i)), Attempt: uint32(u(r.Attempt, i)), MaxAttempts: uint32(u(r.MaxAttempts, i)),
			Backoff: at(r.Backoff, i), BackoffBaseMs: u(r.BackoffBaseMs, i), TimeoutMs: u(r.TimeoutMs, i),
			State: at(r.State, i), WorkerRun: at(r.WorkerRun, i), LastError: at(r.LastError, i),
			RunAfter: ms(r.RunAfterMs, i), FinishedAt: ms(r.FinishedAtMs, i),
			OwnerAppId: at(r.OwnerAppId, i), RequesterRun: at(r.RequesterRun, i), ArgsKind: at(r.ArgsKind, i),
		})
	}
	return
}
