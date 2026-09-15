package watchbillpresence

import "time"

// The two phases a presence row records.
const (
	PhaseStarted = "started"
	PhaseStopped = "stopped"
)

// WorkerPresence is one worker run's declaration of what it drains, as a
// `boxer.facts` row (ADR-0237). Id is the xxh3 of the run id and NaturalKey
// the run id, so a run's rows form one key; Ts is when the phase happened.
type WorkerPresence struct {
	_ struct{} `kind:"watchbillWorker"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	// Kind's value is the label; its membership id is what a query filters on.
	Kind string `lw:"runtimeKindWatchbillWorker,symbol"`
	// RunId is the worker's run, the same id the heartbeat rows carry.
	RunId string `lw:"watchbillWorkerRunId,symbol"`
	// Host is the machine, for a person reading the cell.
	Host string `lw:"watchbillWorkerHost,symbol"`
	// Phase is started or stopped.
	Phase string `lw:"watchbillWorkerPhase,symbol"`
	// Kinds are the handlers the run drains; Queues the queues, empty for
	// every queue; MaxWorkers the concurrency per kind.
	Kinds      []string `lw:"watchbillWorkerKinds,symbolArray"`
	Queues     []string `lw:"watchbillWorkerQueues,symbolArray"`
	MaxWorkers uint32   `lw:"watchbillWorkerMaxWorkers,u32Array,unit"`
}
