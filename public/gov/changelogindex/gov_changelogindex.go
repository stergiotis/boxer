// Package changelogindex generates doc/changelog/INDEX.md — a cross-window
// table of contents over the hand-compiled changelog entries.
//
// The judgment-heavy summarisation happens once per window, in each entry's
// "window in brief", written at compile time and human-reviewed when the
// entry flips to stable. This package only extracts: every entry, newest
// first, with its thematic section headings linked to their GitHub anchors.
// The machinery sections every entry repeats (coverage, breaking changes, …)
// are skipped; see machineryHeadings. The output is a pure function of the
// entry files — no timestamps — so TestIndexIsCurrent can require byte
// equality against the committed file.
package changelogindex

//go:generate sh -c "go run -tags=\"$(cat ../../../tags)\" github.com/stergiotis/boxer/public/app gov changelogindex --dir ../../../doc/changelog"

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Topic is one thematic h2 heading of an entry.
type Topic struct {
	Text   string // heading text, verbatim
	Anchor string // GitHub anchor slug of Text
}

// Entry is one window-bounded changelog file.
type Entry struct {
	FileName string // e.g. "2026-08-16--2026-08-29.md"
	Window   string // e.g. "2026-08-16 – 2026-08-29"
	Topics   []Topic
}

// entryFileRe matches the entry naming rule from doc/changelog/README.md:
// one file per window, named by the window bounds.
var entryFileRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})--(\d{4}-\d{2}-\d{2})\.md$`)

// machineryHeadings are prefixes of the h2 sections every entry repeats.
// They carry no cross-window discrimination, so the index omits them.
// Matched by prefix so dashed variants ("Proposed, not built — and built,
// not accepted") stay covered. A new machinery section introduced by the
// compilation process belongs here; a thematic heading never does.
var machineryHeadings = []string{
	"The window in brief",
	"Tags and ADR status",
	"Coverage and continuation",
	"Breaking changes",
	"Reading surface",
	"Retired within the window",
	"Added and removed within the window",
	"Proposed, not built",
	"Other new surface",
	"Smaller additions",
	"Documentation",
}

// IsMachineryHeading reports whether h is one of the repeated per-entry
// machinery sections rather than a thematic one.
func IsMachineryHeading(h string) bool {
	for _, m := range machineryHeadings {
		if strings.HasPrefix(h, m) {
			return true
		}
	}
	return false
}

// Slug returns the GitHub anchor for a heading: lowercase; letters, digits
// and underscores kept; spaces and hyphens become/stay hyphens; everything
// else dropped. Consecutive hyphens are not collapsed — GitHub does not
// collapse them either.
func Slug(heading string) string {
	var b strings.Builder
	b.Grow(len(heading))
	for _, r := range heading {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			b.WriteRune(unicode.ToLower(r))
		case r == ' ' || r == '-':
			b.WriteByte('-')
		}
	}
	return b.String()
}

// ParseTopics scans markdown content for thematic h2 headings, skipping
// fenced code blocks and machinery sections.
func ParseTopics(content string) (out []Topic) {
	out = make([]Topic, 0, 16)
	inFence := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		h, ok := strings.CutPrefix(line, "## ")
		if !ok {
			continue
		}
		h = strings.TrimSpace(h)
		if h == "" || IsMachineryHeading(h) {
			continue
		}
		out = append(out, Topic{Text: h, Anchor: Slug(h)})
	}
	return
}

// CollectEntries reads every window-bounded entry in dir, newest first.
func CollectEntries(dir string) (out []Entry, err error) {
	var des []os.DirEntry
	des, err = os.ReadDir(dir)
	if err != nil {
		err = eb.Build().Str("dir", dir).Errorf("reading changelog directory: %w", err)
		return
	}
	out = make([]Entry, 0, len(des))
	for _, de := range des {
		m := entryFileRe.FindStringSubmatch(de.Name())
		if m == nil {
			continue
		}
		var content []byte
		content, err = os.ReadFile(filepath.Join(dir, de.Name()))
		if err != nil {
			err = eb.Build().Str("file", de.Name()).Errorf("reading changelog entry: %w", err)
			return
		}
		out = append(out, Entry{
			FileName: de.Name(),
			Window:   m[1] + " – " + m[2],
			Topics:   ParseTopics(string(content)),
		})
	}
	slices.SortFunc(out, func(a, b Entry) int {
		return strings.Compare(b.FileName, a.FileName) // newest first
	})
	return
}

// Render returns the full INDEX.md content for entries.
func Render(entries []Entry) []byte {
	var b bytes.Buffer
	b.WriteString(`---
type: reference
audience: contributor
status: draft
generated: true
generator: boxer gov changelogindex
---

> **Status: draft — pre-human-review.** Machine-extracted index over the
> changelog entries beside it; regenerate with ` + "`boxer gov changelogindex`" + `
> after compiling an entry. Review happens on the entries, not here.

# Changelog index

<!-- Generated by ` + "`boxer gov changelogindex`" + `. Do not edit by hand. -->

The thematic section headings of every changelog entry, newest first, linked
into the entries. The machinery sections every entry repeats (coverage,
breaking changes, …) are omitted; the compilation process is described in
[README.md](./README.md).
`)
	for i := range entries {
		e := &entries[i]
		b.WriteString("\n## [" + e.Window + "](./" + e.FileName + ")\n")
		if len(e.Topics) > 0 {
			b.WriteString("\n")
		}
		for _, t := range e.Topics {
			b.WriteString("- [" + t.Text + "](./" + e.FileName + "#" + t.Anchor + ")\n")
		}
	}
	return b.Bytes()
}

// Generate writes the index for dir to outPath.
func Generate(dir string, outPath string) (err error) {
	var entries []Entry
	entries, err = CollectEntries(dir)
	if err != nil {
		return
	}
	err = os.WriteFile(outPath, Render(entries), 0o644)
	if err != nil {
		err = eb.Build().Str("out", outPath).Errorf("writing changelog index: %w", err)
	}
	return
}

// Check reports an error when outPath does not hold exactly what Generate
// would write for dir — the index has drifted from the entries.
func Check(dir string, outPath string) (err error) {
	var entries []Entry
	entries, err = CollectEntries(dir)
	if err != nil {
		return
	}
	var have []byte
	have, err = os.ReadFile(outPath)
	if err != nil {
		err = eb.Build().Str("out", outPath).Errorf("reading changelog index: %w", err)
		return
	}
	if !bytes.Equal(have, Render(entries)) {
		err = eb.Build().Str("out", outPath).Errorf("changelog index drifted from the entries; regenerate with `boxer gov changelogindex`")
	}
	return
}
