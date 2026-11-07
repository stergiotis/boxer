package adhocdemo

import (
	"fmt"
	"time"

	"github.com/stergiotis/boxer/apps/sqlapplet"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
)

// The window's catalog (ADR-0269, keelson-app A1–A3): the two buttons as
// asynchronous commands that settle when their task lands (ADR-0358),
// one query over the window's own state, and the delegated bundle_
// operations the view inside it serves (ADR-0288 §SD8).

const (
	resGeneration = "generation"
	resTree       = "tree"

	opGetView     = "get_view"
	opRegenerate  = "regenerate"
	opPublishTree = "publish_tree"
)

// Refusals name their remedy (A2).
const (
	notPublishedReason   = "the first publish has not landed; get_view says what it waits on"
	publishingReason     = "a publish is in flight; wait for it to settle"
	treePublishingReason = "a tree publish is in flight; wait for it to settle"
)

// generationValue is the generation resource's value: the generation
// last asked for and the one whose publish landed, comparable so the host
// can tell a change.
type generationValue struct {
	Asked     int
	Published int
}

// treeValue is the tree resource's value.
type treeValue struct {
	Asked     int
	Published int
	Mount     string
}

// adhocSnap is what the catalog's own queries read; the view's operations
// read the view's snapshot, which BundleViewOps captures.
type adhocSnap struct {
	bundle     string
	revision   uint64
	gen        generationValue
	publishing bool
	publishErr string
	tree       treeValue
	treeRun    bool
	treeNote   string
	treeErr    string
	treeExpiry time.Time
}

func (inst *App) generationValue() (v generationValue) {
	v = generationValue{Asked: inst.gen, Published: inst.publishedGen}
	return
}

func (inst *App) treeValue() (v treeValue) {
	v = treeValue{Asked: inst.treeGen, Published: inst.treeLandedGen}
	if inst.treeMount.Value() != 0 {
		v.Mount = fmt.Sprintf("%016x", inst.treeMount.Value())
	}
	return
}

func (inst *App) opsSnapshot() (sn adhocSnap) {
	sn = adhocSnap{bundle: inst.bundle, gen: inst.generationValue(), tree: inst.treeValue(), treeNote: inst.treeNote, treeExpiry: inst.treeExpires}
	if inst.view != nil {
		sn.revision = inst.view.Revision()
	}
	if s := inst.bundleJob.Snapshot(); s.State == bgjob.StateRunning {
		sn.publishing = true
	} else if s.State == bgjob.StateFailed && s.Err != nil {
		sn.publishErr = s.Err.Error()
	}
	if s := inst.treeJob.Snapshot(); s.State == bgjob.StateRunning {
		sn.treeRun = true
	} else if s.State == bgjob.StateFailed && s.Err != nil {
		sn.treeErr = s.Err.Error()
	}
	return
}

// View is get_view's result: the bundle half and the tree half.
type View struct {
	Bundle         string `desc:"the bundle's alias, this window's own"`
	Revision       uint64 `desc:"the bundle revision the view shows; 0 before the first publish landed"`
	Generation     int    `desc:"the generation of the series last asked for; regenerate raises it"`
	Published      int    `desc:"the generation whose publish landed; -1 before the first"`
	Publishing     bool   `desc:"true while a publish runs"`
	PublishError   string `desc:"the last publish's error, empty when none"`
	TreeGen        int    `desc:"the tree generation last asked for; 0 before the first publish_tree"`
	TreePublished  int    `desc:"the tree generation whose publish landed; 0 before the first"`
	TreePublishing bool   `desc:"true while a tree publish runs"`
	TreeMount      string `desc:"the lading mount the tree lives in, as 16 hex digits; empty before the first landed"`
	TreeNote       string `desc:"what the bar says about the tree: entries, mount, expiry"`
	TreeError      string `desc:"the last tree publish's error, empty when none"`
}

// RegenerateOutcome is what regenerate settles with.
type RegenerateOutcome struct {
	Generation int    `desc:"the generation now published"`
	Bundle     string `desc:"the bundle's alias"`
	Revision   uint64 `desc:"the bundle revision the view shows as the call settles; the view follows the service's event, so it may still be the previous one"`
}

// PublishTreeOutcome is what publish_tree settles with.
type PublishTreeOutcome struct {
	Generation int       `desc:"the tree generation now published"`
	Mount      string    `desc:"the lading mount, as 16 hex digits; the same across generations"`
	Entries    uint64    `desc:"entries the snapshot holds"`
	ExpiresAt  time.Time `desc:"when the store forgets the snapshot"`
}

var adhocOps = func() (s *appops.Set[*App, adhocSnap]) {
	s = appops.NewSet((*App).opsSnapshot)

	s.Resource(resGeneration, "the series generation asked for and the one whose publish landed",
		func(inst *App) any { return inst.generationValue() })
	s.Resource(resTree, "the tree generation asked for, the one whose publish landed, and its mount",
		func(inst *App) any { return inst.treeValue() })

	appops.Query(s, app.OperationSpec{Name: opGetView, Version: 1,
		Summary: "read the window's own state: the bundle's alias, revision and generation, what a publish waits on, and the tree's mount",
		Reads:   []string{resGeneration, resTree}, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn adhocSnap, _ appops.None) (out View, err error) {
			out = View{Bundle: sn.bundle, Revision: sn.revision, Generation: sn.gen.Asked, Published: sn.gen.Published,
				Publishing: sn.publishing, PublishError: sn.publishErr,
				TreeGen: sn.tree.Asked, TreePublished: sn.tree.Published, TreePublishing: sn.treeRun,
				TreeMount: sn.tree.Mount, TreeNote: sn.treeNote, TreeError: sn.treeErr}
			return
		})

	// Regenerate republishes the bundle under the same alias as a task;
	// the call settles when the republish lands (ADR-0358). The bundle is
	// the window's own authored state, so the effect is document.
	appops.Command(s, app.OperationSpec{Name: opRegenerate, Version: 1,
		Summary: "compute the next generation of the series and republish the bundle; settles when the republish lands",
		Effect:  app.OperationEffectDocument, Writes: []string{resGeneration}, Agents: true, Async: true, Gesture: "the Regenerate button"},
		func(inst *App, call app.OperationCall, _ appops.None) (out appops.None, err error) {
			switch {
			case inst.view == nil:
				err = app.RefuseOperation(notPublishedReason)
			case inst.bundleJob.Running():
				err = app.RefuseOperation(publishingReason)
			default:
				inst.regenerate()
				inst.waitGen(call.Key, inst.gen)
			}
			return
		})
	appops.Settles(s, opRegenerate, func(inst *App, call app.OperationCall) (out RegenerateOutcome, settled bool, err error) {
		target, found := inst.pendingGen[call.Key]
		switch {
		case !found:
			return out, true, app.RefuseOperation("no regenerate was asked under call " + call.Key)
		case inst.publishedGen >= target:
			delete(inst.pendingGen, call.Key)
			out = RegenerateOutcome{Generation: inst.publishedGen, Bundle: inst.bundle}
			if inst.view != nil {
				out.Revision = inst.view.Revision()
			}
			return out, true, nil
		}
		if s := inst.bundleJob.Snapshot(); s.State == bgjob.StateFailed && s.Err != nil {
			delete(inst.pendingGen, call.Key)
			return out, true, s.Err
		}
		return out, false, nil
	})
	s.Available(opRegenerate, func(sn adhocSnap) (ok bool, reason string) {
		switch {
		case sn.gen.Published < 0:
			return false, notPublishedReason
		case sn.publishing:
			return false, publishingReason
		}
		return true, ""
	})

	// Publish tree writes a snapshot into the lading store and opens a
	// tally window: outside this window twice over, so consequential, and
	// the host asks the person every time.
	appops.Command(s, app.OperationSpec{Name: opPublishTree, Version: 1,
		Summary: "walk the demo tree into the lading store as a snapshot of the window's mount and open tally on it; settles when the snapshot landed",
		Effect:  app.OperationEffectConsequential, Writes: []string{resTree}, Agents: true, Async: true, Gesture: "the Publish tree & browse in tally button"},
		func(inst *App, call app.OperationCall, _ appops.None) (out appops.None, err error) {
			if inst.treeJob.Running() {
				err = app.RefuseOperation(treePublishingReason)
				return
			}
			inst.publishTree()
			inst.waitTree(call.Key, inst.treeGen)
			return
		})
	appops.Settles(s, opPublishTree, func(inst *App, call app.OperationCall) (out PublishTreeOutcome, settled bool, err error) {
		target, found := inst.pendingTree[call.Key]
		switch {
		case !found:
			return out, true, app.RefuseOperation("no publish_tree was asked under call " + call.Key)
		case inst.treeLandedGen >= target:
			delete(inst.pendingTree, call.Key)
			out = PublishTreeOutcome{Generation: inst.treeLandedGen, Mount: inst.treeValue().Mount, Entries: inst.treeEntries, ExpiresAt: inst.treeExpires}
			return out, true, nil
		}
		if s := inst.treeJob.Snapshot(); s.State == bgjob.StateFailed && s.Err != nil {
			delete(inst.pendingTree, call.Key)
			return out, true, s.Err
		}
		return out, false, nil
	})
	s.Available(opPublishTree, func(sn adhocSnap) (ok bool, reason string) {
		if sn.treeRun {
			return false, treePublishingReason
		}
		return true, ""
	})

	// The bundle view's operations, served by the play inside it.
	sqlapplet.BundleViewOps(s, func(inst *App) map[string]*sqlapplet.BundleView {
		if inst.view == nil {
			return nil
		}
		return map[string]*sqlapplet.BundleView{"items": inst.view}
	})
	return
}()

// Operations serves the window's catalog.
func (inst *App) Operations() (h app.OperationsHandlerI) { return adhocOps.Bind(inst) }

var _ app.OperationsAppI = (*App)(nil)

// waitGen records that the call waits for generation gen's publish; an
// empty key is the person's gesture, which nothing settles.
func (inst *App) waitGen(key string, gen int) {
	if key == "" {
		return
	}
	if inst.pendingGen == nil {
		inst.pendingGen = make(map[string]int)
	}
	inst.pendingGen[key] = gen
}

func (inst *App) waitTree(key string, gen int) {
	if key == "" {
		return
	}
	if inst.pendingTree == nil {
		inst.pendingTree = make(map[string]int)
	}
	inst.pendingTree[key] = gen
}

// gesture routes one of the person's buttons through the catalog, so the
// command log sees it as theirs (ADR-0269 §SD8), and applies it directly
// where no host serves the catalog (tour, tests).
func gesture(inst *App, op string, direct func()) {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[appops.None, appops.None](inst.frameCtx, op, appops.None{}); err == nil {
			return
		}
	}
	direct()
}

func (inst *App) regenerateGesture() { gesture(inst, opRegenerate, inst.regenerate) }

func (inst *App) publishTreeGesture() { gesture(inst, opPublishTree, inst.publishTree) }
