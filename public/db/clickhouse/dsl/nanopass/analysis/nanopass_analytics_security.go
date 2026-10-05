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
	// QuerySecurityMutating — the statement changes state (an INSERT
	// wrapper, a non-`param_*` SET), or could not be shown to be anything
	// weaker. Grammar1 parses `SET* … SELECT` chains and the INSERT wrapper
	// (ADR-0181 §SD8) — the latter is witnessed directly; every other
	// mutating statement form (DDL, SYSTEM, KILL, `INTO OUTFILE`, …) is a
	// parse error upstream of this classifier and lands here via the caller
	// contract on [ClassifyQuerySecurity].
	QuerySecurityMutating QuerySecurityClassE = iota
	// QuerySecurityReadEgress — retrieval-only, but it reaches beyond the
	// endpoint it is sent to: a table function that reads elsewhere (`url`,
	// `s3`, `remote`, `file`, `eval`, …) or one the vocabulary does not know,
	// or a scalar that does (`file`, the `ai*` functions).
	QuerySecurityReadEgress
	// QuerySecurityRead — retrieval-only against the endpoint's own data as
	// far as the text shows: no settings change, no state-changing call, no
	// construct that reaches out. The class a `readonly` setting can enforce
	// on the wire.
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
	// SecurityWitnessEgressTableFunction — a table function read as a table
	// that reaches beyond the endpoint, or that the vocabulary does not
	// know; Reach says which.
	SecurityWitnessEgressTableFunction
	// SecurityWitnessEgressFunction — a scalar call that reaches beyond the
	// query's own data; Reach says how.
	SecurityWitnessEgressFunction
	// SecurityWitnessInsertWrapper — the statement is an
	// `INSERT INTO … SELECT` (ADR-0181 §SD8); Name carries the target.
	SecurityWitnessInsertWrapper
	// SecurityWitnessStateChangingFunction — a scalar call that changes
	// persistent server state from inside a SELECT (`generateSerialID`).
	SecurityWitnessStateChangingFunction
)

func (inst SecurityWitnessKindE) String() (s string) {
	switch inst {
	case SecurityWitnessSettingsChange:
		s = "settings change"
	case SecurityWitnessEgressTableFunction:
		s = "egress table function"
	case SecurityWitnessInsertWrapper:
		s = "insert wrapper"
	case SecurityWitnessStateChangingFunction:
		s = "state-changing function"
	default:
		s = "egress function"
	}
	return
}

// SecurityWitness is one construct that forced a class below
// [QuerySecurityRead]: the kind, the name as written (decoded), the class it
// forces, how far it reaches (egress witnesses), and where it sits in the
// source.
type SecurityWitness struct {
	Class QuerySecurityClassE
	Kind  SecurityWitnessKindE
	Name  string
	Reach SecurityReachE
	Src   nanopass.SourceRange
}

// Describe is the witness as a reader sees it: the kind, and how far it
// reaches when it reaches out.
func (inst SecurityWitness) Describe() (s string) {
	s = inst.Kind.String()
	if r := inst.Reach.String(); r != "" {
		s += " (" + r + ")"
	}
	return
}

// ClassifyQuerySecurity assigns a parsed buffer its ADR-0132 §SD5 security
// class and returns the witnesses that forced any class below
// [QuerySecurityRead], ordered by source position.
//
// Caller contract (the ADR's conservative direction): call [nanopass.Parse]
// first and treat a parse error as **cannot classify → the strongest class**
// ([QuerySecurityMutating]). Grammar1's root is `SET* … SELECT` chains plus
// exactly one write shape — the INSERT wrapper (ADR-0181 §SD8) — so DDL,
// SYSTEM and every other mutating form still arrives at the caller as that
// parse error, while a parsed INSERT is witnessed below.
//
// On a parsed tree the classification is:
//
//   - the INSERT wrapper witnesses [QuerySecurityMutating] outright — the
//     write is the statement's whole point;
//   - a top-level SET whose settings are all `param_*` is the parameter
//     prelude — no witness; any non-`param_*` setting in a SET witnesses
//     [QuerySecurityMutating]. A query-tail `SETTINGS` clause is *not* a
//     witness: it is a per-query execution knob, not a state change (whether
//     it constrains the `readonly` enforcement value is the SD5
//     implementation question, decided against the pinned server);
//   - a table function read as a table witnesses [QuerySecurityReadEgress]
//     when the vocabulary says it reaches beyond the endpoint or does not
//     know it (presumed to reach out);
//   - a call in a table function's arguments is read as ClickHouse reads
//     it — an expression, except a table function among the arguments of
//     one that takes a table (`loop`, `remote`, …) — so a tuple, `concat`
//     or `currentDatabase()` there is not a table function, and the class
//     is the same before and after canonicalisation;
//   - a scalar call that reaches out (`file`, `catboostEvaluate`, the
//     `ai*` functions) witnesses [QuerySecurityReadEgress]; one that changes
//     persistent server state (`generateSerialID`) witnesses
//     [QuerySecurityMutating];
//   - otherwise the buffer classifies [QuerySecurityRead].
//
// The guarantee stops at what the text shows: views, dictionaries, table
// engines, and UDFs can reach further than any static reading of the buffer
// (the honesty clause of ADR-0132), and a scalar the vocabulary does not name
// is presumed pure. err is non-nil only for a tree not
// produced by [nanopass.Parse]; class is then the zero value (mutating).
func ClassifyQuerySecurity(pr *nanopass.ParseResult) (class QuerySecurityClassE, witnesses []SecurityWitness, err error) {
	if pr == nil || pr.Tree == nil {
		err = eh.Errorf("ClassifyQuerySecurity: nil parse result")
		return
	}
	// The INSERT wrapper (ADR-0181 §SD8) is the one mutating form grammar1
	// parses, so it must be witnessed from the tree — before the port it
	// arrived as a parse error and classified mutating via the caller
	// contract; a fall-through here would have flipped it to "read".
	if ins := pr.InsertStmt(); ins != nil {
		name := ""
		if tid, ok := ins.TableIdentifier().(*grammar1.TableIdentifierContext); ok {
			name = nanopass.TableIdentifierName(tid)
			if db := nanopass.DatabaseIdentifierName(tid.DatabaseIdentifier()); db != "" {
				name = db + "." + name
			}
		}
		witnesses = append(witnesses, SecurityWitness{
			Class: QuerySecurityMutating,
			Kind:  SecurityWitnessInsertWrapper,
			Name:  name,
			Src:   pr.SourceRangeOf(ins),
		})
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
			if !readAsTable(ctx) {
				if ok {
					witnesses = appendScalarWitness(pr, ctx, name, witnesses)
				}
				continue
			}
			info, known := lookupTableFunction(name)
			switch {
			case !ok:
				// A table function without a readable name cannot be
				// proven local (fail closed).
				witnesses = append(witnesses, SecurityWitness{Class: QuerySecurityReadEgress, Kind: SecurityWitnessEgressTableFunction,
					Name: ctx.GetText(), Reach: SecurityReachUnknown, Src: pr.SourceRangeOf(ctx)})
			case !known:
				witnesses = append(witnesses, SecurityWitness{Class: QuerySecurityReadEgress, Kind: SecurityWitnessEgressTableFunction,
					Name: name, Reach: SecurityReachUnknown, Src: pr.SourceRangeOf(ctx)})
			case info.reach != SecurityReachNone:
				witnesses = append(witnesses, SecurityWitness{Class: QuerySecurityReadEgress, Kind: SecurityWitnessEgressTableFunction,
					Name: name, Reach: info.reach, Src: pr.SourceRangeOf(ctx)})
			}
		case *grammar1.ColumnExprFunctionContext:
			if name, ok := identifierName(ctx.Identifier()); ok {
				witnesses = appendScalarWitness(pr, ctx, name, witnesses)
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

// readAsTable reports whether ClickHouse reads a table-function node as a
// table. The grammar keeps every call in a table function's argument list
// as a table-function node — and canonicalisation turns `(…)` and `[…]`
// there into `tuple(…)` and `array(…)` calls — but the server evaluates
// those arguments as expressions. Only a table function that takes a table
// (`loop`, `viewIfPermitted`, `remote`, `cluster`, …) reads a table function
// among its arguments as a table. Under a local one, an argument the
// vocabulary does not know is read as a table too (fail closed); under one
// that reaches out it cannot lower the class further.
func readAsTable(tf *grammar1.TableFunctionExprContext) (table bool) {
	arg, isArg := tf.GetParent().(*grammar1.TableArgExprContext)
	if !isArg {
		// The FROM/JOIN position, or a shape the grammar may grow: a table.
		return true
	}
	owner := owningTableFunction(arg)
	if owner == nil {
		return true // fail closed
	}
	if !readAsTable(owner) {
		// An argument of an expression is an expression.
		return false
	}
	ownerName, named := identifierName(owner.Identifier())
	info, known := lookupTableFunction(ownerName)
	if !named || !known || !info.takesTable {
		return false
	}
	name, ok := identifierName(tf.Identifier())
	if _, isTableFunction := lookupTableFunction(name); ok && isTableFunction {
		return true
	}
	return info.reach == SecurityReachNone
}

// owningTableFunction is the table function whose argument list holds arg.
func owningTableFunction(arg *grammar1.TableArgExprContext) (owner *grammar1.TableFunctionExprContext) {
	list, ok := arg.GetParent().(*grammar1.TableArgListContext)
	if !ok {
		return nil
	}
	owner, _ = list.GetParent().(*grammar1.TableFunctionExprContext)
	return
}

// appendScalarWitness witnesses a scalar call that reaches out or changes
// state; any other scalar is presumed pure.
func appendScalarWitness(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext, name string, witnesses []SecurityWitness) []SecurityWitness {
	lname := strings.ToLower(name)
	if reach, egress := egressScalarFunctions[lname]; egress {
		witnesses = append(witnesses, SecurityWitness{Class: QuerySecurityReadEgress, Kind: SecurityWitnessEgressFunction,
			Name: name, Reach: reach, Src: pr.SourceRangeOf(ctx)})
	}
	if _, changes := stateChangingScalarFunctions[lname]; changes {
		witnesses = append(witnesses, SecurityWitness{Class: QuerySecurityMutating, Kind: SecurityWitnessStateChangingFunction,
			Name: name, Src: pr.SourceRangeOf(ctx)})
	}
	return witnesses
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
