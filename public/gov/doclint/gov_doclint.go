package doclint

import (
	"bytes"
	"io/fs"
	"iter"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

type FindingSeverityE uint8

const (
	FindingSeverityInfo  FindingSeverityE = 1
	FindingSeverityWarn  FindingSeverityE = 2
	FindingSeverityError FindingSeverityE = 3
)

var AllFindingSeverities = []FindingSeverityE{
	FindingSeverityInfo,
	FindingSeverityWarn,
	FindingSeverityError,
}

func (inst FindingSeverityE) String() (s string) {
	switch inst {
	case FindingSeverityInfo:
		s = "info"
	case FindingSeverityWarn:
		s = "warn"
	case FindingSeverityError:
		s = "error"
	default:
		s = "unknown"
	}
	return
}

// Finding is a single rule violation discovered during a lint pass.
//
// Line and Col are 1-based; zero means "not pinpointed within the file".
type Finding struct {
	RuleId   string           `json:"rule"`
	Severity FindingSeverityE `json:"severity"`
	Path     string           `json:"path"`
	Line     int32            `json:"line,omitempty"`
	Col      int32            `json:"col,omitempty"`
	Message  string           `json:"message"`
}

// RuleI is implemented by every doclint rule.
//
// Check walks the supplied roots and yields findings as they are produced.
// A non-nil error in the second yield slot indicates a walk-time failure
// (e.g. unreadable file) and aborts that rule's pass.
type RuleI interface {
	Id() (id string)
	Check(roots []string) iter.Seq2[Finding, error]
}

// Linter aggregates rules and runs them in sequence.
//
// Zero value is usable; rules are added via Register.
type Linter struct {
	rules []RuleI
}

func NewLinter() (inst *Linter) {
	inst = &Linter{}
	return
}

func (inst *Linter) Register(r RuleI) {
	inst.rules = append(inst.rules, r)
}

// Run executes every registered rule against the given roots and yields
// findings as they are produced. A non-nil error aborts the run.
func (inst *Linter) Run(roots []string) iter.Seq2[Finding, error] {
	return func(yield func(Finding, error) bool) {
		for _, r := range inst.rules {
			for f, err := range r.Check(roots) {
				if !yield(f, err) {
					return
				}
				if err != nil {
					return
				}
			}
		}
	}
}

// shouldSkipDir is consulted by every rule's filesystem walker to keep
// vendored, generated, version-control, fixture, and template trees out
// of the regular lint scope.
//
// Excludes:
//   - .git           — version control metadata
//   - node_modules   — JS dependency tree
//   - vendor         — Go vendored deps
//   - testdata       — Go convention; per-rule fixtures live here
//   - templates      — scaffolding the standard ships under doc/templates/;
//     its files have intentional draft/proposed status
//     and would otherwise show up in DL011 reports
//
// Run doclint with an explicit path under any of these directories to
// process them deliberately.
func shouldSkipDir(name string) (skip bool) {
	switch name {
	case ".git", "node_modules", "vendor", "testdata", "templates":
		skip = true
	}
	return
}

// gitIgnoredSet collects the absolute paths git ignores within root. It is
// best-effort: when git is unavailable, root is not inside a work tree, or the
// command fails, it returns nil and callers lint every file (the prior
// behaviour). A file git would never track — e.g. the git-ignored
// doc/leeway-map render artefacts (doc/.gitignore) — can only produce findings
// that cannot be committed, so the walkers skip it to keep a local run aligned
// with a clean CI checkout. Tracked files are never returned: --others lists
// untracked paths only.
//
// Fully-ignored directories are collapsed to a single entry by --directory, so
// the returned set holds directory paths (consult on the dir node to prune the
// whole subtree via SkipDir) as well as individually-ignored file paths.
func gitIgnoredSet(root string) (ignored map[string]struct{}) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return
	}
	gitDir := absRoot
	if fi, statErr := os.Stat(absRoot); statErr != nil || !fi.IsDir() {
		gitDir = filepath.Dir(absRoot)
	}
	top, err := runGit(gitDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return
	}
	toplevel := strings.TrimSpace(top)
	if toplevel == "" {
		return
	}
	out, err := runGit(gitDir,
		"ls-files", "--others", "--ignored", "--exclude-standard",
		"--directory", "--full-name", "-z", "--", absRoot)
	if err != nil {
		return
	}
	for e := range strings.SplitSeq(out, "\x00") {
		if e == "" {
			continue
		}
		if ignored == nil {
			ignored = make(map[string]struct{})
		}
		ignored[filepath.Clean(filepath.Join(toplevel, e))] = struct{}{}
	}
	return
}

// runGit runs git in dir and returns its stdout. Stderr is discarded; the
// error alone signals failure, which every caller treats as "fall back to
// no filtering".
func runGit(dir string, args ...string) (out string, err error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	err = cmd.Run()
	out = buf.String()
	return
}

// isGitIgnored reports whether path is in the ignored set produced by
// gitIgnoredSet. An empty set (git unavailable / nothing ignored) matches
// nothing.
func isGitIgnored(ignored map[string]struct{}, path string) (yes bool) {
	if len(ignored) == 0 {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	_, yes = ignored[filepath.Clean(abs)]
	return
}

// runMarkdownCheck is the shared filesystem traversal for rules whose scope is
// "every in-scope Markdown file under the standard". It walks each root,
// skipping the directories shouldSkipDir excludes, git-ignored paths, and the
// files IsInScopeForDL001 rejects, and invokes checkOne for each surviving .md
// file. A walk-time error aborts the rule's pass and is labelled with ruleID;
// checkOne returning cont=false stops the walk early (filepath.SkipAll).
//
// DL001/003/004/006/007/010/011 share this verbatim; only ruleID and the
// checkOne callback differ. Rules with a different scope (e.g. DL009) walk
// directly.
func runMarkdownCheck(
	ruleID string,
	roots []string,
	checkOne func(path string, yield func(Finding, error) bool) (cont bool, err error),
) iter.Seq2[Finding, error] {
	return func(yield func(Finding, error) bool) {
		for _, root := range roots {
			ignored := gitIgnoredSet(root)
			err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if d.IsDir() {
					if shouldSkipDir(d.Name()) || isGitIgnored(ignored, path) {
						return filepath.SkipDir
					}
					return nil
				}
				base := filepath.Base(path)
				if !strings.HasSuffix(strings.ToLower(base), ".md") {
					return nil
				}
				if isGitIgnored(ignored, path) {
					return nil
				}
				if !IsInScopeForDL001(path, base) {
					return nil
				}
				cont, fErr := checkOne(path, yield)
				if fErr != nil {
					return fErr
				}
				if !cont {
					return filepath.SkipAll
				}
				return nil
			})
			if err != nil {
				yield(Finding{}, eb.Build().Str("root", root).Errorf("%s walk: %w", ruleID, err))
				return
			}
		}
	}
}
