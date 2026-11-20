package packageprops

import (
	"bufio"
	"bytes"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// minImportersForPackageDoc is the in-module importer count from which a
// package must carry a package doc comment: a package that many others
// depend on is one a reader meets through pkg.go.dev before its source.
const minImportersForPackageDoc = 20

// TestWidelyImportedPackagesHaveAPackageDoc fails for a package of this
// module that at least minImportersForPackageDoc other packages of the
// module import, and that has no package doc comment in any of its non-test
// files. The count is of distinct importing packages, test files excluded.
func TestWidelyImportedPackagesHaveAPackageDoc(t *testing.T) {
	root := moduleRoot(t)
	modulePath := readModulePath(t, filepath.Join(root, "go.mod"))

	importers := make(map[string]map[string]struct{}) // import path -> importing dirs
	hasDoc := make(map[string]bool)                     // import path -> has a package doc
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
				name == "testdata" || name == "vendor" || name == "node_modules" || name == "target") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly|parser.ParseComments)
		if parseErr != nil {
			return nil
		}
		dir := filepath.Dir(path)
		rel, relErr := filepath.Rel(root, dir)
		if relErr != nil {
			return relErr
		}
		self := modulePath
		if rel != "." {
			self += "/" + filepath.ToSlash(rel)
		}
		if file.Doc != nil && strings.TrimSpace(file.Doc.Text()) != "" {
			hasDoc[self] = true
		} else if _, seen := hasDoc[self]; !seen {
			hasDoc[self] = false
		}
		for _, spec := range file.Imports {
			p, uqErr := strconv.Unquote(spec.Path.Value)
			if uqErr != nil || p == self || !strings.HasPrefix(p, modulePath+"/") {
				continue
			}
			set := importers[p]
			if set == nil {
				set = make(map[string]struct{})
				importers[p] = set
			}
			set[self] = struct{}{}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for p, set := range importers {
		if len(set) < minImportersForPackageDoc {
			continue
		}
		if documented, known := hasDoc[p]; known && !documented {
			missing = append(missing, p+" ("+strconv.Itoa(len(set))+" importers)")
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("no package doc comment: %s; add a doc.go (doc/templates/doc.go.tmpl)", m)
	}
}

func moduleRoot(t *testing.T) (root string) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's directory")
		}
		dir = parent
	}
}

func readModulePath(t *testing.T, goMod string) (path string) {
	t.Helper()
	data, err := os.ReadFile(goMod)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		if rest, found := strings.CutPrefix(strings.TrimSpace(sc.Text()), "module "); found {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatal("go.mod declares no module")
	return
}
