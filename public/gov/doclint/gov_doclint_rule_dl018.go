package doclint

import (
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/gov/docstd"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RuleDL018 — a `proposed` document that `accepted` documents link to.
//
// A proposed ADR is a living snapshot: it is edited in place, and git is the
// only trail. An accepted ADR is frozen apart from Tier-1 edits and changes by
// dated Updates. A link from the second to the first therefore cites text that
// can still move under it without either document saying so — the accepted
// side reads as settled while what it relies on is not. The link itself is
// usually true; what has changed is the target's position, which other
// decisions now build on.
//
// The finding is reported on the proposed document, once, listing the accepted
// documents that link to it. That is where the remedy is: flip it to accepted
// when the reliance is intended (DOCUMENTATION_STANDARD §1, *When to flip*).
// Reporting each link instead would ask the authors of accepted documents to
// remove pointers that are correct.
//
// Only links count, not bare `ADR-NNNN` citations; whether a citation names its
// target's state is a separate check (DL014, pending). Links inside fenced code
// are ignored. A target is known only if the walk reached it, so a run over a
// subtree does not see links that leave it; the gate runs over the whole tree.
// Missing and git-ignored targets are DL007's.
//
// Severity is warn: an ADR stays proposed while it is being revised, and the
// finding marks the point at which that stops being free, not an error.
type RuleDL018 struct{}

func NewRuleDL018() (inst *RuleDL018) {
	inst = &RuleDL018{}
	return
}

func (inst *RuleDL018) Id() (id string) {
	id = "DL018"
	return
}

// dl018Source is one accepted document linking a target, at its first link.
type dl018Source struct {
	path string
	line int32
}

func (inst *RuleDL018) Check(roots []string) iter.Seq2[Finding, error] {
	return func(yield func(Finding, error) bool) {
		// Both maps are keyed by absolute path, so a file reached through two
		// roots, or named from two directories, is one file.
		status := make(map[string]string)
		walked := make(map[string]string)
		linkedFrom := make(map[string][]dl018Source)
		collect := func(path string, _ map[string]struct{}, _ func(Finding, error) bool) (cont bool, err error) {
			cont = true
			var abs string
			abs, err = filepath.Abs(path)
			if err != nil {
				err = eb.Build().Str("path", path).Errorf("DL018 abs: %w", err)
				return
			}
			if _, seen := walked[abs]; seen {
				return
			}
			var data []byte
			data, err = os.ReadFile(path)
			if err != nil {
				err = eb.Build().Str("path", path).Errorf("DL018 read: %w", err)
				return
			}
			meta, body, ok, parseErr := parseMdFrontMatter(data)
			if !ok || parseErr != nil {
				return
			}
			walked[abs] = path
			status[abs] = meta.Status
			if meta.Status != docstd.StatusAccepted {
				return
			}
			lineOffset := frontMatterLineOffset(data, body)
			for _, link := range extractInlineLinks(body) {
				target, local := resolveLocalLink(path, link.URL)
				if !local {
					continue
				}
				var targetAbs string
				targetAbs, err = filepath.Abs(target)
				if err != nil {
					err = eb.Build().Str("path", path).Str("target", target).Errorf("DL018 abs: %w", err)
					return
				}
				if targetAbs == abs {
					continue
				}
				sources := linkedFrom[targetAbs]
				if slices.ContainsFunc(sources, func(s dl018Source) bool { return s.path == path }) {
					continue
				}
				linkedFrom[targetAbs] = append(sources, dl018Source{path: path, line: link.Line + lineOffset})
			}
			return
		}
		for _, err := range runMarkdownCheck("DL018", roots, collect) {
			if err != nil {
				yield(Finding{}, err)
				return
			}
		}

		targets := make([]string, 0, len(linkedFrom))
		for t := range linkedFrom {
			if status[t] == docstd.StatusProposed {
				targets = append(targets, t)
			}
		}
		slices.SortFunc(targets, func(a, b string) int { return strings.Compare(walked[a], walked[b]) })
		for _, t := range targets {
			f := Finding{
				RuleId:   "DL018",
				Severity: FindingSeverityWarn,
				Path:     walked[t],
				Line:     1,
				Col:      1,
				Message:  dl018Message(linkedFrom[t]),
			}
			if !yield(f, nil) {
				return
			}
		}
	}
}

// dl018MaxListed caps the sources a message names; the count covers the rest.
const dl018MaxListed = 5

func dl018Message(sources []dl018Source) (msg string) {
	var b strings.Builder
	b.WriteString("status: proposed, but linked from ")
	b.WriteString(strconv.Itoa(len(sources)))
	if len(sources) == 1 {
		b.WriteString(" accepted doc: ")
	} else {
		b.WriteString(" accepted docs: ")
	}
	for i, s := range sources {
		if i == dl018MaxListed {
			b.WriteString(", +")
			b.WriteString(strconv.Itoa(len(sources) - dl018MaxListed))
			b.WriteString(" more")
			break
		}
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(s.path)
		b.WriteString(":")
		b.WriteString(strconv.Itoa(int(s.line)))
	}
	b.WriteString(" — accepted text now relies on a doc that can still change in place; flip it to accepted when that reliance is intended")
	msg = b.String()
	return
}

// resolveLocalLink resolves a Markdown link URL written in the file at path to
// the filesystem path it names, the way DL007 does: external URLs and pure
// anchors are not local, a fragment or query is dropped, and the remainder is
// percent-decoded and taken relative to the file's directory.
func resolveLocalLink(path string, url string) (target string, local bool) {
	if isExternalUrl(url) {
		return
	}
	clean, anchorOnly := stripUrlFragment(url)
	if anchorOnly || clean == "" {
		return
	}
	clean = percentDecodePath(clean)
	if filepath.IsAbs(clean) {
		target = clean
	} else {
		target = filepath.Join(filepath.Dir(path), clean)
	}
	local = true
	return
}
