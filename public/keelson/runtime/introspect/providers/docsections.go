package providers

// The section-grained documentation tables (ADR-0164 §SD5):
// `helpsections` (one row per section of every registered help book)
// and `adrsections` (one row per section of every decision in the ADR
// corpus). Both exist for the docsearch macro's UNION, but they are
// ordinary tables — a hand-written query against either works exactly
// like the macro's expansion does.
//
// Slicing is help/search.SliceSections — the same code the embedded
// search tier scans — so the two tiers cannot disagree about where a
// section begins (ADR-0164 §SD1). Every row carries its canonical
// docref string, frozen in help/docref, which is what makes a hit row
// navigable from any surface.

import (
	"strings"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/gov/adrcorpus"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/help/docref"
	"github.com/stergiotis/boxer/public/keelson/runtime/help/search"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// sectionRow is the common per-section shape both providers emit; the
// per-corpus identity columns differ, the section columns do not.
type sectionRow struct {
	appId   string // helpsections only
	doc     string // helpsections: doc path; adrsections: zero-padded num
	num     int    // adrsections only
	path    string // adrsections only: repo-relative source path
	title   string // doc/ADR title
	kind    string // helpsections: Diátaxis type; adrsections: frontmatter status
	section string // heading slug, "" = doc-level region
	heading string
	level   uint8
	body    string
	ref     string
}

// --- helpsections -----------------------------------------------------------

// helpsectionsProvider exposes every registered help book section-
// grained as keelson.helpsections. Live: the library can gain books
// until every app has registered, and the walk over already-parsed
// books is cheap (BookI caches its parse for the book's lifetime).
type helpsectionsProvider struct{}

func (helpsectionsProvider) Name() string                         { return "helpsections" }
func (helpsectionsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (helpsectionsProvider) Schema() *arrow.Schema                { return helpsectionsTable(nil).Schema() }

func (helpsectionsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	rows := helpSectionRows(help.DefaultLibrary)
	return helpsectionsTable(rows).Build(proj, len(rows)), nil
}

// helpSectionRows walks lib's books into section rows. Separate from
// Snapshot so a test can feed a fixture library.
func helpSectionRows(lib help.LibraryI) (rows []sectionRow) {
	for _, b := range lib.Books() {
		appId := string(b.AppId())
		for _, info := range b.Docs() {
			src, ok := b.Source(info.Path)
			if !ok {
				continue
			}
			text := string(src)
			for _, sp := range search.SliceSections(text, info.Sections) {
				rows = append(rows, sectionRow{
					appId:   appId,
					doc:     info.Path,
					title:   info.Title,
					kind:    info.Type,
					section: sp.Slug,
					heading: sp.Heading,
					level:   sp.Level,
					body:    text[sp.Start:sp.End],
					ref:     docref.FormatHelp(appId, info.Path, sp.Slug),
				})
			}
		}
	}
	return
}

func helpsectionsTable(rows []sectionRow) *introspect.Table {
	return introspect.NewTable().
		String("app_id", func(i int) string { return rows[i].appId }).
		String("doc", func(i int) string { return rows[i].doc }).
		String("section", func(i int) string { return rows[i].section }).
		String("heading", func(i int) string { return rows[i].heading }).
		Int32("level", func(i int) int32 { return int32(rows[i].level) }).
		String("doc_title", func(i int) string { return rows[i].title }).
		String("doc_type", func(i int) string { return rows[i].kind }).
		String("body@text/markdown", func(i int) string { return rows[i].body }).
		String("ref", func(i int) string { return rows[i].ref })
}

// --- adrsections ------------------------------------------------------------

// adrsectionsProvider exposes each decision section-grained as
// keelson.adrsections, keyed like `adrcontent` by the number that
// joins back to `adr`.
//
// Like adrcontent, its cost tracks the corpus: naming it re-reads
// every decision from disk (Live — an ADR edited mid-session shows its
// new text on the next query). Unlike adrcontent, it must also parse
// the markdown for heading offsets, so parses are memoised per
// decision against the exact content bytes: an unchanged file costs a
// string compare, an edited one re-parses alone.
type adrsectionsProvider struct{}

func (adrsectionsProvider) Name() string                         { return "adrsections" }
func (adrsectionsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (adrsectionsProvider) Schema() *arrow.Schema                { return adrsectionsTable(nil).Schema() }

func (adrsectionsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	rows := adrSectionRows(adrcorpus.LoadContents())
	return adrsectionsTable(rows).Build(proj, len(rows)), nil
}

// adrParseCache memoises section slicing per decision. Keyed by Num;
// an entry revalidates by comparing the content string — cheap, exact,
// and free of mtime races. Bounded by the corpus size.
var adrParseCache sync.Map // int → *adrParsed

type adrParsed struct {
	content string
	title   string
	status  string
	spans   []search.SectionSpan
}

func adrSectionRows(contents []adrcorpus.AdrContent) (rows []sectionRow) {
	for _, c := range contents {
		p := adrParse(c)
		for _, sp := range p.spans {
			rows = append(rows, sectionRow{
				num:     c.Num,
				path:    c.Path,
				title:   p.title,
				kind:    p.status,
				section: sp.Slug,
				heading: sp.Heading,
				level:   sp.Level,
				body:    c.Content[sp.Start:sp.End],
				ref:     docref.FormatAdr(c.Num, sp.Slug),
			})
		}
	}
	return
}

func adrParse(c adrcorpus.AdrContent) (p *adrParsed) {
	if cached, ok := adrParseCache.Load(c.Num); ok {
		p = cached.(*adrParsed)
		if p.content == c.Content {
			return
		}
	}
	doc := markdown.Parse([]byte(c.Content))
	heads := doc.Headings()
	secs := make([]help.SectionInfo, len(heads))
	title := ""
	for i, h := range heads {
		secs[i] = help.SectionInfo{Slug: h.Slug, Text: h.Text, Level: h.Level, ByteOffset: h.ByteOffset}
		if title == "" && h.Level == 1 {
			title = h.Text
		}
	}
	status := ""
	if fm := doc.Frontmatter(); fm != nil {
		if v, ok := fm.Get("status"); ok {
			if s, ok2 := v.(string); ok2 {
				status = s
			}
		}
	}
	p = &adrParsed{
		content: c.Content,
		title:   strings.TrimSpace(title),
		status:  status,
		spans:   search.SliceSections(c.Content, secs),
	}
	adrParseCache.Store(c.Num, p)
	return
}

func adrsectionsTable(rows []sectionRow) *introspect.Table {
	return introspect.NewTable().
		Int32("num", func(i int) int32 { return int32(rows[i].num) }).
		String("section", func(i int) string { return rows[i].section }).
		String("heading", func(i int) string { return rows[i].heading }).
		Int32("level", func(i int) int32 { return int32(rows[i].level) }).
		String("title", func(i int) string { return rows[i].title }).
		String("status", func(i int) string { return rows[i].kind }).
		String("body@text/markdown", func(i int) string { return rows[i].body }).
		String("ref", func(i int) string { return rows[i].ref }).
		String("path", func(i int) string { return rows[i].path })
}
