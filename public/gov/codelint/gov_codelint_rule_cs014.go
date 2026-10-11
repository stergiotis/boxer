package codelint

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// The clipboard opcode's one builder (ADR-0026 Update 2026-05-30;
// ADR-0310 §SD11, towards ADR-0317): every copy to the viewport clipboard goes
// through the clipboard broker, whose request leg the bus audits, and the
// host's drain is the one place the egui opcode is built from what the
// broker queued. A widget or an app that builds the opcode itself copies
// without a record — whether it calls the function, stores it in a field
// or a variable, or reaches it through a dot import: any use of the
// binding is a use.
const (
	bindingsPkgPath        = "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	clipboardBrokerPkgPath = "github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	hostbootPkgPath        = "github.com/stergiotis/boxer/public/keelson/runtime/hostboot"
	clipboardOpcodeFunc    = "CopyTextToClipboard"
)

// RuleCS014 — the egui2 CopyTextToClipboard opcode used outside the
// clipboard broker and the host's drain. The broker package, the host's
// package that drains it and the bindings themselves are exempt; test
// files are exempt, as they are not the shipped runtime surface.
type RuleCS014 struct{}

func NewRuleCS014() (inst *RuleCS014) {
	inst = &RuleCS014{}
	return
}

func (inst *RuleCS014) Id() (id string) {
	id = "CS014"
	return
}

func (inst *RuleCS014) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityError
	return
}

func (inst *RuleCS014) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs014",
		Doc:  "CS014: copy to the clipboard through the clipboard broker, not the egui opcode",
		Run:  inst.run,
	}
	return
}

// cs014Exempt reports whether a package path is one of the three the rule
// leaves alone — the broker, the host package that drains it and the
// bindings themselves — matched exactly: a subpackage of any of them is
// not exempt.
func cs014Exempt(path string) bool {
	return path == clipboardBrokerPkgPath || path == hostbootPkgPath || path == bindingsPkgPath
}

// isClipboardOpcode reports whether obj is the bindings' opcode builder.
func isClipboardOpcode(obj types.Object) (is bool) {
	fn, ok := obj.(*types.Func)
	if !ok || fn.Name() != clipboardOpcodeFunc || fn.Pkg() == nil {
		return
	}
	is = fn.Pkg().Path() == bindingsPkgPath
	return
}

func (inst *RuleCS014) run(pass *analysis.Pass) (res any, err error) {
	if pass.Pkg != nil && cs014Exempt(pass.Pkg.Path()) {
		return
	}
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(file, func(n ast.Node) (cont bool) {
			cont = true
			id, ok := n.(*ast.Ident)
			if !ok || id.Name != clipboardOpcodeFunc || !isClipboardOpcode(pass.TypesInfo.Uses[id]) {
				return
			}
			pass.Report(analysis.Diagnostic{
				Pos:     id.Pos(),
				End:     id.End(),
				Message: "CS014: " + clipboardOpcodeFunc + " outside the clipboard broker and the host's drain — request clipboard.write on the bus, which the audit records",
			})
			return
		})
	}
	return
}
