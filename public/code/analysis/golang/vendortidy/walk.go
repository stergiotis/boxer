package vendortidy

import (
	"bytes"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

func hasGoFiles(d string) bool {
	ents, err := os.ReadDir(d)
	if err != nil {
		return false
	}
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			return true
		}
	}
	return false
}

// packageDirs lists a module's package directories: every directory holding
// .go files, minus testdata, vendor, _ and . directories, nested modules and
// the go.mod ignore list (ADR-0384 §SD1). An ignore entry starting with ./ or
// ../ is relative to the module root; any other entry matches a directory of
// that name at any depth.
func packageDirs(m WorkspaceModule) (dirs []string, err error) {
	ignoredRel := make([]string, 0, len(m.File.Ignore))
	ignoredName := make([]string, 0, len(m.File.Ignore))
	for _, ig := range m.File.Ignore {
		if ig.Path == "." || strings.HasPrefix(ig.Path, "./") || strings.HasPrefix(ig.Path, "../") {
			ignoredRel = append(ignoredRel, filepath.Clean(filepath.FromSlash(ig.Path)))
		} else {
			ignoredName = append(ignoredName, ig.Path)
		}
	}
	err = filepath.WalkDir(m.Dir, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if !d.IsDir() {
			return nil
		}
		if p != m.Dir {
			name := d.Name()
			if name == "testdata" || name == "vendor" || strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			rel, rerr := filepath.Rel(m.Dir, p)
			if rerr != nil {
				return rerr
			}
			for _, ig := range ignoredRel {
				if rel == ig || strings.HasPrefix(rel, ig+string(filepath.Separator)) {
					return filepath.SkipDir
				}
			}
			if slices.Contains(ignoredName, name) {
				return filepath.SkipDir
			}
			if _, serr := os.Stat(filepath.Join(p, "go.mod")); serr == nil {
				return filepath.SkipDir
			}
		}
		if hasGoFiles(p) {
			dirs = append(dirs, p)
		}
		return nil
	})
	if err != nil {
		err = eb.Build().Str("dir", m.Dir).Errorf("list module packages: %w", err)
	}
	return
}

// dirImports returns the sorted imports of the .go files in d that build
// under "any tags"; test files count only when withTests is set.
func dirImports(d string, withTests bool) (imps []string, err error) {
	var ents []os.DirEntry
	ents, err = os.ReadDir(d)
	if err != nil {
		err = eb.Build().Str("dir", d).Errorf("read package directory: %w", err)
		return
	}
	set := make(map[string]struct{}, 32)
	fset := token.NewFileSet()
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			continue
		}
		if !withTests && strings.HasSuffix(name, "_test.go") {
			continue
		}
		p := filepath.Join(d, name)
		var src []byte
		src, err = os.ReadFile(p)
		if err != nil {
			err = eb.Build().Str("path", p).Errorf("read source file: %w", err)
			return
		}
		if !isBuiltUnderAnyTags(src) {
			continue
		}
		f, perr := parser.ParseFile(fset, p, src, parser.ImportsOnly)
		if perr != nil {
			err = eb.Build().Str("path", p).Errorf("parse imports: %w", perr)
			return
		}
		for _, is := range f.Imports {
			ip, uerr := strconv.Unquote(is.Path.Value)
			if uerr == nil {
				set[ip] = struct{}{}
			}
		}
	}
	imps = make([]string, 0, len(set))
	for k := range set {
		imps = append(imps, k)
	}
	slices.Sort(imps)
	return
}

// isBuiltUnderAnyTags mirrors cmd/go's ShouldBuild with the "*" tag set that
// go mod tidy loads with: every tag holds, negated or not, except "ignore"
// and malformed tags. File-name GOOS/GOARCH suffixes always match.
func isBuiltUnderAnyTags(src []byte) bool {
	header, goBuild, ok := fileHeader(src)
	if !ok {
		return false
	}
	if goBuild != "" {
		x, err := constraint.Parse(goBuild)
		if err != nil {
			return false
		}
		return evalAnyTags(x, true)
	}
	for line := range strings.SplitSeq(header, "\n") {
		line = strings.TrimSpace(line)
		if !constraint.IsPlusBuild(line) {
			continue
		}
		x, err := constraint.Parse(line)
		if err == nil && !evalAnyTags(x, true) {
			return false
		}
	}
	return true
}

func evalAnyTags(x constraint.Expr, prefer bool) bool {
	switch x := x.(type) {
	case *constraint.TagExpr:
		for _, c := range x.Tag {
			if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '_' && c != '.' {
				return false
			}
		}
		if x.Tag != "" && x.Tag != "ignore" {
			return prefer
		}
		return false
	case *constraint.NotExpr:
		return !evalAnyTags(x.X, !prefer)
	case *constraint.AndExpr:
		return evalAnyTags(x.X, prefer) && evalAnyTags(x.Y, prefer)
	case *constraint.OrExpr:
		return evalAnyTags(x.X, prefer) || evalAnyTags(x.Y, prefer)
	}
	return false
}

// fileHeader mirrors cmd/go's parseFileHeader: header is the part of the
// file where a "+build" line may appear, goBuild the //go:build line if any.
// ok is false when the file carries more than one //go:build line.
func fileHeader(src []byte) (header string, goBuild string, ok bool) {
	end := 0
	p := src
	ended := false
	inSlashStar := false
	ok = true
Lines:
	for len(p) > 0 {
		line := p
		if i := bytes.IndexByte(line, '\n'); i >= 0 {
			line, p = line[:i], p[i+1:]
		} else {
			p = p[len(p):]
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 && !ended {
			end = len(src) - len(p)
			continue Lines
		}
		if !bytes.HasPrefix(line, []byte("//")) {
			ended = true
		}
		if !inSlashStar && constraint.IsGoBuild(string(line)) {
			if goBuild != "" {
				ok = false
				return
			}
			goBuild = string(line)
		}
	Comments:
		for len(line) > 0 {
			if inSlashStar {
				if i := bytes.Index(line, []byte("*/")); i >= 0 {
					inSlashStar = false
					line = bytes.TrimSpace(line[i+2:])
					continue Comments
				}
				continue Lines
			}
			if bytes.HasPrefix(line, []byte("//")) {
				continue Lines
			}
			if bytes.HasPrefix(line, []byte("/*")) {
				inSlashStar = true
				line = bytes.TrimSpace(line[2:])
				continue Comments
			}
			break Lines
		}
	}
	header = string(src[:end])
	return
}
