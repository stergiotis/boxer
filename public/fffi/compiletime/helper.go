package compiletime

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"strconv"
	"strings"

	"github.com/davecgh/go-spew/spew"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

func emitBuilderWithProlog(out io.Writer, builder io.WriterTo, prolog string) (n int, err error) {
	var n0 int
	n0, err = out.Write([]byte(prolog))
	n = n0
	if err != nil {
		err = eh.Errorf("unable to write to output: %w", err)
		return
	}

	var n1 int64
	n1, err = builder.WriteTo(out)
	n += int(n1)
	if err != nil {
		err = eh.Errorf("unable to write to output: %w", err)
		return
	}
	return
}

func splitIdlBody(body []ast.Stmt) (prolog []ast.Stmt, foreignCode string, epilog []ast.Stmt, err error) {
	for i, s := range body {
		var a *ast.AssignStmt
		var ok bool
		a, ok = s.(*ast.AssignStmt)
		if !ok {
			continue
		}
		if a.Lhs == nil || len(a.Lhs) != 1 || a.Rhs == nil || len(a.Rhs) != 1 {
			continue
		}
		var lhsId *ast.Ident
		lhsId, ok = a.Lhs[0].(*ast.Ident)
		if !ok {
			continue
		}
		if lhsId.Name != "_" {
			continue
		}
		var rhsLit *ast.BasicLit
		rhsLit, ok = a.Rhs[0].(*ast.BasicLit)
		if !ok {
			continue
		}
		if rhsLit.Kind != token.STRING {
			continue
		}
		foreignCode, err = strconv.Unquote(rhsLit.Value)
		if err != nil {
			err = eb.Build().Str("literal", rhsLit.Value).Errorf("unable to unquote string literal: %w", err)
			return
		}

		if i == 0 {
			prolog = make([]ast.Stmt, 0, 0)
		} else {
			prolog = append(make([]ast.Stmt, 0, i-1), body[:i]...)
		}
		l := len(body)
		if i == l-1 {
			epilog = make([]ast.Stmt, 0, 0)
		} else {
			epilog = append(make([]ast.Stmt, 0, len(body)-1), body[i+1:]...)
		}
		return
	}
	err = eb.Build().Str("dump", spew.Sdump(body)).Errorf("unable to find foreign code in idl go ast: must have form _ = `r = foreignFunc(a,b,c)`")
	return
}

func parseFunctionBodyCode(code string) (res []ast.Stmt, err error) {
	var f *ast.File
	fset := token.NewFileSet()
	code = "package dummy\nfunc dummy() {\n" + code + "\n}\n"
	f, err = parser.ParseFile(fset, "", code, parser.SkipObjectResolution)
	if err != nil {
		err = eb.Build().Str("code", code).Errorf("unable to parse code: %w", err)
		return
	}

	ast.Inspect(f, func(node ast.Node) bool {
		nt, ok := node.(*ast.FuncDecl)
		if ok {
			res = nt.Body.List
			return false
		}
		return true
	})

	if res == nil {
		err = eb.Build().Str("code", code).Errorf("unable to find ast.FuncDecl node in generated code")
		return
	}
	return
}

func isMethodDeclaration(decl *ast.FuncDecl) bool {
	return decl != nil && decl.Recv != nil && decl.Recv.List != nil && len(decl.Recv.List) == 1
}

func sendReceiverAsArg(field *ast.Field) (send bool, err error) {
	if field.Names == nil {
		err = eh.Errorf("unnamed function receivers are not supported")
		return
	}
	send = field.Names[0].Name == "foreignptr"
	return
}

func getParamsAndResultTypes(decl *ast.FuncDecl, resolver TypeResolver) (paramNames []string, paramGoTypes []string, paramGoCast []bool, paramDeref []string, resultNames []string, resultGoTypes []string, resultGoCastTypes []string, resultDeref []string, explicitErrVarName string, err error) {
	t := decl.Type
	sendReceiver := false
	if isMethodDeclaration(decl) {
		sendReceiver, err = sendReceiverAsArg(decl.Recv.List[0])
		if err != nil {
			err = eb.Build().Str("decl", spew.Sdump(decl)).Errorf("error while handling receiver variable: %w", err)
			return
		}
	}
	hasRegularParams := t.Params != nil && t.Params.List != nil && len(t.Params.List) > 0
	if hasRegularParams || sendReceiver {
		l := 0
		if sendReceiver {
			l++
		}
		if hasRegularParams {
			l += len(t.Params.List)
		}
		paramNames = make([]string, 0, l)
		paramGoTypes = make([]string, 0, l)
		paramGoCast = make([]bool, 0, l)
		paramDeref = make([]string, 0, l)
		var ps []*ast.Field
		if sendReceiver {
			ps = make([]*ast.Field, 0, len(t.Params.List)+1)
			ps = append(ps, decl.Recv.List[0])
		}
		if hasRegularParams {
			ps = append(ps, t.Params.List...)
		}
		for _, f := range ps {
			ns := f.Names
			if ns == nil {
				err = eh.New("unnamed params are not supported")
				return
			}
			if f.Type == nil {
				err = eh.New("unnamed params are not supported")
				return
			}
			var basicType string
			var castType string
			basicType, castType, err = resolver.ResolveBasicType(f.Type)
			if err != nil {
				err = eb.Build().Str("type", spew.Sdump(f.Type)).Errorf("unable to resolve basic type: %w", err)
				return
			}

			paramNames = append(paramNames, ns[0].Name)
			paramGoTypes = append(paramGoTypes, basicType)
			paramGoCast = append(paramGoCast, castType != "")
			paramDeref = append(paramDeref, "")
		}
	} else {
		paramNames = make([]string, 0, 0)
		paramGoTypes = make([]string, 0, 0)
		paramGoCast = make([]bool, 0, 0)
		paramDeref = append(paramDeref, "")
	}

	if t.Results != nil && t.Results.List != nil {
		l := len(t.Results.List)
		resultNames = make([]string, 0, l)
		resultGoTypes = make([]string, 0, l)
		resultGoCastTypes = make([]string, 0, l)
		resultDeref = make([]string, 0, l)
		for _, f := range t.Results.List {
			ns := f.Names
			if ns == nil {
				err = eh.New("unnamed results are not supported")
				return
			}
			if f.Type == nil {
				err = eh.New("unnamed results are not supported")
				return
			}
			var basicType string
			var castType string
			basicType, castType, err = resolver.ResolveBasicType(f.Type)
			if err != nil {
				err = eb.Build().Str("type", spew.Sdump(f.Type)).Errorf("unable to resolve basic type: %w", err)
				return
			}

			resultNames = append(resultNames, ns[0].Name)
			resultGoTypes = append(resultGoTypes, basicType)
			resultGoCastTypes = append(resultGoCastTypes, castType)
			resultDeref = append(resultDeref, "")
		}
	} else {
		resultNames = make([]string, 0, 0)
		resultGoTypes = make([]string, 0, 0)
		resultGoCastTypes = make([]string, 0, 0)
		resultDeref = make([]string, 0, 0)
	}
	explicitError := len(resultGoTypes) > 0 && resultGoTypes[len(resultGoTypes)-1] == "error"
	if explicitError {
		explicitErrVarName = resultNames[len(resultNames)-1]
		resultNames = resultNames[:len(resultNames)-1]
		resultGoTypes = resultGoTypes[:len(resultGoTypes)-1]
		resultGoCastTypes = resultGoCastTypes[:len(resultGoCastTypes)-1]
		resultDeref = resultDeref[:len(resultDeref)-1]
	}
	return
}

func checkForBuildTag(a *ast.File, tag string) (outerIndex int, innerIndex int, err error) {
	outerIndex = -1
	innerIndex = -1
	if a.Comments == nil {
		return
	}
	for i, cg := range a.Comments {
		if cg.List != nil && len(cg.List) > 0 {
			for j, c := range cg.List {
				t := strings.Trim(c.Text, " \t\n")
				if strings.HasPrefix(t, "//go:build") {
					var res []ast.Stmt
					res, err = parseFunctionBodyCode(strings.TrimPrefix(t, "//go:build"))
					if err != nil {
						err = eb.Build().Str("comment", t).Errorf("unable to parse build tag: %w", err)
						return
					}
					for _, s := range res {
						ast.Inspect(s, func(node ast.Node) bool {
							id, ok := node.(*ast.Ident)
							if ok {
								if id.Name == tag {
									outerIndex = i
									innerIndex = j
									return false
								}
							}
							return true
						})
					}
				}
			}
		}
	}
	return
}
