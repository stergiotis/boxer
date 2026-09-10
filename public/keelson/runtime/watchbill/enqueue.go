package watchbill

import (
	"context"
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// idLen matches the task id's: a nanoid of the default alphabet, so a
// job id serves as its run's task id unchanged.
const idLen = 21

// Request is what a consumer asks for (ADR-0223 §SD1, §SD6). Kind names
// the handler; Subject the row in the consumer's own table that says what
// to do; Args, with ArgsKind, the facts-CBOR fallback for a consumer with
// no such row. The zero policy is one attempt, no backoff, no timeout,
// due now.
type Request struct {
	Kind        string
	Subject     string
	Queue       string
	Priority    uint32
	MaxAttempts uint32
	Backoff     string
	BackoffBase time.Duration
	Timeout     time.Duration
	OwnerAppId  app.AppIdT
	// RequesterRun is the run enqueuing; the worker's own for a job it
	// enqueues for itself.
	RequesterRun string
	ArgsKind     string
	Args         []byte
	// RunAfter is when the job becomes due; zero is now.
	RunAfter time.Time
	// ID is the job id; empty mints one.
	ID string
}

// Enqueue writes req as a queued job row and returns its id. It does not
// ring the doorbell: call [Wake] after, on a bus, so the worker polls now.
func Enqueue(ctx context.Context, store StoreI, req Request) (id string, err error) {
	if req.Kind == "" {
		return "", eh.Errorf("watchbill: enqueue without a kind")
	}
	id = req.ID
	if id == "" {
		if id, err = gonanoid.New(idLen); err != nil {
			return "", eh.Errorf("mint job id: %w", err)
		}
	}
	job := watchbillstore.Job{
		ID: id, Kind: req.Kind, Subject: req.Subject, Queue: req.Queue,
		Priority: req.Priority, MaxAttempts: max(req.MaxAttempts, 1),
		Backoff: req.Backoff, BackoffBaseMs: uint64(req.BackoffBase / time.Millisecond),
		TimeoutMs:  uint64(req.Timeout / time.Millisecond),
		OwnerAppId: string(req.OwnerAppId), RequesterRun: req.RequesterRun,
		ArgsKind: req.ArgsKind, Args: req.Args,
		State: watchbillstore.StateQueued, RunAfter: req.RunAfter.UTC(),
	}
	if job.Queue == "" {
		job.Queue = "default"
	}
	if job.Backoff == "" {
		job.Backoff = watchbillstore.BackoffNone
	}
	if job.RunAfter.IsZero() {
		job.RunAfter = time.Now().UTC()
	}
	if err = store.Enqueue(ctx, job); err != nil {
		return "", eb.Build().Str("kind", req.Kind).Errorf("enqueue: %w", err)
	}
	return
}

// Wake rings the doorbell for id. A nil bus is silent: the worker polls at
// its interval regardless.
func Wake(bus app.BusI, id string) (err error) {
	if bus == nil {
		return
	}
	if err = bus.Publish(SubjectWake, []byte(id)); err != nil {
		err = eb.Build().Str("id", id).Errorf("wake: %w", err)
	}
	return
}

// RequestCancel asks for the job to stop (ADR-0223 §SD2): a queued job
// is cancelled outright; a running one is marked, and the holding worker
// acknowledges within a poll. actor is the run asking. ok is false when
// the job is not in a state a cancel applies to.
func RequestCancel(ctx context.Context, store StoreI, id string, actor string, now time.Time) (ok bool, err error) {
	// The current state decides the route, and a repeat of an earlier
	// cancel by the same actor is refused here rather than read as won:
	// the read-back cannot tell "just did it" from "did it before".
	job, found, err := store.Get(ctx, id)
	if err != nil || !found {
		return false, err
	}
	fin := now.UTC()
	switch job.State {
	case watchbillstore.StateQueued:
		// No holder to ask; cancelled here and now.
		job, ok, err = store.Transition(ctx, Transition{
			ID: id, From: []string{watchbillstore.StateQueued}, To: watchbillstore.StateCancelled,
			Actor: actor, FinishedAt: &fin,
		})
		if err != nil || !ok {
			return ok, err
		}
		return true, store.WriteEvent(ctx, fin, watchbillstore.Event{ID: id, State: watchbillstore.StateCancelled, Attempt: job.Attempt, WorkerRun: actor, Note: "cancelled while queued"})
	case watchbillstore.StateRunning:
		// The holder keeps the row: the state changes, the worker-run
		// column must not, or the holder would no longer find it held.
		_, ok, err = store.Transition(ctx, Transition{
			ID: id, From: []string{watchbillstore.StateRunning}, HeldBy: job.WorkerRun,
			To: watchbillstore.StateCancel, Actor: job.WorkerRun,
		})
		if err != nil || !ok {
			return ok, err
		}
		return true, store.WriteEvent(ctx, fin, watchbillstore.Event{ID: id, State: watchbillstore.StateCancel, Attempt: job.Attempt, WorkerRun: actor})
	default:
		return false, nil
	}
}

// Retry puts a finished job back in the queue with its attempts reset
// (ADR-0223 §SD2); ok is false when the job is not in a final state.
func Retry(ctx context.Context, store StoreI, id string, actor string, now time.Time) (ok bool, err error) {
	job, found, err := store.Get(ctx, id)
	if err != nil || !found || !watchbillstore.IsFinal(job.State) {
		return false, err
	}
	at := now.UTC()
	job, ok, err = store.Transition(ctx, Transition{
		ID: id, From: watchbillstore.FinalStates, To: watchbillstore.StateQueued,
		Actor: actor, RunAfter: &at, ResetAttempts: true,
	})
	if err != nil || !ok {
		return ok, err
	}
	return true, store.WriteEvent(ctx, at, watchbillstore.Event{ID: id, State: watchbillstore.StateQueued, Attempt: job.Attempt, WorkerRun: actor, Note: "retried"})
}
