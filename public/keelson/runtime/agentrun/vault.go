package agentrun

import (
	"context"

	"github.com/rs/zerolog"
)

// The vault (ADR-0320 §SD1, ADR-0327 §SD4): the content of an entry, one
// vault row per component, kept in a table of its own beside the ledger
// with the openings the ledger's commitments open under. The host's SQL
// store holds it through the profile's vaulted store; the in-memory store
// keeps the same separation in two maps. Reads join the two and open every
// commitment (§SD5).

// Body is an entry's content as the in-memory vault holds it: every
// content component and the openings their commitments open under, by
// component.
type Body struct {
	Run       uint64
	Ordinal   uint32
	Openings  map[string][]byte
	Body      string
	Calls     string
	Arguments string
	// The vault's records (ADR-0325 §SD1), and an approval's content part.
	Endorse  *Endorsement
	Grant    *GrantRecord
	Question *ExitQuestion
	Answer   *ExitAnswer
	Copy     *CopyRecord
	Approval *ApprovalPart
}

// ApprovalPart is the part of an approval the vault holds: the
// justification given and the payload's content digest.
type ApprovalPart struct {
	Justification string
	Digest        string
}

// bodyOf is the vault row an entry's content makes; empty for an entry
// without content.
func bodyOf(e Entry) (b Body, ok bool) {
	if !e.HasContent() {
		return
	}
	b = Body{Run: e.Run, Ordinal: e.Ordinal, Openings: e.Openings, Body: e.Body, Calls: e.Calls, Arguments: e.Arguments,
		Endorse: e.Endorse, Grant: e.Grant, Question: e.Question, Answer: e.Answer, Copy: e.Copy}
	if e.Approval != nil {
		b.Approval = &ApprovalPart{Justification: e.Approval.Justification, Digest: e.Approval.Digest}
	}
	return b, true
}

// withBody is the entry joined with its vault row.
func withBody(e Entry, b Body) Entry {
	e.Openings, e.Body, e.Calls, e.Arguments = b.Openings, b.Body, b.Calls, b.Arguments
	e.Endorse, e.Grant, e.Question, e.Answer, e.Copy = b.Endorse, b.Grant, b.Question, b.Answer, b.Copy
	if e.Approval != nil && b.Approval != nil {
		a := *e.Approval
		a.Justification, a.Digest = b.Approval.Justification, b.Approval.Digest
		e.Approval = &a
	}
	return e
}

// EraserI is a store that erases the content of named entries of a run
// after their erased entries are in the chain (ADR-0320 §SD4).
type EraserI interface {
	Erase(ctx context.Context, run uint64, ordinals []uint32) (err error)
}

// joined gives each entry the content of its vault row when the row opens
// the entry's commitments; a row that does not is ignored and logged, and
// the entry reads as erased (ADR-0320 §SD5).
func joined(log zerolog.Logger, entries []Entry, bodies []Body) (out []Entry) {
	type key struct {
		run uint64
		ord uint32
	}
	// An ordinal can hold more than one row — an append retried after its
	// ledger write failed, or a batch in flight when another process took
	// the run over (ADR-0319 §SD7) — so every row is tried and the one
	// that opens is kept.
	byKey := make(map[key][]Body, len(bodies))
	for _, b := range bodies {
		k := key{b.Run, b.Ordinal}
		byKey[k] = append(byKey[k], b)
	}
	out = make([]Entry, 0, len(entries))
	for _, e := range entries {
		if rows, ok := byKey[key{e.Run, e.Ordinal}]; ok {
			opened := false
			for _, b := range rows {
				with := withBody(e, b)
				if with.BodiesMatch() {
					e, opened = with, true
					break
				}
			}
			if !opened {
				log.Warn().Uint64("run", e.Run).Uint32("ordinal", e.Ordinal).Msg("a vault row does not open its entry's commitments; the entry reads as erased")
			}
		}
		out = append(out, e)
	}
	return
}
