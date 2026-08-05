package sqlapplet

import (
	"embed"
	"io/fs"
	"sort"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// bookFS embeds the host's own starter book (apps/sqlapplet/book/*.md) —
// the dogfood corpus over the keelson introspection tables.
//
//go:embed book
var bookFS embed.FS

// booktopoFS embeds the topology suite (apps/sqlapplet/booktopo/*.md) —
// the ADR-0126 appliance-topology tables as a curated applet corpus, one
// applet per canonical query shape from doc/howto/topology-queries.md.
//
//go:embed booktopo
var booktopoFS embed.FS

// bookgodepFS embeds the Go dependency suite (apps/sqlapplet/bookgodep/*.md)
// — the keelson('go_*') tables as the four lenses ADR-0064's godepview app
// draws: the closure, the group quotient, and the third-party surface.
//
//go:embed bookgodep
var bookgodepFS embed.FS

// bookpprofFS embeds the profile suite (apps/sqlapplet/bookpprof/*.md) —
// exploration lenses over the pprof_* ad-hoc datasets the imzrt Profiles
// tab publishes (doc/adr-background-work/pprof-profiles-as-data.md M3).
//
//go:embed bookpprof
var bookpprofFS embed.FS

// bookcapmapFS embeds the competence suite (apps/sqlapplet/bookcapmap/*.md) —
// the ADR-0168 business-capability corpus as the four lenses its §SD9 replaces
// the prototype's webapps with: the corpus at a glance, one competence at a
// time, the hierarchy as a treemap, and the link lint.
//
//go:embed bookcapmap
var bookcapmapFS embed.FS

// bookcoverageFS embeds the continuous-coverage suite
// (apps/sqlapplet/bookcoverage/*.md) — canned lenses over the live
// keelson.coverage_* tables of the ADR-0169 sampler: the totals, the
// package treemap, and the uncovered-functions work list.
//
//go:embed bookcoverage
var bookcoverageFS embed.FS

// bookjsonbenchFS embeds the jsonbench-on-facts trial's result lenses
// (apps/sqlapplet/bookjsonbench/*.md) — canned reads over the facts table
// that `jsonbench results` writes, so the trial reports through the layer it
// measures (doc/trials/jsonbench-on-facts/, protocol §6 Reporting).
//
//go:embed bookjsonbench
var bookjsonbenchFS embed.FS

func init() {
	if err := RegisterBook("sqlapplet", help.MustSub(bookFS, "book"), []app.TopicT{app.TopicRuntime}); err != nil {
		log.Warn().Err(err).Msg("sqlapplet: failed to register starter book")
	}
	if err := RegisterBook("topology", help.MustSub(booktopoFS, "booktopo"), []app.TopicT{app.TopicTopology}); err != nil {
		log.Warn().Err(err).Msg("sqlapplet: failed to register topology book")
	}
	if err := RegisterBook("godep", help.MustSub(bookgodepFS, "bookgodep"), []app.TopicT{app.TopicCode}); err != nil {
		log.Warn().Err(err).Msg("sqlapplet: failed to register godep book")
	}
	if err := RegisterBook("pprof", help.MustSub(bookpprofFS, "bookpprof"), []app.TopicT{app.TopicObservability}); err != nil {
		log.Warn().Err(err).Msg("sqlapplet: failed to register pprof book")
	}
	// TopicCode: the corpus describes what the toolbelt can do, which is the
	// shape of the repository at a coarser grain than packages. It is not
	// TopicAbout — that topic is provenance and licence, not a queryable body
	// of documents someone reads to find something out.
	if err := RegisterBook("capmap", help.MustSub(bookcapmapFS, "bookcapmap"), []app.TopicT{app.TopicCode}); err != nil {
		log.Warn().Err(err).Msg("sqlapplet: failed to register capmap book")
	}
	// TopicObservability, like pprof: what a running process did, measured
	// from inside it — not TopicCode, which is what the repository is.
	if err := RegisterBook("coverage", help.MustSub(bookcoverageFS, "bookcoverage"), []app.TopicT{app.TopicObservability}); err != nil {
		log.Warn().Err(err).Msg("sqlapplet: failed to register coverage book")
	}
	// TopicObservability, like pprof and coverage: measurements of what ran,
	// not a description of what the repository is.
	if err := RegisterBook("jsonbench", help.MustSub(bookjsonbenchFS, "bookjsonbench"), []app.TopicT{app.TopicObservability}); err != nil {
		log.Warn().Err(err).Msg("sqlapplet: failed to register jsonbench book")
	}
}

// registeredBook is one contributed applet corpus.
type registeredBook struct {
	id   string
	fsys fs.FS
	// topics is the book's default classification (ADR-0158 §SD7), applied
	// to every document that does not carry its own `topics:` frontmatter.
	// A book is a curated set on one subject, so the book id is exactly the
	// grouping signal the pre-0158 minter discarded.
	topics []app.TopicT
}

// storeDefaultTopics classifies runtime-authored applets (ADR-0132 "O4"),
// which arrive through the store rather than a book and so have no default
// to inherit. They are SQL someone wrote in the playground; nothing more
// specific is knowable without asking the author, and §SD1's vocabulary has
// no "misc" member by design.
var storeDefaultTopics = []app.TopicT{app.TopicSql}

var (
	booksMu sync.Mutex
	books   []registeredBook
)

// RegisterBook contributes an applet book: an fs.FS of markdown documents in
// the ADR-0132 §SD1 shape. Packages call it from init (the help-facility
// pattern); [MintManifests] later parses every registered book. The id names
// the book in diagnostics and must be unique.
//
// topics is the book's default ADR-0158 §SD1 classification, applied to
// every document that does not override it with its own `topics:`
// frontmatter. It is required and must be registered: a book whose applets
// cannot be sectioned is refused here rather than producing manifests the
// launcher silently drops at registration (§SD9).
func RegisterBook(id string, fsys fs.FS, topics []app.TopicT) (err error) {
	if id == "" || fsys == nil {
		err = eh.Errorf("sqlapplet: RegisterBook: empty id or nil fs")
		return
	}
	if len(topics) == 0 {
		err = eh.Errorf("sqlapplet: RegisterBook: book %q declares no default topics (ADR-0158 §SD7)", id)
		return
	}
	for _, t := range topics {
		if !t.IsRegistered() {
			err = eh.Errorf("sqlapplet: RegisterBook: book %q declares unregistered topic %q", id, t)
			return
		}
	}
	booksMu.Lock()
	defer booksMu.Unlock()
	for _, b := range books {
		if b.id == id {
			err = eh.Errorf("sqlapplet: RegisterBook: duplicate book id %q", id)
			return
		}
	}
	books = append(books, registeredBook{id: id, fsys: fsys, topics: topics})
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
		// RegisterBook enforces this, but mintBooks also takes hand-built
		// snapshots. Reporting it once per book beats letting every document
		// in it fail Validate with "declares no Topics", which names the
		// symptom and not the cause.
		if len(b.topics) == 0 {
			errs = append(errs, eh.Errorf("sqlapplet: book %q has no default topics (ADR-0158 §SD7)", b.id))
			continue
		}
		defs, perrs := ParseBook(b.id, b.fsys)
		errs = append(errs, perrs...)
		for _, def := range defs {
			if prior, dup := seen[def.Slug]; dup {
				errs = append(errs, eh.Errorf("sqlapplet: slug %q in book %q already minted from book %q", def.Slug, def.BookID, prior))
				continue
			}
			seen[def.Slug] = def.BookID
			// ADR-0158 §SD7: the book's grouping is the default, applied
			// here rather than at parse because ParseDocSource is shared
			// with callers that have no book. A document's own `topics:`
			// wins outright — it is the more specific statement.
			if len(def.Topics) == 0 {
				def.Topics = b.topics
			}
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
// the two escape hatches only — clipboard.write for the per-fence Copy in
// the Definition drawer and windowhost.open for Open in Playground
// (ADR-0135 §SD7) — and no persisted keys, because the buffer is committed
// definition.
func manifestFor(def *AppletDef, bookFsys fs.FS) (m app.Manifest) {
	m = app.Manifest{
		Id:       app.AppIdT(appletIdPrefix + def.Slug),
		Version:  "0.1.0",
		Display:  def.Title,
		Title:    def.Title,
		Icon:     def.Icon,
		Topics:   def.Topics,
		Keywords: def.Keywords,
		Kind:     app.KindApplet,
		Surface:  app.SurfaceWindowed,
		Help:     bookFsys,
		Caps: []app.SubjectFilter{
			{
				Pattern:   clipboardbroker.SubjectWrite,
				Direction: app.CapDirectionPub,
				Reason:    "Copy a fenced block out of the Definition drawer (ADR-0132 §SD3): the document is the artifact",
			},
			{
				Pattern:   windowhost.OpenSubject,
				Direction: app.CapDirectionPub,
				Reason:    "Open in Playground (ADR-0135 §SD7): the §SD3 escape-hatch upgrade",
			},
		},
	}
	// Declared datasets add the one capability their open-time binding
	// needs (ADR-0134 §SD4, update 2026-08-01); a dataset-less applet
	// keeps the two-cap surface.
	if len(def.Datasets) > 0 {
		m.Caps = append(m.Caps, app.SubjectFilter{
			Pattern:   adhocdata.SubjectResolve,
			Direction: app.CapDirectionPub,
			Reason:    "resolve declared dataset aliases to their newest live handles at open (ADR-0134 §SD4)",
		})
	}
	return
}
