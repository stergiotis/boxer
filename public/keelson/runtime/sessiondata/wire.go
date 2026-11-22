package sessiondata

import (
	"context"
	"errors"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/instanceclosed"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// The capability's subjects, request/reply. The sender's window is the
// envelope's; an agent's call is named by task, epoch and call on the
// request and attested with the dispatcher.
const (
	// SubjectPrefix precedes every subject of the service.
	SubjectPrefix = "runtime.sessiondata."
	// SubjectOpen opens the person's data session for the sending window.
	SubjectOpen = SubjectPrefix + "open"
	// SubjectAttach attaches the sending window to a data session by ticket.
	SubjectAttach = SubjectPrefix + "attach"
	// SubjectClose ends a person's data session the window holds.
	SubjectClose = SubjectPrefix + "close"
	// SubjectDefine defines or redefines a data set.
	SubjectDefine = SubjectPrefix + "define"
	// SubjectDrop drops a data set.
	SubjectDrop = SubjectPrefix + "drop"
	// SubjectPrepare readies a statement to run in a data session.
	SubjectPrepare = SubjectPrefix + "prepare"
	// SubjectRelease ends a prepared statement's hold on its server session.
	SubjectRelease = SubjectPrefix + "release"
	// SubjectList lists a data session's data sets.
	SubjectList = SubjectPrefix + "list"
)

// wireVersion is the version of the request and reply below.
const wireVersion = 1

// RequestTimeout bounds how long a client waits for a define, a drop or a
// prepare, which make tables on the server and may wait for the server
// session.
const RequestTimeout = 10 * time.Minute

// wireRequest is every request's wire form; a verb leaves what it does not
// use empty. The password travels on the in-process bus only and is never
// logged or recorded.
type wireRequest struct {
	V              uint8    `json:"v"`
	Ticket         string   `json:"ticket,omitempty"`
	Endpoint       string   `json:"endpoint,omitempty"`
	User           string   `json:"user,omitempty"`
	Password       string   `json:"password,omitempty"`
	ServesConfined bool     `json:"servesConfined,omitempty"`
	Name           string   `json:"name,omitempty"`
	SQL            string   `json:"sql,omitempty"`
	OrderBy        []string `json:"orderBy,omitempty"`
	Cascade        bool     `json:"cascade,omitempty"`
	Lease          string   `json:"lease,omitempty"`
	OboTask        string   `json:"oboTask,omitempty"`
	OboEpoch       uint64   `json:"oboEpoch,omitempty"`
	OboCall        string   `json:"oboCall,omitempty"`
}

// wireReply is every reply's wire form.
type wireReply struct {
	V           uint8         `json:"v"`
	Ok          bool          `json:"ok"`
	Reason      string        `json:"reason,omitempty"`
	Destination string        `json:"destination,omitempty"`
	Ticket      string        `json:"ticket,omitempty"`
	Dataset     *DatasetInfo  `json:"dataset,omitempty"`
	Datasets    []DatasetInfo `json:"datasets,omitempty"`
	Names       []string      `json:"names,omitempty"`
	Binding     *Binding      `json:"binding,omitempty"`
}

func (inst wireRequest) caller(msg *app.Msg) (c Caller) {
	c.By = Identity{App: msg.Sender, Instance: msg.SenderInstance}
	if inst.OboTask != "" {
		c.OnBehalfOf = &app.OnBehalfOf{Task: inst.OboTask, Epoch: inst.OboEpoch, Call: inst.OboCall}
	}
	return
}

func (inst wireRequest) endpoint() (e Endpoint) {
	return Endpoint{URL: inst.Endpoint, User: inst.User, Password: inst.Password, ServesConfined: inst.ServesConfined}
}

// subscribe binds the subjects and hears windows close.
func (inst *Service) subscribe(bus *inprocbus.Inst) (err error) {
	subjects := []string{SubjectOpen, SubjectAttach, SubjectClose, SubjectDefine, SubjectDrop, SubjectPrepare, SubjectRelease, SubjectList}
	caps := make([]app.SubjectFilter, 0, len(subjects)+2)
	for _, s := range subjects {
		caps = append(caps, app.SubjectFilter{Pattern: s, Direction: app.CapDirectionSub, Reason: "session data: serve " + s})
	}
	caps = append(caps,
		app.SubjectFilter{Pattern: app.SubjectInstanceClosed, Direction: app.CapDirectionSub, Reason: "session data: end a person's data session with its last window"},
		app.SubjectFilter{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "session data: reply to caller inboxes"},
	)
	client := bus.NewClient(ServiceAppId, caps)
	inst.busClient = client
	for _, s := range subjects {
		unsub, subErr := client.Subscribe(s, inst.handleRequest)
		if subErr != nil {
			for _, u := range inst.unsubs {
				u()
			}
			inst.unsubs = nil
			return eb.Build().Str("subject", s).Errorf("subscribe: %w", subErr)
		}
		inst.unsubs = append(inst.unsubs, unsub)
	}
	unsub, err := client.Subscribe(app.SubjectInstanceClosed, inst.handleInstanceClosed)
	if err != nil {
		return eh.Errorf("subscribe to closed windows: %w", err)
	}
	inst.unsubs = append(inst.unsubs, unsub)
	return nil
}

// decodeInstanceClosed is the window an instance-closed event names.
func decodeInstanceClosed(payload []byte) (who Identity, err error) {
	ev, err := buscodec.Decode[instanceclosed.InstanceClosed](payload)
	if err != nil {
		return
	}
	return Identity{App: app.AppIdT(ev.AppId), Instance: ev.InstanceKey}, nil
}

func (inst *Service) handleInstanceClosed(msg *app.Msg) {
	ev, err := decodeInstanceClosed(msg.Payload)
	if err != nil {
		inst.log.Warn().Err(err).Msg("sessiondata: decode instance-closed")
		return
	}
	inst.instanceClosed(context.WithoutCancel(msg.Context()), ev)
}

func (inst *Service) handleRequest(msg *app.Msg) {
	if msg.Reply == "" {
		return
	}
	req, err := buscodec.Decode[wireRequest](msg.Payload)
	if err != nil {
		inst.reply(msg.Reply, refusal(eh.Errorf("decode: %w", err)))
		return
	}
	ctx := msg.Context()
	c := req.caller(msg)
	rep := wireReply{V: wireVersion, Ok: true}
	personal := msg.Subject == SubjectOpen || msg.Subject == SubjectAttach || msg.Subject == SubjectClose
	if personal && c.OnBehalfOf != nil {
		inst.reply(msg.Reply, refusal(eh.Errorf("an agent's work uses its task's data session; opening, attaching and closing one is the person's")))
		return
	}
	switch msg.Subject {
	case SubjectOpen:
		rep.Ticket, err = inst.Open(ctx, c.By)
	case SubjectAttach:
		err = inst.Attach(ctx, c.By, req.Ticket)
	case SubjectClose:
		err = inst.CloseSession(ctx, c.By, req.Ticket)
	case SubjectDefine:
		var res DefineResult
		res, err = inst.Define(ctx, DefineInput{Caller: c, Ticket: req.Ticket, Endpoint: req.endpoint(), Name: req.Name, SQL: req.SQL, OrderBy: req.OrderBy})
		rep.Dataset, rep.Names, rep.Ticket = &res.Dataset, res.Replayed, res.Ticket
	case SubjectDrop:
		rep.Names, err = inst.Drop(ctx, DropInput{Caller: c, Ticket: req.Ticket, Name: req.Name, Cascade: req.Cascade})
	case SubjectPrepare:
		var b Binding
		b, err = inst.Prepare(ctx, PrepareInput{Caller: c, Ticket: req.Ticket, Endpoint: req.endpoint(), SQL: req.SQL})
		if err == nil && b.Lease != "" && ctx.Err() != nil {
			// Nobody waits for the binding any more: free the session now
			// rather than when the lease lapses.
			_ = inst.Release(c.By, b.Lease)
			return
		}
		rep.Binding = &b
	case SubjectRelease:
		err = inst.Release(c.By, req.Lease)
	case SubjectList:
		rep.Datasets, rep.Ticket, err = inst.List(ctx, c, req.Ticket)
	default:
		err = eb.Build().Str("subject", msg.Subject).Errorf("unknown subject")
	}
	if err != nil {
		rep = refusal(err)
	}
	inst.reply(msg.Reply, rep)
}

// refusal is the reply to a refused request: the reason, and the grant
// destination it needed.
func refusal(err error) (rep wireReply) {
	rep = wireReply{V: wireVersion, Reason: err.Error()}
	var ge *GrantError
	if errors.As(err, &ge) {
		rep.Destination = ge.Destination
	}
	return
}

func (inst *Service) reply(subject string, rep wireReply) {
	payload, err := buscodec.Encode(rep)
	if err != nil {
		inst.log.Warn().Err(err).Msg("sessiondata: encode reply")
		return
	}
	if pubErr := inst.busClient.Publish(subject, payload); pubErr != nil {
		inst.log.Warn().Err(pubErr).Msg("sessiondata: publish reply")
	}
}
