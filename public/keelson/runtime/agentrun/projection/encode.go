package projection

import (
	"encoding/json/v2"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/jsonshred"
)

// AttachmentsBody is the attachments column for images, in the canonical
// form the journal stores and reads it back in.
func AttachmentsBody(atts []agentrun.Attachment) string {
	if len(atts) == 0 {
		return ""
	}
	b, _ := json.Marshal(atts)
	return canonical(b)
}

// canonical is a JSON document in jsonshred's canonical form, the form
// the journal stores calls and attachments in (ADR-0325 §SD1).
func canonical(doc []byte) string {
	c, err := jsonshred.Canonical(doc)
	if err != nil {
		return string(doc)
	}
	return string(c)
}

// ParseAttachments reads the column.
func ParseAttachments(s string) (atts []agentrun.Attachment) {
	if s == "" {
		return
	}
	_ = json.Unmarshal([]byte(s), &atts)
	return
}

// CallsBody is the calls column for a reply's tool calls.
func CallsBody(calls []openaichat.ToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	b, _ := json.Marshal(calls, json.Deterministic(true))
	return canonical(b)
}

// ParseCalls reads the column.
func ParseCalls(s string) (calls []openaichat.ToolCall) {
	if s == "" {
		return
	}
	_ = json.Unmarshal([]byte(s), &calls)
	return
}

// Resolver finds an attachment's bytes by digest; nil leaves images out.
type Resolver func(a agentrun.Attachment) (data []byte, ok bool)

// Encode is the batch as the chat-completions wire's messages (ADR-0309
// §SD3: Go encodes, once, per route): the identity stays the batch's.
func Encode(b Batch, resolve Resolver) (msgs []openaichat.Message, err error) {
	msgs = make([]openaichat.Message, 0, len(b.Messages))
	for _, m := range b.Messages {
		var role openaichat.ChatRoleE
		switch m.Role {
		case RoleSystem:
			role = openaichat.ChatRoleSystem
		case RoleUser:
			role = openaichat.ChatRoleUser
		case RoleAssistant:
			role = openaichat.ChatRoleAssistant
		case RoleTool:
			role = openaichat.ChatRoleTool
		default:
			err = eb.Build().Str("role", m.Role).Uint32("ordinal", m.Ordinal).Errorf("no such message role")
			return
		}
		w := openaichat.Message{Role: role, Content: m.Content, ToolCallId: m.ToolCallId, ToolCalls: ParseCalls(m.Calls)}
		for _, a := range ParseAttachments(m.Attachments) {
			if resolve == nil {
				continue
			}
			if data, ok := resolve(a); ok {
				w.Images = append(w.Images, openaichat.Image{MediaType: a.MediaType, Data: data})
			}
		}
		msgs = append(msgs, w)
	}
	return
}
