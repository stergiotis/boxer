//go:build llm_generated_opus47

package task

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskcancel"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskcreated"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskdone"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskerror"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskprogress"
)

// TaskIdT is a per-task identifier. Generated as a nanoid by Spawn when
// SpawnOpts.Id is empty; callers may also supply a deterministic id (test
// fixtures, replay tools). Carried verbatim in subject paths as one NATS
// token — the nanoid default alphabet (A-Z, a-z, 0-9, _, -) is
// NATS-token-safe.
type TaskIdT string

// UnitE classifies the magnitude carried by ProgressReport.Current /
// .Total. The estimator uses it to humanize throughput ("18 MB/s" vs
// "240 items/s" vs "step 3 of 5").
type UnitE uint8

const (
	UnitUnspecified UnitE = 0
	UnitItems       UnitE = 1
	UnitBytes       UnitE = 2
	UnitSteps       UnitE = 3
)

var AllUnits = []UnitE{
	UnitItems,
	UnitBytes,
	UnitSteps,
}

func (inst UnitE) String() (s string) {
	switch inst {
	case UnitItems:
		s = "items"
	case UnitBytes:
		s = "bytes"
	case UnitSteps:
		s = "steps"
	default:
		s = "unspecified"
	}
	return
}

// ParseUnit is the inverse of UnitE.String. Unknown strings map to
// UnitUnspecified — the wire is forward-compatible with future units a
// receiver did not anticipate.
func ParseUnit(s string) (u UnitE) {
	switch s {
	case "items":
		u = UnitItems
	case "bytes":
		u = UnitBytes
	case "steps":
		u = UnitSteps
	default:
		u = UnitUnspecified
	}
	return
}

// ProgressReport is the producer-side input to HandleI.Report. The
// estimator computes throughput + ETA from a sliding window of these.
// Total=0 marks an indeterminate task (count visible, end unknown).
type ProgressReport struct {
	Current uint64
	Total   uint64
	Unit    UnitE
	Note    string
}

// MarshalTaskCreated serialises a [taskcreated.TaskCreated]. The
// payload type lives in keelson/runtime/codec/taskcreated; this helper
// keeps the legacy task.MarshalTaskCreated entry point so existing
// callers remain valid after the type move.
func MarshalTaskCreated(c taskcreated.TaskCreated) (b []byte, err error) {
	b, err = buscodec.Encode(c)
	if err != nil {
		err = eh.Errorf("task: marshal created: %w", err)
	}
	return
}

// UnmarshalTaskCreated is the inverse of MarshalTaskCreated.
func UnmarshalTaskCreated(b []byte) (c taskcreated.TaskCreated, err error) {
	c, err = buscodec.Decode[taskcreated.TaskCreated](b)
	if err != nil {
		err = eh.Errorf("task: unmarshal created: %w", err)
	}
	return
}

// MarshalTaskProgress serialises a [taskprogress.TaskProgress]. The
// payload type lives in keelson/runtime/codec/taskprogress (the first
// broker DTO migrated onto the ADR-0042 leeway codec); this helper
// keeps the legacy task.MarshalTaskProgress entry point so existing
// callers remain valid after the type move.
func MarshalTaskProgress(p taskprogress.TaskProgress) (b []byte, err error) {
	b, err = buscodec.Encode(p)
	if err != nil {
		err = eh.Errorf("task: marshal progress: %w", err)
	}
	return
}

// UnmarshalTaskProgress is the inverse of MarshalTaskProgress.
func UnmarshalTaskProgress(b []byte) (p taskprogress.TaskProgress, err error) {
	p, err = buscodec.Decode[taskprogress.TaskProgress](b)
	if err != nil {
		err = eh.Errorf("task: unmarshal progress: %w", err)
	}
	return
}

// MarshalTaskDone serialises a TaskDone.
func MarshalTaskDone(d taskdone.TaskDone) (b []byte, err error) {
	b, err = buscodec.Encode(d)
	if err != nil {
		err = eh.Errorf("task: marshal done: %w", err)
	}
	return
}

// UnmarshalTaskDone is the inverse of MarshalTaskDone.
func UnmarshalTaskDone(b []byte) (d taskdone.TaskDone, err error) {
	d, err = buscodec.Decode[taskdone.TaskDone](b)
	if err != nil {
		err = eh.Errorf("task: unmarshal done: %w", err)
	}
	return
}

// MarshalTaskError serialises a [taskerror.TaskError]. The payload
// type lives in keelson/runtime/codec/taskerror; this helper keeps
// the legacy task.MarshalTaskError entry point so existing callers
// remain valid after the type move.
func MarshalTaskError(e taskerror.TaskError) (b []byte, err error) {
	b, err = buscodec.Encode(e)
	if err != nil {
		err = eh.Errorf("task: marshal error: %w", err)
	}
	return
}

// UnmarshalTaskError is the inverse of MarshalTaskError.
func UnmarshalTaskError(b []byte) (e taskerror.TaskError, err error) {
	e, err = buscodec.Decode[taskerror.TaskError](b)
	if err != nil {
		err = eh.Errorf("task: unmarshal error: %w", err)
	}
	return
}

// MarshalTaskCancel serialises a [taskcancel.TaskCancel]. The payload
// type lives in keelson/runtime/codec/taskcancel; this helper keeps
// the legacy task.MarshalTaskCancel entry point so existing callers
// remain valid after the type move.
func MarshalTaskCancel(c taskcancel.TaskCancel) (b []byte, err error) {
	b, err = buscodec.Encode(c)
	if err != nil {
		err = eh.Errorf("task: marshal cancel: %w", err)
	}
	return
}

// UnmarshalTaskCancel is the inverse of MarshalTaskCancel. Empty payload
// yields a zero TaskCancel without error — apps that publish "just cancel
// the task" with nil payload remain interoperable.
func UnmarshalTaskCancel(b []byte) (c taskcancel.TaskCancel, err error) {
	if len(b) == 0 {
		return
	}
	c, err = buscodec.Decode[taskcancel.TaskCancel](b)
	if err != nil {
		err = eh.Errorf("task: unmarshal cancel: %w", err)
	}
	return
}
