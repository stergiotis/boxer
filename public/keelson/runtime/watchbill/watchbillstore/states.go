package watchbillstore

// The job states (ADR-0223 §SD2). A job row holds exactly one; every
// change of it is one conditional update guarded on the previous state,
// then one event row.
const (
	// StateQueued is waiting for a worker; RunAfter says from when.
	StateQueued = "queued"
	// StateRunning is claimed; WorkerRun names the run that holds it.
	StateRunning = "running"
	// StateSucceeded is finished well.
	StateSucceeded = "succeeded"
	// StateFailed is an attempt that ended in error with attempts left;
	// the row is re-queued under the policy in the same transition.
	StateFailed = "failed"
	// StateDiscarded is the last attempt failed.
	StateDiscarded = "discarded"
	// StateCancel is a cancel requested by anyone, not yet acknowledged.
	StateCancel = "cancel"
	// StateCancelled is the worker's acknowledgement of a cancel.
	StateCancelled = "cancelled"
	// StateAbandoned is a running job whose worker's run went silent.
	StateAbandoned = "abandoned"
)

// AllStates lists every state, for validation and for the introspection
// table's vocabulary.
var AllStates = []string{
	StateQueued, StateRunning, StateSucceeded, StateFailed,
	StateDiscarded, StateCancel, StateCancelled, StateAbandoned,
}

// The backoff classes (ADR-0223 §SD6): how long a failed attempt waits
// before its retry, from the job's BackoffBaseMs.
const (
	// BackoffNone retries at once.
	BackoffNone = "none"
	// BackoffLinear waits base × attempt.
	BackoffLinear = "linear"
	// BackoffExponential waits base × 2^(attempt−1).
	BackoffExponential = "exponential"
)

// FinalStates are the states a job leaves the queue in; rows in them are
// what the sweep expires.
var FinalStates = []string{StateSucceeded, StateDiscarded, StateCancelled, StateAbandoned}

// IsFinal reports whether state is one of [FinalStates].
func IsFinal(state string) (yes bool) {
	for _, s := range FinalStates {
		if s == state {
			return true
		}
	}
	return false
}
