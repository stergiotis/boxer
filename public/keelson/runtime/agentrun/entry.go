package agentrun

import (
	"encoding/hex"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonform"
)

// Entry is one journal entry (ADR-0308 §SD1, §SD2): its place in the run,
// its kind and what the kind carries, its label, and the chain digest that
// commits it to everything before it (ADR-0314 §SD1).
type Entry struct {
	// Run is the cell-qualified run id and Ordinal the entry's place in
	// it, assigned by the run's single writer, contiguous from 1.
	Run     string
	Ordinal uint32
	// At is when the entry was written, for the person; the ordinal is the
	// order.
	At   time.Time
	Kind KindE
	// Key correlates acts: a request and its reply share the model call's
	// key, an intent and its outcome the tool call's (ADR-0277 §SD6), a
	// question and its answer theirs.
	Key string
	// Role is the message role an entry projects as, where the kind has
	// one: user, assistant, tool, system.
	Role string
	// Body is what the kind carries: the message text, the note, the tool
	// result, the summary, the section text; for the kinds that are not
	// messages, their record as JSON.
	Body string
	// Calls are an assistant reply's tool calls, as JSON, so the
	// projection pairs results with calls (law L2); empty otherwise.
	Calls string
	// BodyDigest and CallsDigest are the content digests of Body and
	// Calls as written, set by the writer. The chain commits to the
	// digests, not the bodies, so a tombstone that empties the bodies
	// leaves the chain verifying (law L31) and the anchored heads with it
	// (law L32): ADR-0308 §SD8, ADR-0314 §SD5.
	BodyDigest  string
	CallsDigest string
	// Name is what a section, tools or note entry is named by: the
	// section slug, the tool set's digest, the note's source; a deleted
	// entry names who deleted, outside the body a tombstone empties.
	Name string
	// Book is a section's prompt book.
	Book string
	// Form is a note's form, snapshot or notice (ADR-0308 §SD2).
	Form string
	// Target is the ordinal an edit replaces, the cut a compaction keeps
	// from, or the ordinal a verdict judges; From is the previous cut of a
	// compaction (law L22).
	Target uint32
	From   uint32
	// Effect is an intent's declared effect class (ADR-0269 §SD5).
	Effect string
	// Turn is the turn the entry belongs to.
	Turn string
	// Label is the entry's label (ADR-0311).
	Label Label
	// Writer is the run, turn and ordinal that wrote a body a model
	// produced, kept as plaintext metadata beside it so re-entry is
	// detected (ADR-0311 §SD4); empty for the person's and the host's.
	WriterRun     string
	WriterTurn    string
	WriterOrdinal uint32
	// Prev is the predecessor's ordinal and Digest the chain digest over
	// the predecessor's digest and this entry's canonical form; the first
	// entry chains to the run row's digest.
	Prev   uint32
	Digest string
	// KeyId is reserved for per-run encryption, empty while it is
	// deferred.
	KeyId string
	// Parent is reserved for branching, zero until a consumer asks.
	Parent uint32
	// Attachments are the images a message carries, as JSON: each a media
	// type and the digest the artefact's store keeps the bytes under, so
	// the journal holds no pixels and the encoder resolves them at the
	// wire.
	Attachments string
}

// canonical is the entry's canonical form: the fields that are content,
// in a fixed order, as canonical CBOR (buscodec's deterministic encoding,
// the record form of ADR-0201 being deferred), without the chain fields,
// so a digest is a function of content alone. Bodies enter as their
// content digests, marks as arrays, and the write instant as text.
type canonical struct {
	Run           string   `json:"run"`
	Ordinal       uint32   `json:"ordinal"`
	At            string   `json:"at"`
	Kind          uint8    `json:"kind"`
	Key           string   `json:"key"`
	Role          string   `json:"role"`
	BodyDigest    string   `json:"body_digest"`
	CallsDigest   string   `json:"calls_digest"`
	Name          string   `json:"name"`
	Book          string   `json:"book"`
	Form          string   `json:"form"`
	Target        uint32   `json:"target"`
	From          uint32   `json:"from"`
	Effect        string   `json:"effect"`
	Turn          string   `json:"turn"`
	Level         uint8    `json:"level"`
	Tier          uint8    `json:"tier"`
	Exposure      []string `json:"exposure"`
	Origin        []string `json:"origin"`
	WriterRun     string   `json:"writer_run"`
	WriterTurn    string   `json:"writer_turn"`
	WriterOrdinal uint32   `json:"writer_ordinal"`
	KeyId         string   `json:"key_id"`
	Parent        uint32   `json:"parent"`
	Attachments   string   `json:"attachments"`
}

// CanonicalForm is the entry's canonical form as bytes. It commits to the
// entry's BodyDigest and CallsDigest, which Digested sets from the bodies.
func (inst Entry) CanonicalForm() (b []byte, err error) {
	b, err = buscodec.NewCBOR().Encode(canonical{
		Run: inst.Run, Ordinal: inst.Ordinal, At: timeText(inst.At), Kind: uint8(inst.Kind), Key: inst.Key, Role: inst.Role,
		BodyDigest: inst.BodyDigest, CallsDigest: inst.CallsDigest,
		Name: inst.Name, Book: inst.Book, Form: inst.Form, Target: inst.Target, From: inst.From,
		Effect: inst.Effect, Turn: inst.Turn, Level: uint8(inst.Label.Level), Tier: uint8(inst.Label.Tier),
		Exposure: unionMarks(inst.Label.Exposure, nil), Origin: unionMarks(inst.Label.Origin, nil),
		WriterRun: inst.WriterRun, WriterTurn: inst.WriterTurn, WriterOrdinal: inst.WriterOrdinal,
		KeyId: inst.KeyId, Parent: inst.Parent, Attachments: inst.Attachments})
	if err != nil {
		err = eh.Errorf("canonical form: %w", err)
	}
	return
}

// Digested is the entry with BodyDigest and CallsDigest set from its
// bodies: what the writer does before chaining.
func (inst Entry) Digested() (e Entry) {
	e = inst
	e.BodyDigest, e.CallsDigest = ContentDigest(inst.Body), ContentDigest(inst.Calls)
	return
}

// Tombstoned says the entry's bodies were emptied after it was written:
// what a deleted run's entries are (ADR-0308 §SD8).
func (inst Entry) Tombstoned() bool {
	return (inst.Body == "" && inst.BodyDigest != emptyDigest) || (inst.Calls == "" && inst.CallsDigest != emptyDigest)
}

// BodiesMatch says the bodies the entry carries are the ones its digests
// commit to, or were emptied by a tombstone.
func (inst Entry) BodiesMatch() bool {
	return (inst.Body == "" || ContentDigest(inst.Body) == inst.BodyDigest) &&
		(inst.Calls == "" || ContentDigest(inst.Calls) == inst.CallsDigest)
}

// digester keys the chain digests the way the canonical record form keys
// its record digests, so the chain is the form's own hash construction.
var digester = canonform.NewBlake3Digester()

// DigestName names the hash construction every digest here uses; it
// is recorded beside digests that outlive this build.
func DigestName() string { return digester.Name() + "/agentrun-chain" }

// ChainDigest is the digest over the predecessor's digest and the entry's
// canonical form (law L31).
func ChainDigest(prev string, form []byte) string {
	h := digester.NewRecord()
	_, _ = h.Write([]byte(prev))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(form)
	return hex.EncodeToString(h.Sum(nil))
}

// ContentDigest is the digest of a body alone, as an endorsement or a
// copy entry names it.
func ContentDigest(body string) string {
	h := digester.NewLeaf()
	_, _ = h.Write([]byte(body))
	return hex.EncodeToString(h.Sum(nil))
}

// chained is the entry with its digests and chain fields set from prev.
func (inst Entry) chained(prevOrdinal uint32, prevDigest string) (e Entry, err error) {
	e = inst.Digested()
	e.Prev = prevOrdinal
	form, err := e.CanonicalForm()
	if err != nil {
		return
	}
	e.Digest = ChainDigest(prevDigest, form)
	return
}

// Verify recomputes the chain over entries in order from the run row's
// digest and returns the first ordinal whose digest does not match, or 0
// when the chain holds (law L31). A body that is neither what its digest
// commits to nor emptied by a tombstone breaks the chain at its entry;
// an emptied body is a tombstone's only when the chain ends with the
// deleted entry, so a body blanked in a live run breaks it too.
func Verify(runDigest string, entries []Entry) (broken uint32, err error) {
	prevOrd, prevDigest := uint32(0), runDigest
	var tombstoned uint32
	for _, e := range entries {
		form, ferr := e.CanonicalForm()
		if ferr != nil {
			return e.Ordinal, ferr
		}
		if e.Prev != prevOrd || e.Digest != ChainDigest(prevDigest, form) || !e.BodiesMatch() {
			return e.Ordinal, nil
		}
		if tombstoned == 0 && e.Tombstoned() {
			tombstoned = e.Ordinal
		}
		prevOrd, prevDigest = e.Ordinal, e.Digest
	}
	if tombstoned != 0 && entries[len(entries)-1].Kind != KindDeleted {
		return tombstoned, nil
	}
	return
}

// VerifyRun is Verify from the run row: the row's digest must be the seed
// its canonical fields reproduce, and the chain must hold from it.
func VerifyRun(r Run, entries []Entry) (broken uint32, err error) {
	if r.Digest != RunDigestSeed(r) {
		err = eb.Build().Str("run", r.Id).Errorf("the run row's digest is not its seed")
		return
	}
	return Verify(r.Digest, entries)
}

// RunDigestSeed is the run row's own digest, the first predecessor of its
// chain: over the run's id and the run row's immutable fields, as the
// store holds them (the owner is the sender the service saw).
func RunDigestSeed(r Run) string {
	h := digester.NewRecord()
	_, _ = h.Write([]byte("run\x00" + r.Id + "\x00" + r.Conversation + "\x00" + r.OwnerApp + "\x00" + r.Coordinator + "\x00" + r.Cell + "\x00" + timeText(r.Created)))
	return hex.EncodeToString(h.Sum(nil))
}

// emptyDigest is the content digest of an empty body.
var emptyDigest = ContentDigest("")

// timeText is the instant as the canonical form spells it; the zero time
// is empty.
func timeText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
