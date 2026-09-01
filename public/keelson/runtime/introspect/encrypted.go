package introspect

import (
	"sync"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// EncryptedEntry is the registry provider for one ad-hoc encrypted
// dataset (ADR-0134). It satisfies Provider so registration, catalog
// enumeration, and the keelson('…') macro keep working, and
// EncryptedDatasetI so the in-process engine streams it through the
// broker rather than snapshotting it. A republish swaps the backing
// file, structure, schema, and revision in place; reads and Update are
// mutex-guarded so a query racing a republish sees a consistent tuple.
type EncryptedEntry struct {
	name string

	mu        sync.RWMutex
	schema    *arrow.Schema
	structure string
	path      string
	revision  uint64
}

// NewEncryptedEntry builds an entry for a freshly published dataset.
func NewEncryptedEntry(name string, schema *arrow.Schema, structure, path string, revision uint64) *EncryptedEntry {
	return &EncryptedEntry{name: name, schema: schema, structure: structure, path: path, revision: revision}
}

// Name is the dataset's handle — a valid keelson table name.
func (e *EncryptedEntry) Name() string { return e.name }

// Freshness is always Live: an ad-hoc dataset reflects mutable runtime
// state and a republish must not serve a cached snapshot.
func (e *EncryptedEntry) Freshness() FreshnessClass { return FreshnessLive }

// Schema returns the dataset's Arrow schema.
func (e *EncryptedEntry) Schema() *arrow.Schema {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.schema
}

// Structure returns the explicit ClickHouse structure string.
func (e *EncryptedEntry) Structure() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.structure
}

// Path returns the absolute path to the chunk-encrypted Arrow file.
func (e *EncryptedEntry) Path() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.path
}

// Revision returns the current dataset revision.
func (e *EncryptedEntry) Revision() uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.revision
}

// Snapshot is never valid for an ad-hoc dataset: it streams through the
// broker's decrypt path, not the in-process snapshot path. Returning an
// error keeps the HTTP source and any accidental snapshot honest.
func (e *EncryptedEntry) Snapshot(Projection) (arrow.RecordBatch, error) {
	return nil, eb.Build().Str("name", e.name).Errorf("introspect: the table is an ad-hoc dataset; it streams through the broker, not Snapshot")
}

// Update swaps the dataset's schema, structure, file, and revision in
// place on republish.
func (e *EncryptedEntry) Update(schema *arrow.Schema, structure, path string, revision uint64) {
	e.mu.Lock()
	e.schema = schema
	e.structure = structure
	e.path = path
	e.revision = revision
	e.mu.Unlock()
}

// IsSealed reports whether the registered provider under name is a sealed
// dataset — one whose bytes are encrypted at rest and readable only by
// decrypting in this process (ADR-0134, ADR-0145).
//
// It is what a dispatcher consults to decide whether a statement naming
// this table is confined, so it answers about the registry rather than
// about SQL text: a name either resolves to a sealed provider here or it
// does not.
func (inst *Registry) IsSealed(name string) (yes bool) {
	p, ok := inst.Lookup(name)
	if !ok {
		return
	}
	_, yes = p.(EncryptedDatasetI)
	return
}
