package env

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// lintAllowlist names files that may call os.Getenv / os.LookupEnv /
// syscall.Getenv directly. Paths are slash-separated and relative to
// the boxer module root.
//
// Only the env package itself is allowlisted in the scaffold. The
// migration PR will fold the surveyed call sites either by migration
// (the desired outcome) or by documented exception entries here.
var lintAllowlist = map[string]struct{}{
	"public/config/env/env.go":      {},
	"public/config/env/string.go":   {},
	"public/config/env/bool.go":     {},
	"public/config/env/int.go":      {},
	"public/config/env/duration.go": {},
	"public/config/env/path.go":     {},
}

// TestNoStrayOsGetenv enforces ADR-0009 §5. Skipped in the scaffold PR
// because the boxer module still has ~24 unmigrated reads; the
// migration PR un-skips this once the call sites are folded.
func TestNoStrayOsGetenv(t *testing.T) {
	t.Skip("ADR-0009 §5: full module walk activates with the migration PR")
	walkModuleForStrayGetenv(t, "../../..", lintAllowlist)
}

func walkModuleForStrayGetenv(t *testing.T, root string, allowlist map[string]struct{}) {
	t.Helper()
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) (next error) {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			base := d.Name()
			if base == "vendor" || base == "testdata" || strings.HasPrefix(base, ".") {
				next = fs.SkipDir
				return
			}
			return
		}
		if !strings.HasSuffix(path, ".go") {
			return
		}
		if strings.HasSuffix(path, ".out.go") || strings.HasSuffix(path, ".gen.go") {
			return
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr == nil {
			_, ok := allowlist[filepath.ToSlash(rel)]
			if ok {
				return
			}
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkgIdent, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			stray := (pkgIdent.Name == "os" && (sel.Sel.Name == "Getenv" || sel.Sel.Name == "LookupEnv")) ||
				(pkgIdent.Name == "syscall" && sel.Sel.Name == "Getenv")
			if stray {
				pos := fset.Position(call.Pos())
				t.Errorf("ADR-0009: stray %s.%s at %s; declare via public/config/env instead",
					pkgIdent.Name, sel.Sel.Name, pos)
			}
			return true
		})
		return
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
