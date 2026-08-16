package identsql

// identsql_roster.go declares the LW_ID_* family as data (ADR-0174 §SD3), so
// a client can list the identity vocabulary and say which of it a given
// server carries.
//
// This family is the one that is genuinely both a macro and a UDF: ExpandPass
// rewrites a call into bit arithmetic before the statement ships, and
// UdfDdlStatements emits the same semantics as CREATE FUNCTION for a server
// that wants them resolvable on its own. A consumer therefore has to be told
// which question it is asking — "can I write this" (yes, wherever ExpandPass
// runs) or "does this server have it" (only where the DDL was applied). The
// roster carries both facts about the family so a UI need not restate them.

import "github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"

// Function is one entry of the LW_ID_* family: the name a query spells, the
// parameters in order, and one line on what it does. Bodies are generated
// (expandIsValid and friends) and differ between the macro expansion and the
// UDF form only in that the macro may fold a constant argument, so they are
// not carried here — UdfDdlStatements is the source for the SQL text.
type Function struct {
	Name   string
	Params []sqlvocab.Param
	Doc    string
}

// Functions returns the LW_ID_* family in the order UdfDdlStatements emits
// it. Every entry is expandable client-side by ExpandPass and installable
// server-side by UdfDdlStatements; there is no member that is only one of the
// two.
func Functions() (fns []Function) {
	fns = []Function{
		{
			Name:   NameIsValid,
			Params: sqlvocab.Exprs("x"),
			Doc:    "1 when x is a well-formed Fibonacci-tagged identifier, 0 otherwise (a comma-less id reports 0, matching the Go decoder)",
		},
		{
			Name:   NameTagWidth,
			Params: sqlvocab.Exprs("x"),
			Doc:    "bit width of the identifier's tag field",
		},
		{
			Name:   NameTagBits,
			Params: sqlvocab.Exprs("x"),
			Doc:    "the raw tag bits, unshifted",
		},
		{
			Name:   NameBody,
			Params: sqlvocab.Exprs("x"),
			Doc:    "the identifier's body, with the tag stripped",
		},
		{
			Name:   NameTagValue,
			Params: sqlvocab.Exprs("x"),
			Doc:    "the decoded Zeckendorf tag value",
		},
		{
			Name:   NameHasTag,
			Params: []sqlvocab.Param{sqlvocab.Expr("x"), sqlvocab.Lit("tag_value", sqlvocab.DomainIdentityTag)},
			Doc:    "1 when x carries tag_value; folds to a comparison when tag_value is constant",
		},
	}
	return
}
