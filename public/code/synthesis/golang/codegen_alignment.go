package golang

import (
	"bytes"
	"errors"
	"go/ast"
	"go/format"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirkon/dst"
	"github.com/sirkon/dst/decorator"
	"golang.org/x/tools/go/packages"

	"github.com/stergiotis/boxer/public/observability/eh"
)

const ignoreCommentMarker = "betteralign:ignore"

// AlignAndFormat reorders fields of every named struct type in src to a
// minimal-footprint layout, equivalent to running
// `go tool betteralign -fix -apply` on the file written to targetPath.
// It does not write src to disk; the loader sees src via an overlay
// keyed on the absolute form of targetPath.
//
// The directory containing targetPath must contain at least one buildable
// Go file in the same package so the loader can resolve imports.
//
// buildTags is a comma- or space-separated list passed as `-tags=` to the
// loader. Pass empty string for none.
//
// On any failure the returned bytes equal src so callers can fall back.
func AlignAndFormat(src []byte, targetPath, buildTags string) (out []byte, err error) {
	out = src

	var targetAbs string
	targetAbs, err = filepath.Abs(targetPath)
	if err != nil {
		err = eh.Errorf("resolve abs path %q: %w", targetPath, err)
		return
	}
	pkgDir := filepath.Dir(targetAbs)

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedTypesSizes |
			packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps,
		Dir: pkgDir,
		Overlay: map[string][]byte{
			targetAbs: src,
		},
	}
	if strings.TrimSpace(buildTags) != "" {
		cfg.BuildFlags = []string{"-tags=" + buildTags}
	}

	var pkgs []*packages.Package
	pkgs, err = packages.Load(cfg, ".")
	if err != nil {
		err = eh.Errorf("load package %q: %w", pkgDir, err)
		return
	}
	if len(pkgs) == 0 {
		err = eh.Errorf("no package loaded for %q", pkgDir)
		return
	}

	pkg, file := findOverlayFile(pkgs, targetAbs)
	if pkg == nil || file == nil {
		err = eh.Errorf("file %q not found in loaded package(s); %s", targetAbs, joinPackageErrors(pkgs))
		return
	}
	if pkg.TypesInfo == nil || pkg.TypesSizes == nil {
		err = eh.Errorf("type info/sizes missing for package containing %q", targetAbs)
		return
	}
	if len(pkg.Errors) > 0 {
		var msgs []string
		for _, e := range pkg.Errors {
			msgs = append(msgs, e.Error())
		}
		err = eh.Errorf("type-check errors in package containing %q: %s", targetAbs, strings.Join(msgs, "; "))
		return
	}

	word := pkg.TypesSizes.Sizeof(types.Typ[types.UnsafePointer])
	maxAlign := pkg.TypesSizes.Alignof(types.Typ[types.UnsafePointer])
	sizes := &gcSizes{WordSize: word, MaxAlign: maxAlign}

	dec := decorator.NewDecorator(pkg.Fset)
	var dFile *dst.File
	dFile, err = dec.DecorateFile(file)
	if err != nil {
		err = eh.Errorf("decorate %q: %w", targetAbs, err)
		return
	}

	var anyChange bool
	ast.Inspect(file, func(n ast.Node) bool {
		st, ok := n.(*ast.StructType)
		if !ok {
			return true
		}
		tv, ok := pkg.TypesInfo.Types[st]
		if !ok || tv.Type == nil {
			return true
		}
		sct, ok := tv.Type.(*types.Struct)
		if !ok {
			return true
		}
		if sct.NumFields() < 2 {
			return true
		}
		_, indexes := optimalOrder(sct, sizes)
		if isIdentity(indexes) {
			return true
		}
		dn, ok := dec.Dst.Nodes[st].(*dst.StructType)
		if !ok || dn == nil {
			return true
		}
		if hasIgnoreComment(dn.Fields) {
			return true
		}
		if !reorderDstFields(dn.Fields, indexes) {
			return true
		}
		anyChange = true
		return true
	})

	if !anyChange {
		// Already optimal — return src unchanged to avoid drift from
		// re-printing through dst+gofmt.
		out = src
		return
	}

	var buf bytes.Buffer
	if err = decorator.Fprint(&buf, dFile); err != nil {
		err = eh.Errorf("dst print %q: %w", targetAbs, err)
		out = src
		return
	}
	out = buf.Bytes()
	if formatted, ferr := format.Source(out); ferr == nil {
		out = formatted
	}
	return
}

// WriteAligned aligns src and writes it to targetPath. Build tags are
// discovered from the boxer module's `tags` file by walking up the
// directory tree from targetPath's parent.
func WriteAligned(targetPath string, src []byte) (err error) {
	var abs string
	abs, err = filepath.Abs(targetPath)
	if err != nil {
		err = eh.Errorf("resolve %q: %w", targetPath, err)
		return
	}
	var tags string
	tags, err = FindModuleBuildTags(filepath.Dir(abs))
	if err != nil {
		err = eh.Errorf("find build tags for %q: %w", abs, err)
		return
	}
	var aligned []byte
	aligned, err = AlignAndFormat(src, abs, tags)
	if err != nil {
		err = eh.Errorf("align %q: %w", abs, err)
		return
	}
	if err = os.WriteFile(abs, aligned, 0o644); err != nil {
		err = eh.Errorf("write %q: %w", abs, err)
		return
	}
	return
}

// FindModuleBuildTags walks up from start until it finds a directory
// containing go.mod, then reads `tags` in that directory. Returns the
// trimmed contents, or "" if the tags file is absent. Returns an error
// if no go.mod is found above start.
func FindModuleBuildTags(start string) (tags string, err error) {
	d := start
	for {
		if _, statErr := os.Stat(filepath.Join(d, "go.mod")); statErr == nil {
			data, readErr := os.ReadFile(filepath.Join(d, "tags"))
			if readErr != nil {
				if errors.Is(readErr, os.ErrNotExist) {
					return "", nil
				}
				return "", eh.Errorf("read tags file: %w", readErr)
			}
			return strings.TrimSpace(string(data)), nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", eh.Errorf("no go.mod ancestor of %q", start)
		}
		d = parent
	}
}

func findOverlayFile(pkgs []*packages.Package, targetAbs string) (*packages.Package, *ast.File) {
	for _, p := range pkgs {
		if p == nil {
			continue
		}
		for i, gf := range p.CompiledGoFiles {
			if filepath.Clean(gf) == targetAbs && i < len(p.Syntax) {
				return p, p.Syntax[i]
			}
		}
		for i, gf := range p.GoFiles {
			if filepath.Clean(gf) == targetAbs && i < len(p.Syntax) {
				return p, p.Syntax[i]
			}
		}
	}
	return nil, nil
}

func joinPackageErrors(pkgs []*packages.Package) string {
	var msgs []string
	for _, p := range pkgs {
		for _, e := range p.Errors {
			msgs = append(msgs, e.Error())
		}
	}
	if len(msgs) == 0 {
		return "no loader errors reported"
	}
	return "loader errors: " + strings.Join(msgs, "; ")
}

func reorderDstFields(fields *dst.FieldList, indexes []int) bool {
	if fields == nil || len(fields.List) == 0 {
		return false
	}
	dummy := &dst.Field{}
	flat := make([]*dst.Field, 0, len(indexes))
	for _, f := range fields.List {
		flat = append(flat, f)
		if len(f.Names) <= 1 {
			continue
		}
		for range f.Names[1:] {
			flat = append(flat, dummy)
		}
	}
	if len(flat) != len(indexes) {
		return false
	}
	reordered := make([]*dst.Field, 0, len(indexes))
	for _, idx := range indexes {
		f := flat[idx]
		if f == dummy {
			continue
		}
		reordered = append(reordered, f)
	}
	fields.List = reordered
	return true
}

func hasIgnoreComment(fields *dst.FieldList) bool {
	if fields == nil {
		return false
	}
	for _, line := range fields.Decs.Opening.All() {
		if strings.HasPrefix(line, "//") && strings.Contains(line, ignoreCommentMarker) {
			return true
		}
	}
	return false
}

func isIdentity(indexes []int) bool {
	for i, v := range indexes {
		if i != v {
			return false
		}
	}
	return true
}
