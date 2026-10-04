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
	// Task and Handle answer an approved request's key.
	Task   string `json:"task,omitempty"`
	Handle string `json:"handle,omitempty"`
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
}

type wireHandle struct {
	V        uint8  `json:"v"`
	Handle   string `json:"handle"`
	Instance uint64 `json:"instance,omitempty"`
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
