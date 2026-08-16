package sqlcomplete

// Off-caret validation (ADR-0190 §SD9): every string literal in the statement
// that sits at an argument position with a closed in-process domain is checked
// against that domain, so a wrong component kind or a field the kind does not
// have is visible while typing rather than when the run refuses it.
//
// In-process domains only. The endpoint's catalogues are exact too, but they
// arrive over a probe, and a literal marked wrong because a listing has not
// landed would be a false accusation — the same reason ADR-0174's panel writes
// `?` and never `MISSING`.
//
// The token under the caret is never reported: a name half typed is not a wrong
// one. That token's report is the match state, which says "resolves" and never
// "wrong".

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
)

// Finding is one literal and what the engine could say about it.
type Finding struct {
	// Range is the literal's content, quotes excluded — what a tint covers.
	Range highlight.Range
	// Text is the content.
	Text string
	// Domain is the domain the position declares.
	Domain sqlvocab.Domain
	// Resolved is true when the domain has a member equal to Text.
	Resolved bool
	// Callee is the enclosing call, for a message.
	Callee string
}

// closedInProcess names the domains whose whole membership this build knows
// without asking an endpoint.
func closedInProcess(k sqlvocab.DomainKindE) bool {
	switch k {
	case sqlvocab.DomainComponentKind, sqlvocab.DomainElementOf,
		sqlvocab.DomainIntrospectionTable, sqlvocab.DomainSection,
		sqlvocab.DomainChannel, sqlvocab.DomainSupportRole,
		sqlvocab.DomainAspect, sqlvocab.DomainCanonicalType,
		sqlvocab.DomainGloss, sqlvocab.DomainGlossKey,
		sqlvocab.DomainIdentityTag:
		return true
	}
	return false
}

// Validate walks the statement's literals and reports what each resolves to.
//
// caret is excluded from the walk — pass a negative offset to validate a
// statement with no caret in it at all, which is what a consumer checking a
// buffer it is not editing wants.
func (inst *Engine) Validate(stmt string, scope *Scope, caret int) (findings []Finding) {
	if stmt == "" {
		return
	}
	spans := highlight.HighlightLex(stmt)
	for i := range spans {
		s := spans[i]
		if s.Category != highlight.CatStringLit || len(s.Text) < 2 {
			continue
		}
		if caret > s.Start && caret < s.Stop {
			continue
		}
		// The site is resolved with a virtual caret just inside the literal's
		// closing quote, which is exactly the state §SD9 is about: a complete
		// literal whose whole content should resolve.
		site := highlight.SiteAt(spans, s.Stop-1)
		if site.Literal == nil || site.Literal.Start != s.Start {
			continue
		}
		req := Request{Site: site, Scope: scope, Statement: stmt, Caret: s.Stop - 1}
		d, of, callee, _, why := inst.domainFor(req)
		if why != "" || !closedInProcess(d.Kind) {
			continue
		}
		items, ready, wired, rwhy := inst.resolve(d, of)
		if rwhy != "" || !ready || !wired || len(items) == 0 {
			// Not ready, not wired, or a domain that could not be narrowed —
			// three different unknowns, and none of them is evidence that the
			// literal is wrong.
			continue
		}
		f := Finding{
			Range:  highlight.Range{Start: s.Start + 1, Stop: s.Stop - 1},
			Text:   site.Literal.Text,
			Domain: d,
			Callee: callee,
		}
		for j := range items {
			if items[j].Text == f.Text {
				f.Resolved = true
				break
			}
		}
		findings = append(findings, f)
	}
	return
}
