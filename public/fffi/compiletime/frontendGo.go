package compiletime

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"go/ast"
	"go/format"
	"go/printer"
	"go/token"
	"golang.org/x/tools/go/ast/astutil"
	"io"
	"strings"
)

type CodeTransformerFrontendGo struct {
	fset                   *token.FileSet
	file                   *ast.File
	builder                *bytes.Buffer
	builderSmall           *bytes.Buffer
	namer                  *Namer
	printerCfg             *printer.Config
	needsAdditionalImports bool
}

func NewCodeTransformerFrontendGo(namer *Namer) *CodeTransformerFrontendGo {
	builder := &bytes.Buffer{}
	cfg := &printer.Config{
		Mode:     printer.UseSpaces, //| printer.SourcePos,
		Tabwidth: 0,
		Indent:   0,
	}
	return &CodeTransformerFrontendGo{
		fset:         nil,
		file:         nil,
		builder:      builder,
		builderSmall: &bytes.Buffer{},
		namer:        namer,
		printerCfg:   cfg,
	}
}

func (inst *CodeTransformerFrontendGo) AddFile(fset *token.FileSet, file *ast.File, resolver TypeResolver, i int, nFiles int, idResolver IdResolver) (err error) {
	var file2 *ast.File
	file2 = file
	inst.fset = fset
	inst.file = astutil.Apply(file, nil, func(c *astutil.Cursor) bool {
		n := c.Node()
		switch nt := n.(type) {
		case *ast.FuncDecl:
			var lst []ast.Stmt
			var extraComments []*ast.Comment
			lst, extraComments, err = inst.generate(nt, uint32(idResolver.FuncDeclToId(nt)), resolver)
			if err != nil {
				err = eh.Errorf("unable to generate code: %w", err)
				return false
			}
			nt.Body.List = lst
			if extraComments != nil {
				if nt.Doc == nil {
					nt.Doc = &ast.CommentGroup{List: nil}
				}
				if nt.Doc.List == nil {
					nt.Doc.List = make([]*ast.Comment, 0, 1)
				}
				nt.Doc.List = append(nt.Doc.List, extraComments...)
			}
			c.Replace(nt)
			log.Info().Str("func", nt.Name.Name).Msg("replacing function")
			break
		}

		return true
	}).(*ast.File)
	if err != nil {
		return
	}

	err = format.Node(inst.builder, fset, file2)
	//err = inst.printerCfg.Fprint(inst.builder, fset, file2)
	if err != nil {
		err = eh.Errorf("unable to print ast (to source code): %w", err)
		return
	}
	return
}
func (inst *CodeTransformerFrontendGo) generate(decl *ast.FuncDecl, id uint32, resolver TypeResolver) (r []ast.Stmt, extraComments []*ast.Comment, err error) {
	var prolog, epilog []ast.Stmt
	var foreignCode string
	prolog, foreignCode, epilog, err = splitIdlBody(decl.Body.List)

	isFunction := decl.Type.Results != nil && len(decl.Type.Results.List) > 0
	b := inst.builderSmall
	b.Reset()

	if decl.Recv == nil || decl.Recv.List == nil || len(decl.Recv.List) != 1 {
		err = eb.Build().Str("name", decl.Name.Name).Errorf("unable to generate code for function, must be method")
		return
	}
	recv0 := decl.Recv.List[0]
	instvar := recv0.Names[0].Name
	{
		_, _ = b.WriteString("f := ")
		_, _ = b.WriteString(instvar)
		_, _ = b.WriteString(".getFffi()\n")
		if isFunction {
			_, _ = b.WriteString("f.AddFunctionId(")
		} else {
			_, _ = b.WriteString("f.AddProcedureId(")
		}
		_, _ = b.WriteString(fmt.Sprintf("0x%08x", id))
		_, _ = b.WriteString(")\n")
	}
	var paramNames, paramGoTypes, resultNames, resultGoTypes, resultGoCastTypes, paramDeref, resultDeref []string
	var explicitErrVarName string
	paramNames, paramGoTypes, _, paramDeref, resultNames, resultGoTypes, resultGoCastTypes, resultDeref, explicitErrVarName, err = getParamsAndResultTypes(decl, resolver)
	if err != nil {
		err = eb.Build().Str("name", decl.Name.Name).Errorf("unable to get params and results types: %w", err)
		return
	}
	{
		for i, name := range paramNames {
			var suffix string
			suffix, err = inst.namer.GoTypeNameToSendRecvFuncNameSuffix(paramGoTypes[i])
			if err != nil {
				err = eb.Build().Str("name", name).Str("goType", paramGoTypes[i]).Errorf("unable to determine send/recv function name suffix from go type: %w", err)
				return
			}
			_, _ = b.WriteString("runtime.Add")
			_, _ = b.WriteString(suffix)
			_, _ = b.WriteString("Arg")
			_, _ = b.WriteString("(f,")
			_, _ = b.WriteString(paramDeref[i])
			_, _ = b.WriteString(name)
			_, _ = b.WriteString(")\n")
		}
	}

	if isFunction {
		if explicitErrVarName != "" {
			_, _ = b.WriteString(explicitErrVarName)
			_, _ = b.WriteString(" = f.CallFunction()\nif ")
			_, _ = b.WriteString(explicitErrVarName)
			_, _ = b.WriteString(" != nil {\n")
			_, _ = b.WriteString("  return\n")
			_, _ = b.WriteString("}\n")
		} else {
			_, _ = b.WriteString("_err_ := f.CallFunction()\n")
			_, _ = b.WriteString("if _err_ != nil {\n")
			_, _ = b.WriteString("  ")
			_, _ = b.WriteString(instvar)
			_, _ = b.WriteString("  .handleError(_err_)\n")
			_, _ = b.WriteString("  return\n")
			_, _ = b.WriteString("}\n")
		}
	} else {
		_, _ = b.WriteString("f.CallProcedure()\n")
	}

	{
		for i, name := range resultNames {
			var funcNameSuffix string
			funcNameSuffix, err = inst.namer.GoTypeNameToSendRecvFuncNameSuffix(resultGoTypes[i])
			if err != nil {
				err = eb.Build().Str("goType", resultGoTypes[i]).Errorf("unable to compose send result function call: %w", err)
				return
			}

			_, _ = b.WriteString(resultDeref[i])
			_, _ = b.WriteString(name)
			_, _ = b.WriteString(" = ")
			cast := resultGoCastTypes[i]
			if cast != "" {
				_, _ = b.WriteString(cast)
				_, _ = b.WriteString("(")
			}
			_, _ = b.WriteString("f.Get")
			_, _ = b.WriteString(funcNameSuffix)
			_, _ = b.WriteString("Retr()")
			if cast != "" {
				_, _ = b.WriteString(")")
			}
			_, _ = b.WriteString("\n")
		}
	}

	extraComments = []*ast.Comment{{Text: "//foreign code:\n//  " + strings.ReplaceAll(foreignCode, "\n", "\n//  ")}}
	code := b.String()
	r, err = parseFunctionBodyCode(code)
	if err != nil {
		err = eb.Build().Str("code", code).Errorf("unable to parse generated code: %w", err)
		return
	}
	r = append(append(prolog, r...), epilog...)
	return
}

const additionalImports = `
`

const defaultImports = `
import "github.com/stergiotis/boxer/public/fffi/runtime"
`

func (inst *CodeTransformerFrontendGo) Emit(out io.Writer) (n int, err error) {
	var n2 int
	n2, err = out.Write([]byte(`// Code generated by fffi generator; DO NOT EDIT.
`))
	n += n2
	if err != nil {
		err = eh.Errorf("unable to write prolog to output: %w", err)
		return
	}

	scanner := bufio.NewScanner(inst.builder)
	scanner.Split(bufio.ScanLines)
	i := 0
	emitImports := false
	for scanner.Scan() {
		t := scanner.Text()
		t = strings.TrimPrefix(t, " \t")
		if strings.HasPrefix(t, "package ") {
			i++
			if i == 1 {
				emitImports = true
			} else {
				n2, err = out.Write(deactivated)
				n += n2
				if err != nil {
					err = eh.Errorf("unable to write code line to output: %w", err)
					return
				}
			}
		}
		n2, err = out.Write(scanner.Bytes())
		n += n2
		if err != nil {
			err = eh.Errorf("unable to write code line to output: %w", err)
			return
		}
		n2, err = out.Write([]byte{'\n'})
		n += n2
		if err != nil {
			err = eh.Errorf("unable to write line feed to output: %w", err)
			return
		}
		if emitImports {
			n2, err = out.Write([]byte(defaultImports))
			n += n2
			if err != nil {
				err = eh.Errorf("unable to write additional imports to output: %w", err)
				return
			}
		}
		if emitImports && inst.needsAdditionalImports {
			n2, err = out.Write([]byte(additionalImports))
			n += n2
			if err != nil {
				err = eh.Errorf("unable to write additional imports to output: %w", err)
				return
			}
		}
		emitImports = false
	}
	err = scanner.Err()
	if err != nil && err != io.EOF {
		err = eh.Errorf("unable to scan source code into lines: %w", err)
		return
	}
	return
}

func (inst *CodeTransformerFrontendGo) Reset() {
	inst.file = nil
	inst.fset = nil
	inst.builder.Reset()
	inst.needsAdditionalImports = false
}

func (inst *CodeTransformerFrontendGo) NextFuncProcIdOffset() uint32 {
	//TODO implement me
	panic("implement me")
}

var _ CodeTransformerFrontend = (*CodeTransformerFrontendGo)(nil)
