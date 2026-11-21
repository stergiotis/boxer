package notebooklet

import (
	"embed"
	"errors"
	"io/fs"
	"sort"
	"strings"
	"sync"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/apps/notebook"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/registration"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook/samples"
)

// helpFS embeds the help book every notebooklet carries.
//
//go:embed help/*.md
var helpFS embed.FS

// The sample notebooks are the book this package registers: their records
// make their own rows, so each runs live against any ClickHouse.
func init() {
	registration.Fail("notebooklet.RegisterBook", RegisterBook("samples", samples.FS(), []app.TopicT{app.TopicData}))
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

// RegisterBook contributes a book of notebooks: every markdown file of fsys,
// at any depth, that holds a record, with its stored results beside it
// where it has any. Packages call it from init; [MintManifests] reads every
// registered book. topics classifies the book's notebooks.
func RegisterBook(id string, fsys fs.FS, topics []app.TopicT) (err error) {
	if id == "" || fsys == nil || len(topics) == 0 {
		return eb.Build().Str("id", id).Errorf("notebooklet: RegisterBook: a book needs an id, a file system and topics")
	}
	booksMu.Lock()
	defer booksMu.Unlock()
	for _, b := range books {
		if b.id == id {
			return eb.Build().Str("id", id).Errorf("notebooklet: RegisterBook: duplicate book id")
		}
	}
	books = append(books, registeredBook{id: id, fsys: fsys, topics: topics})
	return
}

func snapshotBooks() (out []registeredBook) {
	booksMu.Lock()
	defer booksMu.Unlock()
	out = append(out, books...)
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return
}

// MintManifests registers one app per notebook of every registered book.
// The shell calls it once, before launch resolution, so a notebooklet's
// alias resolves like any app's. A document that does not read mints
// nothing and its siblings still do; the corpus test keeps one off main.
func MintManifests(logger zerolog.Logger) (minted int, errs []error) {
	return mintBooks(app.DefaultRegistry, logger, snapshotBooks())
}

func mintBooks(reg *app.Registry, logger zerolog.Logger, snapshot []registeredBook) (minted int, errs []error) {
	for _, b := range snapshot {
		defs, perrs := parseBook(b)
		errs = append(errs, perrs...)
		for _, d := range defs {
			def := d
			if rerr := reg.RegisterFactory(def.Manifest, func() (a app.AppI, ctorErr error) {
				return newWindow(def), nil
			}); rerr != nil {
				errs = append(errs, eb.Build().Str("id", string(def.Manifest.Id)).Errorf("notebooklet: register: %w", rerr))
				continue
			}
			minted++
			logger.Debug().Str("id", string(def.Manifest.Id)).Msg("notebooklet: minted")
		}
	}
	return
}

// parseBook reads every notebook of a book, sorted by path. A markdown file
// that holds no record is not a notebook and is passed over.
func parseBook(b registeredBook) (defs []*Def, errs []error) {
	err := fs.WalkDir(b.fsys, ".", func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			errs = append(errs, werr)
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		src, rerr := fs.ReadFile(b.fsys, p)
		if rerr != nil {
			errs = append(errs, rerr)
			return nil
		}
		def, perr := ParseDocSource(b.id, p, src, b.fsys, b.topics)
		switch {
		case errors.Is(perr, ErrNotANotebook):
		case perr != nil:
			errs = append(errs, perr)
		default:
			defs = append(defs, def)
		}
		return nil
	})
	if err != nil {
		errs = append(errs, err)
	}
	return
}

// Find is the notebook of the registered books whose slug is slug. It
// parses the books on each call; a host calls it once, when it builds its
// pane.
func Find(slug string) (def *Def, err error) {
	for _, b := range snapshotBooks() {
		defs, _ := parseBook(b)
		for _, d := range defs {
			if d.Slug == slug {
				return d, nil
			}
		}
	}
	return nil, eb.Build().Str("slug", slug).Errorf("notebooklet: no registered notebook has this slug")
}

// Embed is a pane over the registered notebook slug, inside the host cfg
// names (ADR-0380 §SD5): what a notebooklet's window draws, in a region of
// the host's frame. An empty cfg.Key takes the slug; cfg.Files is the
// notebook's directory in its book. The host declares notebook.EmbedCaps.
func Embed(slug string, cfg notebook.EmbedConfig) (pane *notebook.EmbeddedPane, err error) {
	def, err := Find(slug)
	if err != nil {
		return
	}
	if cfg.Key == "" {
		cfg.Key = slug
	}
	cfg.Files = def.Files
	return notebook.NewEmbedded(def.Text, cfg)
}
