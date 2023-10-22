package compiletime

import (
	"fmt"
	"github.com/davecgh/go-spew/spew"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/fffi/runtime"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"go/ast"
	"go/token"
	"go/types"
	"golang.org/x/tools/go/packages"
	"strings"
)

type IDLDriverGoFile struct {
	fset     *token.FileSet
	asts     []*ast.File
	typesLU  map[ast.Expr]types.TypeAndValue
	idLU     map[*ast.FuncDecl]runtime.FuncProcId
	idOffset runtime.FuncProcId
}

var _ IDLDriver = (*IDLDriverGoFile)(nil)
var _ TypeResolver = (*IDLDriverGoFile)(nil)

func NewIDLDriverGoFile(idlBuildTag string, packagePattern string, idOffset runtime.FuncProcId) (inst *IDLDriverGoFile, err error) {
	fset := token.NewFileSet()
	loadConfig := &packages.Config{}
	loadConfig.Mode = packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo
	loadConfig.Fset = fset
	loadConfig.BuildFlags = []string{"-tags", idlBuildTag}
	var pkgs []*packages.Package
	pkgs, err = packages.Load(loadConfig, packagePattern)
	if err != nil {
		err = eb.Build().Str("package", packagePattern).Errorf("unable to parse go package: %w", err)
		return
	}

	if len(pkgs) != 1 {
		err = eb.Build().Str("package", packagePattern).Errorf("unable to parse go packages: more than one package")
		return
	}
	a := make([]*ast.File, 0, 4)
	for _, s := range pkgs[0].Syntax {
		var outerIdx, innerIdx int
		outerIdx, innerIdx, err = checkForBuildTag(s, idlBuildTag)
		if outerIdx >= 0 {
			file := fset.Position(s.FileStart).Filename
			log.Info().Str("file", file).Str("package", packagePattern).Str("buildTag", idlBuildTag).Msg("identified file containing idl code")
			s := s // FIXME
			s.Comments[outerIdx].List[innerIdx].Text = string(deactivated) + s.Comments[outerIdx].List[innerIdx].Text
			/*if s.Unresolved != nil && len(s.Unresolved) > 0 {
				err = eb.Build().Str("unresolved", spew.Sdump(s.Unresolved)).Str("package", packagePattern).Str("buildTag", idlBuildTag).Str("file", file).Errorf("all identifiers need to be resolved")
				return
			}*/
			a = append(a, s)
		}
	}
	if len(a) == 0 {
		err = eb.Build().Str("package", packagePattern).Str("buildTag", idlBuildTag).Errorf("unable to find idl go file in package")
		return
	}

	inst = &IDLDriverGoFile{
		fset:     fset,
		asts:     a,
		typesLU:  pkgs[0].TypesInfo.Types,
		idLU:     make(map[*ast.FuncDecl]runtime.FuncProcId, 1024),
		idOffset: idOffset,
	}
	return
}
func isErrorInterface(interf *types.Interface) bool {
	n := interf.NumMethods()
	for i := 0; i < n; i++ {
		m := interf.Method(i)
		if m.Name() == "Error" && m.Type().String() == "func() string" {
			return true
		}
	}
	return false
}
func lexicalName(t *types.Named) (lex string) {
	lex = t.String()
	idx := strings.LastIndex(lex, ".")
	if idx >= 0 {
		lex = lex[idx+1:]
	}
	return
}
func (inst *IDLDriverGoFile) resolveBasicTypeType(t types.Type, castTypeP string) (typeName string, castType string, err error) {
	castType = castTypeP
	switch tt := t.(type) {
	case *types.Basic:
		typeName = tt.Name()
		return
	case *types.Named:
		return inst.resolveBasicTypeType(tt.Underlying(), lexicalName(tt))
	case *types.Interface:
		if isErrorInterface(tt) {
			return "error", tt.String(), nil
		} else {
			err = eb.Build().Str("type", spew.Sdump(t)).Errorf("interface types other than error are not implemented")
			return
		}
	case *types.Array:
		var elem string
		elem, castType, err = inst.resolveBasicTypeType(tt.Elem(), castTypeP)
		if err != nil {
			err = eb.Build().
				Str("type", spew.Sdump(tt)).
				Str("underlying", spew.Sdump(tt.Underlying())).
				Int64("len", tt.Len()).
				Errorf("error while resolving element type of array type: %w", err)
		}
		if tt.Len() < 0 {
			typeName = "[]" + elem
		} else {
			typeName = fmt.Sprintf("[%d]%s", tt.Len(), elem)
		}
		return
	case *types.Slice:
		var elem string
		elem, castType, err = inst.resolveBasicTypeType(tt.Elem(), castTypeP)
		if err != nil {
			err = eb.Build().
				Str("type", spew.Sdump(tt)).
				Str("underlying", spew.Sdump(tt.Underlying())).
				Errorf("error while resolving element type of array type: %w", err)
		}
		typeName = "[]" + elem
		//err = eb.Build().Str("type", spew.Sdump(t)).Errorf("unable to resolve type: slices are not supported")
		return
	case *types.Pointer:
		err = eb.Build().Str("type", spew.Sdump(t)).Errorf("unable to resolve type: pointers are not supported")
		return
	default:
		err = eb.Build().Str("type", spew.Sdump(t)).Errorf("unable to resolve type")
		return
	}
}
func (inst *IDLDriverGoFile) ResolveBasicType(expr ast.Expr) (typeName string, castType string, err error) {
	lu := inst.typesLU
	u := lu[expr]
	return inst.resolveBasicTypeType(u.Type, "")
}

func (inst *IDLDriverGoFile) DriveBackend(generator CodeTransformerBackend) (err error) {
	fset := inst.fset
	for _, a := range inst.asts {
		ast.Inspect(a, func(node ast.Node) bool {
			if node == nil || err != nil {
				return false
			}
			switch decl := node.(type) {
			case *ast.FuncDecl:
				if decl.Recv == nil || decl.Recv.List == nil || len(decl.Recv.List) != 1 {
					err = eb.Build().Str("name", decl.Name.Name).Errorf("unable to generate code for function, must be method")
					return false
				}
				err = generator.AddFunction(decl, inst, inst.FuncDeclToId(decl))
				if err != nil {
					position := fset.Position(decl.Pos())
					err = eb.Build().Str("file", position.Filename).Int("line", position.Line).Str("name", decl.Name.Name).Errorf("unable to handle function declaration: %w", err)
					return false
				}
				break
			}
			return true
		})
	}
	return
}
func (inst *IDLDriverGoFile) DriveFrontend(generator CodeTransformerFrontend) (err error) {
	l := len(inst.asts)
	for i, a := range inst.asts {
		err = generator.AddFile(inst.fset, a, inst, i, l, inst)
		if err != nil {
			position := inst.fset.Position(a.Pos())
			err = eb.Build().Str("file", position.Filename).Int("line", position.Line).Errorf("unable to handle function declaration: %w", err)
			return
		}
	}
	return
}
func (inst *IDLDriverGoFile) FuncDeclToId(decl *ast.FuncDecl) runtime.FuncProcId {
	id, has := inst.idLU[decl]
	if has {
		return id
	} else {
		id = runtime.FuncProcId(int(inst.idOffset) + len(inst.idLU))
		inst.idLU[decl] = id
		return id
	}
}
