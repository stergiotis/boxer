package capbroker

import (
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// BrokerAppId is the synthetic AppId the broker registers under on the bus.
// Apps inspecting Msg.Sender on grant replies see this string.
const BrokerAppId app.AppIdT = "runtime.broker"

// Broker subscribes to runtime.cap.request and arbitrates grants via a
// GrantPolicyI. On approval it mutates the target Client's caps so the
// next Publish/Subscribe succeeds. In-memory grant log for M2.3; M2.5
// routes records to boxer.facts.
type Broker struct {
	inst   *inprocbus.Inst
	log    zerolog.Logger
	policy GrantPolicyI

	busClient *inprocbus.Client
	unsub     func()

	mu         sync.Mutex
	nextId     atomic.Uint64
	grants     []GrantRecord
	factsStore factsstore.FactsStoreI
}

// SetFactsStore wires durable grant persistence. On each approved grant
// the broker writes a factsstore.GrantRow alongside its in-memory log.
// Pass nil to disable. Concurrent-safe.
func (inst *Broker) SetFactsStore(s factsstore.FactsStoreI) {
	inst.mu.Lock()
	inst.factsStore = s
	inst.mu.Unlock()
}

func (inst *Broker) currentFactsStore() (s factsstore.FactsStoreI) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	s = inst.factsStore
	return
}

// GrantRecord captures one approved grant. M2.3 retains this in memory only;
// M2.5 mirrors it into boxer.facts under KindGrant.
type GrantRecord struct {
	Id     uint64
	AppId  app.AppIdT
	Filter app.SubjectFilter
	Reason string
	At     time.Time
}

// NewBroker constructs a broker bound to inst with the given policy. The
// broker immediately subscribes to runtime.cap.request; callers must keep
// it alive for the lifetime of the bus and invoke Close to release the
// subscription. A nil policy defaults to DenyAllPolicy.
func NewBroker(inst *inprocbus.Inst, log zerolog.Logger, policy GrantPolicyI) (b *Broker, err error) {
	if inst == nil {
		err = eh.Errorf("broker: nil inst")
		return
	}
	if policy == nil {
		policy = &DenyAllPolicy{}
	}
	b = &Broker{
		inst:   inst,
		log:    log.With().Str("app", string(BrokerAppId)).Logger(),
		policy: policy,
	}
	b.busClient = inst.NewClient(BrokerAppId, []app.SubjectFilter{
		{Pattern: "runtime.cap.>", Direction: app.CapDirectionBoth, Reason: "broker handles cap requests"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "broker replies to inboxes"},
	})
	b.unsub, err = b.busClient.Subscribe(RequestSubject, b.handleRequest)
	if err != nil {
		err = eh.Errorf("broker: subscribe %s: %w", RequestSubject, err)
		return
	}
	return
}

// Close unsubscribes the broker from the request subject. Safe to call once;
// subsequent calls are no-ops.
func (inst *Broker) Close() {
	if inst.unsub != nil {
		inst.unsub()
		inst.unsub = nil
	}
}

// Grants returns a snapshot of all approved grants in insertion order.
// Useful for tests and for M2.5 audit-replay scenarios.
func (inst *Broker) Grants() (g []GrantRecord) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	g = make([]GrantRecord, len(inst.grants))
	copy(g, inst.grants)
	return
}

// SetPolicy swaps the active policy. Existing grants are unaffected; only
// future requests use the new policy. Concurrent-safe.
func (inst *Broker) SetPolicy(p GrantPolicyI) {
	if p == nil {
		p = &DenyAllPolicy{}
	}
	inst.mu.Lock()
	inst.policy = p
	inst.mu.Unlock()
}

func (inst *Broker) currentPolicy() (p GrantPolicyI) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	p = inst.policy
	return
}

func (inst *Broker) handleRequest(msg *app.Msg) {
	if msg.Reply == "" {
		inst.log.Warn().Str("subject", msg.Subject).Msg("broker: request without reply subject, dropping")
		return
	}
	req, err := UnmarshalRequest(msg.Payload)
	if err != nil {
		inst.replyError(msg.Reply, "decode error")
		return
	}
	if msg.Sender != req.AppId {
		// Hygiene-only warning: payload AppId disagrees with bus-confirmed
		// sender. Logged but not enforced — M4 NKey-based identity will be
		// the real boundary.
		inst.log.Warn().
			Str("sender", string(msg.Sender)).
			Str("payloadAppId", string(req.AppId)).
			Msg("broker: sender / payload AppId mismatch")
	}
	target, ok := inst.inst.ClientByAppId(req.AppId)
	if !ok {
		inst.replyError(msg.Reply, "unknown app")
		return
	}
	decision := inst.currentPolicy().Decide(req)
	if !decision.Granted {
		inst.replyError(msg.Reply, decision.Reason)
		return
	}
	target.AddCap(req.SubjectFilter)
	grantNum := inst.nextId.Add(1)
	now := time.Now()
	inst.mu.Lock()
	inst.grants = append(inst.grants, GrantRecord{
		Id:     grantNum,
		AppId:  req.AppId,
		Filter: req.SubjectFilter,
		Reason: decision.Reason,
		At:     now,
	})
	fs := inst.factsStore
	inst.mu.Unlock()
	if fs != nil {
		_, fsErr := fs.WriteGrant(factsstore.GrantRow{
			AppId:      req.AppId,
			Pattern:    req.SubjectFilter.Pattern,
			Direction:  req.SubjectFilter.Direction,
			Reason:     decision.Reason,
			Sticky:     req.SubjectFilter.Sticky,
			GrantedVia: "policy",
			Ts:         now,
		})
		if fsErr != nil {
			inst.log.Err(fsErr).Str("appId", string(req.AppId)).Msg("broker: facts-store grant write")
		}
	}
	reply := GrantReply{
		Granted: true,
		GrantId: strconv.FormatUint(grantNum, 10),
		Reason:  decision.Reason,
	}
	payload, err := MarshalReply(reply)
	if err != nil {
		inst.log.Err(err).Msg("broker: marshal reply")
		return
	}
	err = inst.busClient.Publish(msg.Reply, payload)
	if err != nil {
		inst.log.Err(err).Str("reply", msg.Reply).Msg("broker: publish reply")
	}
}

func (inst *Broker) replyError(replySubject, reason string) {
	reply := GrantReply{Granted: false, Reason: reason}
	payload, err := MarshalReply(reply)
	if err != nil {
		inst.log.Err(err).Msg("broker: marshal error reply")
		return
	}
	err = inst.busClient.Publish(replySubject, payload)
	if err != nil {
		inst.log.Err(err).Str("reply", replySubject).Msg("broker: publish error reply")
	}
}
