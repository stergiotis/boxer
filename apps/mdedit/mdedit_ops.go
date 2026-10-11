package mdedit

import (
	"hash/fnv"
	"strconv"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// The catalog (ADR-0269 §SD2, ADR-0318): what the agentic harness reads
// and offers in place of a model grant of mdedit's own. An agent reads the
// document and the selection, and proposes a replacement the person
// reviews in the proposal pane (mdedit_proposal.go); nothing an agent does
// through it changes the buffer.

const (
	resDocument = "document"
	resProposal = "proposal"

	opGetDocument = "get_document"
	opProposeEdit = "propose_edit"
	opGetProposal = "get_proposal"
)

// snap is what queries read: a copy taken after the command stage. The
// revision is computed by the query that wants it, not per frame.
type snap struct {
	src      string
	selStart int
	selStop  int
	selOk    bool
	dirty    bool
	pending  *proposal
	outcome  string
}

// Document is get_document's result.
type Document struct {
	Text           string `desc:"the markdown source"`
	Length         int    `desc:"the source's length in bytes"`
	Revision       string `desc:"names this text; propose_edit quotes it, and a document changed since is refused"`
	Selection      string `desc:"the selected text; empty when nothing is selected"`
	SelectionStart int    `desc:"the selection's first byte; equal to SelectionStop when nothing is selected"`
	SelectionStop  int    `desc:"the byte after the selection"`
	Modified       bool   `desc:"the document has changes not yet saved"`
}

// ProposeEditArgs is propose_edit's argument.
type ProposeEditArgs struct {
	Title       string `desc:"one line naming the change, shown above it, e.g. Improve style"`
	Revision    string `desc:"the revision get_document returned"`
	Start       int    `desc:"the first byte replaced: SelectionStart for the selection, 0 for the whole document"`
	Stop        int    `desc:"the byte after the last one replaced: SelectionStop for the selection, Length for the whole document; equal to Start inserts"`
	Replacement string `desc:"the markdown that replaces the span"`
}

// Proposal is get_proposal's result.
type Proposal struct {
	Pending bool   `desc:"a proposal is waiting for the person's verdict"`
	Title   string `desc:"the waiting proposal's title"`
	Outcome string `desc:"what became of the last proposal that is no longer waiting: applied, discarded, or superseded by a later proposal; empty when none"`
}

// documentRevision names a buffer: equal texts, equal revisions.
func documentRevision(src string) (rev string) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(src))
	return strconv.FormatUint(h.Sum64(), 16)
}

var ops = func() (s *appops.Set[*App, snap]) {
	s = appops.NewSet(func(inst *App) (sn snap) {
		sn = snap{src: inst.src, dirty: inst.dirty(), pending: inst.prop.pending, outcome: inst.prop.outcome}
		sn.selStart, sn.selStop, sn.selOk = inst.selectionSpan()
		return
	})
	s.Resource(resDocument, "the markdown source", func(inst *App) any { return inst.src })
	s.Resource(resProposal, "the replacement waiting for the person's verdict", func(inst *App) any {
		if inst.prop.pending == nil {
			return ""
		}
		return inst.prop.pending.content
	})

	appops.Query(s, app.OperationSpec{Name: opGetDocument, Version: 1, Summary: "read the document, its revision and the selection",
		Reads: []string{resDocument}, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn snap, in appops.None) (d Document, err error) {
			d = Document{Text: sn.src, Length: len(sn.src), Revision: documentRevision(sn.src), Modified: sn.dirty}
			if sn.selOk {
				d.SelectionStart, d.SelectionStop = sn.selStart, sn.selStop
				d.Selection = sn.src[sn.selStart:sn.selStop]
			}
			return
		})
	appops.Command(s, app.OperationSpec{Name: opProposeEdit, Version: 1,
		Summary: "propose replacing a span of the document; the person reviews it and applies or discards it",
		Effect:  app.OperationEffectView, Reads: []string{resDocument}, Writes: []string{resProposal}, Agents: true,
		Follows: []string{"the proposal pane opens; a proposal still waiting is superseded"},
		Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance},
		func(inst *App, call app.OperationCall, in ProposeEditArgs) (out appops.None, err error) {
			err = checkProposal(inst.src, in)
			if err != nil {
				return
			}
			inst.offerProposal(proposal{title: in.Title, content: in.Replacement, writer: call.Writer, reason: call.Reason}, in.Start, in.Stop)
			return
		})
	appops.Query(s, app.OperationSpec{Name: opGetProposal, Version: 1, Summary: "read whether a proposal is waiting and what became of the last one",
		Reads: []string{resProposal}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn snap, in appops.None) (p Proposal, err error) {
			p.Outcome = sn.outcome
			if sn.pending != nil {
				p.Pending, p.Title = true, sn.pending.title
			}
			return
		})
	return
}()

// checkProposal refuses a proposal that does not fit the buffer: a stale
// revision, a span outside it, or a span that cuts a character.
func checkProposal(src string, in ProposeEditArgs) (err error) {
	switch {
	case in.Title == "":
		return app.RefuseOperation("name the change in title")
	case in.Revision != documentRevision(src):
		return app.RefuseOperation("the document changed since that revision; read it again with get_document")
	case in.Start < 0 || in.Stop < in.Start || in.Stop > len(src):
		return app.RefuseOperation("the span is outside the document: 0 <= start <= stop <= " + strconv.Itoa(len(src)))
	case !onRuneBoundary(src, in.Start) || !onRuneBoundary(src, in.Stop):
		return app.RefuseOperation("the span cuts a character; use the byte offsets get_document returned")
	}
	return
}

// onRuneBoundary reports whether off starts a character, or is the end.
func onRuneBoundary(src string, off int) (yes bool) {
	return off == len(src) || utf8.RuneStart(src[off])
}
