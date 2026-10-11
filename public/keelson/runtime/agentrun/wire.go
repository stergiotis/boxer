package agentrun

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/observability/eh"
)

const wireVersion = 1

// wireRun is a run row on the bus.
type wireRun struct {
	Id            string    `json:"id"`
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
	Run           string    `json:"run"`
	Ordinal       uint32    `json:"ordinal"`
	At            time.Time `json:"at"`
	Kind          string    `json:"kind"`
	Key           string    `json:"key,omitempty"`
	Role          string    `json:"role,omitempty"`
	Body          string    `json:"body,omitempty"`
	Calls         string    `json:"calls,omitempty"`
	BodyDigest    string    `json:"body_digest,omitempty"`
	CallsDigest   string    `json:"calls_digest,omitempty"`
	Name          string    `json:"name,omitempty"`
	Book          string    `json:"book,omitempty"`
	Form          string    `json:"form,omitempty"`
	Target        uint32    `json:"target,omitempty"`
	From          uint32    `json:"from,omitempty"`
	Effect        string    `json:"effect,omitempty"`
	Turn          string    `json:"turn,omitempty"`
	LabelLevel    uint8     `json:"label_level,omitempty"`
	LabelTier     uint8     `json:"label_tier,omitempty"`
	LabelExposure []string  `json:"label_exposure,omitempty"`
	LabelOrigin   []string  `json:"label_origin,omitempty"`
	WriterRun     string    `json:"writer_run,omitempty"`
	WriterTurn    string    `json:"writer_turn,omitempty"`
	WriterOrdinal uint32    `json:"writer_ordinal,omitempty"`
	Prev          uint32    `json:"prev,omitempty"`
	Digest        string    `json:"digest,omitempty"`
	KeyId         string    `json:"key_id,omitempty"`
	Parent        uint32    `json:"parent,omitempty"`
	Attachments   string    `json:"attachments,omitempty"`
}

func wireOfEntry(e Entry) wireEntry {
	return wireEntry{Run: e.Run, Ordinal: e.Ordinal, At: e.At.UTC(), Kind: e.Kind.String(), Key: e.Key, Role: e.Role, Body: e.Body, Calls: e.Calls, BodyDigest: e.BodyDigest, CallsDigest: e.CallsDigest,
		Name: e.Name, Book: e.Book, Form: e.Form, Target: e.Target, From: e.From, Effect: e.Effect, Turn: e.Turn,
		LabelLevel: uint8(e.Label.Level), LabelTier: uint8(e.Label.Tier), LabelExposure: e.Label.Exposure, LabelOrigin: e.Label.Origin,
		WriterRun: e.WriterRun, WriterTurn: e.WriterTurn, WriterOrdinal: e.WriterOrdinal, Prev: e.Prev, Digest: e.Digest, KeyId: e.KeyId, Parent: e.Parent, Attachments: e.Attachments}
}

func entryOfWire(w wireEntry) Entry {
	k, _ := ParseKind(w.Kind)
	return Entry{Run: w.Run, Ordinal: w.Ordinal, At: zeroed(w.At), Kind: k, Key: w.Key, Role: w.Role, Body: w.Body, Calls: w.Calls, BodyDigest: w.BodyDigest, CallsDigest: w.CallsDigest,
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
	Id    string `json:"id"`
	From  uint32 `json:"from,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type wireAppendRequest struct {
	V       uint8       `json:"v"`
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
