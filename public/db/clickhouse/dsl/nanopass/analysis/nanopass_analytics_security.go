package analysis

import (
	"sort"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// QuerySecurityClassE is the ADR-0132 §SD5 query security class: what a
// statement can *do*, judged from its text alone. Numerically smaller is
// stronger — combining witnesses takes the minimum — and the zero value is
// [QuerySecurityMutating], so an uninitialized or defaulted class fails
// closed rather than claiming "read".
type QuerySecurityClassE uint8

const (
	// QuerySecurityMutating — the statement changes state (a non-`param_*`
	// SET), or could not be shown to be anything weaker. Grammar1 parses
	// only `SET* … SELECT` chains, so every genuinely mutating statement
	// form (INSERT, DDL, SYSTEM, KILL, `INTO OUTFILE`, …) is a parse error
	// upstream of this classifier and lands here via the caller contract on
	// [ClassifyQuerySecurity].
	QuerySecurityMutating QuerySecurityClassE = iota
	// QuerySecurityReadEgress — retrieval-only, but it reaches beyond the
	// endpoint it is sent to: an egress table function (`url`, `s3`,
	// `remote`, …) or an egress scalar (`file`).
	QuerySecurityReadEgress
	// QuerySecurityRead — provably retrieval-only against the endpoint's own
	// data: no settings change, no egress construct. The class a `readonly`
	// setting can enforce on the wire.
	QuerySecurityRead
)

func (inst QuerySecurityClassE) String() (s string) {
	switch inst {
	case QuerySecurityRead:
		s = "read"
	case QuerySecurityReadEgress:
		s = "read-egress"
	default:
		s = "mutating"
	}
	return
}

// SecurityWitnessKindE names the construct kind a [SecurityWitness] pins.
type SecurityWitnessKindE uint8

const (
	// SecurityWitnessSettingsChange — a top-level `SET` touching a
	// non-`param_*` setting (a `param_*`-only SET is the parameter prelude,
	// shipped on the URL param channel, and witnesses nothing).
	SecurityWitnessSettingsChange SecurityWitnessKindE = iota
	// SecurityWitnessEgressTableFunction — a table-position function call
	// not on the local allowlist.
	SecurityWitnessEgressTableFunction
	// SecurityWitnessEgressFunction — a scalar call on the egress denylist
	// (`file`).
	SecurityWitnessEgressFunction
)

func (inst SecurityWitnessKindE) String() (s string) {
	switch inst {
	case SecurityWitnessSettingsChange:
		s = "settings change"
	case SecurityWitnessEgressTableFunction:
		s = "egress table function"
	default:
		s = "egress function"
	}
	return
}

// SecurityWitness is one construct that forced a class below
// [QuerySecurityRead]: the kind, the name as written (decoded), the class it
// forces, and where it sits in the source.
type SecurityWitness struct {
	Class QuerySecurityClassE
	Kind  SecurityWitnessKindE
	Name  string
	Src   nanopass.SourceRange
}

// localTableFunctions is the allowlist of table-position functions that read
// only the endpoint's own data (folded to lower case; ClickHouse resolves
// table functions case-insensitively). Everything *not* listed classifies as
// egress — the conservative direction for the small table-function
// vocabulary, where an allowlist is tractable. Additions belong here as
// ClickHouse grows local generators.
var localTableFunctions = map[string]struct{}{
	// Row generators and inline literals.
	"numbers":         {},
	"numbers_mt":      {},
	"zeros":           {},
	"zeros_mt":        {},
	"generaterandom":  {},
	"generate_series": {},
	"generateseries":  {},
	"values":          {},
	"format":          {},
	"null":            {},
	// Server-local relation wrappers.
	"merge":           {},
	"view":            {},
	"viewifpermitted": {},
	// A dictionary is a server-local object; whether *its* source reaches out
	// is server configuration the text cannot see — the recorded ADR-0132
	// static-analysis limit, shared with views and table engines.
	"dictionary": {},
	// boxer's introspection macro (ADR-0094), classified pre-expansion as a
	// local read: the `url()` it later expands to is pass-generated machinery,
	// not authored egress (ADR-0132 §SD5).
	"keelson": {},
}

// egressScalarFunctions is the denylist of scalar calls that reach outside
// the query's own data (folded to lower case). The scalar vocabulary is far
// too large to allowlist, so — unlike table position — an unlisted scalar is
// presumed pure; this asymmetry is part of the recorded ADR-0132 limit.
var egressScalarFunctions = map[string]struct{}{
	"file": {},
}

// ClassifyQuerySecurity assigns a parsed buffer its ADR-0132 §SD5 security
// class and returns the witnesses that forced any class below
// [QuerySecurityRead], ordered by source position.
//
// Caller contract (the ADR's conservative direction): call [nanopass.Parse]
// first and treat a parse error as **cannot classify → the strongest class**
// ([QuerySecurityMutating]). Grammar1's root is `SET* … SELECT`-chain only,
// so INSERT/DDL/SYSTEM and every other genuinely mutating form arrives at
// the caller as exactly that parse error.
//
// On a parsed tree the classification is:
//
//   - a top-level SET whose settings are all `param_*` is the parameter
//     prelude — no witness; any non-`param_*` setting in a SET witnesses
//     [QuerySecurityMutating]. A query-tail `SETTINGS` clause is *not* a
//     witness: it is a per-query execution knob, not a state change (whether
//     it constrains the `readonly` enforcement value is the SD5
//     implementation question, decided against the pinned server);
//   - a table-position function call off the local allowlist witnesses
//     [QuerySecurityReadEgress] (unknown table functions are presumed to
//     reach out); a scalar call on the egress denylist likewise;
//   - otherwise the buffer classifies [QuerySecurityRead].
//
// The guarantee stops at what the text shows: views, dictionaries, table
// engines, and UDFs can reach further than any static reading of the buffer
// (the honesty clause of ADR-0132). err is non-nil only for a tree not
// produced by [nanopass.Parse]; class is then the zero value (mutating).
func ClassifyQuerySecurity(pr *nanopass.ParseResult) (class QuerySecurityClassE, witnesses []SecurityWitness, err error) {
	if pr == nil || pr.Tree == nil {
		err = eh.Errorf("ClassifyQuerySecurity: nil parse result")
		return
	}
	nodes := nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		switch ctx.(type) {
		case *grammar1.SetStmtContext,
			*grammar1.TableFunctionExprContext,
			*grammar1.ColumnExprFunctionContext:
			return true
		}
		return false
	})
	for _, n := range nodes {
		switch ctx := n.(type) {
		case *grammar1.SetStmtContext:
			witnesses = appendSetStmtWitnesses(pr, ctx, witnesses)
		case *grammar1.TableFunctionExprContext:
			name, ok := identifierName(ctx.Identifier())
			if !ok {
				// A table function without a readable name cannot be proven
				// local — witness it as egress (fail closed).
				name = ctx.GetText()
			}
			if _, local := localTableFunctions[strings.ToLower(name)]; !ok || !local {
				witnesses = append(witnesses, SecurityWitness{
					Class: QuerySecurityReadEgress,
					Kind:  SecurityWitnessEgressTableFunction,
					Name:  name,
					Src:   pr.SourceRangeOf(ctx),
				})
			}
		case *grammar1.ColumnExprFunctionContext:
			name, ok := identifierName(ctx.Identifier())
			if !ok {
				continue
			}
			if _, egress := egressScalarFunctions[strings.ToLower(name)]; egress {
				witnesses = append(witnesses, SecurityWitness{
					Class: QuerySecurityReadEgress,
					Kind:  SecurityWitnessEgressFunction,
					Name:  name,
					Src:   pr.SourceRangeOf(ctx),
				})
			}
		}
	}
	sort.SliceStable(witnesses, func(i, j int) bool {
		if witnesses[i].Src.Start != witnesses[j].Src.Start {
			return witnesses[i].Src.Start < witnesses[j].Src.Start
		}
		return witnesses[i].Name < witnesses[j].Name
	})
	class = QuerySecurityRead
	for i := range witnesses {
		if witnesses[i].Class < class {
			class = witnesses[i].Class
		}
	}
	return
}

// appendSetStmtWitnesses adds one settings-change witness per non-`param_*`
// setting in a top-level SET (grammar1 admits setStmt only at the query
// root, so nothing nested arrives here). A SET whose expression list does
// not have the expected shape is witnessed whole — a settings statement that
// cannot be read cannot be cleared (fail closed).
func appendSetStmtWitnesses(pr *nanopass.ParseResult, stmt *grammar1.SetStmtContext, witnesses []SecurityWitness) []SecurityWitness {
	sel, ok := stmt.SettingExprList().(*grammar1.SettingExprListContext)
	if !ok {
		return append(witnesses, SecurityWitness{
			Class: QuerySecurityMutating,
			Kind:  SecurityWitnessSettingsChange,
			Name:  stmt.GetText(),
			Src:   pr.SourceRangeOf(stmt),
		})
	}
	for _, ise := range sel.AllSettingExpr() {
		se, isSE := ise.(*grammar1.SettingExprContext)
		if !isSE {
			continue
		}
		name, named := identifierName(se.Identifier())
		if named && strings.HasPrefix(name, "param_") {
			continue
		}
		if !named {
			name = se.GetText()
		}
		witnesses = append(witnesses, SecurityWitness{
			Class: QuerySecurityMutating,
			Kind:  SecurityWitnessSettingsChange,
			Name:  name,
			Src:   pr.SourceRangeOf(se),
		})
	}
	return witnesses
}

// identifierName decodes a grammar identifier node's text (unquoting
// backticks and double quotes); ok is false for a nil or empty node.
func identifierName(ictx grammar1.IIdentifierContext) (name string, ok bool) {
	if ictx == nil {
		return
	}
	name = nanopass.DecodeIdentifier(ictx.GetText())
	ok = name != ""
	return
}
