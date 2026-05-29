//go:build llm_generated_opus47

package inprocbus

import (
	"errors"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/audit"
)

// InboxPrefix is the subject prefix the bus uses for ephemeral reply inboxes
// allocated by Request. Subscribers under this prefix bypass cap checks
// because the prefix is internal — clients never publish there directly.
const InboxPrefix = "_INBOX."

// Client is the per-app BusI implementation backed by an Inst. Every
// Publish, Subscribe, and Request is permission-checked against the app's
// declared SubjectFilter caps. The caller obtains a *Client from
// Inst.NewClient; the type is exported so privileged consumers (e.g. the
// cap broker) can call AddCap to extend permissions at runtime.
type Client struct {
	inst    *Inst
	appId   app.AppIdT
	capsMu  sync.RWMutex
	caps    []app.SubjectFilter
	inboxN  atomic.Uint64
}

var _ app.BusI = (*Client)(nil)

func (inst *Client) Publish(subject string, payload []byte) (err error) {
	if !inst.canPublish(subject) {
		err = eb.Build().Str("subject", subject).Str("appId", string(inst.appId)).
			Errorf("bus publish denied: %w", ErrPermissionViolation)
		return
	}
	_, err = inst.inst.publish(inst.appId, subject, "", payload)
	if err != nil {
		err = eh.Errorf("bus publish: %w", err)
	}
	return
}

// AppId returns the identity associated with this client. Used by privileged
// callers (e.g., the cap broker) to look the client up by app identity.
func (inst *Client) AppId() (id app.AppIdT) {
	id = inst.appId
	return
}

// AddCap extends the client's permission set with an additional
// SubjectFilter. Intended for the cap broker (post-M2.3): apps go from
// "denied" to "allowed" on a subject once a grant lands, without recreating
// the client. Idempotent — duplicate filters add a duplicate slice entry
// (cheap; SubjectAllowed short-circuits on first match).
func (inst *Client) AddCap(filter app.SubjectFilter) {
	inst.capsMu.Lock()
	defer inst.capsMu.Unlock()
	inst.caps = append(inst.caps, filter)
}

// Caps returns a snapshot of the current subject-filter set. Returned slice
// is a copy; callers may not assume it tracks subsequent AddCap calls.
func (inst *Client) Caps() (caps []app.SubjectFilter) {
	inst.capsMu.RLock()
	defer inst.capsMu.RUnlock()
	caps = make([]app.SubjectFilter, len(inst.caps))
	copy(caps, inst.caps)
	return
}

func (inst *Client) Subscribe(subject string, handler app.MsgHandlerFunc) (unsubscribe func(), err error) {
	isInbox := strings.HasPrefix(subject, InboxPrefix)
	if !isInbox && !inst.canSubscribe(subject) {
		err = eb.Build().Str("subject", subject).Str("appId", string(inst.appId)).
			Errorf("bus subscribe denied: %w", ErrPermissionViolation)
		return
	}
	var id uint64
	id, err = inst.inst.subscribe(inst.appId, subject, handler)
	if err != nil {
		err = eh.Errorf("bus subscribe: %w", err)
		return
	}
	unsubscribe = func() {
		inst.inst.unsubscribe(id)
	}
	return
}

func (inst *Client) Request(subject string, payload []byte) (reply []byte, err error) {
	start := time.Now()
	defer func() {
		sink := inst.inst.currentAuditSink()
		if sink == nil {
			return
		}
		result := audit.AuditResultOk
		if err != nil {
			switch {
			case errors.Is(err, ErrPermissionViolation):
				result = audit.AuditResultDenied
			case errors.Is(err, ErrTimeout):
				result = audit.AuditResultTimeout
			default:
				result = audit.AuditResultError
			}
		}
		sink.Record(audit.AuditRecord{
			AppId:         inst.appId,
			Subject:       subject,
			Result:        result,
			LatencyMs:     uint32(time.Since(start).Milliseconds()),
			RequestSizeB:  uint32(len(payload)),
			ResponseSizeB: uint32(len(reply)),
			Ts:            start,
		})
	}()
	if !inst.canPublish(subject) {
		err = eb.Build().Str("subject", subject).Str("appId", string(inst.appId)).
			Errorf("bus request denied: %w", ErrPermissionViolation)
		return
	}
	inbox := inst.allocateInbox()
	replyCh := make(chan []byte, 1)
	var inboxId uint64
	inboxId, err = inst.inst.subscribe(inst.appId, inbox, func(msg *app.Msg) {
		select {
		case replyCh <- msg.Payload:
		default:
		}
	})
	if err != nil {
		err = eh.Errorf("bus request: inbox subscribe: %w", err)
		return
	}
	defer inst.inst.unsubscribe(inboxId)
	_, err = inst.inst.publish(inst.appId, subject, inbox, payload)
	if err != nil {
		err = eh.Errorf("bus request: publish: %w", err)
		return
	}
	select {
	case reply = <-replyCh:
		return
	case <-time.After(inst.inst.currentRequestTimeout()):
		err = eb.Build().Str("subject", subject).Errorf("bus request: %w", ErrTimeout)
		return
	}
}

func (inst *Client) allocateInbox() (inbox string) {
	n := inst.inboxN.Add(1)
	// 32-bit random salt + monotonic per-client counter gives uniqueness
	// across clients within an Inst without coordinating a global state.
	inbox = InboxPrefix +
		strconv.FormatUint(uint64(rand.Uint32()), 36) +
		"." +
		strconv.FormatUint(n, 36)
	return
}

func (inst *Client) canPublish(subject string) (ok bool) {
	inst.capsMu.RLock()
	defer inst.capsMu.RUnlock()
	ok = SubjectAllowed(inst.caps, subject, app.CapDirectionPub)
	return
}

func (inst *Client) canSubscribe(subject string) (ok bool) {
	inst.capsMu.RLock()
	defer inst.capsMu.RUnlock()
	ok = SubjectAllowed(inst.caps, subject, app.CapDirectionSub)
	return
}

