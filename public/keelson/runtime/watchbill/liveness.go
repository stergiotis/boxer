package watchbill

import (
	"context"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// LivenessI answers whether a run has shown life since an instant
// (ADR-0223 §SD4). The runtime writes a heartbeat row for every run it
// hosts; a run with none since the instant is dead for the sweep's
// purposes.
type LivenessI interface {
	Alive(ctx context.Context, runId string, since time.Time) (alive bool, err error)
}

// RunEventLiveness is [LivenessI] over the facts store's run-event reader:
// any event of the run since the instant — a heartbeat, a start, a window
// open — is life. The reader is type-asserted off the facts store; a
// store without one (the in-memory fallback) has no heartbeats to read,
// and the worker then sweeps nothing.
type RunEventLiveness struct {
	reader factsstore.RunEventReaderI
}

var _ LivenessI = (*RunEventLiveness)(nil)

// NewRunEventLiveness returns nil when facts cannot read events back.
func NewRunEventLiveness(facts factsstore.FactsStoreI) (inst *RunEventLiveness) {
	reader, ok := facts.(factsstore.RunEventReaderI)
	if !ok || reader == nil {
		return nil
	}
	inst = &RunEventLiveness{reader: reader}
	return
}

func (inst *RunEventLiveness) Alive(_ context.Context, runId string, since time.Time) (alive bool, err error) {
	rows, err := inst.reader.ListRunEvents(factsstore.RunEventFilter{RunId: runId, Since: since, Limit: 1})
	if err != nil {
		return false, eb.Build().Str("runId", runId).Errorf("liveness: %w", err)
	}
	alive = len(rows) > 0
	return
}

// MemLiveness is the test double: a set of live run ids.
type MemLiveness struct {
	Live map[string]bool
}

var _ LivenessI = MemLiveness{}

func (inst MemLiveness) Alive(_ context.Context, runId string, _ time.Time) (alive bool, err error) {
	alive = inst.Live[runId]
	return
}
