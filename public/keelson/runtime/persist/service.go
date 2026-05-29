//go:build llm_generated_opus47

package persist

import (
	"strings"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// ServiceAppId is the synthetic AppId the persist service registers under.
const ServiceAppId app.AppIdT = "runtime.persist"

// Service is the subject handler for runtime.persist.>. On each request it
// parses the subject for (alias, key, op), dispatches to the backend, and
// publishes a PersistReply on the request's inbox.
type Service struct {
	inst      *inprocbus.Inst
	backend   StorageBackendI
	busClient *inprocbus.Client
	unsub     func()
	log       zerolog.Logger
}

// NewService constructs and subscribes a Service. The caller MUST invoke
// Close to release the subscription. A nil backend errors at construction.
func NewService(inst *inprocbus.Inst, log zerolog.Logger, backend StorageBackendI) (s *Service, err error) {
	if inst == nil {
		err = eh.Errorf("persist: nil inst")
		return
	}
	if backend == nil {
		err = eh.Errorf("persist: nil backend")
		return
	}
	s = &Service{
		inst:    inst,
		backend: backend,
		log:     log.With().Str("app", string(ServiceAppId)).Logger(),
	}
	s.busClient = inst.NewClient(ServiceAppId, []app.SubjectFilter{
		{Pattern: SubjectPrefix + ">", Direction: app.CapDirectionBoth, Reason: "persist service serves all keys"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "persist service replies to inboxes"},
	})
	s.unsub, err = s.busClient.Subscribe(SubjectPrefix+">", s.handleRequest)
	if err != nil {
		err = eh.Errorf("persist: subscribe %s>: %w", SubjectPrefix, err)
		return
	}
	return
}

// Close releases the subscription. Safe to call once; further calls no-op.
func (inst *Service) Close() {
	if inst.unsub != nil {
		inst.unsub()
		inst.unsub = nil
	}
}

func (inst *Service) handleRequest(msg *app.Msg) {
	if msg.Reply == "" {
		inst.log.Warn().Str("subject", msg.Subject).Msg("persist: request without reply, dropping")
		return
	}
	alias, key, op, ok := parsePersistSubject(msg.Subject)
	if !ok {
		inst.replyError(msg.Reply, "malformed subject")
		return
	}
	// Hygiene-only sender check: Sender's alias should equal the subject's
	// alias. Log a warning on mismatch but proceed — M4 NKey identity will
	// be the real enforcement boundary.
	senderAlias := msg.Sender.SubjectAlias()
	if senderAlias != alias && msg.Sender != ServiceAppId {
		inst.log.Warn().
			Str("sender", string(msg.Sender)).
			Str("senderAlias", senderAlias).
			Str("subjectAlias", alias).
			Msg("persist: sender / subject alias mismatch")
	}
	switch op {
	case OpGet:
		inst.handleGet(msg.Reply, alias, key)
	case OpSet:
		inst.handleSet(msg.Reply, alias, key, msg.Payload)
	case OpDelete:
		inst.handleDelete(msg.Reply, alias, key)
	default:
		inst.replyError(msg.Reply, "unknown op: "+op)
	}
}

func (inst *Service) handleGet(replySubject, alias, key string) {
	value, found, err := inst.backend.Get(alias, key)
	if err != nil {
		inst.replyError(replySubject, err.Error())
		return
	}
	inst.reply(replySubject, PersistReply{Found: found, Value: value})
}

func (inst *Service) handleSet(replySubject, alias, key string, value []byte) {
	err := inst.backend.Set(alias, key, value)
	if err != nil {
		inst.replyError(replySubject, err.Error())
		return
	}
	inst.reply(replySubject, PersistReply{})
}

func (inst *Service) handleDelete(replySubject, alias, key string) {
	err := inst.backend.Delete(alias, key)
	if err != nil {
		inst.replyError(replySubject, err.Error())
		return
	}
	inst.reply(replySubject, PersistReply{})
}

func (inst *Service) reply(replySubject string, r PersistReply) {
	payload, err := MarshalReply(r)
	if err != nil {
		inst.log.Err(err).Msg("persist: marshal reply")
		return
	}
	err = inst.busClient.Publish(replySubject, payload)
	if err != nil {
		inst.log.Err(err).Str("reply", replySubject).Msg("persist: publish reply")
	}
}

func (inst *Service) replyError(replySubject, reason string) {
	inst.reply(replySubject, PersistReply{Error: reason})
}

// parsePersistSubject splits "runtime.persist.{alias}.{key}.{op}" into its
// parts. Returns ok=false when the subject does not conform.
func parsePersistSubject(subject string) (alias, key, op string, ok bool) {
	const want = "runtime.persist."
	if !strings.HasPrefix(subject, want) {
		return
	}
	rest := subject[len(want):]
	tokens := strings.Split(rest, ".")
	if len(tokens) != 3 {
		return
	}
	alias = tokens[0]
	key = tokens[1]
	op = tokens[2]
	if alias == "" || key == "" || op == "" {
		return
	}
	ok = true
	return
}
