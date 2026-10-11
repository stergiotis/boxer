package agentrun

import (
	"encoding/hex"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonform"
)

// Entry is one journal entry (ADR-0308 §SD1, §SD2): its place in the run,
// its kind and what the kind carries, its label, and the chain digest that
// commits it to everything before it (ADR-0314 §SD1).
type Entry struct {
	// Run is the run's tagged id (ADR-0325 §SD5) and Ordinal the entry's
	// place in it, assigned by the run's single writer, contiguous from 1.
	Run     uint64
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
	// Body is the prose the kind carries: the message text, the note, the
	// tool result, the summary, the section text. The kinds that carry a
	// record carry it in their record field instead (ADR-0325 §SD1).
	Body string
	// Calls are an assistant reply's tool calls, as JSON, so the
	// projection pairs results with calls (law L2); empty otherwise. The
	// vault holds them shredded, and the writer stores them in jsonshred's
	// canonical form, which is the form they read back in.
	Calls string
	// Arguments are a tool call's arguments as the model wrote them, in
	// canonical form when they parse as JSON (an intent's; ADR-0325 §SD1).
	// The vault holds parsed arguments shredded and others as text.
	Arguments string
	// The records (ADR-0325 §SD1): at most one per entry but an approval's,
	// whose content part rides beside its decision. Request, Reply,
	// Compact, Head, Profile, TurnEnd, Tools and an approval's decision are
	// the ledger's; Endorse, Grant, Question, Answer, Copy and an
	// approval's justification and digest are the vault's.
	Request  *RequestMeta
	Reply    *ReplyMeta
	Compact  *CompactIntent
	Approval *Approval
	Head     *HeadRecord
	Profile  *ProfileRecord
	TurnEnd  *TurnState
	Tools    *ToolCatalog
	Endorse  *Endorsement
	Grant    *GrantRecord
	Question *ExitQuestion
	Answer   *ExitAnswer
	Copy     *CopyRecord
	// VaultCommitment is the commitment to the entry's vault row under its
	// Opening (ADR-0325 §SD3, amending ADR-0320 §SD2), set by the writer.
	// The chain commits to it, not the content, so an erasure that
	// destroys the row and its opening leaves the chain verifying (law
	// L31) and the anchored heads with it (law L32), and leaves nothing
	// retained that reveals the content.
	VaultCommitment string
	// Opening is the entry's commitment opening: drawn by the writer for
	// an entry with content, carried on the bus, kept in the vault row
	// beside the content and destroyed with it; never in the canonical
	// form, a ledger column or an anchor. Empty for an entry without
	// content, and for an erased one.
	Opening []byte
	// EntryId is the entry's tagged id (ADR-0325 §SD5), assigned by the
	// store at append; identity, not content, so it is not in the
	// canonical form. Zero until the store assigns one.
	EntryId uint64
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
	WriterRun     uint64
	WriterTurn    string
	WriterOrdinal uint32
	// Prev is the predecessor's ordinal and Digest the chain digest over
	// the predecessor's digest and this entry's canonical form; the first
	// entry chains to the run row's digest.
	Prev   uint32
	Digest string
	// KeyId names the commitment scheme (CommitScheme) on an entry that
	// carries a commitment, so a verifier knows how to open it; empty
	// otherwise. The column ADR-0308 §SD8 reserved.
	KeyId string
	// Parent is reserved for branching, zero until a consumer asks.
	Parent uint32
	// Attachments are the images a message carries, as JSON: each a media
	// type and the digest the artefact's store keeps the bytes under, so
	// the journal holds no pixels and the encoder resolves them at the
	// wire.
	Attachments string
}

// CanonicalForm is the entry's canonical form (ADR-0325 §SD2): the
// canonical wire item of its ledger row, with the chain columns, the
// revision and the entry id at their zero values, so the item is a
// function of what the entry says, not of where the chain or the store put
// it. Content enters as the vault commitment; marks as arrays; records as
// their components.
func (inst Entry) CanonicalForm() (b []byte, err error) {
	e := inst
	e.Prev, e.Digest, e.EntryId = 0, "", 0
	b, err = hasher.LedgerItem(e.Run, uint64(e.Ordinal), func(lb *agentrunstore.EntryEntityBuilder) {
		if ferr := fillLedger(lb, e); ferr != nil {
			err = ferr
		}
	})
	if err != nil {
		err = eh.Errorf("canonical form: %w", err)
	}
	return
}

// HasContent says the entry carries something the vault holds: prose,
// calls, arguments, or a record the vault keeps.
func (inst Entry) HasContent() bool {
	return inst.Body != "" || inst.Calls != "" || inst.Arguments != "" || inst.Endorse != nil || inst.Grant != nil ||
		inst.Question != nil || inst.Answer != nil || inst.Copy != nil || inst.Approval != nil
}

// vaultItem is the canonical item of the entry's vault row without its
// opening: what the commitment covers.
func (inst Entry) vaultItem() (item []byte, err error) {
	body, _ := bodyOf(inst)
	return hasher.VaultItem(inst.Run, uint64(inst.Ordinal), func(b *agentrunstore.BodyRowEntityBuilder) error {
		return fillVault(b, body, false)
	})
}

// Digested is the entry as the writer chains it: calls, arguments and
// attachments in the canonical form they read back in, and the vault
// commitment set over the content under the opening, drawing the opening
// when the entry has content and none yet. An entry without content has
// no opening and no commitment.
func (inst Entry) Digested() (e Entry, err error) {
	e = inst
	if e.Calls != "" {
		var ok bool
		if e.Calls, ok = canonicalJSON(e.Calls); !ok {
			return e, eb.Build().Uint64("run", e.Run).Errorf("the calls are not JSON")
		}
	}
	if e.Arguments != "" {
		e.Arguments, _ = canonicalJSON(e.Arguments)
	}
	if e.Attachments, err = canonicalAttachments(e.Attachments); err != nil {
		return
	}
	if !e.HasContent() {
		e.Opening, e.VaultCommitment, e.KeyId = nil, "", ""
		return
	}
	if len(e.Opening) != OpeningSize {
		e.Opening = NewOpening()
	}
	item, err := e.vaultItem()
	if err != nil {
		return
	}
	e.VaultCommitment = CommitVault(e.Opening, item)
	e.KeyId = CommitScheme
	return
}

// Tombstoned says the entry's content was erased after it was written: a
// commitment stands and no vault row opened it. What a deleted run's
// entries are (ADR-0308 §SD8), and an erased entry (ADR-0320 §SD4).
func (inst Entry) Tombstoned() bool {
	return inst.VaultCommitment != "" && len(inst.Opening) == 0
}

// Erased is the entry with its content and opening destroyed, the
// commitment and the ledger's records standing: what the vault's deletion
// leaves.
func (inst Entry) Erased() (e Entry) {
	e = inst
	e.Body, e.Calls, e.Arguments, e.Opening = "", "", "", nil
	e.Endorse, e.Grant, e.Question, e.Answer, e.Copy = nil, nil, nil, nil, nil
	if inst.Approval != nil {
		a := *inst.Approval
		a.Justification, a.Digest = "", ""
		e.Approval = &a
	}
	return
}

// BodiesMatch says the entry's content opens its commitment: with an
// opening, the vault row it describes commits to the commitment; without
// one, the entry carries no content the commitment would have to cover.
func (inst Entry) BodiesMatch() bool {
	if len(inst.Opening) == 0 {
		return inst.Body == "" && inst.Calls == "" && inst.Arguments == "" && inst.Endorse == nil && inst.Grant == nil &&
			inst.Question == nil && inst.Answer == nil && inst.Copy == nil &&
			(inst.Approval == nil || inst.Approval.Justification == "" && inst.Approval.Digest == "")
	}
	item, err := inst.vaultItem()
	return err == nil && OpensVault(inst.Opening, item, inst.VaultCommitment)
}

// digester keys the chain digests the way the canonical record form keys
// its record digests, so the chain is the form's own hash construction.
var digester = canonform.NewBlake3Digester()

// DigestName names the hash construction every digest here uses; it
// is recorded beside digests that outlive this build. The form's third
// version is the canonical wire item of the ledger row (ADR-0325 §SD2);
// the second held per-field commitments (ADR-0320 §SD3).
func DigestName() string { return digester.Name() + "/agentrun-chain/v3" }

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

// ArgumentsDigest is the digest an endorsement names a tool call's
// arguments by (ADR-0325 §SD4): over the canonical wire item of the
// arguments' shredded attributes, on a vault row that holds nothing else,
// so two spellings of the same arguments are one digest. Arguments that do
// not parse are hashed as the text they are.
func ArgumentsDigest(args string) (digest string, err error) {
	item, err := hasher.VaultItem(0, 0, func(b *agentrunstore.BodyRowEntityBuilder) error {
		return fillVault(b, Body{Arguments: args}, false)
	})
	if err != nil {
		return "", eh.Errorf("arguments digest: %w", err)
	}
	h := digester.NewLeaf()
	_, _ = h.Write(item)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// chained is the entry with its digests and chain fields set from prev.
func (inst Entry) chained(prevOrdinal uint32, prevDigest string) (e Entry, err error) {
	if e, err = inst.Digested(); err != nil {
		return
	}
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
// when the chain holds (law L31). A field that neither opens its
// commitment nor was erased breaks the chain at its entry; an erased
// field is an erasure's only when the chain says so, the run's deleted
// entry at its end or an erased entry naming the ordinal (ADR-0320
// §SD3), so content blanked in a live run breaks it too.
func Verify(runDigest string, entries []Entry) (broken uint32, err error) {
	prevOrd, prevDigest := uint32(0), runDigest
	erased := make(map[uint32]bool)
	for _, e := range entries {
		if e.Kind == KindErased {
			erased[e.Target] = true
		}
	}
	deleted := len(entries) > 0 && entries[len(entries)-1].Kind == KindDeleted
	for _, e := range entries {
		form, ferr := e.CanonicalForm()
		if ferr != nil {
			return e.Ordinal, ferr
		}
		if e.Prev != prevOrd || e.Digest != ChainDigest(prevDigest, form) || !e.BodiesMatch() {
			return e.Ordinal, nil
		}
		if e.Tombstoned() && !deleted && !erased[e.Ordinal] {
			return e.Ordinal, nil
		}
		prevOrd, prevDigest = e.Ordinal, e.Digest
	}
	return
}

// VerifyRun is Verify from the run row: the row's digest must be the seed
// its canonical fields reproduce, and the chain must hold from it.
func VerifyRun(r Run, entries []Entry) (broken uint32, err error) {
	seed, err := RunDigestSeed(r)
	if err != nil {
		return
	}
	if r.Digest != seed {
		err = eb.Build().Uint64("run", r.Id).Errorf("the run row's digest is not its seed")
		return
	}
	return Verify(r.Digest, entries)
}

// RunDigestSeed is the run row's own digest, the first predecessor of its
// chain: over the canonical wire item of the run row with only its
// immutable attributes set — the id, the cell, the conversation, the
// owner, the coordinator and the creation instant, as the store holds
// them (the owner is the sender the service saw; ADR-0325 §SD2).
func RunDigestSeed(r Run) (seed string, err error) {
	item, err := hasher.RunItem(agentrunstore.Run{ID: r.Id, Cell: r.Cell, Conversation: r.Conversation, OwnerApp: r.OwnerApp,
		Coordinator: r.Coordinator, Created: utc(r.Created)})
	if err != nil {
		return "", eb.Build().Uint64("run", r.Id).Errorf("run seed: %w", err)
	}
	h := digester.NewRecord()
	_, _ = h.Write(item)
	return hex.EncodeToString(h.Sum(nil)), nil
}
