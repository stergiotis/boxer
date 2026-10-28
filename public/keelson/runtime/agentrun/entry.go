package agentrun

import (
	"context"
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// scratch is the journal's stores with no executor: what computes
// canonical items, prepares and checks rows without a server (ADR-0325
// §SD2). Safe for concurrent use, as the generated stores' canonical-item
// verbs are.
var scratch = agentrunstore.NewScratch()

// ChainScheme is the hash constructions the chain's digests and the
// vault's commitments are under (ADR-0327 §SD1, §SD9).
var ChainScheme = agentrunstore.ChainScheme

// WrittenBy is the identity an entry was committed under (ADR-0327 §SD6,
// law L35): the call identity's origin and claims, the insert batch, and
// how far a reader may trust the origin and the principal. The store that
// writes sets it from its context — the host's service from the sender it
// vouches for — and the chain commits to it.
type WrittenBy struct {
	Run         string `json:"run,omitempty"`
	App         string `json:"app,omitempty"`
	Instance    uint64 `json:"instance,omitempty"`
	OriginBy    string `json:"origin_by,omitempty"`
	Principal   string `json:"principal,omitempty"`
	PrincipalBy string `json:"principal_by,omitempty"`
	Purpose     string `json:"purpose,omitempty"`
	Correlation string `json:"correlation,omitempty"`
	Batch       string `json:"batch,omitempty"`
}

// Entry is one journal entry (ADR-0308 §SD1, §SD2): its place in the run,
// its kind and what the kind carries, its label, and the chain digest that
// commits it to everything before it (ADR-0314 §SD1). The chain, the
// commitments and the written-by identity are the evidence profile's
// (ADR-0327 §SD9); the store that writes an entry sets them.
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
	// vault holds them shredded, in jsonshred's canonical form, which is
	// the form they read back in.
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
	Verdict  *Verdict
	// Commitments are the commitments to the entry's content, one per
	// vaulted component (ADR-0327 §SD4), under the Openings; the chain
	// commits to them, not the content, so an erasure that destroys a vault
	// row with its opening leaves the chain verifying (law L31) and the
	// anchored heads with it (law L32), and leaves nothing retained that
	// reveals the content. Keyed by the component's kind tag.
	Commitments map[string]string
	// Openings are the commitment openings, by component: drawn by the
	// store that writes the entry, carried on the bus, kept on the vault
	// rows beside the content and destroyed with them; never in the
	// canonical form, a ledger column or an anchor. Empty for an entry
	// without content, and for an erased one.
	Openings map[string][]byte
	// EntryId is the entry's tagged id (ADR-0325 §SD5), assigned by the
	// store at append and in the canonical form, so the store assigns it
	// before it prepares the row. Zero until the store assigns one.
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
	// entry chains to the run row's digest. A writer sets Prev to the head
	// it expects; the store sets the digest.
	Prev   uint32
	Digest string
	// KeyId names the scheme the digest and the commitments are under
	// (ChainScheme.Name): the column ADR-0308 §SD8 reserved.
	KeyId string
	// Parent is reserved for branching, zero until a consumer asks.
	Parent uint32
	// Attachments are the images a message carries, as JSON: each a media
	// type and the digest the artefact's store keeps the bytes under, so
	// the journal holds no pixels and the encoder resolves them at the
	// wire.
	Attachments string
	// WrittenBy is the identity the entry was committed under; nil until
	// the store prepares the entry.
	WrittenBy *WrittenBy
}

// CanonicalForm is the entry's canonical form (ADR-0325 §SD2): the
// canonical wire item of its ledger row, with the chain's Prev and Digest
// and the revision at their zero values, so the item is a function of what
// the entry says, not of where the chain or the store put it. Content
// enters as the commitments; marks as arrays; records as their components.
func (inst Entry) CanonicalForm() (b []byte, err error) {
	ent, err := ledgerEntity(inst)
	if err != nil {
		return nil, eh.Errorf("canonical form: %w", err)
	}
	ch := ent.EvidenceChain.Val
	ch.Prev, ch.Digest = 0, ""
	ent.EvidenceChain = option.Some(ch)
	ent.Revision = 0
	if b, err = scratch.Entry.CanonicalItem(ent); err != nil {
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

// Tombstoned says the entry's content was erased after it was written:
// commitments stand and no opening is there to open them. What a deleted
// run's entries are (ADR-0308 §SD8), and an erased entry (ADR-0320 §SD4).
func (inst Entry) Tombstoned() bool {
	return len(inst.Commitments) > 0 && len(inst.Openings) == 0
}

// Erased is the entry with its content and openings destroyed, the
// commitments and the ledger's records standing: what the vault's
// deletion leaves.
func (inst Entry) Erased() (e Entry) {
	e = inst
	e.Body, e.Calls, e.Arguments, e.Openings = "", "", "", nil
	e.Endorse, e.Grant, e.Question, e.Answer, e.Copy = nil, nil, nil, nil, nil
	if inst.Approval != nil {
		a := *inst.Approval
		a.Justification, a.Digest = "", ""
		e.Approval = &a
	}
	return
}

// BodiesMatch says the entry's content opens its commitments: with
// openings, every vaulted component the entry carries opens the
// commitment the entry carries for it, and there is one for each; without
// openings, the entry carries no content the commitments would have to
// cover.
func (inst Entry) BodiesMatch() bool {
	rows, err := contentRows(inst)
	if err != nil {
		return false
	}
	if len(inst.Openings) == 0 {
		return len(rows) == 0
	}
	if len(rows) != len(inst.Commitments) {
		return false
	}
	for _, v := range rows {
		component := v.Component
		opening, ok := inst.Openings[component]
		commitment, has := inst.Commitments[component]
		if !ok || !has {
			return false
		}
		c := *v
		c.EvidenceOpening = option.None[agentrunstore.EvidenceOpening]()
		item, ierr := scratch.Vault.CanonicalItem(&c)
		if ierr != nil || !ChainScheme.Opens(opening, item, commitment) {
			return false
		}
	}
	return true
}

// prepared is the entry as the store writes it after the predecessor at
// head, under the identity on ctx (ADR-0327 §SD9): calls, arguments and
// attachments in the canonical form they read back in, the content split
// into vault rows with their openings and committed to on the chain
// component, and the digest over the canonical form chained to head's.
// EntryId is in the form, so the caller assigns it first.
func (inst Entry) prepared(ctx context.Context, head evidence.Head) (e Entry, err error) {
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
	e.Openings, e.Commitments, e.WrittenBy = nil, nil, nil
	ent, err := ledgerEntity(e)
	if err != nil {
		return
	}
	rows, err := contentRows(e)
	if err != nil {
		return
	}
	if _, err = scratch.Vaulted.Prepare(ctx, head, ent, rows); err != nil {
		return e, eb.Build().Uint64("run", e.Run).Uint32("ordinal", e.Ordinal).Errorf("prepare: %w", err)
	}
	return withPrepared(e, ent, rows), nil
}

// Verify recomputes the chain over entries in order from the run row's
// digest and returns the first ordinal whose digest does not match, or 0
// when the chain holds (law L31). A component that neither opens its
// commitment nor was erased breaks the chain at its entry; an erased
// component is an erasure's only when the chain says so, the run's deleted
// entry at its end or an erased entry naming the ordinal (ADR-0320
// §SD3), so content blanked in a live run breaks it too.
func Verify(runDigest string, entries []Entry) (broken uint32, err error) {
	rows, err := Rows(entries)
	if err != nil {
		return
	}
	b, _, err := evidence.Verify(ChainScheme, runDigest, rows)
	return uint32(b), err
}

// Rows are the entries as the laws read them (evidence.RowI): the ledger
// rows with the vault rows' openings and items attached.
func Rows(entries []Entry) (rows []evidence.RowI, err error) {
	ledger := make([]*agentrunstore.EntryEntity, 0, len(entries))
	var content []*agentrunstore.EntryVaultEntity
	for _, e := range entries {
		ent, lerr := ledgerEntity(e)
		if lerr != nil {
			return nil, eb.Build().Uint64("run", e.Run).Uint32("ordinal", e.Ordinal).Errorf("rows: %w", lerr)
		}
		ledger = append(ledger, ent)
		vs, verr := contentRows(e)
		if verr != nil {
			return nil, eb.Build().Uint64("run", e.Run).Uint32("ordinal", e.Ordinal).Errorf("rows: %w", verr)
		}
		content = append(content, vs...)
	}
	rows, _ = scratch.Vaulted.Join(ledger, content)
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

// DigestName names the hash construction every digest here uses; it is
// recorded beside digests that outlive this build (ChainScheme.Name).
func DigestName() string { return ChainScheme.Name() }

// ChainDigest is the digest over the predecessor's digest and the entry's
// canonical form (law L31).
func ChainDigest(prev string, form []byte) string { return ChainScheme.ChainDigest(prev, form) }

// ContentDigest is the digest of a body alone, as an endorsement or a
// copy entry names it.
func ContentDigest(body string) string { return ChainScheme.ContentDigest([]byte(body)) }

// ArgumentsDigest is the digest an endorsement names a tool call's
// arguments by (ADR-0325 §SD4): over the canonical wire item of the
// arguments' shredded component, on a vault row that holds nothing else,
// so two spellings of the same arguments are one digest. Arguments that do
// not parse are hashed as the text they are.
func ArgumentsDigest(args string) (digest string, err error) {
	rows, err := contentRows(Entry{Arguments: args})
	if err != nil {
		return "", eh.Errorf("arguments digest: %w", err)
	}
	if len(rows) != 1 {
		return "", eh.Errorf("arguments digest: the arguments make %d vault rows, not one", len(rows))
	}
	item, err := scratch.Vault.CanonicalItem(rows[0])
	if err != nil {
		return "", eh.Errorf("arguments digest: %w", err)
	}
	return ChainScheme.ContentDigest(item), nil
}

// RunDigestSeed is the run row's own digest, the first predecessor of its
// chain: over the canonical wire item of the run row with only its
// immutable attributes set — the id, the cell, the conversation, the
// owner, the coordinator and the creation instant, as the store holds
// them (the owner is the sender the service saw; ADR-0325 §SD2) — chained
// to nothing.
func RunDigestSeed(r Run) (seed string, err error) {
	ent := &agentrunstore.RunEntity{ID: r.Id}
	ent.Run = option.Some(agentrunstore.Run{ID: r.Id, Cell: r.Cell, Conversation: r.Conversation, OwnerApp: r.OwnerApp,
		Coordinator: r.Coordinator, Created: utc(r.Created)})
	item, err := scratch.Run.CanonicalItem(ent)
	if err != nil {
		return "", eb.Build().Uint64("run", r.Id).Errorf("run seed: %w", err)
	}
	return ChainScheme.ChainDigest("", item), nil
}
