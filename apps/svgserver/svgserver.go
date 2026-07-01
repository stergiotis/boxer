// Command svgserver renders imzero2 (egui) views and serves them as SVG over
// HTTP. It drives the GPU-less `headless_svg` imzero2 client (see
// rust/imzero2/src/imzero2/headless_svg.rs): each HTTP request is handed to the
// single render thread, which renders one egui window from the request payload,
// asks the client to export that window as SVG (ExportSvgWindow), reads the
// resulting file back, and returns it as image/svg+xml.
//
// Threading contract: the imzero2 `c.*` API is strictly single-threaded and
// belongs to the render loop only. HTTP handlers run on their own goroutines
// and never touch `c.*`; they hand work to the render loop over a channel and
// wait for the result on a per-request reply channel (the "main-thread
// handoff" pattern). One request is rendered at a time — the render loop
// serialises them.
//
// This is a pragmatic prototype (ADR path A). Deferred: concurrency beyond
// one-in-flight (would need per-request egui contexts), auth/TLS, DPR/size
// negotiation, and response caching.
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	"github.com/stergiotis/boxer/public/thestack/imzero2/application"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// renderJob is one unit of work handed from an HTTP handler to the render loop.
// resultCh is buffered (cap 1) so the render loop's reply never blocks even if
// the handler already gave up and stopped reading.
type renderJob struct {
	title    string
	body     string
	bgRGBA   uint32
	embed    bool
	mode     uint8 // 0 = Faithful (full window frame), 1 = ContentOnly (chrome-cropped)
	resultCh chan renderResult
}

type renderResult struct {
	svg []byte
	err error
}

type server struct {
	jobs   chan *renderJob
	tmpDir string
}

// settleFrames is how many frames the window is rendered before the SVG export
// is queued, so egui has finalised the window's auto-size and area_rect (the
// export uses that rect as the SVG viewBox). A handful of frames is
// imperceptible at the FFFI round-trip cadence.
const settleFrames = 2

// maxPollFrames bounds the wait for the client to write the SVG file after the
// export opcode. The export fires in the same Rust pass, so the file normally
// lands within a frame or two; this guards against a window that never
// established an area_rect (export then writes nothing).
const maxPollFrames = 240

// idleTick paces empty frames when there is no work, so the render loop does
// not peg a core spinning the FFFI round-trip. A pending job is still picked up
// within this interval.
const idleTick = 10 * time.Millisecond

// renderLoop returns the per-frame RenderLoopHandler. All state it closes over
// is owned exclusively by the render thread.
func (s *server) renderLoop() func() error {
	ids := c.NewWidgetIdStack()
	var (
		active     *renderJob
		phase      int
		pollFrames int
		tmpPath    string
		seq        uint64
	)
	return func() error {
		c.CurrentApplicationState.StartServersideFrame()
		defer c.CurrentApplicationState.FinishServersideFrame()
		defer c.RequestRepaint()
		ids.Reset()

		// Idle: wait briefly for a job. On timeout emit an empty frame so the
		// FFFI pipe keeps ticking without busy-spinning.
		if active == nil {
			select {
			case active = <-s.jobs:
				phase = 0
				pollFrames = 0
				seq++
				tmpPath = filepath.Join(s.tmpDir, fmt.Sprintf("report-%d.svg", seq))
			case <-time.After(idleTick):
				return nil
			}
		}

		// Render the report window every frame while the job is active. Handle()
		// captures the window's id for the window-scoped SVG export below.
		win := c.Window(ids.PrepareStr("report"), c.WidgetText().Text(active.title).Keep())
		handle := win.Handle()
		for range win.KeepIter() {
			renderReport(active.title, active.body)
		}

		switch {
		case phase < settleFrames:
			phase++
		case phase == settleFrames:
			// Queue the window-scoped export. ContentOnly (mode 1) crops the
			// title-bar/frame chrome — the "webapp-report" shape; Faithful
			// (mode 0) keeps the whole window. The file is written by the
			// client's SvgExportPlugin in this same pass.
			c.ExportSvgWindow(handle, tmpPath, active.embed, active.mode, active.bgRGBA)
			phase++
		default:
			// Poll for the file the client wrote, then deliver and go idle.
			if fi, err := os.Stat(tmpPath); err == nil && fi.Size() > 0 {
				b, rerr := os.ReadFile(tmpPath)
				_ = os.Remove(tmpPath)
				active.resultCh <- renderResult{svg: b, err: rerr}
				active = nil
			} else {
				pollFrames++
				if pollFrames > maxPollFrames {
					active.resultCh <- renderResult{err: fmt.Errorf("svg export produced no file at %s within %d frames", tmpPath, maxPollFrames)}
					active = nil
				}
			}
		}
		return nil
	}
}

// renderReport draws the request payload as a simple report: a heading, the
// body split into lines, and a footer. Uses only plain widgets so the output is
// predictable; swap in richer imzero2 widgets (tables, plots, cards) as needed.
func renderReport(title string, body string) {
	for rt := range c.RichTextLabel(title) {
		rt.Heading().Strong()
	}
	c.Separator().Send()
	for line := range strings.SplitSeq(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			c.AddSpace(4)
			continue
		}
		c.Label(line).Send()
	}
	c.Separator().Send()
	for rt := range c.RichTextLabel("rendered by imzero2 → SVG") {
		rt.Small().Weak()
	}
}

// handleSVG is the HTTP entry point. It builds a job, hands it to the render
// loop, and waits for the SVG bytes. It never calls into the imzero2 `c.*` API.
func (s *server) handleSVG(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	title := q.Get("title")
	if title == "" {
		title = "imzero2 SVG report"
	}
	body := q.Get("body")
	if body == "" {
		body = defaultBody
	}
	// mode: "faithful" keeps the full window frame; anything else (default)
	// crops chrome to just the content.
	var mode uint8 = 1
	if strings.EqualFold(strings.TrimSpace(q.Get("mode")), "faithful") {
		mode = 0
	}
	job := &renderJob{
		title:    title,
		body:     body,
		bgRGBA:   parseBg(q.Get("bg")),
		embed:    q.Get("embed") != "false",
		mode:     mode,
		resultCh: make(chan renderResult, 1),
	}

	// Submit (bounded wait so a wedged render loop returns 503 rather than
	// hanging the client).
	select {
	case s.jobs <- job:
	case <-time.After(3 * time.Second):
		http.Error(w, "render queue full", http.StatusServiceUnavailable)
		return
	}

	select {
	case res := <-job.resultCh:
		if res.err != nil {
			http.Error(w, res.err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(res.svg)
	case <-time.After(15 * time.Second):
		http.Error(w, "render timeout", http.StatusGatewayTimeout)
	}
}

// handleIndex serves a tiny page that embeds a sample /svg render so the result
// can be eyeballed in a browser.
func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

// parseBg maps the ?bg query value to a packed 0xRRGGBBAA. An alpha byte of 0
// tells the exporter to omit the background rect (transparent — the host page
// shows through). Default is an opaque dark background so a standalone view is
// legible. "transparent"/"none" selects transparency; otherwise an 8-hex-digit
// RRGGBBAA value is accepted.
func parseBg(v string) uint32 {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return 0x1e1e1eff
	case "transparent", "none":
		return 0x00000000
	default:
		v = strings.TrimPrefix(v, "#")
		if n, err := strconv.ParseUint(v, 16, 32); err == nil {
			return uint32(n)
		}
		return 0x1e1e1eff
	}
}

const defaultBody = `dataset: demo.events
rows: 128
window: last 24h

status: ok
p50_latency_ms: 12.4
p99_latency_ms: 88.1
error_rate: 0.3%`

const indexHTML = `<!doctype html>
<meta charset="utf-8">
<title>imzero2 → SVG over HTTP</title>
<body style="background:#111;color:#ddd;font-family:sans-serif;margin:2rem">
<h1>imzero2 &rarr; SVG over HTTP</h1>
<p>GET <code>/svg?title=...&amp;body=...&amp;bg=1e1e1eff|transparent&amp;embed=true</code></p>
<img src="/svg?title=Hello%20from%20imzero2" alt="rendered svg"
     style="border:1px solid #333;background:#000">
</body>`

func main() {
	var (
		addr         string
		clientBinary string
		mainFont     string
		monoFont     string
		phosphorFont string
		winW         string
		winH         string
	)
	flag.StringVar(&addr, "addr", ":8087", "HTTP listen address")
	flag.StringVar(&clientBinary, "clientBinary",
		"rust/imzero2/target/headless_svg/release/imzero2",
		"path to the headless_svg imzero2 client binary")
	flag.StringVar(&mainFont, "mainFontTTF", "", "proportional font TTF (optional; enables self-contained embedded-font SVG)")
	flag.StringVar(&monoFont, "monoFontTTF", "", "monospace font TTF (optional)")
	flag.StringVar(&phosphorFont, "phosphorFontTTF", "", "icon font TTF (optional)")
	flag.StringVar(&winW, "width", "1200", "render viewport width in points")
	flag.StringVar(&winH, "height", "900", "render viewport height in points")
	flag.Parse()

	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	tmpDir, err := os.MkdirTemp("", "imzero2-svgserver-")
	if err != nil {
		log.Fatal().Err(err).Msg("unable to create temp dir")
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	srv := &server{
		jobs:   make(chan *renderJob, 32),
		tmpDir: tmpDir,
	}

	appCfg := &application.Config{
		ClientBinary:    clientBinary,
		MainFontTTF:     mainFont,
		MonoFontTTF:     monoFont,
		PhosphorFontTTF: phosphorFont,
		ImZeroSkiaClientConfig: &application.ImZeroClientConfig{
			AppTitle:                "svgserver",
			InitialMainWindowWidth:  winW,
			InitialMainWindowHeight: winH,
			Vsync:                   "false",
		},
	}
	unm := runtime.NewUnmarshaller(nil, binary.NativeEndian, nil, nil)
	app, err := application.NewApplication(appCfg, unm)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to create application")
	}

	app.FffiEstablishedHandler = func(fffi *runtime.Fffi2[*runtime.Unmarshaller]) error {
		typed.SetCurrentFffiVar(fffi)
		return nil
	}
	app.BeforeFirstFrameInitHandler = func() error { return nil }
	app.RenderLoopHandler = srv.renderLoop()

	// HTTP server on its own goroutine; the render loop owns the main goroutine
	// (app.Run blocks). The jobs channel is the only bridge between them.
	mux := http.NewServeMux()
	mux.HandleFunc("/svg", srv.handleSVG)
	mux.HandleFunc("/", srv.handleIndex)
	go func() {
		log.Info().Str("addr", addr).Msg("svgserver http listening")
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatal().Err(err).Msg("http server failed")
		}
	}()

	if err := app.Launch(); err != nil {
		log.Fatal().Err(err).Msg("unable to launch imzero2 client")
	}
	if err := app.Run(); err != nil {
		log.Fatal().Err(err).Msg("render loop exited with error")
	}
}
