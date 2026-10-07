package adhocdata

import (
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/adhocreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/adhocrequest"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// SubjectRead reads a dataset whole (ADR-0288 (proposed) §SD6): the
// stream as sealed, with its digest. It takes an alias and nothing else —
// no statement — so an app other than play can take a dataset in, but not
// query it.
const SubjectRead = "adhoc.read"

// AuditRead is the audited operation of an `adhoc.read`.
const AuditRead = "read"

// DestinationDataset and DestinationBundle are how a grant names a dataset
// and a bundle (ADR-0288 (proposed) §SD4); play's agent limits and the
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

// ErrDigestMismatch is ReadAllE's answer when the stream it received does
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
}

// Read returns the newest live dataset under alias, whole. A read leaves
// play's query surface (ADR-0288 (proposed) §SD6), so every read is
// audited, and one an agent's call caused is attested first and then held
// to the task's grant as a run in play is: it needs the bundle, or the
// dataset's alias, among the grant's destinations.
func (inst *Service) Read(alias string, by Identity, obo *app.OnBehalfOf) (res ReadResult, err error) {
	cc, err := inst.attest(by, obo)
	if err == nil {
		res, err = inst.read(alias)
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
		r.Outcome, r.Revision = AuditApplied, res.Revision
		r.Handles, r.Rows = []string{res.Handle}, []uint64{res.Rows}
		r.Bytes, r.StreamDigests = []uint64{uint64(len(res.ArrowIPCStream))}, []string{res.StreamDigest}
	}
	inst.audit(r)
	return
}

func (inst *Service) read(alias string) (res ReadResult, err error) {
	inst.mu.RLock()
	if inst.closed {
		inst.mu.RUnlock()
		return res, ErrClosed
	}
	var best *record
	var bestAt int64
	for _, r := range inst.live {
		r.mu.RLock()
		match, at := r.alias == alias, r.createdAt
		r.mu.RUnlock()
		if match && (best == nil || at > bestAt || (at == bestAt && r.handle > best.handle)) {
			best, bestAt = r, at
		}
	}
	inst.mu.RUnlock()
	if best == nil {
		return res, eb.Build().Str("alias", alias).Errorf("read: %w", ErrNoLiveDataset)
	}
	rc, revision, err := best.Open()
	if err != nil {
		return res, eb.Build().Str("alias", alias).Errorf("open dataset: %w", err)
	}
	defer func() { _ = rc.Close() }()
	stream, err := io.ReadAll(rc)
	if err != nil {
		return res, eb.Build().Str("alias", alias).Errorf("read dataset: %w", err)
	}
	best.mu.RLock()
	res = ReadResult{Alias: alias, Bundle: best.bundle, Handle: best.handle, Revision: revision, Rows: best.rows,
		ArrowIPCStream: stream, StreamDigest: best.streamDigest}
	if best.context.Has {
		res.PublisherTask = best.context.Val.Task
	}
	if best.revision != revision {
		// A republish swapped the file between Open and the stats; the
		// digest and rows are the new revision's. Hash what was read.
		res.StreamDigest = streamDigest(stream)
	}
	best.mu.RUnlock()
	return res, nil
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
	res, rErr := inst.Read(req.Alias, sender(msg), callContextFields(req.OboTask, req.OboEpoch, req.OboCall))
	if rErr != nil {
		rep := adhocreply.AdhocReply{At: time.Now().UTC(), Reason: rErr.Error(), NoLive: errors.Is(rErr, ErrNoLiveDataset)}
		var ge *GrantError
		if errors.As(rErr, &ge) {
			rep.Destination = ge.Destination
		}
		inst.reply(msg.Reply, rep)
		return
	}
	inst.reply(msg.Reply, adhocreply.AdhocReply{
		At: time.Now().UTC(), Ok: true, Handle: res.Handle, Bundle: res.Bundle, Revision: res.Revision, Rows: res.Rows,
		Bytes: uint64(len(res.ArrowIPCStream)), ArrowStream: res.ArrowIPCStream, StreamDigest: res.StreamDigest,
		PublisherTask: res.PublisherTask,
	})
}

// ReadAllE reads the newest live dataset under alias whole, via
// adhoc.read (ADR-0288 (proposed) §SD6), and checks the stream against
// the digest it was sealed under. It is how an app other than play takes a
// dataset in: no statement travels, so filtering, joining and aggregating
// stay play's, and what arrives is what was published. obo is the agent's
// call the read is work of, nil when it is none. The caller's bus client
// needs Pub on adhoc.read. Nothing live is a typed ErrNoLiveDataset.
func ReadAllE(bus app.BusI, alias string, obo *app.OnBehalfOf) (res ReadResult, err error) {
	payload, err := buscodec.Encode(adhocrequest.AdhocRequest{
		At: time.Now().UTC(), Op: adhocrequest.OpRead, Alias: alias,
		OboTask: oboTask(obo), OboEpoch: oboEpoch(obo), OboCall: oboCall(obo),
	})
	if err != nil {
		return res, eh.Errorf("encode read: %w", err)
	}
	raw, err := bus.Request(SubjectRead, payload)
	if err != nil {
		return res, eb.Build().Str("alias", alias).Errorf("read request: %w", err)
	}
	rep, err := buscodec.Decode[adhocreply.AdhocReply](raw)
	if err != nil {
		return res, eh.Errorf("decode read reply: %w", err)
	}
	if !rep.Ok {
		if rep.NoLive {
			return res, eb.Build().Str("alias", alias).Errorf("read: %w", ErrNoLiveDataset)
		}
		if rep.Destination != "" {
			return res, &GrantError{Destination: rep.Destination, Reason: strings.TrimPrefix(rep.Reason, "agent limit: ")}
		}
		return res, eb.Build().Str("alias", alias).Errorf("read rejected: %s", rep.Reason) //boxer:lint disable=CS013 reason="the service's refusal crosses the bus as text and is what a reader shows"
	}
	res = ReadResult{Alias: alias, Bundle: rep.Bundle, Handle: rep.Handle, Revision: rep.Revision, Rows: rep.Rows,
		ArrowIPCStream: rep.ArrowStream, StreamDigest: rep.StreamDigest, PublisherTask: rep.PublisherTask}
	if got := streamDigest(res.ArrowIPCStream); got != res.StreamDigest {
		return ReadResult{}, eb.Build().Str("alias", alias).Str("want", res.StreamDigest).Str("got", got).Errorf("%w", ErrDigestMismatch)
	}
	return res, nil
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
