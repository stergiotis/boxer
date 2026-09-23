package drive

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/streaming/stevedore"
	"github.com/stergiotis/boxer/public/streaming/stevedore/deadletter"
)

// Config bounds one run. The zero value handles one request at a time,
// flushes every request, applies no deadline and no body bound, retries
// with the default policy and logs nowhere.
type Config struct {
	// Workers is how many requests are handled at once. Above one, the
	// handler is called from several goroutines and must be safe for that;
	// the sink is always called from one goroutine, in source order.
	Workers int
	// FlushEvery is how many requests are landed between flushes, and so
	// how many a crash costs; zero is one.
	FlushEvery int
	// Deadline bounds one request's handling, attempts included; zero is
	// none.
	Deadline time.Duration
	// MaxBody refuses a request's body above it with a permanent failure
	// before the handler runs; zero is unbounded.
	MaxBody int
	// Retry is the in-place policy for a transient handler or sink error; a
	// zero Attempts takes the default policy.
	Retry stevedore.RetryPolicy
	// Skip leaves out the first Skip requests the source yields — a
	// checkpoint a previous run reported, handed back.
	Skip uint64
	// Checkpoint, when set, is told after each flush how many requests from
	// the start of the source are durable, so a run can be resumed with
	// Skip. An error from it ends the run.
	Checkpoint func(ctx context.Context, done uint64) error
	// Logger is what a handler finds through zerolog.Ctx; nil is silent. A
	// dead letter is the store's to report.
	Logger *zerolog.Logger
}

func (inst Config) workers() int {
	if inst.Workers <= 0 {
		return 1
	}
	return inst.Workers
}

func (inst Config) flushEvery() int {
	if inst.FlushEvery <= 0 {
		return 1
	}
	return inst.FlushEvery
}

func (inst Config) retry() stevedore.RetryPolicy {
	if inst.Retry.Attempts == 0 {
		return stevedore.DefaultRetryPolicy()
	}
	return inst.Retry
}

func (inst Config) logger() *zerolog.Logger {
	if inst.Logger == nil {
		l := zerolog.Nop()
		return &l
	}
	return inst.Logger
}

// panicStackLimit bounds the stack a recovered panic carries into its error.
const panicStackLimit = 4096

// Result is what one run counted.
type Result struct {
	// Requests is how many the source yielded past Skip.
	Requests uint64
	// Items is how many the sink landed.
	Items uint64
	// DeadLetters is how many rows the dead-letter store received.
	DeadLetters uint64
	// Done is the checkpoint: requests from the start of the source that
	// are durable, Skip included.
	Done uint64
}

// Run drives every request of src through handler and sink. It returns
// when the source ends, ctx ends, or a failure no retry cured stops it; the
// result counts what happened either way, and Done says where a resumed run
// starts.
func Run(ctx context.Context, cfg Config, src SourceI, handler stevedore.HandlerI, sink stevedore.SinkI, dead deadletter.StoreI) (res Result, err error) {
	if src == nil || handler == nil || sink == nil || dead == nil {
		err = eh.Errorf("a run needs a source, a handler, a sink and a dead-letter store")
		return
	}
	inst := &runner{cfg: cfg, src: src, handler: handler, sink: sink, dead: dead, now: time.Now}
	inst.res.Done = cfg.Skip
	batch := make([]pending, 0, cfg.flushEvery())
	skipped := uint64(0)
	for req, serr := range src.All(ctx) {
		if serr != nil && req.Origin == "" && len(req.Body) == 0 {
			// The source itself failed; what was landed stays landed.
			err = eh.Errorf("source: %w", serr)
			return inst.res, inst.finish(ctx, batch, err)
		}
		if skipped < cfg.Skip {
			skipped++
			continue
		}
		inst.res.Requests++
		batch = append(batch, pending{req: req, err: serr})
		if len(batch) < cfg.flushEvery() {
			continue
		}
		err = inst.land(ctx, batch)
		batch = batch[:0]
		if err != nil {
			return inst.res, err
		}
	}
	if len(batch) > 0 {
		err = inst.land(ctx, batch)
	}
	return inst.res, err
}

type runner struct {
	cfg     Config
	src     SourceI
	handler stevedore.HandlerI
	sink    stevedore.SinkI
	dead    deadletter.StoreI
	now     func() time.Time
	res     Result
}

// pending is one request between the source and the sink.
type pending struct {
	req   stevedore.Request
	err   error // the source's, then the handler's
	items []stevedore.Item
}

// finish lands what a source failure left behind, so nothing handled is
// lost, then returns the failure.
func (inst *runner) finish(ctx context.Context, batch []pending, cause error) error {
	if len(batch) == 0 {
		return cause
	}
	lerr := inst.land(ctx, batch)
	if lerr != nil {
		return eb.Build().Str("cause", cause.Error()).Errorf("landing after the source failed: %w", lerr)
	}
	return cause
}

// land handles a batch in parallel, lands it in order, flushes, and reports
// the checkpoint.
func (inst *runner) land(ctx context.Context, batch []pending) (err error) {
	inst.handleAll(ctx, batch)
	for i := range batch {
		err = inst.landOne(ctx, &batch[i])
		if err != nil {
			return
		}
	}
	err = stevedore.Retry(ctx, inst.cfg.retry(), inst.sink.Flush)
	if err != nil {
		return eh.Errorf("flush sink: %w", err)
	}
	err = stevedore.Retry(ctx, inst.cfg.retry(), inst.dead.Flush)
	if err != nil {
		return eh.Errorf("flush dead letters: %w", err)
	}
	inst.res.Done += uint64(len(batch))
	if inst.cfg.Checkpoint != nil {
		err = inst.cfg.Checkpoint(ctx, inst.res.Done)
		if err != nil {
			return eh.Errorf("checkpoint: %w", err)
		}
	}
	return
}

// handleAll runs the handler over the batch with the configured workers.
func (inst *runner) handleAll(ctx context.Context, batch []pending) {
	workers := min(inst.cfg.workers(), len(batch))
	if workers <= 1 {
		for i := range batch {
			inst.handleOne(ctx, &batch[i])
		}
		return
	}
	var wg sync.WaitGroup
	next := make(chan int)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				inst.handleOne(ctx, &batch[i])
			}
		}()
	}
	for i := range batch {
		next <- i
	}
	close(next)
	wg.Wait()
}

// handleOne runs the handler for one request the way the processor host
// does: bounded, recovered, retried; the outcome lands on the pending.
func (inst *runner) handleOne(ctx context.Context, p *pending) {
	if p.err != nil {
		return // the source already failed it
	}
	if inst.cfg.MaxBody > 0 && len(p.req.Body) > inst.cfg.MaxBody {
		p.err = stevedore.Permanent(eb.Build().Int("len", len(p.req.Body)).Int("max", inst.cfg.MaxBody).Errorf("request body exceeds the bound"))
		return
	}
	hctx := inst.cfg.logger().With().Str("origin", p.req.Origin).Logger().WithContext(ctx)
	if inst.cfg.Deadline > 0 {
		var cancel context.CancelFunc
		hctx, cancel = context.WithTimeout(hctx, inst.cfg.Deadline)
		defer cancel()
	}
	ref := stevedore.ReferenceOf(p.req)
	var emits []stevedore.Emit
	p.err = stevedore.Retry(hctx, inst.cfg.retry(), func(actx context.Context) error {
		emits = emits[:0]
		return inst.attempt(actx, p.req, &emits)
	})
	if p.err != nil {
		return
	}
	p.items = make([]stevedore.Item, len(emits))
	for i, e := range emits {
		p.items[i] = stevedore.Item{
			Ref: ref, Origin: p.req.Origin, Ordinal: uint64(i),
			Line: e.Line, Offset: e.Offset,
			Split: p.req.Split, Part: p.req.Part, Parts: p.req.Parts, Last: p.req.Last,
			PayloadKind: e.PayloadKind, Payload: e.Payload,
		}
	}
}

func (inst *runner) attempt(ctx context.Context, req stevedore.Request, emits *[]stevedore.Emit) (err error) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		stack := debug.Stack()
		if len(stack) > panicStackLimit {
			stack = stack[:panicStackLimit]
		}
		err = stevedore.Permanent(eb.Build().Str("panic", fmt.Sprint(r)).Str("stack", string(stack)).Errorf("handler panicked"))
	}()
	err = inst.handler.Handle(ctx, req, func(e stevedore.Emit) error {
		*emits = append(*emits, e)
		return nil
	})
	if err == nil && ctx.Err() != nil {
		err = eh.Errorf("handler context ended: %w", ctx.Err())
	}
	return
}

// landOne hands a handled request's items to the sink, or its failure to
// the dead-letter store. A sink failure no retry cured stops the run.
func (inst *runner) landOne(ctx context.Context, p *pending) (err error) {
	if p.err != nil {
		return inst.deadLetter(ctx, p.req, p.err)
	}
	for _, item := range p.items {
		lerr := stevedore.Retry(ctx, inst.cfg.retry(), func(actx context.Context) error {
			return inst.sink.Land(actx, item)
		})
		if lerr == nil {
			inst.res.Items++
			continue
		}
		if stevedore.ClassOf(lerr) == stevedore.ClassPermanent {
			err = inst.deadLetter(ctx, p.req, eb.Build().Uint64("ordinal", item.Ordinal).Errorf("land item: %w", lerr))
			if err != nil {
				return
			}
			continue
		}
		return eh.Errorf("land item: %w", lerr)
	}
	return
}

// deadLetter records one request given up on. Its row is keyed by the
// source's name and the request's origin, so a rerun writes the same row.
func (inst *runner) deadLetter(ctx context.Context, req stevedore.Request, cause error) (err error) {
	row := deadletter.New(inst.now())
	row.Id, row.NaturalKey = deadletter.Identity(inst.src.Name()+"/"+req.Origin, 0, 0)
	row.Ref = stevedore.ReferenceOf(req).Value()
	row.Origin = req.Origin
	row.Class = stevedore.ClassOf(cause).String()
	row.Error = cause.Error()
	row.Topic = inst.src.Name()
	row.Message = req.Body
	err = stevedore.Retry(ctx, inst.cfg.retry(), func(actx context.Context) error {
		return inst.dead.Add(actx, row)
	})
	if err != nil {
		return eb.Build().Str("origin", req.Origin).Errorf("record dead letter: %w", err)
	}
	inst.res.DeadLetters++
	return
}
