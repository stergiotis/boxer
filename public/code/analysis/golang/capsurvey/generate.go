package capsurvey

import (
	"context"
	"os"
	"path/filepath"

	"github.com/stergiotis/boxer/public/code/analysis/golang/propsfile"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/packageprops"
)

// GenerateResult summarizes a GenerateProps run.
type GenerateResult struct {
	Created      int      // declarations that did not exist before
	Updated      int      // existing declarations whose capability verdict changed
	Unchanged    int      // existing declarations already stating this verdict
	WrittenPaths []string // created + updated
}

// GenerateProps surveys opts and records each package's capability verdict in
// its package_props.go.
//
// Unlike the wasm survey's generate, this always overlays rather than
// idempotent-creating. It cannot clobber curated intent: it owns the Caps*
// fields and nothing else, so every other field of an existing declaration —
// the wasm verdicts, a hand-set Kind — is preserved by construction
// (ADR-0120 SD7). Files whose verdict is unchanged are not rewritten, so a
// re-run on a settled tree produces an empty diff.
//
// Scope is every package the survey matched, including main and internal
// packages that the wasm survey cannot reach (ADR-0120 SD6). packageprops
// itself is skipped: it cannot import itself to declare its own props.
func GenerateProps(ctx context.Context, opts Options) (res GenerateResult, err error) {
	var s Survey
	s, err = Run(ctx, opts)
	if err != nil {
		return
	}
	if len(s.Unknown) > 0 {
		err = eb.Build().Strs("capabilities", s.Unknown).Errorf(
			"capslock reported capabilities that public/packageprops does not know; extend packageprops.Capability before generating, or the verdicts would silently omit them")
		return
	}
	for _, pr := range s.Packages {
		if pr.Dir == "" || pr.Name == "" || pr.ImportPath == propsfile.ImportPath {
			continue
		}
		path := filepath.Join(pr.Dir, propsfile.FileName)

		var base packageprops.Props
		_, statErr := os.Stat(path)
		exists := statErr == nil
		if exists {
			// A declaration this parser cannot read degrades to the zero value,
			// which would silently drop another survey's verdicts. Fail instead:
			// the file is committed source, and a parse failure means it is
			// malformed or the vocabulary has moved on.
			base, err = propsfile.Parse(path)
			if err != nil {
				err = eb.Build().Str("path", path).Errorf("parse existing props file: %w", err)
				return
			}
		}

		merged := propsfile.Merge(base, packageprops.Props{
			CapsDirect:    pr.Direct,
			CapsReachable: pr.Reachable,
		}, propsfile.FieldsCaps)
		if exists && merged == base {
			res.Unchanged++
			continue
		}

		var src []byte
		src, err = propsfile.Render(pr.Name, pr.ImportPath, merged)
		if err != nil {
			return
		}
		if err = os.WriteFile(path, src, 0o644); err != nil {
			err = eb.Build().Str("path", path).Errorf("write props file: %w", err)
			return
		}
		if exists {
			res.Updated++
		} else {
			res.Created++
		}
		res.WrittenPaths = append(res.WrittenPaths, path)
	}
	return
}

// VerifyResult is one package whose declared capability verdict disagrees with
// the freshly surveyed one.
type VerifyResult struct {
	ImportPath        string
	DeclaredDirect    packageprops.CapabilitySet
	SurveyedDirect    packageprops.CapabilitySet
	DeclaredReachable packageprops.CapabilitySet
	SurveyedReachable packageprops.CapabilitySet
}

// VerifyProps reconciles the declared capability verdicts against a fresh
// survey, returning the packages that disagree.
//
// Unlike the wasm survey's verify — which gates only on a regression, because a
// package that stopped compiling for wasm is the actionable case — any capability
// drift is reported. A package that gained a capability is the security-relevant
// direction, and one that lost a capability means the declaration is simply
// stale. Both want a regeneration.
//
// Declarations with no capability verdict (the zero set) are skipped: they
// assert nothing, so there is nothing to contradict.
func VerifyProps(ctx context.Context, opts Options) (drift []VerifyResult, err error) {
	var s Survey
	s, err = Run(ctx, opts)
	if err != nil {
		return
	}
	for _, pr := range s.Packages {
		if pr.Dir == "" || pr.ImportPath == propsfile.ImportPath {
			continue
		}
		path := filepath.Join(pr.Dir, propsfile.FileName)
		if _, statErr := os.Stat(path); statErr != nil {
			continue // no declaration to reconcile
		}
		var declared packageprops.Props
		declared, err = propsfile.Parse(path)
		if err != nil {
			err = eb.Build().Str("path", path).Errorf("parse props file: %w", err)
			return
		}
		if !declared.CapsDirect.Surveyed() && !declared.CapsReachable.Surveyed() {
			continue // asserts nothing
		}
		if declared.CapsDirect == pr.Direct && declared.CapsReachable == pr.Reachable {
			continue
		}
		drift = append(drift, VerifyResult{
			ImportPath:        pr.ImportPath,
			DeclaredDirect:    declared.CapsDirect,
			SurveyedDirect:    pr.Direct,
			DeclaredReachable: declared.CapsReachable,
			SurveyedReachable: pr.Reachable,
		})
	}
	return
}
