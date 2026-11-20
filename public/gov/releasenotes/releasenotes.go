// Package releasenotes compiles the release notes of a commit range: the
// breaking commits with their `Breaks:` paragraphs, packages removed or
// renamed, the toolchain and dependency floors that moved, changed contract
// snapshots, and the migration notes added (ADR-0389).
//
// It extracts rather than summarises. Everything it emits is read from git —
// commit subjects and bodies, trees and go.mod at the two ends — so the
// output is a pure function of the range and carries no timestamp. Whether a
// change is breaking is taken from the commit subject's `!` marker
// (CODINGSTANDARDS § Version Control); a breaking change without the marker
// does not appear here, which is why scripts/ci/apidiff.sh exists beside it.
package releasenotes

import (
	"bytes"
	"context"
	"path"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Commit is one commit of the range.
type Commit struct {
	Hash    string
	Subject string
	Body    string // the full message, subject included, as git prints %B
}

// BreakingCommit is a commit whose subject carries `!` before the colon.
type BreakingCommit struct {
	Hash    string
	Subject string
	// Breaks is the commit body's `Breaks:` paragraph without the label, or
	// empty when the body has none.
	Breaks string
}

// PackageChangeKindE says how a package left the tree.
type PackageChangeKindE uint8

const (
	PackageChangeKindRemoved PackageChangeKindE = iota
	PackageChangeKindRenamed
)

// PackageChange is a package present at the start of the range and absent at
// its end.
type PackageChange struct {
	Dir  string // the package directory at the start, module-relative
	Kind PackageChangeKindE
	To   string // the directory it moved to, for PackageChangeKindRenamed
}

// ModuleChange is one requirement of go.mod that differs between the ends.
// From is empty for an added requirement, To for a removed one.
type ModuleChange struct {
	Path     string
	From     string
	To       string
	Indirect bool
}

// Notes is everything collected for one range.
type Notes struct {
	From       string
	To         string
	NumCommits int
	Breaking   []BreakingCommit
	Packages   []PackageChange
	GoFrom     string
	GoTo       string
	ToolFrom   string // the go.mod toolchain line, empty when absent
	ToolTo     string
	Modules    []ModuleChange
	Contracts  []string // `git diff --name-status` lines under contracts/
	Migrations []string // migration notes added, module-relative paths
}

const (
	fieldSep  = "\x1f"
	recordSep = "\x1e"
	// logFormat is the git log --format producing records ParseCommitLog reads.
	logFormat = "%H" + fieldSep + "%s" + fieldSep + "%B" + recordSep
)

// ParseCommitLog splits `git log --format=<logFormat>` output into commits.
func ParseCommitLog(raw string) (commits []Commit) {
	for rec := range strings.SplitSeq(raw, recordSep) {
		rec = strings.TrimLeft(rec, "\n")
		if strings.TrimSpace(rec) == "" {
			continue
		}
		parts := strings.SplitN(rec, fieldSep, 3)
		if len(parts) < 3 {
			continue
		}
		commits = append(commits, Commit{Hash: parts[0], Subject: parts[1], Body: parts[2]})
	}
	return
}

// IsBreakingSubject reports whether a subject is typed `type(scope)!: …` or
// `type!: …`.
func IsBreakingSubject(subject string) (b bool) {
	head, _, ok := strings.Cut(subject, ":")
	if !ok || !strings.HasSuffix(head, "!") {
		return false
	}
	head = strings.TrimSuffix(head, "!")
	if i := strings.IndexByte(head, '('); i >= 0 {
		if !strings.HasSuffix(head, ")") {
			return false
		}
		head = head[:i]
	}
	if head == "" {
		return false
	}
	for _, r := range head {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

// BreaksParagraph returns the paragraph of body that starts with `Breaks:`,
// without the label and with its lines joined by single spaces; empty when
// there is none.
func BreaksParagraph(body string) (breaks string) {
	for para := range strings.SplitSeq(strings.ReplaceAll(body, "\r\n", "\n"), "\n\n") {
		para = strings.TrimSpace(para)
		rest, ok := strings.CutPrefix(para, "Breaks:")
		if !ok {
			continue
		}
		return strings.Join(strings.Fields(rest), " ")
	}
	return ""
}

// BreakingCommits selects the breaking commits, in the order given.
func BreakingCommits(commits []Commit) (out []BreakingCommit) {
	for _, c := range commits {
		if IsBreakingSubject(c.Subject) {
			out = append(out, BreakingCommit{Hash: c.Hash, Subject: c.Subject, Breaks: BreaksParagraph(c.Body)})
		}
	}
	return
}

// propsFile is the per-package declaration (ADR-0080). Every one looks alike,
// so git pairs them as renames across unrelated packages; rename detection
// ignores them.
const propsFile = "package_props.go"

// PackageDirs returns the importable package directories among tracked file
// paths — directories under public/ or apps/ holding a non-test .go file,
// excluding internal and testdata trees, which no consumer can import — with
// the number of their non-test .go files other than the props declaration.
func PackageDirs(files []string) (dirs map[string]int) {
	dirs = make(map[string]int)
	for _, f := range files {
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		if !strings.HasPrefix(f, "public/") && !strings.HasPrefix(f, "apps/") {
			continue
		}
		d := path.Dir(f)
		if hasElem(d, "internal") || hasElem(d, "testdata") {
			continue
		}
		n := dirs[d] // registers the directory even when it holds only the declaration
		if path.Base(f) != propsFile {
			n++
		}
		dirs[d] = n
	}
	return
}

func hasElem(p string, elem string) (b bool) {
	for e := range strings.SplitSeq(p, "/") {
		if e == elem {
			return true
		}
	}
	return false
}

// PackageChanges lists the packages of before (directory → file count, as
// [PackageDirs] returns) that are gone from after. A gone package is a rename
// when at least half of its files moved into one package of after (renames:
// old file → new file, as git reports them); otherwise it is a removal.
func PackageChanges(before map[string]int, after map[string]int, renames map[string]string) (changes []PackageChange) {
	moved := make(map[string]map[string]int)
	for from, to := range renames {
		fd, td := path.Dir(from), path.Dir(to)
		if fd == td || path.Base(from) == propsFile {
			continue
		}
		if moved[fd] == nil {
			moved[fd] = make(map[string]int)
		}
		moved[fd][td]++
	}
	for d := range before {
		if _, ok := after[d]; ok {
			continue
		}
		best, bestN := "", 0
		for td, n := range moved[d] {
			if _, ok := after[td]; !ok {
				continue
			}
			if n > bestN || (n == bestN && td < best) {
				best, bestN = td, n
			}
		}
		if best != "" && 2*bestN >= before[d] {
			changes = append(changes, PackageChange{Dir: d, Kind: PackageChangeKindRenamed, To: best})
		} else {
			changes = append(changes, PackageChange{Dir: d, Kind: PackageChangeKindRemoved})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Dir < changes[j].Dir })
	return
}

// renameMinScore is the similarity below which a git rename is not taken as
// evidence that a package moved: across a long range, small files that merely
// look alike pair up.
const renameMinScore = 75

// ParseRenames reads `git diff --name-status -M` rename lines (`R<score>\told\tnew`)
// of at least renameMinScore similarity into old → new.
func ParseRenames(nameStatus string) (renames map[string]string) {
	renames = make(map[string]string)
	for line := range strings.SplitSeq(nameStatus, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 3 || !strings.HasPrefix(f[0], "R") {
			continue
		}
		score, err := strconv.Atoi(strings.TrimPrefix(f[0], "R"))
		if err != nil || score < renameMinScore {
			continue
		}
		renames[f[1]] = f[2]
	}
	return
}

// GoModFacts is what the notes read from one go.mod.
type GoModFacts struct {
	Go        string
	Toolchain string
	Requires  map[string]ModuleChange // keyed by module path; To holds the version
}

// ParseGoMod reads the go and toolchain directives and the requirements.
func ParseGoMod(src []byte) (facts GoModFacts, err error) {
	var f *modfile.File
	f, err = modfile.Parse("go.mod", src, nil)
	if err != nil {
		err = eb.Build().Errorf("parse go.mod: %w", err)
		return
	}
	if f.Go != nil {
		facts.Go = f.Go.Version
	}
	if f.Toolchain != nil {
		facts.Toolchain = f.Toolchain.Name
	}
	facts.Requires = make(map[string]ModuleChange, len(f.Require))
	for _, r := range f.Require {
		facts.Requires[r.Mod.Path] = ModuleChange{Path: r.Mod.Path, To: r.Mod.Version, Indirect: r.Indirect}
	}
	return
}

// ModuleChanges compares two requirement sets.
func ModuleChanges(before GoModFacts, after GoModFacts) (changes []ModuleChange) {
	for p, a := range after.Requires {
		b, ok := before.Requires[p]
		switch {
		case !ok:
			changes = append(changes, ModuleChange{Path: p, To: a.To, Indirect: a.Indirect})
		case b.To != a.To:
			changes = append(changes, ModuleChange{Path: p, From: b.To, To: a.To, Indirect: a.Indirect})
		}
	}
	for p, b := range before.Requires {
		if _, ok := after.Requires[p]; !ok {
			changes = append(changes, ModuleChange{Path: p, From: b.To, Indirect: b.Indirect})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return
}

// git runs one git command in dir and returns its stdout.
func git(ctx context.Context, dir string, args ...string) (out string, err error) {
	cmd, err := extbin.Git.Command(ctx, extbin.Opts{Dir: dir}, args...)
	if err != nil {
		err = eb.Build().Errorf("releasenotes: resolve git: %w", err)
		return
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		err = eb.Build().Strs("args", args).Str("stderr", strings.TrimSpace(stderr.String())).Errorf("releasenotes: git: %w", err)
		return
	}
	out = stdout.String()
	return
}

// Collect reads the notes for from..to from the repository at dir.
func Collect(ctx context.Context, dir string, from string, to string) (n Notes, err error) {
	n.From, n.To = from, to
	var raw string
	if raw, err = git(ctx, dir, "log", "--no-color", "--format="+logFormat, from+".."+to); err != nil {
		return
	}
	commits := ParseCommitLog(raw)
	n.NumCommits = len(commits)
	n.Breaking = BreakingCommits(commits)

	var filesFrom, filesTo, renamed string
	if filesFrom, err = git(ctx, dir, "ls-tree", "-r", "--name-only", from); err != nil {
		return
	}
	if filesTo, err = git(ctx, dir, "ls-tree", "-r", "--name-only", to); err != nil {
		return
	}
	if renamed, err = git(ctx, dir, "diff", "--no-color", "--name-status", "-M", "--diff-filter=R", from, to, "--", "*.go"); err != nil {
		return
	}
	renames := ParseRenames(renamed)
	n.Packages = PackageChanges(PackageDirs(strings.Split(filesFrom, "\n")), PackageDirs(strings.Split(filesTo, "\n")), renames)

	var modFrom, modTo string
	if modFrom, err = git(ctx, dir, "show", from+":go.mod"); err != nil {
		return
	}
	if modTo, err = git(ctx, dir, "show", to+":go.mod"); err != nil {
		return
	}
	var factsFrom, factsTo GoModFacts
	if factsFrom, err = ParseGoMod([]byte(modFrom)); err != nil {
		return
	}
	if factsTo, err = ParseGoMod([]byte(modTo)); err != nil {
		return
	}
	n.GoFrom, n.GoTo = factsFrom.Go, factsTo.Go
	n.ToolFrom, n.ToolTo = factsFrom.Toolchain, factsTo.Toolchain
	n.Modules = ModuleChanges(factsFrom, factsTo)

	var contracts, migrations string
	if contracts, err = git(ctx, dir, "diff", "--no-color", "--name-status", from, to, "--", "contracts"); err != nil {
		return
	}
	n.Contracts = nonEmptyLines(contracts)
	if migrations, err = git(ctx, dir, "diff", "--no-color", "--name-only", "--diff-filter=A", from, to, "--", "doc/migration"); err != nil {
		return
	}
	n.Migrations = nonEmptyLines(migrations)
	return
}

func nonEmptyLines(s string) (lines []string) {
	for l := range strings.SplitSeq(s, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return
}
