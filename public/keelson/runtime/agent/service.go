package agent

import (
	"slices"
	"strings"
	"sync"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent/agentfacts"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// Config is the service's configuration.
type Config struct {
	// Registry is the app registry whose catalogs are served; nil is
	// app.DefaultRegistry.
	Registry *app.Registry
	// Host is the window host whose instances calls reach; nil leaves every
	// service past describe refusing.
	Host HostI
	// TestGrants lets request issue a grant without the person's approval
	// (ADR-0269 §SD6 "Test grants"). The host sets it only on the headless
	// host and only when TestGrantsEnv asks for it.
	TestGrants bool
	// Exec is the executor of the server holding boxer.facts; with it the
	// action record is also kept there (§SD9). nil keeps only the
	// in-process record.
	Exec recordstore.ExecutorI
}

// Service answers `runtime.agent.*` (ADR-0269 §SD3).
type Service struct {
	cfg       Config
	busClient *inprocbus.Client
	unsub     func()
	closeOnce sync.Once
	log       zerolog.Logger

	mu sync.Mutex
	// tasks are keyed by grant handle.
	tasks    map[string]*task
	nextCall uint64

	recMu   sync.Mutex
	records []ActionRecord
	recHead int

	// facts is the durable half of the action record, nil without an
	// executor; factsMu guards it, and the flusher lands what record
	// buffers, so a call never waits on the store.
	factsMu   sync.Mutex
	facts     *agentfacts.ActionStore
	flushCh   chan struct{}
	stopFlush chan struct{}
	flushDone chan struct{}
}

// NewService subscribes the service. The caller MUST invoke Close.
func NewService(bus *inprocbus.Inst, log zerolog.Logger, cfg Config) (s *Service, err error) {
	if bus == nil {
		err = eh.Errorf("agent: nil bus")
		return
	}
	if cfg.Registry == nil {
		cfg.Registry = app.DefaultRegistry
	}
	s = &Service{cfg: cfg, log: log.With().Str("app", string(ServiceAppId)).Logger(), tasks: make(map[string]*task)}
	if cfg.Exec != nil {
		s.facts = agentfacts.NewActionStore(cfg.Exec, nil, agentfacts.ActionStoreConfig{})
		s.flushCh, s.stopFlush, s.flushDone = make(chan struct{}, 1), make(chan struct{}), make(chan struct{})
		go s.flusher()
	}
	s.busClient = bus.NewClient(ServiceAppId, ServiceCaps())
	s.unsub, err = s.busClient.Subscribe(SubjectAll, s.handleRequest)
	if err != nil {
		if cerr := s.busClient.Close(); cerr != nil {
			s.log.Warn().Err(cerr).Msg("agent: closing the bus client after a failed subscribe")
		}
		err = eh.Errorf("agent: subscribe: %w", err)
		return nil, err
	}
	return
}

// Close unsubscribes and closes the bus client.
func (inst *Service) Close() {
	inst.closeOnce.Do(func() {
		if inst.unsub != nil {
			inst.unsub()
		}
		if err := inst.busClient.Close(); err != nil {
			inst.log.Warn().Err(err).Msg("agent: closing the bus client")
		}
		if inst.facts != nil {
			close(inst.stopFlush)
			<-inst.flushDone
			inst.factsMu.Lock()
			inst.facts.Close()
			inst.facts = nil
			inst.factsMu.Unlock()
		}
	})
}

func (inst *Service) handleRequest(msg *app.Msg) {
	if msg.Reply == "" {
		return
	}
	// An in-process handler runs on its requester's goroutine; a request
	// from the render goroutine would wait on the frame it blocks (ADR-0269
	// §SD3).
	if inst.cfg.Host != nil {
		if rg := inst.cfg.Host.OpsRenderGoroutine(); rg != 0 && rg == opwire.GoroutineId() {
			inst.reply(msg.Reply, wireAck{V: wireVersion, Reason: "requested from the render goroutine; call from a goroutine of your own"})
			return
		}
	}
	if msg.Subject != SubjectDescribe && msg.Subject != SubjectRequest && inst.cfg.Host == nil {
		inst.reply(msg.Reply, wireAck{V: wireVersion, Reason: "no window host"})
		return
	}
	switch msg.Subject {
	case SubjectDescribe:
		inst.reply(msg.Reply, inst.describe(msg))
	case SubjectRequest:
		inst.reply(msg.Reply, inst.grant(msg))
	case SubjectCall:
		inst.reply(msg.Reply, inst.call(msg))
	case SubjectStatus:
		inst.reply(msg.Reply, inst.status(msg))
	case SubjectCancel:
		inst.reply(msg.Reply, inst.cancel(msg))
	case SubjectRead:
		inst.reply(msg.Reply, inst.read(msg))
	case SubjectCapture:
		inst.reply(msg.Reply, inst.capture(msg))
	case SubjectList:
		inst.reply(msg.Reply, inst.list(msg))
	case SubjectDetach:
		inst.reply(msg.Reply, inst.detach(msg))
	case SubjectStop:
		inst.reply(msg.Reply, inst.stop(msg))
	default:
		inst.reply(msg.Reply, wireAck{V: wireVersion, Reason: "no such service"})
	}
}

func (inst *Service) reply(subject string, v any) {
	if err := buscodec.Reply(inst.busClient.Publish, subject, v); err != nil {
		inst.log.Warn().Err(err).Str("subject", subject).Msg("agent: reply failed")
	}
}

// matchesApp reports whether name names m by id or by subject alias.
func matchesApp(m app.Manifest, name string) (ok bool) {
	return name == string(m.Id) || name == m.Id.SubjectAlias()
}

func containsFold(haystack string, needle string) (ok bool) {
	return strings.Contains(strings.ToLower(haystack), needle)
}

// describe lists the operations agents may call, without schemas; naming
// one operation returns it with its schemas, which is how a coordinator
// loads a schema when its model asks for one (ADR-0269 §SD3).
func (inst *Service) describe(msg *app.Msg) (rep wireDescribeReply) {
	rep.V = wireVersion
	req, err := decode[wireDescribeRequest](msg.Payload)
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	if req.Operation != "" && req.App == "" {
		rep.Reason = "naming an operation needs the app"
		return
	}
	search := strings.ToLower(strings.TrimSpace(req.Search))
	for _, r := range inst.cfg.Registry.Registrations() {
		m := r.Manifest
		if m.Operations == nil {
			continue
		}
		if req.App != "" && !matchesApp(m, req.App) {
			continue
		}
		appHit := search == "" || containsFold(string(m.Id), search) || containsFold(m.Display, search) || containsFold(m.Summary, search)
		entry := wireApp{App: string(m.Id), Display: m.Display, Summary: m.Summary}
		for _, o := range m.Operations.Operations {
			if !o.Agents {
				continue
			}
			if req.Operation != "" && o.Name != req.Operation {
				continue
			}
			if !appHit && !containsFold(o.Name, search) && !containsFold(o.Summary, search) {
				continue
			}
			entry.Operations = append(entry.Operations, wireOperationOf(m, o, req.Operation != ""))
		}
		if len(entry.Operations) == 0 {
			continue
		}
		for _, res := range m.Operations.Resources {
			if usesResource(entry.Operations, res.Name) {
				entry.Resources = append(entry.Resources, wireResource{Name: res.Name, Summary: res.Summary})
			}
		}
		rep.Apps = append(rep.Apps, entry)
	}
	if req.Operation != "" && len(rep.Apps) == 0 {
		rep.Reason = "no operation agents may call by that name"
		return
	}
	rep.Ok = true
	return
}

func usesResource(ops []wireOperation, name string) (ok bool) {
	for _, o := range ops {
		if slices.Contains(o.Reads, name) || slices.Contains(o.Writes, name) {
			return true
		}
	}
	return
}

func wireOperationOf(m app.Manifest, o app.OperationSpec, withSchemas bool) (w wireOperation) {
	w = wireOperation{
		Name: o.Name, Version: o.Version, Summary: o.Summary, Class: o.Class.String(), Effect: o.Effect.String(),
		Reads: o.Reads, Writes: o.Writes, Refs: o.Refs, Follows: o.Follows, Untrusted: o.Untrusted, Gesture: o.Gesture,
	}
	if withSchemas {
		v := appops.ViewOf(m, o)
		w.ArgsSchema, w.ResultSchema = v.ArgsSchema, v.ResultSchema
	}
	return
}
