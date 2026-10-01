// Package opwire is what the host's dispatcher and the window host exchange
// about operation calls (ADR-0269 §SD3, §SD4): the subject family an
// operation is addressed on, the phases a call moves through, and the wire
// forms of a call and its outcome. It sits apart from both so neither
// imports the other.
package opwire

import (
	"runtime"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// DispatcherAppId is the only sender the window host accepts a call from:
// the host's runtime.agent service.
const DispatcherAppId app.AppIdT = "runtime.agent"

// HostOpsAppId is the window host's identity on the operation subjects.
const HostOpsAppId app.AppIdT = "runtime.windowhost.ops"

// Pattern is the window host's subscription: every operation of every
// instance.
const Pattern = "app.*.*.op.*"

// Subject addresses an operation of one instance:
// app.{alias}.{instance}.op.{name}. The instance token is numeric, so the
// family cannot collide with app.{id}.event.{name} or app.{id}.request.{name}.
func Subject(alias string, instance uint64, op string) (subject string) {
	return "app." + alias + "." + strconv.FormatUint(instance, 10) + ".op." + op
}

// ParseSubject is the inverse of [Subject].
func ParseSubject(subject string) (alias string, instance uint64, op string, ok bool) {
	parts := strings.Split(subject, ".")
	if len(parts) != 5 || parts[0] != "app" || parts[3] != "op" {
		return
	}
	n, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return
	}
	alias, instance, op, ok = parts[1], n, parts[4], true
	return
}

// MatchesOperationSubjects reports whether a NATS pattern can match a
// subject of the family; registration refuses such a capability for any app
// (ADR-0269 §SD3).
func MatchesOperationSubjects(pattern string) (overlap bool) {
	return app.CapReachesOperationSubjects(pattern)
}

// PhaseE is where a call stands (ADR-0269 §SD4).
type PhaseE uint8

const (
	PhaseUnspecified PhaseE = 0
	// Dispatcher stage.
	PhaseDenied        PhaseE = 1
	PhaseInputRequired PhaseE = 2
	PhaseRefused       PhaseE = 3
	PhaseProposed      PhaseE = 4
	PhaseAccepted      PhaseE = 5
	PhaseRejected      PhaseE = 6
	PhaseStale         PhaseE = 7
	// Queue stage.
	PhaseApplied   PhaseE = 8
	PhaseRendered  PhaseE = 9
	PhaseConflict  PhaseE = 10
	PhaseExpired   PhaseE = 11
	PhaseCancelled PhaseE = 12
	// Work stage, and the end of a query.
	PhaseRunning   PhaseE = 13
	PhaseCompleted PhaseE = 14
	PhaseFailed    PhaseE = 15
)

var AllPhases = []PhaseE{
	PhaseDenied, PhaseInputRequired, PhaseRefused, PhaseProposed, PhaseAccepted, PhaseRejected, PhaseStale,
	PhaseApplied, PhaseRendered, PhaseConflict, PhaseExpired, PhaseCancelled,
	PhaseRunning, PhaseCompleted, PhaseFailed,
}

func (inst PhaseE) String() (s string) {
	switch inst {
	case PhaseDenied:
		s = "denied"
	case PhaseInputRequired:
		s = "input_required"
	case PhaseRefused:
		s = "refused"
	case PhaseProposed:
		s = "proposed"
	case PhaseAccepted:
		s = "accepted"
	case PhaseRejected:
		s = "rejected"
	case PhaseStale:
		s = "stale"
	case PhaseApplied:
		s = "applied"
	case PhaseRendered:
		s = "rendered"
	case PhaseConflict:
		s = "conflict"
	case PhaseExpired:
		s = "expired"
	case PhaseCancelled:
		s = "cancelled"
	case PhaseRunning:
		s = "running"
	case PhaseCompleted:
		s = "completed"
	case PhaseFailed:
		s = "failed"
	default:
		s = "unspecified"
	}
	return
}

// Final reports whether no later phase can follow. Applied is not final:
// rendered follows it.
func (inst PhaseE) Final() (final bool) {
	switch inst {
	case PhaseDenied, PhaseRefused, PhaseRejected, PhaseStale, PhaseRendered, PhaseConflict,
		PhaseExpired, PhaseCancelled, PhaseCompleted, PhaseFailed:
		final = true
	}
	return
}

// Outcome is a call's state as the window host reports it.
type Outcome struct {
	Phase  PhaseE `json:"phase"`
	Reason string `json:"reason,omitempty"`
	// AsOf names the snapshot a query read, or the frame a command was
	// applied in.
	AsOf uint64 `json:"as_of,omitempty"`
	// Revisions are, for a query, those of the resources it read as of the
	// snapshot; for a command, those of the resources it wrote after it
	// applied; for a conflict, the current ones.
	Revisions map[string]uint64 `json:"revisions,omitempty"`
	// Result is CBOR of the operation's declared result type.
	Result []byte `json:"result,omitempty"`
	// Seq is, for a query, the instance's log sequence as of its snapshot:
	// a later change was not in what it read.
	Seq uint64 `json:"seq,omitempty"`
}

// wireVersion versions [CallRequest] and [CallReply].
const WireVersion uint8 = 1

// CallRequest is the envelope on an operation subject.
type CallRequest struct {
	V uint8 `json:"v"`
	// CallId is minted by the dispatcher, unique within the host.
	CallId string `json:"call_id"`
	// Args is CBOR of the operation's declared argument type.
	Args []byte `json:"args,omitempty"`
	// Expects are the revisions a command expects of the resources it
	// writes.
	Expects map[string]uint64 `json:"expects,omitempty"`
	// Writer names the task: "task:<id>".
	Writer string `json:"writer"`
	Key    string `json:"key,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// CallReply answers a [CallRequest]: a command's acceptance into the
// queue, or a query's result.
type CallReply struct {
	V       uint8   `json:"v"`
	Outcome Outcome `json:"outcome"`
}

// InstanceInfo is one open window as the dispatcher sees it.
type InstanceInfo struct {
	App   app.AppIdT
	Alias string
	Key   uint64
	Title string
	// Ops is true when the instance serves its app's catalog.
	Ops bool
}

// WriterTask names a task as a writer.
func WriterTask(task string) (writer string) { return "task:" + task }

// The writers that are not tasks.
const (
	// WriterPerson is the person: a write-back from a widget, or a gesture
	// the app routes through its catalog.
	WriterPerson = "person"
	// WriterApp is the app's own frame logic: a result landing, a rerun.
	WriterApp = "app"
)

// CaptureStatus is where a capture of a window stands (ADR-0269 §SD11).
type CaptureStatus struct {
	Phase     PhaseE
	Reason    string
	Path      string
	MediaType string
	Bytes     int64
}

// GoroutineId returns the id of the calling goroutine, read from the
// header runtime.Stack writes ("goroutine N [...]"); 0 if it cannot be
// parsed. The window host records the render goroutine's id with it, and the
// dispatcher refuses a request made on that goroutine: an in-process bus
// handler runs on its requester's goroutine, so a request from a Frame
// would wait on the frame it blocks (ADR-0269 §SD3).
func GoroutineId() (id uint64) {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	s := string(buf[:n])
	s, ok := strings.CutPrefix(s, "goroutine ")
	if !ok {
		return
	}
	if i := strings.IndexByte(s, ' '); i > 0 {
		id, _ = strconv.ParseUint(s[:i], 10, 64)
	}
	return
}
