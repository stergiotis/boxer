package projection

import (
	"encoding/hex"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonform"
)

// PartE orders the three parts of a request: the head (the prompt
// sections as one system message), the summary of the latest compaction,
// and the message entries from the cut on.
type PartE uint8

const (
	PartHead PartE = iota
	PartSummary
	PartBody
)

// The roles a message projects as, as the chat-completions wire names
// them.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message is one row of the projection (ADR-0309 §SD3).
type Message struct {
	Part PartE
	// Ordinal is the entry the message came from; 0 for the head.
	Ordinal uint32
	Role    string
	Content string
	// Calls are an assistant message's tool calls as JSON; ToolCallId the
	// provider's id of the call a tool message answers and Key the
	// dispatcher's key for it (ADR-0277 §SD6); Attachments the images a
	// user message carries, as JSON.
	Calls       string
	ToolCallId  string
	Key         string
	Attachments string
	// Sources are the ordinals the message was built from, its provenance;
	// Label their join.
	Sources []uint32
	Label   agentrun.Label
}

// Batch is one projected request: the messages in order, the tool names
// the latest tools entry declares, and the head's section names in the
// order they were joined.
type Batch struct {
	Messages []Message
	Tools    []string
	Sections []string
}

// Label is the join over every message's label: the request's label, what
// a sink checks (ADR-0312 §SD2).
func (inst Batch) Label() (l agentrun.Label) {
	for _, m := range inst.Messages {
		l = l.Join(m.Label)
	}
	return
}

// canonicalMessage is a message's canonical form: its content fields in a
// fixed order, deterministic CBOR; provenance and label are not content.
type canonicalMessage struct {
	Role        string `json:"role"`
	Content     string `json:"content"`
	Calls       string `json:"calls"`
	ToolCallId  string `json:"tool_call_id"`
	Attachments string `json:"attachments"`
}

var digester = canonform.NewBlake3Digester()

// Digest is the message's canonical digest.
func (inst Message) Digest() (d string, err error) {
	b, err := buscodec.NewCBOR().Encode(canonicalMessage{Role: inst.Role, Content: inst.Content, Calls: inst.Calls, ToolCallId: inst.ToolCallId, Attachments: inst.Attachments})
	if err != nil {
		err = eh.Errorf("message form: %w", err)
		return
	}
	h := digester.NewLeaf()
	_, _ = h.Write(b)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Digest is the request digest (ADR-0309 §SD3): over the ordered message
// digests, the tool names and the profile fingerprint. Message order is
// content, so the item is an array.
func (inst Batch) Digest(profileFingerprint string) (d string, err error) {
	items := make([]string, 0, len(inst.Messages)+2)
	for _, m := range inst.Messages {
		md, derr := m.Digest()
		if derr != nil {
			return "", derr
		}
		items = append(items, md)
	}
	items = append(items, "tools:"+strings.Join(inst.Tools, ","), "profile:"+profileFingerprint)
	b, err := buscodec.NewCBOR().Encode(items)
	if err != nil {
		err = eh.Errorf("request form: %w", err)
		return
	}
	h := digester.NewRecord()
	_, _ = h.Write(b)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Equal says two batches are the same request: the same messages by
// content, the same tools.
func (inst Batch) Equal(other Batch) bool {
	if len(inst.Messages) != len(other.Messages) || !slices.Equal(inst.Tools, other.Tools) {
		return false
	}
	for i := range inst.Messages {
		a, b := inst.Messages[i], other.Messages[i]
		if a.Part != b.Part || a.Ordinal != b.Ordinal || a.Role != b.Role || a.Content != b.Content || a.Calls != b.Calls ||
			a.ToolCallId != b.ToolCallId || a.Key != b.Key || a.Attachments != b.Attachments || !slices.Equal(a.Sources, b.Sources) || !a.Label.Equal(b.Label) {
			return false
		}
	}
	return true
}

// IsPrefixOf says inst's messages are a prefix of other's, by content:
// the relation law L5 asks of a non-rewriting append.
func (inst Batch) IsPrefixOf(other Batch) bool {
	if len(inst.Messages) > len(other.Messages) {
		return false
	}
	head := Batch{Messages: other.Messages[:len(inst.Messages)], Tools: inst.Tools}
	return inst.Equal(head)
}

// Validate is the shape pass at the end of the pipeline (ADR-0309 §SD2,
// law L2): every tool message answers, once, a call an earlier assistant
// message of the same request made, and the request ends with no
// unanswered call.
func Validate(b Batch) (err error) {
	// asked is the calls made and not yet answered; answered the ones
	// answered.
	asked := make(map[string]bool)
	answered := make(map[string]bool)
	for i, m := range b.Messages {
		switch m.Role {
		case RoleAssistant:
			for _, id := range callIds(m.Calls) {
				if asked[id] || answered[id] {
					return eb.Build().Int("message", i).Str("toolCallId", id).Errorf("an assistant message repeats a call id")
				}
				asked[id] = true
			}
		case RoleTool:
			if answered[m.ToolCallId] {
				return eb.Build().Int("message", i).Str("toolCallId", m.ToolCallId).Errorf("a tool message answers a call twice")
			}
			if !asked[m.ToolCallId] {
				return eb.Build().Int("message", i).Str("toolCallId", m.ToolCallId).Errorf("a tool message answers a call no earlier message made")
			}
			delete(asked, m.ToolCallId)
			answered[m.ToolCallId] = true
		}
	}
	if len(asked) > 0 {
		open := make([]string, 0, len(asked))
		for id := range asked {
			open = append(open, id)
		}
		slices.Sort(open)
		return eb.Build().Strs("toolCallIds", open).Errorf("the request ends with a call no tool message answers")
	}
	return
}
