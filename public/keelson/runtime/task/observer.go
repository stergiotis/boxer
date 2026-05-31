//go:build llm_generated_opus47

package task

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskcancel"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskcreated"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskdone"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskerror"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskprogress"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// ObserverI is the consumer-side contract: a visitor that receives one
// callback per verb. WatchAll fans out a single task.> subscription into
// these per-verb calls. Implementations need not be goroutine-safe — the
// in-proc bus delivers messages on the publisher's goroutine, one at a
// time per subscription. M4 NATS-backed delivery uses one goroutine per
// subscription, so ordering across verbs of a single task is preserved
// even there.
type ObserverI interface {
	OnCreated(c taskcreated.TaskCreated)
	OnProgress(p taskprogress.TaskProgress)
	OnDone(d taskdone.TaskDone)
	OnError(e taskerror.TaskError)
	OnCancel(c taskcancel.TaskCancel)
}

// WatchAll subscribes to PatternAll on bus and demuxes by verb suffix
// into the observer's per-verb methods. Returns an unsubscribe function;
// callers MUST defer it (or call it on Mount/Unmount) to avoid a
// subscription leak when the observer goes out of scope.
//
// Decode errors are swallowed silently — an observer that wants visibility
// into malformed payloads should wrap WatchAll with its own bus subscription.
// The current consumers (UI status panel, M3 supervisor) treat undecodable
// frames as bus noise and do not need a recovery path.
func WatchAll(bus app.BusI, obs ObserverI) (unsubscribe func(), err error) {
	if bus == nil {
		err = eh.Errorf("task: watch: nil bus")
		return
	}
	if obs == nil {
		err = eh.Errorf("task: watch: nil observer")
		return
	}
	unsubscribe, err = bus.Subscribe(PatternAll, func(msg *app.Msg) {
		_, verb, ok := ParseSubject(msg.Subject)
		if !ok {
			return
		}
		switch verb {
		case VerbCreated:
			c, decErr := UnmarshalTaskCreated(msg.Payload)
			if decErr != nil {
				return
			}
			obs.OnCreated(c)
		case VerbProgress:
			p, decErr := UnmarshalTaskProgress(msg.Payload)
			if decErr != nil {
				return
			}
			obs.OnProgress(p)
		case VerbDone:
			d, decErr := UnmarshalTaskDone(msg.Payload)
			if decErr != nil {
				return
			}
			obs.OnDone(d)
		case VerbError:
			e, decErr := UnmarshalTaskError(msg.Payload)
			if decErr != nil {
				return
			}
			obs.OnError(e)
		case VerbCancel:
			c, decErr := UnmarshalTaskCancel(msg.Payload)
			if decErr != nil {
				return
			}
			obs.OnCancel(c)
		}
	})
	if err != nil {
		err = eh.Errorf("task: watch: subscribe: %w", err)
	}
	return
}

// RequestCancel publishes a TaskCancel on task.<id>.cancel. Used by UI
// cancel buttons and by supervisor abandoned-task cleanup. Returns the
// underlying publish error verbatim; callers that lack the CancelerCaps
// receive ErrPermissionViolation.
func RequestCancel(bus app.BusI, id TaskIdT, reason string) (err error) {
	if bus == nil {
		err = eh.Errorf("task: cancel: nil bus")
		return
	}
	if id == "" {
		err = eh.Errorf("task: cancel: empty id")
		return
	}
	msg := taskcancel.TaskCancel{
		TaskId: string(id),
		Reason: reason,
		At:     time.Now().UTC(),
	}
	var b []byte
	b, err = MarshalTaskCancel(msg)
	if err != nil {
		return
	}
	err = bus.Publish(SubjectCancel(id), b)
	if err != nil {
		err = eh.Errorf("task: cancel: publish: %w", err)
	}
	return
}
