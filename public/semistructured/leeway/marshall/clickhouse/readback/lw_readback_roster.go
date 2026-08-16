package readback

// lw_readback_roster.go declares the read-back helper family as data, beside
// the SQL that creates it (ADR-0174 §SD3). The SQL provisions; the roster
// says what the provisioning is supposed to contain, which is what a client
// needs to tell "this server does not have it" from "this server has
// something else".
//
// It is a second declaration of the same facts, deliberately: recovering
// names by parsing lw_readback_udfs.sql at run time would put a SQL parser on
// a read path, where a parse bug shrinks the roster silently. The two are
// pinned to each other by TestRosterMatchesSQL instead — a function added to
// the SQL without a roster entry, or vice versa, fails the build.
//
// Unlike chpack, this family carries no version marker, so a client can ask a
// server *which* of these it has but not which revision — see
// ADR-0171 §SD2.

import "github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"

// Function is one entry of the read-back helper family: the name a query
// spells, the parameters in order, and one line on what it does. The body
// lives in lw_readback_udfs.sql and is not repeated here — a caller that
// wants it asks the server, whose `create_query` is the truth about what is
// actually installed.
type Function struct {
	Name   string
	Params []sqlvocab.Param
	Doc    string
}

// HelperFunctions returns the family HelperUDFsSQL() creates, in the order
// the SQL declares them (dependency order: the server resolves referenced
// functions at CREATE time).
//
// The co/ragged pack that HelperUDFsSQL emits ahead of these is NOT included
// — it is chpack's roster, and chpack.Functions() is where it is declared.
// A consumer wanting everything HelperUDFsSQL provisions concatenates the two,
// which is also the order they install in.
func HelperFunctions() (fns []Function) {
	fns = []Function{
		{
			Name:   "LW_LU_VAL_IDX_TO_MEMB_IDX_END_EXCL",
			Params: sqlvocab.Exprs("cardcol"),
			Doc:    "exclusive end of each attribute's membership-index range in the flattened membership array",
		},
		{
			Name:   "LW_LU_VAL_IDX_TO_MEMB_IDX_BEGIN_INCL",
			Params: sqlvocab.Exprs("cardcol"),
			Doc:    "inclusive start of each attribute's membership-index range in the flattened membership array",
		},
		{
			Name:   "LW_LU_VAL_BY_MEMB_IDX",
			Params: sqlvocab.Exprs("valcol", "cardcol"),
			Doc:    "value broadcast: each membership position carries its owning attribute's value",
		},
		{
			Name:   "LW_LU_ATTR_BY_TAG",
			Params: sqlvocab.Exprs("tagcol", "tagval", "m2v"),
			Doc:    "attribute index carrying membership tagval, or 0 if absent; m2v is LW_RAGGED_PARENT_IDS(cardcol)",
		},
		{
			Name:   "LW_LU_MEMBS_OF_VAL_IDX",
			Params: sqlvocab.Exprs("membcol", "cardcol", "validx"),
			Doc:    "membership set an attribute plays on a channel, aliasing-aware",
		},
		{
			Name:   "LW_VALUE_BY_TAG_EQUAL",
			Params: sqlvocab.Exprs("valcol", "tagcol", "tagval", "m2v"),
			Doc:    "scalar value of the attribute tagged with tagval; the type default if absent",
		},
		{
			Name:   "LW_LIST_BY_TAG_EQUAL",
			Params: sqlvocab.Exprs("valFlat", "lencol", "tagcol", "tagval", "m2v"),
			Doc:    "array/set value of the attribute tagged with tagval; [] if absent — the form that does not truncate under non-uniform cardinality",
		},
	}
	return
}
