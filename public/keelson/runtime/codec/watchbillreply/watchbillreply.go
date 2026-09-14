// Package watchbillreply is the leeway-coded wire form of the worker's
// answer to a watchbill client request (ADR-0234 §SD1), sibling to
// [keelson/runtime/codec/watchbillrequest]. A refusal travels as a reply
// with Ok false and a Reason, never as a silent drop or a bare timeout.
//
// The jobs come back as parallel columns, one entry per job, the shape
// the inflight snapshot uses: enqueue and get answer with one entry, list
// with as many as matched, cancel and retry with the row as it reads
// after the verb. Args bytes do not travel back — the reply reports the
// kind they claim, and the consumer's own row is where the work is
// described (ADR-0223 §SD1).
//
// Vocabulary: the `wbJob…` cohort in vdd, Arbitrary cardinality, and the
// shared `reason`.
package watchbillreply

import "time"

// WatchbillReply is the flat wire form of one reply.
type WatchbillReply struct {
	_ struct{} `kind:"watchbillReply"`

	// FactId is the per-row event id.
	FactId uint64 `lw:",id"`

	// NaturalKey is the entity natural key; these bus DTOs carry none.
	NaturalKey []byte `lw:",naturalKey"`

	// At is the reply instant.
	At time.Time `lw:",ts"`

	// Ok is true when the verb applied: the row was written, the
	// transition won, the job was found. False with Reason otherwise.
	Ok bool `lw:"wbReplyOk,bool"`

	// Reason carries the refusal or failure rationale; empty when Ok.
	Reason string `lw:"reason,textArray"`

	// The jobs, zipped by index.
	Id            []string `lw:"wbJobId,stringArray"`
	Kind          []string `lw:"wbJobKind,symbolArray"`
	Subject       []string `lw:"wbJobSubject,textArray"`
	Queue         []string `lw:"wbJobQueue,symbolArray"`
	Priority      []uint64 `lw:"wbJobPriority,u64Array"`
	State         []string `lw:"wbJobState,symbolArray"`
	Attempt       []uint64 `lw:"wbJobAttempt,u64Array"`
	MaxAttempts   []uint64 `lw:"wbJobMaxAttempts,u64Array"`
	Backoff       []string `lw:"wbJobBackoff,symbolArray"`
	BackoffBaseMs []uint64 `lw:"wbJobBackoffBaseMs,u64Array"`
	TimeoutMs     []uint64 `lw:"wbJobTimeoutMs,u64Array"`
	RunAfterMs    []int64  `lw:"wbJobRunAfterMs,i64Array"`
	WorkerRun     []string `lw:"wbJobWorkerRun,stringArray"`
	FinishedAtMs  []int64  `lw:"wbJobFinishedAtMs,i64Array"`
	LastError     []string `lw:"wbJobLastError,textArray"`
	OwnerAppId    []string `lw:"wbJobOwnerApp,stringArray"`
	RequesterRun  []string `lw:"wbJobRequesterRun,stringArray"`
	ArgsKind      []string `lw:"wbJobArgsKind,symbolArray"`
}

// Len is the number of jobs the reply carries.
func (inst WatchbillReply) Len() (n int) { return len(inst.Id) }
