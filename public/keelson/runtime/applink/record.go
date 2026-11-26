package applink

import (
	"encoding/hex"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// Record is what a handle names (ADR-0409 §SD6, §SD7): a resolved link —
// target, call, config — its author's label and, for a link a task minted,
// the grant it was minted under. The host keeps it in memory; the handle is
// the keyed digest of its canonical CBOR, so a record cannot change under
// its handle.
type Record struct {
	V    uint8 `json:"v"`
	Verb VerbE `json:"verb"`
	// App is the app's alias; Kind the launch kind, for a kind's config.
	App  string `json:"app,omitempty"`
	Kind string `json:"kind,omitempty"`
	// Window names one window: the record raises it and nothing else.
	Window    uint64 `json:"window,omitempty"`
	Operation string `json:"operation,omitempty"`
	Version   uint16 `json:"version,omitempty"`
	// Args is the operation's CBOR, Config the kind's.
	Args   []byte `json:"args,omitempty"`
	Config []byte `json:"config,omitempty"`
	// AuthorLevel and AuthorTier are the ADR-0311 label of who wrote it.
	AuthorLevel uint8 `json:"author_level,omitempty"`
	AuthorTier  uint8 `json:"author_tier,omitempty"`
	// Task, Epoch and Conversation name the grant a task minted it under;
	// empty for a link the person minted.
	Task         string `json:"task,omitempty"`
	Epoch        uint64 `json:"epoch,omitempty"`
	Conversation string `json:"conversation,omitempty"`
	// Expects are the revisions the task had read of what the call writes,
	// in the window it names; a call into a window that moved since is a
	// conflict.
	Expects map[string]uint64 `json:"expects,omitempty"`
}

// RecordVersion is the record's wire version.
const RecordVersion = 1

// Canonical is the record's canonical CBOR, what its handle is minted
// over.
func (inst Record) Canonical() (b []byte, err error) {
	return buscodec.Encode(inst)
}

// Digest names the call a link resolves to, whatever spelled it and
// whoever wrote it: the hex of BLAKE3-256 over the target, the call and the
// config. Two spellings of one call are one digest; the follow record
// carries it (ADR-0409 §SD9).
func (inst Record) Digest() (d string) {
	call := Record{V: RecordVersion, Verb: inst.Verb, App: inst.App, Kind: inst.Kind, Window: inst.Window,
		Operation: inst.Operation, Version: inst.Version, Args: inst.Args, Config: inst.Config}
	b, err := call.Canonical()
	if err != nil {
		return ""
	}
	sum := blake3.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// FromTask says a task minted the record under its grant.
func (inst Record) FromTask() bool {
	return inst.Task != ""
}
