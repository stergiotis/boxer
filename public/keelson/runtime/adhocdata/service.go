package adhocdata

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Quotas bound the store (ADR-0134 SD1). A publish that would breach one
// is refused with a named error, never discovered at query time.
const (
	// PerDatasetMaxBytes caps one dataset's ciphertext.
	PerDatasetMaxBytes = 256 << 20 // 256 MiB
	// StoreMaxBytes caps the whole store.
	StoreMaxBytes = 1 << 30 // 1 GiB
	// MaxDatasets caps how many datasets may coexist.
	MaxDatasets = 64
)

// ServiceAppId is the synthetic identity the capability service speaks
// under on the bus; audit rows attribute publishes/grants/retracts to it.
const ServiceAppId app.AppIdT = "runtime.adhoc"

// StoreDir names the directory holding the encrypted dataset files. Empty
// resolves to <user cache dir>/boxer/adhoc; on the appliance the host sets
// it beneath /perm (ADR-0134 SD1/SD8).
var StoreDir = env.NewString(env.Spec{
	Name:        "BOXER_ADHOC_DIR",
	Default:     "",
	Description: "directory for the ad-hoc dataset store (ADR-0134); empty resolves to <user cache dir>/boxer/adhoc",
	Category:    env.CategorySystem,
})

// KeyRegistrar is the broker-side key custody the service drives (ADR-0134
// K2). *chlocalbroker.KeyStore satisfies it; taking an interface keeps
// this package from importing the broker (the broker imports the AEAD
// stream from here).
type KeyRegistrar interface {
	RegisterDatasetKey(name string, key []byte)
	DeregisterDatasetKey(name string)
}

// Config parameterises the capability Service.
type Config struct {
	// Bus, when non-nil, backs the adhoc.publish/grant/retract
	// request/reply subjects. Nil leaves only the in-process Go methods.
	Bus *inprocbus.Inst
	// Registry is where dataset handles register as EncryptedEntry
	// providers; defaults to introspect.Default.
	Registry *introspect.Registry
	// Keys is the broker key store; required.
	Keys KeyRegistrar
	// Dir overrides the store directory; empty resolves from StoreDir.
	Dir string
	// Log is the service logger.
	Log zerolog.Logger
}

// dataset is the service's authoritative record of one live dataset.
type dataset struct {
	handle          string
	alias           string
	schema          *arrow.Schema
	structure       string
	path            string
	revision        uint64
	rows            uint64
	bytes           uint64 // ciphertext file size
	createdAtUnixUs int64
	entry           *introspect.EncryptedEntry
}

// Service owns the encrypted dataset store: it validates and encrypts
// published data, mints ephemeral handles, custodies keys with the
// broker, registers handles as queryable providers, and retracts on
// request (ADR-0134 SD2). It is safe for concurrent use.
type Service struct {
	reg  *introspect.Registry
	keys KeyRegistrar
	dir  string
	log  zerolog.Logger

	busClient *inprocbus.Client
	unsub     func()

	mu         sync.RWMutex
	datasets   map[string]*dataset
	totalBytes uint64
}

// PublishInput is the in-process shape of a publish (the bus wire mirrors
// it). Handle empty mints a new dataset; a known Handle republishes it.
type PublishInput struct {
	Alias          string
	Handle         string
	ArrowIPCStream []byte
}

// PublishResult reports the minted (or reused) handle and dataset stats.
type PublishResult struct {
	Handle   string
	Revision uint64
	Rows     uint64
	Bytes    uint64
}

// GrantResult is the metadata a grant hands back.
type GrantResult struct {
	Structure     string
	SchemaSummary string
	Revision      uint64
	Alias         string
}

// NewService builds the Service, creates and sweeps the store directory
// (ADR-0134 SD1: crash residue is ciphertext without a key, but the sweep
// removes it anyway), and, when a bus is supplied, subscribes to the
// capability subjects.
func NewService(cfg Config) (inst *Service, err error) {
	if cfg.Keys == nil {
		return nil, eh.Errorf("adhocdata: key registrar is required")
	}
	reg := cfg.Registry
	if reg == nil {
		reg = introspect.Default
	}
	dir := cfg.Dir
	if dir == "" {
		dir = resolveStoreDir()
	}
	if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
		return nil, eh.Errorf("adhocdata: mkdir store dir %q: %w", dir, mkErr)
	}
	inst = &Service{
		reg:      reg,
		keys:     cfg.Keys,
		dir:      dir,
		log:      cfg.Log,
		datasets: make(map[string]*dataset),
	}
	if removed := inst.sweep(); removed > 0 {
		inst.log.Info().Int("removed", removed).Str("dir", dir).Msg("adhocdata: swept leftover dataset files on start")
	}
	if cfg.Bus != nil {
		if subErr := inst.subscribe(cfg.Bus); subErr != nil {
			return nil, subErr
		}
	}
	return inst, nil
}

// Close unsubscribes, deregisters every key and provider, and deletes all
// store files (best-effort; the ephemerality guarantee does not rest on
// this — after a crash the files are ciphertext whose key is gone).
func (inst *Service) Close(context.Context) (err error) {
	if inst.unsub != nil {
		inst.unsub()
		inst.unsub = nil
	}
	inst.mu.Lock()
	handles := make([]string, 0, len(inst.datasets))
	for h := range inst.datasets {
		handles = append(handles, h)
	}
	inst.datasets = make(map[string]*dataset)
	inst.totalBytes = 0
	inst.mu.Unlock()

	for _, h := range handles {
		inst.keys.DeregisterDatasetKey(h)
		inst.reg.Unregister(h)
	}
	removed := inst.sweep()
	inst.log.Info().Int("removed", removed).Msg("adhocdata: deleted dataset files on close")
	return nil
}

// Publish validates and encrypts a dataset, mints or reuses a handle,
// registers the key and a queryable provider, and returns the handle and
// stats (ADR-0134 SD1/SD2). A republish (known Handle) bumps the revision
// and swaps the file/key in place under the same handle.
func (inst *Service) Publish(in PublishInput) (res PublishResult, err error) {
	if !validAlias(in.Alias) {
		return res, eh.Errorf("adhocdata: invalid alias %q (want [A-Za-z_][A-Za-z0-9_]*, <=64)", in.Alias)
	}
	schema, structure, plaintext, rows, err := canonicalize(in.ArrowIPCStream)
	if err != nil {
		return res, err
	}
	if uint64(len(plaintext)) > PerDatasetMaxBytes {
		return res, eh.Errorf("adhocdata: dataset exceeds per-dataset quota (%d bytes)", PerDatasetMaxBytes)
	}

	// Resolve the handle and reserve quota under the lock; encrypt outside.
	inst.mu.Lock()
	var existing *dataset
	handle := in.Handle
	if handle != "" {
		existing = inst.datasets[handle]
		if existing == nil {
			inst.mu.Unlock()
			return res, eh.Errorf("adhocdata: unknown handle %q to republish", handle)
		}
	} else {
		handle, err = inst.mintHandleLocked()
		if err != nil {
			inst.mu.Unlock()
			return res, err
		}
	}
	if quErr := inst.checkQuotaLocked(existing, uint64(len(plaintext))); quErr != nil {
		inst.mu.Unlock()
		return res, quErr
	}
	revision := uint64(1)
	if existing != nil {
		revision = existing.revision + 1
	}
	inst.mu.Unlock()

	key := make([]byte, KeySize)
	if _, rErr := rand.Read(key); rErr != nil {
		return res, eh.Errorf("adhocdata: generate key: %w", rErr)
	}
	path := filepath.Join(inst.dir, handle+".bxad")
	nbytesI64, encErr := encryptToFile(path, key, plaintext)
	if encErr != nil {
		return res, eh.Errorf("adhocdata: write dataset: %w", encErr)
	}
	nbytes := uint64(nbytesI64)

	inst.mu.Lock()
	// Re-check against the actual ciphertext size before committing.
	if quErr := inst.checkQuotaLocked(existing, nbytes); quErr != nil {
		inst.mu.Unlock()
		_ = os.Remove(path)
		return res, quErr
	}
	inst.keys.RegisterDatasetKey(handle, key)
	if existing != nil {
		inst.totalBytes -= existing.bytes
		existing.alias = in.Alias
		existing.schema = schema
		existing.structure = structure
		existing.path = path
		existing.revision = revision
		existing.rows = rows
		existing.bytes = nbytes
		inst.totalBytes += nbytes
		existing.entry.Update(schema, structure, path, revision)
	} else {
		entry := introspect.NewEncryptedEntry(handle, schema, structure, path, revision)
		if regErr := inst.reg.Register(entry); regErr != nil {
			inst.mu.Unlock()
			inst.keys.DeregisterDatasetKey(handle)
			_ = os.Remove(path)
			return res, eh.Errorf("adhocdata: register %q: %w", handle, regErr)
		}
		inst.datasets[handle] = &dataset{
			handle: handle, alias: in.Alias, schema: schema, structure: structure,
			path: path, revision: revision, rows: rows, bytes: nbytes,
			createdAtUnixUs: time.Now().UnixMicro(), entry: entry,
		}
		inst.totalBytes += nbytes
	}
	inst.mu.Unlock()

	inst.emitAudit("publish", handle, in.Alias, revision)
	return PublishResult{Handle: handle, Revision: revision, Rows: rows, Bytes: nbytes}, nil
}

// Grant returns a dataset's binding metadata and records the audit event
// that is the grant (ADR-0134 SD2: audited, not enforced).
func (inst *Service) Grant(handle string) (res GrantResult, err error) {
	inst.mu.RLock()
	ds := inst.datasets[handle]
	if ds == nil {
		inst.mu.RUnlock()
		return res, eh.Errorf("adhocdata: unknown handle %q", handle)
	}
	res = GrantResult{
		Structure:     ds.structure,
		SchemaSummary: fmt.Sprintf("%d columns, %d rows", len(ds.schema.Fields()), ds.rows),
		Revision:      ds.revision,
		Alias:         ds.alias,
	}
	inst.mu.RUnlock()
	inst.emitAudit("grant", handle, res.Alias, res.Revision)
	return res, nil
}

// Retract forgets a dataset: deregister the key, unregister the provider,
// delete the file, drop the record (ADR-0134 SD2).
func (inst *Service) Retract(handle string) (err error) {
	inst.mu.Lock()
	ds := inst.datasets[handle]
	if ds == nil {
		inst.mu.Unlock()
		return eh.Errorf("adhocdata: unknown handle %q", handle)
	}
	delete(inst.datasets, handle)
	inst.totalBytes -= ds.bytes
	inst.mu.Unlock()

	inst.keys.DeregisterDatasetKey(handle)
	inst.reg.Unregister(handle)
	if rmErr := os.Remove(ds.path); rmErr != nil && !os.IsNotExist(rmErr) {
		inst.log.Warn().Err(rmErr).Str("handle", handle).Msg("adhocdata: remove file on retract")
	}
	inst.emitAudit("retract", handle, ds.alias, ds.revision)
	return nil
}

// checkQuotaLocked verifies the count and byte budgets for a publish of
// newBytes, treating existing (nil for a new dataset) as being replaced.
// The caller holds inst.mu.
func (inst *Service) checkQuotaLocked(existing *dataset, newBytes uint64) (err error) {
	count := len(inst.datasets)
	if existing == nil {
		count++
	}
	if count > MaxDatasets {
		return eh.Errorf("adhocdata: dataset count quota (%d) exceeded", MaxDatasets)
	}
	total := inst.totalBytes
	if existing != nil {
		total -= existing.bytes
	}
	total += newBytes
	if total > StoreMaxBytes {
		return eh.Errorf("adhocdata: store byte quota (%d) exceeded", StoreMaxBytes)
	}
	return nil
}

// mintHandleLocked returns a fresh, unused handle. The caller holds inst.mu.
func (inst *Service) mintHandleLocked() (handle string, err error) {
	for range 8 {
		h, hErr := newHandle()
		if hErr != nil {
			return "", hErr
		}
		if _, exists := inst.datasets[h]; !exists {
			return h, nil
		}
	}
	return "", eh.Errorf("adhocdata: could not mint a unique handle")
}

// sweep removes every file in the store directory, returning the count.
func (inst *Service) sweep() (removed int) {
	entries, err := os.ReadDir(inst.dir)
	if err != nil {
		inst.log.Warn().Err(err).Str("dir", inst.dir).Msg("adhocdata: read store dir")
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if rmErr := os.Remove(filepath.Join(inst.dir, e.Name())); rmErr != nil {
			inst.log.Warn().Err(rmErr).Str("file", e.Name()).Msg("adhocdata: remove file")
			continue
		}
		removed++
	}
	return
}

// emitAudit logs one structured event per capability operation; the log
// bridge routes it into the audit surface (the grant IS the audit event,
// ADR-0134 SD2).
func (inst *Service) emitAudit(op, handle, alias string, revision uint64) {
	inst.log.Info().
		Str("op", op).
		Str("handle", handle).
		Str("alias", alias).
		Uint64("revision", revision).
		Msg("adhocdata: " + op)
}

// canonicalize decodes an Arrow IPC stream, validates its type set,
// re-encodes it to a canonical stream, and counts its rows.
func canonicalize(streamBytes []byte) (schema *arrow.Schema, structure string, canonical []byte, rows uint64, err error) {
	rdr, err := ipc.NewReader(bytes.NewReader(streamBytes))
	if err != nil {
		return nil, "", nil, 0, eh.Errorf("adhocdata: decode arrow stream: %w", err)
	}
	defer rdr.Release()
	schema = rdr.Schema()
	structure, err = StructureFor(schema)
	if err != nil {
		return nil, "", nil, 0, err
	}
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	for rdr.Next() {
		rec := rdr.RecordBatch()
		rows += uint64(rec.NumRows())
		if wErr := w.Write(rec); wErr != nil {
			_ = w.Close()
			return nil, "", nil, 0, eh.Errorf("adhocdata: re-encode arrow stream: %w", wErr)
		}
	}
	if rErr := rdr.Err(); rErr != nil {
		_ = w.Close()
		return nil, "", nil, 0, eh.Errorf("adhocdata: read arrow stream: %w", rErr)
	}
	if cErr := w.Close(); cErr != nil {
		return nil, "", nil, 0, eh.Errorf("adhocdata: finalize arrow stream: %w", cErr)
	}
	return schema, structure, buf.Bytes(), rows, nil
}

// encryptToFile writes the chunk-AEAD encryption of plaintext under key to
// <path>.tmp and renames it into place, returning the ciphertext size.
func encryptToFile(path string, key, plaintext []byte) (n int64, err error) {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	w, err := NewWriter(f, key)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return 0, err
	}
	if _, err = w.Write(plaintext); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return 0, err
	}
	if err = w.Close(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return 0, err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return 0, err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return 0, err
	}
	if err = f.Close(); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	if err = os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	return fi.Size(), nil
}

// newHandle mints an unguessable handle: adhoc_ + 16 lowercase hex chars
// (64 bits from crypto/rand). It satisfies the keelson identifier rule.
func newHandle() (handle string, err error) {
	var b [8]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", eh.Errorf("adhocdata: generate handle: %w", err)
	}
	return "adhoc_" + hex.EncodeToString(b[:]), nil
}

// resolveStoreDir returns the configured store directory, defaulting to
// <user cache dir>/boxer/adhoc.
func resolveStoreDir() string {
	if d := StoreDir.Get(); d != "" {
		return d
	}
	if cache, err := os.UserCacheDir(); err == nil {
		return filepath.Join(cache, "boxer", "adhoc")
	}
	return filepath.Join(os.TempDir(), "boxer-adhoc")
}

// validAlias reports whether s is a bare identifier usable as a stable
// alias in an applet's frontmatter and rewrite.
func validAlias(s string) bool { return validColumnName(s) }
