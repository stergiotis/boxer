// Package linkhost is the host's side of `boxer:` links (ADR-0409): it
// keeps the records handles name, in memory, and follows and describes
// links for every surface that renders them. A link minted
// by a task runs under that task's grant, checked again at the click by the
// agent dispatcher; any other link — inline, or minted under a grant that
// no longer authorises it — is followed with the person as the trigger and
// its author's label on what the call writes, under rules set by the
// operation's effect (§SD5).
package linkhost

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/applink"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opjson"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// AppId is the service's identity on the bus.
const AppId app.AppIdT = "runtime.link"

// MaxRecords bounds the records kept; the oldest is evicted first.
const MaxRecords = 4096

// HostI is what the service needs of the window host.
type HostI interface {
	OpsOpen(appId app.AppIdT, kind string, cfg []byte) (key uint64, err error)
	OpsRaise(key uint64) (err error)
	OpsInstances() (out []opwire.InstanceInfo)
	OpsRevisions(key uint64) (revs map[string]uint64, ok bool)
	OpsStatus(key uint64, callId string) (out opwire.Outcome, ok bool)
	OpsSubmit(ctx context.Context, key uint64, op string, req opwire.CallRequest) (out opwire.Outcome, ok bool)
	ResolveLaunchKind(kind string) (id app.AppIdT, err error)
	WindowInfos() (out []windowhost.WindowInfo)
}

var _ HostI = (*windowhost.Inst)(nil)

// FollowerI runs a minted link's call under its task's grant: the agent
// dispatcher.
type FollowerI interface {
	FollowLink(ctx context.Context, c agent.LinkClick) (out agent.LinkFollowed)
	LinkLive(taskId string, epoch uint64) (live bool)
}

var _ FollowerI = (*agent.Service)(nil)

// RecorderI writes a follow's record on the trail (ADR-0409 §SD9).
type RecorderI interface {
	Followed(ctx context.Context, f Followed)
}

// Config is what New needs.
type Config struct {
	Bus      *inprocbus.Inst
	Host     HostI
	Registry *app.Registry
	// Recorder writes each follow's record; nil records nothing.
	Recorder RecorderI
	Log      zerolog.Logger
	// Key fixes the key handles are minted under; the zero key draws one.
	// Only a test fixes it, so a scene sees the same handle on every run.
	Key applink.Key
}

// Service keeps the records and serves the follow and describe subjects.
type Service struct {
	cfg       Config
	key       applink.Key
	busClient *inprocbus.Client
	unsubs    []func()
	log       zerolog.Logger

	mu       sync.Mutex
	records  map[applink.Handle]applink.Record
	order    []applink.Handle
	follower FollowerI
	clicks   uint64
}

// New starts the service and subscribes its subjects.
func New(cfg Config) (svc *Service, err error) {
	if cfg.Bus == nil || cfg.Host == nil {
		err = eh.Errorf("linkhost: a bus and a host are required")
		return
	}
	if cfg.Registry == nil {
		cfg.Registry = app.DefaultRegistry
	}
	svc = &Service{cfg: cfg, key: cfg.Key, log: cfg.Log.With().Str("app", string(AppId)).Logger(),
		records: make(map[applink.Handle]applink.Record)}
	if svc.key == (applink.Key{}) {
		if svc.key, err = applink.NewKey(); err != nil {
			err = eh.Errorf("linkhost: draw the handle key: %w", err)
			return
		}
	}
	svc.busClient = cfg.Bus.NewClient(AppId, []app.SubjectFilter{
		{Pattern: applink.FollowSubject, Direction: app.CapDirectionSub, Reason: "the host follows links for the surfaces that render them"},
		{Pattern: applink.DescribeSubject, Direction: app.CapDirectionSub, Reason: "the host describes links for their hovers"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "the link service replies to inboxes"},
		clipboardbroker.WriteCap("Window ▸ Copy link to this window puts the window's link on the clipboard"),
	})
	for subject, h := range map[string]app.MsgHandlerFunc{applink.FollowSubject: svc.handleFollow, applink.DescribeSubject: svc.handleDescribe} {
		var unsub func()
		if unsub, err = svc.busClient.Subscribe(subject, h); err != nil {
			svc.Close()
			err = eb.Build().Str("subject", subject).Errorf("linkhost: subscribe: %w", err)
			return
		}
		svc.unsubs = append(svc.unsubs, unsub)
	}
	return
}

// Close unsubscribes the service.
func (inst *Service) Close() {
	for _, u := range inst.unsubs {
		u()
	}
	inst.unsubs = nil
}

// SetFollower installs the dispatcher that runs minted links; without
// one, every minted link is followed as an authorless one.
func (inst *Service) SetFollower(f FollowerI) {
	inst.mu.Lock()
	inst.follower = f
	inst.mu.Unlock()
}

// MintTask keeps a record a task's grant checked and returns its link
// (agent.LinkMinterI).
func (inst *Service) MintTask(rec applink.Record) (link string, err error) {
	if rec.Task == "" {
		err = eh.Errorf("linkhost: a task's record names its task")
		return
	}
	return inst.mint(rec)
}

// MintWindow keeps the person's link to one open window — no call, the
// person as author — and returns it: *Copy link to this window*.
func (inst *Service) MintWindow(key uint64) (link string, err error) {
	info, open := inst.instance(key)
	if !open {
		err = eb.Build().Uint64("window", key).Errorf("linkhost: no open window by that key")
		return
	}
	return inst.mint(applink.Record{V: applink.RecordVersion, Verb: applink.VerbRaise, App: info.Alias, Window: key, AuthorLevel: applink.LevelPerson})
}

// CopyWindowLink mints the person's link to a window and puts it on the
// clipboard, through the broker, as the person's text. It blocks on the
// broker's answer: call it off the render goroutine.
func (inst *Service) CopyWindowLink(ctx context.Context, key uint64) (err error) {
	link, err := inst.MintWindow(key)
	if err != nil {
		return
	}
	_, err = clipboardbroker.Write(ctx, inst.busClient, clipboardbroker.Payload{Text: link, Source: "window menu: copy link to this window"})
	return
}

func (inst *Service) mint(rec applink.Record) (link string, err error) {
	rec.V = applink.RecordVersion
	b, err := rec.Canonical()
	if err != nil {
		return
	}
	h := applink.Mint(inst.key, b)
	inst.mu.Lock()
	if _, seen := inst.records[h]; !seen {
		inst.records[h] = rec
		inst.order = append(inst.order, h)
		for len(inst.order) > MaxRecords {
			delete(inst.records, inst.order[0])
			inst.order = inst.order[1:]
		}
	}
	inst.mu.Unlock()
	link = applink.Format(applink.Envelope{Form: applink.FormHandle, Handle: h})
	return
}

// Record is the record a handle names, while the service keeps it.
func (inst *Service) Record(h applink.Handle) (rec applink.Record, ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	rec, ok = inst.records[h]
	return
}

// resolved is a link read as far as the host reads it.
type resolved struct {
	rec    applink.Record
	minted bool
	appId  app.AppIdT
	m      app.Manifest
	spec   app.OperationSpec
	// author is the label the call carries when it is followed authorless.
	author applink.Author
}

// expiredReason is what a handle whose record is gone says.
const expiredReason = "the link has expired: its record is gone — evicted, or from an earlier session"

// resolve reads a link: the envelope, the record a handle names or the
// codec's reading of an inline tail, and the operation it calls.
func (inst *Service) resolve(link string, author applink.Author) (r resolved, err error) {
	env, err := applink.Parse(link)
	if err != nil {
		return
	}
	r.author = author
	if env.Form == applink.FormHandle {
		var ok bool
		if r.rec, ok = inst.Record(env.Handle); !ok {
			err = eh.Errorf(expiredReason)
			return
		}
		r.minted = r.rec.FromTask()
		r.author = applink.Author{Level: r.rec.AuthorLevel, Tier: r.rec.AuthorTier}
	} else {
		r.rec = applink.Record{V: applink.RecordVersion, Verb: env.Verb, App: env.App, Kind: env.Kind,
			Operation: env.Operation, Version: env.Version, AuthorLevel: author.Level, AuthorTier: author.Tier}
	}
	if r.rec.Kind != "" {
		if r.appId, err = inst.cfg.Host.ResolveLaunchKind(r.rec.Kind); err != nil {
			return
		}
		if env.Form != applink.FormHandle && env.Tail != "" {
			codec, ok := applink.KindCodec(r.rec.Kind)
			if !ok {
				err = eb.Build().Str("kind", r.rec.Kind).Errorf("linkhost: the kind spells no link")
				return
			}
			if r.rec.Config, err = parseTail(codec, env.Tail); err != nil {
				return
			}
		}
		r.m, _ = inst.cfg.Registry.LookupManifest(r.appId)
		r.rec.App = r.m.SubjectAlias()
		return
	}
	var ok bool
	if r.m, ok = inst.manifestByAlias(r.rec.App); !ok {
		err = eb.Build().Str("app", r.rec.App).Errorf("linkhost: no app this host opens has that alias")
		return
	}
	r.appId = r.m.Id
	if r.rec.Operation == "" {
		return
	}
	ok = false
	if r.m.Operations != nil {
		r.spec, ok = r.m.Operations.Lookup(r.rec.Operation)
	}
	switch {
	case !ok:
		err = eb.Build().Str("operation", r.rec.Operation).Errorf("linkhost: the app has no such operation")
	case r.spec.Version != r.rec.Version:
		err = eb.Build().Str("operation", r.rec.Operation).Errorf("linkhost: the link names version %d; the app serves version %d", r.rec.Version, r.spec.Version) //boxer:lint disable=CS013 reason="the refusal is the reason a person reads on the surface that followed the link: the versions belong in its text"
	case env.Form != applink.FormHandle && r.spec.Link == nil:
		err = eb.Build().Str("operation", r.rec.Operation).Errorf("linkhost: the operation takes no links")
	case env.Form != applink.FormHandle:
		r.rec.Args, err = parseTail(r.spec.Link, env.Tail)
	}
	return
}

// parseTail runs a codec, which reads text boxer did not write: a panic is
// a refusal.
func parseTail(codec app.LinkCodecI, tail string) (b []byte, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = eb.Build().Str("panic", fmt.Sprint(p)).Errorf("linkhost: the codec failed on the link")
		}
	}()
	b, err = codec.ParseLink(tail)
	return
}

func (inst *Service) manifestByAlias(alias string) (m app.Manifest, ok bool) {
	for _, cand := range inst.cfg.Registry.LaunchableManifests() {
		if cand.SubjectAlias() == alias {
			return cand, true
		}
	}
	return
}

func (inst *Service) instance(key uint64) (info opwire.InstanceInfo, open bool) {
	for _, i := range inst.cfg.Host.OpsInstances() {
		if i.Key == key {
			return i, true
		}
	}
	return
}

// topmost lists the open windows of an app, front first: what raise looks
// at.
func (inst *Service) topmost(id app.AppIdT) (keys []uint64) {
	open := make(map[uint64]bool)
	for _, i := range inst.cfg.Host.OpsInstances() {
		open[i.Key] = true
	}
	type cand struct {
		key   uint64
		stack uint32
	}
	var cs []cand
	for _, w := range inst.cfg.Host.WindowInfos() {
		if w.AppId == id && open[uint64(w.Key)] {
			cs = append(cs, cand{key: uint64(w.Key), stack: w.Geom.Stack})
		}
	}
	slices.SortFunc(cs, func(a, b cand) int {
		if a.stack != b.stack {
			return int(b.stack) - int(a.stack)
		}
		if a.key > b.key {
			return -1
		}
		return 1
	})
	for _, c := range cs {
		keys = append(keys, c.key)
	}
	return
}

// argsText is a call's arguments as JSON, for a hover.
func argsText(spec app.OperationSpec, args []byte) (s string) {
	if spec.Args == nil || len(args) == 0 {
		return ""
	}
	v := reflect.New(spec.Args)
	if err := buscodec.Default().Decode(args, v.Interface()); err != nil {
		return "(unreadable arguments)"
	}
	b, err := opjson.Encode(v.Elem().Interface())
	if err != nil {
		return "(unreadable arguments)"
	}
	return string(b)
}

func levelName(level uint8) (s string) {
	switch level {
	case applink.LevelPerson:
		return "the person"
	case applink.LevelSystem:
		return "the system"
	case applink.LevelExternal:
		return "external text"
	case applink.LevelOwnModel, applink.LevelOtherModel:
		return "a model"
	}
	return "level " + strconv.Itoa(int(level))
}

// loadSettle bounds how long a follow waits for a window it opened.
const loadSettle = 3 * time.Second

// applySettle bounds how long a follow waits for its call to apply.
const applySettle = 3 * time.Second

// waitLoaded waits until a window has mounted and serves its catalog, as
// the dispatcher's launch does.
func (inst *Service) waitLoaded(key uint64) (ok bool) {
	deadline := time.Now().Add(loadSettle)
	for {
		info, found := inst.instance(key)
		if found && (info.Load == opwire.LoadReady || info.Load == opwire.LoadUnspecified) {
			if !info.Ops {
				return true
			}
			if revs, rok := inst.cfg.Host.OpsRevisions(key); rok && revs != nil {
				return true
			}
		}
		if found && info.Load == opwire.LoadFailed {
			return false
		}
		if !time.Now().Before(deadline) {
			return found
		}
		time.Sleep(20 * time.Millisecond)
	}
}
