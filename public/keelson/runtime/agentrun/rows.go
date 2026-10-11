package agentrun

import (
	"encoding/json/v2"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/jsonshred"
)

// The rows an entry is stored as (ADR-0325 §SD1): the ledger row and its
// record components, and, for an entry with content, the vault row with
// its components and the shredded arguments and calls. The SQL stores and
// the canonical forms share these mappings, so a row as stored and a row
// as hashed are one row.

// hasher computes the canonical items of rows not stored (ADR-0325 §SD2).
var hasher = agentrunstore.NewHasher()

// fillLedger composes e's ledger row on b.
func fillLedger(b *agentrunstore.EntryEntityBuilder, e Entry) (err error) {
	types, digests, err := attachmentColumns(e.Attachments)
	if err != nil {
		return
	}
	b.AddEntry(agentrunstore.Entry{At: utc(e.At), Kind: e.Kind.String(), Key: e.Key, Role: e.Role,
		EntryId: e.EntryId, VaultCommitment: e.VaultCommitment,
		Name: e.Name, Book: e.Book, Form: e.Form, Target: e.Target, From: e.From, Effect: e.Effect, Turn: e.Turn,
		LabelLevel: uint8(e.Label.Level), LabelTier: uint8(e.Label.Tier), LabelExposure: unionMarks(e.Label.Exposure, nil), LabelOrigin: unionMarks(e.Label.Origin, nil),
		WriterRun: e.WriterRun, WriterTurn: e.WriterTurn, WriterOrdinal: e.WriterOrdinal, Prev: e.Prev, Digest: e.Digest, KeyId: e.KeyId, Parent: e.Parent,
		AttachmentMediaTypes: types, AttachmentDigests: digests})
	if r := e.Request; r != nil {
		b.AddRequestMeta(agentrunstore.RequestMeta{Digest: r.Digest, Messages: r.Messages, Tools: r.Tools, Round: r.Round, Label: r.Label})
	}
	if r := e.Reply; r != nil {
		b.AddReplyMeta(agentrunstore.ReplyMeta{CallId: r.CallId, FinishReason: r.FinishReason, InputTokens: r.InputTokens, OutputTokens: r.OutputTokens})
	}
	if c := e.Compact; c != nil {
		b.AddCompactIntent(agentrunstore.CompactIntent{Cut: c.Cut, From: c.From, Span: c.Span})
	}
	if a := e.Approval; a != nil {
		b.AddApprovalDecision(agentrunstore.ApprovalDecision{Channel: a.Channel, Edge: a.Edge, Tier: a.Tier, Path: a.Path, Approver: a.Approver, At: utc(a.At)})
	}
	if h := e.Head; h != nil {
		b.AddHeadRecord(agentrunstore.HeadRecord{Ordinal: h.Ordinal, Digest: h.Digest, ReceiptId: h.ReceiptId, ReceiptAt: utc(h.ReceiptAt), Cadence: h.Cadence, Durable: h.Durable})
	}
	if p := e.Profile; p != nil {
		b.AddProfileRecord(agentrunstore.ProfileRecord{Fingerprint: p.Fingerprint})
	}
	if t := e.TurnEnd; t != nil {
		b.AddTurnState(agentrunstore.TurnState{State: t.State, Reason: t.Reason})
	}
	if t := e.Tools; t != nil {
		b.AddToolCatalog(agentrunstore.ToolCatalog{Names: t.Names})
	}
	return
}

// entryOfLedger is the entry a ledger row holds, without content: the
// vault's join gives it that.
func entryOfLedger(ent *agentrunstore.EntryEntity) (e Entry) {
	row := ent.Entry.Val
	k, _ := ParseKind(row.Kind)
	e = Entry{Run: ent.ID, Ordinal: uint32(ent.Ord), At: zeroed(row.At), Kind: k, Key: row.Key, Role: row.Role,
		EntryId: row.EntryId, VaultCommitment: row.VaultCommitment,
		Name: row.Name, Book: row.Book, Form: row.Form, Target: row.Target, From: row.From, Effect: row.Effect, Turn: row.Turn,
		Label:     Label{Level: LevelE(row.LabelLevel), Tier: TierE(row.LabelTier), Exposure: unionMarks(row.LabelExposure, nil), Origin: unionMarks(row.LabelOrigin, nil)},
		WriterRun: row.WriterRun, WriterTurn: row.WriterTurn, WriterOrdinal: row.WriterOrdinal, Prev: row.Prev, Digest: row.Digest, KeyId: row.KeyId, Parent: row.Parent,
		Attachments: attachmentsText(row.AttachmentMediaTypes, row.AttachmentDigests)}
	if ent.RequestMeta.Has {
		c := ent.RequestMeta.Val
		e.Request = &RequestMeta{Digest: c.Digest, Messages: c.Messages, Tools: c.Tools, Round: c.Round, Label: c.Label}
	}
	if ent.ReplyMeta.Has {
		c := ent.ReplyMeta.Val
		e.Reply = &ReplyMeta{CallId: c.CallId, FinishReason: c.FinishReason, InputTokens: c.InputTokens, OutputTokens: c.OutputTokens}
	}
	if ent.CompactIntent.Has {
		c := ent.CompactIntent.Val
		e.Compact = &CompactIntent{Cut: c.Cut, From: c.From, Span: c.Span}
	}
	if ent.ApprovalDecision.Has {
		c := ent.ApprovalDecision.Val
		e.Approval = &Approval{Channel: c.Channel, Edge: c.Edge, Tier: c.Tier, Path: c.Path, Approver: c.Approver, At: zeroed(c.At)}
	}
	if ent.HeadRecord.Has {
		c := ent.HeadRecord.Val
		e.Head = &HeadRecord{Ordinal: c.Ordinal, Digest: c.Digest, ReceiptId: c.ReceiptId, ReceiptAt: zeroed(c.ReceiptAt), Cadence: c.Cadence, Durable: c.Durable}
	}
	if ent.ProfileRecord.Has {
		c := ent.ProfileRecord.Val
		e.Profile = &ProfileRecord{Fingerprint: c.Fingerprint}
	}
	if ent.TurnState.Has {
		c := ent.TurnState.Val
		e.TurnEnd = &TurnState{State: c.State, Reason: c.Reason}
	}
	if ent.ToolCatalog.Has {
		c := ent.ToolCatalog.Val
		e.Tools = &ToolCatalog{Names: c.Names}
	} else if k == KindTools {
		// A catalog of no tools writes no attribute.
		e.Tools = &ToolCatalog{}
	}
	return
}

// fillVault composes b's vault row from body, the opening left out when
// withOpening is false: what the commitment covers.
func fillVault(b *agentrunstore.BodyRowEntityBuilder, body Body, withOpening bool) (err error) {
	row := agentrunstore.BodyRow{Body: body.Body}
	if withOpening {
		row.Opening = OpeningText(body.Opening)
	}
	var shredded []jsonshred.Attr
	if body.Arguments != "" {
		attrs, xerr := shred(fieldArguments, body.Arguments)
		if xerr != nil {
			row.ArgumentsText = body.Arguments
		} else {
			shredded = append(shredded, attrs...)
		}
	}
	if body.Calls != "" {
		attrs, xerr := shred(fieldCalls, body.Calls)
		if xerr != nil {
			return eb.Build().Str("run", body.Run).Uint32("ordinal", body.Ordinal).Errorf("the calls do not parse: %w", xerr)
		}
		shredded = append(shredded, attrs...)
	}
	b.AddBodyRow(row)
	if c := body.Endorse; c != nil {
		b.AddEndorsement(agentrunstore.Endorsement{Digest: c.Digest, Epoch: c.Epoch, Classes: c.Classes, Previous: c.Previous, Diff: c.Diff})
	}
	if c := body.Grant; c != nil {
		b.AddGrantRecord(agentrunstore.GrantRecord{Task: c.Task, Handle: c.Handle, HasTerms: c.HasTerms, CallsBudget: c.CallsBudget, CallsUsed: c.CallsUsed,
			Deadline: utc(c.Deadline), Destinations: c.Destinations, Unattended: c.Unattended})
	}
	if c := body.Question; c != nil {
		b.AddExitQuestion(agentrunstore.ExitQuestion{Question: c.Question, Sink: c.Sink, Tier: c.Tier, Path: c.Path})
	}
	if c := body.Answer; c != nil {
		b.AddExitAnswer(agentrunstore.ExitAnswer{Sink: c.Sink, Tier: c.Tier, Approved: c.Approved, Justification: c.Justification})
	}
	if c := body.Approval; c != nil {
		b.AddApprovalContent(agentrunstore.ApprovalContent{Justification: c.Justification, Digest: c.Digest})
	}
	if c := body.Copy; c != nil {
		b.AddCopyRecord(agentrunstore.CopyRecord{Digest: c.Digest, Level: c.Level, Tier: c.Tier, Source: c.Source, Handle: c.Handle, Plaintext: c.Plaintext,
			Recorded: c.Recorded, Approval: c.Approval, Raw: c.Raw, Refused: c.Refused, Reason: c.Reason, Bytes: c.Bytes})
	}
	return agentrunstore.ContributeShredded(b, shredded)
}

// bodyOfVault is the content a vault row holds.
func bodyOfVault(row agentrunstore.VaultRow) (body Body, err error) {
	ent := row.Ent
	body = Body{Run: ent.ID, Ordinal: uint32(ent.Ord)}
	if ent.BodyRow.Has {
		c := ent.BodyRow.Val
		body.Opening, body.Body, body.Arguments = OpeningOf(c.Opening), c.Body, c.ArgumentsText
	}
	if ent.Endorsement.Has {
		c := ent.Endorsement.Val
		body.Endorse = &Endorsement{Digest: c.Digest, Epoch: c.Epoch, Classes: c.Classes, Previous: c.Previous, Diff: c.Diff}
	}
	if ent.GrantRecord.Has {
		c := ent.GrantRecord.Val
		body.Grant = &GrantRecord{Task: c.Task, Handle: c.Handle, HasTerms: c.HasTerms, CallsBudget: c.CallsBudget, CallsUsed: c.CallsUsed,
			Deadline: zeroed(c.Deadline), Destinations: c.Destinations, Unattended: c.Unattended}
	}
	if ent.ExitQuestion.Has {
		c := ent.ExitQuestion.Val
		body.Question = &ExitQuestion{Question: c.Question, Sink: c.Sink, Tier: c.Tier, Path: c.Path}
	}
	if ent.ExitAnswer.Has {
		c := ent.ExitAnswer.Val
		body.Answer = &ExitAnswer{Sink: c.Sink, Tier: c.Tier, Approved: c.Approved, Justification: c.Justification}
	}
	if ent.ApprovalContent.Has {
		c := ent.ApprovalContent.Val
		body.Approval = &ApprovalPart{Justification: c.Justification, Digest: c.Digest}
	}
	if ent.CopyRecord.Has {
		c := ent.CopyRecord.Val
		body.Copy = &CopyRecord{Digest: c.Digest, Level: c.Level, Tier: c.Tier, Source: c.Source, Handle: c.Handle, Plaintext: c.Plaintext,
			Recorded: c.Recorded, Approval: c.Approval, Raw: c.Raw, Refused: c.Refused, Reason: c.Reason, Bytes: c.Bytes}
	}
	var args, calls []jsonshred.Attr
	for _, a := range row.Shredded {
		switch {
		case strings.HasPrefix(a.Path, "/"+fieldArguments):
			args = append(args, a)
		case strings.HasPrefix(a.Path, "/"+fieldCalls):
			calls = append(calls, a)
		}
	}
	if len(args) > 0 {
		if body.Arguments, err = unshred(fieldArguments, args); err != nil {
			return
		}
	}
	if len(calls) > 0 {
		body.Calls, err = unshred(fieldCalls, calls)
	}
	return
}

// The top-level names the shredded fields sit under in the vault row, so
// one row's arguments and calls never share a path.
const (
	fieldArguments = "arguments"
	fieldCalls     = "calls"
)

// shred explodes a JSON document under the field's name.
func shred(field string, doc string) (attrs []jsonshred.Attr, err error) {
	return jsonshred.Explode([]byte(`{"`+field+`":`+doc+`}`), jsonshred.Options{})
}

// unshred composes a field's attributes back into its canonical document.
func unshred(field string, attrs []jsonshred.Attr) (doc string, err error) {
	b, err := jsonshred.Compose(attrs)
	if err != nil {
		return "", eb.Build().Str("field", field).Errorf("compose the shredded field: %w", err)
	}
	s := string(b)
	prefix := `{"` + field + `":`
	if !strings.HasPrefix(s, prefix) || !strings.HasSuffix(s, "}") {
		return "", eb.Build().Str("field", field).Errorf("the shredded field composes to another document")
	}
	return s[len(prefix) : len(s)-1], nil
}

// canonicalJSON is doc in jsonshred's canonical form, and ok when it
// parses.
func canonicalJSON(doc string) (canon string, ok bool) {
	b, err := jsonshred.Canonical([]byte(doc))
	if err != nil {
		return doc, false
	}
	return string(b), true
}

// attachmentColumns splits an attachments list into its parallel columns.
func attachmentColumns(text string) (types []string, digests []string, err error) {
	if text == "" {
		return
	}
	var atts []Attachment
	if err = json.Unmarshal([]byte(text), &atts); err != nil {
		return nil, nil, eh.Errorf("the attachments do not parse: %w", err)
	}
	for _, a := range atts {
		types = append(types, a.MediaType)
		digests = append(digests, a.Digest)
	}
	return
}

// attachmentsText is the attachments list its parallel columns hold, in the
// canonical form the writer stores (empty for none).
func attachmentsText(types []string, digests []string) (text string) {
	if len(types) == 0 {
		return ""
	}
	atts := make([]Attachment, len(types))
	for i := range types {
		atts[i] = Attachment{MediaType: types[i]}
		if i < len(digests) {
			atts[i].Digest = digests[i]
		}
	}
	b, _ := json.Marshal(atts)
	text, _ = canonicalJSON(string(b))
	return
}

// canonicalAttachments is an attachments list in the form attachmentsText
// reads back.
func canonicalAttachments(text string) (canon string, err error) {
	types, digests, err := attachmentColumns(text)
	if err != nil {
		return
	}
	return attachmentsText(types, digests), nil
}
