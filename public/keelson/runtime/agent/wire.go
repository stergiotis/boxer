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
	App        string          `json:"app"`
	Display    string          `json:"display"`
	Summary    string          `json:"summary,omitempty"`
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
