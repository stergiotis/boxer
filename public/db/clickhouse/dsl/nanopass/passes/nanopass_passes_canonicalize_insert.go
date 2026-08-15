package passes

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// CanonicalizeInsertWrapper drops the TABLE noise word from an INSERT
// wrapper (ADR-0181 §SD8): grammar1 admits `INSERT INTO TABLE t …` for
// upstream-lineage fidelity, but canonical form keeps one spelling per
// statement and grammar2's mirror deliberately has no TABLE alternative —
// leaving the word in would make the canonicalizer's own output fail its
// terminal grammar2 proof. Everything else about the wrapper canonicalises
// through the generic sub-passes (identifier quoting, keyword case), which
// walk the whole CST.
//
// Runs before CanonicalizeWhitespaceSingleLine in the sequence, so the gap
// the deleted token leaves behind is collapsed by the pass whose job that
// is.
var CanonicalizeInsertWrapper = nanopass.LiftBodyPass(
	"CanonicalizeInsertWrapper",
	canonicalizeInsertWrapperImpl,
	nanopass.PassProperties{
		Idempotent: true,
		Reads:      nanopass.RegionBody,
		Writes:     nanopass.RegionBody,
	},
)

func canonicalizeInsertWrapperImpl(sql string) (result string, err error) {
	result = sql
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("CanonicalizeInsertWrapper: %w", err)
		return
	}
	ins := pr.InsertStmt()
	if ins == nil {
		return
	}
	tbl := ins.TABLE()
	if tbl == nil {
		return
	}
	rw := nanopass.NewRewriter(pr)
	idx := tbl.GetSymbol().GetTokenIndex()
	rw.DeleteDefault(idx, idx)
	result = nanopass.GetText(rw)
	return
}
