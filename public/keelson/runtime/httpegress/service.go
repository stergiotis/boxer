package httpegress

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Config is the service's configuration.
type Config struct {
	// Destinations is the registry to serve; nil is [Registered].
	Destinations []DestinationSpec
	// KeepCalls bounds the in-process call record; zero is
	// DefaultKeepCalls.
	KeepCalls int
}

// DefaultKeepCalls bounds the in-process call record. A screenful of tiles
// is dozens of calls, so the ring is larger than llm's.
const DefaultKeepCalls = 4096

// resolved is one registered destination after the host resolved it.
type resolved struct {
	spec     DestinationSpec
	dest     Destination
	prefixes []prefix
	local    bool
	client   *http.Client
	// err is why the destination cannot be served; requests to it are
	// refused with this reason.
	err error
}

// Service answers `net.http.fetch.<destination>` (ADR-0262 §SD3). It
// resolves the registry once, at construction; a destination that failed
// to resolve stays listed and is refused with the reason.
type Service struct {
	cfg       Config
	dests     map[string]*resolved
	order     []string
	busClient *inprocbus.Client
	unsub     func()
	closeOnce sync.Once
	log       zerolog.Logger

	mu sync.Mutex
	// calls is a ring of cfg.KeepCalls records; head is the oldest once
	// it is full.
	calls []CallRecord
	head  int
	next  uint64
}

// NewService resolves the destinations and subscribes. The caller MUST
// invoke Close.
func NewService(bus *inprocbus.Inst, log zerolog.Logger, cfg Config) (s *Service, err error) {
	if bus == nil {
		err = eh.Errorf("httpegress: nil bus")
		return
	}
	if cfg.KeepCalls <= 0 {
		cfg.KeepCalls = DefaultKeepCalls
	}
	specs := cfg.Destinations
	if specs == nil {
		specs = Registered()
	}
	s = &Service{cfg: cfg, dests: make(map[string]*resolved, len(specs)), log: log.With().Str("app", string(ServiceAppId)).Logger()}
	for _, spec := range specs {
		if _, dup := s.dests[spec.Name]; dup {
			err = eb.Build().Str("destination", spec.Name).Errorf("httpegress: destination given twice")
			return nil, err
		}
		s.dests[spec.Name] = s.resolve(spec)
		s.order = append(s.order, spec.Name)
	}
	s.busClient = bus.NewClient(ServiceAppId, ServiceCaps())
	s.unsub, err = s.busClient.Subscribe(SubjectAll, s.handleRequest)
	if err != nil {
		if cerr := s.busClient.Close(); cerr != nil {
			s.log.Warn().Err(cerr).Msg("httpegress: closing the bus client after a failed subscribe")
		}
		err = eh.Errorf("httpegress: subscribe: %w", err)
		return nil, err
	}
	return
}

func (inst *Service) resolve(spec DestinationSpec) (r *resolved) {
	r = &resolved{spec: spec}
	if !ValidDestinationName(spec.Name) {
		r.err = eh.Errorf("invalid destination name")
		return
	}
	if spec.Resolve == nil {
		r.err = eh.Errorf("no resolve function")
		return
	}
	d, err := spec.Resolve()
	if err != nil {
		r.err = err
		return
	}
	if len(d.Prefixes) == 0 {
		r.err = eh.Errorf("the destination names no url prefix")
		return
	}
	for _, raw := range d.Prefixes {
		p, perr := parsePrefix(raw)
		if perr != nil {
			r.err = perr
			return
		}
		r.prefixes = append(r.prefixes, p)
	}
	if d.Timeout <= 0 {
		d.Timeout = DefaultTimeout
	}
	if d.MaxBodyBytes <= 0 {
		d.MaxBodyBytes = DefaultMaxBodyBytes
	}
	r.dest = d
	r.local = localPrefixes(r.prefixes)
	// No redirect across the destination's edge: a redirect is a second
	// request, and it must lie under the same prefixes as the first.
	r.client = &http.Client{
		Transport: d.newTransport(inst.log.With().Str("destination", spec.Name).Logger()),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return eh.Errorf("too many redirects")
			}
			if _, merr := matchURL(r.prefixes, req.URL.String()); merr != nil {
				return eb.Build().Str("url", recordedURL(req.URL)).Errorf("redirect: %w", merr)
			}
			return nil
		},
	}
	return
}

// Close releases the subscription and the bus client. Safe to call more
// than once. A fetch already in flight — the handler runs on the
// requester's goroutine — finishes, and its reply meets the closed client
// rather than a nil one.
func (inst *Service) Close() {
	inst.closeOnce.Do(func() {
		inst.unsub()
		_ = inst.busClient.Close()
		for _, r := range inst.dests {
			if r.client != nil {
				r.client.CloseIdleConnections()
			}
		}
	})
}

func (inst *Service) handleRequest(msg *app.Msg) {
	if msg.Reply == "" {
		inst.log.Warn().Str("subject", msg.Subject).Msg("httpegress: request without reply, dropping")
		return
	}
	name := strings.TrimPrefix(msg.Subject, SubjectPrefix)
	rec := CallRecord{At: time.Now().UTC(), Sender: msg.Sender, SenderInstance: msg.SenderInstance, Destination: name}
	req, err := decode[wireRequest](msg.Payload)
	if err != nil {
		inst.refuse(msg, "malformed request: "+err.Error(), rec)
		return
	}
	rec.Purpose, rec.Sensitivity = req.Purpose, queryengine.SensitivityE(req.Sensitivity)
	rec.Method = req.Method
	if rec.Method == "" {
		rec.Method = http.MethodGet
	}
	r, ok := inst.dests[name]
	if !ok {
		inst.refuse(msg, "no destination "+name+" is registered on this host", rec)
		return
	}
	if r.err != nil {
		inst.refuse(msg, "destination "+name+" is unavailable: "+r.err.Error(), rec)
		return
	}
	if rec.Method != http.MethodGet && rec.Method != http.MethodHead {
		inst.refuse(msg, "method "+rec.Method+" is not offered; GET and HEAD are", rec)
		return
	}
	u, err := matchURL(r.prefixes, req.URL)
	if err != nil {
		inst.refuse(msg, err.Error(), rec)
		return
	}
	rec.URL = recordedURL(u)
	// The sensitivity wall (ADR-0262 §SD4, the ADR-0145 rule): a request
	// composed from sealed data goes to a loopback destination and
	// nowhere else.
	if rec.Sensitivity == queryengine.SensitivityConfined && !r.local {
		inst.refuse(msg, "the request derives from sealed data that must not leave this box, and destination "+name+" is not loopback", rec)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.dest.Timeout)
	defer cancel()
	if req.DeadlineUnixNanos > 0 {
		if d := time.Unix(0, req.DeadlineUnixNanos); d.Before(time.Now().Add(r.dest.Timeout)) {
			var c2 context.CancelFunc
			ctx, c2 = context.WithDeadline(ctx, d)
			defer c2()
		}
	}
	started := time.Now()
	rep := inst.exchange(ctx, r, rec.Method, u.String())
	rec.Elapsed = time.Since(started)
	rep.ElapsedNs = int64(rec.Elapsed)
	rec.Status, rec.Bytes = int(rep.Status), len(rep.Body)
	if !rep.Ok {
		rec.Error = rep.Reason
	}
	inst.record(rec)
	lg := inst.log.Debug()
	if !rep.Ok {
		lg = inst.log.Warn()
	}
	lg.Str("sender", string(msg.Sender)).Str("destination", name).Str("url", rec.URL).Bool("ok", rep.Ok).
		Int32("status", rep.Status).Int("bytes", rec.Bytes).Str("reason", rep.Reason).Dur("elapsed", rec.Elapsed).Msg("httpegress: fetch")
	inst.reply(msg.Reply, rep)
}

// exchange runs one request under the destination's policy.
func (inst *Service) exchange(ctx context.Context, r *resolved, method string, target string) (rep wireReply) {
	hreq, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return wireReply{ErrorKind: errKindTransport, Reason: err.Error()}
	}
	if r.dest.UserAgent != "" {
		hreq.Header.Set("User-Agent", r.dest.UserAgent)
	}
	resp, err := r.client.Do(hreq)
	if err != nil {
		kind := errKindTransport
		if errors.Is(err, context.DeadlineExceeded) {
			kind = errKindTimeout
		}
		return wireReply{ErrorKind: kind, Reason: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, r.dest.MaxBodyBytes+1))
	if err != nil {
		kind := errKindTransport
		if errors.Is(err, context.DeadlineExceeded) {
			kind = errKindTimeout
		}
		return wireReply{ErrorKind: kind, Reason: "body unreadable: " + err.Error()}
	}
	if int64(len(body)) > r.dest.MaxBodyBytes {
		return wireReply{ErrorKind: errKindTooLarge, Reason: "the body exceeds the destination's cap of " + strconv.FormatInt(r.dest.MaxBodyBytes, 10) + " bytes", Status: int32(resp.StatusCode)}
	}
	ct := resp.Header.Get("Content-Type")
	if mt, _, perr := mime.ParseMediaType(ct); perr == nil {
		ct = mt
	}
	rep = wireReply{Ok: true, Status: int32(resp.StatusCode), ContentType: ct, Body: body}
	return
}

// recordedURL is a URL as the record and the log keep it: scheme, host
// and path. The query is dropped, not redacted — a tile server's API key
// rides there, and keelson('http_calls') is readable by any app granted
// it.
func recordedURL(u *url.URL) (s string) {
	s = u.Scheme + "://" + u.Host + u.EscapedPath()
	if u.RawQuery != "" {
		s += "?…"
	}
	return
}

// refuse answers with the reason and records the refusal: a refused call
// is a call the table should show.
func (inst *Service) refuse(msg *app.Msg, reason string, rec CallRecord) {
	rec.Refused, rec.Error = true, reason
	inst.record(rec)
	inst.log.Warn().Str("sender", string(msg.Sender)).Str("destination", rec.Destination).Str("purpose", rec.Purpose).Str("reason", reason).Msg("httpegress: refused")
	inst.reply(msg.Reply, wireReply{ErrorKind: errKindRefused, Reason: reason})
}

func (inst *Service) reply(inbox string, rep wireReply) {
	rep.V = wireVersion
	payload, err := encode(rep)
	if err != nil {
		inst.log.Error().Err(err).Msg("httpegress: encode reply")
		return
	}
	if err = inst.busClient.Publish(inbox, payload); err != nil {
		inst.log.Warn().Err(err).Str("inbox", inbox).Msg("httpegress: publish reply")
	}
}
