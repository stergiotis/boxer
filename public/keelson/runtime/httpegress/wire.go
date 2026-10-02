package httpegress

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// The wire forms, plain CBOR structs through buscodec with a version byte —
// the llm and chlocal shape. The destination is the subject's last token,
// not a field: the bus's publish check is what enforces it.

const wireVersion uint8 = 1

// wireRequest is the envelope on net.http.fetch.<destination>.
type wireRequest struct {
	V           uint8  `json:"v"`
	Method      string `json:"method,omitempty"`
	URL         string `json:"url"`
	Purpose     string `json:"purpose,omitempty"`
	Sensitivity uint8  `json:"sensitivity,omitempty"`
	// DeadlineUnixNanos carries the caller's ctx deadline, since the
	// handler has no ctx of its own. 0 means none.
	DeadlineUnixNanos int64 `json:"deadline_ns,omitempty"`
	// OnBehalfTask and OnBehalfEpoch name the agent task whose work this
	// fetch is (ADR-0269 §SD6); empty for the app's own.
	OnBehalfTask  string `json:"obo_task,omitempty"`
	OnBehalfEpoch uint64 `json:"obo_epoch,omitempty"`
}

// wireReply is the reply. Ok says the exchange completed, whatever its
// status: HTTP semantics are the caller's. Ok false carries the reason and
// the kind of failure.
type wireReply struct {
	V           uint8  `json:"v"`
	Ok          bool   `json:"ok"`
	Reason      string `json:"reason,omitempty"`
	ErrorKind   string `json:"error_kind,omitempty"`
	Status      int32  `json:"status,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Body        []byte `json:"body,omitempty"`
	ElapsedNs   int64  `json:"elapsed_ns,omitempty"`
}

// The failure kinds a reply can name.
const (
	errKindRefused   = "refused"
	errKindTooLarge  = "too_large"
	errKindTimeout   = "timeout"
	errKindTransport = "transport"
)

func encode[T any](v T) (b []byte, err error) {
	b, err = buscodec.Encode(v)
	if err != nil {
		err = eh.Errorf("httpegress: encode: %w", err)
	}
	return
}

func decode[T any](b []byte) (v T, err error) {
	v, err = buscodec.Decode[T](b)
	if err != nil {
		err = eh.Errorf("httpegress: decode: %w", err)
	}
	return
}
