package workstatelet

import (
	"io/fs"
	"sort"
	"strings"
	"sync"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// idPrefix is what a minted app id starts with. The tail is "ws-" and the
// slug, so the subject alias a document is launched by cannot be the alias of
// the app its windows show.
const idPrefix = "github.com/stergiotis/boxer/public/keelson/runtime/workstatelet/ws-"

// manifestVersion is the version a minted manifest carries.
const manifestVersion = "0.1.0"

// defaultIcon is the launcher glyph of a document that names none. It is a
// Phosphor glyph, a font the client loads; a document's own `icon` should be
// one too, since a glyph the text font lacks is drawn by whichever fallback
// face answers.
const defaultIcon = icons.PhLayout

// AppIdOf is the app id the document named slug is minted under.
func AppIdOf(slug string) (id app.AppIdT) {
	return app.AppIdT(idPrefix + slug)
}

// OriginE says where a minted document came from.
type OriginE uint8

const (
	// OriginBook is a document of a registered book: committed, reviewed.
	OriginBook OriginE = 1
	// OriginStored is a document a person captured from the desktop.
	OriginStored OriginE = 2
)

var AllOrigins = []OriginE{OriginBook, OriginStored}

// String is the origin as keelson('workstatelets') spells it.
func (inst OriginE) String() (s string) {
	switch inst {
	case OriginBook:
		s = "book"
	case OriginStored:
		s = "stored"
	default:
		s = "unknown"
	}
	return
}

// Entry is one minted document.
type Entry struct {
	AppId  app.AppIdT
	Origin OriginE
	// Book is the id of the book the document came from; empty for a stored
	// one.
	Book string
	Doc  Doc
}

// Catalog is the set of minted documents, by the app id each was minted
// under. The window host reads a document out of it when the id is opened.
// Safe for concurrent use.
type Catalog struct {
	mu      sync.RWMutex
	entries map[app.AppIdT]Entry
}

// NewCatalog returns an empty catalog. Tests use it for isolation;
// production code uses DefaultCatalog.
func NewCatalog() (inst *Catalog) {
	return &Catalog{entries: make(map[app.AppIdT]Entry, 8)}
}

// DefaultCatalog is the process-wide catalog, beside app.DefaultRegistry.
var DefaultCatalog = NewCatalog()

// Lookup returns the document minted under id.
func (inst *Catalog) Lookup(id app.AppIdT) (doc Doc, ok bool) {
	inst.mu.RLock()
	e, ok := inst.entries[id]
	inst.mu.RUnlock()
	doc = e.Doc
	return
}

// Entry returns the catalog's record of id.
func (inst *Catalog) Entry(id app.AppIdT) (e Entry, ok bool) {
	inst.mu.RLock()
	e, ok = inst.entries[id]
	inst.mu.RUnlock()
	return
}

// Entries lists the minted documents, ordered by app id.
func (inst *Catalog) Entries() (out []Entry) {
	inst.mu.RLock()
	out = make([]Entry, 0, len(inst.entries))
	for _, e := range inst.entries {
		out = append(out, e)
	}
	inst.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].AppId < out[j].AppId })
	return
}

// ManifestOf is the launcher entry of doc (ADR-0369 §SD3): a windowed
// manifest of kind workstatelet with no capabilities, no launch kind and no
// state, since nothing of it ever mounts in a window host.
func ManifestOf(doc Doc) (m app.Manifest) {
	icon := doc.Icon
	if icon == "" {
		icon = defaultIcon
	}
	m = app.Manifest{
		Id:       AppIdOf(doc.Slug),
		Version:  manifestVersion,
		Display:  doc.Title,
		Summary:  doc.Summary,
		Icon:     icon,
		Topics:   doc.Topics,
		Keywords: doc.Keywords,
		Kind:     app.KindWorkstatelet,
		Surface:  app.SurfaceWindowed,
		SurfaceHints: app.SurfaceHints{
			ScreenshotFlags: app.ScreenshotFlagSkipInTour,
		},
	}
	return
}

// Mint registers doc's manifest in reg and records the document, so an open
// of the id finds it. The registry's refusal — a duplicate id, an alias
// another app holds — is returned and nothing is recorded.
func (inst *Catalog) Mint(reg *app.Registry, doc Doc, origin OriginE, book string) (id app.AppIdT, err error) {
	if err = doc.Validate(); err != nil {
		err = eh.Errorf("mint: %w", err)
		return
	}
	m := ManifestOf(doc)
	id = m.Id
	card := doc
	err = reg.RegisterFactory(m, func() (a app.AppI, ctorErr error) {
		a = newCard(m, card)
		return
	})
	if err != nil {
		err = eb.Build().Str("id", string(id)).Errorf("mint: %w", err)
		return
	}
	inst.mu.Lock()
	inst.entries[id] = Entry{AppId: id, Origin: origin, Book: book, Doc: doc}
	inst.mu.Unlock()
	return
}

// Replace swaps the document a minted id opens. The manifest stays as
// registered until the next boot, the applet store's rule for a stored
// document saved again.
func (inst *Catalog) Replace(doc Doc) (err error) {
	if err = doc.Validate(); err != nil {
		err = eh.Errorf("replace: %w", err)
		return
	}
	id := AppIdOf(doc.Slug)
	inst.mu.Lock()
	defer inst.mu.Unlock()
	e, ok := inst.entries[id]
	if !ok {
		err = eb.Build().Str("id", string(id)).Errorf("replace: no minted document under this id")
		return
	}
	e.Doc = doc
	inst.entries[id] = e
	return
}

type registeredBook struct {
	id     string
	fsys   fs.FS
	topics []app.TopicT
}

var (
	booksMu sync.Mutex
	books   []registeredBook
)

// RegisterBook contributes a book of workstatelet documents: every markdown
// file of fsys is one document and its base name the slug. Packages call it
// from init; MintManifests reads every registered book. topics files a
// document that names none.
func RegisterBook(id string, fsys fs.FS, topics []app.TopicT) (err error) {
	if id == "" || fsys == nil || len(topics) == 0 {
		err = eb.Build().Str("id", id).Errorf("a book needs an id, a file system and default topics")
		return
	}
	booksMu.Lock()
	defer booksMu.Unlock()
	for _, b := range books {
		if b.id == id {
			err = eb.Build().Str("id", id).Errorf("duplicate book id")
			return
		}
	}
	books = append(books, registeredBook{id: id, fsys: fsys, topics: topics})
	return
}

func snapshotBooks() (out []registeredBook) {
	booksMu.Lock()
	out = append(out, books...)
	booksMu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return
}

// MintManifests mints every document of every registered book into
// app.DefaultRegistry and DefaultCatalog. A shell calls it once, before
// launch resolution, so a document's alias resolves as an app's does. A
// document that does not parse mints nothing and its siblings still do.
func MintManifests(logger zerolog.Logger) (minted int, errs []error) {
	return MintBooks(app.DefaultRegistry, DefaultCatalog, logger)
}

// MintBooks is MintManifests over a registry and a catalog of the caller's.
func MintBooks(reg *app.Registry, cat *Catalog, logger zerolog.Logger) (minted int, errs []error) {
	for _, b := range snapshotBooks() {
		docs, perrs := ParseBook(b.id, b.fsys, b.topics)
		errs = append(errs, perrs...)
		for _, doc := range docs {
			id, err := cat.Mint(reg, doc, OriginBook, b.id)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			minted++
			logger.Debug().Str("id", string(id)).Msg("workstatelet: minted")
		}
	}
	return
}

// ParseBook reads every markdown document of a book, in path order. The
// corpus test of a book calls it and expects no error.
func ParseBook(bookId string, fsys fs.FS, topics []app.TopicT) (docs []Doc, errs []error) {
	walkErr := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			errs = append(errs, werr)
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		src, rerr := fs.ReadFile(fsys, path)
		if rerr != nil {
			errs = append(errs, eb.Build().Str("book", bookId).Str("path", path).Errorf("read document: %w", rerr))
			return nil
		}
		base := path
		if i := strings.LastIndexByte(base, '/'); i >= 0 {
			base = base[i+1:]
		}
		doc, perr := Parse(strings.TrimSuffix(base, ".md"), src, topics)
		if perr != nil {
			errs = append(errs, eb.Build().Str("book", bookId).Str("path", path).Errorf("parse document: %w", perr))
			return nil
		}
		docs = append(docs, doc)
		return nil
	})
	if walkErr != nil {
		errs = append(errs, walkErr)
	}
	return
}
