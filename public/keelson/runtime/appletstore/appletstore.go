// Package appletstore is the wire contract of the runtime applet store
// (ADR-0132 Update "O4"): the bus subject and request/reply shapes through
// which an authoring app (apps/sqlappletcreator) submits a SQL-applet
// document for validation, persistence, and minting, plus [ComposeAppletDoc]
// — the canonical document shape both the authoring side produces and the
// store's gate parses back. Only the contract lives here — the service
// implementation is the sqlapplet host's (apps/sqlapplet), which this
// package must not import (authoring apps import this).
package appletstore

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// SubjectSave is the request subject: publish a [SaveRequest], receive a
// [SaveReply]. The store service validates the document exactly as the
// committed-book corpus gate does — an invalid document is refused, never
// half-saved.
const SubjectSave = "applet.store.save"

// wireVersion versions the CBOR envelopes below.
const wireVersion uint8 = 1

// SaveRequest submits one applet document for storage under Slug.
type SaveRequest struct {
	V uint8 `json:"v"`
	// Slug is the applet's durable name (lowercase alphanumerics and
	// dashes, as for committed docs). Saving an existing stored slug
	// overwrites its document; colliding with a committed applet is an
	// error.
	Slug string `json:"slug"`
	// Doc is the full markdown document (frontmatter, prose, sql fence) —
	// the same shape a committed applet book carries.
	Doc []byte `json:"doc"`
}

// SaveReply reports the outcome. On success Class carries the ADR-0132
// §SD5 security class the stored buffer was assigned.
type SaveReply struct {
	V     uint8  `json:"v"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Class string `json:"class,omitempty"`
}

// EncodeSaveRequest wire-encodes req (stamping the version).
func EncodeSaveRequest(req SaveRequest) (b []byte, err error) {
	req.V = wireVersion
	b, err = buscodec.Encode(req)
	if err != nil {
		err = eh.Errorf("appletstore: encode save request: %w", err)
	}
	return
}

// DecodeSaveRequest decodes and version-checks a save request.
func DecodeSaveRequest(b []byte) (req SaveRequest, err error) {
	req, err = buscodec.Decode[SaveRequest](b)
	if err != nil {
		err = eh.Errorf("appletstore: decode save request: %w", err)
		return
	}
	if req.V == 0 || req.V > wireVersion {
		err = eh.Errorf("appletstore: save request version %d unsupported (max %d)", req.V, wireVersion)
	}
	return
}

// EncodeSaveReply wire-encodes rep (stamping the version).
func EncodeSaveReply(rep SaveReply) (b []byte, err error) {
	rep.V = wireVersion
	b, err = buscodec.Encode(rep)
	if err != nil {
		err = eh.Errorf("appletstore: encode save reply: %w", err)
	}
	return
}

// DecodeSaveReply decodes and version-checks a save reply.
func DecodeSaveReply(b []byte) (rep SaveReply, err error) {
	rep, err = buscodec.Decode[SaveReply](b)
	if err != nil {
		err = eh.Errorf("appletstore: decode save reply: %w", err)
		return
	}
	if rep.V == 0 || rep.V > wireVersion {
		err = eh.Errorf("appletstore: save reply version %d unsupported (max %d)", rep.V, wireVersion)
	}
	return
}
