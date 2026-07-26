// Package chlocal delivers query results from the chlocal broker
// ([ADR-0028]) as a runstream — engine 1 of [ADR-0144]'s three, and the
// one that has the least.
//
// A broker-spawned worker is a one-shot `clickhouse-local` process with no
// listener. It runs, answers, and dies, taking its system tables with it.
// So this engine plays exactly one of the three roles: it delivers. There
// is no process list to observe and nothing to kill, and rather than
// answering "unsupported" at runtime it simply does not implement
// [queryengine.ObservationI] or [queryengine.ControlI] — a consumer asking
// for those gets its answer from the type.
//
// The result arrives whole, in one bus reply, so the stream it yields is a
// single data frame and a terminal. That is not a degenerate stream pending
// improvement; it is what a buffered engine honestly has to say. Streaming
// replies need a wire that does not exist yet ([ADR-0143]), and the
// broker's reserved `Streaming` request flag is still refused for the same
// reason.
//
// [ADR-0028]: https://github.com/stergiotis/boxer/blob/main/doc/adr/0028-chlocal-low-latency-sql-cap.md
// [ADR-0143]: https://github.com/stergiotis/boxer/blob/main/doc/adr/0143-bus-streaming-reply-channel.md
// [ADR-0144]: https://github.com/stergiotis/boxer/blob/main/doc/adr/0144-query-engine-adapters.md
package chlocal

import (
	"context"
	"io"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Config parameterises an [Engine]. Bus is required.
type Config struct {
	// Bus is the caller's bus client. It must hold a SubjectFilter with
	// CapDirectionPub (or Both) covering `ch.local.exec.<PoolName>`.
	Bus app.BusI
	// PoolName is the chlocal pool to execute on; empty means
	// [DefaultPoolName].
	PoolName string
}

// DefaultPoolName is the pool an Engine targets when Config names none.
const DefaultPoolName = "introspect"

// Engine delivers results from one chlocal pool.
type Engine struct {
	bus      app.BusI
	poolName string
}

var _ queryengine.DeliveryI = (*Engine)(nil)

// New returns an Engine bound to one pool.
func New(cfg Config) (inst *Engine, err error) {
	if cfg.Bus == nil {
		err = eh.Errorf("chlocal: bus is required")
		return
	}
	pool := cfg.PoolName
	if pool == "" {
		pool = DefaultPoolName
	}
	inst = &Engine{bus: cfg.Bus, poolName: pool}
	return
}

// PoolName reports the pool this engine executes on.
func (inst *Engine) PoolName() (name string) {
	name = inst.poolName
	return
}

// Deliver runs req on the pool and returns its result as a stream.
//
// The run id is accepted and goes nowhere, which is R10 rather than an
// omission: a one-shot worker has no process list to register it in and no
// query log to retain it, so on this engine the id names the run only on
// this side of the bus. That is also why the observation and control roles
// are absent — there would be nothing to address them to.
func (inst *Engine) Deliver(ctx context.Context, req queryengine.Request) (st queryengine.StreamI, res queryengine.Result, err error) {
	err = req.Validate()
	if err != nil {
		return
	}
	rep, reqErr := chlocalbroker.ExecOnPool(ctx, inst.bus, inst.poolName, chlocalbroker.ExecRequest{
		SQL:         req.SQL,
		Format:      req.Format,
		Cacheable:   req.Cacheable,
		Settings:    req.Settings,
		Params:      req.Params,
		InputTables: req.Inputs,
	})
	if reqErr != nil {
		// The bus never answered: no worker, no capability, or a timeout.
		// That is how this run ended, not a reason to refuse the request
		// the caller already made — so it is a terminal, not an err.
		st = queryengine.NewSliceStream(nil, runstream.Failed(reqErr), nil)
		return
	}
	res = queryengine.Result{
		ContentType: rep.ContentType,
		CacheHit:    rep.CacheHit,
		Summary:     queryengine.Summary{ElapsedNs: uint64(rep.Elapsed.Nanoseconds())},
	}
	if repErr := rep.Err(); repErr != nil {
		_ = rep.Close()
		st = queryengine.NewSliceStream(nil, runstream.Failed(repErr), nil)
		return
	}

	body, readErr := io.ReadAll(rep)
	if readErr != nil {
		_ = rep.Close()
		st = queryengine.NewSliceStream(nil, runstream.Failed(readErr), nil)
		return
	}
	st = queryengine.NewSliceStream([][]byte{body}, terminalFor(req.Cap), rep)
	return
}

// terminalFor decides how a delivered result ended.
//
// A declared row cap cannot be judged here, and saying so is the whole
// point. The broker reports no result-row count, so an engine that hit
// `max_result_rows` with `result_overflow_mode = break` returns a short
// answer with nothing on the wire to mark it. Reporting that as complete
// would be the exact mistake R9 exists to prevent, so a declared cap is
// reported as possibly-a-prefix rather than silently as the answer — loud
// and ambiguous beats quiet and wrong.
func terminalFor(cap queryengine.RowCap) (t runstream.Terminal) {
	if cap.MaxResultRows == 0 || !cap.Breaks {
		t = runstream.Complete()
		return
	}
	t = runstream.Truncated("the request declared max_result_rows with " +
		"result_overflow_mode=break, and this engine reports no result row count — " +
		"the result may be a prefix")
	return
}
