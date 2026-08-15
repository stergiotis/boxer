package windowhost

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// ADR-0188 M3 — the interleaving lane. Windows of a factory app and of a
// singleton app open and close in any order; each instance subscribes on
// the bus and publishes an ad-hoc dataset in Mount, and retracts it in
// Unmount, the adhocdemo shape. After every step the runtime's effect
// state must be a function of the set of open windows alone — no leaked
// subscription, client or dataset from a window that has closed — and at
// the end it must equal what a fresh host given only the final set of open
// windows produces: the confluence oracle of the lessons page (its L5) —
// dynamic history leaves no trace.

// effApp is the app under test: one subscription and one dataset per
// mounted instance, released in Unmount the way a producer does today.
type effApp struct {
	manifest app.Manifest
	svc      *adhocdata.Service
	handle   string
	key      uint64
	unsub    func()
}

var _ app.AppI = (*effApp)(nil)

func (inst *effApp) Manifest() (m app.Manifest) { return inst.manifest }
func (inst *effApp) Mount(ctx app.MountContextI) (err error) {
	inst.key = ctx.InstanceKey()
	inst.unsub, err = ctx.Bus().Subscribe(fmt.Sprintf("t.%d", inst.key), func(*app.Msg) {})
	if err != nil {
		return
	}
	res, pErr := inst.svc.Publish(adhocdata.PublishInput{
		Alias:     fmt.Sprintf("ds_%s", strings.ReplaceAll(string(inst.manifest.Id), ".", "_")),
		Publisher: string(inst.manifest.Id), ArrowIPCStream: oneRowStream(),
	})
	if pErr != nil {
		err = pErr
		return
	}
	inst.handle = res.Handle
	return
}
func (inst *effApp) Frame(ctx app.FrameContextI) (err error) { return }
func (inst *effApp) Unmount(ctx app.MountContextI) (err error) {
	// The producer's own release: retract, and (deliberately) NOT the
	// subscription — the host must release that.
	if inst.handle != "" {
		_ = inst.svc.Retract(inst.handle)
		inst.handle = ""
	}
	return
}

func oneRowStream() []byte {
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	rb.Field(0).(*array.Int64Builder).AppendValues([]int64{1}, nil)
	rec := rb.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	_ = w.Write(rec)
	_ = w.Close()
	return buf.Bytes()
}

type nopKeys struct{}

func (nopKeys) RegisterDatasetKey(string, []byte) {}
func (nopKeys) DeregisterDatasetKey(string)       {}

const (
	effFactoryId   app.AppIdT = "test.eff.factory"
	effSingletonId app.AppIdT = "test.eff.singleton"
)

// effWorld is one host with its bus and dataset service.
type effWorld struct {
	bus  *inprocbus.Inst
	reg  *introspect.Registry
	svc  *adhocdata.Service
	host *Inst
	open []WindowKeyT
}

func newEffWorld(t *testing.T) (w *effWorld) {
	t.Helper()
	w = &effWorld{bus: inprocbus.NewInst(zerolog.Nop()), reg: introspect.NewRegistry()}
	svc, err := adhocdata.NewService(adhocdata.Config{
		Bus: w.bus, Registry: w.reg, Keys: nopKeys{}, Dir: t.TempDir(), Log: zerolog.Nop(),
		RetractGrace: time.Millisecond,
	})
	require.NoError(t, err)
	w.svc = svc
	appReg := app.NewRegistry()
	caps := []app.SubjectFilter{{Pattern: "t.>", Direction: app.CapDirectionBoth}}
	fm := mkManifest(effFactoryId)
	fm.Caps = caps
	require.NoError(t, appReg.RegisterFactory(fm, func() (a app.AppI, err error) {
		a = &effApp{manifest: fm, svc: svc}
		return
	}))
	sm := mkManifest(effSingletonId)
	sm.Caps = caps
	require.NoError(t, appReg.Register(&effApp{manifest: sm, svc: svc}))
	w.host = NewInst(appReg, zerolog.Nop())
	w.host.SetBus(w.bus)
	return
}

func (w *effWorld) close(t *testing.T) {
	t.Helper()
	w.host.CloseAll("test")
	w.host.reapClosed()
	_ = w.svc.Close(context.Background())
}

func (w *effWorld) openWindow(t *testing.T, id app.AppIdT) {
	t.Helper()
	k, err := w.host.Open(id)
	require.NoError(t, err)
	mountForTest(t, w.host, k)
	w.open = append(w.open, k)
}

func (w *effWorld) closeWindow(t *testing.T, i int) {
	t.Helper()
	k := w.open[i]
	w.open = append(w.open[:i], w.open[i+1:]...)
	w.host.Close(k, "test")
	w.host.reapClosed()
}

// summary is the observable effect state, keyed so two worlds compare:
// counts per app id rather than instance keys, which differ between hosts.
type effSummary struct {
	openFactory, openSingleton   int
	subsFactory, subsSingleton   int
	clientsFactory               int
	clientsSingletonMin          int // open windows
	clientsSingletonMax          int // open windows + one carried mounting window
	liveDatasets                 int
	registeredProviders          int
	factoryLiveDatasetsExpected  int
	singletonLiveDatasetExpected int
}

func (w *effWorld) summary(t *testing.T) (s effSummary) {
	t.Helper()
	w.svc.FlushRetracts()
	for _, k := range w.open {
		w.host.mu.Lock()
		var id app.AppIdT
		for _, win := range w.host.windows {
			if win.key == k {
				id = win.manifest.Id
			}
		}
		w.host.mu.Unlock()
		switch id {
		case effFactoryId:
			s.openFactory++
		case effSingletonId:
			s.openSingleton++
		}
	}
	for _, r := range w.bus.Subscriptions() {
		if strings.HasPrefix(r.Pattern, inprocbus.InboxPrefix) || !strings.HasPrefix(r.Pattern, "t.") {
			continue // service clients and inboxes are not the app's effects
		}
		switch r.AppId {
		case effFactoryId:
			s.subsFactory++
		case effSingletonId:
			s.subsSingleton++
		}
	}
	for _, c := range w.bus.LiveClients() {
		if c.AppId() == effFactoryId {
			s.clientsFactory++
		}
	}
	s.clientsSingletonMin = s.openSingleton
	s.clientsSingletonMax = s.openSingleton
	if s.openSingleton > 0 {
		s.clientsSingletonMax++
	}
	// Datasets: one per mounted factory instance, one for the singleton
	// while any of its windows is open.
	live := 0
	for _, name := range w.reg.Names() {
		if strings.HasPrefix(name, "adhoc_") {
			live++
		}
	}
	s.registeredProviders = live
	s.factoryLiveDatasetsExpected = s.openFactory
	if s.openSingleton > 0 {
		s.singletonLiveDatasetExpected = 1
	}
	s.liveDatasets = s.factoryLiveDatasetsExpected + s.singletonLiveDatasetExpected
	return
}

func (w *effWorld) check(t *testing.T, rt *rapid.T) {
	t.Helper()
	s := w.summary(t)
	if s.subsFactory != s.openFactory {
		rt.Fatalf("factory subscriptions %d != open factory windows %d (leak or loss)", s.subsFactory, s.openFactory)
	}
	wantSingleSubs := 0
	if s.openSingleton > 0 {
		wantSingleSubs = 1
	}
	if s.subsSingleton != wantSingleSubs {
		rt.Fatalf("singleton subscriptions %d != %d for %d open windows", s.subsSingleton, wantSingleSubs, s.openSingleton)
	}
	if s.clientsFactory != s.openFactory {
		rt.Fatalf("factory live clients %d != open windows %d", s.clientsFactory, s.openFactory)
	}
	singleClients := 0
	for _, c := range w.bus.LiveClients() {
		if c.AppId() == effSingletonId {
			singleClients++
		}
	}
	if singleClients < s.clientsSingletonMin || singleClients > s.clientsSingletonMax {
		rt.Fatalf("singleton live clients %d outside [%d,%d]", singleClients, s.clientsSingletonMin, s.clientsSingletonMax)
	}
	if s.registeredProviders != s.liveDatasets {
		rt.Fatalf("registered dataset providers %d != live datasets %d", s.registeredProviders, s.liveDatasets)
	}
}

type effMachine struct {
	t *testing.T
	w *effWorld
}

func (m *effMachine) openFactory(rt *rapid.T)   { m.w.openWindow(m.t, effFactoryId) }
func (m *effMachine) openSingleton(rt *rapid.T) { m.w.openWindow(m.t, effSingletonId) }
func (m *effMachine) closeAny(rt *rapid.T) {
	if len(m.w.open) == 0 {
		return
	}
	m.w.closeWindow(m.t, rapid.IntRange(0, len(m.w.open)-1).Draw(rt, "window"))
}
func (m *effMachine) republish(rt *rapid.T) {
	// A republish is a `published` event and a revision bump; it must not
	// change the live set.
	m.w.host.mu.Lock()
	var apps []*effApp
	for _, win := range m.w.host.windows {
		if a, ok := win.appInst.(*effApp); ok && a.handle != "" {
			apps = append(apps, a)
		}
	}
	m.w.host.mu.Unlock()
	if len(apps) == 0 {
		return
	}
	a := apps[rapid.IntRange(0, len(apps)-1).Draw(rt, "producer")]
	_, err := m.w.svc.Publish(adhocdata.PublishInput{
		Alias: "ds_re", Handle: a.handle, Publisher: string(a.manifest.Id), ArrowIPCStream: oneRowStream(),
	})
	require.NoError(m.t, err)
}
func (m *effMachine) check(rt *rapid.T) { m.w.check(m.t, rt) }

func TestClosingEdge_InterleavingLeavesNoTrace(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		w := newEffWorld(t)
		defer w.close(t)
		m := &effMachine{t: t, w: w}
		rt.Repeat(map[string]func(*rapid.T){
			"openFactory":   m.openFactory,
			"openSingleton": m.openSingleton,
			"closeAny":      m.closeAny,
			"republish":     m.republish,
			"":              m.check,
		})
		// Confluence: a fresh world given only the final configuration —
		// the same number of open windows per app, opened once, in
		// registration order — reaches the same observable state.
		final := w.summary(t)
		fresh := newEffWorld(t)
		defer fresh.close(t)
		for range final.openFactory {
			fresh.openWindow(t, effFactoryId)
		}
		for range final.openSingleton {
			fresh.openWindow(t, effSingletonId)
		}
		got := fresh.summary(t)
		if got.subsFactory != final.subsFactory || got.subsSingleton != final.subsSingleton ||
			got.clientsFactory != final.clientsFactory || got.registeredProviders != final.registeredProviders {
			rt.Fatalf("dynamic history left a trace: dynamic=%+v static=%+v", final, got)
		}
	})
}
