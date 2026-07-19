package sqlapplet

import (
	"embed"
	"io/fs"
	"sort"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// bookFS embeds the host's own starter book (apps/sqlapplet/book/*.md) —
// the dogfood corpus over the keelson introspection tables.
//
//go:embed book
var bookFS embed.FS

func init() {
	if err := RegisterBook("sqlapplet", help.MustSub(bookFS, "book")); err != nil {
		log.Warn().Err(err).Msg("sqlapplet: failed to register starter book")
	}
}

// registeredBook is one contributed applet corpus.
type registeredBook struct {
	id   string
	fsys fs.FS
}

var (
	booksMu sync.Mutex
	books   []registeredBook
)

// RegisterBook contributes an applet book: an fs.FS of markdown documents in
// the ADR-0132 §SD1 shape. Packages call it from init (the help-facility
// pattern); [MintManifests] later parses every registered book. The id names
// the book in diagnostics and must be unique.
func RegisterBook(id string, fsys fs.FS) (err error) {
	if id == "" || fsys == nil {
		err = eh.Errorf("sqlapplet: RegisterBook: empty id or nil fs")
		return
	}
	booksMu.Lock()
	defer booksMu.Unlock()
	for _, b := range books {
		if b.id == id {
			err = eh.Errorf("sqlapplet: RegisterBook: duplicate book id %q", id)
			return
		}
	}
	books = append(books, registeredBook{id: id, fsys: fsys})
	return
}

// MintManifests parses every registered applet book and registers one
// factory-backed Manifest per applet with the default app registry
// (ADR-0132 §SD2). The shell calls it exactly once at startup, after
// init-time book registrations and before launch resolution, so
// `--launch <appletId>` and the Apps menu see the minted set.
//
// Minting is best-effort per document — an invalid doc yields an error and
// mints nothing, valid siblings still mint — because the corpus test (§SD6)
// is the hard gate; at boot, a partially minted set beats no shell.
func MintManifests(logger zerolog.Logger) (minted int, errs []error) {
	booksMu.Lock()
	snapshot := make([]registeredBook, len(books))
	copy(snapshot, books)
	booksMu.Unlock()
	return mintBooks(app.DefaultRegistry, logger, snapshot)
}

// mintBooks is MintManifests against an explicit registry and book list
// (tests exercise it directly).
func mintBooks(reg *app.Registry, logger zerolog.Logger, snapshot []registeredBook) (minted int, errs []error) {
	sort.Slice(snapshot, func(i, j int) bool { return snapshot[i].id < snapshot[j].id })
	seen := make(map[string]string, 8) // slug → book id
	for _, b := range snapshot {
		defs, perrs := ParseBook(b.id, b.fsys)
		errs = append(errs, perrs...)
		for _, def := range defs {
			if prior, dup := seen[def.Slug]; dup {
				errs = append(errs, eh.Errorf("sqlapplet: slug %q in book %q already minted from book %q", def.Slug, def.BookID, prior))
				continue
			}
			seen[def.Slug] = def.BookID
			m := manifestFor(def, b.fsys)
			if verr := m.Validate(); verr != nil {
				errs = append(errs, eh.Errorf("sqlapplet: %s/%s: manifest: %w", def.BookID, def.Slug, verr))
				continue
			}
			defCopy := def
			if rerr := reg.RegisterFactory(m, func() (a app.AppI, ctorErr error) {
				a = &appletApp{def: defCopy, m: m}
				return
			}); rerr != nil {
				errs = append(errs, eh.Errorf("sqlapplet: %s/%s: register: %w", def.BookID, def.Slug, rerr))
				continue
			}
			minted++
			logger.Debug().Str("id", string(m.Id)).Str("class", defCopy.Class.String()).Msg("sqlapplet: minted")
		}
	}
	return
}

// manifestFor builds the minted manifest. Help is the whole contributing
// book's FS, so the applet's prose page is reachable through the Help
// center; narrowing Help to the single document is a recorded nicety for
// later. The cap list is the attenuation in manifest form (ADR-0132 §SD8):
// exactly one subject — clipboard.write for the Copy SQL escape hatch — and
// no persisted keys, because the buffer is committed definition.
func manifestFor(def *AppletDef, bookFsys fs.FS) (m app.Manifest) {
	m = app.Manifest{
		Id:       app.AppIdT(appletIdPrefix + def.Slug),
		Version:  "0.1.0",
		Display:  def.Title,
		Title:    def.Title,
		Icon:     def.Icon,
		Category: "Applets",
		Surface:  app.SurfaceWindowed,
		Help:     bookFsys,
		Caps: []app.SubjectFilter{
			{
				Pattern:   clipboardbroker.SubjectWrite,
				Direction: app.CapDirectionPub,
				Reason:    "Copy SQL escape hatch (ADR-0132 §SD3): the buffer is the artifact",
			},
		},
	}
	return
}
