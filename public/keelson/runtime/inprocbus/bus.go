//go:build llm_generated_opus47

package inprocbus

import (
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/audit"
)

// DefaultRequestTimeout is the wait Request applies before returning
// ErrTimeout. Mirrors the NATS Go client default order of magnitude.
const DefaultRequestTimeout = 5 * time.Second

// Inst is the process-wide subject router. Apps don't talk to Inst directly;
// they receive permissioned clients via NewClient. Inst owns the
// subscription table, the client registry, and routes published messages
// by pattern match.
type Inst struct {
	mu             sync.RWMutex
	subs           []*subscription
	clients        map[app.AppIdT]*Client
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
	handler app.MsgHandlerFunc
}

// NewInst returns a fresh router. log receives internal diagnostics; pass
// zerolog.Nop() in tests to silence.
func NewInst(log zerolog.Logger) (inst *Inst) {
	inst = &Inst{
		clients:        make(map[app.AppIdT]*Client),
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
// it up by AppId. Duplicate appIds replace the prior registration.
func (inst *Inst) NewClient(appId app.AppIdT, caps []app.SubjectFilter) (c *Client) {
	c = &Client{
		inst:  inst,
		appId: appId,
		caps:  caps,
	}
	inst.mu.Lock()
	inst.clients[appId] = c
	inst.mu.Unlock()
	return
}

// ClientByAppId returns the Client previously created via NewClient for the
// given AppId. Used by the cap broker to mutate a target app's caps after
// a grant.
func (inst *Inst) ClientByAppId(appId app.AppIdT) (c *Client, ok bool) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	c, ok = inst.clients[appId]
	return
}

// publish dispatches a message to all matching subscriptions. n is the
// number of handlers invoked; sender is recorded on the Msg so handlers
// can identify the originating app without inspecting payload.
func (inst *Inst) publish(sender app.AppIdT, subject, reply string, payload []byte) (n int, err error) {
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
		Subject: subject,
		Reply:   reply,
		Sender:  sender,
		Payload: payload,
	}
	for _, s := range matched {
		s.handler(msg)
	}
	n = len(matched)
	return
}

func (inst *Inst) subscribe(appId app.AppIdT, pattern string, handler app.MsgHandlerFunc) (id uint64, err error) {
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
		id:      id,
		pattern: pattern,
		appId:   appId,
		handler: handler,
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
