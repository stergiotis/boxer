package inprocbus

import (
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/audit"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// DefaultRequestTimeout is the wait Request applies before returning
// ErrTimeout. Mirrors the NATS Go client default order of magnitude.
const DefaultRequestTimeout = 5 * time.Second

// Inst is the process-wide subject router. Apps don't talk to Inst directly;
// they receive permissioned clients via NewClient. Inst owns the
// subscription table, the client registry, and routes published messages
// by pattern match.
//
// The client registry holds the LIVE clients of each app id in creation
// order (ADR-0188 §SD1): a window mints one client, and closing that client
// removes it, so a privileged lookup by app id (the cap broker, the fs
// Powerbox) always lands on the newest client that is still open rather
// than on one whose window has been reaped.
type Inst struct {
	mu             sync.RWMutex
	subs           []*subscription
	clients        map[app.AppIdT][]*Client
	nextId         uint64
	requestTimeout time.Duration
	log            zerolog.Logger

	auditMu   sync.RWMutex
	auditSink audit.AuditSinkI
}

type subscription struct {
	id      uint64
	pattern string
	appId   app.AppIdT
	// instanceKey is the host-minted window/embed key of the client that
	// subscribed (0 when the client was minted without one). It is what
	// lets the live `subscriptions` table (ADR-0188 §SD4) attribute a
	// subscription to one window when several windows share an app id.
	instanceKey uint64
	handler     app.MsgHandlerFunc
}

// NewInst returns a fresh router. log receives internal diagnostics; pass
// zerolog.Nop() in tests to silence.
func NewInst(log zerolog.Logger) (inst *Inst) {
	inst = &Inst{
		clients:        make(map[app.AppIdT][]*Client),
		requestTimeout: DefaultRequestTimeout,
		log:            log,
	}
	return
}

// SetRequestTimeout overrides DefaultRequestTimeout for Request waits. Useful
// for tests that need quick failure paths.
func (inst *Inst) SetRequestTimeout(d time.Duration) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.requestTimeout = d
}

// NewClient returns a Client rooted at this Inst with the given app identity
// and subject permission set. The client is the handle apps consume as
// app.BusI; Inst itself stays internal to the host. The client is also
// registered on the Inst so privileged consumers (the cap broker) can look
// it up by AppId; several clients may be live under one app id (one per
// window), and ClientByAppId answers with the newest that has not been
// closed. Close removes a client from the registry (ADR-0188 §SD1).
func (inst *Inst) NewClient(appId app.AppIdT, caps []app.SubjectFilter) (c *Client) {
	c = &Client{
		inst:   inst,
		appId:  appId,
		caps:   caps,
		subIds: make(map[uint64]struct{}),
	}
	inst.mu.Lock()
	inst.clients[appId] = append(inst.clients[appId], c)
	inst.mu.Unlock()
	return
}

// dropClient removes c from the live-client registry; a no-op when c was
// already dropped. Called by Client.Close under no lock of its own.
func (inst *Inst) dropClient(c *Client) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	live := inst.clients[c.appId]
	for i, x := range live {
		if x == c {
			live = append(live[:i], live[i+1:]...)
			break
		}
	}
	if len(live) == 0 {
		delete(inst.clients, c.appId)
	} else {
		inst.clients[c.appId] = live
	}
}

// LiveClients returns a snapshot of every client that has been minted and
// not closed, in creation order per app id. It is the read surface of the
// `client_caps` introspection table (ADR-0188 §SD4).
func (inst *Inst) LiveClients() (cs []*Client) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	for _, live := range inst.clients {
		cs = append(cs, live...)
	}
	return
}

// SubscriptionInfo is one row of the live subscription table (ADR-0188
// §SD4): who holds which pattern, attributed to a window when the client
// carried an instance key.
type SubscriptionInfo struct {
	Id          uint64
	AppId       app.AppIdT
	InstanceKey uint64
	Pattern     string
}

// Subscriptions returns a snapshot of the subscription table. Reply inboxes
// allocated by in-flight Requests are included — they are subscriptions
// like any other and disappear when the request completes.
func (inst *Inst) Subscriptions() (rows []SubscriptionInfo) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	rows = make([]SubscriptionInfo, 0, len(inst.subs))
	for _, s := range inst.subs {
		rows = append(rows, SubscriptionInfo{
			Id:          s.id,
			AppId:       s.appId,
			InstanceKey: s.instanceKey,
			Pattern:     s.pattern,
		})
	}
	return
}

var _ app.BusProvider = (*Inst)(nil)

// NewBusClient satisfies app.BusProvider, minting a per-app client as
// app.BusI. The error is always nil (NewClient cannot fail); the signature
// matches the interface so NATS providers can surface dial errors.
func (inst *Inst) NewBusClient(appId app.AppIdT, caps []app.SubjectFilter) (bus app.BusI, err error) {
	bus = inst.NewClient(appId, caps)
	return
}

// ClientByAppId returns the newest live Client created via NewClient for
// the given AppId. Used by the cap broker to mutate a target app's caps
// after a grant. Grants stay addressed to an app id — the wire carries no
// instance key — so with several windows open the newest one receives the
// grant (recorded as pre-existing in ADR-0188).
func (inst *Inst) ClientByAppId(appId app.AppIdT) (c *Client, ok bool) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	live := inst.clients[appId]
	if len(live) == 0 {
		return
	}
	c = live[len(live)-1]
	ok = true
	return
}

// publish dispatches a message to all matching subscriptions. n is the
// number of handlers invoked; sender and senderInstance are recorded on the
// Msg so handlers can identify the originating app — and, since ADR-0191
// §SD2, the originating window — without inspecting payload.
func (inst *Inst) publish(sender app.AppIdT, senderInstance uint64, subject, reply string, payload []byte) (n int, err error) {
	err = ValidateSubject(subject)
	if err != nil {
		return
	}
	inst.mu.RLock()
	var matched []*subscription
	for _, s := range inst.subs {
		if Match(s.pattern, subject) {
			matched = append(matched, s)
		}
	}
	inst.mu.RUnlock()
	msg := &app.Msg{
		Subject:        subject,
		Reply:          reply,
		Sender:         sender,
		SenderInstance: senderInstance,
		Payload:        payload,
	}
	for _, s := range matched {
		s.handler(msg)
	}
	n = len(matched)
	return
}

func (inst *Inst) subscribe(appId app.AppIdT, instanceKey uint64, pattern string, handler app.MsgHandlerFunc) (id uint64, err error) {
	if handler == nil {
		err = eh.Errorf("subscribe: nil handler")
		return
	}
	err = ValidatePattern(pattern)
	if err != nil {
		return
	}
	inst.mu.Lock()
	inst.nextId++
	id = inst.nextId
	inst.subs = append(inst.subs, &subscription{
		id:          id,
		pattern:     pattern,
		appId:       appId,
		instanceKey: instanceKey,
		handler:     handler,
	})
	inst.mu.Unlock()
	return
}

func (inst *Inst) unsubscribe(id uint64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for i, s := range inst.subs {
		if s.id == id {
			inst.subs = append(inst.subs[:i], inst.subs[i+1:]...)
			return
		}
	}
}

func (inst *Inst) currentRequestTimeout() (d time.Duration) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	d = inst.requestTimeout
	return
}

// SetAuditSink installs an audit.AuditSinkI that receives one AuditRecord
// per Client.Request. Pass nil to disable auditing. Concurrent-safe.
func (inst *Inst) SetAuditSink(sink audit.AuditSinkI) {
	inst.auditMu.Lock()
	inst.auditSink = sink
	inst.auditMu.Unlock()
}

func (inst *Inst) currentAuditSink() (sink audit.AuditSinkI) {
	inst.auditMu.RLock()
	defer inst.auditMu.RUnlock()
	sink = inst.auditSink
	return
}
