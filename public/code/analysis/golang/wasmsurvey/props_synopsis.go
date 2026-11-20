package wasmsurvey

import (
	"go/doc"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
)

// packageSynopsis returns the first sentence of the package doc comment found
// in dir, or "" when no non-test file carries one. Files are read in name
// order with doc.go first, so a package that spreads its doc over several
// files (which godoc concatenates) reports the conventional one. Only the
// package clause is parsed; build constraints are not evaluated, which is
// right for a doc comment — godoc does not evaluate them either.
func packageSynopsis(dir string) (synopsis string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if (names[i] == "doc.go") != (names[j] == "doc.go") {
			return names[i] == "doc.go"
		}
		return names[i] < names[j]
	})
	fset := token.NewFileSet()
	for _, n := range names {
		f, e := parser.ParseFile(fset, dir+string(os.PathSeparator)+n, nil, parser.PackageClauseOnly|parser.ParseComments)
		if e != nil || f.Doc == nil {
			continue
		}
		if s := strings.TrimSpace(new(doc.Package).Synopsis(f.Doc.Text())); s != "" {
			return s
		}
	}
	return ""
}
