package search

import (
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

// Hit is one matched section, ready for a results row: the navigable
// ref (feed it to helphost's OpenRef), the display fields, and a
// context line for body matches. Heading is empty for the doc-level
// section (the region before the first heading), where DocTitle is the
// thing to show.
type Hit struct {
	Ref      help.RefT
	DocTitle string
	DocType  string
	Heading  string
	Level    uint8
	Score    int
	Context  string
	// SpanBytes is the hit section's body size — what the hit "weighs"
	// in [Index.Coverage]. Carried on the hit because a slug is not a
	// reliable key back into the section table (duplicate slugs).
	SpanBytes int
}

// Weights of the per-pattern field tiers (ADR-0164 §SD3). A pattern
// contributes the strongest tier it hit — the tiers do not add up for
// one pattern, so a word occurring in a heading and ten times in the
// body still counts once, at heading strength. The facts-plane
// executor (§SD5) must mirror these values; the golden query set is
// the drift alarm.
const (
	weightTitle   = 8
	weightHeading = 4
	weightBody    = 1
)

// Index is the section-grained scan table over one or more help books
// (ADR-0164 §SD3). Construct with [NewIndex] or [NewIndexBooks]; the
// underlying books are immutable after parse, so an Index never
// invalidates. Search cost is a linear RE2 sweep per call — callers
// re-search on query change, not per frame.
type Index struct {
	docs []docEntry
}

type docEntry struct {
	appId    app.AppIdT
	path     string
	title    string
	docType  string
	src      string
	sections []SectionSpan
}

// SectionSpan is one scannable region of a doc's source: the doc-level
// preamble (slug "", level 0) or one heading's span. Start/End bound
// the region, running from the heading's line start to the next
// section's line start (the marker glyphs of the *next* heading are
// excluded by that next section owning its own line).
//
// Exported for the facts-plane producers (ADR-0164 §SD5): the
// helpsections/adrsections providers slice their corpora with exactly
// this logic, so the two tiers cannot disagree about where a section
// begins.
type SectionSpan struct {
	Slug    string
	Heading string
	Level   uint8
	Start   int
	End     int
}

// NewIndex builds an index over every book in lib. Triggers each
// book's one-shot parse walk (the books cache it; a later HelpHost
// open pays nothing extra).
func NewIndex(lib help.LibraryI) (inst *Index) {
	inst = NewIndexBooks(lib.Books()...)
	return
}

// NewIndexBooks builds an index over the given books, in the given
// order. The single-book form is what play's snippet filter uses.
func NewIndexBooks(books ...help.BookI) (inst *Index) {
	inst = &Index{docs: make([]docEntry, 0, 16)}
	for _, b := range books {
		appId := b.AppId()
		for _, info := range b.Docs() {
			src, ok := b.Source(info.Path)
			if !ok {
				continue
			}
			inst.docs = append(inst.docs, buildDocEntry(appId, info, string(src)))
		}
	}
	return
}

// SliceSections slices a doc's source into its scannable regions
// (ADR-0164 §SD1): one doc-level span from the end of the frontmatter
// block to the first heading's line start, then one span per heading
// running to the next heading's line start. A heading without a byte
// offset (degenerate, see markdown.HeadingInfo) gets an empty region
// at the running cursor — its heading text still participates in
// scoring. Boundaries are monotonically clamped: an offset that walks
// backwards (cannot happen from a well-formed parse, but a slicer must
// not panic on one) folds into the previous region instead of
// inverting a slice.
func SliceSections(src string, secs []help.SectionInfo) (spans []SectionSpan) {
	spans = make([]SectionSpan, 0, len(secs)+1)
	cursor := frontmatterEnd(src)
	bounds := make([]int, len(secs))
	prev := cursor
	for i, s := range secs {
		b := prev
		if s.ByteOffset >= 0 && s.ByteOffset <= len(src) {
			b = max(lineStart(src, s.ByteOffset), prev)
		}
		bounds[i] = b
		prev = b
	}
	firstBound := len(src)
	if len(bounds) > 0 {
		firstBound = bounds[0]
	}
	spans = append(spans, SectionSpan{Start: cursor, End: firstBound})
	for i, s := range secs {
		end := len(src)
		if i+1 < len(bounds) {
			end = bounds[i+1]
		}
		spans = append(spans, SectionSpan{
			Slug:    s.Slug,
			Heading: s.Text,
			Level:   s.Level,
			Start:   bounds[i],
			End:     end,
		})
	}
	return
}

// buildDocEntry wraps SliceSections with the doc identity the scan
// needs.
func buildDocEntry(appId app.AppIdT, info help.DocInfo, src string) (d docEntry) {
	d = docEntry{
		appId:    appId,
		path:     info.Path,
		title:    info.Title,
		docType:  info.Type,
		src:      src,
		sections: SliceSections(src, info.Sections),
	}
	return
}

// Search sweeps the corpus with the battery and returns hits ordered
// by score (descending), stable in corpus order within a score. limit
// caps the result count; <= 0 means unlimited. An empty battery
// returns nil — the "no query" state (Battery.IsZero) renders the
// browse view, not the whole corpus.
func (inst *Index) Search(b Battery, limit int) (hits []Hit) {
	if b.IsZero() {
		return
	}
	for di := range inst.docs {
		d := &inst.docs[di]
		for si := range d.sections {
			h, ok := scoreSection(d, si, &b)
			if !ok {
				continue
			}
			hits = append(hits, h)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return
}

// scoreSection evaluates every battery pattern against one section.
// ok=false when the section does not qualify (no pattern hit, or a
// pattern missed under RequireAll).
func scoreSection(d *docEntry, si int, b *Battery) (h Hit, ok bool) {
	s := &d.sections[si]
	body := d.src[s.Start:s.End]
	score := 0
	context := ""
	for pi := range b.Patterns {
		p := &b.Patterns[pi]
		w := 0
		if si == 0 && p.Matches(d.title) {
			w = weightTitle
		} else if s.Heading != "" && p.Matches(s.Heading) {
			w = weightHeading
		}
		if mStart, mEnd, hit := p.find(body); hit {
			if w < weightBody {
				w = weightBody
			}
			if context == "" {
				context = contextLine(body, mStart, mEnd)
			}
		}
		if w == 0 {
			if b.RequireAll {
				return
			}
			continue
		}
		score += w
	}
	if score == 0 {
		return
	}
	h = Hit{
		Ref:       help.RefT{AppId: d.appId, Doc: d.path, Section: s.Slug},
		DocTitle:  d.title,
		DocType:   d.docType,
		Heading:   s.Heading,
		Level:     s.Level,
		Score:     score,
		Context:   context,
		SpanBytes: s.End - s.Start,
	}
	ok = true
	return
}

// Coverage reports how much of a corpus a selection takes. Bytes are
// the honest "how much" — sections vary hugely in size — and the
// section counts are what a results header can name. Every scannable
// region counts, the doc-level preamble regions included: a preamble
// hit is as real as a heading one.
type Coverage struct {
	SelBytes      int
	TotalBytes    int
	SelSections   int
	TotalSections int
}

// Frac is the byte share, the value a progress bar draws. 0 on an
// empty corpus.
func (inst Coverage) Frac() (f float32) {
	if inst.TotalBytes > 0 {
		f = float32(inst.SelBytes) / float32(inst.TotalBytes)
	}
	return
}

// Coverage sums the corpus share of a hit set against the whole index.
// Search never emits a section twice, so the hits sum without
// deduplication; pass the UNcapped hit list — a display-truncated one
// under-reports (ADR-0164 §SD4).
func (inst *Index) Coverage(hits []Hit) (cov Coverage) {
	for di := range inst.docs {
		d := &inst.docs[di]
		cov.TotalSections += len(d.sections)
		for si := range d.sections {
			cov.TotalBytes += d.sections[si].End - d.sections[si].Start
		}
	}
	cov.SelSections = len(hits)
	for i := range hits {
		cov.SelBytes += hits[i].SpanBytes
	}
	return
}

// DocCoverage sums the share of one doc's body an accepted-slug set
// selects — the snippets filter's view of coverage, where accepted
// already includes descendant expansion and never the doc-level ""
// (ADR-0164 §SD4). Duplicate slugs conflate here exactly as they do in
// the filter that consumes the same set. Zero Coverage when the doc is
// not indexed.
func (inst *Index) DocCoverage(appId app.AppIdT, docPath string, accepted map[string]bool) (cov Coverage) {
	for di := range inst.docs {
		d := &inst.docs[di]
		if d.appId != appId || d.path != docPath {
			continue
		}
		cov.TotalSections = len(d.sections)
		for si := range d.sections {
			s := &d.sections[si]
			span := s.End - s.Start
			cov.TotalBytes += span
			if accepted[s.Slug] && (s.Slug != "" || si == 0) {
				cov.SelBytes += span
				cov.SelSections++
			}
		}
		return
	}
	return
}

// contextLine extracts the line containing [mStart, mEnd) from body,
// trimmed to a display-friendly length around the match. Markdown
// syntax in the line is left as-is — the corpus is prose, and honest
// raw text beats a half-stripped rendering.
func contextLine(body string, mStart int, mEnd int) (line string) {
	ls := strings.LastIndexByte(body[:mStart], '\n') + 1
	le := strings.IndexByte(body[mEnd:], '\n')
	if le < 0 {
		le = len(body)
	} else {
		le += mEnd
	}
	const maxLen = 120
	// Budget what the match itself doesn't consume evenly around it,
	// so a match at the line's end still shows its left neighbourhood.
	if le-ls > maxLen {
		spare := max(maxLen-(mEnd-mStart), 0)
		left := max(mStart-spare/2, ls)
		right := left + maxLen
		if right > le {
			right = le
			left = max(right-maxLen, ls)
		}
		ls, le = left, right
	}
	line = strings.TrimSpace(body[ls:le])
	return
}

// lineStart returns the offset of the first byte of the line
// containing offset. markdown.HeadingInfo.ByteOffset points at the
// heading *text* (after the `## ` marker); section regions want the
// whole heading line, so the marker of section N never trails into
// section N-1's body.
func lineStart(src string, offset int) (start int) {
	start = strings.LastIndexByte(src[:offset], '\n') + 1
	return
}

// frontmatterEnd returns the offset just past the closing `---` line
// of a leading YAML frontmatter block, or 0 when the source has none.
// Frontmatter fields are index metadata (DocInfo.Title/Type), not
// body text — a query for "draft" should not hit every doc whose
// status field says so (ADR-0164 §SD1).
func frontmatterEnd(src string) (end int) {
	rest, found := strings.CutPrefix(src, "---\n")
	if !found {
		if rest, found = strings.CutPrefix(src, "---\r\n"); !found {
			return
		}
	}
	for _, closer := range []string{"\n---\n", "\n---\r\n"} {
		if idx := strings.Index(rest, closer); idx >= 0 {
			candidate := len(src) - len(rest) + idx + len(closer)
			if end == 0 || candidate < end {
				end = candidate
			}
		}
	}
	return
}

// ExpandDescendants widens an accepted-slug set to include every
// section nested under an accepted one (deeper level, in document
// order, until the next heading at the accepted level or shallower).
// This is what makes filtering to a matched H2 keep its H3 children
// visible (ADR-0164 §SD4). Duplicate slugs conflate, as everywhere
// slugs are keys.
func ExpandDescendants(sections []help.SectionInfo, accepted map[string]bool) (out map[string]bool) {
	out = make(map[string]bool, len(accepted))
	var active uint8
	for i := range sections {
		s := &sections[i]
		if active != 0 && s.Level <= active {
			active = 0
		}
		if accepted[s.Slug] {
			out[s.Slug] = true
			if active == 0 {
				active = s.Level
			}
			continue
		}
		if active != 0 {
			out[s.Slug] = true
		}
	}
	return
}
