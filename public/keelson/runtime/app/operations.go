package app

import (
	"bytes"
	"context"
	"reflect"
	"regexp"
	"slices"
	"strconv"
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

// OperationRebuildE is how a resource a command writes comes back after
// the process that held it is gone (ADR-0315 §SD4): what a resumed agent
// may rely on, and what resume tells it is lost.
type OperationRebuildE uint8

const (
	OperationRebuildUnspecified OperationRebuildE = 0
	// OperationRebuildDurable: survives a restart (facts, captured runs).
	OperationRebuildDurable OperationRebuildE = 1
	// OperationRebuildRefold: rebuilt by re-applying the latest journaled
	// set-style command (a buffer, parameters, signals, bindings).
	OperationRebuildRefold OperationRebuildE = 2
	// OperationRebuildReplay: rebuilt by re-execution, its sources of
	// nondeterminism declared.
	OperationRebuildReplay OperationRebuildE = 3
	// OperationRebuildVolatile: lost; resume tells the model (results,
	// result ids, task refs).
	OperationRebuildVolatile OperationRebuildE = 4
)

var AllOperationRebuilds = []OperationRebuildE{OperationRebuildDurable, OperationRebuildRefold, OperationRebuildReplay, OperationRebuildVolatile}

func (inst OperationRebuildE) String() (s string) {
	switch inst {
	case OperationRebuildDurable:
		s = "durable"
	case OperationRebuildRefold:
		s = "refold"
	case OperationRebuildReplay:
		s = "replay"
	case OperationRebuildVolatile:
		s = "volatile"
	default:
		s = "unspecified"
	}
	return
}

// OperationForkE is how an operation behaves under fan-out (ADR-0315
// §SD5): explore read-only, commit once.
type OperationForkE uint8

const (
	OperationForkUnspecified OperationForkE = 0
	// OperationForkReadShared: queries fan out on one instance.
	OperationForkReadShared OperationForkE = 1
	// OperationForkPerInstance: isolate by opening a new instance from an
	// explicit configuration, never from an ambient workingset.
	OperationForkPerInstance OperationForkE = 2
	// OperationForkNone: not forked.
	OperationForkNone OperationForkE = 3
)

var AllOperationForks = []OperationForkE{OperationForkReadShared, OperationForkPerInstance, OperationForkNone}

func (inst OperationForkE) String() (s string) {
	switch inst {
	case OperationForkReadShared:
		s = "read-shared"
	case OperationForkPerInstance:
		s = "per-instance"
	case OperationForkNone:
		s = "none"
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
	// AnyRevision is a command whose effect does not depend on what it
	// writes having stayed as it was read, such as asking for a refill: it
	// names no base revision and no write of another moves it to a conflict.
	AnyRevision bool
	// Args and Result are the Go types of the arguments and the result,
	// both structs; nil means none. The model's JSON and the bus's CBOR are
	// both derived from them (opjson, buscodec).
	Args   reflect.Type
	Result reflect.Type
	// Refs names argument fields that carry references — a result
	// reference or a data handle — rather than literal values.
	Refs []string
	// Follows names effects that follow from the operation, in prose: "a
	// parameter write reruns a Live query". A Direct operation's tool
	// description carries its leading lines, up to a bound, in every
	// request: put first what a model needs on every call.
	Follows []string
	// Agents exposes the operation to agents. Off unless declared.
	Agents bool
	// Direct offers the operation to an agent's model as a tool of its own,
	// named by the app and the operation, whether or not a window of the
	// app is open: a call to it asks for access to the app and opens a
	// window of it when the task has neither, then calls the operation
	// there. It suits the one or two operations an app is reached for, such
	// as running a query. Only an operation Agents exposes is direct, and a
	// catalog declares at most MaxDirectOperations.
	Direct bool
	// Untrusted marks output that may carry content an attacker can
	// influence: cells, documents, titles.
	Untrusted bool
	// Gesture names the UI gesture that does the same; empty means there
	// is none.
	Gesture string
	// Consent lets a consequential command run under the person's
	// standing consent instead of a confirmation per call; zero means
	// every call is confirmed.
	Consent OperationConsent
	// The stateful-tool contract (ADR-0315): Rebuild is how what the
	// command writes comes back after the process is gone (§SD4), Fork how
	// the operation behaves under fan-out (§SD5), Async that the command
	// stays running after it applies until its handler says the work
	// settled (ADR-0358 §SD1, [OperationsAsyncI]), and DryRun names the
	// catalog's query that runs the command's pure core without committing
	// (§SD7). Rebuild, Fork and DryRun are declared, not enforced:
	// unspecified is allowed and shown as such.
	Rebuild OperationRebuildE
	Fork    OperationForkE
	Async   bool
	DryRun  string
	// Link is the codec that spells the operation's arguments in the tail
	// of a `boxer:` link (ADR-0409 §SD3); nil means no link
	// names the operation. Its presence is the opt-in.
	Link LinkCodecI
	// LinkStage names a document-effect command of the same catalog, with
	// the same argument type, that puts a run operation's arguments in
	// place without running them; an authorless link calls it instead of
	// the run (ADR-0409 §SD5).
	LinkStage string
}

// linkCodecProblem round-trips a codec's examples (ADR-0409
// §SD3): each formats to a tail a link carries, parses back to the same
// arguments, and formats to the same tail again. A codec that fails spells
// one call two ways or reads back another, so its catalog is withdrawn at
// registration. applink.CheckCodec is the same check for a kind's codec.
func linkCodecProblem(codec LinkCodecI) (problem string) {
	examples := codec.LinkExamples()
	if len(examples) == 0 {
		return "a link codec carries at least one example"
	}
	for _, ex := range examples {
		tail, err := codec.FormatLink(ex)
		if err != nil {
			return "the link codec does not format its example: " + err.Error()
		}
		if !linkTailCarried(tail) {
			return "the link codec formats a tail a link cannot carry: " + tail
		}
		back, err := codec.ParseLink(tail)
		if err != nil {
			return "the link codec does not read back its example: " + err.Error()
		}
		if !bytes.Equal(back, ex) {
			return "the link codec reads its example back as other arguments: " + tail
		}
		if again, err := codec.FormatLink(back); err != nil || again != tail {
			return "the link codec spells its example two ways: " + tail
		}
	}
	return
}

// linkTailCarried says a tail is one a link carries: empty, or starting
// with '/', '?' or '#', in printable ASCII.
func linkTailCarried(tail string) bool {
	if tail == "" {
		return true
	}
	if c := tail[0]; c != '/' && c != '?' && c != '#' {
		return false
	}
	for i := 0; i < len(tail); i++ {
		if tail[i] <= 0x20 || tail[i] >= 0x7f {
			return false
		}
	}
	return true
}

// LinkCodecI spells one operation's arguments — or one launch kind's config
// — as the tail of a `boxer:` link, and reads them back (ADR-0409
// §SD3). Arguments cross it as their CBOR (buscodec), the
// encoding every call carries. A codec is a pure function of its input:
// no I/O, no state, bounded work for a bounded input, and strict — an
// unknown key or a malformed value is an error, never a default. appops
// builds one over a typed argument struct.
type LinkCodecI interface {
	// FormatLink spells args as a tail: empty, or starting with '/', '?'
	// or '#'.
	FormatLink(args []byte) (tail string, err error)
	// ParseLink reads a tail back into the arguments' CBOR.
	ParseLink(tail string) (args []byte, err error)
	// LinkExamples are argument values, as CBOR, that the codec spells; a
	// conformance test round-trips each, and a coordinator's describe
	// shows them as links.
	LinkExamples() (examples [][]byte)
}

// OperationConsent names how a task's grant covers a consequential call
// (ADR-0288 §SD4): a grant destination "<Class>:<prefix>",
// approved by the person as any destination is, covers a call whose
// argument Arg is a string starting with the non-empty prefix. A covered
// call is applied as the task's mode applies a document command, and its
// record names the destination that admitted it.
type OperationConsent struct {
	// Class is the destination class, one of ConsentClasses: what the
	// person consents to is the platform's to define, never the app's.
	Class string
	// Arg is the argument field, as the model's JSON names it, whose value
	// the prefix is matched against.
	Arg string
}

// ConsentClassPublish covers publishing ad-hoc data — a bundle, or
// datasets such as a projection's — under the name the consent's argument
// carries (ADR-0288 §SD4 and its update of 2026-10-09).
const ConsentClassPublish = "publish"

// ConsentClasses are the consent classes a catalog may declare, each with
// the one meaning a grant destination of that class consents to. A class
// is a platform decision: an app that could name its own would let one
// approved destination cover whatever any app called by that name.
var ConsentClasses = map[string]string{
	ConsentClassPublish: "publish ad-hoc data — a bundle or datasets — under a name starting with the prefix",
}

// Pattern is the destination as a person or a model writes it in a
// grant: "publish:<bundle prefix>"; empty when there is no consent.
func (inst OperationConsent) Pattern() (pattern string) {
	if inst == (OperationConsent{}) {
		return ""
	}
	return inst.Destination("<" + inst.Arg + " prefix>")
}

// Destination is the grant destination for prefix under this consent.
func (inst OperationConsent) Destination(prefix string) (destination string) {
	return inst.Class + ":" + prefix
}

// OperationsCatalog is what an app declares in Manifest.Operations.
type OperationsCatalog struct {
	Resources  []ResourceSpec
	Operations []OperationSpec
}

// MaxDirectOperations bounds the direct operations of one catalog: each is
// a tool in every request a model is sent, whether the app is used or not.
const MaxDirectOperations = 2

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
	classes := make(map[string]OperationClassE, len(inst.Operations))
	direct := 0
	for _, o := range inst.Operations {
		if problem := o.problem(resources); problem != "" {
			return &CatalogError{Operation: o.Name, Problem: problem}
		}
		if names[o.Name] {
			return &CatalogError{Operation: o.Name, Problem: "declared twice"}
		}
		names[o.Name] = true
		classes[o.Name] = o.Class
		if o.Direct {
			direct++
			if direct > MaxDirectOperations {
				return &CatalogError{Operation: o.Name, Problem: "a catalog declares at most " + strconv.Itoa(MaxDirectOperations) + " direct operations"}
			}
		}
	}
	for _, o := range inst.Operations {
		// A dry-run twin is a query of the same catalog that takes the
		// command's arguments (ADR-0315 §SD7).
		if o.DryRun == "" {
			continue
		}
		if classes[o.DryRun] != OperationClassQuery {
			return &CatalogError{Operation: o.Name, Problem: "dry_run names no query of the catalog: " + o.DryRun}
		}
		twin, _ := inst.Lookup(o.DryRun)
		if twin.Args != o.Args {
			return &CatalogError{Operation: o.Name, Problem: "dry_run twin " + o.DryRun + " takes other arguments than the command"}
		}
	}
	for _, o := range inst.Operations {
		// A link stage is a document command of the same catalog that takes
		// the run's arguments (ADR-0409 §SD3).
		if o.LinkStage == "" {
			continue
		}
		stage, ok := inst.Lookup(o.LinkStage)
		if !ok || stage.Class != OperationClassCommand || stage.Effect != OperationEffectDocument {
			return &CatalogError{Operation: o.Name, Problem: "link_stage names no document command of the catalog: " + o.LinkStage}
		}
		if stage.Args != o.Args {
			return &CatalogError{Operation: o.Name, Problem: "link_stage " + o.LinkStage + " takes other arguments than the run"}
		}
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
	case inst.Direct && !inst.Agents:
		return "a direct operation is a tool of an agent's model; it declares Agents"
	}
	// Links (ADR-0409 §SD3): nothing that writes outside the app
	// is reachable from a click on someone else's text, a reference names
	// something a URL cannot carry, and only a run operation is staged.
	if inst.Link != nil {
		if inst.Class != OperationClassCommand {
			return "only a command takes a link codec: a query's answer would have nowhere to go"
		}
		if inst.Effect == OperationEffectConsequential {
			return "a consequential operation takes no link codec"
		}
		if len(inst.Refs) > 0 {
			return "an operation whose arguments carry references takes no link codec"
		}
		if problem := linkCodecProblem(inst.Link); problem != "" {
			return problem
		}
	}
	if inst.LinkStage != "" && (inst.Link == nil || inst.Effect != OperationEffectRun) {
		return "only a run operation with a link codec names a link stage"
	}
	// The stateful-tool contract (ADR-0315 §SD4–§SD7): each field is one
	// of its enumerated values or unspecified; a rebuild class is a
	// command's and describes what it writes; a query fans out read-shared
	// and a command never does (§SD5).
	if inst.Rebuild != OperationRebuildUnspecified && !slices.Contains(AllOperationRebuilds, inst.Rebuild) {
		return "rebuild class out of range"
	}
	if inst.Fork != OperationForkUnspecified && !slices.Contains(AllOperationForks, inst.Fork) {
		return "fork class out of range"
	}
	if inst.Class == OperationClassCommand {
		if inst.Effect == OperationEffectNone {
			return "a command declares an effect"
		}
		if inst.Rebuild != OperationRebuildUnspecified && len(inst.Writes) == 0 {
			return "a rebuild class describes what the command writes; it writes nothing"
		}
		if inst.Fork == OperationForkReadShared {
			return "a command is not read-shared under fan-out; explore read-only, commit once"
		}
	} else {
		if inst.Effect != OperationEffectNone {
			return "only a command has an effect"
		}
		if len(inst.Writes) > 0 {
			return "only a command writes"
		}
		if inst.Async || inst.Rebuild != OperationRebuildUnspecified || inst.DryRun != "" {
			return "only a command is asynchronous, rebuilds, or has a dry-run twin"
		}
		if inst.Fork == OperationForkPerInstance {
			return "only a command isolates per instance under fan-out"
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
	if inst.Consent != (OperationConsent{}) {
		switch {
		case inst.Effect != OperationEffectConsequential:
			return "only a consequential command declares a consent"
		case ConsentClasses[inst.Consent.Class] == "":
			return "a consent's class is not one the platform defines: " + inst.Consent.Class
		case inst.Args == nil:
			return "a consent names an argument, and the command takes none"
		}
		fieldNames, err := opjson.FieldNames(inst.Args)
		if err != nil {
			return err.Error()
		}
		if !slices.Contains(fieldNames, inst.Consent.Arg) {
			return "the consent's argument " + inst.Consent.Arg + " names no argument field"
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
	// revisions (ADR-0269 §SD4). The value changes only on the render
	// goroutine, inside a frame: a value set by a background job is
	// latched in the app's Frame, since a change that appears between
	// frames is the person's.
	ResourceValue(name string) (v any)
	// Confined reports the window's label (ADR-0145, ADR-0269 §SD7): true
	// while any result it holds is confined. What its operations return
	// carries this label, for every reader unless the handler also says
	// for whom it does not apply (OperationsLabelForI).
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

// OperationsAsyncI is implemented by a handler whose catalog declares an
// Async command (ADR-0358 §SD1). The host asks once per frame, after the
// app's Frame, for each such call still running; it runs on the render
// goroutine, where a frame-driven app already looks at its own state. A
// call settles with result, CBOR of the declared result type, or with err
// as a refusal or a failure. args are the call's, as the command took them.
type OperationsAsyncI interface {
	SettleAsync(call OperationCall, name string, args []byte) (settled bool, result []byte, err error)
}

// OperationsSnapshotI is an immutable view of an instance; safe for use
// from any goroutine.
type OperationsSnapshotI interface {
	// Available reports whether an operation can run now, and why not.
	Available(name string) (ok bool, reason string)
	// Query answers a query. args and result are CBOR of the declared
	// types.
	Query(name string, args []byte) (result []byte, err error)
	// ExternalRead answers an external read. It takes the call because the
	// probe it starts is agent-caused work and checks the on-behalf-of
	// context against the app's agent limits (ADR-0269 §SD6). confined is
	// set when what the probe read is confined although the window is not.
	ExternalRead(call OperationCall, name string, args []byte) (result []byte, confined bool, err error)
}

// OperationsLabelForI is implemented by a handler whose window's label
// depends on who reads it: the window holds sealed content that is
// unlabelled for one task alone, the one whose own work it is. A handler
// without it has one label for every reader, its Confined.
type OperationsLabelForI interface {
	// LabelFor reports the window's label with the reader left open.
	// confined is the label for every reader, as Confined reports it;
	// clearFor, when set, names the one task for which the window is not
	// labelled although confined is true. The host stamps an outcome of
	// that task's call unlabelled; a call of any other task, a capture of
	// the window, its title and its row in a listing take confined. It
	// runs on the render goroutine, as Confined does.
	LabelFor() (confined bool, clearFor string)
}

// ConfinedResultI is implemented by an external read's result type whose
// value may carry confined content (ADR-0145) the window's own label does
// not cover: the schema of a sealed dataset it has not read, say. The host
// labels the outcome confined when it reports true (ADR-0269 §SD7).
type ConfinedResultI interface {
	ResultConfined() (confined bool)
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
	// Link is set on a call a followed link made (ADR-0409
	// §SD5): who wrote the link, by the label of the text it was found in,
	// and where it was clicked. A handler marks what such a call writes,
	// when Level is 2 or above, as it marks what a task wrote. nil on
	// every other call.
	Link *LinkOrigin

	ctx context.Context
}

// LinkOrigin is the author of a followed link and the surface it was
// clicked on (ADR-0409 §SD5). Level and Tier are the ADR-0311
// label of the text that carried the link (agentrun.LevelE and TierE).
type LinkOrigin struct {
	Level   uint8
	Tier    uint8
	Surface AppIdT
	// Digest names the resolved link, as the follow record does.
	Digest string
}

// LinkMarked says the call was made by a link whose author is not the
// person or the system: what it writes is marked as a task's is.
func (inst OperationCall) LinkMarked() (marked bool) {
	return inst.Link != nil && inst.Link.Level >= 2
}

// Context is what the handler does the call's I/O under (ADR-0396 §4): for
// a query or an external read the request's own context (app.Msg.Context),
// ending when the requester stops waiting; for a command an agent queued,
// the requester's values — its call identity — under the window's
// lifetime, since the request was answered before the command ran; for a
// gesture the window's lifetime (ContextOf). A call built without one, in a
// test, has a context that never ends.
func (inst OperationCall) Context() (ctx context.Context) {
	if inst.ctx == nil {
		return context.Background() //boxer:lint disable=CS020 reason="the root for a call built without a context, as Msg.Context"
	}
	return inst.ctx
}

// WithContext is call with Context ctx. A host uses it when it hands a
// call to a handler.
func (inst OperationCall) WithContext(ctx context.Context) (out OperationCall) {
	out = inst
	out.ctx = ctx
	return
}

// OnBehalfOf is the context the host's dispatcher stamps on an agent's
// call; the callee reads it and cannot change what the host checks it
// against.
type OnBehalfOf struct {
	Task  string
	Epoch uint64
	// Call is the dispatcher's id for the call, which the callee stamps on
	// what the call causes — a query run, a fetch, a completion — so that
	// work joins the action record on (Task, Call) (ADR-0277 §SD7).
	Call string
	// Principal is whose authority the task holds: the person.
	Principal string
	// Act is the chain the call came through: the person, the coordinator
	// window, the called window.
	Act []string
	// Destinations are what the grant lets agent-caused work reach.
	Destinations []string
	// Tier is the run's current TLP tier (agentrun.TierE) as the
	// coordinator declared it on the call (ADR-0312 §SD4). A callee carries
	// it on the onward requests the call causes — a fetch — so a wall that
	// reads a tier sees the run's; the host checks the dispatcher's
	// attested copy ([CallContext]), never the callee's word alone.
	Tier uint8
	// Run is the tagged id of the run journal the coordinator writes for
	// the conversation, as it declared it on the call; 0 where it declared
	// none. A callee that records what the call caused names it so the
	// record joins the run.
	Run uint64
}

// DelegationI is what a host service that reaches outside asks about an
// on-behalf-of context: is the call one the dispatcher sent to the sender's
// window ([CallContextI]), and does the task's grant list the destination?
// A service records the task on its row only once the dispatcher has
// attested it (ADR-0277 §SD1). The host's dispatcher implements it.
type DelegationI interface {
	CallContextI
	AllowDestination(task string, epoch uint64, destination string) (ok bool, reason string)
}

// CallContext is what the dispatcher recorded for an agent's call, as it
// answers a host service asking about it (ADR-0288 §SD5).
// Conversation and Turn are what the coordinator stated when it made the
// call (ADR-0277 §SD1); ModelCall, ToolCall and ToolIndex the model call
// whose reply asked for it. Empty where the coordinator stated none.
type CallContext struct {
	Task  string
	Epoch uint64
	Call  string

	Conversation string
	Turn         string
	ModelCall    string
	ToolCall     string
	ToolIndex    uint32

	App       AppIdT
	Instance  uint64
	Operation string
	// Tier is the run's TLP tier the coordinator declared on the call
	// (ADR-0312 §SD4), as the dispatcher recorded it; 0 where it declared
	// none. A wall takes the higher of this and what the sender declares.
	Tier uint8
	// Run is the run journal's tagged id the coordinator declared on the
	// call, as the dispatcher recorded it; 0 where it declared none.
	Run uint64
	// InFlight is true when the window had not answered the call yet:
	// work its handler did. False is work the window did later, under a
	// call it had answered while the task stayed live (ADR-0288
	// §SD5).
	InFlight bool
}

// CallContextI attests an on-behalf-of context for a host service that
// records agent-caused work: it answers only for a call routed to the
// sender's window (app and instance) in a task that is live at that epoch,
// and returns what the dispatcher recorded for it. A service that records
// the answer records the dispatcher's word for conversation and turn, not
// the sender's. The host's dispatcher implements it.
type CallContextI interface {
	CallContext(task string, epoch uint64, call string, sender AppIdT, senderInstance uint64) (cc CallContext, ok bool, reason string)
}

// OperationRefusal is an error a handler returns to decline a call without
// failing it. Conflict marks a refusal the caller can resolve by reading
// again: the person is editing, or the state moved. Destinations, when set,
// are what a grant would have to list for the call to go through.
type OperationRefusal struct {
	Conflict     bool
	Reason       string
	Destinations []string
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

// RefuseForDestinations declines a call whose task's grant does not list
// the destinations it would reach. A destination that names nothing after
// its class — "clickhouse:", an endpoint without a host — is left out: no
// grant entry of that shape covers a call, so it is never asked for.
func RefuseForDestinations(reason string, destinations ...string) (err error) {
	named := make([]string, 0, len(destinations))
	for _, d := range destinations {
		if class, rest, ok := strings.Cut(d, ":"); d == "" || (ok && class != "" && strings.TrimSpace(rest) == "") {
			continue
		}
		named = append(named, d)
	}
	if len(named) == 0 {
		named = nil
	}
	return &OperationRefusal{Reason: reason, Destinations: named}
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
