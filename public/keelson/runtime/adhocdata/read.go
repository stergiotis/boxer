package adhocdata

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/adhocreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/adhocrequest"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// SubjectRead reads a dataset whole (ADR-0288 §SD6): the
// stream as sealed, with its digest. It takes an alias and nothing else —
// no statement — so an app other than play can take a dataset in, but not
// query it.
const SubjectRead = "runtime.adhoc.read"

// AuditRead is the audited operation of an `runtime.adhoc.read`.
const AuditRead = "read"

// DestinationDataset and DestinationBundle are how a grant names a dataset
// and a bundle (ADR-0288 §SD4); play's agent limits and the
// read's check use the same spelling.
func DestinationDataset(alias string) (name string) { return "keelson:" + alias }

// DestinationBundle is how a grant names a bundle; it covers every
// dataset of it.
func DestinationBundle(bundle string) (name string) { return "keelson-bundle:" + bundle }

// ReadDestinations are the grant destinations that would cover a read of
// alias, the one a refusal asks for first: the bundle a member's alias
// names (`<bundle>__<local>`), then the alias itself. An app checks them
// against app.OnBehalfOf.Destinations to refuse an agent's read in its
// command, before any bus round trip; the service's check at the read
// stays the one that decides.
func ReadDestinations(alias string) (dests []string) {
	if bundle, _, ok := strings.Cut(alias, BundleAliasSeparator); ok && bundle != "" {
		dests = append(dests, DestinationBundle(bundle))
	}
	return append(dests, DestinationDataset(alias))
}

// GrantError is a read an agent's call caused that its task's grant does
// not cover (§SD6). Destination is what the grant would have to list; an
// app surfaces it with app.RefuseForDestinations so the coordinator can
// ask the person for it.
type GrantError struct {
	Destination string
	Reason      string
}

func (inst *GrantError) Error() (s string) { return "agent limit: " + inst.Reason }

// ErrDigestMismatch is ReadAll's answer when the stream it received does
// not hash to the digest the service sealed it under.
var ErrDigestMismatch = errors.New("dataset stream does not match its digest")

// ReadResult is a dataset read whole: its Arrow IPC stream as sealed — the
// bytes every reader of it reads — and where it came from.
type ReadResult struct {
	Alias          string
	Bundle         string
	Handle         string
	Revision       uint64
	Rows           uint64
	ArrowIPCStream []byte
	StreamDigest   string
	// PublisherTask is the task whose attested call published the
	// dataset's live revision; empty when no agent's call did.
	PublisherTask string
	// OwnClear is the service's answer for the call that read: the
	// revision in ArrowIPCStream was published by an attested call of the
	// reading task at TLP:CLEAR and derives from no sealed data, so what
	// the reader makes of it need not be labelled as derived from sealed
	// data for that task. False — for the person, an app's own read,
	// another task, a labelled or derived publication, and a reader that
	// does not look — leaves the read sealed.
	OwnClear bool
}

// Read returns the newest live dataset under alias, whole. A read leaves
// play's query surface (ADR-0288 §SD6), so every read is
// audited, and one an agent's call caused is attested first and then held
// to the task's grant as a run in play is: it needs the bundle, or the
// dataset's alias, among the grant's destinations.
//
// The answer says whether the revision read is the reading task's own
// publication at TLP:CLEAR (ReadResult.OwnClear), judged against the
// publication the stream was opened on.
func (inst *Service) Read(ctx context.Context, alias string, by Identity, obo *app.OnBehalfOf) (res ReadResult, err error) {
	cc, err := inst.attest(by, obo)
	var pub published
	if err == nil {
		res, pub, err = inst.read(alias)
	}
	if err == nil && cc.Has {
		err = inst.checkGrant(obo, res)
		if err != nil {
			res = ReadResult{Alias: res.Alias, Bundle: res.Bundle}
		}
	}
	r := AuditRecord{Operation: AuditRead, Bundle: res.Bundle, By: by, Context: cc, Aliases: []string{alias}}
	if err != nil {
		r.Outcome, r.Reason = AuditRefused, err.Error()
	} else {
		res.OwnClear = cc.Has && standingOf(pub.context, pub.derived, cc.Val.Task) == standingOwnClear
		r.Outcome, r.Revision = AuditApplied, res.Revision
		r.Handles, r.Rows = []string{res.Handle}, []uint64{res.Rows}
		r.Bytes, r.StreamDigests = []uint64{uint64(len(res.ArrowIPCStream))}, []string{res.StreamDigest}
		if res.OwnClear {
			r.Reason = ownClearReason(pub)
		}
	}
	inst.audit(ctx, r)
	return
}

// AuditReadLabel is the audited answer that a dataset a task reads by its
// handle is that task's own publication at TLP:CLEAR.
const AuditReadLabel = "read-label"

// ownClearReason is what the trail row of a read says when the read was
// taken as the reading task's own publication: why sealed data was read
// and no label followed.
func ownClearReason(pub published) (reason string) {
	return "read as the task's own publication at TLP:CLEAR (published by its call " + pub.context.Val.Call + ", from no sealed data): the read is not labelled"
}

// LabelResult is the service's answer about a dataset a call reads by its
// handle. Alias, Bundle and Revision are set only with OwnClear: the
// question is not held to the task's grant, so an answer about a dataset
// that is not the asking task's own names nothing of it.
type LabelResult struct {
	Handle   string
	Alias    string
	Bundle   string
	Revision uint64
	// OwnClear is as ReadResult.OwnClear, for the dataset registered under
	// Handle.
	OwnClear bool
}

// ReadLabel answers, for a call that reads the dataset registered under
// handle through this process's plane, whether that dataset is the reading
// task's own publication at TLP:CLEAR. A bundle's dataset keeps its handle,
// its bytes and its publication for as long as it is registered — a
// republish registers new handles — so the answer holds for every read of
// the handle, whenever it is made; a dataset published on its own, which a
// republish replaces under its handle, has no attested publisher and is
// never a task's own. The call is attested; only an own-clear answer and a
// refusal are audited, since every other answer leaves a read as it was.
func (inst *Service) ReadLabel(ctx context.Context, handle string, by Identity, obo *app.OnBehalfOf) (res LabelResult, err error) {
	res.Handle = handle
	cc, err := inst.attest(by, obo)
	if err != nil {
		inst.audit(ctx, AuditRecord{Operation: AuditReadLabel, Outcome: AuditRefused, Reason: err.Error(), By: by, Handles: []string{handle}})
		return
	}
	var rec *record
	if p, ok := inst.reg.Lookup(handle); ok {
		rec, _ = p.(*record)
	}
	if rec == nil || !cc.Has {
		return
	}
	rec.mu.RLock()
	pub := published{revision: rec.revision, context: rec.context, derived: rec.derived}
	alias, bundle := rec.alias, rec.bundle
	digest, rows, plain := rec.streamDigest, rec.rows, rec.plainBytes
	rec.mu.RUnlock()
	res.OwnClear = bundle != "" && standingOf(pub.context, pub.derived, cc.Val.Task) == standingOwnClear
	if res.OwnClear {
		res.Alias, res.Bundle, res.Revision = alias, bundle, pub.revision
		inst.audit(ctx, AuditRecord{Operation: AuditReadLabel, Outcome: AuditApplied, Reason: ownClearReason(pub), Bundle: res.Bundle,
			Revision: res.Revision, By: by, Context: cc, Aliases: []string{res.Alias}, Handles: []string{handle},
			Rows: []uint64{rows}, Bytes: []uint64{plain}, StreamDigests: []string{digest}})
	}
	return
}

// newest is the newest live dataset under alias.
func (inst *Service) newest(alias string) (best *record, err error) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	if inst.closed {
		return nil, ErrClosed
	}
	var bestAt int64
	for _, r := range inst.live {
		r.mu.RLock()
		match, at := r.alias == alias, r.createdAt
		r.mu.RUnlock()
		if match && (best == nil || at > bestAt || (at == bestAt && r.handle > best.handle)) {
			best, bestAt = r, at
		}
	}
	if best == nil {
		return nil, eb.Build().Str("alias", alias).Errorf("read: %w", ErrNoLiveDataset)
	}
	return
}

// soleDataset is the alias of the one dataset a live bundle named alias
// holds. A bundle of several says which there are, so the caller can name
// one; several is empty when alias names no bundle.
func (inst *Service) soleDataset(alias string) (sole string, several []string) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	b := inst.bundles[alias]
	if b == nil {
		return
	}
	if len(b.localNames) == 1 {
		return DatasetAlias(alias, b.localNames[0]), nil
	}
	for _, local := range b.localNames {
		several = append(several, DatasetAlias(alias, local))
	}
	return
}

// read reads the dataset under alias, or the one dataset of the bundle
// named alias.
func (inst *Service) read(alias string) (res ReadResult, pub published, err error) {
	best, err := inst.newest(alias)
	if errors.Is(err, ErrNoLiveDataset) {
		switch sole, several := inst.soleDataset(alias); {
		case sole != "":
			alias = sole
			best, err = inst.newest(alias)
		case len(several) > 0:
			return res, pub, eb.Build().Str("bundle", alias).Strs("datasets", several).Errorf("the bundle holds several datasets; name one of %s", strings.Join(several, ", ")) //boxer:lint disable=CS013 reason="the refusal crosses the bus as text; the dataset names are what the reader picks from"
		}
	}
	if err != nil {
		return res, pub, err
	}
	// The stream and the publication it belongs to are taken together:
	// what judges the publication judges the bytes read.
	rc, pub, err := best.openPublished()
	if err != nil {
		return res, pub, eb.Build().Str("alias", alias).Errorf("open dataset: %w", err)
	}
	defer func() { _ = rc.Close() }()
	stream, err := io.ReadAll(rc)
	if err != nil {
		return res, pub, eb.Build().Str("alias", alias).Errorf("read dataset: %w", err)
	}
	best.mu.RLock()
	res = ReadResult{Alias: alias, Bundle: best.bundle, Handle: best.handle, Revision: pub.revision, Rows: best.rows,
		ArrowIPCStream: stream, StreamDigest: best.streamDigest}
	if pub.context.Has {
		res.PublisherTask = pub.context.Val.Task
	}
	if best.revision != pub.revision {
		// A republish swapped the file between Open and the stats; the
		// digest and rows are the new revision's. Hash what was read.
		res.StreamDigest = streamDigest(stream)
	}
	best.mu.RUnlock()
	return res, pub, nil
}

// streamDigest is the content digest of a sealed stream, the form
// trail.ContentDigest uses.
func streamDigest(stream []byte) (digest string) {
	sum := blake3.Sum256(stream)
	return hex.EncodeToString(sum[:16])
}

func (inst *Service) handleRead(msg *app.Msg) {
	req, err := buscodec.Decode[adhocrequest.AdhocRequest](msg.Payload)
	if err != nil {
		inst.refuse(msg, "decode: "+err.Error())
		return
	}
	if req.ColumnsOnly {
		inst.handleReadColumns(msg, req)
		return
	}
	if req.LabelOnly {
		res, lErr := inst.ReadLabel(context.WithoutCancel(msg.Context()), req.Handle, sender(msg), callContextFields(req.OboTask, req.OboEpoch, req.OboCall))
		if lErr != nil {
			inst.reply(msg.Reply, readRefusal(lErr))
			return
		}
		inst.reply(msg.Reply, adhocreply.AdhocReply{At: time.Now().UTC(), Ok: true, Handle: res.Handle, Alias: res.Alias, Bundle: res.Bundle,
			Revision: res.Revision, OwnClear: res.OwnClear})
		return
	}
	res, rErr := inst.Read(context.WithoutCancel(msg.Context()), req.Alias, sender(msg), callContextFields(req.OboTask, req.OboEpoch, req.OboCall))
	if rErr != nil {
		inst.reply(msg.Reply, readRefusal(rErr))
		return
	}
	inst.reply(msg.Reply, adhocreply.AdhocReply{
		At: time.Now().UTC(), Ok: true, Handle: res.Handle, Bundle: res.Bundle, Revision: res.Revision, Rows: res.Rows,
		Bytes: uint64(len(res.ArrowIPCStream)), ArrowStream: res.ArrowIPCStream, StreamDigest: res.StreamDigest,
		PublisherTask: res.PublisherTask, OwnClear: res.OwnClear,
	})
}

// readRefusal is the reply to a refused read: the reason, whether nothing
// was live, and the grant destination it needed.
func readRefusal(err error) (rep adhocreply.AdhocReply) {
	rep = adhocreply.AdhocReply{At: time.Now().UTC(), Reason: err.Error(), NoLive: errors.Is(err, ErrNoLiveDataset)}
	var ge *GrantError
	if errors.As(err, &ge) {
		rep.Destination = ge.Destination
	}
	return
}

// readReplyError is the error a refused read's reply stands for.
func readReplyError(alias string, rep adhocreply.AdhocReply) (err error) {
	if rep.NoLive {
		return eb.Build().Str("alias", alias).Errorf("read: %w", ErrNoLiveDataset)
	}
	if rep.Destination != "" {
		return &GrantError{Destination: rep.Destination, Reason: strings.TrimPrefix(rep.Reason, "agent limit: ")}
	}
	return eb.Build().Str("alias", alias).Errorf("read rejected: %s", rep.Reason) //boxer:lint disable=CS013 reason="the service's refusal crosses the bus as text and is what a reader shows"
}

// ReadAll reads the newest live dataset under alias whole, via
// runtime.adhoc.read (ADR-0288 §SD6), and checks the stream against
// the digest it was sealed under. It is how an app other than play takes a
// dataset in: no statement travels, so filtering, joining and aggregating
// stay play's, and what arrives is what was published. obo is the agent's
// call the read is work of, nil when it is none. The caller's bus client
// needs Pub on runtime.adhoc.read. Nothing live is a typed ErrNoLiveDataset.
func ReadAll(ctx context.Context, bus app.BusI, alias string, obo *app.OnBehalfOf) (res ReadResult, err error) {
	payload, err := buscodec.Encode(adhocrequest.AdhocRequest{
		At: time.Now().UTC(), Op: adhocrequest.OpRead, Alias: alias,
		OboTask: oboTask(obo), OboEpoch: oboEpoch(obo), OboCall: oboCall(obo),
	})
	if err != nil {
		return res, eh.Errorf("encode read: %w", err)
	}
	raw, err := bus.Request(ctx, SubjectRead, payload)
	if err != nil {
		return res, eb.Build().Str("alias", alias).Errorf("read request: %w", err)
	}
	rep, err := buscodec.Decode[adhocreply.AdhocReply](raw)
	if err != nil {
		return res, eh.Errorf("decode read reply: %w", err)
	}
	if !rep.Ok {
		return res, readReplyError(alias, rep)
	}
	res = ReadResult{Alias: alias, Bundle: rep.Bundle, Handle: rep.Handle, Revision: rep.Revision, Rows: rep.Rows,
		ArrowIPCStream: rep.ArrowStream, StreamDigest: rep.StreamDigest, PublisherTask: rep.PublisherTask, OwnClear: rep.OwnClear}
	if got := streamDigest(res.ArrowIPCStream); got != res.StreamDigest {
		return ReadResult{}, eb.Build().Str("alias", alias).Str("want", res.StreamDigest).Str("got", got).Errorf("%w", ErrDigestMismatch)
	}
	return res, nil
}

// ReadLabelRequest asks the dataset service, over runtime.adhoc.read,
// whether the dataset registered under handle is the own publication at
// TLP:CLEAR of the task obo names (Service.ReadLabel). It is how an app
// that reads a dataset through this process's plane, where the stream does
// not pass through its hands, learns the label of what it read. Without an
// obo, or when the service cannot attest it, nothing is the caller's own:
// OwnClear is false or err is set, and the read stays sealed.
func ReadLabelRequest(ctx context.Context, bus app.BusI, handle string, obo *app.OnBehalfOf) (res LabelResult, err error) {
	payload, err := buscodec.Encode(adhocrequest.AdhocRequest{
		At: time.Now().UTC(), Op: adhocrequest.OpRead, Handle: handle, LabelOnly: true,
		OboTask: oboTask(obo), OboEpoch: oboEpoch(obo), OboCall: oboCall(obo),
	})
	if err != nil {
		return res, eh.Errorf("encode label read: %w", err)
	}
	raw, err := bus.Request(ctx, SubjectRead, payload)
	if err != nil {
		return res, eb.Build().Str("handle", handle).Errorf("label read request: %w", err)
	}
	rep, err := buscodec.Decode[adhocreply.AdhocReply](raw)
	if err != nil {
		return res, eh.Errorf("decode label read reply: %w", err)
	}
	if !rep.Ok {
		return res, readReplyError(handle, rep)
	}
	return LabelResult{Handle: handle, Alias: rep.Alias, Bundle: rep.Bundle, Revision: rep.Revision, OwnClear: rep.OwnClear}, nil
}

// checkGrant asks the dispatcher whether the task's grant lists the
// bundle res belongs to, or its alias; it refuses with the bundle's
// destination first, the one that covers the bundle's other datasets. A
// task reads what it published without a grant entry (§SD4): the live
// revision's attested publisher is the reading task, which the caller's
// attestation has confirmed is obo's.
func (inst *Service) checkGrant(obo *app.OnBehalfOf, res ReadResult) (err error) {
	if res.PublisherTask != "" && res.PublisherTask == obo.Task {
		return nil
	}
	ref := inst.callCtx.Load()
	if ref == nil || ref.c == nil {
		return eh.Errorf("no dispatcher to check the grant: %w", ErrUnattested)
	}
	dests := make([]string, 0, 2)
	if res.Bundle != "" {
		dests = append(dests, DestinationBundle(res.Bundle))
	}
	dests = append(dests, DestinationDataset(res.Alias))
	var why string
	for _, d := range dests {
		ok, reason := ref.c.AllowDestination(obo.Task, obo.Epoch, d)
		if ok {
			return nil
		}
		if why == "" {
			why = reason
		}
	}
	return &GrantError{Destination: dests[0], Reason: "the grant does not list " + dests[0] + " (" + why + ")"}
}
