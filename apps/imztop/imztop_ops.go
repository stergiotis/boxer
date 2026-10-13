package imztop

import (
	"cmp"
	"context"
	"math"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmreplay"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// The operations catalog (ADR-0329, proposed). Queries read the published
// snapshot the frame drew and never the window's view (§SD1); commands set
// the view and never toggle (§SD2); the process-wide freeze and replay
// session are written only through the controlling window (§SD3).

const (
	resProcView      = "proc_view"
	resProcMapMetric = "procmap_metric"
	resTopologyDim   = "topology_dim"
	resInterface     = "interface"
	resLive          = "live"
	resReplay        = "replay"
	resPanel         = "panel"

	opGetOverview   = "get_overview"
	opListProcesses = "list_processes"
	opGetProcess    = "get_process"
	opGetHistory    = "get_history"
	opGetHardware   = "get_hardware"
	opGetView       = "get_view"
	opGetCoverage   = "get_coverage"

	opSetProcView      = "set_proc_view"
	opSetProcMapMetric = "set_procmap_metric"
	opSetTopologyDim   = "set_topology_dim"
	opSelectInterface  = "select_interface"
	opShowPanel        = "show_panel"

	opSetFrozen       = "set_frozen"
	opEnterReplay     = "enter_replay"
	opLeaveReplay     = "leave_replay"
	opSetReplayPaused = "set_replay_paused"
	opSetReplaySpeed  = "set_replay_speed"
	opSetReplayWindow = "set_replay_window"
)

// Result bounds (§SD1): what one inline result may carry.
const (
	opsMaxProcessRows   = 50
	opsDefaultProcRows  = 20
	opsMaxHistoryPoints = 120
	opsDefaultHistPts   = 60
	opsMaxCmdLen        = 256
)

const notControllerReason = "another imztop window controls freeze and replay; share that window instead"

// opsLiveSampler is the live sampler the catalog reads and freezes; a test
// replaces it.
var opsLiveSampler = ensureSampler

// controller is the window that writes the process-wide state (§SD3). The
// first window to draw claims it; it is released on Unmount and claimed by
// the next window that draws.
var controller atomic.Pointer[App]

func (inst *App) claimController() {
	controller.CompareAndSwap(nil, inst)
}

func (inst *App) releaseController() {
	controller.CompareAndSwap(inst, nil)
}

func (inst *App) isController() (ok bool) {
	return controller.Load() == inst
}

// replayValue is the replay resource's value: comparable, and without the
// playback position, which moves on its own and is not a write.
type replayValue struct {
	State  ReplayStateE
	FromMs int64
	ToMs   int64
	Paused bool
	Speed  float64
}

// latchProcessWide copies the process-wide state into the window, so that a
// change made between frames — another window's click, a replay open
// completing — appears inside this window's frame rather than before it
// (app.OperationsHandlerI.ResourceValue). Only the controller latches;
// every other window reports a fixed value.
func (inst *App) latchProcessWide() {
	if !inst.isController() {
		inst.latchedFrozen = false
		inst.latchedReplay = replayValue{}
		return
	}
	if s, err := opsLiveSampler(); err == nil {
		inst.latchedFrozen = s.IsPaused()
	}
	v := replayValue{State: CurrentReplayStatus().State}
	if session := ActiveReplay(); session != nil {
		w := session.Window()
		v.FromMs, v.ToMs = unixMsOrZero(w.From), unixMsOrZero(w.To)
		v.Paused = session.IsPaused()
		v.Speed = session.Speed()
	}
	inst.latchedReplay = v
}

func unixMsOrZero(t time.Time) (ms int64) {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// opsSnap is what queries read. pub is shared with the render path, which is
// safe because a PublishedSnapshot is never written after it is published.
type opsSnap struct {
	pub         *PublishedSnapshot
	replaying   bool
	frozen      bool
	intervalMs  int64
	view        procViewState
	procMetric  procMetricE
	topoDim     topoDimE
	ifaceIdx    int
	controller  bool
	replay      ReplayStatus
	replayErr   string
	replayWin   sysmreplay.Window
	replayPos   time.Time
	replayPosOk bool
	replayPause bool
	replaySpeed float64
	replayDone  bool
}

func (inst *App) opsSnapshot() (sn opsSnap) {
	sn.view = inst.procView
	sn.procMetric = inst.procMetric
	sn.topoDim = inst.topoDim
	sn.ifaceIdx = inst.netSelectedIfaceIdx
	sn.controller = inst.isController()
	sn.replay = CurrentReplayStatus()
	if sn.replay.Err != nil {
		sn.replayErr = sn.replay.Err.Error()
		sn.replay.Err = nil
	}
	if session := ActiveReplay(); session != nil {
		sn.replaying = true
		sn.pub = session.Latest()
		sn.replayWin = session.Window()
		sn.replayPos, sn.replayPosOk = session.Position()
		sn.replayPause = session.IsPaused()
		sn.replaySpeed = session.Speed()
		sn.replayDone = session.Exhausted()
		sn.intervalMs = session.Interval().Milliseconds()
		return
	}
	if s, err := opsLiveSampler(); err == nil {
		sn.pub = s.Latest()
		sn.frozen = s.IsPaused()
		sn.intervalMs = s.Interval().Milliseconds()
	}
	return
}

func (inst opsSnap) mode() (m string) {
	if inst.replaying {
		return "replay"
	}
	return "live"
}

// --- names the model sees ---

var procSortNames = []struct {
	key  ProcSortByE
	name string
}{
	{ProcSortByCPU, "cpu"}, {ProcSortByCPURaw, "cpu_raw"}, {ProcSortByMem, "mem"},
	{ProcSortByPID, "pid"}, {ProcSortByUser, "user"}, {ProcSortByName, "name"},
	{ProcSortByIORead, "io_read"}, {ProcSortByIOWrite, "io_write"},
}

func procSortName(by ProcSortByE) (name string) {
	for _, e := range procSortNames {
		if e.key == by {
			return e.name
		}
	}
	return ""
}

func parseProcSort(name string) (by ProcSortByE, ok bool) {
	if name == "" {
		return ProcSortByCPU, true
	}
	for _, e := range procSortNames {
		if e.name == name {
			return e.key, true
		}
	}
	return
}

func procSortNameList() (s string) {
	names := make([]string, 0, len(procSortNames))
	for _, e := range procSortNames {
		names = append(names, e.name)
	}
	return strings.Join(names, ", ")
}

// procSortDefaultDesc is the direction a sort key starts in: descending
// for measurements, ascending for identifiers.
func procSortDefaultDesc(by ProcSortByE) (desc bool) {
	return by != ProcSortByPID && by != ProcSortByUser && by != ProcSortByName
}

// parseOrder resolves "asc", "desc" or empty (the key's own direction).
func parseOrder(order string, by ProcSortByE) (desc bool, ok bool) {
	switch order {
	case "":
		return procSortDefaultDesc(by), true
	case "desc":
		return true, true
	case "asc":
		return false, true
	}
	return
}

func orderName(desc bool) (s string) {
	if desc {
		return "desc"
	}
	return "asc"
}

var procMetricNames = map[procMetricE]string{procMetricRSS: "rss", procMetricCPU: "cpu", procMetricIO: "io"}
var topoDimNames = map[topoDimE]string{topoDimLoad: "load", topoDimFreq: "freq"}

var panelTabs = []struct {
	name string
	tab  uint64
}{
	{"cpu", dockTabCPU}, {"topology", dockTabTopo}, {"proc_map", dockTabProcMap},
	{"pressure", dockTabPressure}, {"memory", dockTabMem}, {"battery", dockTabBattery},
	{"sensors", dockTabSensors}, {"disk", dockTabDisk}, {"network", dockTabNet},
	{"gpu", dockTabGPU}, {"processes", dockTabProc},
}

// --- argument and result types ---

type Overview struct {
	Mode            string     `desc:"live or replay"`
	Frozen          bool       `desc:"live data is frozen on the last sample"`
	SampledAtUnixMs int64      `desc:"when the sample was taken, epoch milliseconds; 0 before the first sample"`
	IntervalMs      int64      `desc:"observed time between samples"`
	CPUPercent      float64    `desc:"total CPU busy, 0 to 100"`
	LogicalCPUs     int        `desc:"logical CPUs in the sample"`
	LoadAvg1        float32    `desc:"1-minute load average"`
	LoadAvg5        float32    `desc:"5-minute load average"`
	LoadAvg15       float32    `desc:"15-minute load average"`
	MemUsedBytes    uint64     `desc:"memory in use"`
	MemTotalBytes   uint64     `desc:"memory installed"`
	SwapUsedBytes   uint64     `desc:"swap in use"`
	SwapTotalBytes  uint64     `desc:"swap configured"`
	DiskReadMiBs    float64    `desc:"block-device reads, MiB/s, summed over devices"`
	DiskWriteMiBs   float64    `desc:"block-device writes, MiB/s"`
	NetRxMiBs       float64    `desc:"network receive, MiB/s, summed over interfaces"`
	NetTxMiBs       float64    `desc:"network transmit, MiB/s"`
	PSIAvailable    bool       `desc:"pressure-stall information is present"`
	PSICPUSome10    float32    `desc:"share of time some task stalled on CPU, 10 s average, percent"`
	PSIMemSome10    float32    `desc:"same for memory"`
	PSIMemFull10    float32    `desc:"share of time all tasks stalled on memory, 10 s average, percent"`
	PSIIOSome10     float32    `desc:"same as cpu, for I/O"`
	PSIIOFull10     float32    `desc:"same as memory full, for I/O"`
	GPUs            []GPUState `desc:"per GPU"`
	Batteries       []Battery  `desc:"per battery"`
	Processes       int        `desc:"processes in the sample"`
	Errors          []string   `desc:"collectors that failed, as domain: message"`
	HistorySeries   []string   `desc:"series names get_history accepts"`
}

type GPUState struct {
	Name          string `desc:"device name"`
	BusyPercent   uint8  `desc:"busy, 0 to 100"`
	MemUsedBytes  uint64 `desc:"device memory in use"`
	MemTotalBytes uint64 `desc:"device memory"`
}

type Battery struct {
	Name    string `desc:"sysfs name, e.g. BAT0"`
	Percent uint8  `desc:"charge, 0 to 100"`
	State   string `desc:"charging, discharging, full, ..."`
}

type ListProcessesArgs struct {
	SortBy string `desc:"cpu (smoothed, default), cpu_raw, mem, pid, user, name, io_read, io_write"`
	Order  string `desc:"asc or desc; empty takes the key's own direction"`
	Filter string `desc:"case-insensitive substring of the name or command line; empty keeps all"`
	Limit  int    `desc:"rows to return, 1 to 50; 0 means 20"`
}

type ProcessRow struct {
	PID                uint32  `desc:"process id"`
	PPID               uint32  `desc:"parent process id"`
	Name               string  `desc:"executable name"`
	User               string  `desc:"owner"`
	State              string  `desc:"kernel state letter"`
	CPUPercent         float32 `desc:"CPU over the last interval; 100 is one core"`
	CPUSmoothed        float32 `desc:"exponentially smoothed CPU, same unit"`
	RSSBytes           uint64  `desc:"resident memory"`
	IOKnown            bool    `desc:"the I/O rates were measured"`
	IOReadBytesPerSec  uint64  `desc:"storage reads"`
	IOWriteBytesPerSec uint64  `desc:"storage writes"`
	Threads            int32   `desc:"thread count"`
	StartedAtUnixMs    int64   `desc:"start time, epoch milliseconds"`
	Cmd                string  `desc:"command line, truncated"`
}

type ListProcessesResult struct {
	Mode            string       `desc:"live or replay"`
	SampledAtUnixMs int64        `desc:"when the sample was taken"`
	Matched         int          `desc:"processes that pass the filter"`
	Rows            []ProcessRow `desc:"the first rows in the requested order"`
}

type GetProcessArgs struct {
	PID uint32 `desc:"process id"`
}

type GetProcessResult struct {
	Mode            string       `desc:"live or replay"`
	SampledAtUnixMs int64        `desc:"when the sample was taken"`
	Process         ProcessRow   `desc:"the process"`
	ParentName      string       `desc:"the parent's name; empty when the parent is not in the sample"`
	Children        []ProcessRow `desc:"direct children by smoothed CPU, at most 50"`
	ChildCount      int          `desc:"direct children in the sample"`
}

type GetHistoryArgs struct {
	Series string `desc:"a name from get_overview's history_series"`
	Points int    `desc:"downsampled points to return, 1 to 120; 0 means 60"`
}

type GetHistoryResult struct {
	Mode       string    `desc:"live or replay; in replay the history is the replayed window's"`
	Series     string    `desc:"the series"`
	Unit       string    `desc:"percent or MiB/s"`
	Samples    int       `desc:"samples in the window"`
	FromUnixMs int64     `desc:"first sample"`
	ToUnixMs   int64     `desc:"last sample"`
	Min        float64   `desc:"minimum"`
	Mean       float64   `desc:"mean"`
	P95        float64   `desc:"95th percentile"`
	Max        float64   `desc:"maximum"`
	Last       float64   `desc:"latest value"`
	StepMs     int64     `desc:"time each point covers"`
	Points     []float64 `desc:"bucket means, oldest first"`
}

type HardwareResult struct {
	Mode        string   `desc:"live or replay"`
	CPUModel    string   `desc:"CPU model name"`
	LogicalCPUs int32    `desc:"logical CPUs"`
	Packages    int      `desc:"CPU packages"`
	Cores       int      `desc:"physical cores"`
	NUMANodes   int      `desc:"NUMA nodes"`
	ActiveCPUs  []int32  `desc:"CPUs this cgroup may run on"`
	PerCoreMHz  []uint32 `desc:"current frequency per logical CPU"`
	Sensors     []Sensor `desc:"temperature sensors; not recorded, so empty in replay"`
	Container   string   `desc:"container engine the scraper runs under, if any"`
}

type Sensor struct {
	Name      string  `desc:"sensor label"`
	TempC     float32 `desc:"temperature, Celsius"`
	CriticalC float32 `desc:"critical threshold; 0 when unknown"`
}

type ViewResult struct {
	Mode          string     `desc:"live or replay"`
	Controller    bool       `desc:"this window may freeze and replay; another imztop window holds that otherwise"`
	Frozen        bool       `desc:"live data is frozen"`
	ProcSortBy    string     `desc:"process table sort key"`
	ProcOrder     string     `desc:"asc or desc"`
	ProcFilter    string     `desc:"process table filter"`
	ProcTree      bool       `desc:"process table nests children under parents"`
	ProcMapMetric string     `desc:"what a Proc Map box's area encodes: rss, cpu, io"`
	TopologyDim   string     `desc:"what the Topology tint encodes: load, freq"`
	Interface     string     `desc:"the network interface the Network panel shows"`
	Replay        ReplayView `desc:"the replay session"`
}

type ReplayView struct {
	State          string  `desc:"off, opening, on, failed"`
	Error          string  `desc:"why opening failed"`
	Host           string  `desc:"whose history is replayed"`
	Endpoint       string  `desc:"the ClickHouse server it comes from"`
	Empty          bool    `desc:"the window held no recorded samples"`
	FromUnixMs     int64   `desc:"window start; 0 is unbounded"`
	ToUnixMs       int64   `desc:"window end; 0 is unbounded"`
	PositionUnixMs int64   `desc:"where playback is; 0 before the first sample"`
	Paused         bool    `desc:"playback is paused"`
	Speed          float64 `desc:"playback speed"`
	Exhausted      bool    `desc:"playback reached the end of the window"`
}

type CoverageResult struct {
	FromUnixMs int64         `desc:"start of the range the runs were read over"`
	ToUnixMs   int64         `desc:"end of that range"`
	Runs       []CoverageRun `desc:"contiguous stretches with recorded samples"`
	Error      string        `desc:"why coverage could not be read"`
}

type CoverageRun struct {
	StartUnixMs int64 `desc:"run start"`
	EndUnixMs   int64 `desc:"run end"`
	Rows        int64 `desc:"recorded bundles"`
}

type SetProcViewArgs struct {
	SortBy string `desc:"cpu, cpu_raw, mem, pid, user, name, io_read, io_write; empty is cpu"`
	Order  string `desc:"asc or desc; empty takes the key's own direction"`
	Filter string `desc:"case-insensitive substring of name or command line; empty shows all"`
	Tree   bool   `desc:"nest children under parents"`
}

type SetProcMapMetricArgs struct {
	Metric string `desc:"rss, cpu or io"`
}

type SetTopologyDimArgs struct {
	Dim string `desc:"load or freq"`
}

type SelectInterfaceArgs struct {
	Name string `desc:"interface name, e.g. eth0"`
}

type ShowPanelArgs struct {
	Panel string `desc:"cpu, topology, proc_map, pressure, memory, battery, sensors, disk, network, gpu, processes"`
}

type SetFrozenArgs struct {
	Frozen bool `desc:"true holds the last live sample on screen"`
}

type ReplayWindowArgs struct {
	FromUnixMs int64 `desc:"window start, epoch milliseconds"`
	ToUnixMs   int64 `desc:"window end, epoch milliseconds; both 0 opens the last 10 minutes"`
}

type SetReplayPausedArgs struct {
	Paused bool `desc:"true pauses playback"`
}

type SetReplaySpeedArgs struct {
	Speed float64 `desc:"one of 0.5, 1, 2, 5, 10, 60"`
}

// --- query helpers ---

func processRow(p *sysmsnap.ProcInfo, smoothed float32) (r ProcessRow) {
	cmd := p.Cmd
	if len(cmd) > opsMaxCmdLen {
		cmd = cmd[:opsMaxCmdLen] + "…"
	}
	r = ProcessRow{PID: p.PID, PPID: p.PPID, Name: p.Name, User: p.User, CPUPercent: p.CPUPercent,
		CPUSmoothed: smoothed, RSSBytes: p.RSSBytes, IOKnown: p.IOKnown, IOReadBytesPerSec: p.IOReadBytesPerSec,
		IOWriteBytesPerSec: p.IOWriteBytesPerSec, Threads: p.NumThreads, StartedAtUnixMs: p.StartedAtUnixMs, Cmd: cmd}
	if p.State != 0 {
		r.State = string(rune(p.State))
	}
	return
}

func smoothedAt(pub *PublishedSnapshot, i int) (v float32) {
	if i < len(pub.ProcCPUSmoothed) {
		v = pub.ProcCPUSmoothed[i]
	}
	return
}

func lastOf(y []float64) (v float64) {
	if len(y) > 0 {
		v = y[len(y)-1]
	}
	return
}

// historySeries is one named series of a snapshot's fold.
type historySeries struct {
	name string
	unit string
	y    []float64
}

func historyOf(pub *PublishedSnapshot) (out []historySeries) {
	if pub == nil {
		return
	}
	out = []historySeries{
		{"cpu_total", "percent", pub.HistoryCPUTotal},
		{"mem_used", "percent", pub.HistoryMemUsed},
		{"disk_read", "MiB/s", pub.HistoryDiskRead},
		{"disk_write", "MiB/s", pub.HistoryDiskWrite},
		{"net_rx", "MiB/s", pub.HistoryNetRx},
		{"net_tx", "MiB/s", pub.HistoryNetTx},
	}
	if len(pub.HistoryBatteryPct) > 0 {
		out = append(out, historySeries{"battery", "percent", pub.HistoryBatteryPct})
	}
	for i, y := range pub.HistoryCPUPerCore {
		out = append(out, historySeries{"cpu_core:" + strconv.Itoa(i), "percent", y})
	}
	for i, y := range pub.HistoryGPUBusyPerDev {
		out = append(out, historySeries{"gpu_busy:" + strconv.Itoa(i), "percent", y})
	}
	named := func(prefix string, set []NamedSeries) {
		for _, s := range set {
			out = append(out, historySeries{prefix + s.Name, "MiB/s", s.Y})
		}
	}
	named("disk_read:", pub.HistoryDiskReadByDev)
	named("disk_write:", pub.HistoryDiskWriteByDev)
	named("net_rx:", pub.HistoryNetRxByIface)
	named("net_tx:", pub.HistoryNetTxByIface)
	return
}

// summarise reduces y to its statistics and n bucket means. times are the
// fold's sample times in seconds, aligned to y from the end: a keyed series
// that appeared late is shorter than the fold.
func summarise(y []float64, times []float64, n int) (r GetHistoryResult) {
	r.Samples = len(y)
	if len(y) == 0 {
		return
	}
	if k := len(times) - len(y); k >= 0 && len(times) > 0 {
		t := times[k:]
		r.FromUnixMs = int64(t[0] * 1000)
		r.ToUnixMs = int64(t[len(t)-1] * 1000)
	}
	sorted := slices.Clone(y)
	slices.Sort(sorted)
	r.Min, r.Max, r.Last = sorted[0], sorted[len(sorted)-1], y[len(y)-1]
	sum := 0.0
	for _, v := range y {
		sum += v
	}
	r.Mean = sum / float64(len(y))
	r.P95 = sorted[max(0, int(math.Ceil(0.95*float64(len(sorted))))-1)]
	n = min(n, len(y))
	r.Points = make([]float64, n)
	for b := range n {
		lo, hi := b*len(y)/n, (b+1)*len(y)/n
		s := 0.0
		for _, v := range y[lo:hi] {
			s += v
		}
		r.Points[b] = s / float64(hi-lo)
	}
	if span := r.ToUnixMs - r.FromUnixMs; span > 0 {
		r.StepMs = span / int64(n)
	}
	return
}

func countTopo(o *sysmsnap.TopoObject, kind sysmsnap.TopoKindE) (n int) {
	if o == nil {
		return
	}
	if o.Kind == kind {
		n++
	}
	for _, ch := range o.Children {
		n += countTopo(ch, kind)
	}
	return
}

func (inst opsSnap) ifaceName() (name string) {
	if inst.pub == nil || inst.pub.LatestNet == nil {
		return
	}
	ifs := inst.pub.LatestNet.Interfaces
	if inst.ifaceIdx >= 0 && inst.ifaceIdx < len(ifs) {
		name = ifs[inst.ifaceIdx].Name
	} else if len(ifs) > 0 {
		name = ifs[0].Name
	}
	return
}

// --- command cores, shared by the handlers and the direct gesture path ---

func (inst *App) applyProcView(v procViewState) {
	inst.procView = v
	// The filter box shows the draft; keep it on what was set, or the next
	// keystroke would commit the old text back.
	inst.procFilterDraft = v.Filter
}

func (inst *App) applyFrozen(frozen bool) {
	if s, err := opsLiveSampler(); err == nil {
		s.Pause(frozen)
	}
	inst.latchProcessWide()
}

func (inst *App) applyEnterReplay(w sysmreplay.Window) {
	if w.From.IsZero() && w.To.IsZero() {
		w = defaultReplayWindow()
	}
	StartReplay(context.Background(), w, StoreSourceOptions{})
	inst.latchProcessWide()
}

func (inst *App) applyLeaveReplay() {
	_ = LeaveReplay()
	inst.latchProcessWide()
}

func (inst *App) applyReplayPaused(paused bool) {
	if session := ActiveReplay(); session != nil {
		session.Pause(paused)
	}
	inst.latchProcessWide()
}

func (inst *App) applyReplaySpeed(speed float64) {
	if i := slices.Index(replaySpeeds, speed); i >= 0 {
		replaySpeedIdx = i
	}
	if session := ActiveReplay(); session != nil {
		session.SetSpeed(speed)
	}
	inst.latchProcessWide()
}

func (inst *App) applyReplayWindow(from, to time.Time) {
	if session := ActiveReplay(); session != nil {
		session.SeekWindow(from, to)
	}
	inst.latchProcessWide()
}

// gestureOr routes the person's control through the catalog (ADR-0329
// §SD5), so the command log sees it as theirs, and applies it directly where
// no host serves the catalog or the window may not run it — a click in a
// window that is not the controller still acts, as it always has.
func gestureOr[In any](inst *App, op string, in In, direct func()) {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[In, appops.None](inst.frameCtx, op, in); err == nil {
			return
		}
	}
	direct()
}

// clickHouseDestination is how a grant names the server replay reads from
// (ADR-0329 §SD4), spelled as play spells an endpoint.
func clickHouseDestination() (dest string) {
	raw := chclient.ConfigFromEnv().URL
	host := strings.TrimSpace(raw)
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		host = u.Host
	}
	return "clickhouse:" + host
}

func replayWindowArgs(in ReplayWindowArgs) (w sysmreplay.Window, err error) {
	if in.FromUnixMs == 0 && in.ToUnixMs == 0 {
		return
	}
	if in.FromUnixMs <= 0 || in.ToUnixMs <= in.FromUnixMs {
		err = app.RefuseOperation("from_unix_ms must be positive and before to_unix_ms")
		return
	}
	w = sysmreplay.Window{From: time.UnixMilli(in.FromUnixMs).UTC(), To: time.UnixMilli(in.ToUnixMs).UTC()}
	return
}

func refuseUnlessController(inst *App) (err error) {
	if !inst.isController() {
		err = app.RefuseOperation(notControllerReason)
	}
	return
}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet((*App).opsSnapshot)

	s.Resource(resProcView, "the process table's sort key, direction, filter and tree toggle",
		func(inst *App) any { return inst.procView })
	s.Restorable(resProcView, func(inst *App, v any) bool {
		pv, ok := v.(procViewState)
		if ok {
			inst.applyProcView(pv)
		}
		return ok
	})
	s.Editing(resProcView, func(inst *App) bool { return appops.WidgetEditing(inst.procFilterH) })
	s.Resource(resProcMapMetric, "what a Proc Map box's area encodes", func(inst *App) any { return inst.procMetric })
	s.Restorable(resProcMapMetric, func(inst *App, v any) bool {
		m, ok := v.(procMetricE)
		if ok {
			inst.procMetric = m
		}
		return ok
	})
	s.Resource(resTopologyDim, "what the Topology tint encodes", func(inst *App) any { return inst.topoDim })
	s.Restorable(resTopologyDim, func(inst *App, v any) bool {
		d, ok := v.(topoDimE)
		if ok {
			inst.topoDim = d
		}
		return ok
	})
	s.Resource(resInterface, "the network interface the Network panel shows", func(inst *App) any { return inst.netSelectedIfaceIdx })
	s.Restorable(resInterface, func(inst *App, v any) bool {
		i, ok := v.(int)
		if ok {
			inst.netSelectedIfaceIdx = i
		}
		return ok
	})
	s.Resource(resPanel, "the panel show_panel last brought to the front; the person's tab clicks are not tracked",
		func(inst *App) any { return inst.shownPanel })
	s.Resource(resLive, "live data frozen or flowing; process-wide, written through the controlling window",
		func(inst *App) any { return inst.latchedFrozen })
	s.Resource(resReplay, "the replay session and its transport; process-wide, written through the controlling window",
		func(inst *App) any { return inst.latchedReplay })

	// --- queries ---

	appops.Query(s, app.OperationSpec{Name: opGetOverview, Version: 1,
		Summary: "read the latest sample: CPU, memory, disk and network rates, pressure, GPUs, batteries",
		Agents:  true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out Overview, err error) {
			out.Mode, out.Frozen, out.IntervalMs = sn.mode(), sn.frozen, sn.intervalMs
			pub := sn.pub
			if pub == nil {
				return
			}
			out.SampledAtUnixMs = pub.SampledAtUnixMs
			if cpu := pub.LatestCPU; cpu != nil {
				out.CPUPercent = float64(cpu.TotalPercent)
				out.LogicalCPUs = len(cpu.PerCorePercent)
				out.LoadAvg1, out.LoadAvg5, out.LoadAvg15 = cpu.LoadAvg1, cpu.LoadAvg5, cpu.LoadAvg15
			}
			if m := pub.LatestMem; m != nil {
				out.MemUsedBytes, out.MemTotalBytes = m.UsedBytes, m.TotalBytes
				out.SwapUsedBytes, out.SwapTotalBytes = m.SwapUsedBytes, m.SwapTotalBytes
			}
			out.DiskReadMiBs, out.DiskWriteMiBs = lastOf(pub.HistoryDiskRead), lastOf(pub.HistoryDiskWrite)
			out.NetRxMiBs, out.NetTxMiBs = lastOf(pub.HistoryNetRx), lastOf(pub.HistoryNetTx)
			if p := pub.LatestPSI; p != nil && p.Available {
				out.PSIAvailable = true
				out.PSICPUSome10 = p.CPU.Some.Avg10
				out.PSIMemSome10, out.PSIMemFull10 = p.Memory.Some.Avg10, p.Memory.Full.Avg10
				out.PSIIOSome10, out.PSIIOFull10 = p.IO.Some.Avg10, p.IO.Full.Avg10
			}
			if g := pub.LatestGPU; g != nil {
				for _, d := range g.Devices {
					out.GPUs = append(out.GPUs, GPUState{Name: d.Name, BusyPercent: d.BusyPercent,
						MemUsedBytes: d.MemoryUsedBytes, MemTotalBytes: d.MemoryTotalBytes})
				}
			}
			if b := pub.LatestBattery; b != nil {
				for _, bs := range b.Batteries {
					out.Batteries = append(out.Batteries, Battery{Name: bs.Name, Percent: bs.Percent, State: bs.State.String()})
				}
			}
			out.Processes = len(pub.Procs)
			for d, e := range pub.Errors {
				if e != nil {
					out.Errors = append(out.Errors, string(d)+": "+e.Error())
				}
			}
			sort.Strings(out.Errors)
			for _, h := range historyOf(pub) {
				out.HistorySeries = append(out.HistorySeries, h.name)
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opListProcesses, Version: 1,
		Summary: "list processes from the latest sample, sorted and filtered as asked, at most 50",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in ListProcessesArgs) (out ListProcessesResult, err error) {
			by, ok := parseProcSort(in.SortBy)
			if !ok {
				err = app.RefuseOperation("sort_by is one of " + procSortNameList())
				return
			}
			desc, ok := parseOrder(in.Order, by)
			if !ok {
				err = app.RefuseOperation("order is asc, desc or empty")
				return
			}
			limit := in.Limit
			if limit <= 0 {
				limit = opsDefaultProcRows
			}
			limit = min(limit, opsMaxProcessRows)
			out.Mode = sn.mode()
			if sn.pub == nil {
				return
			}
			out.SampledAtUnixMs = sn.pub.SampledAtUnixMs
			infos, smoothed := applyProcView(sn.pub.Procs, sn.pub.ProcCPUSmoothed,
				procViewState{SortBy: by, Desc: desc, Filter: in.Filter})
			out.Matched = len(infos)
			for i := range min(limit, len(infos)) {
				var sm float32
				if i < len(smoothed) {
					sm = smoothed[i]
				}
				out.Rows = append(out.Rows, processRow(&infos[i], sm))
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetProcess, Version: 1,
		Summary: "read one process from the latest sample, with its parent's name and its children",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetProcessArgs) (out GetProcessResult, err error) {
			out.Mode = sn.mode()
			pub := sn.pub
			if pub == nil {
				err = app.RefuseOperation("no sample has arrived yet")
				return
			}
			out.SampledAtUnixMs = pub.SampledAtUnixMs
			found := false
			type child struct {
				i  int
				sm float32
			}
			var kids []child
			for i := range pub.Procs {
				p := &pub.Procs[i]
				switch {
				case p.PID == in.PID:
					out.Process, found = processRow(p, smoothedAt(pub, i)), true
				case p.PPID == in.PID && in.PID != 0:
					kids = append(kids, child{i, smoothedAt(pub, i)})
				}
			}
			if !found {
				err = app.RefuseOperation("process " + strconv.FormatUint(uint64(in.PID), 10) + " is not in the latest sample")
				return
			}
			for i := range pub.Procs {
				if pub.Procs[i].PID == out.Process.PPID {
					out.ParentName = pub.Procs[i].Name
					break
				}
			}
			slices.SortStableFunc(kids, func(a, b child) int { return cmp.Compare(b.sm, a.sm) })
			out.ChildCount = len(kids)
			for _, k := range kids[:min(len(kids), opsMaxProcessRows)] {
				out.Children = append(out.Children, processRow(&pub.Procs[k.i], k.sm))
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetHistory, Version: 1,
		Summary: "summarise one series over the in-memory window (about 10 minutes): min, mean, p95, max, last, and up to 120 points",
		Agents:  true, Fork: app.OperationForkReadShared,
		Follows: []string{"older history is SQL over boxer.facts, not this operation"}},
		func(sn opsSnap, in GetHistoryArgs) (out GetHistoryResult, err error) {
			n := in.Points
			if n <= 0 {
				n = opsDefaultHistPts
			}
			n = min(n, opsMaxHistoryPoints)
			series := historyOf(sn.pub)
			i := slices.IndexFunc(series, func(h historySeries) bool { return h.name == in.Series })
			if i < 0 {
				names := make([]string, 0, len(series))
				for _, h := range series {
					names = append(names, h.name)
				}
				err = app.RefuseOperation("series is one of: " + strings.Join(names, ", "))
				return
			}
			out = summarise(series[i].y, sn.pub.HistoryTimeUnixSec, n)
			out.Mode, out.Series, out.Unit = sn.mode(), series[i].name, series[i].unit
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetHardware, Version: 1,
		Summary: "read the CPU topology, per-core frequencies, temperature sensors and container",
		Agents:  true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out HardwareResult, err error) {
			out.Mode = sn.mode()
			pub := sn.pub
			if pub == nil {
				return
			}
			if cpu := pub.LatestCPU; cpu != nil {
				out.CPUModel = cpu.ModelName
				out.ActiveCPUs = cpu.ActiveCPUs
				out.PerCoreMHz = cpu.PerCoreFreqMHz
			}
			if t := pub.Topology; t != nil {
				out.LogicalCPUs = t.LogicalCount
				out.Packages = countTopo(t.Root, sysmsnap.TopoKindPackage)
				out.Cores = countTopo(t.Root, sysmsnap.TopoKindCore)
				out.NUMANodes = countTopo(t.Root, sysmsnap.TopoKindNUMANode)
			}
			for _, r := range pub.Sensors {
				out.Sensors = append(out.Sensors, Sensor{Name: r.Name, TempC: r.TempC, CriticalC: r.CriticalC})
			}
			if ct := pub.LatestContainer; ct != nil {
				out.Container = ct.Engine.String()
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetView, Version: 1,
		Summary: "read what this window shows: process table view, panel options, freeze, and the replay session",
		Reads:   []string{resProcView, resProcMapMetric, resTopologyDim, resInterface, resLive, resReplay},
		Agents:  true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out ViewResult, err error) {
			out.Mode, out.Controller, out.Frozen = sn.mode(), sn.controller, sn.frozen
			out.ProcSortBy, out.ProcOrder = procSortName(sn.view.SortBy), orderName(sn.view.Desc)
			out.ProcFilter, out.ProcTree = sn.view.Filter, sn.view.Tree
			out.ProcMapMetric, out.TopologyDim = procMetricNames[sn.procMetric], topoDimNames[sn.topoDim]
			out.Interface = sn.ifaceName()
			out.Replay = ReplayView{State: sn.replay.State.String(), Error: sn.replayErr, Host: sn.replay.Host,
				Endpoint: sn.replay.Endpoint, Empty: sn.replay.Empty}
			if sn.replaying {
				out.Replay.FromUnixMs, out.Replay.ToUnixMs = unixMsOrZero(sn.replayWin.From), unixMsOrZero(sn.replayWin.To)
				if sn.replayPosOk {
					out.Replay.PositionUnixMs = sn.replayPos.UnixMilli()
				}
				out.Replay.Paused, out.Replay.Speed, out.Replay.Exhausted = sn.replayPause, sn.replaySpeed, sn.replayDone
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetCoverage, Version: 1,
		Summary: "read where recorded history exists, as the replay availability strip loaded it",
		Agents:  true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out CoverageResult, err error) {
			runs, from, to, cErr := coverageSnapshot()
			out.FromUnixMs, out.ToUnixMs = unixMsOrZero(from), unixMsOrZero(to)
			if cErr != nil {
				out.Error = cErr.Error()
			}
			for _, r := range runs {
				out.Runs = append(out.Runs, CoverageRun{StartUnixMs: r.StartMS, EndUnixMs: r.EndMS, Rows: r.Rows})
			}
			return
		})
	s.Available(opGetCoverage, func(sn opsSnap) (bool, string) {
		return sn.replaying, "coverage is loaded while replay is on; call enter_replay first"
	})

	// --- view commands (§SD2) ---

	appops.Command(s, app.OperationSpec{Name: opSetProcView, Version: 1,
		Summary: "set the process table's sort key, direction, filter and tree mode",
		Effect:  app.OperationEffectView, Writes: []string{resProcView}, Agents: true,
		Gesture: "a column header, the filter box, the tree button", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetProcViewArgs) (out appops.None, err error) {
			by, ok := parseProcSort(in.SortBy)
			if !ok {
				err = app.RefuseOperation("sort_by is one of " + procSortNameList())
				return
			}
			desc, ok := parseOrder(in.Order, by)
			if !ok {
				err = app.RefuseOperation("order is asc, desc or empty")
				return
			}
			inst.applyProcView(procViewState{SortBy: by, Desc: desc, Filter: in.Filter, Tree: in.Tree})
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetProcMapMetric, Version: 1,
		Summary: "set what a Proc Map box's area encodes",
		Effect:  app.OperationEffectView, Writes: []string{resProcMapMetric}, Agents: true,
		Gesture: "the Proc Map area combo", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetProcMapMetricArgs) (out appops.None, err error) {
			for m, name := range procMetricNames {
				if name == in.Metric {
					inst.procMetric = m
					return
				}
			}
			err = app.RefuseOperation("metric is rss, cpu or io")
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetTopologyDim, Version: 1,
		Summary: "set what the Topology tint encodes",
		Effect:  app.OperationEffectView, Writes: []string{resTopologyDim}, Agents: true,
		Gesture: "the Topology tint combo", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetTopologyDimArgs) (out appops.None, err error) {
			for d, name := range topoDimNames {
				if name == in.Dim {
					inst.topoDim = d
					return
				}
			}
			err = app.RefuseOperation("dim is load or freq")
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSelectInterface, Version: 1,
		Summary: "select the network interface the Network panel shows",
		Effect:  app.OperationEffectView, Writes: []string{resInterface}, Agents: true,
		Gesture: "the Network interface combo", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SelectInterfaceArgs) (out appops.None, err error) {
			pub := inst.opsSnapshot().pub
			if pub != nil && pub.LatestNet != nil {
				for i, ifc := range pub.LatestNet.Interfaces {
					if ifc.Name == in.Name {
						inst.netSelectedIfaceIdx = i
						return
					}
				}
			}
			err = app.RefuseOperation("interface " + strconv.Quote(in.Name) + " is not in the latest sample")
			return
		})

	appops.Command(s, app.OperationSpec{Name: opShowPanel, Version: 1,
		Summary: "bring a panel's dock tab to the front",
		Effect:  app.OperationEffectView, Writes: []string{resPanel}, Agents: true,
		Gesture: "clicking a dock tab", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in ShowPanelArgs) (out appops.None, err error) {
			for _, p := range panelTabs {
				if p.name == in.Panel {
					inst.pendingTab, inst.shownPanel = p.tab, p.name
					return
				}
			}
			names := make([]string, 0, len(panelTabs))
			for _, p := range panelTabs {
				names = append(names, p.name)
			}
			err = app.RefuseOperation("panel is one of: " + strings.Join(names, ", "))
			return
		})

	// --- process-wide commands, controller only (§SD3) ---

	controllerOnly := func(name string, extra func(sn opsSnap) (bool, string)) {
		s.Available(name, func(sn opsSnap) (bool, string) {
			if !sn.controller {
				return false, notControllerReason
			}
			if extra != nil {
				return extra(sn)
			}
			return true, ""
		})
	}
	replayOn := func(sn opsSnap) (bool, string) {
		return sn.replaying, "replay is not on"
	}

	appops.Command(s, app.OperationSpec{Name: opSetFrozen, Version: 1,
		Summary: "freeze live data on the last sample, or let it flow again; every imztop window follows",
		Effect:  app.OperationEffectView, Writes: []string{resLive}, Agents: true,
		Gesture: "the Freeze / Go live button", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetFrozenArgs) (out appops.None, err error) {
			if err = refuseUnlessController(inst); err != nil {
				return
			}
			inst.applyFrozen(in.Frozen)
			return
		})
	controllerOnly(opSetFrozen, func(sn opsSnap) (bool, string) {
		return !sn.replaying, "replay is on; use set_replay_paused"
	})

	appops.Command(s, app.OperationSpec{Name: opEnterReplay, Version: 1,
		Summary: "open a replay of recorded history over a window; every imztop window follows",
		Effect:  app.OperationEffectView, Writes: []string{resReplay}, Agents: true,
		Gesture: "the Replay button", Rebuild: app.OperationRebuildVolatile,
		Follows: []string{"the session opens in the background; get_view reports opening, on or failed",
			"an agent's call needs the ClickHouse server holding boxer.facts in its grant"}},
		func(inst *App, call app.OperationCall, in ReplayWindowArgs) (out appops.None, err error) {
			if err = refuseUnlessController(inst); err != nil {
				return
			}
			w, err := replayWindowArgs(in)
			if err != nil {
				return
			}
			if obo := call.OnBehalfOf; obo != nil {
				if dest := clickHouseDestination(); !slices.Contains(obo.Destinations, dest) {
					err = app.RefuseForDestinations("replay reads recorded history from "+dest, dest)
					return
				}
			}
			inst.applyEnterReplay(w)
			return
		})
	controllerOnly(opEnterReplay, func(sn opsSnap) (bool, string) {
		st := sn.replay.State
		return st == ReplayOff || st == ReplayFailed, "replay is already " + st.String()
	})

	appops.Command(s, app.OperationSpec{Name: opLeaveReplay, Version: 1,
		Summary: "end replay and return every imztop window to live data",
		Effect:  app.OperationEffectView, Writes: []string{resReplay}, Agents: true,
		Gesture: "the Go live, Cancel or Dismiss button", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, _ appops.None) (out appops.None, err error) {
			if err = refuseUnlessController(inst); err != nil {
				return
			}
			inst.applyLeaveReplay()
			return
		})
	controllerOnly(opLeaveReplay, func(sn opsSnap) (bool, string) {
		return sn.replay.State != ReplayOff, "replay is off"
	})

	appops.Command(s, app.OperationSpec{Name: opSetReplayPaused, Version: 1,
		Summary: "pause or resume replay playback",
		Effect:  app.OperationEffectView, Writes: []string{resReplay}, Agents: true,
		Gesture: "the Play / Pause button", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetReplayPausedArgs) (out appops.None, err error) {
			if err = refuseUnlessController(inst); err != nil {
				return
			}
			inst.applyReplayPaused(in.Paused)
			return
		})
	controllerOnly(opSetReplayPaused, replayOn)

	appops.Command(s, app.OperationSpec{Name: opSetReplaySpeed, Version: 1,
		Summary: "set replay playback speed",
		Effect:  app.OperationEffectView, Writes: []string{resReplay}, Agents: true,
		Gesture: "the speed combo", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetReplaySpeedArgs) (out appops.None, err error) {
			if err = refuseUnlessController(inst); err != nil {
				return
			}
			if !slices.Contains(replaySpeeds, in.Speed) {
				err = app.RefuseOperation("speed is one of 0.5, 1, 2, 5, 10, 60")
				return
			}
			inst.applyReplaySpeed(in.Speed)
			return
		})
	controllerOnly(opSetReplaySpeed, replayOn)

	appops.Command(s, app.OperationSpec{Name: opSetReplayWindow, Version: 1,
		Summary: "move replay to another window of recorded history and play it from the start",
		Effect:  app.OperationEffectView, Writes: []string{resReplay}, Agents: true,
		Gesture: "the earlier / later buttons", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in ReplayWindowArgs) (out appops.None, err error) {
			if err = refuseUnlessController(inst); err != nil {
				return
			}
			if in.FromUnixMs == 0 && in.ToUnixMs == 0 {
				err = app.RefuseOperation("give from_unix_ms and to_unix_ms")
				return
			}
			w, err := replayWindowArgs(in)
			if err != nil {
				return
			}
			inst.applyReplayWindow(w.From, w.To)
			return
		})
	controllerOnly(opSetReplayWindow, replayOn)
	return
}()

// Operations serves this window's catalog (app.OperationsAppI).
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

var _ app.OperationsAppI = (*App)(nil)
