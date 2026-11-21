package appstate

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/appstatereply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/appstaterequest"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist/persiststore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// StoreI is what the service clears through: persist.StoreBackend, the one
// durable store every kind of app state lives on (ADR-0105 D3a and its
// Update of 2026-08-15).
type StoreI interface {
	LiveEntries(ctx context.Context, appId app.AppIdT) (entries []persist.StateEntry, err error)
	EntryAt(ctx context.Context, entityId string) (e persist.StateEntry, found bool, err error)
	DeleteEntity(ctx context.Context, entityId string) (err error)
}

var _ StoreI = (*persist.StoreBackend)(nil)

// BatchDeleterI is a store that tombstones many entries in one write. A
// forget through one is a single round trip to the store, whatever the app
// kept; through a plain StoreI it is one per entry.
type BatchDeleterI interface {
	DeleteEntities(ctx context.Context, entityIds []string) (err error)
}

var _ BatchDeleterI = (*persist.StoreBackend)(nil)

// Service answers `runtime.appstate.<op>`. Without a durable store it still
// subscribes and refuses every request with the reason, so a manager is
// told what is wrong instead of waiting out a timeout — the in-memory
// fallbacks hold no state another process could see, and keelson('app_state')
// is empty in that mode anyway.
type Service struct {
	store     StoreI
	busClient *inprocbus.Client
	unsub     func()
	log       zerolog.Logger
}

// NewService constructs and subscribes a Service. store may be nil (see
// Service). The caller MUST invoke Close.
func NewService(inst *inprocbus.Inst, log zerolog.Logger, store StoreI) (s *Service, err error) {
	if inst == nil {
		err = eh.Errorf("appstate: nil bus")
		return
	}
	s = &Service{store: store, log: log.With().Str("app", string(ServiceAppId)).Logger()}
	s.busClient = inst.NewClient(ServiceAppId, ServiceCaps())
	s.unsub, err = s.busClient.Subscribe(SubjectAll, s.handleRequest)
	if err != nil {
		err = eb.Build().Str("subject", SubjectAll).Errorf("appstate: subscribe: %w", err)
		return
	}
	return
}

// Close releases the subscription. Safe to call more than once.
func (inst *Service) Close() {
	if inst.unsub != nil {
		inst.unsub()
		inst.unsub = nil
	}
}

func (inst *Service) handleRequest(msg *app.Msg) {
	if msg.Reply == "" {
		inst.log.Warn().Str("subject", msg.Subject).Msg("appstate: request without reply, dropping")
		return
	}
	op := strings.TrimPrefix(msg.Subject, SubjectPrefix)
	req, err := buscodec.Decode[appstaterequest.AppStateRequest](msg.Payload)
	if err != nil {
		inst.refuse(msg.Reply, "appstate: malformed request: "+err.Error())
		return
	}
	if req.Op != "" && req.Op != op {
		inst.refuse(msg.Reply, "appstate: op in subject ("+op+") and payload ("+req.Op+") disagree")
		return
	}
	if inst.store == nil {
		inst.refuse(msg.Reply, "appstate: no durable state store in this process — nothing another run could see is stored here")
		return
	}
	if req.AppId == "" {
		inst.refuse(msg.Reply, "appstate: the request names no app")
		return
	}
	var reply appstatereply.AppStateReply
	switch op {
	case appstaterequest.OpDelete:
		reply = inst.opDelete(msg.Context(), req)
	case appstaterequest.OpForget:
		reply = inst.opForget(msg.Context(), app.AppIdT(req.AppId))
	default:
		inst.refuse(msg.Reply, "appstate: unknown op: "+op)
		return
	}
	lg := inst.log.Info()
	if !reply.Ok {
		lg = inst.log.Warn()
	}
	lg.Str("op", op).Str("sender", string(msg.Sender)).Str("appId", req.AppId).
		Str("kind", req.Kind).Str("key", req.Key).Bool("ok", reply.Ok).Str("reason", reply.Reason).
		Msg("appstate: request")
	inst.reply(msg.Reply, reply)
}

// opDelete clears one entry. It is named as keelson('app_state') shows it:
// by (kind, key) for a known kind, by the store key for an `unknown` row.
// Either way the entry must be live and belong to the named app — a delete
// that addresses nothing, or someone else's entry, is refused rather than
// appending a tombstone to a key that was never there.
func (inst *Service) opDelete(ctx context.Context, req appstaterequest.AppStateRequest) (reply appstatereply.AppStateReply) {
	entityId := req.EntityId
	if req.Kind != persiststore.KindUnknown {
		id, err := persiststore.EntityIdOf(req.Kind, req.AppId, req.Key)
		if err != nil {
			return refusalOf("appstate: " + err.Error())
		}
		entityId = id
	}
	if entityId == "" {
		return refusalOf("appstate: a delete of an unknown kind names the entity by its store key")
	}
	e, found, err := inst.store.EntryAt(ctx, entityId)
	if err != nil {
		return refusalOf(err.Error())
	}
	if !found {
		return refusalOf("appstate: no live entry at " + entityId)
	}
	if string(e.AppId) != req.AppId || e.Kind != req.Kind {
		return refusalOf("appstate: the entry at " + entityId + " is " + e.Kind + " of " + string(e.AppId) + ", not " + req.Kind + " of " + req.AppId)
	}
	o := outcomes{}
	o.record(e.Kind, inst.store.DeleteEntity(ctx, entityId))
	return o.reply()
}

// opForget clears every live entry appId keeps, of every kind (ADR-0185
// §SD4). A partial clear is the worst outcome of the gesture, so the reply
// says per kind exactly what happened. A store that deletes in batches
// clears them in one write, so the forget costs the same few round trips
// however many entries there are; a failed batch is reported from a
// second read of what is still live, not assumed whole. Otherwise every
// entry is attempted on its own, and the first failure stops none of the
// rest.
func (inst *Service) opForget(ctx context.Context, appId app.AppIdT) (reply appstatereply.AppStateReply) {
	entries, err := inst.store.LiveEntries(ctx, appId)
	if err != nil {
		return refusalOf(err.Error())
	}
	o := outcomes{}
	if bd, ok := inst.store.(BatchDeleterI); ok && len(entries) > 0 {
		inst.forgetInOneWrite(ctx, bd, appId, entries, &o)
		return o.reply()
	}
	for _, e := range entries {
		o.record(e.Kind, inst.store.DeleteEntity(ctx, e.EntityId))
	}
	return o.reply()
}

// forgetInOneWrite deletes entries through bd. When the write fails, the
// entries still live afterwards are the failures and the rest were
// cleared; when that read fails too, none is counted as cleared.
func (inst *Service) forgetInOneWrite(ctx context.Context, bd BatchDeleterI, appId app.AppIdT, entries []persist.StateEntry, o *outcomes) {
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.EntityId)
	}
	err := bd.DeleteEntities(ctx, ids)
	if err == nil {
		for _, e := range entries {
			o.record(e.Kind, nil)
		}
		return
	}
	left, lerr := inst.store.LiveEntries(ctx, appId)
	still := make(map[string]bool, len(left))
	for _, e := range left {
		still[e.EntityId] = true
	}
	for _, e := range entries {
		if lerr != nil || still[e.EntityId] {
			o.record(e.Kind, err)
			continue
		}
		o.record(e.Kind, nil)
	}
}

// outcomes tallies deletes per kind and keeps the first failure's message.
type outcomes struct {
	cleared, failed map[string]uint64
	firstErr        error
}

func (inst *outcomes) record(kind string, err error) {
	if inst.cleared == nil {
		inst.cleared, inst.failed = map[string]uint64{}, map[string]uint64{}
	}
	if err != nil {
		inst.failed[kind]++
		if inst.firstErr == nil {
			inst.firstErr = err
		}
		return
	}
	inst.cleared[kind]++
}

func (inst *outcomes) reply() (r appstatereply.AppStateReply) {
	r = appstatereply.AppStateReply{At: time.Now().UTC(), Ok: inst.firstErr == nil}
	kinds := make([]string, 0, len(inst.cleared)+len(inst.failed))
	var failed uint64
	for k := range inst.cleared {
		kinds = append(kinds, k)
	}
	for k, n := range inst.failed {
		failed += n
		if _, seen := inst.cleared[k]; !seen {
			kinds = append(kinds, k)
		}
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		r.Kind = append(r.Kind, k)
		r.Cleared = append(r.Cleared, inst.cleared[k])
		r.Failed = append(r.Failed, inst.failed[k])
	}
	if inst.firstErr != nil {
		r.Reason = strconv.FormatUint(failed, 10) + " entries could not be cleared; first: " + inst.firstErr.Error()
	}
	return
}

func refusalOf(reason string) appstatereply.AppStateReply {
	return appstatereply.AppStateReply{At: time.Now().UTC(), Reason: reason}
}

func (inst *Service) refuse(replySubject string, reason string) {
	inst.reply(replySubject, refusalOf(reason))
}

func (inst *Service) reply(replySubject string, r appstatereply.AppStateReply) {
	if err := buscodec.Reply(inst.busClient.Publish, replySubject, r); err != nil {
		inst.log.Warn().Err(err).Str("reply", replySubject).Msg("appstate: publish reply")
	}
}
