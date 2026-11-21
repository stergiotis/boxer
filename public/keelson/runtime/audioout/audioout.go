package audioout

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// SubjectOpen is the request subject an app publishes to for an audio output
// (ADR-0372 §SD1). The request is an [OpenRequest], the reply an
// [OpenReply].
const SubjectOpen = "runtime.audio.out.open"

// ServiceAppId is the synthetic AppId the service registers under.
const ServiceAppId app.AppIdT = "runtime.audio" // designlint:ignore=L12 (the audio service's bus identity, a runtime service in the dotted NATS-aligned form; renaming changes a wire id)

// openTimeout bounds [Open]'s wait for the service. A service in this
// process answers within the call; where nothing serves the subject the
// caller waits this long before it gets the null sink, which is why it is
// well under the bus's default.
const openTimeout = 750 * time.Millisecond

// maxPending bounds the grants not yet redeemed, and maxRecords the
// service's own record of opens; the oldest go first.
const (
	maxPending = 256
	maxRecords = 1024
)

// DeviceI is the host's audio device as the service sees it: something that
// opens one sink per granted stream.
type DeviceI interface {
	// Name names the kind of device in a [Status] and in the records.
	Name() (name string)
	// OpenSink opens a playback stream over src. label is the requesting
	// app's id, for a device that shows its streams by name. The sink does
	// not own src.
	OpenSink(src pcm.SourceI, label string) (s sink.SinkI, err error)
}

// LatencyDeviceI is a [DeviceI] that can hold more audio ahead of its output
// than it does by default, for a stream whose request names a latency.
type LatencyDeviceI interface {
	DeviceI
	// OpenSinkWithLatency is OpenSink for a stream that asks for at least
	// latency of audio held ahead; zero is the device's default.
	OpenSinkWithLatency(src pcm.SourceI, label string, latency time.Duration) (s sink.SinkI, err error)
}

// OpenRequest describes the stream an app asks an output for. It is what the
// service records; the source itself stays with the caller.
type OpenRequest struct {
	SampleRate uint32
	Channels   uint16
	Frames     int64
	// Latency is how much audio the app asks the device to hold ahead of
	// its output; zero is the device's default. See [Options].
	Latency time.Duration
}

// OpenReply is the service's answer: a token to redeem in process, or a
// refusal with its reason.
type OpenReply struct {
	Token   string
	Device  string
	Refused bool
	Reason  string
}

// OpenRecord is one request as the service saw it.
type OpenRecord struct {
	At      time.Time
	From    app.AppIdT
	Device  string
	Request OpenRequest
	// Opened is set once the token was redeemed and the device opened.
	Opened bool
	// Error is why the device did not open, when it did not.
	Error string
}

// Status says what [Open] returned.
type Status struct {
	// Device is the kind of device behind the sink; empty for the null sink.
	Device string
	// Reason is why no device plays the sink, phrased to follow "no output
	// device:"; empty when one does.
	Reason string
}

// Audible reports whether a device is behind the sink.
func (inst Status) Audible() (yes bool) { return inst.Reason == "" }

// grant is a token the service handed out and nobody redeemed yet.
type grant struct {
	svc     *Service
	from    app.AppIdT
	record  int // index into svc.records, offset by svc.dropped
	latency time.Duration
}

// pending holds the unredeemed grants of every service in the process, so
// [Open] finds the service behind a token without holding a reference to it.
var pending = struct {
	mu     sync.Mutex
	grants map[string]grant
	order  []string
}{grants: make(map[string]grant)}

// Service answers [SubjectOpen] and opens the granted streams on its device.
type Service struct {
	log       zerolog.Logger
	busClient *inprocbus.Client
	unsub     func()
	dev       DeviceI

	mu sync.Mutex
	// records are the opens, oldest first, bounded by maxRecords; dropped
	// counts the ones already discarded, so a grant's index stays valid.
	records []OpenRecord
	dropped int
}

// NewService subscribes the service on bus with dev as its device.
func NewService(bus *inprocbus.Inst, log zerolog.Logger, dev DeviceI) (s *Service, err error) {
	if bus == nil {
		return nil, eh.Errorf("audioout: nil bus")
	}
	if dev == nil {
		return nil, eh.Errorf("audioout: nil device")
	}
	s = &Service{
		log: log.With().Str("app", string(ServiceAppId)).Logger(),
		dev: dev,
	}
	s.busClient = bus.NewClient(ServiceAppId, []app.SubjectFilter{
		{Pattern: SubjectOpen, Direction: app.CapDirectionBoth, Reason: "the audio-output service serves runtime.audio.out.open"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "the audio-output service replies to inboxes"},
	})
	s.unsub, err = s.busClient.Subscribe(SubjectOpen, s.handleOpen)
	if err != nil {
		return nil, eh.Errorf("audioout: subscribe: %w", err)
	}
	return s, nil
}

// Close releases the subscription and the grants nobody redeemed. Sinks
// already opened are their owners' to close. Idempotent.
func (inst *Service) Close() {
	if inst.unsub != nil {
		inst.unsub()
		inst.unsub = nil
	}
	pending.mu.Lock()
	kept := pending.order[:0]
	for _, token := range pending.order {
		if pending.grants[token].svc == inst {
			delete(pending.grants, token)
			continue
		}
		kept = append(kept, token)
	}
	pending.order = kept
	pending.mu.Unlock()
}

// Device is the device the service opens streams on.
func (inst *Service) Device() (dev DeviceI) { return inst.dev }

// Records are the opens the service saw, oldest first.
func (inst *Service) Records() (records []OpenRecord) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return append([]OpenRecord(nil), inst.records...)
}

func (inst *Service) handleOpen(msg *app.Msg) {
	if msg.Sender == ServiceAppId || msg.Reply == "" {
		return
	}
	reply := OpenReply{Device: inst.dev.Name()}
	req, err := buscodec.Decode[OpenRequest](msg.Payload)
	if err != nil {
		reply.Refused, reply.Reason = true, "the request does not decode: "+err.Error()
	} else {
		reply.Token = inst.grant(msg.Sender, req)
	}
	if err = buscodec.Reply(inst.busClient.Publish, msg.Reply, reply); err != nil {
		inst.log.Error().Err(err).Str("from", string(msg.Sender)).Msg("audioout: reply publish failed")
	}
}

// grant records the request and mints the token that redeems it. The token
// carries nothing: it is a key, not a description.
func (inst *Service) grant(from app.AppIdT, req OpenRequest) (token string) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("audioout: the random source failed: " + err.Error())
	}
	token = hex.EncodeToString(b[:])

	inst.mu.Lock()
	inst.records = append(inst.records, OpenRecord{At: time.Now(), From: from, Device: inst.dev.Name(), Request: req})
	if over := len(inst.records) - maxRecords; over > 0 {
		inst.records = append([]OpenRecord(nil), inst.records[over:]...)
		inst.dropped += over
	}
	index := inst.dropped + len(inst.records) - 1
	inst.mu.Unlock()

	pending.mu.Lock()
	pending.grants[token] = grant{svc: inst, from: from, record: index, latency: req.Latency}
	pending.order = append(pending.order, token)
	if len(pending.order) > maxPending {
		delete(pending.grants, pending.order[0])
		pending.order = pending.order[1:]
	}
	pending.mu.Unlock()
	return token
}

// redeem opens the granted stream over src and forgets the token.
func redeem(token string, src pcm.SourceI) (s sink.SinkI, device string, err error) {
	pending.mu.Lock()
	g, ok := pending.grants[token]
	if ok {
		delete(pending.grants, token)
		for i, t := range pending.order {
			if t == token {
				pending.order = append(pending.order[:i], pending.order[i+1:]...)
				break
			}
		}
	}
	pending.mu.Unlock()
	if !ok {
		return nil, "", eh.Errorf("the grant is not one this process can redeem")
	}
	device = g.svc.dev.Name()
	if dev, ok := g.svc.dev.(LatencyDeviceI); ok && g.latency > 0 {
		s, err = dev.OpenSinkWithLatency(src, string(g.from), g.latency)
	} else {
		s, err = g.svc.dev.OpenSink(src, string(g.from))
	}

	g.svc.mu.Lock()
	if i := g.record - g.svc.dropped; i >= 0 && i < len(g.svc.records) {
		if err != nil {
			g.svc.records[i].Error = err.Error()
		} else {
			g.svc.records[i].Opened = true
		}
	}
	g.svc.mu.Unlock()
	return s, device, err
}

// Audible reports whether a device plays s at this moment, and why not when
// none does. It differs from a [Status], which says what [Open] returned: a
// device whose samples are played elsewhere — a viewer's page — may have
// nobody listening, or a browser that has not let it play yet, and says so
// here while the sink keeps a playhead moving.
func Audible(s sink.SinkI) (yes bool, reason string) {
	if a, ok := s.(interface {
		Audible() (yes bool, reason string)
	}); ok {
		return a.Audible()
	}
	if _, null := s.(*sink.Null); null || s == nil {
		return false, "no output device"
	}
	return true, ""
}

// Options are what an app may ask of its output beyond the source.
type Options struct {
	// Latency is how much audio the device is asked to hold ahead of its
	// output, for a source whose reads are slow or uneven: the output runs
	// dry less readily, and a seek or a pause is heard that much later. Zero
	// is the device's default. A device may hold more, and one that cannot
	// hold more than its default ignores the figure.
	Latency time.Duration
}

// Open is [OpenWith] with no options.
func Open(ctx context.Context, bus app.BusI, src pcm.SourceI) (s sink.SinkI, status Status) {
	return OpenWith(ctx, bus, src, Options{})
}

// OpenWith asks the host for an audio output over src on the app's bus client.
// It always returns a sink the caller owns and closes: the host's device
// where the request was granted and the device opened, and otherwise the
// device-less [sink.Null], with the reason in status. The sink does not own
// src.
//
// The request waits on the bus and the device may talk to a sound server, so
// it is called off the render path.
func OpenWith(ctx context.Context, bus app.BusI, src pcm.SourceI, opts Options) (s sink.SinkI, status Status) {
	null := func(reason string) (sink.SinkI, Status) {
		return sink.NewNull(src, nil), Status{Reason: reason}
	}
	if src == nil {
		return null("no source")
	}
	if bus == nil {
		return null("the app has no bus")
	}
	format := src.Format()
	payload, err := buscodec.Encode(OpenRequest{SampleRate: format.SampleRate, Channels: format.Channels, Frames: src.Frames(), Latency: max(opts.Latency, 0)})
	if err != nil {
		return null(err.Error())
	}
	raw, err := app.RequestWithin(ctx, bus, openTimeout, SubjectOpen, payload)
	if err != nil {
		// Refused by the bus (the manifest does not declare the subject) or
		// unanswered (this host runs no audio-output service).
		return null(err.Error())
	}
	reply, err := buscodec.Decode[OpenReply](raw)
	if err != nil {
		return null("the reply does not decode: " + err.Error())
	}
	if reply.Refused {
		return null(reply.Reason)
	}
	dev, device, err := redeem(reply.Token, src)
	if err != nil {
		return null(err.Error())
	}
	return dev, Status{Device: device}
}
