//go:build llm_generated_opus46

package passes

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// PruneUnreferencedParams returns a Pass that drops SET lines for extracted
// parameters whose name no longer appears anywhere in the query body — either
// as a `{name: Type}` slot or as a bare identifier (e.g. a CTE-injected
// reference like `WITH value AS name … SELECT … name …`).
//
// It is meant to follow passes that fold or remove parameter placeholders —
// for example FunctionEvaluator after constant folding consumed inputs —
// where the source SETs would otherwise survive as dead prelude. Composes in
// either order with InjectParamsAsCTE thanks to the bare-identifier scan.
//
// Only SET lines whose names parse against the extraction prefix are
// candidates. Session-level SETs (and any SET line that does not match the
// prefix) are preserved verbatim.
//
// The body is re-parsed and walked at the CST level: a SET is kept if its
// name matches a `ParamSlotContext` identifier or any `IdentifierContext`
// whose text successfully parses as an extracted-param name (prefix match
// plus valid CBOR-encoded metadata suffix — same predicate used to admit
// SET lines). Tokens inside comments or string literals do not count.
//
// If prefix is empty, ParamPrefixExtracted is used.
func PruneUnreferencedParams(prefix string) nanopass.Pass {
	if prefix == "" {
		prefix = ParamPrefixExtracted
	}
	return func(sql string) (result string, err error) {
		extractedSets, regularSets, body := ParseExtractedQuery(sql, prefix)
		if len(extractedSets) == 0 {
			result = sql
			return
		}

		referenced, scanErr := collectReferencedParamNames(body, prefix)
		if scanErr != nil {
			err = eh.Errorf("PruneUnreferencedParams: %w", scanErr)
			return
		}

		var sb strings.Builder
		sb.Grow(len(sql))
		for _, set := range extractedSets {
			name := extractSetParamName(set)
			if name == "" || referenced[name] {
				sb.WriteString(set)
				sb.WriteString(";\n")
			}
		}
		for _, set := range regularSets {
			sb.WriteString(set)
			sb.WriteString(";\n")
		}
		sb.WriteString(body)
		result = sb.String()
		return
	}
}

// collectReferencedParamNames parses the body and returns the set of
// extracted-param names referenced anywhere in it — both `{name: Type}` slots
// and bare identifiers that parse as valid extracted-param names. The prefix
// is used as a fast-path filter before attempting CBOR-metadata validation.
// An empty/whitespace-only body yields an empty set without parsing.
func collectReferencedParamNames(body string, prefix string) (names map[string]bool, err error) {
	names = make(map[string]bool, 8)
	if strings.TrimSpace(body) == "" {
		return
	}
	pr, parseErr := nanopass.Parse(body)
	if parseErr != nil {
		err = eh.Errorf("collectReferencedParamNames: %w", parseErr)
		return
	}
	bareNamePrefix := prefix + "_"
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		switch c := ctx.(type) {
		case *grammar1.ParamSlotContext:
			if id := c.Identifier(); id != nil {
				names[id.GetText()] = true
			}
		case *grammar1.IdentifierContext:
			text := c.GetText()
			if !strings.HasPrefix(text, bareNamePrefix) {
				return true
			}
			if _, _, parseErr := ParseParamName(text, prefix); parseErr != nil {
				return true
			}
			names[text] = true
		}
		return true
	})
	return
}

// extractSetParamName parses `SET name = value` and returns name.
// Returns the empty string for malformed inputs; callers treat that as
// "leave alone" (preserve the SET line).
func extractSetParamName(setLine string) (name string) {
	parts := strings.SplitN(setLine, " = ", 2)
	if len(parts) != 2 {
		return
	}
	name = strings.TrimSpace(strings.TrimPrefix(parts[0], "SET "))
	return
}
