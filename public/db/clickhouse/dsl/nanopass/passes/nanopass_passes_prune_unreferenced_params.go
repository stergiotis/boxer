//go:build llm_generated_opus47

package passes

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// PruneUnreferencedParams returns a Pass that drops env.Params entries whose
// extracted-param name no longer appears anywhere in the body — either as a
// `{name: Type}` slot or as a bare identifier (CTE-injected reference).
//
// Meant to follow passes that fold or remove parameter placeholders. Composes
// in either order with InjectParamsAsCTE thanks to the bare-identifier scan.
//
// Only env.Params entries whose name parses against the extraction prefix are
// candidates — session-level settings and any param whose name does not match
// the prefix-plus-metadata format are preserved verbatim.
//
// If prefix is empty, ParamPrefixExtracted is used.
func PruneUnreferencedParams(prefix string) nanopass.Pass {
	if prefix == "" {
		prefix = ParamPrefixExtracted
	}
	return nanopass.Pass{
		Name: "PruneUnreferencedParams",
		Apply: func(e *env.Environment, body string) (string, error) {
			referenced, err := collectReferencedParamNames(body, prefix)
			if err != nil {
				return "", eh.Errorf("PruneUnreferencedParams: %w", err)
			}
			if e == nil {
				return body, nil
			}
			for name := range e.Params {
				if !strings.HasPrefix(name, prefix+"_") {
					continue
				}
				if _, _, parseErr := ParseParamName(name, prefix); parseErr != nil {
					continue
				}
				if referenced[name] {
					continue
				}
				delete(e.Params, name)
			}
			return body, nil
		},
		Properties: nanopass.PassProperties{
			Idempotent: true,
			Reads:      nanopass.RegionBody | nanopass.RegionParams,
			Writes:     nanopass.RegionParams,
		},
	}
}

// collectReferencedParamNames parses the body and returns the set of
// extracted-param names referenced anywhere in it — both `{name: Type}` slots
// and bare identifiers that parse as valid extracted-param names.
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
				names[nanopass.DecodeIdentifier(id.GetText())] = true
			}
		case *grammar1.IdentifierContext:
			// Decode first — CTE-injected references may have been
			// double-quoted by CanonicalizeIdentifiers.
			text := nanopass.DecodeIdentifier(c.GetText())
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
