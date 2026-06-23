package play

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// paramSlot is one `{name : Type}` placeholder occurrence the
// debounced parse found in the editor buffer. The renderer pairs
// each unique Name with a paramWidget that authors a matching
// `SET param_<Name> = ...;` line in the editor's leading prelude
// (see play_param_inject.go); ExtractParams then ships the value
// over ClickHouse's HTTP param channel at execute time.
//
// Name keeps the placeholder's casing verbatim; Type carries the
// raw column-type source text (e.g. "UInt64", "DateTime",
// "Nullable(String)"). Src bounds the placeholder for downstream
// uses that want to highlight or rewrite it — v1 only reads Name
// and Type, but keeping Src keeps the type cheap to extend.
type paramSlot struct {
	Name string
	Type string
	Src  nanopass.SourceRange
}

// ExtractParamSlots walks sql via the Grammar1 parser and returns
// one paramSlot per ColumnExprParamSlot CST node. Duplicate names
// are returned with the first occurrence's Type and Src. Hot-path
// callers should prefer extractSlotsAndParams, which parses once
// and produces both the slot list and the prelude value map.
func ExtractParamSlots(sql string) (slots []paramSlot, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("ExtractParamSlots: %w", err)
		return
	}
	slots = collectParamSlots(pr)
	return
}

// extractSlotsAndParams parses sql once and returns the placeholder
// list plus the `param_<name> → value` map harvested from the
// leading SET prelude. Equivalent to ExtractParamSlots + ExtractParams
// in series, but single-parse — the editor's per-debounce path uses
// this to halve its ANTLR cost.
//
// Errors from value collection (e.g. a SET statement that mixes
// param_* with regular settings) come back as err; the slot list is
// still returned so the UI can render widgets while the user fixes
// the SET line.
func extractSlotsAndParams(sql string) (slots []paramSlot, params map[string]string, err error) {
	pr, perr := nanopass.Parse(sql)
	if perr != nil {
		err = eh.Errorf("extractSlotsAndParams: %w", perr)
		return
	}
	slots = collectParamSlots(pr)
	params, err = collectParamValues(pr)
	if err != nil {
		err = eh.Errorf("extractSlotsAndParams: %w", err)
	}
	return
}

// collectParamSlots is the CST walk that backs ExtractParamSlots and
// extractSlotsAndParams. Pure read — no rewriter, no allocation
// beyond the slot slice and the dedup set.
func collectParamSlots(pr *nanopass.ParseResult) (out []paramSlot) {
	seen := make(map[string]struct{})
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		ps, ok := ctx.(*grammar1.ParamSlotContext)
		if !ok {
			return true
		}
		ident := ps.Identifier()
		typeCtx := ps.ColumnTypeExpr()
		if ident == nil || typeCtx == nil {
			return true
		}
		name := strings.Trim(ident.GetText(), "`")
		if name == "" {
			return true
		}
		if _, dup := seen[name]; dup {
			return true
		}
		seen[name] = struct{}{}
		out = append(out, paramSlot{
			Name: name,
			Type: strings.TrimSpace(nanopass.NodeText(pr, typeCtx)),
			Src:  pr.SourceRangeOf(ps),
		})
		return true
	})
	return
}

// collectParamValues backs the orchestrator's prelude-value cache: it returns
// just the `param_<name> → value` map from the leading SET prelude, reusing
// the same harvest as ExtractParams (collectParamSettings) and discarding the
// SetStmt-deletion list. Rejects mixed SETs (param_* + regular settings).
func collectParamValues(pr *nanopass.ParseResult) (params map[string]string, err error) {
	params, _, err = collectParamSettings(pr)
	return
}
