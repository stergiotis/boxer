package llm

import (
	"encoding/hex"
	"encoding/json/v2"
	"strconv"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// turn is what a request adds to its call record beside the counts: the
// conversation it belongs to, the messages it sent, the range of the
// logical conversation it declared it left out and, when the provider
// answered, the reply as the message the app will echo back. plan fills in
// which of the messages are new since the parent; the trail gets a row for
// each of those (ADR-0277 §SD3), with its text when body is set (ADR-0264).
type turn struct {
	conversation string
	parent       string
	messages     []openaichat.Message
	omitFrom     int
	omitTo       int
	reply        option.Option[openaichat.Message]
	reasoning    string

	// body says the rows carry the text: a retained request under a durable
	// ceiling.
	body bool
	// planned says plan ran. covered is how many of the request's messages
	// the parent already holds and from the ordinal the new ones start at;
	// parentHashes is the parent's logical conversation, reqHashes one hash
	// per message of the request.
	planned      bool
	covered      int
	from         int
	parentHashes []string
	reqHashes    []string
	// requested says the request's rows were buffered; failed is the first
	// error buffering any row. notAhead says they were not on the trail
	// when the request left (ADR-0277 §SD3).
	requested bool
	failed    error
	notAhead  bool
}

// logical is the logical conversation after the turn, one hash per message:
// what its successor is checked against.
func (inst *turn) logical() (hs []string) {
	hs = append(append(make([]string, 0, len(inst.parentHashes)+len(inst.reqHashes)-inst.covered+1), inst.parentHashes...), inst.reqHashes[inst.covered:]...)
	if inst.reply.Has {
		hs = append(hs, messageHashes([]openaichat.Message{inst.reply.Val})...)
	}
	return
}

// seen is what the service remembers of a call whose rows it wrote, so the
// next call writes only what is new (ADR-0264 §SD3): one hash per message
// of the logical conversation after that call.
type seen struct {
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

// toolsDigest is the digest of the tool definitions a request offered, ""
// when it offered none.
func toolsDigest(tools []openaichat.Tool) (d string) {
	if len(tools) == 0 {
		return ""
	}
	b, err := json.Marshal(tools, json.Deterministic(true))
	if err != nil {
		return ""
	}
	return trail.ContentDigest(string(b))
}

// continuation says how a request continues its parent. The request must
// be the parent's logical conversation with [omitFrom, omitTo) left out,
// followed by what is new; then covered is how many of the request's
// messages the parent already holds and ok is true, and the new ones take
// ordinals from len(parent's hashes) on. Anything else — no parent seen,
// another conversation, a rewritten prefix, an omission that does not
// match — is not ok, and the caller writes the whole request.
func continuation(parent option.Option[seen], conversation string, req []string, omitFrom, omitTo int) (covered int, ok bool) {
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

// plan decides which of the turn's messages are new since parent, the call
// the request named, when the service has seen it.
func (inst *turn) plan(parent option.Option[seen]) {
	inst.reqHashes = messageHashes(inst.messages)
	inst.planned = true
	covered, ok := continuation(parent, inst.conversation, inst.reqHashes, inst.omitFrom, inst.omitTo)
	if !ok {
		return
	}
	inst.covered, inst.from, inst.parentHashes = covered, len(parent.Val.hashes), parent.Val.hashes
}

// messageRow is one message as the trail takes it: the audit row, and the
// text for a kept one.
type messageRow struct {
	audit trail.LlmMessage
	body  trail.LlmMessageBody
}

// messageRows are the rows of ms, the first at ordinal first. reasoning is
// the reply's, carried by the last row when it is set.
func messageRows(rec CallRecord, ms []openaichat.Message, first int, reasoning string) (rows []messageRow) {
	for i, m := range ms {
		row := messageRow{
			audit: trail.LlmMessage{
				CallId: rec.CallId, Ordinal: uint32(first + i), Role: m.Role.String(),
				Sensitivity: sensitivityName(rec.Sensitivity),
				Bytes:       uint64(len(m.Content)), Digest: trail.ContentDigest(m.Content),
			},
			body: trail.LlmMessageBody{Content: m.Content},
		}
		if m.ToolCallId != "" {
			row.audit.ToolCallId = option.Some(m.ToolCallId)
		}
		if i == len(ms)-1 {
			row.body.Reasoning = reasoning
		}
		for _, tc := range m.ToolCalls {
			row.audit.ToolCallIds = append(row.audit.ToolCallIds, tc.Id)
			row.audit.ToolNames = append(row.audit.ToolNames, tc.Name)
			b, err := json.Marshal(struct {
				Id        string `json:"id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{tc.Id, tc.Name, tc.Arguments})
			if err == nil {
				row.body.ToolCalls = append(row.body.ToolCalls, string(b))
			}
		}
		for _, img := range m.Images {
			d := blake3.Sum256(img.Data)
			row.audit.Images = append(row.audit.Images, img.MediaType+" "+hex.EncodeToString(d[:])+" "+strconv.Itoa(len(img.Data)))
		}
		rows = append(rows, row)
	}
	return
}
