package sqlapplet

// A bundle view (ADR-0288 (proposed) §SD7) is an embedded play showing an
// ad-hoc bundle inside another app's window: one constructor, one manifest
// entry. It follows the bundle — a dataset revision rebinds, a document
// revision rebuilds the embedded play, a retract leaves the view saying
// what it waits for — and does all of that on the frame it is drawn, never
// in between: a receiver that culls a view out of sight or keeps it in a
// tab may not call its frame for a long time, and the next frame shows the
// bundle as it stands rather than the sequence it missed. The bus is asked
// only off the render goroutine; answers wait in a mailbox.

import (
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// BundleViewCaps is what a receiver adds to its manifest to show bundles:
// resolving a bundle and its datasets, hearing their events, and the
// embedded play's two escape hatches. A bundle document may ask for
// nothing beyond these (§SD7): play.PublishBundleE composes only documents
// on the introspection endpoint that read their own datasets, so every
// bundle opens in every receiver.
var BundleViewCaps = []app.SubjectFilter{
	{Pattern: adhocdata.SubjectBundleResolve, Direction: app.CapDirectionPub,
		Reason: "a bundle view resolves the bundle it shows: its document and datasets (ADR-0288 §SD7)"},
	{Pattern: adhocdata.SubjectBundleEventAll, Direction: app.CapDirectionSub,
		Reason: "and follows the bundle's republish and retract"},
	{Pattern: adhocdata.SubjectResolve, Direction: app.CapDirectionPub,
		Reason: "and keeps the bundle's datasets bound"},
	{Pattern: adhocdata.SubjectEventAll, Direction: app.CapDirectionSub,
		Reason: "and follows their republish and retract"},
	{Pattern: clipboardbroker.SubjectWrite, Direction: app.CapDirectionPub,
		Reason: "Copy a fenced block out of a bundle view's Definition drawer (ADR-0132 §SD3)"},
	{Pattern: windowhost.OpenSubject, Direction: app.CapDirectionPub,
		Reason: "a bundle view's Open in Playground opens the bundle in a play window (ADR-0288 §SD7)"},
}

// BundleViewConfig is a bundle view's wiring: the receiver's bus, logger
// and identity, which the embedded play's runs are stamped with.
type BundleViewConfig struct {
	Bus         app.BusI
	Log         zerolog.Logger
	RunId       string
	StampAppId  string
	InstanceKey uint64
	// Rules is the gloss rule repository; nil takes play's default.
	Rules *gloss.Repository
	// Operable offers the view's play operations to agents through the
	// receiver's catalog (ADR-0288 (proposed) §SD8, BundleViewOps); a plain
	// view is drawn for the person only.
	Operable bool
}

// BundleView shows one bundle. Build it with NewBundleView, call Frame
// from the receiver's frame, and Close it when the receiver drops it.
type BundleView struct {
	alias string
	cfg   BundleViewConfig

	// The render goroutine's.
	inner    *play.PlayApp
	follower *adhocdata.Follower
	revision uint64
	digest   string
	runnable bool
	// panes are the result panes the embedded play shows.
	panes    []string
	failed   string
	polled   bool
	nextPoll time.Time
	unsub    func()

	// The mailbox: the event handler and the resolve goroutine write it.
	mu        sync.Mutex
	dirty     bool
	inflight  bool
	arrived   *adhocdata.BundleResult
	waiting   string
	withdrawn bool
}

// NewBundleView builds a view of the bundle under alias. It asks the bus
// nothing: the first frame starts the resolve, and the view shows what it
// waits for until the answer is applied.
func NewBundleView(alias string, cfg BundleViewConfig) (inst *BundleView) {
	inst = &BundleView{alias: alias, cfg: cfg, dirty: true, waiting: "not asked yet"}
	if cfg.Bus == nil {
		inst.waiting = "no bus to resolve the bundle over"
		inst.dirty = false
		return
	}
	unsub, err := cfg.Bus.Subscribe(adhocdata.SubjectBundleEventAll, func(msg *app.Msg) {
		ev, dErr := adhocdata.DecodeEvent(msg.Subject, msg.Payload)
		if dErr != nil || ev.Bundle != alias {
			return
		}
		inst.mu.Lock()
		inst.dirty = true
		inst.withdrawn = ev.Op == adhocdata.EventOpRetracted
		inst.mu.Unlock()
	})
	if err != nil {
		inst.polled = true
	} else {
		inst.unsub = unsub
	}
	return
}

// Alias is the bundle the view shows.
func (inst *BundleView) Alias() (alias string) { return inst.alias }

// Revision is the bundle revision the view shows; zero before the first.
func (inst *BundleView) Revision() (revision uint64) { return inst.revision }

// Inner is the embedded play, nil until the bundle was first applied.
func (inst *BundleView) Inner() (inner *play.PlayApp) { return inst.inner }

// Operable reports whether the view offers its operations to agents.
func (inst *BundleView) Operable() (operable bool) { return inst.cfg.Operable }

// Panes are the result panes the view shows; render goroutine only.
func (inst *BundleView) Panes() (panes []string) { return inst.panes }

// Waiting says what the view waits for, empty once it shows the bundle.
func (inst *BundleView) Waiting() (why string) {
	if inst.failed != "" {
		return inst.failed
	}
	if inst.inner != nil {
		return ""
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.waiting
}

// Close stops following the bundle and releases the embedded play.
func (inst *BundleView) Close() {
	if inst.unsub != nil {
		inst.unsub()
		inst.unsub = nil
	}
	if inst.follower != nil {
		inst.follower.Close()
		inst.follower = nil
	}
	if inst.inner != nil {
		inst.inner.Close()
		inst.inner = nil
	}
}

// Sync is the view's frame minus the drawing: start a resolve when one is
// due, apply what arrived, keep the datasets bound. Frame calls it; a lane
// without a client calls it alone.
func (inst *BundleView) Sync() {
	now := time.Now()
	inst.mu.Lock()
	if inst.polled && !inst.inflight && now.After(inst.nextPoll) && (inst.withdrawn || inst.revision == 0) {
		inst.dirty = true
	}
	start := inst.dirty && !inst.inflight && inst.cfg.Bus != nil
	if start {
		inst.dirty, inst.inflight = false, true
		inst.nextPoll = now.Add(adhocdata.DefaultPollInterval)
	}
	arrived := inst.arrived
	inst.arrived = nil
	inst.mu.Unlock()
	if start {
		go inst.resolve()
	}
	if arrived != nil {
		inst.apply(*arrived)
	}
	if inst.follower != nil && inst.inner != nil {
		if bound, _ := inst.follower.Sync(inst.inner); bound && inst.runnable {
			inst.inner.RequestRun()
		}
	}
	if inst.inner != nil {
		inst.mu.Lock()
		withdrawn := inst.withdrawn
		inst.mu.Unlock()
		if withdrawn {
			inst.inner.SetDatasetNotice([]byte("**Bundle `" + inst.alias + "` was withdrawn.** The view reloads it by itself if it is published again."))
		}
	}
}

// Frame syncs the view and draws it: the embedded play once the bundle was
// applied, else what the view waits for.
func (inst *BundleView) Frame(ctx app.FrameContextI) (err error) {
	inst.Sync()
	if inst.inner == nil {
		inst.mu.Lock()
		waiting := inst.waiting
		inst.mu.Unlock()
		if inst.failed != "" {
			waiting = inst.failed
		}
		c.Label("Waiting for bundle " + inst.alias + ": " + waiting).Send()
		return nil
	}
	if inst.cfg.Operable {
		return inst.inner.FrameServed(ctx)
	}
	return inst.inner.Frame(ctx)
}

func (inst *BundleView) resolve() {
	res, err := adhocdata.ResolveBundleRequest(inst.cfg.Bus, inst.alias, nil)
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.inflight = false
	if err != nil {
		inst.waiting = err.Error()
		return
	}
	inst.waiting, inst.withdrawn = "", false
	inst.arrived = &res
}

// apply takes a resolved bundle in on the render goroutine. A new document
// rebuilds the embedded play from it — parameter values the person set are
// not carried over — and every revision rebinds the datasets under their
// local names.
func (inst *BundleView) apply(res adhocdata.BundleResult) {
	if res.Revision == inst.revision && res.DocumentDigest == inst.digest {
		return
	}
	if res.DocumentDigest != inst.digest || inst.inner == nil {
		def, err := ParseDocSource("bundle", bundleDocPath(inst.alias+".md"), res.Document)
		switch {
		case err != nil:
			inst.failed = "its document does not parse: " + err.Error()
		case def == nil:
			inst.failed = "its document has no sql fence"
		case def.Endpoint != EndpointIntrospection:
			inst.failed = "its document does not ask for the introspection endpoint, where bundles resolve"
		}
		if inst.failed != "" {
			inst.revision, inst.digest = res.Revision, res.DocumentDigest
			return
		}
		inner, eErr := NewEmbedded(def, EmbedConfig{StampAppId: inst.cfg.StampAppId, RunId: inst.cfg.RunId,
			InstanceKey: inst.cfg.InstanceKey, Bus: inst.cfg.Bus, Log: inst.cfg.Log, Rules: inst.cfg.Rules})
		if eErr != nil {
			inst.failed = "the embedded play could not be built: " + eErr.Error()
			inst.revision, inst.digest = res.Revision, res.DocumentDigest
			return
		}
		// The first run waits for the datasets: an AutoRun before they are
		// bound would only report an unknown table.
		inst.runnable = inner.AutoRun && def.Class == analysis.QuerySecurityRead
		inner.AutoRun = false
		inner.SetOpenPlaygroundBundle(inst.alias)
		if inst.inner != nil {
			inst.inner.Close()
		}
		if inst.follower != nil {
			inst.follower.Close()
			inst.follower = nil
		}
		inst.inner, inst.failed = inner, ""
		inst.panes = inst.panes[:0]
		for _, spec := range inner.Tabs().Specs() {
			inst.panes = append(inst.panes, spec.ID)
		}
	}
	names := make(map[string]string, len(res.Datasets))
	for _, d := range res.Datasets {
		names[d.Alias] = d.LocalName
	}
	if inst.follower == nil {
		inst.follower = adhocdata.NewDeferredFollower(adhocdata.FollowerConfig{Bus: inst.cfg.Bus, Log: inst.cfg.Log, LocalNames: names})
	}
	if inst.follower != nil {
		for _, d := range res.Datasets {
			inst.follower.FollowAs(d.Alias, d.LocalName)
		}
	}
	for _, d := range res.Datasets {
		// A grant names the dataset by its bundle or its alias, never by
		// the name the document reads it under (ADR-0288 §SD3).
		inst.inner.SetDatasetOrigin(d.LocalName, d.Alias, inst.alias)
	}
	inst.revision, inst.digest = res.Revision, res.DocumentDigest
	inst.inner.SetDatasetNotice(nil)
}
