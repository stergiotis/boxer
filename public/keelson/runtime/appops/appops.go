// Package appops is how an app declares its operations catalog (ADR-0269
// §SD2) and the handlers behind it in one place. A [Set] is declared once per
// app type, at package level; its [Set.Catalog] goes into the manifest and
// [Set.Bind] serves one instance:
//
//	var ops = appops.NewSet(func(inst *App) snap { return inst.snapshot() })
//
//	func init() {
//		ops.Resource("doc", "the document text", func(inst *App) any { return inst.doc })
//		appops.Command(ops, app.OperationSpec{Name: "set_doc", Version: 1,
//			Summary: "replace the document", Effect: app.OperationEffectDocument,
//			Writes: []string{"doc"}, Agents: true},
//			func(inst *App, call app.OperationCall, in setDocArgs) (appops.None, error) { … })
//	}
//
// Argument and result types come from the handler's signature, so the
// catalog cannot name one type and the handler decode another. Commands run
// on the render goroutine with the app instance; queries run off it, with a
// snapshot the host took after the last command stage.
package appops

import (
	"maps"
	"reflect"
	"slices"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// None is the argument or result type of an operation that has none.
type None struct{}

var typeNone = reflect.TypeFor[None]()

type commandFn[A any] func(inst A, call app.OperationCall, args []byte) (result []byte, err error)
type queryFn[S any] func(snap S, args []byte) (result []byte, err error)

// Set is an app's catalog with its handlers. A is the app instance type, S
// the snapshot type queries read.
type Set[A any, S any] struct {
	resources []app.ResourceSpec
	values    map[string]func(inst A) any
	editing   map[string]func(inst A) bool
	restore   map[string]func(inst A, v any) bool
	ops       []app.OperationSpec
	commands  map[string]commandFn[A]
	queries   map[string]queryFn[S]
	avail     map[string]func(snap S) (ok bool, reason string)
	snapshot  func(inst A) S
}

// NewSet starts a catalog. snapshot captures what queries read; it runs on
// the render goroutine and must return a value queries can read from any
// goroutine — a copy, never a pointer into live state.
func NewSet[A any, S any](snapshot func(inst A) S) (s *Set[A, S]) {
	s = &Set[A, S]{
		values:   make(map[string]func(A) any),
		editing:  make(map[string]func(A) bool),
		restore:  make(map[string]func(A, any) bool),
		commands: make(map[string]commandFn[A]),
		queries:  make(map[string]queryFn[S]),
		avail:    make(map[string]func(S) (bool, string)),
		snapshot: snapshot,
	}
	return
}

// Resource declares a resource and how to read its current value: the value
// itself when it is comparable and cheap to compare, else a revision or a
// digest of it.
func (inst *Set[A, S]) Resource(name string, summary string, value func(inst A) any) *Set[A, S] {
	inst.resources = append(inst.resources, app.ResourceSpec{Name: name, Summary: summary})
	inst.values[name] = value
	return inst
}

// Editing declares how to tell that the person is editing a resource,
// usually from the bound widget's response flags (see [WidgetEditing]).
// Without it, a resource is never reported as being edited.
func (inst *Set[A, S]) Editing(name string, fn func(inst A) bool) *Set[A, S] {
	inst.editing[name] = fn
	return inst
}

// Restorable declares how to put back a value of a resource that its
// value function returned earlier; undo restores only such resources. fn
// reports false when the value does not fit.
func (inst *Set[A, S]) Restorable(name string, fn func(inst A, v any) bool) *Set[A, S] {
	inst.restore[name] = fn
	return inst
}

// Available declares when an operation can run; without it, an operation
// is always available.
func (inst *Set[A, S]) Available(name string, fn func(snap S) (ok bool, reason string)) *Set[A, S] {
	inst.avail[name] = fn
	return inst
}

func typeOrNil[T any]() (t reflect.Type) {
	t = reflect.TypeFor[T]()
	if t == typeNone {
		t = nil
	}
	return
}

// Command declares a command and its handler. Class defaults to command;
// Args and Result come from In and Out.
func Command[A any, S any, In any, Out any](set *Set[A, S], spec app.OperationSpec, fn func(inst A, call app.OperationCall, in In) (out Out, err error)) {
	if spec.Class == app.OperationClassUnspecified {
		spec.Class = app.OperationClassCommand
	}
	spec.Args, spec.Result = typeOrNil[In](), typeOrNil[Out]()
	set.ops = append(set.ops, spec)
	set.commands[spec.Name] = func(inst A, call app.OperationCall, args []byte) (result []byte, err error) {
		var in In
		in, err = decodeArgs[In](spec.Name, args)
		if err != nil {
			return
		}
		var out Out
		out, err = fn(inst, call, in)
		if err != nil {
			return
		}
		result, err = encodeResult(spec.Name, out)
		return
	}
}

// Query declares a query or an external read and its handler. Class
// defaults to query and Effect to none.
func Query[A any, S any, In any, Out any](set *Set[A, S], spec app.OperationSpec, fn func(snap S, in In) (out Out, err error)) {
	if spec.Class == app.OperationClassUnspecified {
		spec.Class = app.OperationClassQuery
	}
	if spec.Effect == app.OperationEffectUnspecified {
		spec.Effect = app.OperationEffectNone
	}
	spec.Args, spec.Result = typeOrNil[In](), typeOrNil[Out]()
	set.ops = append(set.ops, spec)
	set.queries[spec.Name] = func(snap S, args []byte) (result []byte, err error) {
		var in In
		in, err = decodeArgs[In](spec.Name, args)
		if err != nil {
			return
		}
		var out Out
		out, err = fn(snap, in)
		if err != nil {
			return
		}
		result, err = encodeResult(spec.Name, out)
		return
	}
}

func decodeArgs[In any](name string, args []byte) (in In, err error) {
	if len(args) == 0 {
		return
	}
	in, err = buscodec.Decode[In](args)
	if err != nil {
		err = eb.Build().Str("operation", name).Errorf("appops: decode arguments: %w", err)
	}
	return
}

func encodeResult[Out any](name string, out Out) (result []byte, err error) {
	result, err = buscodec.Encode(out)
	if err != nil {
		err = eb.Build().Str("operation", name).Errorf("appops: encode result: %w", err)
	}
	return
}

// Catalog returns the catalog for the manifest. Each call returns a fresh
// copy.
func (inst *Set[A, S]) Catalog() (c *app.OperationsCatalog) {
	c = &app.OperationsCatalog{
		Resources:  slices.Clone(inst.resources),
		Operations: slices.Clone(inst.ops),
	}
	return
}

// Names lists the operations declared, in declaration order.
func (inst *Set[A, S]) Names() (names []string) {
	for _, o := range inst.ops {
		names = append(names, o.Name)
	}
	return
}

// Bind serves the catalog for one instance; the instance returns it from
// its Operations method.
func (inst *Set[A, S]) Bind(a A) (h app.OperationsHandlerI) {
	h = &bound[A, S]{set: inst, inst: a}
	return
}

type bound[A any, S any] struct {
	set  *Set[A, S]
	inst A
}

func (inst *bound[A, S]) ResourceValue(name string) (v any) {
	if fn, ok := inst.set.values[name]; ok {
		v = fn(inst.inst)
	}
	return
}

func (inst *bound[A, S]) Restore(name string, v any) (ok bool) {
	if fn, declared := inst.set.restore[name]; declared {
		ok = fn(inst.inst, v)
	}
	return
}

func (inst *bound[A, S]) Editing(name string) (editing bool) {
	if fn, ok := inst.set.editing[name]; ok {
		editing = fn(inst.inst)
	}
	return
}

func (inst *bound[A, S]) ApplyCommand(call app.OperationCall, name string, args []byte) (result []byte, err error) {
	fn, ok := inst.set.commands[name]
	if !ok {
		err = eb.Build().Str("operation", name).Errorf("appops: no such command")
		return
	}
	result, err = fn(inst.inst, call, args)
	return
}

func (inst *bound[A, S]) Snapshot() (s app.OperationsSnapshotI) {
	s = &snapshot[A, S]{set: inst.set, snap: inst.set.snapshot(inst.inst)}
	return
}

type snapshot[A any, S any] struct {
	set  *Set[A, S]
	snap S
}

func (inst *snapshot[A, S]) Available(name string) (ok bool, reason string) {
	fn, declared := inst.set.avail[name]
	if !declared {
		ok = true
		return
	}
	ok, reason = fn(inst.snap)
	return
}

func (inst *snapshot[A, S]) Query(name string, args []byte) (result []byte, err error) {
	fn, ok := inst.set.queries[name]
	if !ok {
		err = eb.Build().Str("operation", name).Errorf("appops: no such query")
		return
	}
	if avail, reason := inst.Available(name); !avail {
		err = app.RefuseOperation(reason)
		return
	}
	result, err = fn(inst.snap, args)
	return
}

// ResourceNames lists the declared resources in declaration order.
func (inst *Set[A, S]) ResourceNames() (names []string) {
	names = slices.Sorted(maps.Keys(inst.values))
	return
}

// Gesture applies one of the app's commands as the person, from Frame,
// through the host (ADR-0269 §SD8 "One path"): the handler runs at once,
// and the change is logged with the person as writer. Where the host serves
// no catalog for the window, the call is refused and the app should apply
// the change itself.
func Gesture[In any, Out any](ctx app.FrameContextI, op string, in In) (out Out, err error) {
	g, ok := ctx.(app.OperationsGestureI)
	if !ok {
		err = app.RefuseOperation("the frame context offers no gesture path")
		return
	}
	args, err := buscodec.Encode(in)
	if err != nil {
		err = eb.Build().Str("operation", op).Errorf("appops: encode arguments: %w", err)
		return
	}
	raw, err := g.OperationGesture(op, args)
	if err != nil || len(raw) == 0 {
		return
	}
	out, err = buscodec.Decode[Out](raw)
	if err != nil {
		err = eb.Build().Str("operation", op).Errorf("appops: decode result: %w", err)
	}
	return
}
