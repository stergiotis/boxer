package help

import (
	"io/fs"
	"sort"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// SectionInfo describes one top-level heading inside a help document.
// Slug matches [markdown.SlugHeading] so [RefT.Section] values resolve
// to the same key the markdown widget consumes for in-doc anchors.
type SectionInfo struct {
	Slug  string
	Text  string
	Level uint8
}

// DocInfo is the static metadata of one help document. Populated at
// first-parse time and cached on the [BookI] for the book's lifetime.
//
// Title resolution order: frontmatter `title:` → first H1 → filename
// leaf. Type and Status come from the Diátaxis-mandated frontmatter
// keys ([CLAUDE.md]); empty strings when the doc omits them. Sections
// is the in-document heading list in document order, suitable as a
// TOC sidebar source.
type DocInfo struct {
	Path     string
	Title    string
	Type     string
	Status   string
	Sections []SectionInfo
}

// BookI is the parsed help corpus for one app. Implementations are
// lazy: the first call to [BookI.Docs], [BookI.Doc], or
// [BookI.HasSection] walks the fs.FS, parses every .md file, and
// caches the result for the book's lifetime. Subsequent calls hit the
// cache.
//
// Failures during the walk (missing FS entries, parse errors) are
// logged at Warn but do not poison the cache — the affected file is
// simply absent from [BookI.Docs]. Callers that need hard-fail
// semantics should use the [NewBook] error return on construction (a
// nil fs.FS) and treat per-file gaps as drift to be fixed in CI.
type BookI interface {
	// AppId returns the app this book belongs to. Stable across the
	// book's lifetime.
	AppId() (id app.AppIdT)
	// Docs returns the indexed documents in path-sorted order. Triggers
	// a one-shot walk + parse on first call.
	Docs() (docs []DocInfo)
	// Doc returns the parsed markdown document and its DocInfo for the
	// given FS-relative path minus the `.md` suffix (e.g. `"overview"`
	// or `"howto/replay"`). ok=false when the path is not indexed.
	Doc(docPath string) (doc *markdown.Doc, info DocInfo, ok bool)
	// Source returns the raw markdown bytes the doc was parsed from.
	// Consumers that want to display a "view source" toggle (such as
	// the HelpHost app) read from here and feed the bytes through
	// codeview.PrepareMarkdown for syntax-highlighted rendering. The
	// returned slice is owned by the book and MUST NOT be mutated.
	// ok=false when the path is not indexed.
	Source(docPath string) (src []byte, ok bool)
	// HasSection reports whether the document at docPath contains a
	// heading whose slug matches section. ok=false when the doc is
	// absent or the section is unknown. Used by [RefT] consumers to
	// validate cross-document links at write time.
	HasSection(docPath string, section string) (ok bool)
	// Validate reports documentation-standard front-matter conformance
	// problems across every indexed document, in path-sorted order (empty
	// when the book conforms). Operator-facing, so `type: adr` is rejected;
	// see [ValidateDocInfo] for the per-document check. Triggers the
	// one-shot walk on first call, like the other index methods.
	Validate() (problems []Problem)
}

// NewBook constructs a [BookI] over fsys. The book holds the fs.FS
// reference but does no I/O until one of the index methods is called;
// callers can therefore construct books cheaply at init() and amortise
// the parse cost across actual help opens.
//
// Returns an error only when fsys is nil — every other failure mode
// (missing files, parse errors, no .md content) degrades to an empty
// index that the consumer can render as "no help available for this
// app".
func NewBook(id app.AppIdT, fsys fs.FS) (b BookI, err error) {
	if fsys == nil {
		err = eb.Build().Str("appid", string(id)).Errorf("help.NewBook: nil fs.FS")
		return
	}
	b = &book{appId: id, fsys: fsys}
	return
}

// parsedDoc is the cache entry: full DocInfo + the parsed markdown
// document + the raw source bytes. Stored by docPath in book.parsed;
// entries survive for the book's lifetime. The src copy is small
// (typical help doc is <16 KB) and is what powers [BookI.Source] for
// the HelpHost's "view source" toggle without re-reading the fs.FS.
type parsedDoc struct {
	info DocInfo
	doc  *markdown.Doc
	src  []byte
}

type book struct {
	appId app.AppIdT
	fsys  fs.FS

	once   sync.Once
	parsed map[string]*parsedDoc
	index  []DocInfo
}

var _ BookI = (*book)(nil)

func (inst *book) AppId() (id app.AppIdT) {
	id = inst.appId
	return
}

func (inst *book) Docs() (docs []DocInfo) {
	inst.ensureIndex()
	docs = inst.index
	return
}

func (inst *book) Doc(docPath string) (doc *markdown.Doc, info DocInfo, ok bool) {
	inst.ensureIndex()
	p, exists := inst.parsed[docPath]
	if !exists {
		return
	}
	doc = p.doc
	info = p.info
	ok = true
	return
}

func (inst *book) Source(docPath string) (src []byte, ok bool) {
	inst.ensureIndex()
	p, exists := inst.parsed[docPath]
	if !exists {
		return
	}
	src = p.src
	ok = true
	return
}

func (inst *book) HasSection(docPath string, section string) (ok bool) {
	inst.ensureIndex()
	p, exists := inst.parsed[docPath]
	if !exists {
		return
	}
	for i := range p.info.Sections {
		if p.info.Sections[i].Slug == section {
			ok = true
			return
		}
	}
	return
}

// ensureIndex walks the fs.FS, parses every .md file, and builds the
// path-sorted DocInfo slice. Guarded by sync.Once — subsequent calls
// are zero-cost lookups against the populated maps.
func (inst *book) ensureIndex() {
	inst.once.Do(func() {
		inst.parsed = make(map[string]*parsedDoc, 8)
		walkErr := fs.WalkDir(inst.fsys, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				log.Warn().Err(err).Str("appid", string(inst.appId)).Str("path", p).
					Msg("help.book: walk error, skipping subtree")
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(p, ".md") {
				return nil
			}
			src, readErr := fs.ReadFile(inst.fsys, p)
			if readErr != nil {
				log.Warn().Err(readErr).Str("appid", string(inst.appId)).Str("path", p).
					Msg("help.book: read failed, skipping doc")
				return nil
			}
			md := markdown.Parse(src, markdown.WithResolver(NewFSImageResolver(inst.fsys)))
			docPath := strings.TrimSuffix(p, ".md")
			info := buildDocInfo(docPath, md)
			for _, prob := range ValidateDocInfo(info) {
				log.Warn().Str("appid", string(inst.appId)).Str("path", prob.DocPath).
					Str("field", prob.Field).Str("value", prob.Value).Str("detail", prob.Message).
					Msg("help.book: front-matter does not conform to the documentation standard")
			}
			inst.parsed[docPath] = &parsedDoc{info: info, doc: md, src: src}
			return nil
		})
		if walkErr != nil {
			log.Warn().Err(walkErr).Str("appid", string(inst.appId)).
				Msg("help.book: WalkDir terminated early")
		}
		inst.index = make([]DocInfo, 0, len(inst.parsed))
		for _, p := range inst.parsed {
			inst.index = append(inst.index, p.info)
		}
		sort.Slice(inst.index, func(i, j int) bool { return inst.index[i].Path < inst.index[j].Path })
	})
}

// buildDocInfo derives a DocInfo from a parsed markdown.Doc. Title
// resolves in the priority described on DocInfo; Sections mirrors the
// document's heading list from [markdown.Doc.Headings] in document
// order — the markdown package already walks the AST for headings, so
// help reuses that output rather than parsing the source a second time.
func buildDocInfo(docPath string, md *markdown.Doc) (info DocInfo) {
	info.Path = docPath
	if headings := md.Headings(); len(headings) > 0 {
		info.Sections = make([]SectionInfo, len(headings))
		for i := range headings {
			info.Sections[i] = SectionInfo{
				Slug:  headings[i].Slug,
				Text:  headings[i].Text,
				Level: headings[i].Level,
			}
		}
	}
	if fm := md.Frontmatter(); fm != nil {
		if v, ok := fm.Get("title"); ok {
			if s, ok2 := v.(string); ok2 {
				info.Title = s
			}
		}
		if v, ok := fm.Get("type"); ok {
			if s, ok2 := v.(string); ok2 {
				info.Type = s
			}
		}
		if v, ok := fm.Get("status"); ok {
			if s, ok2 := v.(string); ok2 {
				info.Status = s
			}
		}
	}
	if info.Title == "" {
		for i := range info.Sections {
			if info.Sections[i].Level == 1 {
				info.Title = info.Sections[i].Text
				break
			}
		}
	}
	if info.Title == "" {
		info.Title = pathLeaf(docPath)
	}
	return
}

// pathLeaf returns the final '/'-separated segment of p (or p itself
// when there is no '/'). Used as the last-resort title fallback when
// neither frontmatter nor an H1 supplies one.
func pathLeaf(p string) (leaf string) {
	leaf = p
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			leaf = p[i+1:]
			return
		}
	}
	return
}
