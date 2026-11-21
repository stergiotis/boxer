package codelint

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// loopbackPkgPath is the one package allowed to bind a TCP listener directly:
// its Gate is the bind-address check every boxer listener goes through
// (ADR-0082 §SD1, derived practice).
const loopbackPkgPath = "github.com/stergiotis/boxer/public/keelson/runtime/loopback"

// listenBannedFuncs are the standard-library entry points that bind a TCP
// listener without asking whether its address leaves this machine, keyed by
// types.Func.FullName.
var listenBannedFuncs = map[string]struct{}{
	"net.Listen":                           {},
	"net.ListenTCP":                        {},
	"(*net.ListenConfig).Listen":           {},
	"net/http.ListenAndServe":              {},
	"net/http.ListenAndServeTLS":           {},
	"(*net/http.Server).ListenAndServe":    {},
	"(*net/http.Server).ListenAndServeTLS": {},
}

// RuleCS018 — a listener bound outside package loopback.
//
// A listener bound with net.Listen or http.ListenAndServe takes whatever
// address it is handed, and ":port" binds every interface. loopback.Gate
// refuses a non-loopback bind unless the operator sets the listener's
// opt-in, the listener declares whom it serves and why (Exposure), or it
// authenticates its peers by TLS client certificate (PeerTLS); it returns
// a failed bind instead of letting it be logged and dropped. Test files are exempt; a listener that must bind directly (a
// Unix socket, say) carries a `//boxer:lint disable=CS018 reason="…"`
// directive saying why.
type RuleCS018 struct{}

func NewRuleCS018() (inst *RuleCS018) {
	inst = &RuleCS018{}
	return
}

func (inst *RuleCS018) Id() (id string) {
	id = "CS018"
	return
}

func (inst *RuleCS018) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityError
	return
}

func (inst *RuleCS018) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs018",
		Doc:  "CS018: bind listeners through loopback.Gate, not net.Listen or http.ListenAndServe",
		Run:  inst.run,
	}
	return
}

func (inst *RuleCS018) run(pass *analysis.Pass) (res any, err error) {
	if pass.Pkg != nil && pass.Pkg.Path() == loopbackPkgPath {
		return
	}
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(file, func(n ast.Node) (cont bool) {
			cont = true
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return
			}
			fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
			if !ok {
				return
			}
			name := fn.FullName()
			if _, banned := listenBannedFuncs[name]; !banned {
				return
			}
			pass.Report(analysis.Diagnostic{
				Pos:     call.Pos(),
				End:     call.End(),
				Message: "CS018: " + name + " outside public/keelson/runtime/loopback — bind through loopback.Gate: a non-loopback address then needs an operator opt-in, an Exposure declaration (whom it serves and why) or a PeerTLS config that requires client certificates",
			})
			return
		})
	}
	return
}
