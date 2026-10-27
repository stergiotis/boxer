package ladingpublish

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"sync"
	"testing/fstest"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingingest"
	"github.com/stergiotis/boxer/public/fs/lading/ladingpolicy"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// DefaultTtlDays is the class a request that names none is kept for.
const DefaultTtlDays = 30

// storeNamePrefix begins the policy record's store field for a published
// mount, before the publishing app's id, as ladingadhoc's "adhoc:" does.
const storeNamePrefix = "publish:"

// Limits are the deployment's bounds on a publish (ADR-0338 §SD3, §SD4).
type Limits struct {
	// Ceiling is the highest tier the store may hold.
	Ceiling agentrun.TierE
	// MaxTtlDays is the longest retention class a request may name.
	MaxTtlDays uint16
	// MaxBytes bounds the files' bytes in one request.
	MaxBytes int64
}

// LimitsFromEnv are the limits the registered variables give.
func LimitsFromEnv() (l Limits) {
	l.Ceiling = agentrun.TierAmber
	if t, ok := agentrun.ParseTier(Ceiling.Get()); ok {
		l.Ceiling = t
	}
	l.MaxTtlDays = uint16(min(max(MaxTtlDays.Get(), 1), 65535))
	l.MaxBytes = max(MaxBytes.Get(), 0)
	return
}

// Config parameterises the Service.
type Config struct {
	// Bus backs the publish subjects. Nil leaves only the in-process method.
	Bus *inprocbus.Inst
	// Registry is where the tag owners are read from: the manifests that
	// declare a publish subject. Nil reads app.DefaultRegistry.
	Registry *app.Registry
	// Connect opens the store's executor on the first publish, and again
	// after a failure; nil refuses every publish for want of a store.
	Connect func(ctx context.Context) (exec recordstore.ExecutorI, err error)
	// Layout is where the store's tables live; the zero value is the
	// default database.
	Layout ladingschema.Layout
	Limits Limits
	// Trail receives one row per publish (ADR-0338 §SD5); nil records
	// nothing beyond the log.
	Trail *trail.Recorder
	Log   zerolog.Logger
}

// Service answers lading.publish.<tag>.
type Service struct {
	cfg       Config
	log       zerolog.Logger
	busClient *inprocbus.Client
	unsub     func()
	// owners is each declared tag's app; a tag two apps declare maps to
	// the empty id, which owns nothing.
	owners map[identifier.TagValue]app.AppIdT

	// turn serialises the writes: the generated stores are single-goroutine
	// (ADR-0100), and a publish is batch-shaped. A channel, not a mutex, so
	// a publish waiting behind another gives up at its deadline. Holding it
	// guards exec.
	turn chan struct{}
	exec recordstore.ExecutorI

	// closeMu orders the handlers' wg.Add against Close's Wait; ctx ends
	// with Close, and with it every publish in flight.
	closeMu sync.Mutex
	closed  bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// newService is the service without its subscription.
func newService(cfg Config, log zerolog.Logger, reg *app.Registry) (inst *Service) {
	inst = &Service{cfg: cfg, log: log, turn: make(chan struct{}, 1)}
	inst.ctx, inst.cancel = context.WithCancel(context.Background())
	if reg != nil {
		inst.owners = ownersOf(reg.AllManifests(), log)
	}
	return
}

// NewService reads the tag owners from the registry and subscribes.
func NewService(cfg Config) (inst *Service, err error) {
	reg := cfg.Registry
	if reg == nil {
		reg = app.DefaultRegistry
	}
	inst = newService(cfg, cfg.Log.With().Str("app", string(ServiceAppId)).Logger(), reg)
	if cfg.Bus == nil {
		return
	}
	inst.busClient = cfg.Bus.NewClient(ServiceAppId, []app.SubjectFilter{
		{Pattern: subjectWildcard, Direction: app.CapDirectionBoth, Reason: "the lading publish service serves lading.publish.<tag> (ADR-0338)"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "the lading publish service replies to inboxes"},
	})
	if inst.unsub, err = inst.busClient.Subscribe(subjectWildcard, inst.handle); err != nil {
		err = eh.Errorf("ladingpublish: subscribe: %w", err)
	}
	return
}

// ownersOf maps each tag a manifest declares a publish subject for to its
// app; a declaration to subscribe only owns nothing. A tag two apps declare
// is logged and owned by neither.
func ownersOf(manifests []app.Manifest, log zerolog.Logger) (owners map[identifier.TagValue]app.AppIdT) {
	owners = make(map[identifier.TagValue]app.AppIdT)
	for _, m := range manifests {
		for _, c := range m.Caps {
			tag, ok := tagOfSubject(c.Pattern)
			if !ok || (c.Direction != app.CapDirectionPub && c.Direction != app.CapDirectionBoth) {
				continue
			}
			prior, has := owners[tag]
			switch {
			case !has:
				owners[tag] = m.Id
			case prior != m.Id:
				log.Error().Uint64("tag", uint64(tag)).Str("app", string(m.Id)).Str("other", string(prior)).
					Msg("ladingpublish: two apps declare one mount tag; neither may publish under it")
				owners[tag] = ""
			}
		}
	}
	return
}

// Close unsubscribes, cancels the publishes in flight and waits for them.
func (inst *Service) Close() {
	inst.closeMu.Lock()
	inst.closed = true
	inst.closeMu.Unlock()
	if inst.unsub != nil {
		inst.unsub()
		inst.unsub = nil
	}
	inst.cancel()
	inst.wg.Wait()
}

func (inst *Service) handle(msg *app.Msg) {
	if msg.Sender == ServiceAppId || msg.Reply == "" {
		return
	}
	tag, _ := tagOfSubject(msg.Subject)
	req, derr := buscodec.Decode[Request](msg.Payload)
	switch {
	case derr != nil:
		// Not a publish yet, but a refused one all the same (ADR-0338 §SD5).
		res := Result{Refused: true, Reason: "the request does not decode: " + derr.Error()}
		inst.record(time.Now(), msg.Sender, msg.SenderInstance, trail.LadingPublish{Tag: uint64(tag)}, res)
		inst.reply(msg, res)
		return
	case req.DryRun:
		// The check alone is quick, answered on the sender's goroutine.
		res := Result{Mount: req.Mount}
		if res.Reason = inst.check(msg.Sender, tag, req); res.Reason != "" {
			res.Refused = true
		}
		inst.reply(msg, res)
		return
	}
	inst.closeMu.Lock()
	if inst.closed {
		inst.closeMu.Unlock()
		return
	}
	inst.wg.Add(1)
	inst.closeMu.Unlock()
	// The deadline runs from receipt, so a publish queued behind another
	// gives up before the client stops waiting for it.
	ctx, cancel := context.WithTimeout(inst.ctx, writeDeadline)
	go func() {
		defer inst.wg.Done()
		defer cancel()
		inst.reply(msg, inst.Publish(ctx, msg.Sender, msg.SenderInstance, tag, req))
	}()
}

func (inst *Service) reply(msg *app.Msg, res Result) {
	if err := buscodec.Reply(inst.busClient.Publish, msg.Reply, res); err != nil {
		inst.log.Warn().Err(err).Str("from", string(msg.Sender)).Msg("ladingpublish: reply")
	}
}

// Publish checks and writes req from the app by under tag. It is what the
// bus handler runs, and the in-process form for a caller that holds the
// service.
func (inst *Service) Publish(ctx context.Context, by app.AppIdT, instance uint64, tag identifier.TagValue, req Request) (res Result) {
	row := trailRow(tag, req)
	at := time.Now()
	defer func() { inst.record(at, by, instance, row, res) }()
	refuse := func(reason string) Result {
		return Result{Mount: req.Mount, Refused: true, Reason: reason}
	}
	if reason := inst.check(by, tag, req); reason != "" {
		return refuse(reason)
	}
	ttl := req.TtlDays
	if ttl == 0 {
		ttl = DefaultTtlDays
	}
	mount := identifier.TaggedId(req.Mount)
	if mount == 0 {
		var err error
		if mount, err = mintMount(tag); err != nil {
			return Result{Failed: true, Reason: err.Error()}
		}
	}
	row.Mount, row.TtlDays = uint64(mount), uint32(ttl)
	out, err := inst.write(ctx, by, mount, ttl, req)
	if err != nil {
		return Result{Mount: uint64(mount), Failed: true, Reason: err.Error()}
	}
	row.Snapshot = uint64(out.Snap.UnixNano())
	return Result{Mount: uint64(mount), SnapNanos: out.Snap.UnixNano(), ExpiresNanos: out.ExpiresAt.UnixNano(), Files: uint64(len(req.Files)), Bytes: out.Bytes}
}

// check is the reason req may not be written, empty when it may.
func (inst *Service) check(by app.AppIdT, tag identifier.TagValue, req Request) (reason string) {
	lim := inst.cfg.Limits
	switch owner, ok := inst.owners[tag]; {
	case !tag.IsValid():
		return "the subject names no mount tag"
	case !ok:
		return "no app declares the mount tag " + strconv.FormatUint(uint64(tag), 10)
	case owner == "":
		return "two apps declare the mount tag " + strconv.FormatUint(uint64(tag), 10) + "; neither may publish under it"
	case owner != by:
		return "the mount tag " + strconv.FormatUint(uint64(tag), 10) + " is " + string(owner) + "'s"
	}
	if m := identifier.TaggedId(req.Mount); req.Mount != 0 && (!tag.GetTag().SameTag(m) || m.RemoveTag() == 0) {
		return "the mount is not under the tag the request was made under"
	}
	if req.Confined {
		return "the tree holds sealed data, which the lading store may not hold (ADR-0145)"
	}
	if t := agentrun.TierE(req.Tier); t > lim.Ceiling {
		return "the tree is " + t.String() + ", above the " + lim.Ceiling.String() + " the lading store may hold"
	}
	if req.TtlDays > lim.MaxTtlDays {
		return "the class of " + strconv.Itoa(int(req.TtlDays)) + " days is longer than the " + strconv.Itoa(int(lim.MaxTtlDays)) + " this host keeps"
	}
	if len(req.Files) == 0 {
		return "the tree has no files"
	}
	var total int64
	seen := make(map[string]bool, len(req.Files))
	for _, f := range req.Files {
		if !fs.ValidPath(f.Path) || f.Path == "." {
			return "the path " + strconv.Quote(f.Path) + " is not a slash-separated path from the root"
		}
		if seen[f.Path] {
			return "the path " + strconv.Quote(f.Path) + " is in the tree twice"
		}
		seen[f.Path] = true
		total += int64(len(f.Data))
	}
	// A path that is also another's directory would hide that one.
	for _, f := range req.Files {
		for d := path.Dir(f.Path); d != "."; d = path.Dir(d) {
			if seen[d] {
				return "the path " + strconv.Quote(d) + " is a file and a directory of " + strconv.Quote(f.Path)
			}
		}
	}
	if total > lim.MaxBytes {
		return "the tree has " + strconv.FormatInt(total, 10) + " bytes, more than the " + strconv.FormatInt(lim.MaxBytes, 10) + " one publish may carry"
	}
	return ""
}

// write records the mount's policy and writes one snapshot of req's files.
func (inst *Service) write(ctx context.Context, by app.AppIdT, mount identifier.TaggedId, ttl uint16, req Request) (out ladingingest.Result, err error) {
	select {
	case inst.turn <- struct{}{}:
	case <-ctx.Done():
		err = eh.Errorf("gave up waiting behind another publish: %w", context.Cause(ctx))
		return
	}
	defer func() { <-inst.turn }()
	exec, err := inst.executor(ctx)
	if err != nil {
		return
	}
	defer func() {
		// A store that failed once is connected to afresh on the next publish.
		if err != nil {
			inst.exec = nil
		}
	}()
	layout := inst.cfg.Layout
	if err = lading.VerifyIn(ctx, exec, layout); err != nil {
		err = eh.Errorf("the lading store: %w", err)
		return
	}
	policy := ladingingest.DefaultPolicy()
	policy.Ttl = ladingingest.TtlClassE(ttl)
	// Every file is stored whole: the request's bound is the per-file bound,
	// so none is left as a reference to a source the store cannot reach.
	policy.InlineMax = uint64(max(inst.cfg.Limits.MaxBytes, 1))
	policies := ladingpolicy.NewPolicyStore(exec, nil, ladingpolicy.PolicyStoreConfig{Table: layout.PolicyTable()})
	defer policies.Close()
	name := req.Name
	if name == "" {
		name = "published tree"
	}
	if err = ladingingest.RecordPolicy(ctx, policies, mount, policy, name, storeNamePrefix+string(by)); err != nil {
		err = eh.Errorf("record the mount's policy: %w", err)
		return
	}
	stores := lading.NewStores(exec, layout)
	defer stores.Meta.Close()
	defer stores.Data.Close()
	if out, err = ladingingest.Snapshot(ctx, treeOf(req.Files, time.Now()), mount, policy, stores); err != nil {
		err = eh.Errorf("write the snapshot: %w", err)
		return
	}
	if out.Stored != uint64(len(req.Files)) || out.Errors != 0 {
		err = eh.Errorf("the snapshot holds %d of %d files whole, with %d errors", out.Stored, len(req.Files), out.Errors)
	}
	return
}

// executor is the held executor, connecting when there is none.
func (inst *Service) executor(ctx context.Context) (exec recordstore.ExecutorI, err error) {
	if inst.exec != nil {
		return inst.exec, nil
	}
	if inst.cfg.Connect == nil {
		return nil, eh.Errorf("no lading store is configured on this host")
	}
	if exec, err = inst.cfg.Connect(ctx); err != nil {
		return nil, eh.Errorf("connect to the lading store: %w", err)
	}
	inst.exec = exec
	return
}

// treeOf is the files as a tree whose every file was modified at at.
func treeOf(files []File, at time.Time) (fsys fs.FS) {
	m := make(fstest.MapFS, len(files))
	for _, f := range files {
		m[f.Path] = &fstest.MapFile{Data: f.Data, Mode: 0o644, ModTime: at}
	}
	return m
}

// mintMount is a fresh mount id under tag over a random body.
func mintMount(tag identifier.TagValue) (mount identifier.TaggedId, err error) {
	t := tag.GetTag()
	var buf [8]byte
	if _, err = rand.Read(buf[:]); err != nil {
		err = eh.Errorf("mint a mount id: %w", err)
		return
	}
	body := binary.LittleEndian.Uint64(buf[:]) % (uint64(t.GetMaxPossibleIdIncl()) + 1)
	if body == 0 {
		body = 1
	}
	return t.ComposeId(identifier.UntaggedId(body)), nil
}

// trailRow is the request's part of its trail row: the label, and every
// path with its digest, sorted.
func trailRow(tag identifier.TagValue, req Request) (row trail.LadingPublish) {
	row = trail.LadingPublish{Tag: uint64(tag), Mount: req.Mount, Name: req.Name, TtlDays: uint32(req.TtlDays),
		Tier: agentrun.TierE(req.Tier).String(), Confined: req.Confined}
	files := slices.Clone(req.Files)
	slices.SortFunc(files, func(a, b File) int {
		switch {
		case a.Path < b.Path:
			return -1
		case a.Path > b.Path:
			return 1
		}
		return 0
	})
	for _, f := range files {
		sum := sha256.Sum256(f.Data)
		row.Paths = append(row.Paths, f.Path)
		row.Digests = append(row.Digests, hex.EncodeToString(sum[:]))
		row.Bytes += uint64(len(f.Data))
	}
	return
}

// record writes the publish's trail row and logs it.
func (inst *Service) record(at time.Time, by app.AppIdT, instance uint64, row trail.LadingPublish, res Result) {
	switch {
	case res.Refused:
		row.Decision = "refused"
	case res.Failed:
		row.Decision = "failed"
	default:
		row.Decision = "written"
	}
	if res.Reason != "" {
		row.Reason = []string{res.Reason}
	}
	ev := inst.log.Info()
	if res.Refused || res.Failed {
		ev = inst.log.Warn().Str("reason", res.Reason)
	}
	ev.Str("from", string(by)).Uint64("tag", row.Tag).Uint64("mount", row.Mount).Int("files", len(row.Paths)).
		Uint64("bytes", row.Bytes).Str("decision", row.Decision).Msg("ladingpublish: publish")
	rec := inst.cfg.Trail
	if rec == nil || !rec.Durable() {
		return
	}
	c := trail.Context{Origin: rec.OriginOf(by, instance)}
	if err := rec.LadingPublish(at, c, row); err != nil {
		inst.log.Warn().Err(err).Msg("ladingpublish: buffer the trail row")
		return
	}
	rec.FlushSoon()
}
