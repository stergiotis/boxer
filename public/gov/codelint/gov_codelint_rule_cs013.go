package codelint

import (
	"go/ast"
	"go/constant"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// ebPkgPath is the structural-error builder. Its Errorf is the sanctioned
// destination for the context this rule moves out of format strings.
const ebPkgPath = ehPkgPath + "/eb"

// ehFormatEntryPoints maps an eh package-level error constructor to the index
// of its format parameter. eb's Errorf is a method and is matched separately,
// by receiver type, so a builder held in a variable is covered too.
var ehFormatEntryPoints = map[string]int{
	"Errorf":                     0,
	"ErrorfWithData":             1,
	"ErrorfWithDataWithoutStack": 1,
}

// RuleCS013 — a format directive other than %w in an eh / eb error message.
//
// This is ADR-0011's phase-2 "always-%w for error args" rule.
//
// CODINGSTANDARDS.md "Error Handling → Error Construction" splits the two jobs
// an error message used to do: the format string wraps (`%w`, nothing else) and
// the eb builder carries the context (`Str`, `Int`, …). A `%q` or `%d` in the
// message flattens a value into prose, where nothing downstream can read it
// back — eh's CBOR payload, the zerolog field set, and every query over
// persisted errors see one opaque string.
//
// Not flagged, deliberately:
//
//   - A format string with no directives at all. `eb.Build()…Errorf("msg")` is
//     the sanctioned root-error form (eb has no New), so a bare message cannot
//     be a finding here. Whether `eh.Errorf("msg")` should be `eh.New("msg")`
//     is a separate claim about a different function.
//   - Two or more `%w`. Joining errors preserves both, which is what the rule
//     is protecting.
//   - A non-constant format string. Undecidable statically; `go vet`'s printf
//     analyzer already treats eh.Errorf and (*eb.ErrorBuilder).Errorf as
//     wrappers and reports what it can about them.
//
// eh and its subpackages are exempt: their implementations forward a caller's
// format string, and eh.New is spelled Errorf("%s", msg).
//
// The rule's premise — that a consumer can read the structured payload — fails
// for an error whose message crosses a text-only boundary: an HTTP response
// body, a CLI line, anything the far side sees as a string. Moving the value
// off the message deletes it from what that consumer can see. Those sites keep
// the directive and carry a per-line
//
//	//boxer:lint disable=CS013 reason="<which boundary the message crosses>"
//
// naming the boundary, not merely asserting the message matters. A test
// asserting the value appears in Error() is the usual evidence that a site is
// one of these.
//
// A finding carries a SuggestedFix when the rewrite needs no human judgment.
// suggestCS013Fix decides that, and the reasons it declines are the useful
// half — see gov_codelint_rule_cs013_fix.go.
type RuleCS013 struct{}

func NewRuleCS013() (inst *RuleCS013) {
	inst = &RuleCS013{}
	return
}

func (inst *RuleCS013) Id() (id string) {
	id = "CS013"
	return
}

// DefaultSeverity is error. The rule shipped at warn under ADR-0011's staging
// policy — measure the backlog without breaking the build — and promotes here
// because that policy's condition is met: the residual count is zero across
// public/ and apps/.
//
// A new finding is therefore a build failure, which is the point. The sites
// that legitimately keep a directive carry a per-line disable naming the
// reason, and the reasons that recurred are worth knowing before adding
// another: a message that crosses a text-only boundary, a directive filling a
// grammatical slot rather than a data slot, a remedy the reader copies, and a
// message another component string-matches.
func (inst *RuleCS013) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityError
	return
}

func (inst *RuleCS013) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs013",
		Doc:  "CS013: eh / eb error format strings carry %w only; context belongs in eb.Build() fields",
		Run:  inst.run,
	}
	return
}

func (inst *RuleCS013) run(pass *analysis.Pass) (res any, err error) {
	if pass.Pkg != nil {
		path := pass.Pkg.Path()
		if path == ehPkgPath || strings.HasPrefix(path, ehPkgPath+"/") {
			return
		}
	}
	for _, file := range pass.Files {
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
			fn, _ := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
			if fn == nil || fn.Pkg() == nil {
				return
			}
			fmtIdx, kind := classifyErrorConstructor(fn)
			if kind == "" {
				return
			}
			if fmtIdx >= len(call.Args) {
				return
			}
			format, ok := constantString(pass, call.Args[fmtIdx])
			if !ok {
				return
			}
			verbs := nonWrapVerbs(format)
			if len(verbs) == 0 {
				return
			}
			d := analysis.Diagnostic{
				Pos: call.Pos(),
				End: call.End(),
				Message: "CS013: " + kind + " format carries " + strings.Join(verbs, ", ") +
					" — wrap with \"%w\" only and move the values to eb.Build().Str(…)/Int(…)",
			}
			fix, fixable, decline := suggestCS013Fix(pass, call, sel, kind, fmtIdx, format)
			if fixable {
				d.SuggestedFixes = []analysis.SuggestedFix{fix}
			} else if decline != "" {
				d.Message += " [" + decline + "]"
			}
			pass.Report(d)
			return
		})
	}
	return
}

// classifyErrorConstructor reports the format-parameter index and a
// human-readable spelling for the eh / eb error constructors this rule covers,
// or an empty kind for anything else.
func classifyErrorConstructor(fn *types.Func) (fmtIdx int, kind string) {
	path := fn.Pkg().Path()
	sig, _ := fn.Type().(*types.Signature)
	if sig != nil && sig.Recv() != nil {
		// (*eb.ErrorBuilder).Errorf — matched by receiver so that a builder
		// held in a variable (inst.Errorf, bld.Errorf) is covered too.
		if path == ebPkgPath && fn.Name() == "Errorf" {
			fmtIdx, kind = 0, "eb.Build()…Errorf"
		}
		return
	}
	if path != ehPkgPath {
		return
	}
	idx, found := ehFormatEntryPoints[fn.Name()]
	if !found {
		return
	}
	fmtIdx, kind = idx, "eh."+fn.Name()
	return
}

// constantString resolves an expression to its string value when the type
// checker can fold it to a constant, so a format built from string constants
// is judged like a literal one.
func constantString(pass *analysis.Pass, e ast.Expr) (s string, ok bool) {
	tv, found := pass.TypesInfo.Types[e]
	if !found || tv.Value == nil || tv.Value.Kind() != constant.String {
		return
	}
	s = constant.StringVal(tv.Value)
	ok = true
	return
}

// nonWrapVerbs returns the printf verbs in format other than w, in order of
// appearance and without repeats, each spelled as it would be written ("%d").
// A doubled %% is an escape and carries no verb. Malformed directives are
// skipped: go vet's printf analyzer is the authority on those.
func nonWrapVerbs(format string) (verbs []string) {
	seen := make(map[rune]struct{}, 4)
	rs := []rune(format)
	for i := 0; i < len(rs); i++ {
		if rs[i] != '%' {
			continue
		}
		i++
		if i >= len(rs) {
			break
		}
		if rs[i] == '%' {
			continue
		}
		// Step over flags, argument index, width and precision. None of them
		// can be a letter, so the first rune that is not one of these is the
		// verb.
		for i < len(rs) && strings.ContainsRune("+-# 0123456789.*[]", rs[i]) {
			i++
		}
		if i >= len(rs) {
			break
		}
		v := rs[i]
		if v == 'w' || !isVerbRune(v) {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		verbs = append(verbs, "%"+string(v))
	}
	return
}

func isVerbRune(r rune) (ok bool) {
	ok = (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
	return
}
