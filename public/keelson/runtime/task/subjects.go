//go:build llm_generated_opus47

package task

import "strings"

// Subject taxonomy (ADR-0038). Flat per-task layout:
//
//	task.<id>.<verb>
//
// Producers publish created/progress/done/error; consumers publish cancel;
// observers subscribe task.> with one wildcard.
const (
	SubjectPrefix = "task."

	VerbCreated  = "created"
	VerbProgress = "progress"
	VerbCancel   = "cancel"
	VerbDone     = "done"
	VerbError    = "error"

	// PatternAll matches every task verb across every task id. Used by
	// WatchAll observers and by the M3 supervisor.
	PatternAll = "task.>"

	// PatternCancelAll matches the cancel verb across every task id. Used
	// by callers that want to issue cancels but never produce or observe
	// progress.
	PatternCancelAll = "task.*.cancel"

	// SubjectListInflight is the request/reply subject the M3 supervisor
	// serves. A consumer publishes a Request on this subject (empty
	// payload) and receives a marshalled InflightSnapshotReply on its
	// reply inbox. The constant lives on the task package — not on the
	// supervisor — so consumers can query the snapshot without taking
	// a build-time dependency on the supervisor package.
	SubjectListInflight = "task.list.inflight"
)

// SubjectCreated returns the subject a producer publishes once per task on
// spawn.
func SubjectCreated(id TaskIdT) (subject string) {
	subject = SubjectPrefix + string(id) + "." + VerbCreated
	return
}

// SubjectProgress returns the subject for periodic progress emissions.
func SubjectProgress(id TaskIdT) (subject string) {
	subject = SubjectPrefix + string(id) + "." + VerbProgress
	return
}

// SubjectCancel returns the subject a consumer publishes to request
// cancellation. The handle subscribes to this on Spawn.
func SubjectCancel(id TaskIdT) (subject string) {
	subject = SubjectPrefix + string(id) + "." + VerbCancel
	return
}

// SubjectDone returns the terminal-success subject. Emitted once at
// most.
func SubjectDone(id TaskIdT) (subject string) {
	subject = SubjectPrefix + string(id) + "." + VerbDone
	return
}

// SubjectError returns the terminal-failure subject. Emitted once at
// most.
func SubjectError(id TaskIdT) (subject string) {
	subject = SubjectPrefix + string(id) + "." + VerbError
	return
}

// ParseSubject splits a task.<id>.<verb> subject into its id and verb
// components. Returns ok=false for subjects that do not match the
// taxonomy. Used by WatchAll to demux a single task.> subscription into
// per-verb observer callbacks.
func ParseSubject(subject string) (id TaskIdT, verb string, ok bool) {
	if !strings.HasPrefix(subject, SubjectPrefix) {
		return
	}
	rest := subject[len(SubjectPrefix):]
	dot := strings.LastIndexByte(rest, '.')
	if dot <= 0 || dot == len(rest)-1 {
		return
	}
	id = TaskIdT(rest[:dot])
	verb = rest[dot+1:]
	ok = true
	return
}
