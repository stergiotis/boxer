package clipboardbroker

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// The labelled payload (ADR-0317 §SD1): a copy of labelled text names its
// label and the widget it came from, so the broker's record and the
// journal's `copy` entry say what left for the clipboard and under which
// label. A raw UTF-8 payload is the legacy form every existing publisher
// sends and stays valid: it is text with no label, from no named source.
//
// deferred: the host plugin on egui's output_hook that drops a CopyText
// the broker did not make, and the clipboard policy opcode in the IDL, are
// the one Rust change ADR-0317 §SD2 names, with the browser host digest
// refreshed as ADR-0278 §SD5 requires; until it lands, a copy made outside
// the broker is kept out by the lint alone (codelint CS014).

// Payload is one copy of labelled text.
type Payload struct {
	Text string
	// Label is the text's label; the zero label is plain text.
	Label agentrun.Label
	// Source names the widget or app surface the text was copied from.
	Source string
}

// payloadMagic opens a labelled payload on the wire: a byte sequence no
// UTF-8 text starts with, so the broker tells it from raw text without a
// guess.
var payloadMagic = []byte("\x00boxer-clip\x01")

// wirePayload is the payload's body after the magic, canonical CBOR.
type wirePayload struct {
	Text     string   `json:"text"`
	Level    uint8    `json:"level"`
	Tier     uint8    `json:"tier"`
	Exposure []string `json:"exposure,omitempty"`
	Origin   []string `json:"origin,omitempty"`
	Source   string   `json:"source,omitempty"`
}

// Encode is the payload as the request's bytes.
func (inst Payload) Encode() (b []byte, err error) {
	body, err := buscodec.Encode(wirePayload{Text: inst.Text, Level: uint8(inst.Label.Level), Tier: uint8(inst.Label.Tier),
		Exposure: inst.Label.Exposure, Origin: inst.Label.Origin, Source: inst.Source})
	if err != nil {
		err = eh.Errorf("clipboardbroker: encode payload: %w", err)
		return
	}
	return append(append(make([]byte, 0, len(payloadMagic)+len(body)), payloadMagic...), body...), nil
}

// DecodePayload reads a request's bytes: a labelled payload after the
// magic, or raw text as a payload with the zero label. labelled says which.
func DecodePayload(b []byte) (p Payload, labelled bool, err error) {
	if !bytes.HasPrefix(b, payloadMagic) {
		p.Text = string(b)
		return
	}
	w, err := buscodec.Decode[wirePayload](b[len(payloadMagic):])
	if err != nil {
		err = eh.Errorf("clipboardbroker: decode payload: %w", err)
		return
	}
	p = Payload{Text: w.Text, Label: agentrun.Label{Level: agentrun.LevelE(w.Level), Tier: agentrun.TierE(w.Tier), Exposure: w.Exposure, Origin: w.Origin}, Source: w.Source}
	return p, true, nil
}

// HandlePrefix opens a copy by reference (ADR-0317 §SD3): the string an
// outside program sees, inert there, resolving inside boxer to the payload
// with its labels.
const HandlePrefix = "boxer:clip/"

// IsHandle says the text is a copy handle, and gives its id.
func IsHandle(text string) (id string, ok bool) {
	id, ok = strings.CutPrefix(text, HandlePrefix)
	return id, ok && id != ""
}

// CopyRecord is what a copy of labelled text leaves for the journal's
// `copy` entry (ADR-0308 §SD2, ADR-0317 §SD1): the digest of the text, its
// label and source, and how it left — as a handle or as plaintext.
type CopyRecord struct {
	Digest string         `json:"digest"`
	Label  agentrun.Label `json:"-"`
	Level  uint8          `json:"level"`
	Tier   uint8          `json:"tier"`
	Source string         `json:"source,omitempty"`
	// Handle is the copy handle when the copy went by reference; empty for
	// plaintext.
	Handle string `json:"handle,omitempty"`
	// Plaintext says the text went to the clipboard as it is.
	Plaintext bool `json:"plaintext"`
	// Recorded says the policy only recorded the exit (a host the person
	// administers, ADR-0317 §SD4); false where it enforced.
	Recorded bool `json:"recorded,omitempty"`
	// Approval is the approval path under which plaintext above the free
	// tier left, where an approval was given; empty otherwise. A handle is
	// no approval, and no path gives one today (ADR-0317 §SD2's dialog is
	// deferred), so it is empty on every record the broker writes.
	Approval string `json:"approval,omitempty"`
	// Raw says the copy was unlabelled text, the legacy form.
	Raw bool `json:"raw,omitempty"`
	// Refused says nothing reached the clipboard, and Reason why.
	Refused bool   `json:"refused,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Bytes   int    `json:"bytes"`
}

// RefusedError is a copy the broker refused (ADR-0317 §SD4).
type RefusedError struct {
	Record CopyRecord
}

func (inst *RefusedError) Error() string { return "clipboardbroker: refused: " + inst.Record.Reason }

// Write hands a labelled payload to the broker and returns what it did
// with it: the record for the journal, with the handle when the copy went
// by reference. The caller's bus client holds a Pub cap on SubjectWrite.
// It blocks until the broker acks, so call it off the frame goroutine.
func Write(bus app.BusI, p Payload) (rec CopyRecord, err error) {
	b, err := p.Encode()
	if err != nil {
		return
	}
	reply, err := bus.Request(SubjectWrite, b)
	if err != nil {
		err = eh.Errorf("clipboardbroker: write: %w", err)
		return
	}
	if len(reply) == 0 {
		// A broker that acks a labelled payload with nothing did not read
		// the label; the copy is not confirmed.
		err = eh.Errorf("clipboardbroker: write: the broker returned no record for a labelled copy")
		return
	}
	if rec, err = buscodec.Decode[CopyRecord](reply); err != nil {
		err = eh.Errorf("clipboardbroker: write ack: %w", err)
		return
	}
	rec.Label = agentrun.Label{Level: agentrun.LevelE(rec.Level), Tier: agentrun.TierE(rec.Tier), Exposure: p.Label.Exposure, Origin: p.Label.Origin}
	if rec.Refused {
		err = &RefusedError{Record: rec}
	}
	return
}

// WriteText hands raw text to the broker, the legacy form: plain text with
// no label.
func WriteText(bus app.BusI, text string) (err error) {
	if _, err = bus.Request(SubjectWrite, []byte(text)); err != nil {
		err = eh.Errorf("clipboardbroker: write: %w", err)
	}
	return
}

// Resolve asks the broker for the payload behind a handle, so a paste
// inside boxer restores the labels; ok is false for a handle it does not
// hold.
func Resolve(bus app.BusI, handle string) (p Payload, ok bool, err error) {
	id, isHandle := IsHandle(handle)
	if !isHandle {
		return
	}
	reply, err := bus.Request(SubjectResolve, []byte(id))
	if err != nil {
		err = eh.Errorf("clipboardbroker: resolve: %w", err)
		return
	}
	if len(reply) == 0 {
		return
	}
	p, _, err = DecodePayload(reply)
	return p, err == nil, err
}

// ForwardedCopy is one CopyText the host forwarded to the clipboard: what
// the plugin of ADR-0317 §SD2 would see, and what DrainPending hands the
// host today.
type ForwardedCopy struct {
	Text string
}

// CheckL20 is law L20 (ADR-0317 §SD4) as a check over a run of copies:
// every record of labelled text above the free tier went as a handle, was
// refused, or is recorded as a recorded exit; integrity-only labels went
// plaintext with a record; and every forwarded copy has a broker record
// behind it, a raw copy's included. It returns the records that break it.
func CheckL20(free agentrun.TierE, records []CopyRecord, forwarded []ForwardedCopy) (broken []string) {
	seen := map[string]bool{}
	for i, r := range records {
		if r.Refused {
			continue
		}
		seen[r.Digest] = true
		if r.Handle != "" {
			seen[agentrun.ContentDigest(r.Handle)] = true
		}
		if r.Tier > uint8(free) && r.Plaintext && !r.Recorded {
			broken = append(broken, "record "+itoa(i)+": labelled text above the free tier left as plaintext without a recorded exit")
		}
	}
	for i, f := range forwarded {
		if !seen[agentrun.ContentDigest(f.Text)] {
			broken = append(broken, "forwarded copy "+itoa(i)+" has no broker record")
		}
	}
	return
}

func itoa(i int) string { return strconv.Itoa(i) }

// deferred: L20 has no SQL form yet. The journal's copy entry is written
// before the act (ADR-0308 §SD2), so it carries the label and the digest
// and not how the copy left; how it left is the broker's own record, in
// memory, with no served relation. The SQL twin needs the broker record
// on the trail; until then CheckL20 over Records is the check.
