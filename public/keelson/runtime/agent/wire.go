package agent

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// The wire forms: plain CBOR structs through buscodec with a version byte,
// the llm and httpegress shape.

const wireVersion uint8 = 1

// wireDescribeRequest is the envelope on runtime.agent.describe. App names
// an app by id or by its subject alias; Search matches app and operation
// names and summaries; Operation asks for one operation with its schemas.
type wireDescribeRequest struct {
	V         uint8  `json:"v"`
	App       string `json:"app,omitempty"`
	Search    string `json:"search,omitempty"`
	Operation string `json:"operation,omitempty"`
	// Key, Conversation and the cause name the model's tool call, when one
	// asked: the describe then leaves an action row. The coordinator's own
	// reads of the catalog name none and leave none.
	Key          string `json:"key,omitempty"`
	Conversation string `json:"conversation,omitempty"`
	wireCause
}

// wireCause is the turn a request belongs to and the model call whose
// reply asked for it, the provider's id for the tool call and its index in
// that reply (ADR-0277 §SD1), as the coordinator states them. Embedded, its
// fields travel as the request's own.
type wireCause struct {
	Turn      string `json:"turn,omitempty"`
	ModelCall string `json:"model_call,omitempty"`
	ToolCall  string `json:"tool_call,omitempty"`
	ToolIndex uint32 `json:"tool_index,omitempty"`
}

type wireResource struct {
	Name    string `json:"name"`
	Summary string `json:"summary,omitempty"`
}

type wireOperation struct {
	Name         string   `json:"name"`
	Version      uint16   `json:"version"`
	Summary      string   `json:"summary"`
	Class        string   `json:"class"`
	Effect       string   `json:"effect"`
	Reads        []string `json:"reads,omitempty"`
	Writes       []string `json:"writes,omitempty"`
	Refs         []string `json:"refs,omitempty"`
	Follows      []string `json:"follows,omitempty"`
	Untrusted    bool     `json:"untrusted,omitempty"`
	Gesture      string   `json:"gesture,omitempty"`
	Consent      string   `json:"consent,omitempty"`
	ArgsSchema   string   `json:"args_schema,omitempty"`
	ResultSchema string   `json:"result_schema,omitempty"`
}

type wireApp struct {
	App     string `json:"app"`
	Display string `json:"display"`
	Summary string `json:"summary,omitempty"`
	// Help says the app ships inline help, read with SubjectHelp.
	Help       bool            `json:"help,omitempty"`
	Resources  []wireResource  `json:"resources,omitempty"`
	Operations []wireOperation `json:"operations"`
}

type wireDescribeReply struct {
	V      uint8     `json:"v"`
	Ok     bool      `json:"ok"`
	Reason string    `json:"reason,omitempty"`
	Apps   []wireApp `json:"apps,omitempty"`
}

func encode[T any](v T) (b []byte, err error) {
	b, err = buscodec.Encode(v)
	if err != nil {
		err = eh.Errorf("agent: encode: %w", err)
	}
	return
}

func decode[T any](b []byte) (v T, err error) {
	v, err = buscodec.Decode[T](b)
	if err != nil {
		err = eh.Errorf("agent: decode: %w", err)
	}
	return
}

// wireGrantEntry is one instance a task may work in.
type wireGrantEntry struct {
	Instance   uint64   `json:"instance"`
	Mode       string   `json:"mode"`
	Operations []string `json:"operations,omitempty"`
}

// wireGrantRequest asks for a grant, or with Handle for the widening of
// one.
type wireGrantRequest struct {
	V      uint8  `json:"v"`
	Handle string `json:"handle,omitempty"`
	// Conversation names the coordinator's conversation; taint belongs to
	// it (ADR-0269 §SD7).
	Conversation string            `json:"conversation,omitempty"`
	Plan         string            `json:"plan"`
	Entries      []wireGrantEntry  `json:"entries"`
	Destinations []string          `json:"destinations,omitempty"`
	Calls        uint32            `json:"calls,omitempty"`
	DeadlineSecs uint32            `json:"deadline_secs,omitempty"`
	Launches     []wireLaunchEntry `json:"launches,omitempty"`
	// Desktop asks for a mode over the desktop as a whole: "act" lets the
	// task arrange every window (ADR-0276 §SD4).
	Desktop string `json:"desktop,omitempty"`
	// Ceiling is the most the coordinator's settings let the model ask for
	// (ADR-0280); absent, the task has none. A request above it is refused
	// before the person is asked.
	Ceiling *wireCeiling `json:"ceiling,omitempty"`
	// The model call that asked for the grant; its rows carry it.
	wireCause
}

// wireLaunchEntry names an app the task may open windows of.
type wireLaunchEntry struct {
	App   string `json:"app"`
	Mode  string `json:"mode"`
	Count uint32 `json:"count"`
}

// wireLaunch opens a window for the task.
type wireLaunch struct {
	V      uint8  `json:"v"`
	Handle string `json:"handle"`
	App    string `json:"app"`
	Kind   string `json:"kind,omitempty"`
	Config []byte `json:"config,omitempty"`
	// Key, Turn, ModelCall, ToolCall and ToolIndex are a call's (wireCall):
	// the launch leaves an action row under them (ADR-0277 §SD7).
	Key       string `json:"key,omitempty"`
	Turn      string `json:"turn,omitempty"`
	ModelCall string `json:"model_call,omitempty"`
	ToolCall  string `json:"tool_call,omitempty"`
	ToolIndex uint32 `json:"tool_index,omitempty"`
}

type wireLaunchReply struct {
	V        uint8  `json:"v"`
	Ok       bool   `json:"ok"`
	Reason   string `json:"reason,omitempty"`
	Instance uint64 `json:"instance,omitempty"`
	// Load is how far the opened window had come when the reply left:
	// opening, ready or failed; LoadReason says why it failed.
	Load       string `json:"load,omitempty"`
	LoadReason string `json:"load_reason,omitempty"`
}

type wireGrantReply struct {
	V      uint8  `json:"v"`
	Ok     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
	// Key names the request for status; Phase is pending, approved,
	// rejected or expired.
	Key    string `json:"key,omitempty"`
	Phase  string `json:"phase,omitempty"`
	Task   string `json:"task,omitempty"`
	Handle string `json:"handle,omitempty"`
	// Terms are the approved task's bounds.
	Terms *wireGrantTerms `json:"terms,omitempty"`
}

// wireGrantTerms are an approved task's bounds as the person left them:
// its call budget and what is spent, its deadline, and the destinations it
// may reach. The person may approve less than was asked, so the caller
// reads them here rather than from its request.
type wireGrantTerms struct {
	Calls        int32    `json:"calls"`
	CallsUsed    int32    `json:"calls_used,omitempty"`
	DeadlineMs   int64    `json:"deadline_ms,omitempty"`
	Destinations []string `json:"destinations,omitempty"`
}

// wireCall is the envelope on runtime.agent.call. Args is the model's JSON.
type wireCall struct {
	V         uint8             `json:"v"`
	Handle    string            `json:"handle"`
	Instance  uint64            `json:"instance"`
	Operation string            `json:"operation"`
	Args      string            `json:"args,omitempty"`
	Expects   map[string]uint64 `json:"expects,omitempty"`
	Key       string            `json:"key"`
	Reason    string            `json:"reason,omitempty"`
	// Title is the model's one-line title for the call, as the person is
	// shown it: recorded, never routed to the app.
	Title string `json:"title,omitempty"`
	// Turn is the conversation's turn the call belongs to; ModelCall,
	// ToolCall and ToolIndex name the model call whose reply asked for it,
	// the provider's id for the tool call and its index in that reply
	// (ADR-0277 §SD1). Recorded as the coordinator states them.
	Turn      string `json:"turn,omitempty"`
	ModelCall string `json:"model_call,omitempty"`
	ToolCall  string `json:"tool_call,omitempty"`
	ToolIndex uint32 `json:"tool_index,omitempty"`
}

type wireOutcome struct {
	Phase     string            `json:"phase"`
	Reason    string            `json:"reason,omitempty"`
	AsOf      uint64            `json:"as_of,omitempty"`
	Revisions map[string]uint64 `json:"revisions,omitempty"`
	ResultRef string            `json:"result_ref,omitempty"`
	Job       string            `json:"job,omitempty"`
	Confined  bool              `json:"confined,omitempty"`
	// Held marks an input_required call that waits on the person.
	Held bool `json:"held,omitempty"`
	// Task and Handle answer an approved request's key, and Terms are the
	// task's bounds.
	Task   string          `json:"task,omitempty"`
	Handle string          `json:"handle,omitempty"`
	Terms  *wireGrantTerms `json:"terms,omitempty"`
	// Remedy, on a refusal, is what would let the call through.
	Remedy *wireRemedy `json:"remedy,omitempty"`
}

type wireRemedy struct {
	Destinations []string `json:"destinations,omitempty"`
	ArgsSchema   string   `json:"args_schema,omitempty"`
}

type wireCallReply struct {
	V       uint8       `json:"v"`
	Ok      bool        `json:"ok"`
	Reason  string      `json:"reason,omitempty"`
	Outcome wireOutcome `json:"outcome"`
}

// wireStatus asks for a call's or a job's phase by key, waiting up to
// WaitMs for a final one.
type wireStatus struct {
	V      uint8  `json:"v"`
	Handle string `json:"handle"`
	Key    string `json:"key"`
	WaitMs uint32 `json:"wait_ms,omitempty"`
}

type wireCancel struct {
	V      uint8  `json:"v"`
	Handle string `json:"handle"`
	Key    string `json:"key"`
}

type wireRead struct {
	V      uint8  `json:"v"`
	Handle string `json:"handle"`
	Ref    string `json:"ref"`
}

type wireReadReply struct {
	V         uint8  `json:"v"`
	Ok        bool   `json:"ok"`
	Reason    string `json:"reason,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Text      string `json:"text,omitempty"`
	Path      string `json:"path,omitempty"`
	// Data is a capture's bytes: a capture is kept sealed and has no path
	// (ADR-0281 §SD6).
	Data []byte `json:"data,omitempty"`
	// Untrusted marks content an attacker could influence; Source
	// attributes it to its window and operation (ADR-0269 §SD7).
	Untrusted bool   `json:"untrusted,omitempty"`
	Source    string `json:"source,omitempty"`
	// DataHandle stands in for confined content the coordinator's model
	// may not see: a reference it may pass, never text.
	DataHandle string `json:"data_handle,omitempty"`
	Confined   bool   `json:"confined,omitempty"`
}

type wireCapture struct {
	V        uint8  `json:"v"`
	Handle   string `json:"handle"`
	Instance uint64 `json:"instance"`
	Key      string `json:"key"`
	// Format is "svg" or "png"; empty is "svg" (ADR-0281).
	Format string `json:"format,omitempty"`
	// Instances, when set, are the windows captured together, and Instance
	// is ignored; Crop keeps a part of the frame, in logical points.
	Instances []uint64  `json:"instances,omitempty"`
	Crop      *wireRect `json:"crop,omitempty"`
	wireCause
}

// wireRect is a rectangle in logical points.
type wireRect struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
	W float32 `json:"w"`
	H float32 `json:"h"`
}

// wireWindowAct is arrange, raise or place (ADR-0276 §SD3). Command and
// Instances are arrange's; Instance is raise's and place's; X, Y, W, H is
// place's outer rect in logical points.
type wireWindowAct struct {
	V         uint8    `json:"v"`
	Handle    string   `json:"handle"`
	Key       string   `json:"key"`
	Command   string   `json:"command,omitempty"`
	Instances []uint64 `json:"instances,omitempty"`
	Instance  uint64   `json:"instance,omitempty"`
	X         float32  `json:"x,omitempty"`
	Y         float32  `json:"y,omitempty"`
	W         float32  `json:"w,omitempty"`
	H         float32  `json:"h,omitempty"`
	wireCause
}

type wireHandle struct {
	V        uint8  `json:"v"`
	Handle   string `json:"handle"`
	Instance uint64 `json:"instance,omitempty"`
	// Key and the cause name the model's tool call behind a list, a stop or
	// a turn, when one asked; a list then leaves an action row.
	Key string `json:"key,omitempty"`
	wireCause
	// By is who stops a task, "person" or "coordinator" (the default), and
	// Reason why; stop's only. "person" is the coordinator's claim.
	By     string `json:"by,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type wireInstance struct {
	Instance uint64 `json:"instance"`
	App      string `json:"app"`
	// Title is untrusted text, and withheld for a confined window.
	Title    string `json:"title"`
	Mode     string `json:"mode"`
	Ops      bool   `json:"ops"`
	Confined bool   `json:"confined,omitempty"`
	// Load is opening, ready or failed; LoadReason says why it failed.
	Load       string `json:"load,omitempty"`
	LoadReason string `json:"load_reason,omitempty"`
}

type wireListReply struct {
	V         uint8          `json:"v"`
	Ok        bool           `json:"ok"`
	Reason    string         `json:"reason,omitempty"`
	Instances []wireInstance `json:"instances,omitempty"`
}

type wireAck struct {
	V      uint8  `json:"v"`
	Ok     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}
