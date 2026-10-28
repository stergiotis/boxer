package agentrun

import (
	"encoding/json/v2"
	"time"
)

// The records an entry carries beside its prose (ADR-0325 §SD1). Each is a
// component of the ledger or of the vault: an attribute that holds content,
// or a deterministic digest of content (ADR-0320 §SD6), lives in the vault
// and is erased with the entry's content; the rest is the ledger's and
// stays. The json tags are the bodies these records used to be written as,
// which RecordBody renders for the readers that still read text: the
// introspection tables and the laws over them.

// RequestMeta is a request entry's record (ledger): the request's digest
// and its size.
type RequestMeta struct {
	Digest   string `json:"digest"`
	Messages uint32 `json:"messages"`
	Tools    uint32 `json:"tools"`
	Round    uint32 `json:"round"`
	Label    string `json:"label"`
}

// ReplyMeta is a model call's reply record (ledger): the provider's call
// id, why it finished, and the tokens it took.
type ReplyMeta struct {
	CallId       string `json:"call_id"`
	FinishReason string `json:"finish_reason,omitempty"`
	InputTokens  uint32 `json:"input_tokens,omitempty"`
	OutputTokens uint32 `json:"output_tokens,omitempty"`
}

// CompactIntent is a summariser's intent (ledger): the cut, the previous
// cut and how many entries the summary spans.
type CompactIntent struct {
	Cut  uint32 `json:"cut"`
	From uint32 `json:"from"`
	Span uint32 `json:"span"`
}

// Approval is an approval entry's record: its decision and approver are
// the ledger's, its justification and the payload's content digest the
// vault's.
type Approval struct {
	Channel       string    `json:"channel"`
	Edge          string    `json:"edge,omitempty"`
	Tier          string    `json:"tier"`
	Path          string    `json:"path"`
	Approver      string    `json:"approver"`
	Justification string    `json:"justification,omitempty"`
	At            time.Time `json:"at"`
	Digest        string    `json:"digest,omitempty"`
}

// HeadRecord is a head entry's record (ledger; ADR-0314): the ordinal and
// digest the receipt covers, the receipt, its cadence and whether it is
// durable.
type HeadRecord struct {
	Ordinal   uint32    `json:"ordinal"`
	Digest    string    `json:"digest"`
	ReceiptId string    `json:"receipt_id"`
	ReceiptAt time.Time `json:"receipt_at"`
	Cadence   string    `json:"cadence"`
	// Durable is the receipt's: false for a head the process alone holds,
	// which anchors nothing (ADR-0314, Migration).
	Durable bool `json:"durable"`
}

// ProfileRecord is a profile entry's record (ledger): the fingerprint of
// the harness profile the run is pinned to.
type ProfileRecord struct {
	Fingerprint string `json:"fingerprint"`
}

// TurnState is a turn entry's record (ledger): how the turn ended, and why
// when it did not end answered.
type TurnState struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

// ToolCatalog is a tools entry's record (ledger): the names of the tools
// offered, in order.
type ToolCatalog struct {
	Names []string `json:"names"`
}

// Endorsement is an endorse entry's record (vault: it names content by
// digest): the digest of what was shown, the rendering it was shown
// under, the act classes it covers, and the previous endorsement's digest
// and what changed since.
type Endorsement struct {
	Digest   string   `json:"digest"`
	Epoch    string   `json:"epoch"`
	Classes  []string `json:"classes"`
	Previous string   `json:"previous"`
	Diff     string   `json:"diff,omitempty"`
}

// GrantRecord is a grant answer's record (vault): the task and handle, the
// terms when there are any, and whether it runs unattended.
type GrantRecord struct {
	Task         string    `json:"task"`
	Handle       string    `json:"handle"`
	HasTerms     bool      `json:"has_terms,omitempty"`
	CallsBudget  uint32    `json:"calls_budget,omitempty"`
	CallsUsed    uint32    `json:"calls_used,omitempty"`
	Deadline     time.Time `json:"deadline"`
	Destinations []string  `json:"destinations,omitempty"`
	Unattended   bool      `json:"unattended,omitempty"`
}

// ExitQuestion is an exit's asked record (vault): the question put to the
// person and the exit it concerns.
type ExitQuestion struct {
	Question string `json:"question"`
	Sink     string `json:"sink"`
	Tier     string `json:"tier"`
	Path     string `json:"path"`
}

// ExitAnswer is an exit's answered record (vault): the exit, whether the
// person approved it, and why.
type ExitAnswer struct {
	Sink          string `json:"sink"`
	Tier          string `json:"tier"`
	Approved      bool   `json:"approved"`
	Justification string `json:"justification,omitempty"`
}

// CopyRecord is a copy entry's record (vault: it names content by digest;
// ADR-0317): what was copied, at which label, how, and whether it reached
// the clipboard.
type CopyRecord struct {
	Digest    string `json:"digest"`
	Level     uint8  `json:"level"`
	Tier      uint8  `json:"tier"`
	Source    string `json:"source,omitempty"`
	Handle    string `json:"handle,omitempty"`
	Plaintext bool   `json:"plaintext"`
	Recorded  bool   `json:"recorded,omitempty"`
	Approval  string `json:"approval,omitempty"`
	Raw       bool   `json:"raw,omitempty"`
	Refused   bool   `json:"refused,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Bytes     uint32 `json:"bytes"`
}

// Verdict is a verdict entry's record (ledger; ADR-0340 §SD4):
// the assertion's form and outcome, the keys of the calls it rests on, the
// result it was checked against, the digests of the assertion's statement
// and of the statement that produced the result, the endpoint the check
// ran on as the grant names it, and the reason when the check did not
// hold or did not run. The assertion itself is the entry's arguments and
// the observed value its body: content, held by the vault.
type Verdict struct {
	Form     string   `json:"form"`
	Outcome  string   `json:"outcome"`
	RestsOn  []string `json:"rests_on,omitempty"`
	Result   uint64   `json:"result,omitempty"`
	Digest   string   `json:"statement_digest,omitempty"`
	Source   string   `json:"source_digest,omitempty"`
	Endpoint string   `json:"endpoint,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

// Attachment is one image a message carries: its media type and the
// digest the artefact store keeps the bytes under, so the journal holds
// no pixels (ledger: a reference, not content).
type Attachment struct {
	MediaType string `json:"media_type"`
	Digest    string `json:"digest"`
}

// RecordBody is the entry's body as text: the prose, or the record it
// carries rendered as the JSON its body used to be written as, for the
// readers that read text — the introspection tables and the SQL over them.
// The record is the entry's; the rendering is a view.
func (inst Entry) RecordBody() string {
	var v any
	switch {
	case inst.Request != nil:
		v = inst.Request
	case inst.Compact != nil && inst.Kind == KindIntent:
		v = inst.Compact
	case inst.Approval != nil:
		v = inst.Approval
	case inst.Head != nil:
		v = inst.Head
	case inst.Profile != nil:
		return inst.Profile.Fingerprint
	case inst.TurnEnd != nil:
		return inst.TurnEnd.Text()
	case inst.Tools != nil:
		v = inst.Tools.Names
	case inst.Endorse != nil:
		v = inst.Endorse
	case inst.Grant != nil:
		v = inst.Grant
	case inst.Question != nil:
		v = inst.Question
	case inst.Answer != nil:
		v = inst.Answer
	case inst.Copy != nil:
		v = inst.Copy
	case inst.Verdict != nil:
		v = inst.Verdict
	case inst.Kind == KindIntent && inst.Arguments != "":
		return inst.Arguments
	case inst.Kind == KindOutcome && inst.Reply != nil && inst.Body == "":
		v = inst.Reply
	default:
		return inst.Body
	}
	b, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		return ""
	}
	return string(b)
}

// Text is the turn's end as one line: the state, then the reason.
func (inst TurnState) Text() string {
	if inst.Reason == "" {
		return inst.State
	}
	return inst.State + ": " + inst.Reason
}
