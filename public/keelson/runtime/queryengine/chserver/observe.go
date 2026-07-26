package chserver

import (
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryprogress"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// observe.go — the observation role, which is the E7 poller
// (queryprogress) wearing this engine's hat rather than a component
// standing on its own.
//
// The poller already had the hard parts: one statement per tick covering
// every registered run, publication on per-run bus subjects so a party that
// never held the connection can watch, and two refusals that keep it
// honest — it never synthesises a terminal state, and it never deregisters
// a run by itself. What it lacked was a reason to be bound to any
// particular server. An engine is that binding: `system.processes` is
// per-server, so a poller pointed anywhere else than at the server that ran
// the query reports nothing, and having the two share one endpoint by
// construction removes the way to get it wrong.
//
// Observation is optional, and the optionality is carried by the TYPE. A
// plain [Engine] has no Watch method, so it does not satisfy
// [queryengine.ObservationI] and no consumer can ask it to observe. An
// engine built with a bus does. That is the whole discovery mechanism:
//
//	if obs, ok := eng.(queryengine.ObservationI); ok {
//		err = obs.Watch(runID)
//	}

// The E7 poller IS the observation role for a server; the interface was
// drawn to fit what it already did.
var _ queryengine.ObservationI = (*queryprogress.Poller)(nil)

// ObservationConfig parameterises the poller an [ObservingEngine] carries.
// Bus is required; the endpoint and credentials come from the engine's own
// [Config], which is the point.
type ObservationConfig struct {
	// Bus receives the ticks. It needs the poller's publish capability
	// (queryprogress.PublishFilter).
	Bus app.BusI
	// Interval is the tick period; zero takes the poller's default. It is
	// also the staleness bound — a tick reports what the server said at
	// that moment, and the next says nothing until it arrives.
	Interval time.Duration
	Log      zerolog.Logger
}

// ObservingEngine is an [Engine] whose runs can also be watched by somebody
// other than the party holding the result path (R8).
type ObservingEngine struct {
	*Engine
	poller *queryprogress.Poller
}

var (
	_ queryengine.DeliveryI    = (*ObservingEngine)(nil)
	_ queryengine.ObservationI = (*ObservingEngine)(nil)
	_ queryengine.ControlI     = (*ObservingEngine)(nil)
)

// NewObserving returns an engine that delivers, observes and controls one
// server. The poller it builds watches the same endpoint under the same
// credentials, so a run this engine issued is a run this engine can observe.
//
// It does not start ticking; call [ObservingEngine.Start].
func NewObserving(cfg Config, obs ObservationConfig) (inst *ObservingEngine, err error) {
	eng, err := New(cfg)
	if err != nil {
		return
	}
	if obs.Bus == nil {
		err = eh.Errorf("chserver: observation needs a bus")
		return
	}
	poller, err := queryprogress.New(queryprogress.Options{
		Endpoint: cfg.Endpoint,
		User:     cfg.User,
		Password: cfg.Password,
		Bus:      obs.Bus,
		Interval: obs.Interval,
		Log:      obs.Log,
	})
	if err != nil {
		return
	}
	inst = &ObservingEngine{Engine: eng, poller: poller}
	return
}

// Poller exposes the underlying poller, for a caller that wants to drive
// the cadence itself ([queryprogress.Poller.Tick]) or inspect the watch set.
func (inst *ObservingEngine) Poller() (poller *queryprogress.Poller) {
	poller = inst.poller
	return
}

// Start begins observing until Close.
func (inst *ObservingEngine) Start() {
	inst.poller.Start()
}

// Close stops observation and waits for the tick loop to finish. It does
// not affect delivery.
func (inst *ObservingEngine) Close() (err error) {
	err = inst.poller.Close()
	return
}

// Watch registers a run for observation. Its ticks are published on
// [queryprogress.Subject](runID), where any holder of the subscribe
// capability can read them — including a process that did not issue the run.
func (inst *ObservingEngine) Watch(runID string) (err error) {
	err = inst.poller.Watch(runID)
	return
}

// Unwatch deregisters a run.
//
// The caller decides when, and the right moment is the terminal frame. This
// engine will not do it on its own: the only signal it has — the run
// leaving `system.processes` — cannot tell finished from killed from
// failed, and acting on it would be inventing an outcome.
func (inst *ObservingEngine) Unwatch(runID string) {
	inst.poller.Unwatch(runID)
}
