package imztop

import (
	"testing"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

func sampleProcs() (infos []sysmsnap.ProcInfo, smoothed []float32) {
	infos = procs(
		sysmsnap.ProcInfo{PID: 1, Name: "systemd", Cmd: "/sbin/init", CPUPercent: 1, RSSBytes: 100},
		sysmsnap.ProcInfo{PID: 2, Name: "postgres", Cmd: "postgres -D /var", CPUPercent: 9, RSSBytes: 900},
		sysmsnap.ProcInfo{PID: 3, Name: "bash", Cmd: "-bash", CPUPercent: 5, RSSBytes: 300},
	)
	smoothed = []float32{1, 9, 5}
	return
}

func snapOf(infos []sysmsnap.ProcInfo, smoothed []float32) (out *PublishedSnapshot) {
	return &PublishedSnapshot{Procs: infos, ProcCPUSmoothed: smoothed, SampledAtUnixMs: 1}
}

// Regression, 2026-07-24 review: sort key, filter and tree toggle were
// package globals, so two imztop windows shared them — contradicting the
// App doc, which states each Open() gets independent UI state. The tour
// wrote the same globals, so running it changed a live window's filter.
func TestProcViewIsPerWindow(t *testing.T) {
	a, b := newApp(), newApp()

	a.setProcFilter("postgres")
	a.setProcSort(ProcSortByMem)
	a.toggleProcTree()

	if got := b.procView.Filter; got != "" {
		t.Errorf("second window saw the first window's filter: %q", got)
	}
	if b.procView.SortBy != ProcSortByCPU {
		t.Errorf("second window saw the first window's sort key: %v", b.procView.SortBy)
	}
	if b.procView.Tree {
		t.Error("second window saw the first window's tree toggle")
	}
	if a.procView.Filter != "postgres" || a.procView.SortBy != ProcSortByMem || !a.procView.Tree {
		t.Errorf("first window lost its own state: %+v", a.procView)
	}
}

func TestNewAppStartsOnTheDefaultView(t *testing.T) {
	got := newApp().procView
	if got != defaultProcView() {
		t.Fatalf("procView = %+v, want %+v", got, defaultProcView())
	}
	if got.SortBy != ProcSortByCPU || !got.Desc {
		t.Fatalf("default view should sort by smoothed CPU, descending: %+v", got)
	}
}

// Repeating a sort key flips the direction; a new key picks its own
// natural direction. This behaviour moved from a package function to a
// method and must be unchanged.
func TestSetProcSortTogglesDirection(t *testing.T) {
	inst := newApp()
	inst.setProcSort(ProcSortByMem)
	if !inst.procView.Desc {
		t.Error("numeric column should start descending")
	}
	inst.setProcSort(ProcSortByMem)
	if inst.procView.Desc {
		t.Error("repeating the key should flip direction")
	}
	inst.setProcSort(ProcSortByName)
	if inst.procView.Desc {
		t.Error("name column should start ascending")
	}
}

// The published slices are shared by every window, so applying a view must
// not write through them. The filter used to compact in place into the
// input's backing array, which was safe only while the Sampler owned the
// slices outright.
func TestApplyProcViewDoesNotMutateItsInputs(t *testing.T) {
	infos, smoothed := sampleProcs()
	origInfos := append([]sysmsnap.ProcInfo(nil), infos...)
	origSmoothed := append([]float32(nil), smoothed...)

	out, outSm := applyProcView(infos, smoothed, procViewState{Filter: "postgres", SortBy: ProcSortByMem, Desc: true})
	if len(out) != 1 || out[0].Name != "postgres" {
		t.Fatalf("filter produced %+v", out)
	}
	if len(outSm) != 1 || outSm[0] != 9 {
		t.Fatalf("smoothed slice lost alignment: %v", outSm)
	}

	for i := range origInfos {
		if infos[i].PID != origInfos[i].PID || infos[i].Name != origInfos[i].Name {
			t.Fatalf("input mutated at %d: %+v, want %+v", i, infos[i], origInfos[i])
		}
	}
	for i := range origSmoothed {
		if smoothed[i] != origSmoothed[i] {
			t.Fatalf("input smoothed mutated at %d: %v, want %v", i, smoothed[i], origSmoothed[i])
		}
	}
}

// Two windows filtering the same snapshot differently must each see their
// own result — the sharp end of the in-place filtering hazard.
func TestTwoWindowsFilterTheSameSnapshotIndependently(t *testing.T) {
	infos, smoothed := sampleProcs()
	snap := snapOf(infos, smoothed)

	a, b := newApp(), newApp()
	a.setProcFilter("postgres")
	b.setProcFilter("bash")

	gotA, _ := a.viewProcs(snap)
	gotB, _ := b.viewProcs(snap)

	if len(gotA) != 1 || gotA[0].Name != "postgres" {
		t.Errorf("window A saw %+v", gotA)
	}
	if len(gotB) != 1 || gotB[0].Name != "bash" {
		t.Errorf("window B saw %+v", gotB)
	}
	if len(snap.Procs) != 3 {
		t.Errorf("the shared snapshot was rewritten: %d procs left", len(snap.Procs))
	}
}

// Applying the view moved from once per sample to once per frame, so the
// memo is what keeps the cost where it was.
func TestViewProcsMemoisesPerSnapshotAndView(t *testing.T) {
	infos, smoothed := sampleProcs()
	snap := snapOf(infos, smoothed)
	inst := newApp()

	first, _ := inst.viewProcs(snap)
	second, _ := inst.viewProcs(snap)
	if &first[0] != &second[0] {
		t.Error("same snapshot and view should reuse the cached result")
	}

	inst.setProcFilter("bash")
	afterView, _ := inst.viewProcs(snap)
	if len(afterView) != 1 {
		t.Fatalf("view change was not applied: %+v", afterView)
	}

	next := snapOf(infos, smoothed)
	next.SampledAtUnixMs = 2
	afterSnap, _ := inst.viewProcs(next)
	if len(afterSnap) != 1 || afterSnap[0].Name != "bash" {
		t.Fatalf("new snapshot lost the window's view: %+v", afterSnap)
	}
	if len(first) != 3 {
		t.Error("the earlier result was rewritten in place")
	}
}

func TestViewProcsHandlesNilSnapshot(t *testing.T) {
	infos, smoothed := newApp().viewProcs(nil)
	if infos != nil || smoothed != nil {
		t.Fatalf("nil snapshot should yield nothing, got %v / %v", infos, smoothed)
	}
}

// A filter matching nothing is distinct from no filter at all.
func TestFilterMatchingNothingYieldsNoRows(t *testing.T) {
	infos, smoothed := sampleProcs()
	inst := newApp()
	inst.setProcFilter("no-such-process")
	got, gotSm := inst.viewProcs(snapOf(infos, smoothed))
	if len(got) != 0 || len(gotSm) != 0 {
		t.Fatalf("expected no rows, got %+v / %v", got, gotSm)
	}
}

// Sorting by smoothed CPU keys off the smoothed slice, which has to stay
// index-aligned with the infos it was reordered alongside.
func TestSortKeepsSmoothedAligned(t *testing.T) {
	infos, smoothed := sampleProcs()
	inst := newApp() // defaults: smoothed CPU, descending
	got, gotSm := inst.viewProcs(snapOf(infos, smoothed))
	if len(got) != 3 || len(gotSm) != 3 {
		t.Fatalf("got %d rows / %d smoothed", len(got), len(gotSm))
	}
	want := []string{"postgres", "bash", "systemd"} // smoothed 9, 5, 1
	for i, name := range want {
		if got[i].Name != name {
			t.Fatalf("row %d = %s, want %s", i, got[i].Name, name)
		}
	}
	for i, p := range got {
		var src float32
		switch p.Name {
		case "postgres":
			src = 9
		case "bash":
			src = 5
		case "systemd":
			src = 1
		}
		if gotSm[i] != src {
			t.Fatalf("row %d (%s) smoothed = %v, want %v", i, p.Name, gotSm[i], src)
		}
	}
}
