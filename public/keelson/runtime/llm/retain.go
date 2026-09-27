package llm

import (
	"encoding/hex"
	"encoding/json/v2"
	"strconv"

	"github.com/zeebo/xxh3"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/llmfacts"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// messageKindLabel is the llmMessage row's kind label.
const messageKindLabel = "llmMessage"

// turn is what a retained request adds to its call record (ADR-0264): the
// conversation it belongs to, the messages it sent, the range of the
// logical conversation it declared it left out and, when the provider
// answered, the reply as the message the app will echo back.
type turn struct {
	conversation string
	parent       string
	messages     []openaichat.Message
	omitFrom     int
	omitTo       int
	reply        option.Option[openaichat.Message]
	reasoning    string
}

// sent is what the model saw on this turn: the request's messages and the
// reply, when there is one.
func (inst turn) sent() (ms []openaichat.Message) {
	ms = inst.messages
	if inst.reply.Has {
		ms = append(append([]openaichat.Message(nil), ms...), inst.reply.Val)
	}
	return
}

// kept is what the service remembers of a call whose messages it kept, so
// the next turn stores only what is new (§SD3): one hash per message of
// the logical conversation after that call.
type kept struct {
	conversation string
	hashes       []string
}

// messageHashes is one hash per message. Reasoning is not in it, since an
// app does not echo it back.
func messageHashes(ms []openaichat.Message) (hs []string) {
	hs = make([]string, 0, len(ms))
	for _, m := range ms {
		h := blake3.New(32, nil)
		writeField(h, m.Role.String())
		writeField(h, m.Content)
		writeField(h, m.ToolCallId)
		for _, tc := range m.ToolCalls {
			writeField(h, tc.Id)
			writeField(h, tc.Name)
			writeField(h, tc.Arguments)
		}
		for _, img := range m.Images {
			writeField(h, img.MediaType)
			d := blake3.Sum256(img.Data)
			_, _ = h.Write(d[:])
		}
		hs = append(hs, hex.EncodeToString(h.Sum(nil)))
	}
	return
}

// historyHash is the hash over a whole logical conversation, the value the
// call row carries.
func historyHash(hs []string) (h string) {
	d := blake3.New(32, nil)
	for _, x := range hs {
		writeField(d, x)
	}
	return hex.EncodeToString(d.Sum(nil))
}

// writeField writes s length-prefixed, so field boundaries cannot shift.
func writeField(h interface{ Write([]byte) (int, error) }, s string) {
	_, _ = h.Write([]byte(strconv.Itoa(len(s)) + ":"))
	_, _ = h.Write([]byte(s))
}

// continuation says how a request continues its parent. The request must
// be the parent's logical conversation with [omitFrom, omitTo) left out,
// followed by what is new; then covered is how many of the request's
// messages the parent already holds and ok is true, and the new ones take
// ordinals from len(parent's hashes) on. Anything else — no parent kept,
// another conversation, a rewritten prefix, an omission that does not
// match — is not ok, and the caller keeps the whole request.
func continuation(parent option.Option[kept], conversation string, req []string, omitFrom, omitTo int) (covered int, ok bool) {
	if !parent.Has || parent.Val.conversation != conversation {
		return 0, false
	}
	ph := parent.Val.hashes
	n := len(ph)
	if omitTo == 0 {
		omitFrom = 0
	}
	if omitFrom < 0 || omitFrom > omitTo || omitTo > n {
		return 0, false
	}
	covered = omitFrom + (n - omitTo)
	if len(req) < covered {
		return 0, false
	}
	for i := 0; i < omitFrom; i++ {
		if req[i] != ph[i] {
			return 0, false
		}
	}
	for i := omitTo; i < n; i++ {
		if req[omitFrom+i-omitTo] != ph[i] {
			return 0, false
		}
	}
	return covered, true
}

// messageRows are the llmMessage rows of ms, the first at ordinal first;
// the reply, when last is it, carries the reasoning.
func messageRows(rec CallRecord, t turn, ms []openaichat.Message, first int, lastIsReply bool) (rows []llmfacts.LlmMessage) {
	for i, m := range ms {
		ord := first + i
		key := rec.CallId + "/" + strconv.Itoa(ord)
		row := llmfacts.LlmMessage{
			Id: xxh3.HashString(key), NaturalKey: []byte(key), Ts: rec.At.UTC(),
			Kind: messageKindLabel, CallId: rec.CallId, Conversation: t.conversation, App: string(rec.Sender),
			Sensitivity: sensitivityName(rec.Sensitivity), Ordinal: uint32(ord), Role: m.Role.String(),
			Content: m.Content, ToolCallId: m.ToolCallId,
		}
		if lastIsReply && i == len(ms)-1 {
			row.Reasoning = t.reasoning
		}
		for _, tc := range m.ToolCalls {
			b, err := json.Marshal(struct {
				Id        string `json:"id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{tc.Id, tc.Name, tc.Arguments})
			if err == nil {
				row.ToolCalls = append(row.ToolCalls, string(b))
			}
		}
		for _, img := range m.Images {
			d := blake3.Sum256(img.Data)
			row.Images = append(row.Images, img.MediaType+" "+hex.EncodeToString(d[:])+" "+strconv.Itoa(len(img.Data)))
		}
		rows = append(rows, row)
	}
	return
}

// keep decides what a retained turn stores: the rows, where they start in
// the logical conversation, and the logical conversation after the turn,
// which is what its successor is checked against.
func keep(rec CallRecord, t turn, parent option.Option[kept]) (rows []llmfacts.LlmMessage, from int, logical []string) {
	sent := t.sent()
	sh := messageHashes(sent)
	covered, ok := continuation(parent, t.conversation, sh[:len(t.messages)], t.omitFrom, t.omitTo)
	if !ok {
		return messageRows(rec, t, sent, 0, t.reply.Has), 0, sh
	}
	from = len(parent.Val.hashes)
	logical = append(append(make([]string, 0, from+len(sh)-covered), parent.Val.hashes...), sh[covered:]...)
	rows = messageRows(rec, t, sent[covered:], from, t.reply.Has)
	return
}
