// Command wasmspike is the Go frame producer of the keelson-wasm-frame-cost
// trial (ADR-0077 SD2, the Phase-0 spike). It runs the unmodified imzero2
// application layer — the same [application.Application], bindings and
// StateManager.Sync the desktop host uses — against a peer that interprets
// nothing (rust/fffi2stub), and reports what one Go frame costs on each
// target: native, GOOS=js and GOOS=wasip1.
//
// Two consumers:
//   - pipe: FFFI2 over fd 0/1, exactly as on the desktop. Natively the peer is
//     the fffi2stub binary behind a pipe; under wasm the host's JS shim hands
//     each write to the fffi2stub wasm module synchronously and serves its
//     replies to the next read — the in-page bridge of ADR-0077 SD1.
//   - inproc: an in-process channel that answers fetchers from memory, so the
//     Go emission cost is measured with no transport at all.
//
// Two scenes: `gallery` renders every registry demo linked into this binary
// (the ones that compile for wasm), each on its own stage; `labels` is a
// synthetic grid whose row count sets the bytes per frame.
//
// The fetcher table the stub needs is derived from the generated bindings by
// `-dumpFetchTable`, run natively; the measured runs receive it as text.
//
// stdout is the FFFI2 data channel; every report goes to stderr as one line
// prefixed `RESULT `.
package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"iter"
	"os"
	"path/filepath"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	"github.com/stergiotis/boxer/public/thestack/imzero2/application"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/metrics"

	// The registry demos that compile for wasm and emit the same frame on
	// every target; each registers in init. sccmap compiles too but reads
	// the filesystem, so its frame differs between native and wasm.
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/idsshowcase"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/leewaywidgets"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/logdemo"

	// A real app that is a pure front end (no bus, no store) and registers
	// its own tour demos: the browser demonstrator's first application.
	_ "github.com/stergiotis/boxer/apps/fibscope"
)

// ---- fetch table -----------------------------------------------------------

// fetchKinds maps a generated Fetcher read helper to the stub's reply kind.
var fetchKinds = map[string]string{
	"readB": "b", "readU8": "u8", "readU32": "u32", "readU64": "u64",
	"readI64": "i64", "readF32": "f32", "readF64": "f64",
}

// parseOpcodes reads every FuncProcId constant from the generated enums file.
func parseOpcodes(fset *token.FileSet, bindingsDir string) (opcodes map[string]int, err error) {
	enums, err := parser.ParseFile(fset, filepath.Join(bindingsDir, "enums.out.go"), nil, 0)
	if err != nil {
		return
	}
	opcodes = map[string]int{}
	for _, d := range enums.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, sp := range gd.Specs {
			vs := sp.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				be, ok := vs.Values[i].(*ast.BinaryExpr)
				if !ok {
					continue
				}
				lit, ok := be.Y.(*ast.BasicLit)
				if !ok {
					continue
				}
				n, _ := strconv.Atoi(lit.Value)
				opcodes[name.Name] = n + int(c.FuncProcIdOffset)
			}
		}
	}
	return
}

// dumpOpcodes prints `<opcode> <name>` for every FuncProcId, the table the
// stub's pass-through mode uses to name what it counted.
func dumpOpcodes(bindingsDir string, w io.Writer) (err error) {
	opcodes, err := parseOpcodes(token.NewFileSet(), bindingsDir)
	if err != nil {
		return
	}
	names := make([]string, 0, len(opcodes))
	for n := range opcodes {
		names = append(names, n)
	}
	slices.SortFunc(names, func(a, b string) int { return opcodes[a] - opcodes[b] })
	for _, n := range names {
		_, _ = fmt.Fprintf(w, "%d %s\n", opcodes[n], strings.TrimPrefix(n, "FuncProcId"))
	}
	return
}

// dumpFetchTable derives the stub's reply table from the generated bindings:
// fetchers.out.go gives each fetcher's reads, enums.out.go its opcode.
func dumpFetchTable(bindingsDir string, w io.Writer) (err error) {
	fset := token.NewFileSet()
	opcodes, err := parseOpcodes(fset, bindingsDir)
	if err != nil {
		return
	}
	fetchers, err := parser.ParseFile(fset, filepath.Join(bindingsDir, "fetchers.out.go"), nil, 0)
	if err != nil {
		return
	}
	names := make([]string, 0, 32)
	lines := map[string]string{}
	for _, d := range fetchers.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || !strings.HasPrefix(fd.Name.Name, "Fetch") {
			continue
		}
		op := -1
		kinds := make([]string, 0, 8)
		for _, st := range fd.Body.List {
			switch s := st.(type) {
			case *ast.ExprStmt:
				call, ok := s.X.(*ast.CallExpr)
				if !ok || len(call.Args) != 1 {
					continue
				}
				id, ok := call.Args[0].(*ast.Ident)
				if !ok {
					continue
				}
				op = opcodes[id.Name]
			case *ast.AssignStmt:
				call, ok := s.Rhs[0].(*ast.CallExpr)
				if !ok {
					continue
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				m := sel.Sel.Name
				switch {
				case strings.HasSuffix(m, "h"):
					kinds = append(kinds, "h")
				case fetchKinds[m] != "":
					kinds = append(kinds, fetchKinds[m])
				default:
					return fmt.Errorf("fetcher %s: unknown read helper %s", fd.Name.Name, m)
				}
			}
		}
		if op < 0 {
			return fmt.Errorf("fetcher %s: no invoke found", fd.Name.Name)
		}
		names = append(names, fd.Name.Name)
		lines[fd.Name.Name] = fmt.Sprintf("fetch %d %s %s\n", op, fd.Name.Name, strings.Join(kinds, " "))
	}
	slices.Sort(names)
	for _, n := range names {
		_, _ = io.WriteString(w, lines[n])
	}
	return
}

// ---- in-process consumer ---------------------------------------------------

type fetchSpec struct {
	name  string
	kinds []string
}

// inprocChannel answers fetchers from memory: every SendSingleUseMsg is
// inspected for a fetcher opcode and the canned reply is appended to the
// buffer the unmarshaller reads from. No bytes leave the process.
type inprocChannel struct {
	u       *runtime.Unmarshaller
	replies *bytes.Buffer
	table   map[uint32]fetchSpec
	stageW  float32
	stageH  float32
	pass    uint64
	bytes   int64
	msgs    int64
}

var _ runtime.ChannelI[*runtime.Unmarshaller] = (*inprocChannel)(nil)

func (inst *inprocChannel) SyncMultiUseMsg(_ uint64, msg []byte) { inst.SendSingleUseMsg(msg) }
func (inst *inprocChannel) FlushMessages()                       {}
func (inst *inprocChannel) ReceiveMsg() iter.Seq[*runtime.Unmarshaller] {
	return func(yield func(*runtime.Unmarshaller) bool) { yield(inst.u) }
}
func (inst *inprocChannel) SendSingleUseMsg(msg []byte) {
	inst.bytes += int64(len(msg)) + 4
	inst.msgs++
	if len(msg) < 4 {
		return
	}
	op := binary.NativeEndian.Uint32(msg[:4])
	spec, ok := inst.table[op]
	if !ok {
		return
	}
	var scratch [8]byte
	switch spec.name {
	case "FetchR18AvailableSize":
		_ = binary.Write(inst.replies, binary.NativeEndian, inst.stageW)
		_ = binary.Write(inst.replies, binary.NativeEndian, inst.stageH)
	case "FetchFrameMetrics":
		inst.pass++
		_ = binary.Write(inst.replies, binary.NativeEndian, uint64(0))
		_ = binary.Write(inst.replies, binary.NativeEndian, inst.pass)
	default:
		for _, k := range spec.kinds {
			w := 4
			switch k {
			case "b", "u8":
				w = 1
			case "u64", "i64", "f64":
				w = 8
			}
			inst.replies.Write(scratch[:w])
		}
	}
}

func parseFetchTable(text string) (table map[uint32]fetchSpec, err error) {
	table = map[uint32]fetchSpec{}
	for line := range strings.SplitSeq(text, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "fetch":
			var n uint64
			n, err = strconv.ParseUint(f[1], 10, 32)
			table[uint32(n)] = fetchSpec{name: f[2], kinds: f[3:]}
		default:
			err = fmt.Errorf("fetch table: unknown line %q", line)
		}
		if err != nil {
			return
		}
	}
	return
}

// ---- scenes ----------------------------------------------------------------

type scene interface {
	setup(ids *c.WidgetIdStack)
	render(ids *c.WidgetIdStack, stageW, stageH float32)
}

// galleryScene renders every registered demo each frame, each on its own
// stage below the previous one, with the same Init/RenderStateful handling
// as the screenshot tour.
type galleryScene struct {
	demos []registry.Demo
	state map[string]any
	only  string
}

func (inst *galleryScene) setup(ids *c.WidgetIdStack) {
	inst.state = map[string]any{}
	for _, d := range registry.All() {
		if inst.only != "" && d.Name != inst.only {
			continue
		}
		switch {
		case d.BusInit != nil:
			inst.state[d.Name] = d.BusInit(ids, nil)
		case d.Init != nil:
			inst.state[d.Name] = d.Init(ids)
		}
		inst.demos = append(inst.demos, d)
	}
}

func (inst *galleryScene) render(ids *c.WidgetIdStack, stageW, stageH float32) {
	var y float32
	for _, d := range inst.demos {
		w, h := d.Stage[0], d.Stage[1]
		if w == 0 || w > stageW {
			w = stageW
		}
		if h == 0 || h > stageH {
			h = stageH
		}
		for range c.IdScope(ids.PrepareStr(d.Name)) {
			for range c.AllocateUiAtRect(0, y, w, y+h).KeepIter() {
				if d.RenderStateful != nil {
					d.RenderStateful(ids, inst.state[d.Name])
				} else {
					d.Render(ids)
				}
			}
		}
		y += h
	}
}

// labelsScene is a synthetic grid: one label, one button, one checkbox and
// one slider per row, so bytes per frame scale with the row count.
type labelsScene struct {
	rows    int
	checked []bool
	values  []float64
}

func (inst *labelsScene) setup(*c.WidgetIdStack) {
	inst.checked = make([]bool, inst.rows)
	inst.values = make([]float64, inst.rows)
}

func (inst *labelsScene) render(ids *c.WidgetIdStack, stageW, stageH float32) {
	for range c.AllocateUiAtRect(0, 0, stageW, stageH).KeepIter() {
		for range c.ScrollArea().Vscroll(true).KeepIter() {
			for range c.Grid(ids.PrepareStr("labels-grid")).NumColumns(4).KeepIter() {
				for i := range inst.rows {
					seq := uint64(i) * 4
					c.Label(fmt.Sprintf("row %d — a label of ordinary length", i)).Send()
					c.Button(ids.PrepareSeq(seq+1), c.Atoms().Text("action").Keep()).Send()
					c.Checkbox(ids.PrepareSeq(seq+2), inst.checked[i], "flag").SendRespVal(&inst.checked[i])
					c.SliderF64(ids.PrepareSeq(seq+3), inst.values[i], 0, 1).SendRespVal(&inst.values[i])
					c.EndRow()
				}
			}
		}
	}
}

// ---- measurement -----------------------------------------------------------

type sample struct {
	renderNs int64
	syncNs   int64
	totalNs  int64
}

type report struct {
	Arm        string  `json:"arm"`
	Target     string  `json:"target"`
	Consumer   string  `json:"consumer"`
	LazyFlush  bool    `json:"lazy_flush"`
	Scene      string  `json:"scene"`
	Frames     int     `json:"frames"`
	Warmup     int     `json:"warmup"`
	BytesFrame float64 `json:"bytes_per_frame"`
	MsgsFrame  float64 `json:"messages_per_frame"`
	RenderUs   quant   `json:"render_us"`
	SyncUs     quant   `json:"sync_us"`
	TotalUs    quant   `json:"total_us"`
}

type quant struct {
	P50 float64 `json:"p50"`
	P90 float64 `json:"p90"`
	Max float64 `json:"max"`
}

func quantiles(v []int64) quant {
	if len(v) == 0 {
		return quant{}
	}
	s := slices.Clone(v)
	slices.Sort(s)
	at := func(q float64) float64 { return float64(s[min(len(s)-1, int(q*float64(len(s))))]) / 1e3 }
	return quant{P50: at(0.5), P90: at(0.9), Max: float64(s[len(s)-1]) / 1e3}
}

func main() {
	run(os.Args[1:])
}

// run is main's body with the arguments passed in: a wasip1 reactor build
// never runs main and sees no argv, so its setup export hands the same
// arguments to this function (reactor_wasip1.go).
func run(args []string) {
	var (
		dumpTable = flag.String("dumpFetchTable", "", "derive the stub's fetch table from this bindings directory and exit")
		dumpOps   = flag.String("dumpOpcodes", "", "print every opcode's number and name from this bindings directory and exit")
		tableText = flag.String("fetchTable", "", "the fetch table text (from -dumpFetchTable); required for -consumer inproc")
		arm       = flag.String("arm", "", "label for the report")
		target    = flag.String("target", "", "label for the report (native, js, wasip1)")
		consumer  = flag.String("consumer", "pipe", "pipe | inproc")
		lazyFlush = flag.Bool("lazyFlush", true, "pipe: defer flushes to the next blocking read (the channel default); -lazyFlush=false flushes after every message")
		sceneName = flag.String("scene", "gallery", "gallery | labels")
		only      = flag.String("demo", "", "gallery: render only this registry demo")
		rows      = flag.Int("rows", 200, "labels: rows in the grid")
		frames    = flag.Int("frames", 300, "measured frames")
		warmup    = flag.Int("warmup", 30, "frames discarded before measuring")
		stage     = flag.String("stage", "1024x600", "stage size in points")
		list      = flag.Bool("list", false, "list the registry demos linked in and exit")
		logLevel  = flag.String("logLevel", "error", "zerolog global level during the run; a warning per widget would dominate the wasm arms")
		reactor   = flag.Bool("reactor", false, "wasip1 only: set up and return from main; the host calls the exported frame function per tick")
		cpuProf   = flag.String("cpuprofile", "", "write a CPU profile of the measured frames to this file (native)")
	)
	if err := flag.CommandLine.Parse(args); err != nil {
		os.Exit(2)
	}
	if lvl, err := zerolog.ParseLevel(*logLevel); err == nil {
		zerolog.SetGlobalLevel(lvl)
	}
	if *dumpOps != "" {
		if err := dumpOpcodes(*dumpOps, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "wasmspike:", err)
			os.Exit(1)
		}
		return
	}
	if *dumpTable != "" {
		if err := dumpFetchTable(*dumpTable, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "wasmspike:", err)
			os.Exit(1)
		}
		return
	}
	if *list {
		for _, d := range registry.All() {
			fmt.Fprintf(os.Stderr, "%s\t%s\t%gx%g\n", d.Name, d.Category, d.Stage[0], d.Stage[1])
		}
		return
	}
	var stageW, stageH float32
	if _, err := fmt.Sscanf(*stage, "%gx%g", &stageW, &stageH); err != nil {
		fmt.Fprintln(os.Stderr, "wasmspike: bad -stage:", err)
		os.Exit(1)
	}

	var sc scene
	switch *sceneName {
	case "gallery":
		sc = &galleryScene{only: *only}
	case "labels":
		sc = &labelsScene{rows: *rows}
	default:
		fmt.Fprintln(os.Stderr, "wasmspike: unknown scene", *sceneName)
		os.Exit(1)
	}

	if *cpuProf != "" {
		f, err := os.Create(*cpuProf)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wasmspike:", err)
			os.Exit(1)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintln(os.Stderr, "wasmspike:", err)
			os.Exit(1)
		}
		defer pprof.StopCPUProfile()
	}
	rep := report{Arm: *arm, Target: *target, Consumer: *consumer, LazyFlush: *lazyFlush, Scene: *sceneName, Frames: *frames, Warmup: *warmup}
	samples := make([]sample, 0, *frames)
	var bytesSum, msgsSum int64
	ids := c.NewWidgetIdStack()
	frame := 0
	stop := func() {}
	var bytesOf func() (bytes int64, msgs int64)

	loop := func() (err error) {
		if frame == 0 {
			sc.setup(ids)
		}
		st := c.CurrentApplicationState
		st.StartServersideFrame()
		c.RequestRepaint()
		ids.Reset()
		for range c.IdScope(ids.PrepareStr("wasmspike")) {
			sc.render(ids, stageW, stageH)
		}
		st.FinishServersideFrame()
		m := metrics.Current
		if frame >= *warmup {
			samples = append(samples, sample{renderNs: m.LastRenderNs, syncNs: m.LastSyncNs, totalNs: m.LastTotalNs})
		}
		frame++
		if frame == *warmup {
			b, n := bytesOf()
			bytesSum, msgsSum = -b, -n
		}
		if frame >= *warmup+*frames {
			b, n := bytesOf()
			bytesSum += b
			msgsSum += n
			stop()
		}
		return
	}

	setupReport(&rep, &samples, &bytesSum, &msgsSum)

	switch *consumer {
	case "inproc":
		table, err := parseFetchTable(*tableText)
		if err != nil || len(table) == 0 {
			fmt.Fprintln(os.Stderr, "wasmspike: -consumer inproc needs -fetchTable:", err)
			os.Exit(1)
		}
		replies := &bytes.Buffer{}
		u := runtime.NewUnmarshaller(replies, binary.NativeEndian, nil, nil)
		ch := &inprocChannel{u: u, replies: replies, table: table, stageW: stageW, stageH: stageH}
		typed.SetCurrentFffiVar(runtime.NewFffi2[*runtime.Unmarshaller](ch))
		bytesOf = func() (int64, int64) { return ch.bytes, ch.msgs }
		running := true
		stop = func() { running = false }
		for running {
			if err := loop(); err != nil {
				fmt.Fprintln(os.Stderr, "wasmspike: frame error:", err)
				os.Exit(1)
			}
		}
	case "pipe":
		cfg := &application.Config{}
		cfg.Validate(true)
		u := runtime.NewUnmarshaller(nil, binary.NativeEndian, nil, nil)
		app, err := application.NewApplication(cfg, u)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wasmspike:", err)
			os.Exit(1)
		}
		app.FffiEstablishedHandler = func(fffi *runtime.Fffi2[*runtime.Unmarshaller]) error {
			typed.SetCurrentFffiVar(fffi)
			return nil
		}
		var written, read int64
		app.RenderLoopHandler = func() error {
			// LastWritten/LastRead hold the previous frame's counts (the Run
			// loop records them after this handler returns), so sums lag by
			// one frame at each edge; the window is long enough for that
			// not to matter.
			written += int64(metrics.Current.LastWritten)
			read += int64(metrics.Current.LastRead)
			return loop()
		}
		bytesOf = func() (int64, int64) { return written, 0 }
		stop = app.Shutdown
		if err = app.Launch(); err != nil {
			fmt.Fprintln(os.Stderr, "wasmspike:", err)
			os.Exit(1)
		}
		app.Channel().SetDeferFlush(*lazyFlush)
		if *reactor {
			// The host owns the cadence: main returns after Begin and the
			// exported frame function (reactor_wasip1.go) runs one Step per
			// call, printing the report once the loop has stopped.
			if err = app.Begin(); err != nil {
				fmt.Fprintln(os.Stderr, "wasmspike:", err)
				os.Exit(1)
			}
			reported := false
			stepFn = func() int32 {
				more, e := app.Step()
				if more {
					return 0
				}
				if !reported {
					reported = true
					if e != nil {
						fmt.Fprintln(os.Stderr, "wasmspike:", e)
					}
					app.CloseAll()
					printReport()
				}
				return 1
			}
			return
		}
		if err = app.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "wasmspike:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "wasmspike: unknown consumer", *consumer)
		os.Exit(1)
	}

	printReport()
}

// stepFn is the reactor's per-tick step, set by main when -reactor is on.
var stepFn func() int32

// printReport writes the RESULT line; set up by main once the samples,
// counters and report are in place.
var printReport = func() {}

func setupReport(rep *report, samples *[]sample, bytesSum, msgsSum *int64) {
	printReport = func() {
		reportOut(*rep, *samples, *bytesSum, *msgsSum)
	}
}

func reportOut(rep report, samples []sample, bytesSum, msgsSum int64) {
	n := float64(len(samples))
	rep.Frames = len(samples)
	rep.BytesFrame = float64(bytesSum) / n
	rep.MsgsFrame = float64(msgsSum) / n
	r, s, t := make([]int64, 0, len(samples)), make([]int64, 0, len(samples)), make([]int64, 0, len(samples))
	for _, x := range samples {
		r = append(r, x.renderNs)
		s = append(s, x.syncNs)
		t = append(t, x.totalNs)
	}
	rep.RenderUs, rep.SyncUs, rep.TotalUs = quantiles(r), quantiles(s), quantiles(t)
	out := bufio.NewWriter(os.Stderr)
	b, _ := json.Marshal(rep)
	_, _ = fmt.Fprintf(out, "RESULT %s\n", b)
	_ = out.Flush()
}
