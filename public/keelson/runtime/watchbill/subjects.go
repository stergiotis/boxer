package watchbill

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

// WorkerAppId is the identity the host's worker holds on the bus, the
// shape of the runtime's other services.
const WorkerAppId app.AppIdT = "runtime.watchbill"

// The two subjects (ADR-0223 §SD5). Both carry a job id as the payload
// and nothing else: the table is the truth, and a consumer that hears one
// re-reads the rows it cares about.
const (
	// SubjectWake is published by whoever wrote a job row, so the worker
	// polls now rather than at its interval.
	SubjectWake = "watchbill.wake"
	// SubjectChanged is published by the worker after every row it wrote
	// and flushed — never before.
	SubjectChanged = "watchbill.changed"
)

// WorkerCaps is the subject-filter set a worker's bus client needs: the
// wake to hear, the changed to say, and the task producer's, since every
// run is a task.
func WorkerCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: SubjectWake, Direction: app.CapDirectionSub, Reason: "watchbill: hear the doorbell"},
		{Pattern: SubjectChanged, Direction: app.CapDirectionPub, Reason: "watchbill: announce a row"},
	}
	caps = append(caps, task.ProducerCaps()...)
	return
}

// ClientCaps is what an enqueuing app declares: ring the doorbell, hear
// the announcements.
func ClientCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: SubjectWake, Direction: app.CapDirectionPub, Reason: "watchbill: wake the worker after a row"},
		{Pattern: SubjectChanged, Direction: app.CapDirectionSub, Reason: "watchbill: hear which rows changed"},
	}
	return
}
