package codelint

import (
	"go/ast"
	"go/constant"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// A copy to the clipboard is a labelled broker request (ADR-0317; the
// keelson-app skill's D5): `clipboardbroker.Write` with a `Payload` carrying
// the text's label and source, whose record the caller shows. Two shapes
// reach the same subject without the label: a `Request` on the bus with
// `clipboardbroker.SubjectWrite` (or its value) as the subject and the
// text as raw bytes, and `clipboardbroker.WriteText`, the legacy call that
// does exactly that. Both leave a copy in the journal with no label, so
// the agentic tier it carries is unknown. CS014 covers the opcode built
// below the broker; this rule covers the request built beside it.
const (
	clipboardWriteSubject  = "clipboard.write" // mirrors clipboardbroker.SubjectWrite
	clipboardWriteTextFunc = "WriteText"
)

// RuleCS015 — a raw `Request` on the clipboard write subject, or a use of
// `clipboardbroker.WriteText`, outside the clipboard broker's own package.
// Test files are exempt, as they are not the shipped runtime surface.
type RuleCS015 struct{}

func NewRuleCS015() (inst *RuleCS015) {
	inst = &RuleCS015{}
	return
}

func (inst *RuleCS015) Id() (id string) {
	id = "CS015"
	return
}

func (inst *RuleCS015) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityWarn
	return
}

func (inst *RuleCS015) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs015",
		Doc:  "CS015: copy to the clipboard through clipboardbroker.Write with a Payload, not a raw clipboard.write request or WriteText",
		Run:  inst.run,
	}
	return
}

// cs015Exempt reports whether a package path is the clipboard broker's own,
// matched exactly: a subpackage is not exempt.
func cs015Exempt(path string) bool {
	return path == clipboardBrokerPkgPath
}

// isClipboardWriteSubject reports whether expr is a constant string equal to
// the clipboard write subject — the `clipboardbroker.SubjectWrite` constant
// by any name, or the literal spelled out.
func isClipboardWriteSubject(pass *analysis.Pass, expr ast.Expr) (is bool) {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return
	}
	is = constant.StringVal(tv.Value) == clipboardWriteSubject
	return
}

// isClipboardWriteText reports whether obj is the broker's unlabelled text
// call.
func isClipboardWriteText(obj types.Object) (is bool) {
	fn, ok := obj.(*types.Func)
	if !ok || fn.Name() != clipboardWriteTextFunc || fn.Pkg() == nil {
		return
	}
	is = fn.Pkg().Path() == clipboardBrokerPkgPath
	return
}

func (inst *RuleCS015) run(pass *analysis.Pass) (res any, err error) {
	if pass.Pkg != nil && cs015Exempt(pass.Pkg.Path()) {
		return
	}
	const remedy = " — copy through clipboardbroker.Write with a Payload carrying the label and source, and show the record it returns"
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(file, func(n ast.Node) (cont bool) {
			cont = true
			switch v := n.(type) {
			case *ast.CallExpr:
				sel, ok := v.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Request" || len(v.Args) == 0 || !isClipboardWriteSubject(pass, v.Args[0]) {
					return
				}
				pass.Report(analysis.Diagnostic{
					Pos:     v.Pos(),
					End:     v.End(),
					Message: "CS015: raw Request on " + clipboardWriteSubject + " outside the clipboard broker" + remedy,
				})
			case *ast.Ident:
				if v.Name != clipboardWriteTextFunc || !isClipboardWriteText(pass.TypesInfo.Uses[v]) {
					return
				}
				pass.Report(analysis.Diagnostic{
					Pos:     v.Pos(),
					End:     v.End(),
					Message: "CS015: clipboardbroker." + clipboardWriteTextFunc + " is the unlabelled text path" + remedy,
				})
			}
			return
		})
	}
	return
}
