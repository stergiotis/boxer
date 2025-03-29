package compiletime

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/rs/zerolog/log"
	"golang.org/x/tools/go/packages"

	"github.com/stergiotis/boxer/public/fffi/runtime"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

type IDLDriverGoFile struct {
	fset     *token.FileSet
	typesLU  map[ast.Expr]types.TypeAndValue
	idLU     map[*ast.FuncDecl]runtime.FuncProcId
	asts     []*ast.File
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
				err = eb.Build().Stringer("unresolved", s.Unresolved).Str("package", packagePattern).Str("buildTag", idlBuildTag).Str("file", file).Errorf("all identifiers need to be resolved")
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

func lexicalName(t fmt.Stringer) (lex string) {
	u := t.String()
	idx := strings.LastIndex(u, ".")
	idxB := strings.LastIndex(u, "]")
	if idxB < 0 {
		idxB = 0
	}
	if idx >= 0 {
		lex = u[:idxB] + u[idx+1:]
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
	case *types.Alias:
		return inst.resolveBasicTypeType(tt.Underlying(), lexicalName(tt))
	case *types.Interface:
		if tt.IsMethodSet() {
			if isErrorInterface(tt) {
				return "error", tt.String(), nil
			} else {
				err = eb.Build().Stringer("type", t).Errorf("interface types other than error are not implemented")
				return
			}
		} else {
			// assuming constraint in generic type
			switch tt.NumEmbeddeds() {
			case 1:
				return inst.resolveBasicTypeType(tt.EmbeddedType(0), castType)
			default:
				err = eb.Build().Stringer("type", t).Errorf("unable to handle interface with multiple embedded types: are you using more than one constraint type in generic type param?")
				return
			}
		}
	case *types.Array:
		var elem string
		elem, castType, err = inst.resolveBasicTypeType(tt.Elem(), castTypeP)
		if err != nil {
			err = eb.Build().
				Stringer("type", tt).
				Stringer("underlying", tt.Underlying()).
				Int64("len", tt.Len()).
				Errorf("error while resolving element type of array type: %w", err)
		}
		var pfx string
		if tt.Len() < 0 {
			pfx = "[]" + elem
		} else {
			pfx = fmt.Sprintf("[%d]", tt.Len())
		}
		if castType != "" && castTypeP == "" {
			castType = pfx + castType
		}
		typeName = pfx + elem
		return
	case *types.Slice:
		var elem string
		elem, castType, err = inst.resolveBasicTypeType(tt.Elem(), castTypeP)
		if err != nil {
			err = eb.Build().
				Stringer("type", tt).
				Stringer("underlying", tt.Underlying()).
				Errorf("error while resolving element type of array type: %w", err)
		}
		typeName = "[]" + elem
		if castType != "" && castTypeP == "" {
			castType = "[]" + castType
		}
		return
	case *types.Union:
		switch tt.Len() {
		case 1:
			term := tt.Term(0)
			if !term.Tilde() {
				log.Info().Str("term", term.String()).Msg("encountered union type that with tilde = false")
			}
			return inst.resolveBasicTypeType(term.Type(), castType)
		default:
			err = eb.Build().Str("str", tt.String()).Errorf("encountered unsupported kind of union type: len != 1")
		}
		return
	case *types.Pointer:
		err = eb.Build().Stringer("type", t).Errorf("unable to resolve type: pointers are not supported")
		return
	case *types.TypeParam:
		constraint := tt.Constraint().String()
		if constraint != "" && constraint[0] == '~' && !strings.Contains(constraint[1:], "~") { // FIXME remove this check (see union type and interface type)
			var u string
			castType, u, err = inst.resolveBasicTypeType(tt.Constraint(), "")
			if err != nil {
				err = eb.Build().Str("constraint", constraint).Errorf("unable to resolve type constraint in parametric/template type: %w", err)
				return
			}
			if u != "" {
				err = eb.Build().Str("constraint", constraint).Str("cast", u).Errorf("unable to resolve type constraint in parametric/template type: constraint needs cast")
				return
			}
			typeName = castType
			castType = ""
		} else {
			err = eb.Build().Str("type", tt.String()).Str("constraint", constraint).Errorf("unable to resolve parametric/templated type")
			return
		}
		return
	default:
		err = eb.Build().Stringer("type", t).Errorf("unable to resolve type")
		return
	}
}

func (inst *IDLDriverGoFile) ResolveBasicType(expr ast.Expr) (typeName string, castType string, err error) {
	lu := inst.typesLU
	u := lu[expr]
	return inst.resolveBasicTypeType(u.Type, "")
}

func (inst *IDLDriverGoFile) DriveBackend(generator CodeTransformerBackend, nothrow bool) (err error) {
	fset := inst.fset
	for _, a := range inst.asts {
		ast.Inspect(a, func(node ast.Node) bool {
			if node == nil || err != nil {
				return false
			}
			switch decl := node.(type) {
			case *ast.FuncDecl:
				err = generator.AddFunction(decl, inst, inst.FuncDeclToId(decl), nothrow)
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

func (inst *IDLDriverGoFile) DriveFrontend(generator CodeTransformerFrontend, nothrow bool) (err error) {
	l := len(inst.asts)
	for i, a := range inst.asts {
		err = generator.AddFile(inst.fset, a, inst, i, l, inst, nothrow)
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
	}
	id = runtime.FuncProcId(int(inst.idOffset) + len(inst.idLU))
	inst.idLU[decl] = id
	return id
}
