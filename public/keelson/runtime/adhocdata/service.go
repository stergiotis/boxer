// Package adhocdata is the ad-hoc dataset capability (ADR-0240, from
// ADR-0134): a running app hands tabular data to SQL without creating
// durable state, a durable public name, or plaintext at rest. A dataset is
// a [sealed.File] — an unnamed inode under a key that exists only inside
// it — registered under an unguessable handle that is a valid
// `keelson('…')` table name, published under a stable alias, owned by the
// instance that published it, and withdrawn in two phases so a query that
// already resolved it completes.
package adhocdata

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/sealed"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Quotas bound the store (ADR-0240 §SD2). A publish that would breach one
// is refused with a named error, never discovered at query time.
const (
	// PerDatasetMaxBytes caps one dataset — checked against the incoming
	// stream before it is decoded, and against the ciphertext after.
	PerDatasetMaxBytes = 256 << 20 // 256 MiB
	// StoreMaxBytes caps the live datasets' ciphertext together.
	StoreMaxBytes = 1 << 30 // 1 GiB
	// MaxDatasets caps how many datasets may be live at once.
	MaxDatasets = 64
)

// ServiceAppId is the synthetic identity the capability service speaks
// under on the bus; audit rows attribute publishes and retracts to it.
const ServiceAppId app.AppIdT = "runtime.adhoc"

// DefaultRetractGrace bounds both halves of a withdrawal (ADR-0240 §SD4):
// how long a dataset that has left stays registered for a query that
// resolved it but has not fetched yet, and then how long an open reader
// may keep the file after that. One bus request timeout each.
const DefaultRetractGrace = inprocbus.DefaultRequestTimeout

// ErrNotOwner is returned when a republish or retract comes from an
// identity other than the one that published the dataset (ADR-0240 §SD2).
// Hygiene, not security: it turns "any app that can read the catalog can
// delete anyone's dataset" into an accident that cannot happen.
var ErrNotOwner = errors.New("not the dataset's publisher")

// ErrClosed is returned by every operation after Close.
var ErrClosed = errors.New("adhocdata: service closed")

// ErrNoLiveDataset is Resolve's answer when nothing is published under the
// alias (or everything under it has been retracted). It travels the wire as
// its own flag so a bus caller can tell it from a transport failure with
// errors.Is — the difference between "wait" and "retry".
var ErrNoLiveDataset = errors.New("no live dataset under alias")

// Identity is who publishes or retracts: the bus envelope's sender app and
// sender instance (the window or embed the client was minted for). The zero
// Identity is the runtime itself — an in-process caller of the Go API —
// which owns everything.
type Identity struct {
	App      app.AppIdT
	Instance uint64
}

// IsRuntime reports whether the identity is the runtime's own.
func (inst Identity) IsRuntime() (yes bool) { return inst.App == "" }

// Config parameterises the capability Service.
type Config struct {
	// Bus, when non-nil, backs the adhoc.publish/retract/resolve
	// request/reply subjects and the adhoc.event.* announcements. Nil
	// leaves only the in-process Go methods.
	Bus *inprocbus.Inst
	// Registry is where dataset handles register as providers; defaults to
	// introspect.Default.
	Registry *introspect.Registry
	// Dir is the directory whose filesystem holds the unnamed sealed files;
	// empty resolves from sealed.BaseDir. Nothing is ever listable in it.
	Dir string
	// Log is the service logger.
	Log zerolog.Logger
	// RetractGrace overrides DefaultRetractGrace; zero keeps the default.
	RetractGrace time.Duration
}

// PublishInput is the in-process shape of a publish (the bus wire mirrors
// it). Handle empty mints a new dataset; a known Handle republishes it,
// which its publisher alone may do.
type PublishInput struct {
	Alias          string
	Handle         string
	ArrowIPCStream []byte
	// By is the publisher. The bus handler fills it from the envelope; an
	// in-process caller leaves it zero and publishes as the runtime.
	By Identity
	// KeepAfterClose marks the dataset as owned by the publishing app
	// rather than the publishing instance: any instance of the app may
	// republish or retract it, and it survives the publishing window
	// (ADR-0240 §SD5). Sticky across republishes.
	KeepAfterClose bool
}

// PublishResult reports the minted (or reused) handle and dataset stats.
type PublishResult struct {
	Handle   string
	Revision uint64
	Rows     uint64
	Bytes    uint64
}

// ResolveResult is Resolve's answer: the newest live dataset under an alias.
type ResolveResult struct {
	Handle          string
	Revision        uint64
	Rows            uint64
	Bytes           uint64
	CreatedAtUnixUs int64
}

// record is the one record of a dataset: the registry provider, the owner,
// the stats and the sealed file, under one lock (ADR-0240 §SD2). It
// implements introspect.EncryptedDatasetI, so the registry entry *is* the
// record and /table opens it directly.
type record struct {
	handle string

	mu             sync.RWMutex
	alias          string
	owner          Identity
	keepAfterClose bool
	schema         *arrow.Schema
	structure      string
	revision       uint64
	rows           uint64
	bytes          uint64 // ciphertext
	createdAt      int64  // unix µs
	file           *sealed.File
}

var _ introspect.EncryptedDatasetI = (*record)(nil)

// Name is the dataset's handle — a valid keelson table name.
func (inst *record) Name() (s string) { return inst.handle }

// Freshness is always Live: a republish must never serve a cached snapshot.
func (inst *record) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }

// Schema returns the dataset's Arrow schema.
func (inst *record) Schema() (s *arrow.Schema) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	return inst.schema
}

// Structure returns the explicit ClickHouse structure string.
func (inst *record) Structure() (s string) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	return inst.structure
}

// Revision returns the current dataset revision.
func (inst *record) Revision() (r uint64) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	return inst.revision
}

// Snapshot is never valid for a sealed dataset; the honest answer to an
// accidental snapshot is an error, not ciphertext.
func (inst *record) Snapshot(introspect.Projection) (arrow.RecordBatch, error) {
	return nil, eb.Build().Str("name", inst.handle).Errorf("adhocdata: the table is a sealed dataset; it is read through /table, not Snapshot")
}

// Open returns a reader over the plaintext and the revision it belongs to,
// taken together under the record's lock so a republish cannot split them.
func (inst *record) Open() (rc io.ReadSeekCloser, revision uint64, err error) {
	inst.mu.RLock()
	f, rev := inst.file, inst.revision
	inst.mu.RUnlock()
	r, err := f.Open()
	if err != nil {
		return nil, 0, err
	}
	return r, rev, nil
}

// Service owns the live datasets: it validates and seals published data,
// mints handles, registers them as providers, resolves aliases and
// withdraws in two phases. It is safe for concurrent use.
type Service struct {
	reg          *introspect.Registry
	dir          string
	log          zerolog.Logger
	retractGrace time.Duration

	busClient *inprocbus.Client
	unsubs    []func()

	mu         sync.RWMutex
	live       map[string]*record
	leaving    map[string]*time.Timer // left, still registered until the timer unloads
	totalBytes uint64
	closed     bool
}

// NewService builds the Service and, when a bus is supplied, subscribes to
// the capability subjects. Nothing is swept: a sealed file has no name.
func NewService(cfg Config) (inst *Service, err error) {
	reg := cfg.Registry
	if reg == nil {
		reg = introspect.Default
	}
	dir := cfg.Dir
	if dir == "" {
		dir = sealed.BaseDirPath()
	}
	grace := cfg.RetractGrace
	if grace <= 0 {
		grace = DefaultRetractGrace
	}
	inst = &Service{
		reg:          reg,
		dir:          dir,
		log:          cfg.Log,
		retractGrace: grace,
		live:         make(map[string]*record),
		leaving:      make(map[string]*time.Timer),
	}
	// A probe publish is not worth a start-up dependency on the base
	// directory, but an unusable one must not surface as the first app's
	// publish error: allocate and drop one unnamed file now.
	probe, pErr := sealed.CreateIn(dir)
	if pErr != nil {
		return nil, eh.Errorf("adhocdata: sealed store unavailable: %w", pErr)
	}
	_ = probe.Close()
	if regErr := reg.Register(newCatalogProvider(inst)); regErr != nil {
		return nil, eb.Build().Str("catalogTableName", CatalogTableName).Errorf("adhocdata: register catalog: %w", regErr)
	}
	if cfg.Bus != nil {
		if subErr := inst.subscribe(cfg.Bus); subErr != nil {
			return nil, subErr
		}
	}
	return inst, nil
}

// Close unsubscribes, unregisters every provider and closes every sealed
// file at once — readers mid-flight fail rather than outlive the service.
// Idempotent; every later operation returns ErrClosed.
func (inst *Service) Close(context.Context) (err error) {
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return
	}
	inst.closed = true
	recs := make([]*record, 0, len(inst.live)+len(inst.leaving))
	for _, r := range inst.live {
		recs = append(recs, r)
	}
	for h, timer := range inst.leaving {
		timer.Stop()
		if r, ok := inst.reg.Lookup(h); ok {
			if rec, isRec := r.(*record); isRec {
				recs = append(recs, rec)
			}
		}
	}
	inst.live = make(map[string]*record)
	inst.leaving = make(map[string]*time.Timer)
	inst.totalBytes = 0
	unsubs := inst.unsubs
	inst.unsubs = nil
	inst.mu.Unlock()

	for _, u := range unsubs {
		u()
	}
	for _, r := range recs {
		inst.reg.Unregister(r.handle)
		r.mu.RLock()
		f := r.file
		r.mu.RUnlock()
		_ = f.Close()
	}
	inst.reg.Unregister(CatalogTableName)
	inst.log.Info().Int("datasets", len(recs)).Msg("adhocdata: closed")
	return nil
}

// FlushRetracts runs the UNLOAD step now for every dataset that has left
// but whose grace has not elapsed, closing their files regardless of open
// readers. Tests use it to make the two-phase withdrawal synchronous.
func (inst *Service) FlushRetracts() {
	inst.mu.Lock()
	handles := make([]string, 0, len(inst.leaving))
	for h, timer := range inst.leaving {
		timer.Stop()
		handles = append(handles, h)
	}
	inst.leaving = make(map[string]*time.Timer)
	inst.mu.Unlock()
	for _, h := range handles {
		inst.unload(h, 0)
	}
}

// Publish seals in.ArrowIPCStream into a new unnamed file and registers
// it under a fresh handle, or — with in.Handle set — swaps it into that
// dataset's record, bumping the revision (ADR-0240 §SD2). The stream is
// decoded batch by batch into the sealed writer; no canonical copy is
// held. A republish or a publish onto an unknown, retracted or foreign
// handle is refused before any byte is sealed.
func (inst *Service) Publish(in PublishInput) (res PublishResult, err error) {
	if !validAlias(in.Alias) {
		return res, eb.Build().Str("alias", in.Alias).Errorf("adhocdata: invalid alias (want [A-Za-z_][A-Za-z0-9_]*, <=64)")
	}
	if uint64(len(in.ArrowIPCStream)) > PerDatasetMaxBytes {
		return res, eb.Build().Int("quotaBytes", PerDatasetMaxBytes).Errorf("adhocdata: dataset exceeds the per-dataset quota")
	}

	// Admission under the lock: identity, ownership, count and an estimate
	// of the byte budget (the stream's own length; the ciphertext is
	// re-checked exactly at commit). Nothing is reserved — a concurrent
	// publish is re-admitted at commit against the state it then finds.
	inst.mu.RLock()
	if inst.closed {
		inst.mu.RUnlock()
		return res, ErrClosed
	}
	var existing *record
	if in.Handle != "" {
		existing = inst.live[in.Handle]
		if existing == nil {
			inst.mu.RUnlock()
			return res, eb.Build().Str("handle", in.Handle).Errorf("adhocdata: unknown handle to republish")
		}
		if ownErr := existing.checkOwner(in.By); ownErr != nil {
			inst.mu.RUnlock()
			return res, ownErr
		}
	}
	err = inst.checkQuotaLocked(existing, uint64(len(in.ArrowIPCStream)))
	inst.mu.RUnlock()
	if err != nil {
		return res, err
	}

	f, err := sealed.CreateIn(inst.dir)
	if err != nil {
		return res, eh.Errorf("adhocdata: allocate sealed file: %w", err)
	}
	schema, structure, rows, err := sealStream(f, in.ArrowIPCStream)
	if err != nil {
		_ = f.Close()
		return res, err
	}
	nbytes := uint64(f.Size())
	if nbytes > PerDatasetMaxBytes {
		_ = f.Close()
		return res, eb.Build().Int("quotaBytes", PerDatasetMaxBytes).Errorf("adhocdata: dataset exceeds the per-dataset quota")
	}

	// Commit under the lock, against the state as it is now.
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		_ = f.Close()
		return res, ErrClosed
	}
	var old *sealed.File
	var rec *record
	if in.Handle != "" {
		rec = inst.live[in.Handle]
		if rec == nil {
			inst.mu.Unlock()
			_ = f.Close()
			return res, eb.Build().Str("handle", in.Handle).Errorf("adhocdata: handle retracted during publish")
		}
	}
	if quErr := inst.checkQuotaLocked(rec, nbytes); quErr != nil {
		inst.mu.Unlock()
		_ = f.Close()
		return res, quErr
	}
	var revision uint64
	var publisher Identity
	if rec != nil {
		rec.mu.Lock()
		old = rec.file
		inst.totalBytes -= rec.bytes
		rec.alias = in.Alias
		rec.schema, rec.structure = schema, structure
		rec.revision++
		rec.rows, rec.bytes = rows, nbytes
		rec.file = f
		rec.keepAfterClose = rec.keepAfterClose || in.KeepAfterClose
		revision, publisher = rec.revision, rec.owner
		rec.mu.Unlock()
		inst.totalBytes += nbytes
	} else {
		handle, hErr := inst.mintHandleLocked()
		if hErr != nil {
			inst.mu.Unlock()
			_ = f.Close()
			return res, hErr
		}
		rec = &record{
			handle: handle, alias: in.Alias, owner: in.By, keepAfterClose: in.KeepAfterClose,
			schema: schema, structure: structure, revision: 1, rows: rows, bytes: nbytes,
			createdAt: time.Now().UnixMicro(), file: f,
		}
		if regErr := inst.reg.Register(rec); regErr != nil {
			inst.mu.Unlock()
			_ = f.Close()
			return res, eb.Build().Str("handle", handle).Errorf("adhocdata: register: %w", regErr)
		}
		inst.live[handle] = rec
		inst.totalBytes += nbytes
		revision, publisher = 1, in.By
	}
	handle := rec.handle
	grace := inst.retractGrace
	inst.mu.Unlock()

	if old != nil {
		// Readers of the previous revision finish; the file goes with the
		// last of them, or at the ceiling.
		old.Retire(grace)
	}
	inst.emitAudit("publish", handle, in.Alias, revision)
	inst.publishEvent(SubjectEventPublished, Event{
		Op: EventOpPublished, Handle: handle, Alias: in.Alias, Publisher: string(publisher.App), Revision: revision,
	})
	return PublishResult{Handle: handle, Revision: revision, Rows: rows, Bytes: nbytes}, nil
}

// checkOwner is the ownership rule (ADR-0240 §SD2/§SD5): the runtime may
// touch anything; otherwise the app must match, and the instance too
// unless the dataset is kept after close, which makes it the app's.
func (inst *record) checkOwner(by Identity) (err error) {
	if by.IsRuntime() {
		return nil
	}
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	if inst.owner.App != by.App || (!inst.keepAfterClose && inst.owner.Instance != by.Instance) {
		return eb.Build().Str("handle", inst.handle).Str("app", string(by.App)).Uint64("instance", by.Instance).
			Errorf("adhocdata: %w", ErrNotOwner)
	}
	return nil
}

// IsLive reports whether handle names a dataset in the live set — published
// and not retracted. A dataset that has left but is still registered for
// its grace is not live: it still answers queries but no longer resolves,
// and a consumer verifying its binding should rebind (ADR-0188 §SD3).
func (inst *Service) IsLive(handle string) (live bool) {
	inst.mu.RLock()
	_, live = inst.live[handle]
	inst.mu.RUnlock()
	return
}

// LiveCount reports how many datasets are live.
func (inst *Service) LiveCount() (n int) {
	inst.mu.RLock()
	n = len(inst.live)
	inst.mu.RUnlock()
	return
}

// Resolve maps a stable alias to the newest live dataset published under
// it — newest by creation instant, ties broken on handle for determinism.
// A republish keeps its creation instant, so a producer that reuses one
// handle per alias stays the resolution target across re-captures.
func (inst *Service) Resolve(alias string) (res ResolveResult, err error) {
	inst.mu.RLock()
	if inst.closed {
		inst.mu.RUnlock()
		return res, ErrClosed
	}
	var best *record
	var bestAt int64
	for _, r := range inst.live {
		r.mu.RLock()
		match := r.alias == alias
		at := r.createdAt
		r.mu.RUnlock()
		if !match {
			continue
		}
		if best == nil || at > bestAt || (at == bestAt && r.handle > best.handle) {
			best, bestAt = r, at
		}
	}
	if best == nil {
		inst.mu.RUnlock()
		return res, eb.Build().Str("alias", alias).Errorf("adhocdata: resolve: %w", ErrNoLiveDataset)
	}
	best.mu.RLock()
	res = ResolveResult{
		Handle: best.handle, Revision: best.revision, Rows: best.rows, Bytes: best.bytes, CreatedAtUnixUs: best.createdAt,
	}
	best.mu.RUnlock()
	inst.mu.RUnlock()
	inst.emitAudit("resolve", res.Handle, alias, res.Revision)
	return res, nil
}

// Retract withdraws a dataset in two phases (ADR-0188 §SD3, ADR-0240
// §SD4). LEAVE, now: the record leaves the live set, so the catalog and
// Resolve stop naming it, its quota is released, and adhoc.event.retracted
// goes out. UNLOAD, after the grace: the provider is unregistered and the
// sealed file retires — at once if no reader is open, else with the last
// reader or at a second grace, whichever comes first. Only the publisher
// (or the runtime) may retract; a republish onto a retracted handle is
// refused as unknown from the leave step on.
func (inst *Service) Retract(handle string, by Identity) (err error) {
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return ErrClosed
	}
	rec := inst.live[handle]
	if rec == nil {
		inst.mu.Unlock()
		return eb.Build().Str("handle", handle).Errorf("adhocdata: unknown handle")
	}
	if ownErr := rec.checkOwner(by); ownErr != nil {
		inst.mu.Unlock()
		return ownErr
	}
	delete(inst.live, handle)
	rec.mu.RLock()
	alias, revision, publisher, nbytes := rec.alias, rec.revision, rec.owner, rec.bytes
	rec.mu.RUnlock()
	inst.totalBytes -= nbytes
	grace := inst.retractGrace
	inst.leaving[handle] = time.AfterFunc(grace, func() {
		inst.mu.Lock()
		if _, pending := inst.leaving[handle]; !pending {
			inst.mu.Unlock()
			return // flushed or closed meanwhile; that path unloaded it
		}
		delete(inst.leaving, handle)
		inst.mu.Unlock()
		inst.unload(handle, grace)
	})
	inst.mu.Unlock()

	inst.emitAudit("retract", handle, alias, revision)
	inst.publishEvent(SubjectEventRetracted, Event{
		Op: EventOpRetracted, Handle: handle, Alias: alias, Publisher: string(publisher.App), Revision: revision,
	})
	return nil
}

// unload is the UNLOAD step: the provider leaves the registry and the file
// retires once its readers are gone, bounded by ceiling (zero: at once).
func (inst *Service) unload(handle string, ceiling time.Duration) {
	p, ok := inst.reg.Lookup(handle)
	if !ok {
		return
	}
	rec, isRec := p.(*record)
	if !isRec {
		return
	}
	inst.reg.Unregister(handle)
	rec.mu.RLock()
	f, alias, revision := rec.file, rec.alias, rec.revision
	rec.mu.RUnlock()
	f.Retire(ceiling)
	inst.emitAudit("unload", handle, alias, revision)
}

// checkQuotaLocked verifies the count and byte budgets for a publish of
// newBytes, treating existing (nil for a new dataset) as being replaced.
// The caller holds inst.mu.
func (inst *Service) checkQuotaLocked(existing *record, newBytes uint64) (err error) {
	if existing == nil && len(inst.live) >= MaxDatasets {
		return eb.Build().Int("quotaCount", MaxDatasets).Errorf("adhocdata: dataset count quota exceeded")
	}
	total := inst.totalBytes
	if existing != nil {
		existing.mu.RLock()
		total -= existing.bytes
		existing.mu.RUnlock()
	}
	total += newBytes
	if total > StoreMaxBytes {
		return eb.Build().Int("quotaBytes", StoreMaxBytes).Errorf("adhocdata: store byte quota exceeded")
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
		if _, exists := inst.live[h]; exists {
			continue
		}
		if _, leaving := inst.leaving[h]; leaving {
			continue
		}
		return h, nil
	}
	return "", eh.Errorf("adhocdata: could not mint a unique handle")
}

// emitAudit logs one structured event per capability operation; the log
// bridge routes it into the audit surface.
func (inst *Service) emitAudit(op, handle, alias string, revision uint64) {
	inst.log.Info().
		Str("op", op).
		Str("handle", handle).
		Str("alias", alias).
		Uint64("revision", revision).
		Msg("adhocdata: " + op)
}

// sealStream decodes an Arrow IPC stream, validates its type set, and
// writes it batch by batch into the sealed file — one pass, no canonical
// copy — returning the schema, its ClickHouse structure and the row count.
// The writer is closed on success; on failure the caller closes the file.
func sealStream(f *sealed.File, streamBytes []byte) (schema *arrow.Schema, structure string, rows uint64, err error) {
	rdr, err := ipc.NewReader(bytes.NewReader(streamBytes))
	if err != nil {
		return nil, "", 0, eh.Errorf("adhocdata: decode arrow stream: %w", err)
	}
	defer rdr.Release()
	schema = rdr.Schema()
	structure, err = StructureFor(schema)
	if err != nil {
		return nil, "", 0, err
	}
	sw, err := f.Writer()
	if err != nil {
		return nil, "", 0, err
	}
	w := ipc.NewWriter(sw, ipc.WithSchema(schema))
	for rdr.Next() {
		rec := rdr.RecordBatch()
		rows += uint64(rec.NumRows())
		if wErr := w.Write(rec); wErr != nil {
			_ = w.Close()
			return nil, "", 0, eh.Errorf("adhocdata: seal arrow stream: %w", wErr)
		}
	}
	if rErr := rdr.Err(); rErr != nil {
		_ = w.Close()
		return nil, "", 0, eh.Errorf("adhocdata: read arrow stream: %w", rErr)
	}
	if cErr := w.Close(); cErr != nil {
		return nil, "", 0, eh.Errorf("adhocdata: finalize arrow stream: %w", cErr)
	}
	if cErr := sw.Close(); cErr != nil {
		return nil, "", 0, eh.Errorf("adhocdata: seal: %w", cErr)
	}
	return schema, structure, rows, nil
}

// newHandle mints an unguessable handle: adhoc_ + 16 lowercase hex chars
// (64 bits from crypto/rand). It satisfies the keelson identifier rule.
func newHandle() (handle string, err error) {
	var b [8]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", eh.Errorf("adhocdata: random handle: %w", err)
	}
	return "adhoc_" + hex.EncodeToString(b[:]), nil
}

// validAlias reports whether s is a bare identifier usable as a stable
// alias in an applet's frontmatter and rewrite — the table-name rule.
func validAlias(s string) bool { return introspect.ValidTableName(s) }
