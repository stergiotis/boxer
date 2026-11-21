// Package apptest is the app conformance kit (ADR-0396 §8): the checks
// this module runs over its own apps, as functions another module's tests
// call over theirs. Each takes a testing.TB and fails it; none needs a
// host, a bus or a display.
//
//   - MountContext is a mount context for a test, whose ContextOf ends
//     with the test.
//   - ManifestOK validates a manifest as registration does.
//   - HelpCorpus checks a registered app's help book: indexed, front
//     matter valid, every page parses and loses nothing; HelpNamesCatalog
//     that it names every operation and resource of the catalog.
//   - CatalogNotWithdrawn checks that a registered app keeps its catalog
//     and that its instance serves it.
//   - FrameHeadless mounts one app, runs two frames under the discard
//     harness (egui2test) and unmounts it; FrameAll does so for every
//     windowed app of a registry. Strict mode fails on what a host only
//     warns about: an id used twice in a frame, an unbalanced pop.
package apptest

import (
	"runtime/debug"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
)

// MountContext is a mount context for a test of the app id: a lifetime
// scope the test's cleanup abandons, so app.ContextOf of it ends with the
// test; a Cancel channel the cleanup closes; a no-op bus and storage that
// refuse every call; a fresh id stack; no launch config. Set what the test
// needs with the StaticMountContext setters. An app built without Mount
// takes app.ContextOf(MountContext(t, id)) for the context field Mount
// would have set.
func MountContext(t testing.TB, id app.AppIdT) (mc *app.StaticMountContext) {
	t.Helper()
	mc, _ = newMountContext(t, id)
	return
}

// newMountContext is MountContext with the closing edge also offered to
// the caller: leave closes Cancel and retires the scope, as the host does
// before Unmount. The test's cleanup leaves and abandons the scope.
func newMountContext(t testing.TB, id app.AppIdT) (mc *app.StaticMountContext, leave func()) {
	cancel := make(chan struct{})
	mc = app.NewStaticMountContext(id, zerolog.Nop(), nil, nil, cancel)
	scope := lifetime.New(nil)
	mc.SetLifetime(scope)
	var once sync.Once
	leave = func() {
		once.Do(func() {
			close(cancel)
			scope.Retire()
		})
	}
	t.Cleanup(func() {
		leave()
		scope.Abandon()
	})
	return
}

// FrameContext is MountContext wrapped for Frame: focused, with no close
// and no operation gesture.
func FrameContext(t testing.TB, id app.AppIdT) (fc *app.StaticFrameContext) {
	t.Helper()
	fc = app.NewStaticFrameContext(MountContext(t, id))
	return
}

// ManifestOK fails t when m would be refused at registration on its own:
// Validate, the subject grammar of its capabilities included.
func ManifestOK(t testing.TB, m app.Manifest) {
	t.Helper()
	if err := m.Validate(); err != nil {
		t.Errorf("apptest: manifest %s: %v", m.Id, err)
	}
	if m.Operations != nil {
		if err := m.Operations.Validate(); err != nil {
			t.Errorf("apptest: manifest %s: operations catalog: %v", m.Id, err)
		}
	}
}

// HelpCorpus fails t when the help book of the app registered under id in
// app.DefaultRegistry is missing or broken: not indexed from Manifest.Help,
// front matter that does not conform, or a page that does not parse or that
// the markdown lowering loses content of.
func HelpCorpus(t testing.TB, id app.AppIdT) {
	t.Helper()
	m, registered := app.LookupManifest(id)
	if !registered {
		t.Errorf("apptest: %s is not registered", id)
		return
	}
	if m.Help == nil {
		t.Errorf("apptest: %s declares no Help", m.Id)
		return
	}
	help.SyncFromRegistry()
	b, ok := help.Book(m.Id)
	if !ok {
		t.Errorf("apptest: %s: the help book is not indexed; is the app registered?", m.Id)
		return
	}
	for _, p := range b.Validate() {
		t.Errorf("apptest: %s: help front matter: %+v", m.Id, p)
	}
	docs := b.Docs()
	if len(docs) == 0 {
		t.Errorf("apptest: %s: the help book has no pages", m.Id)
	}
	for _, info := range docs {
		doc, _, parsed := b.Doc(info.Path)
		if !parsed || doc == nil {
			t.Errorf("apptest: %s: help page %q does not parse", m.Id, info.Path)
			continue
		}
		if dropped := doc.Dropped(); len(dropped) > 0 {
			t.Errorf("apptest: %s: help page %q loses content in the lowering: %+v", m.Id, info.Path, dropped)
		}
	}
}

// HelpNamesCatalog fails t when the help book of the app registered under
// id does not name, in backticks, every operation and resource of its
// catalog: one a model is told exists and nothing explains.
func HelpNamesCatalog(t testing.TB, id app.AppIdT) {
	t.Helper()
	m, registered := app.LookupManifest(id)
	if !registered || m.Operations == nil {
		t.Errorf("apptest: %s is not registered with an operations catalog", id)
		return
	}
	help.SyncFromRegistry()
	b, ok := help.Book(m.Id)
	if !ok {
		t.Errorf("apptest: %s: the help book is not indexed", m.Id)
		return
	}
	var corpus strings.Builder
	for _, info := range b.Docs() {
		if src, found := b.Source(info.Path); found {
			corpus.Write(src)
			corpus.WriteByte('\n')
		}
	}
	text := corpus.String()
	for _, op := range m.Operations.Operations {
		if !strings.Contains(text, "`"+op.Name+"`") {
			t.Errorf("apptest: %s: operation %q is not named in the help book", m.Id, op.Name)
		}
	}
	for _, r := range m.Operations.Resources {
		if !strings.Contains(text, "`"+r.Name+"`") {
			t.Errorf("apptest: %s: resource %q is not named in the help book", m.Id, r.Name)
		}
	}
}

// CatalogNotWithdrawn fails t when id is not registered in reg with the
// operations catalog its manifest declared, or when an instance opened
// from reg does not serve it.
func CatalogNotWithdrawn(t testing.TB, reg *app.Registry, id app.AppIdT) {
	t.Helper()
	m, ok := reg.LookupManifest(id)
	if !ok {
		t.Errorf("apptest: %s is not registered", id)
		return
	}
	if m.Operations == nil {
		t.Errorf("apptest: %s is registered without an operations catalog", id)
		return
	}
	a, err := reg.Open(id)
	if err != nil {
		t.Errorf("apptest: %s: %v", id, err)
		return
	}
	if oa, isOps := a.(app.OperationsAppI); !isOps || oa.Operations() == nil {
		t.Errorf("apptest: %s: the instance serves no catalog", id)
	}
}

// FrameOpts tunes FrameHeadless and FrameAll.
type FrameOpts struct {
	// Strict fails on an id used twice in a frame and on a pop of an empty
	// or mismatched id stack, which a host only logs.
	Strict bool
	// Prepare, when set, runs on the mount context before Mount: a launch
	// config, a mount value, an environment the app reads.
	Prepare func(mc *app.StaticMountContext)
}

// FrameHeadless opens id from reg, mounts it, runs two frames under the
// discard harness with the id stack prepared as the window host prepares
// it, and unmounts it. An error from Mount, Frame or Unmount, a panic, or
// an id stack left unbalanced fails t, as does, in strict mode, any id
// violation.
func FrameHeadless(t testing.TB, reg *app.Registry, id app.AppIdT, opts FrameOpts) {
	t.Helper()
	reset := egui2test.Install()
	defer reset()
	var violations []string
	if opts.Strict {
		prev := c.OnIdViolation(func(what string, vid uint64) {
			violations = append(violations, what)
		})
		defer c.OnIdViolation(prev)
	}
	a, err := reg.Open(id)
	if err != nil {
		t.Errorf("apptest: %s: open: %v", id, err)
		return
	}
	mc, leave := newMountContext(t, id)
	ids := c.NewWidgetIdStack()
	mc.SetIds(ids)
	mc.SetInstanceKey(1)
	if opts.Prepare != nil {
		opts.Prepare(mc)
	}
	fc := app.NewStaticFrameContext(mc)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("apptest: %s: panic: %v\n%s", id, r, debug.Stack())
		}
	}()
	if err = a.Mount(mc); err != nil {
		t.Errorf("apptest: %s: mount: %v", id, err)
		return
	}
	for frame := 0; frame < 2; frame++ {
		c.CurrentApplicationState.StateManager.ScriptReset()
		depth := ids.Depth()
		for range c.IdScope(ids.PrepareHighEntropy(0x5e5e5e5e5e5e5e5e)) {
			if fErr := a.Frame(fc); fErr != nil {
				t.Errorf("apptest: %s: frame %d: %v", id, frame, fErr)
			}
		}
		if ids.Depth() != depth {
			t.Errorf("apptest: %s: frame %d leaves the id stack %d deep, not %d", id, frame, ids.Depth(), depth)
		}
	}
	// The closing edge as the window host takes it: Cancel and the scope's
	// work context end before Unmount; the tail ends with the test.
	leave()
	if err = a.Unmount(mc); err != nil {
		t.Errorf("apptest: %s: unmount: %v", id, err)
	}
	for _, v := range violations {
		t.Errorf("apptest: %s: %s", id, v)
	}
}

// FrameAll runs FrameHeadless over every windowed app of reg, each as a
// subtest named by its alias. Skip names apps the caller leaves out, with
// the reason the subtest reports.
func FrameAll(t *testing.T, reg *app.Registry, opts FrameOpts, skip map[app.AppIdT]string) {
	t.Helper()
	for _, m := range reg.Manifests() {
		if m.Surface != app.SurfaceWindowed {
			continue
		}
		t.Run(m.SubjectAlias(), func(t *testing.T) {
			if why, skipped := skip[m.Id]; skipped {
				t.Skip(why)
			}
			FrameHeadless(t, reg, m.Id, opts)
		})
	}
}
