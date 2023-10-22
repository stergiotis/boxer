package compiletime

import (
	"github.com/stergiotis/boxer/public/fffi/runtime"
	"go/ast"
	"go/token"
	"io"
)

type Emitter interface {
	Emit(out io.Writer) (n int, err error)
}

// CodeTransformerBackend generate code for the backend i.e. the library executing the foreign function body (in the foreign programming language)
type CodeTransformerBackend interface {
	AddFunction(decl *ast.FuncDecl, resolver TypeResolver, id runtime.FuncProcId) (err error)
	Reset()
	Emitter
}

// CodeTransformerFrontend generate code for the frontend i.e. the library invoking the foreign functions
type CodeTransformerFrontend interface {
	AddFile(fset *token.FileSet, file *ast.File, resolver TypeResolver, i int, nFiles int, idResolver IdResolver) (err error)
	Reset()
	Emitter
}

// IDLDriver convert interface description language to go ast.FuncDecl ast nodes
type IDLDriver interface {
	DriveBackend(generator CodeTransformerBackend) (err error)
	DriveFrontend(generator CodeTransformerFrontend) (err error)
}
type TypeResolver interface {
	ResolveBasicType(expr ast.Expr) (typeName string, castType string, err error)
}
type IdResolver interface {
	FuncDeclToId(decl *ast.FuncDecl) runtime.FuncProcId
}
