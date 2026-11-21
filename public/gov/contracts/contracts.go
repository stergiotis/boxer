// Package contracts collects what boxer promises across a release — the
// stored, wire and command-line shapes another build or a deployment
// depends on — as a snapshot, and compares a tree against a committed one
// (ADR-0388).
//
// A snapshot is a directory of plain text, so a change to one reads as a
// diff in review:
//
//	ddl/<path>          the DDL golden of each production table, verbatim
//	membership/<path>   each membership-assignment golden, verbatim
//	lwsurface.txt       the leeway SQL surface: generation, digest, roster
//	env.txt             the registered environment variable names
//	idl.txt             the imzero2 IDL fingerprint
//	versions.txt        the wire and format version constants
//	cli.txt             the command tree: every command path and flag
//
// Diff classifies each difference as additive or not, per kind; the rules
// are ADR-0388's. A non-additive difference is acknowledged by a note under
// doc/migration that names it on a line `contract: <kind>/<item>`.
//
// The snapshot reads what the running binary links (the surface, the
// registry, the IDL fingerprint, the command tree) and what the tree holds
// on disk (goldens, version constants). It is taken from the repository
// root by the boxer CLI, which links every package.
package contracts

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	cli "github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// KindE names one kind of contract in a snapshot.
type KindE string

const (
	KindDDL        KindE = "ddl"
	KindMembership KindE = "membership"
	KindLwSurface  KindE = "lwsurface"
	KindEnv        KindE = "env"
	KindIDL        KindE = "idl"
	KindVersion    KindE = "version"
	KindCLI        KindE = "cli"
)

// Snapshot is the contracts of one tree: per kind, item → content. For the
// verbatim kinds (ddl, membership) the item is the golden's path relative
// to the root and the content its text; for the others the item is one
// line's key and the content its value, empty when the line is only a key.
type Snapshot map[KindE]map[string]string

func (inst Snapshot) put(k KindE, item string, content string) {
	if inst[k] == nil {
		inst[k] = make(map[string]string)
	}
	inst[k][item] = content
}

// scanRoots are the directories the goldens and constants are looked for
// under; the rest of the checkout (vendored trees, worktrees, build output)
// is not the repository's contract.
var scanRoots = []string{"public", "apps"}

// ddlSuffix marks a table's DDL golden.
const ddlSuffix = "_ddl_clickhouse.out.sql"

// membershipGolden is the name of a vocabulary's membership-assignment
// golden.
const membershipGolden = "membership-assignments.golden"

// versionConstNames are the constant names collected into versions.txt
// wherever they are declared.
var versionConstNames = []string{"wireVersion", "WireVersion", "TreeVersion", "PlanFormatVersion", "FormatVersion"}

// versionConstsAt are further constants collected by package directory:
// ones whose name alone is too common to collect everywhere.
var versionConstsAt = map[string][]string{
	"public/semistructured/leeway/canonwire/runtime": {"Version"},
}

// nonProductionDDL are the directories whose DDL goldens describe a store
// no deployment holds, beyond the examples, demos and fixtures the path
// says: the leeway documentation anchor and the shared-section
// demonstration store.
var nonProductionDDL = []string{
	"public/semistructured/leeway/anchor/",
	"public/storage/recordstore/internal/sharedsection/",
}

// productionDDL says whether a DDL golden belongs to a production table:
// not an example, a demo, a test fixture, or a directory nonProductionDDL
// names.
func productionDDL(rel string) bool {
	for seg := range strings.SplitSeq(filepath.ToSlash(filepath.Dir(rel)), "/") {
		if strings.Contains(seg, "example") || strings.Contains(seg, "demo") || seg == "testdata" {
			return false
		}
	}
	for _, p := range nonProductionDDL {
		if strings.HasPrefix(filepath.ToSlash(rel), p) {
			return false
		}
	}
	return true
}

// Collect takes the snapshot of the tree at root, with root as the command
// tree's root.
func Collect(root string, cmdRoot *cli.Command) (snap Snapshot, err error) {
	snap = make(Snapshot)
	err = collectFiles(root, snap)
	if err != nil {
		return
	}
	snap.put(KindLwSurface, "generation", strconv.Itoa(lwsqlsurface.Version))
	snap.put(KindLwSurface, "digest", lwsqlsurface.Digest())
	for name := range lwsqlsurface.DeclaredNames() {
		snap.put(KindLwSurface, "function/"+name, "")
	}
	for _, s := range env.Snapshot() {
		snap.put(KindEnv, s.Name, "")
	}
	snap.put(KindIDL, "fingerprint", "0x"+strconv.FormatUint(bindings.IdlFingerprint, 16))
	if cmdRoot != nil {
		collectCLI(cmdRoot, nil, snap)
	}
	return
}

func collectFiles(root string, snap Snapshot) (err error) {
	for _, sub := range scanRoots {
		base := filepath.Join(root, sub)
		err = filepath.WalkDir(base, func(path string, d fs.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") || d.Name() == "target" || d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			rel = filepath.ToSlash(rel)
			name := d.Name()
			switch {
			case strings.HasSuffix(name, ddlSuffix) && productionDDL(rel):
				b, e := os.ReadFile(path)
				if e != nil {
					return e
				}
				snap.put(KindDDL, rel, string(b))
			case name == membershipGolden:
				b, e := os.ReadFile(path)
				if e != nil {
					return e
				}
				snap.put(KindMembership, rel, string(b))
			case strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go"):
				return collectVersionConsts(path, filepath.ToSlash(filepath.Dir(rel)), snap)
			}
			return nil
		})
		if err != nil {
			err = eb.Build().Str("dir", base).Errorf("collect contracts: %w", err)
			return
		}
	}
	return
}

// collectVersionConsts records the version constants a Go file declares at
// package level, keyed "<dir>.<name>". Only constants whose value is an
// integer literal, or a conversion of one, are recorded; anything else is
// recorded as its source text.
func collectVersionConsts(path string, dir string, snap Snapshot) (err error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return
	}
	names := slices.Concat(versionConstNames, versionConstsAt[dir])
	found := false
	for _, n := range names {
		if bytes.Contains(src, []byte(n)) {
			found = true
			break
		}
	}
	if !found {
		return
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return eb.Build().Str("path", path).Errorf("parse: %w", err)
	}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, id := range vs.Names {
				if !slices.Contains(names, id.Name) || i >= len(vs.Values) {
					continue
				}
				snap.put(KindVersion, dir+"."+id.Name, constText(src, fset, vs.Values[i]))
			}
		}
	}
	return
}

// constText is an integer literal's text, unwrapping one conversion
// (`uint32(1)`); otherwise the expression's source.
func constText(src []byte, fset *token.FileSet, e ast.Expr) (s string) {
	if call, ok := e.(*ast.CallExpr); ok && len(call.Args) == 1 {
		e = call.Args[0]
	}
	if lit, ok := e.(*ast.BasicLit); ok {
		return lit.Value
	}
	return string(src[fset.Position(e.Pos()).Offset:fset.Position(e.End()).Offset])
}

// cliRootLabel stands for the root command, whose own name is a build
// string.
const cliRootLabel = "boxer"

// collectCLI records every command path and every flag under it, the
// root's flags included. The help command and flag urfave/cli adds to every
// command are left out.
func collectCLI(cmd *cli.Command, path []string, snap Snapshot) {
	key := strings.Join(append([]string{cliRootLabel}, path...), " ")
	snap.put(KindCLI, key, "")
	for _, f := range cmd.Flags {
		names := f.Names()
		if len(names) > 0 && names[0] != "help" {
			snap.put(KindCLI, key+" --"+names[0], "")
		}
	}
	for _, sub := range cmd.Commands {
		if sub.Name == "help" {
			continue
		}
		collectCLI(sub, append(slices.Clone(path), sub.Name), snap)
	}
}

// singleFileKinds are written one line per item.
var singleFileKinds = map[KindE]string{
	KindLwSurface: "lwsurface.txt",
	KindEnv:       "env.txt",
	KindIDL:       "idl.txt",
	KindVersion:   "versions.txt",
	KindCLI:       "cli.txt",
}

// Write writes the snapshot to dir, which it creates; files of a previous
// snapshot in dir are not removed, so dir is expected to be fresh.
func (inst Snapshot) Write(dir string) (err error) {
	for _, k := range []KindE{KindDDL, KindMembership} {
		for item, content := range inst[k] {
			p := filepath.Join(dir, string(k), filepath.FromSlash(item))
			if err = os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return
			}
			if err = os.WriteFile(p, []byte(content), 0o644); err != nil {
				return
			}
		}
	}
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	for k, file := range singleFileKinds {
		var b strings.Builder
		for _, item := range sortedKeys(inst[k]) {
			b.WriteString(item)
			if v := inst[k][item]; v != "" {
				b.WriteString(" = ")
				b.WriteString(v)
			}
			b.WriteString("\n")
		}
		if err = os.WriteFile(filepath.Join(dir, file), []byte(b.String()), 0o644); err != nil {
			return
		}
	}
	return
}

// Load reads a snapshot Write wrote.
func Load(dir string) (snap Snapshot, err error) {
	snap = make(Snapshot)
	for _, k := range []KindE{KindDDL, KindMembership} {
		base := filepath.Join(dir, string(k))
		if _, serr := os.Stat(base); serr != nil {
			continue
		}
		err = filepath.WalkDir(base, func(path string, d fs.DirEntry, werr error) error {
			if werr != nil || d.IsDir() {
				return werr
			}
			rel, rerr := filepath.Rel(base, path)
			if rerr != nil {
				return rerr
			}
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			snap.put(k, filepath.ToSlash(rel), string(b))
			return nil
		})
		if err != nil {
			err = eb.Build().Str("dir", base).Errorf("load snapshot: %w", err)
			return
		}
	}
	for k, file := range singleFileKinds {
		b, rerr := os.ReadFile(filepath.Join(dir, file))
		if rerr != nil {
			err = eb.Build().Str("file", file).Errorf("load snapshot: %w", rerr)
			return
		}
		snap[k] = make(map[string]string)
		for line := range strings.SplitSeq(string(b), "\n") {
			if line == "" {
				continue
			}
			item, value, _ := strings.Cut(line, " = ")
			snap[k][item] = value
		}
	}
	return
}

func sortedKeys(m map[string]string) (keys []string) {
	keys = make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return
}
