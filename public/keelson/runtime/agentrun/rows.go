package agentrun

import (
	"encoding/json/v2"
	"strings"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/jsonshred"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// The rows an entry is stored as (ADR-0325 §SD1, ADR-0327 §SD9): the
// ledger row with its record components, the chain and the written-by
// component, and, for an entry with content, one vault row per content
// component — the prose, the shredded arguments and calls, and the records
// the vault keeps — each with its opening. The stores and the canonical
// forms share these mappings, so a row as stored and a row as hashed are
// one row.

// The vault's component names: the kind tags of the vault store's
// components, which name a vault row and key an entry's commitments.
const (
	componentBody            = "agentrunBody"
	componentShredded        = agentrunstore.EvidenceShreddedKind
	componentEndorse         = "agentrunEndorse"
	componentGrant           = "agentrunGrant"
	componentQuestion        = "agentrunQuestion"
	componentAnswer          = "agentrunAnswer"
	componentApprovalContent = "agentrunApprovalContent"
	componentCopy            = "agentrunCopy"
)

// ledgerEntity is e's ledger row: the entry component, its records, the
// chain component with the mark its kind implies and the commitments it
// carries, and the written-by component when it has one.
func ledgerEntity(e Entry) (ent *agentrunstore.EntryEntity, err error) {
	types, digests, err := attachmentColumns(e.Attachments)
	if err != nil {
		return
	}
	ent = &agentrunstore.EntryEntity{ID: e.Run, Ord: uint64(e.Ordinal)}
	ent.Entry = option.Some(agentrunstore.Entry{At: utc(e.At), Kind: e.Kind.String(), Key: e.Key, Role: e.Role,
		EntryId: e.EntryId,
		Name:    e.Name, Book: e.Book, Form: e.Form, Target: e.Target, From: e.From, Effect: e.Effect, Turn: e.Turn,
		LabelLevel: uint8(e.Label.Level), LabelTier: uint8(e.Label.Tier), LabelExposure: unionMarks(e.Label.Exposure, nil), LabelOrigin: unionMarks(e.Label.Origin, nil),
		WriterRun: e.WriterRun, WriterTurn: e.WriterTurn, WriterOrdinal: e.WriterOrdinal, Parent: e.Parent,
		AttachmentMediaTypes: types, AttachmentDigests: digests})
	if r := e.Request; r != nil {
		ent.RequestMeta = option.Some(agentrunstore.RequestMeta{Digest: r.Digest, Messages: r.Messages, Tools: r.Tools, Round: r.Round, Label: r.Label})
	}
	if r := e.Reply; r != nil {
		ent.ReplyMeta = option.Some(agentrunstore.ReplyMeta{CallId: r.CallId, FinishReason: r.FinishReason, InputTokens: r.InputTokens, OutputTokens: r.OutputTokens})
	}
	if c := e.Compact; c != nil {
		ent.CompactIntent = option.Some(agentrunstore.CompactIntent{Cut: c.Cut, From: c.From, Span: c.Span})
	}
	if a := e.Approval; a != nil {
		ent.ApprovalDecision = option.Some(agentrunstore.ApprovalDecision{Channel: a.Channel, Edge: a.Edge, Tier: a.Tier, Path: a.Path, Approver: a.Approver, At: utc(a.At)})
	}
	if h := e.Head; h != nil {
		ent.HeadRecord = option.Some(agentrunstore.HeadRecord{Ordinal: h.Ordinal, Digest: h.Digest, ReceiptId: h.ReceiptId, ReceiptAt: utc(h.ReceiptAt), Cadence: h.Cadence, Durable: h.Durable})
	}
	if p := e.Profile; p != nil {
		ent.ProfileRecord = option.Some(agentrunstore.ProfileRecord{Fingerprint: p.Fingerprint})
	}
	if t := e.TurnEnd; t != nil {
		ent.TurnState = option.Some(agentrunstore.TurnState{State: t.State, Reason: t.Reason})
	}
	if t := e.Tools; t != nil {
		ent.ToolCatalog = option.Some(agentrunstore.ToolCatalog{Names: t.Names})
	}
	if v := e.Verdict; v != nil {
		ent.VerdictRecord = option.Some(agentrunstore.VerdictRecord{Form: v.Form, Outcome: v.Outcome, RestsOn: v.RestsOn, Result: v.Result,
			Digest: v.Digest, Source: v.Source, Endpoint: v.Endpoint, Reason: v.Reason})
	}
	ent.EvidenceChain = option.Some(chainOf(e))
	if w := e.WrittenBy; w != nil {
		ent.EvidenceWrittenBy = option.Some(writtenByRow(*w))
	}
	return
}

// chainOf is e's chain component: the chain fields, the mark its kind
// implies (ADR-0327 §SD3), and its commitments in component order.
func chainOf(e Entry) (ch agentrunstore.EvidenceChain) {
	ch = agentrunstore.EvidenceChain{Kind: agentrunstore.EvidenceChainKind, Prev: uint64(e.Prev), Digest: e.Digest, Scheme: e.KeyId,
		Mark: evidence.MarkNone.String()}
	switch e.Kind {
	case KindDeleted:
		ch.Mark = evidence.MarkDeleted.String()
	case KindErased:
		ch.Mark, ch.MarkTarget = evidence.MarkErased.String(), uint64(e.Target)
	case KindHead:
		ch.Mark = evidence.MarkKeep.String()
		if e.Head != nil {
			ch.MarkTarget, ch.HeadDigest = uint64(e.Head.Ordinal), option.Some(e.Head.Digest)
		}
	}
	for _, component := range componentOrder {
		if c, ok := e.Commitments[component]; ok {
			ch.CommitComponents = append(ch.CommitComponents, component)
			ch.Commitments = append(ch.Commitments, c)
		}
	}
	return
}

// componentOrder is the order content rows are made and commitments
// listed in: fixed, so a row as prepared and a row as checked agree.
var componentOrder = []string{componentBody, componentShredded, componentEndorse, componentGrant, componentQuestion, componentAnswer, componentApprovalContent, componentCopy}

func writtenByRow(w WrittenBy) (row agentrunstore.EvidenceWrittenBy) {
	row = agentrunstore.EvidenceWrittenBy{Kind: agentrunstore.EvidenceWrittenByKind, Instance: w.Instance, OriginBy: w.OriginBy, PrincipalBy: w.PrincipalBy}
	some := func(s string) option.Option[string] {
		if s == "" {
			return option.None[string]()
		}
		return option.Some(s)
	}
	row.Run, row.App, row.Principal, row.Purpose, row.Correlation, row.Batch = some(w.Run), some(w.App), some(w.Principal), some(w.Purpose), some(w.Correlation), some(w.Batch)
	return
}

func writtenByOf(row agentrunstore.EvidenceWrittenBy) *WrittenBy {
	return &WrittenBy{Run: row.Run.Val, App: row.App.Val, Instance: row.Instance, OriginBy: row.OriginBy,
		Principal: row.Principal.Val, PrincipalBy: row.PrincipalBy, Purpose: row.Purpose.Val, Correlation: row.Correlation.Val, Batch: row.Batch.Val}
}

// entryOfLedger is the entry a ledger row holds, without content: the
// vault's join gives it that.
func entryOfLedger(ent *agentrunstore.EntryEntity) (e Entry) {
	row := ent.Entry.Val
	k, _ := ParseKind(row.Kind)
	e = Entry{Run: ent.ID, Ordinal: uint32(ent.Ord), At: zeroed(row.At), Kind: k, Key: row.Key, Role: row.Role,
		EntryId: row.EntryId,
		Name:    row.Name, Book: row.Book, Form: row.Form, Target: row.Target, From: row.From, Effect: row.Effect, Turn: row.Turn,
		Label:     Label{Level: LevelE(row.LabelLevel), Tier: TierE(row.LabelTier), Exposure: unionMarks(row.LabelExposure, nil), Origin: unionMarks(row.LabelOrigin, nil)},
		WriterRun: row.WriterRun, WriterTurn: row.WriterTurn, WriterOrdinal: row.WriterOrdinal, Parent: row.Parent,
		Attachments: attachmentsText(row.AttachmentMediaTypes, row.AttachmentDigests)}
	if ent.EvidenceChain.Has {
		ch := ent.EvidenceChain.Val
		e.Prev, e.Digest, e.KeyId = uint32(ch.Prev), ch.Digest, ch.Scheme
		for i, component := range ch.CommitComponents {
			if i < len(ch.Commitments) {
				if e.Commitments == nil {
					e.Commitments = make(map[string]string, len(ch.CommitComponents))
				}
				e.Commitments[component] = ch.Commitments[i]
			}
		}
	}
	if ent.EvidenceWrittenBy.Has {
		e.WrittenBy = writtenByOf(ent.EvidenceWrittenBy.Val)
	}
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
	if ent.VerdictRecord.Has {
		c := ent.VerdictRecord.Val
		e.Verdict = &Verdict{Form: c.Form, Outcome: c.Outcome, RestsOn: c.RestsOn, Result: c.Result, Digest: c.Digest, Source: c.Source, Endpoint: c.Endpoint, Reason: c.Reason}
	}
	return
}

// contentRows are e's vault rows, one per content component in
// componentOrder, keyed by the entry's run and ordinal and named by the
// component, each with e's opening for it when e has one. An entry
// without content makes none.
func contentRows(e Entry) (rows []*agentrunstore.EntryVaultEntity, err error) {
	row := func(component string) (v *agentrunstore.EntryVaultEntity) {
		v = &agentrunstore.EntryVaultEntity{ID: e.Run, Ord: uint64(e.Ordinal)}
		v.Component = component
		if o, ok := e.Openings[component]; ok && len(o) > 0 {
			v.EvidenceOpening = option.Some(agentrunstore.EvidenceOpening{Kind: agentrunstore.EvidenceOpeningKind, Opening: evidence.OpeningText(o)})
		}
		rows = append(rows, v)
		return
	}
	var shredded []jsonshred.Attr
	body := agentrunstore.BodyRow{Body: e.Body}
	if e.Arguments != "" {
		attrs, xerr := shred(fieldArguments, e.Arguments)
		if xerr != nil {
			body.ArgumentsText = e.Arguments
		} else {
			shredded = append(shredded, attrs...)
		}
	}
	if e.Calls != "" {
		attrs, xerr := shred(fieldCalls, e.Calls)
		if xerr != nil {
			return nil, eb.Build().Uint64("run", e.Run).Uint32("ordinal", e.Ordinal).Errorf("the calls do not parse: %w", xerr)
		}
		shredded = append(shredded, attrs...)
	}
	if body.Body != "" || body.ArgumentsText != "" {
		row(componentBody).BodyRow = option.Some(body)
	}
	if len(shredded) > 0 {
		s, serr := agentrunstore.EvidenceShreddedOf(shredded)
		if serr != nil {
			return nil, eb.Build().Uint64("run", e.Run).Uint32("ordinal", e.Ordinal).Errorf("shred: %w", serr)
		}
		row(componentShredded).EvidenceShredded = option.Some(s)
	}
	if c := e.Endorse; c != nil {
		row(componentEndorse).Endorsement = option.Some(agentrunstore.Endorsement{Digest: c.Digest, Epoch: c.Epoch, Classes: c.Classes, Previous: c.Previous, Diff: c.Diff})
	}
	if c := e.Grant; c != nil {
		row(componentGrant).GrantRecord = option.Some(agentrunstore.GrantRecord{Task: c.Task, Handle: c.Handle, HasTerms: c.HasTerms, CallsBudget: c.CallsBudget, CallsUsed: c.CallsUsed,
			Deadline: utc(c.Deadline), Destinations: c.Destinations, Unattended: c.Unattended})
	}
	if c := e.Question; c != nil {
		row(componentQuestion).ExitQuestion = option.Some(agentrunstore.ExitQuestion{Question: c.Question, Sink: c.Sink, Tier: c.Tier, Path: c.Path})
	}
	if c := e.Answer; c != nil {
		row(componentAnswer).ExitAnswer = option.Some(agentrunstore.ExitAnswer{Sink: c.Sink, Tier: c.Tier, Approved: c.Approved, Justification: c.Justification})
	}
	if c := e.Approval; c != nil && (c.Justification != "" || c.Digest != "") {
		row(componentApprovalContent).ApprovalContent = option.Some(agentrunstore.ApprovalContent{Justification: c.Justification, Digest: c.Digest})
	}
	if c := e.Copy; c != nil {
		row(componentCopy).CopyRecord = option.Some(agentrunstore.CopyRecord{Digest: c.Digest, Level: c.Level, Tier: c.Tier, Source: c.Source, Handle: c.Handle, Plaintext: c.Plaintext,
			Recorded: c.Recorded, Approval: c.Approval, Raw: c.Raw, Refused: c.Refused, Reason: c.Reason, Bytes: c.Bytes})
	}
	return
}

// withPrepared is e with what the store's Prepare set on its ledger row
// and content rows: the ordinal, the chain fields, the commitments, the
// openings and the written-by identity.
func withPrepared(e Entry, ent *agentrunstore.EntryEntity, rows []*agentrunstore.EntryVaultEntity) Entry {
	e.Ordinal = uint32(ent.Ord)
	ch := ent.EvidenceChain.Val
	e.Prev, e.Digest, e.KeyId = uint32(ch.Prev), ch.Digest, ch.Scheme
	e.Commitments, e.Openings = nil, nil
	for i, component := range ch.CommitComponents {
		if e.Commitments == nil {
			e.Commitments = make(map[string]string, len(ch.CommitComponents))
		}
		if i < len(ch.Commitments) {
			e.Commitments[component] = ch.Commitments[i]
		}
	}
	for _, v := range rows {
		if v.EvidenceOpening.Has {
			if o := evidence.OpeningOf(v.EvidenceOpening.Val.Opening); o != nil {
				if e.Openings == nil {
					e.Openings = make(map[string][]byte, len(rows))
				}
				e.Openings[v.Component] = o
			}
		}
	}
	if ent.EvidenceWrittenBy.Has {
		e.WrittenBy = writtenByOf(ent.EvidenceWrittenBy.Val)
	}
	return e
}

// bodyOfRows is the content the vault rows of one entry hold, with their
// openings: what the join takes and opens against the ledger row's
// commitments. Of several rows for one component — a batch in flight when
// another process took the run over, retried after its ledger write
// failed — the one at the highest revision stands, as the ledger's row
// does (ADR-0319 §SD7); whether it opens is the join's to check.
func bodyOfRows(run uint64, ordinal uint32, rows []*agentrunstore.EntryVaultEntity) (b Body, err error) {
	newest := make(map[string]*agentrunstore.EntryVaultEntity, len(rows))
	for _, v := range rows {
		if have, ok := newest[v.Component]; !ok || v.Revision > have.Revision {
			newest[v.Component] = v
		}
	}
	kept := make([]*agentrunstore.EntryVaultEntity, 0, len(newest))
	for _, component := range componentOrder {
		if v, ok := newest[component]; ok {
			kept = append(kept, v)
		}
	}
	e, err := withContent(Entry{Run: run, Ordinal: ordinal}, kept)
	if err != nil {
		return
	}
	b, _ = bodyOf(e)
	b.Run, b.Ordinal, b.Openings = run, ordinal, e.Openings
	return
}

// withContent is e joined with the content its vault rows hold and their
// openings; whether the content opens e's commitments is the reader's to
// check (BodiesMatch).
func withContent(e Entry, rows []*agentrunstore.EntryVaultEntity) (out Entry, err error) {
	out = e
	out.Openings = nil
	var args, calls []jsonshred.Attr
	for _, v := range rows {
		if v.EvidenceOpening.Has {
			if o := evidence.OpeningOf(v.EvidenceOpening.Val.Opening); o != nil {
				if out.Openings == nil {
					out.Openings = make(map[string][]byte, len(rows))
				}
				out.Openings[v.Component] = o
			}
		}
		switch {
		case v.BodyRow.Has:
			c := v.BodyRow.Val
			out.Body, out.Arguments = c.Body, c.ArgumentsText
		case v.EvidenceShredded.Has:
			for _, a := range v.EvidenceShredded.Val.Attrs() {
				switch {
				case strings.HasPrefix(a.Path, "/"+fieldArguments):
					args = append(args, a)
				case strings.HasPrefix(a.Path, "/"+fieldCalls):
					calls = append(calls, a)
				}
			}
		case v.Endorsement.Has:
			c := v.Endorsement.Val
			out.Endorse = &Endorsement{Digest: c.Digest, Epoch: c.Epoch, Classes: c.Classes, Previous: c.Previous, Diff: c.Diff}
		case v.GrantRecord.Has:
			c := v.GrantRecord.Val
			out.Grant = &GrantRecord{Task: c.Task, Handle: c.Handle, HasTerms: c.HasTerms, CallsBudget: c.CallsBudget, CallsUsed: c.CallsUsed,
				Deadline: zeroed(c.Deadline), Destinations: c.Destinations, Unattended: c.Unattended}
		case v.ExitQuestion.Has:
			c := v.ExitQuestion.Val
			out.Question = &ExitQuestion{Question: c.Question, Sink: c.Sink, Tier: c.Tier, Path: c.Path}
		case v.ExitAnswer.Has:
			c := v.ExitAnswer.Val
			out.Answer = &ExitAnswer{Sink: c.Sink, Tier: c.Tier, Approved: c.Approved, Justification: c.Justification}
		case v.ApprovalContent.Has:
			c := v.ApprovalContent.Val
			a := Approval{}
			if out.Approval != nil {
				a = *out.Approval
			}
			a.Justification, a.Digest = c.Justification, c.Digest
			out.Approval = &a
		case v.CopyRecord.Has:
			c := v.CopyRecord.Val
			out.Copy = &CopyRecord{Digest: c.Digest, Level: c.Level, Tier: c.Tier, Source: c.Source, Handle: c.Handle, Plaintext: c.Plaintext,
				Recorded: c.Recorded, Approval: c.Approval, Raw: c.Raw, Refused: c.Refused, Reason: c.Reason, Bytes: c.Bytes}
		}
	}
	if len(args) > 0 {
		if out.Arguments, err = unshred(fieldArguments, args); err != nil {
			return
		}
	}
	if len(calls) > 0 {
		out.Calls, err = unshred(fieldCalls, calls)
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
