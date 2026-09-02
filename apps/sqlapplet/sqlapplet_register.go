package sqlapplet

import (
	"embed"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
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

// bookcodevolFS embeds the code-volume suite
// (apps/sqlapplet/bookcodevol/*.md) — canned lenses over the ADR-0173
// self-inspection tables keelson.go_modules and keelson.go_symbols: how much
// of this binary is its own code and how much is somebody else's, the module
// inventory ranked by contributed machine code, the same split as a treemap,
// and the shipped-vs-executed contrast against the coverage tables.
//
//go:embed bookcodevol
var bookcodevolFS embed.FS

// bookcatalogFS embeds the data-catalog suite
// (apps/sqlapplet/bookcatalog/*.md) — canned lenses over the four ADR-0170
// `boxer.tables_*` tables a `boxer datacatalog refresh` writes: what this
// ClickHouse instance holds, which leeway tables share a schema, and which
// opaque ones no panel knows how to draw yet.
//
//go:embed bookcatalog
var bookcatalogFS embed.FS

// bookadrFS embeds the decision-corpus suite (apps/sqlapplet/bookadr/*.md) —
// canned lenses over the ADR-0122 §SD4 `keelson('adr')` family, which reads
// this repository's decision records rather than the running process.
//
//go:embed bookadr
var bookadrFS embed.FS

// booklading is the lading store's book (ADR-0200 §SD8, M0): the snapshot
// ledger, a directory, find, content search, history, diff, du, problems and
// the block audit — the ADR-0198 §7 operations as pasteable chapters, every
// knob prelude-bound and `'*'` (every visible mount) the default mount.
//
//go:embed booklading
var bookladingFS embed.FS

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
	// TopicCode rather than TopicObservability: these tables describe what
	// the binary is made of — a property of the code that shipped — even
	// though they are read from the running process. The one applet that
	// crosses into "what ran" reads the coverage tables to say so explicitly.
	if err := RegisterBook("codevol", help.MustSub(bookcodevolFS, "bookcodevol"), []app.TopicT{app.TopicCode}); err != nil {
		log.Warn().Err(err).Msg("sqlapplet: failed to register code-volume book")
	}
	// TopicData: the subject is the ClickHouse instance's own contents — what
	// tables exist and what shape they are — not the repository (TopicCode) and
	// not what a process did (TopicObservability).
	if err := RegisterBook("catalog", help.MustSub(bookcatalogFS, "bookcatalog"), []app.TopicT{app.TopicData}); err != nil {
		log.Warn().Err(err).Msg("sqlapplet: failed to register data-catalog book")
	}
	// TopicAbout, the topic docs-search already claims for the same corpus:
	// the subject is the project's own record of its decisions, not the code
	// (TopicCode) and not the process (TopicRuntime).
	if err := RegisterBook("adr", help.MustSub(bookadrFS, "bookadr"), []app.TopicT{app.TopicAbout}); err != nil {
		log.Warn().Err(err).Msg("sqlapplet: failed to register adr book")
	}
	if err := RegisterBook("lading", help.MustSub(bookladingFS, "booklading"), []app.TopicT{app.TopicData}); err != nil {
		log.Warn().Err(err).Msg("sqlapplet: failed to register lading book")
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
		err = eb.Build().Str("id", id).Errorf("sqlapplet: RegisterBook: book declares no default topics (ADR-0158 §SD7)")
		return
	}
	for _, t := range topics {
		if !t.IsRegistered() {
			err = eb.Build().Str("id", id).Stringer("topic", t).Errorf("sqlapplet: RegisterBook: book declares unregistered topic")
			return
		}
	}
	booksMu.Lock()
	defer booksMu.Unlock()
	for _, b := range books {
		if b.id == id {
			err = eb.Build().Str("id", id).Errorf("sqlapplet: RegisterBook: duplicate book id")
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
			errs = append(errs, eb.Build().Str("id", b.id).Errorf("sqlapplet: book has no default topics (ADR-0158 §SD7)"))
			continue
		}
		defs, perrs := ParseBook(b.id, b.fsys)
		errs = append(errs, perrs...)
		for _, def := range defs {
			if prior, dup := seen[def.Slug]; dup {
				errs = append(errs, eb.Build().Str("slug", def.Slug).Str("bookID", def.BookID).Str("prior", prior).Errorf("sqlapplet: the slug in this book was already minted from another book"))
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
				errs = append(errs, eb.Build().Str("bookID", def.BookID).Str("slug", def.Slug).Errorf("sqlapplet: manifest: %w", verr))
				continue
			}
			defCopy := def
			if rerr := reg.RegisterFactory(m, func() (a app.AppI, ctorErr error) {
				a = &appletApp{def: defCopy, m: m}
				return
			}); rerr != nil {
				errs = append(errs, eb.Build().Str("bookID", def.BookID).Str("slug", def.Slug).Errorf("sqlapplet: register: %w", rerr))
				continue
			}
			minted++
			logger.Debug().Str("id", string(m.Id)).Str("class", defCopy.Class.String()).Msg("sqlapplet: minted")
		}
	}
	return
}

// WindowSize opens an applet at a chosen size, for scripted screenshots.
//
// It mirrors play's `BOXER_PLAY_WINDOW_SIZE` and exists for the same reason:
// the host's archetype fallback is a 900×640 application window, and an applet
// whose document places panes in three zones (ADR-0132 Update 2026-08-14)
// opens with all three of them cramped — which a capture would then record as
// the shape of the applet rather than the shape of the default window. Inert
// when unset, so an ordinary launch is unaffected.
var WindowSize = env.NewString(env.Spec{
	Name:        "BOXER_SQLAPPLET_WINDOW_SIZE",
	Description: "open an applet window at \"WxH\" logical points (scripted screenshots); empty or unparseable keeps the host's archetype default",
	Category:    env.CategoryE("boxer-sqlapplet"),
})

// DatasetEvents is a fault-injection knob for the dataset binder's event path
// (ADR-0188 §SD3). "on" subscribes to adhoc.event.> and acts on the events
// (the default); "drop" subscribes but discards every event, which is what a
// slow consumer on NATS core experiences and leaves the reconcile tick alone
// to keep the binding in step; "off" does not subscribe at all, the
// pre-events poll. It exists so the headless lane can show the reconcile
// working against a running host; it is not an operating knob.
var DatasetEvents = env.NewCategorialString(env.Spec{
	Name:        "BOXER_SQLAPPLET_DATASET_EVENTS",
	Default:     "on",
	Description: "dataset binder event path (ADR-0188 §SD3): on = subscribe and act; drop = subscribe but discard, reconcile alone binds (fault injection); off = do not subscribe, poll",
	Category:    env.CategoryE("boxer-sqlapplet"),
}, []string{"on", "drop", "off"})

// DatasetReconcile overrides the binder's reconcile interval — how often a
// window with events subscribed re-asks for pending aliases and verifies its
// bound handles (ADR-0188 §SD3). The default is the value the binder would
// use anyway; shorten it for the headless lane or under a lossy transport.
var DatasetReconcile = env.NewDuration(env.Spec{
	Name:        "BOXER_SQLAPPLET_DATASET_RECONCILE",
	Default:     "30s",
	Description: "dataset binder reconcile interval with events subscribed (ADR-0188 §SD3); a Go duration such as 30s or 5s",
	Category:    env.CategoryE("boxer-sqlapplet"),
})

// appletSurfaceHints reads [WindowSize] into a manifest's window hints.
//
// The registry caches an environment value on first read, which is right for a
// manifest built once at registration and is why the parsing lives in
// [parseSurfaceHints] — a pure function is the half worth testing.
func appletSurfaceHints() (h app.SurfaceHints) {
	return parseSurfaceHints(WindowSize.Get())
}

// parseSurfaceHints reads a "WxH" spelling. Unset, unparseable or out of range
// returns the zero value, which the host reads as "pick the archetype default"
// — a window of zero size is never what an unreadable knob should produce.
func parseSurfaceHints(raw string) (h app.SurfaceHints) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(raw)), "x")
	if len(parts) != 2 {
		return
	}
	w, wErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	ht, hErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if wErr != nil || hErr != nil || w <= 0 || ht <= 0 || w > 65535 || ht > 65535 {
		return
	}
	h.PreferredWidth = uint16(w)
	h.PreferredHeight = uint16(ht)
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
		Summary:  def.Summary,
		Icon:     def.Icon,
		Topics:   def.Topics,
		Keywords: def.Keywords,
		Kind:     app.KindApplet,
		Surface:  app.SurfaceWindowed,
		// No hints unless the screenshot knob is set: the host's archetype
		// default is the right window for an applet opened by a person.
		SurfaceHints: appletSurfaceHints(),
		Help:         bookFsys,
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
	// Declared datasets add the two capabilities their binding needs
	// (ADR-0134 §SD4, ADR-0188 §SD3): resolving an alias at open, and
	// hearing the service's publish/retract events so the binding follows
	// the dataset for the life of the window. A dataset-less applet keeps
	// the two-cap surface.
	if len(def.Datasets) > 0 {
		m.Caps = append(m.Caps,
			app.SubjectFilter{
				Pattern:   adhocdata.SubjectResolve,
				Direction: app.CapDirectionPub,
				Reason:    "resolve declared dataset aliases to their newest live handles at open (ADR-0134 §SD4)",
			},
			app.SubjectFilter{
				Pattern:   adhocdata.SubjectEventAll,
				Direction: app.CapDirectionSub,
				Reason:    "follow declared datasets as they are published and withdrawn (ADR-0188 §SD3)",
			},
		)
	}
	return
}
