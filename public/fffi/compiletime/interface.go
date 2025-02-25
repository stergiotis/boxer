package compiletime

import (
	"go/ast"
	"go/token"
	"io"

	"github.com/stergiotis/boxer/public/fffi/runtime"
)

type Emitter interface {
	Emit(out io.Writer, preamble []byte) (n int, err error)
}

// CodeTransformerBackend generate code for the backend i.e. the library executing the foreign function body (in the foreign programming language)
type CodeTransformerBackend interface {
	AddFunction(decl *ast.FuncDecl, resolver TypeResolver, id runtime.FuncProcId, nothrow bool) (err error)
	Reset()
	Emitter
}

// CodeTransformerFrontend generate code for the frontend i.e. the library invoking the foreign functions
type CodeTransformerFrontend interface {
	AddFile(fset *token.FileSet, file *ast.File, resolver TypeResolver, i int, nFiles int, idResolver IdResolver, nothrow bool) (err error)
	Reset()
	Emitter
}

// IDLDriver convert interface description language to go ast.FuncDecl ast nodes
type IDLDriver interface {
	DriveBackend(generator CodeTransformerBackend, nothrow bool) (err error)
	DriveFrontend(generator CodeTransformerFrontend, nothrow bool) (err error)
}

type TypeResolver interface {
	ResolveBasicType(expr ast.Expr) (typeName string, castType string, err error)
}

type IdResolver interface {
	FuncDeclToId(decl *ast.FuncDecl) runtime.FuncProcId
}
