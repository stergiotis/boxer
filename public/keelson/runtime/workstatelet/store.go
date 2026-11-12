package workstatelet

import (
	"slices"
	"strings"
	"sync"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// StoreAppId is the synthetic identity the store persists under. It is never
// a registered app; its SubjectAlias namespaces the stored documents, so
// changing it would orphan every one of them.
const StoreAppId app.AppIdT = "runtime.workstatelet"

const (
	// storeKeyPrefix + slug is one stored document. A persist key is one
	// NATS subject token, so the separator is an underscore and the slug
	// keeps its dash charset.
	storeKeyPrefix = "doc_"
	// storeIndexKey lists the stored slugs, newline-joined: StorageI has no
	// listing (the applet store's index, ADR-0132 Update "O4").
	storeIndexKey = "index"
)

// Store keeps the documents a person captured from the desktop and mints
// them (ADR-0369 §SD8), under the applet store's rules: a committed document
// of the same slug wins, and saving a stored slug again replaces its
// document while the manifest stays as registered until the next boot.
type Store struct {
	log     zerolog.Logger
	reg     *app.Registry
	cat     *Catalog
	storage app.StorageI

	mu     sync.Mutex
	stored map[string]struct{} // slugs this store minted this boot
}

// StartStore builds the store over the runtime persist facility on bus and
// mints what is stored. Call it after the books are minted into reg: the
// collision rule reads the registry.
func StartStore(reg *app.Registry, cat *Catalog, bus *inprocbus.Inst, logger zerolog.Logger) (inst *Store, err error) {
	if reg == nil || cat == nil || bus == nil {
		err = eb.Build().Bool("reg", reg != nil).Bool("catalog", cat != nil).Bool("bus", bus != nil).
			Errorf("the store needs a registry, a catalog and a bus")
		return
	}
	busClient := bus.NewClient(StoreAppId, []app.SubjectFilter{
		{Pattern: persist.SubjectPrefix + StoreAppId.SubjectAlias() + ".>", Direction: app.CapDirectionPub,
			Reason: "persist captured workstatelet documents"},
	})
	storage, err := persist.NewClient(busClient, StoreAppId)
	if err != nil {
		err = eh.Errorf("persist client: %w", err)
		return
	}
	inst = NewStore(reg, cat, storage, logger)
	inst.Load()
	return
}

// NewStore builds a store over storage without loading it; StartStore is the
// production entry point.
func NewStore(reg *app.Registry, cat *Catalog, storage app.StorageI, logger zerolog.Logger) (inst *Store) {
	return &Store{log: logger, reg: reg, cat: cat, storage: storage, stored: make(map[string]struct{}, 4)}
}

// Load parses and mints every stored document, best-effort per document: one
// that no longer parses, or whose slug a committed document took meanwhile,
// is skipped with a warning.
func (inst *Store) Load() {
	raw, found, err := inst.storage.Get(storeIndexKey)
	if err != nil {
		inst.log.Warn().Err(err).Msg("workstatelet: store index unreadable; stored documents skipped")
		return
	}
	if !found {
		return
	}
	for slug := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		if slug == "" {
			continue
		}
		src, docFound, gerr := inst.storage.Get(storeKeyPrefix + slug)
		if gerr != nil || !docFound {
			inst.log.Warn().Err(gerr).Str("slug", slug).Msg("workstatelet: indexed document is not stored; skipped")
			continue
		}
		doc, perr := Parse(slug, src, nil)
		if perr != nil {
			inst.log.Warn().Err(perr).Str("slug", slug).Msg("workstatelet: stored document no longer parses; skipped")
			continue
		}
		if _, taken := inst.reg.LookupManifest(AppIdOf(slug)); taken {
			inst.log.Warn().Str("slug", slug).Msg("workstatelet: stored document collides with a committed one; committed wins")
			continue
		}
		if _, merr := inst.cat.Mint(inst.reg, doc, OriginStored, ""); merr != nil {
			inst.log.Warn().Err(merr).Str("slug", slug).Msg("workstatelet: stored document failed to mint; skipped")
			continue
		}
		inst.mu.Lock()
		inst.stored[slug] = struct{}{}
		inst.mu.Unlock()
	}
}

// Save composes doc, persists it under its slug and mints it, or replaces
// the document a stored slug opens. A slug a committed document holds is
// refused. replaced reports which of the two happened.
func (inst *Store) Save(doc Doc) (id app.AppIdT, replaced bool, err error) {
	composed, err := Compose(doc)
	if err != nil {
		return
	}
	// The stored bytes are what the next boot reads; refuse now what it
	// would refuse then.
	parsed, err := Parse(composed.Slug, composed.Source, nil)
	if err != nil {
		err = eh.Errorf("composed document does not read back: %w", err)
		return
	}
	id = AppIdOf(parsed.Slug)
	inst.mu.Lock()
	defer inst.mu.Unlock()
	_, replaced = inst.stored[parsed.Slug]
	if _, registered := inst.reg.LookupManifest(id); registered && !replaced {
		err = eb.Build().Str("slug", parsed.Slug).Errorf("slug is taken by a committed workstatelet")
		return
	}
	if err = inst.storage.Set(storeKeyPrefix+parsed.Slug, parsed.Source); err != nil {
		err = eb.Build().Str("slug", parsed.Slug).Errorf("persist document: %w", err)
		return
	}
	if err = inst.updateIndexLocked(parsed.Slug); err != nil {
		err = eb.Build().Str("slug", parsed.Slug).Errorf("persist index: %w", err)
		return
	}
	if replaced {
		err = inst.cat.Replace(parsed)
		return
	}
	if _, err = inst.cat.Mint(inst.reg, parsed, OriginStored, ""); err != nil {
		return
	}
	inst.stored[parsed.Slug] = struct{}{}
	return
}

// updateIndexLocked adds slug to the persisted index. The index is rebuilt
// from what is stored rather than from this boot's minted set, so a document
// skipped at Load stays listed and is not orphaned by the next save.
func (inst *Store) updateIndexLocked(slug string) (err error) {
	raw, _, err := inst.storage.Get(storeIndexKey)
	if err != nil {
		return
	}
	var slugs []string
	for s := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		if s != "" {
			slugs = append(slugs, s)
		}
	}
	if slices.Contains(slugs, slug) {
		return
	}
	slugs = append(slugs, slug)
	slices.Sort(slugs)
	err = inst.storage.Set(storeIndexKey, []byte(strings.Join(slugs, "\n")))
	return
}

// MemoryStorage is an app.StorageI held in memory: what a store is built
// over where captures must not outlive the process or reach the machine's
// persist store. Safe for concurrent use.
type MemoryStorage struct {
	mu sync.Mutex
	m  map[string][]byte
}

var _ app.StorageI = (*MemoryStorage)(nil)

// NewMemoryStorage returns an empty MemoryStorage.
func NewMemoryStorage() (inst *MemoryStorage) {
	return &MemoryStorage{m: make(map[string][]byte, 4)}
}

func (inst *MemoryStorage) Get(key string) (value []byte, found bool, err error) {
	inst.mu.Lock()
	v, found := inst.m[key]
	inst.mu.Unlock()
	value = slices.Clone(v)
	return
}

func (inst *MemoryStorage) Set(key string, value []byte) (err error) {
	inst.mu.Lock()
	inst.m[key] = slices.Clone(value)
	inst.mu.Unlock()
	return
}

func (inst *MemoryStorage) Delete(key string) (err error) {
	inst.mu.Lock()
	delete(inst.m, key)
	inst.mu.Unlock()
	return
}
