package play

import (
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
)

// The security ceiling a SQL-valued knob is judged against — ADR-0187
// (proposed) §SD5.
//
// # Why a ceiling and not a check
//
// In `play` an expression knob grants nothing: the editor already accepts
// arbitrary SQL, which is the argument `sanitizeTable` states in the Map panel.
// An applet is the same engine with the editor removed, and its premise
// (ADR-0132 §SD5) is that a committed, classified query is the whole surface —
// `AutoRun` is gated on the read class and the class is wire-enforced with
// `readonly`. An expression knob would turn a frozen read-class applet into an
// arbitrary-SQL surface: a spliced `url(…)` is egress, a spliced scalar
// subquery reads tables the applet never named.
//
// So the mint-time class stops being a description of the document and becomes
// a CEILING on what any substitution may produce. That is cheap because the
// classifier already exists, is already conservative ("cannot prove → stronger
// class"), and already fails closed on its zero value.
//
// # The direction of the comparison
//
// [analysis.QuerySecurityClassE] is ordered so that numerically SMALLER is
// stronger, and its zero value is the strongest ([analysis.QuerySecurityMutating]).
// A refusal is therefore `substituted < ceiling`, and a zero ceiling — what
// `play` itself carries — refuses nothing, which is exactly the "report, do not
// enforce" half of §SD5 with no flag to get wrong.

// SetSecurityCeiling declares the strongest class this instance's queries may
// reach (ADR-0187 (proposed) §SD5). An applet passes its mint-time class; play
// leaves it unset, and the zero value refuses nothing.
//
// It is a ceiling on the SUBSTITUTED body, so it constrains what a knob may
// turn the document into — not what the document already is. Raising the
// document's own class is the author's business and the mint-time
// classification already covers it.
func (inst *PlayApp) SetSecurityCeiling(class analysis.QuerySecurityClassE) {
	inst.securityCeiling = class
}

// exprCeilingRefusal is the run gate's half: it asks the client for the body
// the substitution produces and judges that against this instance's ceiling.
//
// The values come from the client rather than the pane because the client is
// where both tiers are merged (§SD3) — asking the pane would miss a value a
// panel published and never declared.
func (inst *PlayApp) exprCeilingRefusal(sql string) (reason string) {
	if inst.securityCeiling == analysis.QuerySecurityMutating || inst.client == nil {
		return ""
	}
	return exprCeilingRefusal(sql, inst.client.exprValuesFor(sql), inst.securityCeiling)
}

// exprCeilingRefusal reports why a run must be refused, or "" when it may
// proceed.
//
// Pure over its inputs so the rule is testable without a frame or a server: the
// buffer, the values that would be substituted into it, and the ceiling.
//
// The unparseable and unclassifiable cases both refuse when a ceiling is set.
// That is the conservative direction the classifier itself takes, and the
// alternative — running a body nobody could classify against an applet that
// promised a class — is the one outcome this exists to prevent.
func exprCeilingRefusal(sql string, values map[string]string, ceiling analysis.QuerySecurityClassE) (reason string) {
	if ceiling == analysis.QuerySecurityMutating {
		// No ceiling: nothing is above "mutating". This is `play`.
		return ""
	}
	substituted, spl, err := spliceExprSlots(sql, values)
	if err != nil {
		return "cannot classify the substituted query — refusing under this applet's " +
			ceiling.String() + " class"
	}
	if len(spl) == 0 {
		// Nothing was substituted, so the mint-time class still describes the
		// body and re-judging it would only re-report what the document said.
		return ""
	}
	pr, perr := nanopass.Parse(substituted)
	if perr != nil {
		return "the substituted query does not parse — refusing under this applet's " +
			ceiling.String() + " class"
	}
	class, wits, cerr := analysis.ClassifyQuerySecurity(pr)
	if cerr != nil {
		return "cannot classify the substituted query — refusing under this applet's " +
			ceiling.String() + " class"
	}
	if class >= ceiling {
		return ""
	}
	return exprCeilingReason(class, ceiling, wits, spl)
}

// exprCeilingReason renders the refusal, naming the knob that raised the class
// when one of the witnesses can be attributed to a spliced value.
//
// Attribution is what makes the message actionable: "this applet is read-class
// and the query would reach outside it" tells a reader nothing they can act on,
// where "{cond} raises …, witness url()" points at the field to edit. A witness
// outside every spliced value means the document itself carries it, which is
// not this knob's doing and is reported without a name.
func exprCeilingReason(class, ceiling analysis.QuerySecurityClassE, wits []analysis.SecurityWitness, spl []exprSplice) string {
	var b strings.Builder
	if name, wit, ok := attributeWitness(wits, spl, class); ok {
		b.WriteString("{")
		b.WriteString(name)
		b.WriteString("} raises this query to ")
		b.WriteString(class.String())
		b.WriteString(" — ")
		b.WriteString(witnessText(wit))
	} else {
		b.WriteString("the substituted query is ")
		b.WriteString(class.String())
		if w, ok := firstWitnessOf(wits, class); ok {
			b.WriteString(" — ")
			b.WriteString(witnessText(w))
		}
	}
	b.WriteString("; this applet is declared ")
	b.WriteString(ceiling.String())
	return b.String()
}

// attributeWitness finds a witness for the offending class that landed inside a
// spliced value, and names the slot it came from.
func attributeWitness(wits []analysis.SecurityWitness, spl []exprSplice, class analysis.QuerySecurityClassE) (name string, wit analysis.SecurityWitness, ok bool) {
	for _, w := range wits {
		if w.Class != class {
			continue
		}
		for _, s := range spl {
			if w.Src.Start >= s.Out.Start && w.Src.Start < s.Out.End {
				return s.Name, w, true
			}
		}
	}
	return
}

// firstWitnessOf returns the first witness carrying the offending class.
func firstWitnessOf(wits []analysis.SecurityWitness, class analysis.QuerySecurityClassE) (wit analysis.SecurityWitness, ok bool) {
	for _, w := range wits {
		if w.Class == class {
			return w, true
		}
	}
	return
}

// witnessText names what the classifier pointed at, falling back to the witness
// kind when it carries no name.
func witnessText(w analysis.SecurityWitness) string {
	if w.Name != "" {
		return "witness " + w.Name
	}
	return "witness " + w.Kind.String()
}
