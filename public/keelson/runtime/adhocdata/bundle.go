package adhocdata

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/sealed"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Bundle bounds (ADR-0288 (proposed) §SD1). The datasets of a bundle
// count against the dataset quotas like any other; these bound what the
// bundle adds.
const (
	// MaxBundleDatasets caps the datasets one bundle carries.
	MaxBundleDatasets = 16
	// MaxBundleDocumentBytes caps a bundle's applet document, held in
	// memory beside the records.
	MaxBundleDocumentBytes = 256 << 10
)

// BundleAliasSeparator joins a bundle alias and a local name into the
// dataset alias the service mints (ADR-0288 (proposed) §SD1), so a
// document reads its datasets by local name under any bundle alias.
const BundleAliasSeparator = "__"

// ErrAliasHeld is returned when a publish names an alias another owner
// holds (ADR-0288 (proposed) §SD3).
var ErrAliasHeld = errors.New("alias held by another publisher")

// ErrBundleMember is returned when a verb on one dataset names a dataset
// that belongs to a bundle: a bundle is published, republished and
// retracted whole (ADR-0288 (proposed) §SD2).
var ErrBundleMember = errors.New("dataset belongs to a bundle")

// ErrNoLiveBundle is ResolveBundle's answer when no bundle is live under
// the alias; like ErrNoLiveDataset it means "wait", not "retry".
var ErrNoLiveBundle = errors.New("no live bundle under alias")

// DatasetAlias is the alias the service publishes a bundle's dataset under.
func DatasetAlias(bundle string, localName string) (alias string) {
	return bundle + BundleAliasSeparator + localName
}

// BundleDatasetInput is one dataset of a bundle publish.
type BundleDatasetInput struct {
	// LocalName is the name the document reads the dataset by, in
	// `datasets:` and `keelson('…')`.
	LocalName      string
	ArrowIPCStream []byte
}

// BundlePublishInput is the in-process shape of a bundle publish (the bus
// wire mirrors it). A publish under an alias the same owner holds
// republishes the bundle: every dataset and the document are replaced
// together.
type BundlePublishInput struct {
	Alias string
	// Document is the applet document (ADR-0132) that reads the datasets.
	// The service stores it without parsing it.
	Document []byte
	Datasets []BundleDatasetInput
	// By is the publisher; the bus handler fills it from the envelope.
	By Identity
	// KeepAfterClose makes the bundle the app's rather than the window's
	// (ADR-0240 §SD5). Sticky across republishes.
	KeepAfterClose bool
	// OnBehalfOf is the agent's call the publish is work of, as the
	// publishing app received it; the dispatcher attests it (ADR-0288
	// (proposed) §SD5). Nil for the person's and the app's own publish.
	OnBehalfOf *app.OnBehalfOf
}

// BundleDataset is one dataset of a live bundle.
type BundleDataset struct {
	LocalName string
	Alias     string
	Handle    string
	Rows      uint64
	Bytes     uint64
	// StreamDigest is the content digest of the stream as sealed.
	StreamDigest string
}

// BundleResult describes a live bundle: what a publish produced and what
// a resolve finds.
type BundleResult struct {
	Alias          string
	Revision       uint64
	Document       []byte
	DocumentDigest string
	Datasets       []BundleDataset
	CreatedAtUs    int64
	// Owner is the publisher of record; Context the attested call that
	// published the live revision, when an agent's call did.
	Owner   Identity
	Context option.Option[app.CallContext]
}

// bundleRec is one live bundle. It is replaced, never mutated, so a
// pointer read under Service.mu stays a consistent snapshot after the
// lock is released.
type bundleRec struct {
	alias          string
	owner          Identity
	keepAfterClose bool
	revision       uint64
	document       []byte
	documentDigest string
	localNames     []string
	handles        []string
	createdAt      int64 // unix µs, kept across republishes
	context        option.Option[app.CallContext]
}

func (inst *bundleRec) checkOwner(by Identity) (err error) {
	if by.IsRuntime() {
		return nil
	}
	if inst.owner.App != by.App || (!inst.keepAfterClose && inst.owner.Instance != by.Instance) {
		return eb.Build().Str("bundle", inst.alias).Str("app", string(by.App)).Uint64("instance", by.Instance).
			Errorf("%w", ErrNotOwner)
	}
	return nil
}

// sealedDataset is one stream sealed ahead of a bundle's commit.
type sealedDataset struct {
	localName string
	alias     string
	file      *sealed.File
	schema    *arrow.Schema
	structure string
	rows      uint64
	bytes     uint64
	digest    string
}

func validateBundle(in BundlePublishInput) (err error) {
	if !validAlias(in.Alias) || strings.Contains(in.Alias, BundleAliasSeparator) {
		return eb.Build().Str("alias", in.Alias).Errorf("invalid bundle alias (want [A-Za-z_][A-Za-z0-9_]*, <=64, without a double underscore)")
	}
	if len(in.Document) == 0 {
		return eb.Build().Str("alias", in.Alias).Errorf("a bundle carries an applet document")
	}
	if len(in.Document) > MaxBundleDocumentBytes {
		return eb.Build().Str("alias", in.Alias).Int("limitBytes", MaxBundleDocumentBytes).Int("bytes", len(in.Document)).
			Errorf("bundle document exceeds its limit")
	}
	if len(in.Datasets) == 0 || len(in.Datasets) > MaxBundleDatasets {
		return eb.Build().Str("alias", in.Alias).Int("datasets", len(in.Datasets)).Int("limit", MaxBundleDatasets).
			Errorf("a bundle carries one or more datasets, up to its limit")
	}
	seen := make(map[string]struct{}, len(in.Datasets))
	for _, d := range in.Datasets {
		if !validAlias(d.LocalName) || !validAlias(DatasetAlias(in.Alias, d.LocalName)) {
			return eb.Build().Str("alias", in.Alias).Str("localName", d.LocalName).
				Errorf("invalid local name, or bundle alias and local name together exceed an alias")
		}
		if _, dup := seen[d.LocalName]; dup {
			return eb.Build().Str("alias", in.Alias).Str("localName", d.LocalName).Errorf("local name given twice")
		}
		seen[d.LocalName] = struct{}{}
		if uint64(len(d.ArrowIPCStream)) > PerDatasetMaxBytes {
			return eb.Build().Str("localName", d.LocalName).Int("quotaBytes", PerDatasetMaxBytes).
				Errorf("dataset exceeds the per-dataset quota")
		}
	}
	return nil
}

// PublishBundle seals every dataset of in, then makes them and the bundle
// live together under one lock (ADR-0288 (proposed) §SD2): a failure at
// any step leaves nothing of this publish live. A republish swaps the
// document and every dataset; the previous revision's datasets withdraw
// in two phases, so a reader that holds one finishes. Validation and
// quota checks run before any byte is sealed and again at commit. An
// agent-caused publish is attested first, and every publish, applied or
// refused, is audited (§SD5).
func (inst *Service) PublishBundle(in BundlePublishInput) (res BundleResult, err error) {
	cc, err := inst.attest(in.By, in.OnBehalfOf)
	op := AuditPublish
	if err == nil {
		res, op, err = inst.publishBundle(in, cc)
	}
	r := AuditRecord{Operation: op, Bundle: in.Alias, By: in.By, Context: cc}
	if err != nil {
		r.Outcome, r.Reason, r.Owner = AuditRefused, err.Error(), in.By
		for _, d := range in.Datasets {
			r.LocalNames = append(r.LocalNames, d.LocalName)
		}
		inst.audit(r)
		return
	}
	r.Outcome, r.Revision, r.Owner, r.DocumentDigest = AuditApplied, res.Revision, res.Owner, res.DocumentDigest
	r.fillDatasets(res.Datasets)
	inst.audit(r)
	return
}

// fillDatasets copies a bundle's datasets into the record's parallel lists.
func (inst *AuditRecord) fillDatasets(ds []BundleDataset) {
	inst.LocalNames = make([]string, 0, len(ds))
	inst.Aliases = make([]string, 0, len(ds))
	inst.Handles = make([]string, 0, len(ds))
	inst.Rows = make([]uint64, 0, len(ds))
	inst.Bytes = make([]uint64, 0, len(ds))
	inst.StreamDigests = make([]string, 0, len(ds))
	for _, d := range ds {
		inst.LocalNames = append(inst.LocalNames, d.LocalName)
		inst.Aliases = append(inst.Aliases, d.Alias)
		inst.Handles = append(inst.Handles, d.Handle)
		inst.Rows = append(inst.Rows, d.Rows)
		inst.Bytes = append(inst.Bytes, d.Bytes)
		inst.StreamDigests = append(inst.StreamDigests, d.StreamDigest)
	}
}

// publishBundle is PublishBundle's body; op is publish or republish.
func (inst *Service) publishBundle(in BundlePublishInput, cc option.Option[app.CallContext]) (res BundleResult, op string, err error) {
	op = AuditPublish
	err = validateBundle(in)
	if err != nil {
		return
	}
	newAliases := make([]string, 0, len(in.Datasets))
	var streamBytes uint64
	for _, d := range in.Datasets {
		newAliases = append(newAliases, DatasetAlias(in.Alias, d.LocalName))
		streamBytes += uint64(len(d.ArrowIPCStream))
	}

	inst.mu.RLock()
	if inst.closed {
		inst.mu.RUnlock()
		return res, op, ErrClosed
	}
	if inst.bundles[in.Alias] != nil {
		op = AuditRepublish
	}
	err = inst.admitBundleLocked(in, newAliases, streamBytes)
	inst.mu.RUnlock()
	if err != nil {
		return
	}

	sealedSet := make([]sealedDataset, 0, len(in.Datasets))
	closeAll := func() {
		for _, s := range sealedSet {
			_ = s.file.Close()
		}
	}
	var sealedBytes uint64
	for i, d := range in.Datasets {
		f, fErr := sealed.CreateIn(inst.dir)
		if fErr != nil {
			closeAll()
			return res, op, eh.Errorf("allocate sealed file: %w", fErr)
		}
		schema, structure, rows, digest, sErr := sealStream(f, d.ArrowIPCStream)
		if sErr != nil {
			_ = f.Close()
			closeAll()
			return res, op, eb.Build().Str("localName", d.LocalName).Errorf("seal: %w", sErr)
		}
		nbytes := uint64(f.Size())
		if nbytes > PerDatasetMaxBytes {
			_ = f.Close()
			closeAll()
			return res, op, eb.Build().Str("localName", d.LocalName).Int("quotaBytes", PerDatasetMaxBytes).
				Errorf("dataset exceeds the per-dataset quota")
		}
		sealedBytes += nbytes
		sealedSet = append(sealedSet, sealedDataset{localName: d.LocalName, alias: newAliases[i], file: f,
			schema: schema, structure: structure, rows: rows, bytes: nbytes, digest: digest})
	}
	digest := trail.ContentDigest(string(in.Document))
	document := slices.Clone(in.Document)

	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		closeAll()
		return res, op, ErrClosed
	}
	err = inst.admitBundleLocked(in, newAliases, sealedBytes)
	if err != nil {
		inst.mu.Unlock()
		closeAll()
		return
	}
	old := inst.bundles[in.Alias]
	now := time.Now().UnixMicro()
	owner, keep, revision, createdAt := in.By, in.KeepAfterClose, uint64(1), now
	if old != nil {
		owner, keep, revision, createdAt = old.owner, keep || old.keepAfterClose, old.revision+1, old.createdAt
	}
	registered := make([]*record, 0, len(sealedSet))
	for _, s := range sealedSet {
		handle, hErr := inst.mintHandleLocked()
		if hErr == nil {
			rec := &record{
				handle: handle, alias: s.alias, bundle: in.Alias, owner: owner, keepAfterClose: keep,
				schema: s.schema, structure: s.structure, revision: 1, rows: s.rows, bytes: s.bytes,
				createdAt: now, file: s.file, streamDigest: s.digest, context: cc,
			}
			hErr = inst.reg.Register(rec)
			if hErr == nil {
				inst.live[handle] = rec
				registered = append(registered, rec)
				continue
			}
		}
		for _, r := range registered {
			delete(inst.live, r.handle)
			inst.reg.Unregister(r.handle)
		}
		inst.mu.Unlock()
		closeAll()
		return res, op, eb.Build().Str("bundle", in.Alias).Errorf("register bundle dataset: %w", hErr)
	}
	var leftEvents []Event
	if old != nil {
		for _, h := range old.handles {
			if r := inst.live[h]; r != nil {
				leftEvents = append(leftEvents, inst.leaveLocked(r))
			}
		}
	}
	b := &bundleRec{
		alias: in.Alias, owner: owner, keepAfterClose: keep, revision: revision,
		document: document, documentDigest: digest, createdAt: createdAt, context: cc,
		localNames: make([]string, 0, len(registered)), handles: make([]string, 0, len(registered)),
	}
	for i, r := range registered {
		inst.totalBytes += r.bytes
		b.localNames = append(b.localNames, sealedSet[i].localName)
		b.handles = append(b.handles, r.handle)
	}
	inst.bundles[in.Alias] = b
	inst.mu.Unlock()

	res = b.result(registered)
	for _, ev := range leftEvents {
		inst.emitAudit("retract", ev.Handle, ev.Alias, ev.Revision)
		inst.publishEvent(SubjectEventRetracted, ev)
	}
	for _, r := range registered {
		inst.emitAudit("publish", r.handle, r.alias, r.revision)
		inst.publishEvent(SubjectEventPublished, Event{
			Op: EventOpPublished, Handle: r.handle, Alias: r.alias, Bundle: in.Alias, Publisher: string(b.owner.App), Revision: r.revision,
		})
	}
	inst.publishEvent(SubjectBundleEventPublished, Event{Op: EventOpPublished, Bundle: in.Alias, Publisher: string(b.owner.App), Revision: revision})
	return res, op, nil
}

// admitBundleLocked checks ownership, alias collisions and the quotas for
// a bundle publish against the state the caller holds Service.mu over.
// newBytes is the estimate before sealing and the exact ciphertext after.
func (inst *Service) admitBundleLocked(in BundlePublishInput, newAliases []string, newBytes uint64) (err error) {
	old := inst.bundles[in.Alias]
	if old != nil {
		err = old.checkOwner(in.By)
		if err != nil {
			return
		}
	} else if held := inst.aliasHolderLocked(in.Alias); held != nil && *held != in.By {
		return eb.Build().Str("alias", in.Alias).Str("holder", string(held.App)).Uint64("holderInstance", held.Instance).
			Errorf("%w", ErrAliasHeld)
	}
	var replacedCount int
	var replacedBytes uint64
	if old != nil {
		for _, h := range old.handles {
			if r := inst.live[h]; r != nil {
				replacedCount++
				replacedBytes += r.bytes
			}
		}
	}
	for _, a := range newAliases {
		for _, r := range inst.live {
			if r.alias == a && r.bundle != in.Alias {
				return eb.Build().Str("alias", a).Str("holder", string(r.owner.App)).Uint64("holderInstance", r.owner.Instance).
					Errorf("%w", ErrAliasHeld)
			}
		}
	}
	if len(inst.live)-replacedCount+len(newAliases) > MaxDatasets {
		return eb.Build().Int("quotaCount", MaxDatasets).Errorf("dataset count quota exceeded")
	}
	owner := in.By
	if old != nil {
		owner = old.owner
	}
	if !owner.IsRuntime() && inst.ownedCountLocked(owner)-replacedCount+len(newAliases) > MaxDatasetsPerOwner {
		return eb.Build().Int("quotaCount", MaxDatasetsPerOwner).Str("app", string(owner.App)).Uint64("instance", owner.Instance).
			Errorf("per-owner dataset count quota exceeded")
	}
	if inst.totalBytes-replacedBytes+newBytes > StoreMaxBytes {
		return eb.Build().Int("quotaBytes", StoreMaxBytes).Errorf("store byte quota exceeded")
	}
	return nil
}

// aliasHolderLocked returns the owner of a live dataset published on its
// own under alias, or nil. A bundle alias and a dataset alias share one
// namespace, so a bundle cannot take the name of another owner's dataset.
func (inst *Service) aliasHolderLocked(alias string) (holder *Identity) {
	for _, r := range inst.live {
		if r.alias == alias && r.bundle == "" {
			o := r.owner
			return &o
		}
	}
	return nil
}

// ResolveBundle returns the live bundle under alias: its document and its
// datasets with their handles, in the order they were published. A resolve
// an agent's call caused is attested and audited (ADR-0288 (proposed)
// §SD5); the person's and the apps' own resolves — every bundle view
// resolves on each revision — are not, so the trail records what an agent
// looked up rather than every follower's reconcile.
func (inst *Service) ResolveBundle(alias string, by Identity, obo *app.OnBehalfOf) (res BundleResult, err error) {
	cc, err := inst.attest(by, obo)
	if err == nil {
		res, err = inst.resolveBundle(alias)
	}
	if obo == nil {
		return
	}
	r := AuditRecord{Operation: AuditResolve, Bundle: alias, By: by, Context: cc}
	if err != nil {
		r.Outcome, r.Reason = AuditRefused, err.Error()
	} else {
		r.Outcome, r.Revision, r.Owner, r.DocumentDigest = AuditApplied, res.Revision, res.Owner, res.DocumentDigest
		r.fillDatasets(res.Datasets)
	}
	inst.audit(r)
	return
}

func (inst *Service) resolveBundle(alias string) (res BundleResult, err error) {
	inst.mu.RLock()
	if inst.closed {
		inst.mu.RUnlock()
		return res, ErrClosed
	}
	b := inst.bundles[alias]
	if b == nil {
		inst.mu.RUnlock()
		return res, eb.Build().Str("bundle", alias).Errorf("resolve: %w", ErrNoLiveBundle)
	}
	recs := make([]*record, 0, len(b.handles))
	for _, h := range b.handles {
		recs = append(recs, inst.live[h])
	}
	inst.mu.RUnlock()
	res = b.result(recs)
	return res, nil
}

// RetractBundle withdraws a bundle whole: every dataset leaves in two
// phases as Retract withdraws one, and the bundle stops resolving at once.
// Only its owner, or the runtime, may retract it. An agent-caused retract
// is attested first; every retract is audited (ADR-0288 (proposed) §SD5).
func (inst *Service) RetractBundle(alias string, by Identity, obo *app.OnBehalfOf) (err error) {
	cc, err := inst.attest(by, obo)
	if err != nil {
		inst.audit(AuditRecord{Operation: AuditRetract, Outcome: AuditRefused, Reason: err.Error(), Bundle: alias, By: by})
		return
	}
	return inst.retractBundle(alias, by, cc, AuditRetract)
}

// retractBundle is RetractBundle's body, audited as op: retract, or
// withdraw when the runtime retracts for a closed window.
func (inst *Service) retractBundle(alias string, by Identity, cc option.Option[app.CallContext], op string) (err error) {
	r := AuditRecord{Operation: op, Bundle: alias, By: by, Context: cc}
	defer func() {
		if err != nil {
			r.Outcome, r.Reason = AuditRefused, err.Error()
		} else {
			r.Outcome = AuditApplied
		}
		inst.audit(r)
	}()
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return ErrClosed
	}
	b := inst.bundles[alias]
	if b == nil {
		inst.mu.Unlock()
		return eb.Build().Str("bundle", alias).Errorf("retract: %w", ErrNoLiveBundle)
	}
	err = b.checkOwner(by)
	if err != nil {
		inst.mu.Unlock()
		return
	}
	delete(inst.bundles, alias)
	recs := make([]*record, 0, len(b.handles))
	for _, h := range b.handles {
		recs = append(recs, inst.live[h])
	}
	res := b.result(recs)
	events := make([]Event, 0, len(b.handles))
	for _, rec := range recs {
		if rec != nil {
			events = append(events, inst.leaveLocked(rec))
		}
	}
	inst.mu.Unlock()
	r.Revision, r.Owner, r.DocumentDigest = res.Revision, res.Owner, res.DocumentDigest
	r.fillDatasets(res.Datasets)
	for _, ev := range events {
		inst.emitAudit("retract", ev.Handle, ev.Alias, ev.Revision)
		inst.publishEvent(SubjectEventRetracted, ev)
	}
	inst.publishEvent(SubjectBundleEventRetracted, Event{Op: EventOpRetracted, Bundle: alias, Publisher: string(b.owner.App), Revision: b.revision})
	return nil
}

// result renders b with its dataset records; a record already gone (nil)
// keeps its name and handle with zero stats.
func (inst *bundleRec) result(recs []*record) (res BundleResult) {
	res = BundleResult{
		Alias: inst.alias, Revision: inst.revision, Document: inst.document, DocumentDigest: inst.documentDigest,
		CreatedAtUs: inst.createdAt, Datasets: make([]BundleDataset, 0, len(inst.handles)),
		Owner: inst.owner, Context: inst.context,
	}
	for i, h := range inst.handles {
		d := BundleDataset{LocalName: inst.localNames[i], Alias: DatasetAlias(inst.alias, inst.localNames[i]), Handle: h}
		if i < len(recs) && recs[i] != nil {
			recs[i].mu.RLock()
			d.Rows, d.Bytes, d.StreamDigest = recs[i].rows, recs[i].bytes, recs[i].streamDigest
			recs[i].mu.RUnlock()
		}
		res.Datasets = append(res.Datasets, d)
	}
	return
}
