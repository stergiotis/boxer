package chatlet

import (
	"embed"
	"io/fs"
	"sort"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/apps/chat"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/promptbook"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// bookFS embeds this package's own book.
//
//go:embed book
var bookFS embed.FS

func init() {
	if err := RegisterBook("chatlet", help.MustSub(bookFS, "book"), []app.TopicT{app.TopicObservability}); err != nil {
		log.Warn().Err(err).Msg("chatlet: failed to register the book")
	}
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

// RegisterBook contributes a book of chatlet documents, which also appears
// in keelson('llm_prompts'). Packages call it from init; [MintManifests]
// reads every registered book. topics classifies a document naming none.
func RegisterBook(id string, fsys fs.FS, topics []app.TopicT) (err error) {
	if id == "" || fsys == nil || len(topics) == 0 {
		return eb.Build().Str("id", id).Errorf("chatlet: RegisterBook: a book needs an id, a file system and default topics")
	}
	booksMu.Lock()
	defer booksMu.Unlock()
	for _, b := range books {
		if b.id == id {
			return eb.Build().Str("id", id).Errorf("chatlet: RegisterBook: duplicate book id")
		}
	}
	if err = promptbook.Register("chatlet-"+id, fsys); err != nil {
		return eh.Errorf("chatlet: %w", err)
	}
	books = append(books, registeredBook{id: id, fsys: fsys, topics: topics})
	return
}

// MintManifests registers one app per document of every registered book. The
// shell calls it once, before launch resolution and before the host's boot,
// so the coordinators predicate sees the minted set (ADR-0364, proposed).
// A document that does not parse mints nothing and its siblings still do; the
// corpus test is the gate that keeps one off main.
func MintManifests(logger zerolog.Logger) (minted int, errs []error) {
	return mintBooks(app.DefaultRegistry, logger, snapshotBooks())
}

func snapshotBooks() (out []registeredBook) {
	booksMu.Lock()
	defer booksMu.Unlock()
	out = append(out, books...)
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return
}

func mintBooks(reg *app.Registry, logger zerolog.Logger, snapshot []registeredBook) (minted int, errs []error) {
	for _, b := range snapshot {
		presets, perrs := parseBook(b)
		errs = append(errs, perrs...)
		for _, p := range presets {
			preset := p
			if rerr := reg.RegisterFactory(preset.Manifest, func() (a app.AppI, ctorErr error) {
				return chat.NewPreset(preset)
			}); rerr != nil {
				errs = append(errs, eb.Build().Str("id", string(preset.Manifest.Id)).Errorf("chatlet: register: %w", rerr))
				continue
			}
			minted++
			logger.Debug().Str("id", string(preset.Manifest.Id)).Msg("chatlet: minted")
		}
	}
	return
}

// parseBook reads every markdown document of a book, sorted by path.
func parseBook(b registeredBook) (presets []chat.Preset, errs []error) {
	err := fs.WalkDir(b.fsys, ".", func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			errs = append(errs, werr)
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		src, rerr := fs.ReadFile(b.fsys, path)
		if rerr != nil {
			errs = append(errs, rerr)
			return nil
		}
		p, perr := ParseDocSource(b.id, path, src, b.topics)
		if perr != nil {
			errs = append(errs, perr)
			return nil
		}
		presets = append(presets, p)
		return nil
	})
	if err != nil {
		errs = append(errs, err)
	}
	return
}
