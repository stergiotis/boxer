package agentrun

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

const wireVersion = 3

// wireRun is a run row on the bus.
type wireRun struct {
	Id            uint64    `json:"id"`
	Cell          string    `json:"cell,omitempty"`
	Conversation  string    `json:"conversation,omitempty"`
	OwnerApp      string    `json:"owner_app,omitempty"`
	Coordinator   string    `json:"coordinator,omitempty"`
	State         string    `json:"state,omitempty"`
	Fingerprint   string    `json:"fingerprint,omitempty"`
	Build         string    `json:"build,omitempty"`
	Profile       string    `json:"profile,omitempty"`
	Settings      string    `json:"settings,omitempty"`
	LabelLevel    uint8     `json:"label_level,omitempty"`
	LabelTier     uint8     `json:"label_tier,omitempty"`
	LabelExposure []string  `json:"label_exposure,omitempty"`
	LabelOrigin   []string  `json:"label_origin,omitempty"`
	Created       time.Time `json:"created"`
	LastOrdinal   uint32    `json:"last_ordinal,omitempty"`
	Closed        time.Time `json:"closed"`
	Digest        string    `json:"digest,omitempty"`
	Head          string    `json:"head,omitempty"`
	Version       uint32    `json:"version,omitempty"`
	DeletedBy     string    `json:"deleted_by,omitempty"`
	DeletedAt     time.Time `json:"deleted_at"`
}

func wireOfRun(r Run) wireRun {
	return wireRun{Id: r.Id, Cell: r.Cell, Conversation: r.Conversation, OwnerApp: r.OwnerApp, Coordinator: r.Coordinator,
		State: r.State.String(), Fingerprint: r.Fingerprint, Build: r.Build, Profile: r.Profile, Settings: r.Settings,
		LabelLevel: uint8(r.Label.Level), LabelTier: uint8(r.Label.Tier), LabelExposure: r.Label.Exposure, LabelOrigin: r.Label.Origin,
		Created: r.Created.UTC(), LastOrdinal: r.LastOrdinal, Closed: r.Closed.UTC(), Digest: r.Digest, Head: r.Head, Version: r.Version, DeletedBy: r.DeletedBy, DeletedAt: r.DeletedAt.UTC()}
}

func runOfWire(w wireRun) Run {
	st, _ := ParseState(w.State)
	return Run{Id: w.Id, Cell: w.Cell, Conversation: w.Conversation, OwnerApp: w.OwnerApp, Coordinator: w.Coordinator,
		State: st, Fingerprint: w.Fingerprint, Build: w.Build, Profile: w.Profile, Settings: w.Settings,
		Label:   Label{Level: LevelE(w.LabelLevel), Tier: TierE(w.LabelTier), Exposure: unionMarks(w.LabelExposure, nil), Origin: unionMarks(w.LabelOrigin, nil)},
		Created: zeroed(w.Created), LastOrdinal: w.LastOrdinal, Closed: zeroed(w.Closed), Digest: w.Digest, Head: w.Head, Version: w.Version, DeletedBy: w.DeletedBy, DeletedAt: zeroed(w.DeletedAt)}
}

// wireEntry is an entry on the bus.
type wireEntry struct {
	Run       uint64    `json:"run"`
	Ordinal   uint32    `json:"ordinal"`
	At        time.Time `json:"at"`
	Kind      string    `json:"kind"`
	Key       string    `json:"key,omitempty"`
	Role      string    `json:"role,omitempty"`
	Body      string    `json:"body,omitempty"`
	Calls     string    `json:"calls,omitempty"`
	Arguments string    `json:"arguments,omitempty"`
	// The records (ADR-0325 §SD1).
	Request  *RequestMeta   `json:"request,omitempty"`
	Reply    *ReplyMeta     `json:"reply,omitempty"`
	Compact  *CompactIntent `json:"compact,omitempty"`
	Approval *Approval      `json:"approval,omitempty"`
	Head     *HeadRecord    `json:"head,omitempty"`
	Profile  *ProfileRecord `json:"profile,omitempty"`
	TurnEnd  *TurnState     `json:"turn_end,omitempty"`
	Tools    *ToolCatalog   `json:"tools,omitempty"`
	Endorse  *Endorsement   `json:"endorse,omitempty"`
	Grant    *GrantRecord   `json:"grant,omitempty"`
	Question *ExitQuestion  `json:"question,omitempty"`
	Answer   *ExitAnswer    `json:"answer,omitempty"`
	Copy     *CopyRecord    `json:"copy,omitempty"`
	Verdict  *Verdict       `json:"verdict,omitempty"`
	// Commitments are the commitments to the content by component, and
	// Openings the openings they open under, hex; the openings travel with
	// the bodies and are held only by the vault (ADR-0320 §SD2, ADR-0327
	// §SD4).
	Commitments map[string]string `json:"commitments,omitempty"`
	Openings    map[string]string `json:"openings,omitempty"`
	// WrittenBy is the identity the store committed the entry under.
	WrittenBy     *WrittenBy `json:"written_by,omitempty"`
	EntryId       uint64     `json:"entry_id,omitempty"`
	Name          string     `json:"name,omitempty"`
	Book          string     `json:"book,omitempty"`
	Form          string     `json:"form,omitempty"`
	Target        uint32     `json:"target,omitempty"`
	From          uint32     `json:"from,omitempty"`
	Effect        string     `json:"effect,omitempty"`
	Turn          string     `json:"turn,omitempty"`
	LabelLevel    uint8      `json:"label_level,omitempty"`
	LabelTier     uint8      `json:"label_tier,omitempty"`
	LabelExposure []string   `json:"label_exposure,omitempty"`
	LabelOrigin   []string   `json:"label_origin,omitempty"`
	WriterRun     uint64     `json:"writer_run,omitempty"`
	WriterTurn    string     `json:"writer_turn,omitempty"`
	WriterOrdinal uint32     `json:"writer_ordinal,omitempty"`
	Prev          uint32     `json:"prev,omitempty"`
	Digest        string     `json:"digest,omitempty"`
	KeyId         string     `json:"key_id,omitempty"`
	Parent        uint32     `json:"parent,omitempty"`
	Attachments   string     `json:"attachments,omitempty"`
}

func wireOfEntry(e Entry) wireEntry {
	var openings map[string]string
	if len(e.Openings) > 0 {
		openings = make(map[string]string, len(e.Openings))
		for component, o := range e.Openings {
			openings[component] = evidence.OpeningText(o)
		}
	}
	return wireEntry{Run: e.Run, Ordinal: e.Ordinal, At: e.At.UTC(), Kind: e.Kind.String(), Key: e.Key, Role: e.Role, Body: e.Body, Calls: e.Calls, Arguments: e.Arguments,
		Request: e.Request, Reply: e.Reply, Compact: e.Compact, Approval: e.Approval, Head: e.Head, Profile: e.Profile, TurnEnd: e.TurnEnd, Tools: e.Tools,
		Endorse: e.Endorse, Grant: e.Grant, Question: e.Question, Answer: e.Answer, Copy: e.Copy, Verdict: e.Verdict, Commitments: e.Commitments, Openings: openings, WrittenBy: e.WrittenBy, EntryId: e.EntryId,
		Name: e.Name, Book: e.Book, Form: e.Form, Target: e.Target, From: e.From, Effect: e.Effect, Turn: e.Turn,
		LabelLevel: uint8(e.Label.Level), LabelTier: uint8(e.Label.Tier), LabelExposure: e.Label.Exposure, LabelOrigin: e.Label.Origin,
		WriterRun: e.WriterRun, WriterTurn: e.WriterTurn, WriterOrdinal: e.WriterOrdinal, Prev: e.Prev, Digest: e.Digest, KeyId: e.KeyId, Parent: e.Parent, Attachments: e.Attachments}
}

func entryOfWire(w wireEntry) Entry {
	k, _ := ParseKind(w.Kind)
	var openings map[string][]byte
	for component, text := range w.Openings {
		if o := evidence.OpeningOf(text); o != nil {
			if openings == nil {
				openings = make(map[string][]byte, len(w.Openings))
			}
			openings[component] = o
		}
	}
	return Entry{Run: w.Run, Ordinal: w.Ordinal, At: zeroed(w.At), Kind: k, Key: w.Key, Role: w.Role, Body: w.Body, Calls: w.Calls, Arguments: w.Arguments,
		Request: w.Request, Reply: w.Reply, Compact: w.Compact, Approval: w.Approval, Head: w.Head, Profile: w.Profile, TurnEnd: w.TurnEnd, Tools: w.Tools,
		Endorse: w.Endorse, Grant: w.Grant, Question: w.Question, Answer: w.Answer, Copy: w.Copy, Verdict: w.Verdict, Commitments: w.Commitments, Openings: openings, WrittenBy: w.WrittenBy, EntryId: w.EntryId,
		Name: w.Name, Book: w.Book, Form: w.Form, Target: w.Target, From: w.From, Effect: w.Effect, Turn: w.Turn,
		Label:     Label{Level: LevelE(w.LabelLevel), Tier: TierE(w.LabelTier), Exposure: unionMarks(w.LabelExposure, nil), Origin: unionMarks(w.LabelOrigin, nil)},
		WriterRun: w.WriterRun, WriterTurn: w.WriterTurn, WriterOrdinal: w.WriterOrdinal, Prev: w.Prev, Digest: w.Digest, KeyId: w.KeyId, Parent: w.Parent, Attachments: w.Attachments}
}

type wireRunRequest struct {
	V   uint8   `json:"v"`
	Run wireRun `json:"run"`
}

type wireIdRequest struct {
	V     uint8  `json:"v"`
	Id    uint64 `json:"id"`
	From  uint32 `json:"from,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

// wireEraseRequest names the entries of a run whose content is erased.
type wireEraseRequest struct {
	V        uint8    `json:"v"`
	Id       uint64   `json:"id"`
	Ordinals []uint32 `json:"ordinals"`
}

// wireAppendRequest carries the entries a writer wants appended, with
// their ordinals and the head digest the writer expects, so the service
// refuses a writer whose run moved since it read it.
type wireAppendRequest struct {
	V       uint8       `json:"v"`
	Head    string      `json:"head,omitempty"`
	Entries []wireEntry `json:"entries"`
}

type wireAck struct {
	V      uint8  `json:"v"`
	Ok     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

type wireRunReply struct {
	wireAck
	Found bool      `json:"found,omitempty"`
	Run   wireRun   `json:"run"`
	Runs  []wireRun `json:"runs,omitempty"`
}

// wireMintReply carries a run id the service's store minted.
type wireMintReply struct {
	wireAck
	Id uint64 `json:"id"`
}

// wireAppendReply carries the entries as the store prepared and wrote
// them, in order: ids, digests, commitments, openings and the identity.
type wireAppendReply struct {
	wireAck
	Entries []wireEntry `json:"entries,omitempty"`
}

type wireEntriesReply struct {
	wireAck
	Entries []wireEntry `json:"entries,omitempty"`
}

func encode[T any](v T) (b []byte, err error) {
	if b, err = buscodec.Encode(v); err != nil {
		err = eh.Errorf("encode: %w", err)
	}
	return
}

func decode[T any](b []byte) (v T, err error) {
	if v, err = buscodec.Decode[T](b); err != nil {
		err = eh.Errorf("decode: %w", err)
	}
	return
}
