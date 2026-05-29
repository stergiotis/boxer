//go:build llm_generated_opus47

// Package capbroker is the ADR-0026 §SD7 capability broker — a runtime-
// internal subject handler bound to runtime.cap.request that arbitrates
// dynamic permission grants. For M2.3 the broker decides via a pluggable
// GrantPolicyI, mutates the target Client's caps in memory, and replies
// with a GrantReply. Grant persistence to runtime.facts arrives alongside
// audit recording in M2.5; the egui dialog policy lands when the M3 dock
// host provides an overlay layer.
package capbroker

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/grantreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/grantrequest"
)

// RequestSubject is the well-known subject apps publish cap requests to.
// Per the ADR §SD3 taxonomy.
const RequestSubject = "runtime.cap.request"

// GrantRequest is the broker's request payload. The AppId is the app the
// grant targets — for M2.3 the broker logs (but does not enforce) a
// mismatch between AppId and the Msg.Sender; M4 NKey-based identity will
// upgrade this to a real enforcement boundary.
type GrantRequest struct {
	AppId         app.AppIdT        `json:"appId"`
	SubjectFilter app.SubjectFilter `json:"subjectFilter"`
}

// GrantReply is the broker's response. On approval, GrantId is the broker-
// local identifier for the grant record; on denial, GrantId is empty and
// Reason carries the policy's explanation.
type GrantReply struct {
	Granted bool   `json:"granted"`
	GrantId string `json:"grantId,omitempty"`
	Reason  string `json:"reason"`
}

// MarshalRequest serialises a GrantRequest for transmission as a
// Msg.Payload. The codec wire form (grantrequest.GrantRequest) flattens
// the nested SubjectFilter into peer columns; this helper handles the
// conversion so callers can keep using the broker's native shape.
func MarshalRequest(r GrantRequest) (b []byte, err error) {
	wire := grantrequest.GrantRequest{
		AppId:           string(r.AppId),
		FilterPattern:   r.SubjectFilter.Pattern,
		FilterReason:    r.SubjectFilter.Reason,
		FilterDirection: r.SubjectFilter.Direction.String(),
		FilterSticky:    r.SubjectFilter.Sticky,
	}
	b, err = buscodec.Encode(wire)
	if err != nil {
		err = eh.Errorf("marshal grant request: %w", err)
	}
	return
}

// UnmarshalRequest is the inverse of MarshalRequest. Reconstructs the
// nested SubjectFilter from its flattened columns.
func UnmarshalRequest(b []byte) (r GrantRequest, err error) {
	var wire grantrequest.GrantRequest
	wire, err = buscodec.Decode[grantrequest.GrantRequest](b)
	if err != nil {
		err = eh.Errorf("unmarshal grant request: %w", err)
		return
	}
	r = GrantRequest{
		AppId: app.AppIdT(wire.AppId),
		SubjectFilter: app.SubjectFilter{
			Pattern:   wire.FilterPattern,
			Reason:    wire.FilterReason,
			Direction: app.ParseCapDirection(wire.FilterDirection),
			Sticky:    wire.FilterSticky,
		},
	}
	return
}

// MarshalReply serialises a GrantReply for transmission as a Msg.Payload.
func MarshalReply(r GrantReply) (b []byte, err error) {
	wire := grantreply.GrantReply{
		Approved: r.Granted,
		GrantId:  r.GrantId,
		Reason:   r.Reason,
	}
	b, err = buscodec.Encode(wire)
	if err != nil {
		err = eh.Errorf("marshal grant reply: %w", err)
	}
	return
}

// UnmarshalReply is the inverse of MarshalReply.
func UnmarshalReply(b []byte) (r GrantReply, err error) {
	var wire grantreply.GrantReply
	wire, err = buscodec.Decode[grantreply.GrantReply](b)
	if err != nil {
		err = eh.Errorf("unmarshal grant reply: %w", err)
		return
	}
	r = GrantReply{
		Granted: wire.Approved,
		GrantId: wire.GrantId,
		Reason:  wire.Reason,
	}
	return
}
