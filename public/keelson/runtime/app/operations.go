package app

import (
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opjson"
)

// The app operations contract (ADR-0269): an app declares the commands and
// queries it offers in its manifest, and the window host calls them per
// instance. The catalog is static, so a caller can discover what an app does
// before any window is open; what an instance adds is availability.

// OperationClassE is an operation's class (ADR-0269 §SD1).
type OperationClassE uint8

const (
	OperationClassUnspecified OperationClassE = 0
	// OperationClassQuery has no effect: a function of its arguments and a
	// snapshot of the instance, answered off the render goroutine.
	OperationClassQuery OperationClassE = 1
	// OperationClassExternalRead reads outside the app through a fixed,
	// bounded probe the app owns — never a statement the caller writes.
	OperationClassExternalRead OperationClassE = 2
	// OperationClassCommand changes state; it runs on the render goroutine
	// after the frame's widget values are written back.
	OperationClassCommand OperationClassE = 3
)

var AllOperationClasses = []OperationClassE{OperationClassQuery, OperationClassExternalRead, OperationClassCommand}

func (inst OperationClassE) String() (s string) {
	switch inst {
	case OperationClassQuery:
		s = "query"
	case OperationClassExternalRead:
		s = "external_read"
	case OperationClassCommand:
		s = "command"
	default:
		s = "unspecified"
	}
	return
}

// OperationEffectE is what an operation may change (ADR-0269 §SD5). A grant's
// mode is checked against it.
type OperationEffectE uint8

const (
	OperationEffectUnspecified OperationEffectE = 0
	// OperationEffectNone: queries and external reads.
	OperationEffectNone OperationEffectE = 1
	// OperationEffectView changes what a window shows, not what it holds: a
	// selection, a camera.
	OperationEffectView OperationEffectE = 2
	// OperationEffectDocument changes authored state: text, parameters,
	// signals, pane bindings and options.
	OperationEffectDocument OperationEffectE = 3
	// OperationEffectRun executes against a data source and produces a
	// result.
	OperationEffectRun OperationEffectE = 4
	// OperationEffectConsequential writes outside the app: pinning,
	// publishing, adjudication, export. Confirmed by the person every time.
	OperationEffectConsequential OperationEffectE = 5
)

var AllOperationEffects = []OperationEffectE{
	OperationEffectNone, OperationEffectView, OperationEffectDocument, OperationEffectRun, OperationEffectConsequential,
}

func (inst OperationEffectE) String() (s string) {
	switch inst {
	case OperationEffectNone:
		s = "none"
	case OperationEffectView:
		s = "view"
	case OperationEffectDocument:
		s = "document"
	case OperationEffectRun:
		s = "run"
	case OperationEffectConsequential:
		s = "consequential"
	default:
		s = "unspecified"
	}
	return
}

// ResourceSpec declares one unit of app state with its own revision: a
// document, a parameter, a pane binding.
type ResourceSpec struct {
	Name    string
	Summary string
}

// OperationSpec declares one operation (ADR-0269 §SD2).
type OperationSpec struct {
	// Name is lower snake_case, unique within the catalog.
	Name string
	// Version starts at 1 and moves when the arguments or the meaning
	// change.
	Version uint16
	// Summary is one line for the model and for people: what the operation
	// does, verb first.
	Summary string
	Class   OperationClassE
	Effect  OperationEffectE
	// Reads and Writes name resources of the catalog. Only a command writes.
	Reads  []string
	Writes []string
	// Args and Result are the Go types of the arguments and the result,
	// both structs; nil means none. The model's JSON and the bus's CBOR are
	// both derived from them (opjson, buscodec).
	Args   reflect.Type
	Result reflect.Type
	// Refs names argument fields that carry references — a result
	// reference or a data handle — rather than literal values.
	Refs []string
	// Follows names effects that follow from the operation, in prose: "a
	// parameter write reruns a Live query".
	Follows []string
	// Agents exposes the operation to agents. Off unless declared.
	Agents bool
	// Untrusted marks output that may carry content an attacker can
	// influence: cells, documents, titles.
	Untrusted bool
	// Gesture names the UI gesture that does the same; empty means there
	// is none.
	Gesture string
}

// OperationsCatalog is what an app declares in Manifest.Operations.
type OperationsCatalog struct {
	Resources  []ResourceSpec
	Operations []OperationSpec
}

var operationNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// ValidOperationName reports whether s may name an operation or a resource.
func ValidOperationName(s string) (ok bool) { return operationNameRe.MatchString(s) }

// Lookup returns the operation named name.
func (inst *OperationsCatalog) Lookup(name string) (spec OperationSpec, ok bool) {
	if inst == nil {
		return
	}
	for _, o := range inst.Operations {
		if o.Name == name {
			spec, ok = o, true
			return
		}
	}
	return
}

// CatalogError is why a catalog failed validation. Its message names the
// operation or resource, since it becomes the catalog's diagnostic.
type CatalogError struct {
	Operation string
	Resource  string
	Problem   string
}

func (inst *CatalogError) Error() (s string) {
	s = "operations: "
	if inst.Operation != "" {
		s += "operation " + inst.Operation + ": "
	}
	if inst.Resource != "" {
		s += "resource " + inst.Resource + ": "
	}
	s += inst.Problem
	return
}

// Validate returns why the catalog cannot be served, or nil.
func (inst *OperationsCatalog) Validate() (err error) {
	if inst == nil {
		return
	}
	resources := make(map[string]bool, len(inst.Resources))
	for _, r := range inst.Resources {
		if !ValidOperationName(r.Name) {
			return &CatalogError{Resource: r.Name, Problem: "invalid name; lower snake_case"}
		}
		if resources[r.Name] {
			return &CatalogError{Resource: r.Name, Problem: "declared twice"}
		}
		resources[r.Name] = true
	}
	if len(inst.Operations) == 0 {
		return &CatalogError{Problem: "a catalog declares no operation"}
	}
	names := make(map[string]bool, len(inst.Operations))
	for _, o := range inst.Operations {
		if problem := o.problem(resources); problem != "" {
			return &CatalogError{Operation: o.Name, Problem: problem}
		}
		if names[o.Name] {
			return &CatalogError{Operation: o.Name, Problem: "declared twice"}
		}
		names[o.Name] = true
	}
	return
}

// problem returns why an operation cannot be served, or "".
func (inst OperationSpec) problem(resources map[string]bool) (problem string) {
	switch {
	case !ValidOperationName(inst.Name):
		return "invalid name; lower snake_case"
	case inst.Version == 0:
		return "version starts at 1"
	case inst.Summary == "":
		return "empty summary"
	case !slices.Contains(AllOperationClasses, inst.Class):
		return "unspecified class"
	case !slices.Contains(AllOperationEffects, inst.Effect):
		return "unspecified effect"
	}
	if inst.Class == OperationClassCommand {
		if inst.Effect == OperationEffectNone {
			return "a command declares an effect"
		}
	} else {
		if inst.Effect != OperationEffectNone {
			return "only a command has an effect"
		}
		if len(inst.Writes) > 0 {
			return "only a command writes"
		}
	}
	if (inst.Effect == OperationEffectView || inst.Effect == OperationEffectDocument) && len(inst.Writes) == 0 {
		return "a view or document command names the resources it writes"
	}
	for _, r := range slices.Concat(inst.Reads, inst.Writes) {
		if !resources[r] {
			return "names the undeclared resource " + r
		}
	}
	for _, t := range []reflect.Type{inst.Args, inst.Result} {
		if t == nil {
			continue
		}
		if t.Kind() != reflect.Struct {
			return "arguments and results are structs, not " + t.String()
		}
		if _, err := opjson.Schema(t); err != nil {
			return err.Error()
		}
	}
	if len(inst.Refs) > 0 {
		if inst.Args == nil {
			return "references declared without arguments"
		}
		fieldNames, err := opjson.FieldNames(inst.Args)
		if err != nil {
			return err.Error()
		}
		for _, r := range inst.Refs {
			if !slices.Contains(fieldNames, r) {
				return "the reference " + r + " names no argument field"
			}
		}
	}
	return
}

// OperationsAppI is implemented by an app instance whose manifest declares
// a catalog. The window host asks for the handler once, after Mount.
type OperationsAppI interface {
	Operations() (h OperationsHandlerI)
}

// OperationsHandlerI serves one instance's catalog. Every method except a
// snapshot's Query runs on the render goroutine.
type OperationsHandlerI interface {
	// ResourceValue returns the current value of a declared resource as a
	// comparable value — the value itself, or a revision or digest of it.
	// The host compares values across the frame's write-back to bump
	// revisions (ADR-0269 §SD4).
	ResourceValue(name string) (v any)
	// Confined reports the window's label (ADR-0145, ADR-0269 §SD7): true
	// while any result it holds is confined. What its operations return
	// carries this label.
	Confined() (confined bool)
	// Restore puts back a value ResourceValue returned earlier, for undo
	// (ADR-0269 §SD8). It reports false for a resource it cannot restore —
	// one whose value is a digest, or that has no setter — and undo leaves
	// that resource alone.
	Restore(name string, v any) (ok bool)
	// Editing reports whether the person is editing a resource: its widget
	// has keyboard focus or received input in the last frame. A command to
	// it is a conflict (ADR-0269 §SD4).
	Editing(name string) (editing bool)
	// ApplyCommand runs a command. args and result are CBOR of the
	// declared types. Refuse with [OperationRefusal]; any other error
	// fails the call.
	ApplyCommand(call OperationCall, name string, args []byte) (result []byte, err error)
	// Snapshot captures what queries and availability read. The host takes
	// one after each command stage and answers queries from the latest
	// snapshot off the render goroutine.
	Snapshot() (s OperationsSnapshotI)
}

// OperationsSnapshotI is an immutable view of an instance; safe for use
// from any goroutine.
type OperationsSnapshotI interface {
	// Available reports whether an operation can run now, and why not.
	Available(name string) (ok bool, reason string)
	// Query answers a query or an external read. args and result are CBOR
	// of the declared types.
	Query(name string, args []byte) (result []byte, err error)
}

// OperationsGestureI is the capability a frame context offers an app whose
// catalog the host serves: a gesture of the person's that the catalog also
// exposes as a command goes through it, so it calls the same handler and
// enters the same log (ADR-0269 §SD8 "One path"). Call it from Frame.
type OperationsGestureI interface {
	OperationGesture(op string, args []byte) (result []byte, err error)
}

// OperationCall is what the host passes a handler with each command.
type OperationCall struct {
	// Writer names who asked: "person" for the app's own gestures routed
	// through the catalog, "task:<id>" for an agent's task.
	Writer string
	// Key is the caller's key for the call.
	Key string
	// Reason is the caller's one-line reason.
	Reason string
	// RefData is, per argument declared as a reference, the CBOR of the
	// result it names; the host resolved it, and it never reached the
	// model.
	RefData map[string][]byte
	// OnBehalfOf is set on an agent's call: the work the call starts is
	// agent-caused, runs under the app's agent limits, and carries this
	// context on every onward request it makes (ADR-0269 §SD6). nil on the
	// person's gestures.
	OnBehalfOf *OnBehalfOf
}

// OnBehalfOf is the context the host's dispatcher stamps on an agent's
// call; the callee reads it and cannot change what the host checks it
// against.
type OnBehalfOf struct {
	Task  string
	Epoch uint64
	// Principal is whose authority the task holds: the person.
	Principal string
	// Act is the chain the call came through: the person, the coordinator
	// window, the called window.
	Act []string
	// Destinations are what the grant lets agent-caused work reach.
	Destinations []string
}

// DelegationI is what a host service that reaches outside asks about an
// on-behalf-of context: is the task live at this epoch, and does its grant
// list the destination? The host's dispatcher implements it.
type DelegationI interface {
	AllowDestination(task string, epoch uint64, destination string) (ok bool, reason string)
}

// OperationRefusal is an error a handler returns to decline a call without
// failing it. Conflict marks a refusal the caller can resolve by reading
// again: the person is editing, or the state moved.
type OperationRefusal struct {
	Conflict bool
	Reason   string
}

func (inst *OperationRefusal) Error() string {
	if inst.Conflict {
		return "operation conflict: " + inst.Reason
	}
	return "operation refused: " + inst.Reason
}

// RefuseOperation declines a call: unavailable, or a precondition failed.
func RefuseOperation(reason string) (err error) {
	return &OperationRefusal{Reason: reason}
}

// ConflictOperation declines a call the caller can retry after reading
// again.
func ConflictOperation(reason string) (err error) {
	return &OperationRefusal{Conflict: true, Reason: reason}
}

// CapReachesOperationSubjects reports whether a NATS pattern can match an
// operation subject, app.{alias}.{instance}.op.{name} (ADR-0269 §SD3). Only
// the host publishes or subscribes there, so registration refuses an app
// declaring such a capability.
func CapReachesOperationSubjects(pattern string) (overlap bool) {
	template := []string{"app", "*", "*", "op", "*"}
	tokens := strings.Split(pattern, ".")
	for i, tok := range tokens {
		if tok == ">" {
			return i <= len(template)-1
		}
		if i >= len(template) {
			return false
		}
		if tok != "*" && template[i] != "*" && tok != template[i] {
			return false
		}
		// The instance token is numeric: a literal that is not cannot reach
		// the family, which is what keeps app.{id}.request.> apart from it.
		if i == 2 && tok != "*" && !numeric(tok) {
			return false
		}
	}
	return len(tokens) == len(template)
}

func numeric(s string) (ok bool) {
	if s == "" {
		return
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return
		}
	}
	ok = true
	return
}
