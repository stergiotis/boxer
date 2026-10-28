package watchbillstore

import "time"

// Job is one row of the job table: the request, and the state the worker
// rewrites in place (ADR-0223 §SD1, §SD2).
//
// ID is the job id, minted by whoever enqueues. Subject is the identity of
// the work — the row in the consumer's own table that says what to do —
// and Args is the fallback for a consumer with no such row: facts-CBOR of
// the vocabulary kind ArgsKind names, so a free-form payload has no
// spelling here (the ADR-0135 §SD2 rule). Prefer Subject.
//
// The memberships are the runtime vocabulary's, resolved at generation
// time; adding a field means minting its membership first, then
// regenerating (ADR-0183 D0).
type Job struct {
	_             struct{}  `kind:"watchbillJob"`
	ID            string    `lw:",id"`
	Kind          string    `lw:"watchbillKind,jobKind"`
	Subject       string    `lw:"watchbillSubject,jobSubject"`
	Queue         string    `lw:"watchbillQueue,jobQueue"`
	Priority      uint32    `lw:"watchbillPriority,jobPriority"`
	MaxAttempts   uint32    `lw:"watchbillMaxAttempts,jobMaxAttempts"`
	Backoff       string    `lw:"watchbillBackoff,jobBackoff"`
	BackoffBaseMs uint64    `lw:"watchbillBackoffBaseMs,jobBackoffBaseMs"`
	TimeoutMs     uint64    `lw:"watchbillTimeoutMs,jobTimeoutMs"`
	OwnerAppId    string    `lw:"runtimeApp,jobOwnerApp"`
	RequesterRun  string    `lw:"runtimeRun,jobRequesterRun"`
	ArgsKind      string    `lw:"watchbillArgsKind,jobArgsKind"`
	Args          []byte    `lw:"watchbillArgs,jobArgs"`
	State         string    `lw:"watchbillState,jobState"`
	Attempt       uint32    `lw:"watchbillAttempt,jobAttempt"`
	RunAfter      time.Time `lw:"watchbillRunAfter,jobRunAfter"`
	WorkerRun     string    `lw:"watchbillWorkerRun,jobWorkerRun"`
	FinishedAt    time.Time `lw:"watchbillFinishedAt,jobFinishedAt"`
	LastError     string    `lw:"watchbillLastError,jobLastError"`
	// The delegation: the agent's task, its epoch and the call whose
	// handler enqueued the job, as the dispatcher attested them (ADR-0344
	// §SD2). Empty is a job no agent caused. The memberships are the
	// trail's, so the join to the action record is on the same names.
	// Declared last, after every earlier section, so a table created
	// before them is migrated by appending columns.
	Task      string `lw:"trailTask,jobTask"`
	TaskEpoch uint64 `lw:"trailTaskEpoch,jobTaskEpoch"`
	Call      string `lw:"trailCall,jobCall"`
}

// AgentCaused says whether an attested delegation enqueued the job: the
// bit that chooses the entry frame it runs under (ADR-0344 §SD1).
func (inst Job) AgentCaused() (yes bool) { return inst.Task != "" }
