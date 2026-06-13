package help

import (
	"slices"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// LibraryI is the registry of [BookI] values keyed by [app.AppIdT].
// Implementations auto-sync from [app.DefaultRegistry] on first
// read — every registered [app.Manifest] with a non-nil Help fs.FS
// becomes a book on first access — so the typical app does not need
// to call [LibraryI.Register] explicitly.
//
// Manual registration is supported for tests, special-purpose
// libraries built around a [Registry] other than DefaultRegistry, and
// runtime-injected docs (e.g. bundled "About Keelson" content that
// isn't owned by any single app).
type LibraryI interface {
	// Book returns the book registered for id, or ok=false when absent.
	// Triggers a one-shot SyncFromRegistry on first call.
	Book(id app.AppIdT) (b BookI, ok bool)
	// Books returns every registered book in AppId-sorted order.
	// Triggers a one-shot SyncFromRegistry on first call.
	Books() (books []BookI)
	// Register inserts an explicitly-built book. Returns an error on
	// nil book or on duplicate AppId (first registration wins).
	Register(b BookI) (err error)
	// SyncFromRegistry walks app.DefaultRegistry and registers a fresh
	// Book for every Manifest with a non-nil Help fs.FS that isn't
	// already registered. Returns the number of books added. Safe to
	// call repeatedly; idempotent past the first call.
	SyncFromRegistry() (added int)
}

// DefaultLibrary is the process-wide library populated by the auto-sync
// path. Mirrors [app.DefaultRegistry]'s role.
var DefaultLibrary LibraryI = NewLibrary()

// NewLibrary returns an empty library suitable for tests or special
// wiring. Production code uses [DefaultLibrary].
func NewLibrary() (l LibraryI) {
	l = &library{
		books: make(map[app.AppIdT]BookI, 8),
	}
	return
}

type library struct {
	mu     sync.RWMutex
	books  map[app.AppIdT]BookI

	syncMu sync.Mutex
	synced bool
}

var _ LibraryI = (*library)(nil)

func (inst *library) Book(id app.AppIdT) (b BookI, ok bool) {
	inst.maybeSync()
	inst.mu.RLock()
	b, ok = inst.books[id]
	inst.mu.RUnlock()
	return
}

func (inst *library) Books() (books []BookI) {
	inst.maybeSync()
	inst.mu.RLock()
	ids := make([]app.AppIdT, 0, len(inst.books))
	for id := range inst.books {
		ids = append(ids, id)
	}
	inst.mu.RUnlock()
	slices.Sort(ids)
	books = make([]BookI, 0, len(ids))
	inst.mu.RLock()
	for _, id := range ids {
		books = append(books, inst.books[id])
	}
	inst.mu.RUnlock()
	return
}

func (inst *library) Register(b BookI) (err error) {
	if b == nil {
		err = eh.Errorf("help.library: nil book")
		return
	}
	id := b.AppId()
	if id == "" {
		err = eh.Errorf("help.library: book has empty AppId")
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if _, exists := inst.books[id]; exists {
		err = eb.Build().Str("appid", string(id)).Errorf("help.library: duplicate AppId")
		return
	}
	inst.books[id] = b
	return
}

func (inst *library) SyncFromRegistry() (added int) {
	for _, m := range app.AllManifests() {
		if m.Help == nil {
			continue
		}
		inst.mu.Lock()
		if _, exists := inst.books[m.Id]; exists {
			inst.mu.Unlock()
			continue
		}
		b, bookErr := NewBook(m.Id, m.Help)
		if bookErr != nil {
			inst.mu.Unlock()
			log.Warn().Err(bookErr).Str("appid", string(m.Id)).
				Msg("help.library: SyncFromRegistry: NewBook failed, skipping")
			continue
		}
		inst.books[m.Id] = b
		inst.mu.Unlock()
		added++
	}
	return
}

// maybeSync runs SyncFromRegistry exactly once across the library's
// lifetime, the first time any reader requests a book. Tests that need
// to re-trigger the sync after registering more apps call
// [LibraryI.SyncFromRegistry] explicitly; that path is idempotent.
func (inst *library) maybeSync() {
	inst.syncMu.Lock()
	already := inst.synced
	inst.synced = true
	inst.syncMu.Unlock()
	if !already {
		inst.SyncFromRegistry()
	}
}
