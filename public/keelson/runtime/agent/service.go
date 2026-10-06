package agent

import (
	"context"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/capture"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/observability/eh"
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
	// ModelLocal reports whether the coordinators' model endpoint is local
	// (ADR-0254 §SD3); confined content reaches only a local one. nil
	// treats it as remote.
	ModelLocal func() (local bool)
	// Coordinators are the app ids or subject aliases the person registered
	// as coordinators (CoordinatorsEnv): the only apps whose requests reach
	// the person.
	Coordinators []string
	// Trail is the host's audit trail (ADR-0277): with it the action record
	// and the grant events are also kept on boxer.facts (§SD9). Nil, or a
	// recorder without a backend, keeps only the in-process record.
	Trail *trail.Recorder
	// Deadline is how long a task runs, and how much more time an approved
	// widening of a late task gives; zero is DefaultDeadline (DeadlineEnv).
	Deadline time.Duration
	// Pace is the least time between two visible changes of a paced task
	// (ADR-0280 §SD6); zero is DefaultPace (PaceEnv).
	Pace time.Duration
	// CallsMin and CallsMax bound a new task's call budget, the range of
	// the dialog's slider; zero is DefaultCallsMin and DefaultCallsMax
	// (CallsMinEnv, CallsMaxEnv).
	CallsMin int
	CallsMax int
	// ActionsLog, when set, receives every action record as one JSON line,
	// for scoring a run after the host exits (ActionsFileEnv); the headless
	// host sets it.
	ActionsLog io.Writer
}

// Service answers `runtime.agent.*` (ADR-0269 §SD3).
type Service struct {
	cfg Config
	// captures is the capture service, the PEP every capture passes
	// through (ADR-0281).
	captures  *capture.Service
	busClient *inprocbus.Client
	unsub     func()
	closeOnce sync.Once
	log       zerolog.Logger

	mu sync.Mutex
	// tasks are keyed by grant handle.
	tasks    map[string]*task
	nextCall uint64
	// requests are the decisions the person owes, keyed by request key,
	// in arrival order.
	requests     map[string]*request
	requestOrder []string
	// taints holds the conversations that read untrusted content, keyed
	// by coordinator, window and conversation.
	taints      map[string]bool
	unsubClosed func()
	// leftBy names, per window a task launched and then ended, the task
	// that left it to the person.
	leftBy map[uint64]string

	// helpCache holds the help books served, by app.
	helpMu    sync.Mutex
	helpCache map[app.AppIdT]help.BookI

	recMu   sync.Mutex
	records []ActionRecord
	recHead int

	// events carries what hear queues to the publisher.
	events     chan wireEvent
	eventsDone chan struct{}
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
	s = &Service{cfg: cfg, log: log.With().Str("app", string(ServiceAppId)).Logger(), tasks: make(map[string]*task),
		requests: make(map[string]*request), taints: make(map[string]bool), leftBy: make(map[uint64]string),
		helpCache: make(map[app.AppIdT]help.BookI)}
	if cfg.Host != nil {
		s.captures = capture.NewService(capture.GrantPolicy{}, capture.NewRegistry(), cfg.Host)
	}
	s.events, s.eventsDone = make(chan wireEvent, eventQueueLen), make(chan struct{})
	s.busClient = bus.NewClient(ServiceAppId, ServiceCaps())
	go s.publishEvents()
	s.unsub, err = s.busClient.Subscribe(SubjectAll, s.handleRequest)
	if err != nil {
		if cerr := s.busClient.Close(); cerr != nil {
			s.log.Warn().Err(cerr).Msg("agent: closing the bus client after a failed subscribe")
		}
		err = eh.Errorf("agent: subscribe: %w", err)
		return nil, err
	}
	s.unsubClosed, err = s.busClient.Subscribe(app.SubjectInstanceClosed, s.instanceClosed)
	if err != nil {
		s.unsub()
		_ = s.busClient.Close()
		err = eh.Errorf("agent: subscribe to closing instances: %w", err)
		return nil, err
	}
	return
}

// Listener is what the window host calls with every change a window's
// engine logs (windowhost.SetOpsListener).
func (inst *Service) Listener() (fn func(key uint64, e opengine.LogEntry)) { return inst.hear }

// Close unsubscribes and closes the bus client.
func (inst *Service) Close() {
	inst.closeOnce.Do(func() {
		if inst.unsub != nil {
			inst.unsub()
		}
		if inst.unsubClosed != nil {
			inst.unsubClosed()
		}
		close(inst.events)
		<-inst.eventsDone
		if err := inst.busClient.Close(); err != nil {
			inst.log.Warn().Err(err).Msg("agent: closing the bus client")
		}
		if err := inst.cfg.Trail.Flush(context.Background()); err != nil {
			inst.log.Warn().Err(err).Msg("agent: flush the action record at close")
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
	if msg.Subject != SubjectDescribe && msg.Subject != SubjectRequest && msg.Subject != SubjectDisclose && inst.cfg.Host == nil {
		inst.reply(msg.Reply, wireAck{V: wireVersion, Reason: "no window host"})
		return
	}
	switch msg.Subject {
	case SubjectDescribe:
		rep, req := inst.describe(msg)
		inst.reply(msg.Reply, rep)
		inst.recordAsked(msg, nil, "describe", req.Key, req.Conversation, req.wireCause, req.App, rep.Ok, rep.Reason)
	case SubjectHelp:
		rep, req := inst.help(msg)
		inst.reply(msg.Reply, rep)
		inst.recordAsked(msg, nil, "help", req.Key, req.Conversation, req.wireCause, req.App, rep.Ok, rep.Reason)
	case SubjectRequest:
		inst.reply(msg.Reply, inst.requestGrant(msg))
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
	case SubjectTurn:
		inst.reply(msg.Reply, inst.turn(msg))
	case SubjectLaunch:
		inst.reply(msg.Reply, inst.launch(msg))
	case SubjectAuthority:
		inst.reply(msg.Reply, inst.authority(msg))
	case SubjectDisclose:
		inst.reply(msg.Reply, inst.disclose(msg))
	case SubjectArrange, SubjectRaise, SubjectPlace:
		inst.reply(msg.Reply, inst.windowAct(msg))
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
func (inst *Service) describe(msg *app.Msg) (rep wireDescribeReply, req wireDescribeRequest) {
	rep.V = wireVersion
	var err error
	req, err = decode[wireDescribeRequest](msg.Payload)
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
		// An app the launch limit refuses (ADR-0272) has no window to
		// operate and none can be opened, so describing it would mislead.
		if !inst.cfg.Registry.Launchable(m.Id) {
			continue
		}
		if req.App != "" && !matchesApp(m, req.App) {
			continue
		}
		appHit := search == "" || containsFold(string(m.Id), search) || containsFold(m.Display, search) || containsFold(m.Summary, search) ||
			slices.ContainsFunc(m.Keywords, func(k string) bool { return containsFold(k, search) })
		entry := wireApp{App: string(m.Id), Display: m.Display, Summary: m.Summary, Help: m.Help != nil}
		var ops []app.OperationSpec
		if m.Operations != nil {
			ops = m.Operations.Operations
		}
		for _, o := range ops {
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
			// An app with no operation for agents can still be opened: it is
			// listed, with none, when its own fields match and no operation
			// was asked for — so a model learns the id it opens it by. An
			// applet only when named or searched for: a build mints dozens,
			// and the whole list is the model's first call.
			if req.Operation != "" || !appHit || m.Surface != app.SurfaceWindowed {
				continue
			}
			if m.Kind == app.KindApplet && req.App == "" && search == "" {
				continue
			}
			entry.Operations = []wireOperation{}
			rep.Apps = append(rep.Apps, entry)
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
