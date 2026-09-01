package doclint

import (
	"iter"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RuleDL017 — a backticked in-repo path resolves to a file or directory
// that exists.
//
// Implements DOCUMENTATION_STANDARD §4 *Claims that decay* / §7: a file path
// is a Markdown link, not a bare backticked path, because a link is checked
// (DL007) and a reader can follow it. A bare `doc/planning/2026_07_31__x.md`
// is checked by nothing, and phantom citations of exactly that shape — paths
// that never existed, or existed in another repository — reached ADRs in
// this ecosystem without any rule objecting. This rule closes that gap
// from the other side: it does not demand the link (the standard's row for
// that stays a judgment on what deserves one), it demands that the path
// named be real.
//
// A span qualifies as an in-repo path when it has no whitespace, contains
// a '/', and its first segment is either '.' ('./x') or a directory at the
// top of the repository the document lives in (doc/, public/, scripts/ …,
// discovered by listing the work tree root rather than hard-coding boxer's
// layout, so a consuming repository's src/ or contrib/ counts too). A span
// containing '<', '>', '*', '{', '$' or '…' is a template, not a citation,
// and is skipped; so is one starting with '../', which names a sibling
// checkout ('../boxer/doc/x.md' written from a consumer) that a clean
// checkout of this repository cannot see, one containing '...' (a Go
// package pattern) or '::' (a Rust symbol path), and one with a colon
// that is not a line pin. A trailing ':NNN' line pin is
// stripped before resolving — DL016 reports the pin, this rule checks the
// file. A span that is the whole text of an inline link is skipped: the
// link is the reference there, and DL007 already checks it.
//
// Resolution tries both the repository root and the document's own
// directory ('./tags' under doc/ means the root's tags file, as the reader
// is expected to run from there); the span passes if
// either exists and neither is git-ignored (the DL007 reasoning: it exists
// here and in no clean checkout). Severity is warn while the in-tree
// backlog is measured; a repository that has cleared it can raise it.
type RuleDL017 struct {
	mu   sync.Mutex
	tops map[string]repoTop
}

// repoTop is one discovered repository root and its top-level directory
// names; "" as Dir means the directory is not inside a repository.
type repoTop struct {
	Dir      string
	TopLevel map[string]struct{}
}

func NewRuleDL017() (inst *RuleDL017) {
	inst = &RuleDL017{tops: make(map[string]repoTop)}
	return
}

func (inst *RuleDL017) Id() (id string) {
	id = "DL017"
	return
}

func (inst *RuleDL017) Check(roots []string) iter.Seq2[Finding, error] {
	return runMarkdownCheck("DL017", roots, inst.checkOne)
}

// templateRunes mark a span as a pattern the author never meant to resolve.
const templateRunes = "<>*{$…"

// classifyBacktickPath decides whether tok is an in-repo path DL017 should
// resolve and returns the path to resolve (line pin stripped). top is the
// set of top-level directory names of the containing repository.
func classifyBacktickPath(tok string, top map[string]struct{}) (path string, candidate bool) {
	if tok == "" || strings.ContainsAny(tok, " \t") || strings.ContainsAny(tok, templateRunes) {
		return
	}
	if !strings.Contains(tok, "/") {
		return
	}
	if strings.HasPrefix(tok, "../") || strings.HasPrefix(tok, "/") {
		return
	}
	// 'public/...' is a Go package pattern, 'app.rs::load_fonts' a Rust
	// symbol path: neither names one file.
	if strings.Contains(tok, "...") || strings.Contains(tok, "::") {
		return
	}
	path = tok
	if p, pinned := matchLinePin(tok); pinned {
		path = p
	} else if strings.Contains(tok, ":") {
		// A colon that is not a line pin ('doc/x.md:Section', 'host:port'
		// with a slash) is not a path this rule can resolve.
		return
	}
	first, _, _ := strings.Cut(path, "/")
	if first == "." {
		candidate = true
		return
	}
	_, candidate = top[first]
	return
}

// findRepoTop walks up from dir to the nearest directory holding a '.git'
// entry. It is a filesystem probe rather than a git call so a test can
// stand up a repository shape with mkdir, and so a tree outside any work
// tree (Dir == "") falls back cleanly to "no top-level directories": only
// './' spans are then candidates.
func (inst *RuleDL017) findRepoTop(dir string) (top repoTop) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if cached, ok := inst.tops[abs]; ok {
		top = cached
		return
	}
	cur := abs
	for {
		if _, statErr := os.Stat(filepath.Join(cur, ".git")); statErr == nil {
			top.Dir = cur
			top.TopLevel = make(map[string]struct{})
			if entries, readErr := os.ReadDir(cur); readErr == nil {
				for _, e := range entries {
					if e.IsDir() && e.Name() != ".git" {
						top.TopLevel[e.Name()] = struct{}{}
					}
				}
			}
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	inst.tops[abs] = top
	return
}

func (inst *RuleDL017) checkOne(filePath string, ignored map[string]struct{}, yield func(Finding, error) bool) (cont bool, err error) {
	cont = true
	var data []byte
	data, err = os.ReadFile(filePath)
	if err != nil {
		err = eb.Build().Str("path", filePath).Errorf("DL017 read: %w", err)
		return
	}
	_, body, ok, parseErr := parseMdFrontMatter(data)
	if !ok || parseErr != nil {
		body = data
	}
	lineOffset := frontMatterLineOffset(data, body)
	fileDir := filepath.Dir(filePath)
	top := inst.findRepoTop(fileDir)

	forEachBacktickToken(body, func(tok backtickToken) bool {
		if tok.InLinkText {
			return true
		}
		path, candidate := classifyBacktickPath(tok.Text, top.TopLevel)
		if !candidate {
			return true
		}
		// A root-anchored path ('doc/x.md') means the repository root; a
		// './' path means the document's directory — except that docs
		// under doc/ routinely write './tags' or './generate.sh' for a
		// file at the repository root, as the reader is expected to run
		// it from there. Both bases are tried in both cases; the order
		// only decides which resolved path is reported.
		bases := []string{fileDir, top.Dir}
		if !strings.HasPrefix(path, "./") {
			bases = []string{top.Dir, fileDir}
		}
		exists := false
		gitIgnored := false
		for _, base := range bases {
			if base == "" {
				continue
			}
			resolved := filepath.Join(base, path)
			if _, statErr := os.Stat(resolved); statErr != nil {
				continue
			}
			if isGitIgnoredTree(ignored, resolved) {
				gitIgnored = true
				continue
			}
			exists = true
			break
		}
		if exists {
			return true
		}
		msg := "backticked path '" + tok.Text + "' does not resolve in this repository; link it (§7) or fix the path"
		if gitIgnored {
			msg = "backticked path '" + tok.Text + "' is git-ignored: it exists in this checkout but never in a clean one"
		}
		f := Finding{
			RuleId:   "DL017",
			Severity: FindingSeverityWarn,
			Path:     filePath,
			Line:     tok.Line + lineOffset,
			Col:      1,
			Message:  msg,
		}
		cont = yield(f, nil)
		return cont
	})
	return
}
