package watchbill

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

// WorkerAppId is the identity the host's worker holds on the bus, the
// shape of the runtime's other services.
const WorkerAppId app.AppIdT = "runtime.watchbill"

// The two doorbell subjects (ADR-0223 §SD5). Both carry a job id as the
// payload and nothing else: the table is the truth, and a consumer that
// hears one re-reads the rows it cares about.
const (
	// SubjectWake is published by whoever wrote a job row, so the worker
	// polls now rather than at its interval.
	SubjectWake = "watchbill.wake"
	// SubjectChanged is published by the worker after every row it wrote
	// and flushed — never before.
	SubjectChanged = "watchbill.changed"
)

// The client protocol (ADR-0234 §SD1): `watchbill.job.<op>` is a
// request/reply subject the worker serves. The payload is a
// watchbillrequest.WatchbillRequest, the reply a
// watchbillreply.WatchbillReply, and the op in the subject and in the
// payload must agree. The verbs are River's client verbs.
const (
	SubjectJobPrefix = "watchbill.job."
	SubjectJobAll    = "watchbill.job.*"
)

// SubjectJob is the request subject for op.
func SubjectJob(op string) (subject string) { return SubjectJobPrefix + op }

// WorkerCaps is the subject-filter set a worker's bus client needs: the
// wake to hear, the changed to say, the job requests to serve and the
// inboxes to answer on, and the task producer's, since every run is a
// task.
func WorkerCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: SubjectWake, Direction: app.CapDirectionSub, Reason: "watchbill: hear the doorbell"},
		{Pattern: SubjectChanged, Direction: app.CapDirectionPub, Reason: "watchbill: announce a row"},
		{Pattern: SubjectJobAll, Direction: app.CapDirectionSub, Reason: "watchbill: serve job requests"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "watchbill: reply to inboxes"},
	}
	caps = append(caps, task.ProducerCaps()...)
	return
}

// ClientCaps is what an app that uses the queue declares: the job verbs
// to ask, the doorbell to ring, the announcements to hear. The reply
// inbox needs no cap of its own — the in-proc client bypasses it for its
// own requests, and a NATS server grants it with the request.
func ClientCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: SubjectJobAll, Direction: app.CapDirectionPub, Reason: "watchbill: enqueue, cancel, retry, get and list jobs"},
		{Pattern: SubjectWake, Direction: app.CapDirectionPub, Reason: "watchbill: wake the worker after a row"},
		{Pattern: SubjectChanged, Direction: app.CapDirectionSub, Reason: "watchbill: hear which rows changed"},
	}
	return
}
