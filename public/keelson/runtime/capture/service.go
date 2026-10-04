package capture

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

// SourceI renders captures: the window host. It draws exactly the windows
// it is given.
type SourceI interface {
	// RenderSvg exports one window's shapes.
	RenderSvg(window uint64) (job string, err error)
	// RenderPixels replays the windows' spans of one frame and rasterizes
	// them. recheck is called when the spans are chosen, a frame after the
	// decision; false fails the render (ADR-0281 §SD4).
	RenderPixels(windows []uint64, recheck func() bool) (job string, err error)
	SourceStatus(job string) (r SourceResult, ok bool)
}

// SourceResult is a render as the source reports it.
type SourceResult struct {
	Phase  opwire.PhaseE
	Reason string
	// A completed pixel render: RGBA, premultiplied, Width × Height.
	Rgba           []byte
	Width, Height  int
	PixelsPerPoint float32
	// A completed SVG render: the exported file.
	SvgPath string
	// SpansDigest names the stream a pixel render replayed.
	SpansDigest string
}

// Info is what the record of a capture names (ADR-0281 §SD6).
type Info struct {
	Request     Request
	Decision    Decision
	Obligations []string
	// Digest is the BLAKE3-256 of the bytes handed out, after every
	// obligation; SpansDigest that of the stream a pixel capture replayed.
	Digest      string
	SpansDigest string
	// Windows are the windows drawn: the scope's.
	Windows []uint64
	Bytes   int64
}

// Service is the policy enforcement point. It never decides: it asks the
// policy, refuses what the policy denies or what no handler can carry out,
// and holds the bytes until the artifact is written.
type Service struct {
	policy   PolicyI
	registry *Registry
	source   SourceI

	mu   sync.Mutex
	dir  string
	jobs map[string]*job
}

type job struct {
	info      Info
	sourceJob string
	status    opwire.CaptureStatus
}

// NewService returns a PEP over a policy, a handler registry and a source.
func NewService(policy PolicyI, registry *Registry, source SourceI) *Service {
	return &Service{policy: policy, registry: registry, source: source, jobs: map[string]*job{}}
}

// Capture decides a request and, when it is permitted, starts the render.
// A denied capture returns no job and the decision saying why.
func (inst *Service) Capture(req Request, facts Facts, recheck func() bool) (id string, d Decision, err error) {
	d = inst.policy.Decide(req, facts)
	if d.Effect != EffectPermit {
		return
	}
	var scope *ScopeParams
	for _, o := range d.Obligations {
		var check func(Obligation) error
		switch req.Format {
		case FormatPng:
			if h, ok := inst.registry.pixel[o.Name]; ok {
				check = h.Check
			}
		case FormatSvg:
			if h, ok := inst.registry.svg[o.Name]; ok {
				check = h.Check
			}
		default:
			d = deny(d, "a capture is svg or png, not "+string(req.Format))
			return
		}
		if check == nil {
			d = deny(d, "no handler carries out the "+o.Name+" obligation on a "+string(req.Format)+" capture")
			return
		}
		if e := check(o); e != nil {
			d = deny(d, o.Name+": "+e.Error())
			return
		}
		if o.Name == ObligationScope {
			scope = o.Scope
		}
	}
	if scope == nil {
		// What is drawn is the scope's to say; without one nothing is.
		d = deny(d, "the decision carries no scope")
		return
	}
	var sourceJob string
	switch req.Format {
	case FormatPng:
		sourceJob, err = inst.source.RenderPixels(slices.Clone(scope.Windows), recheck)
	case FormatSvg:
		sourceJob, err = inst.source.RenderSvg(scope.Windows[0])
	}
	if err != nil {
		return
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	id = "cap-" + hex.EncodeToString(b[:])
	inst.mu.Lock()
	inst.jobs[id] = &job{info: Info{Request: req, Decision: d, Windows: slices.Clone(scope.Windows)}, sourceJob: sourceJob,
		status: opwire.CaptureStatus{Phase: opwire.PhaseRunning}}
	inst.mu.Unlock()
	return
}

func deny(d Decision, reason string) Decision {
	return Decision{Effect: EffectDeny, Reason: reason, Policy: d.Policy}
}

// Status reports a capture, carrying out its obligations once the source
// has rendered it.
func (inst *Service) Status(id string) (st opwire.CaptureStatus, ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	j, ok := inst.jobs[id]
	if !ok {
		return
	}
	if j.status.Phase == opwire.PhaseRunning {
		if r, found := inst.source.SourceStatus(j.sourceJob); found && r.Phase != opwire.PhaseRunning {
			if r.Phase == opwire.PhaseCompleted {
				inst.finishLocked(id, j, r)
			} else {
				j.status = opwire.CaptureStatus{Phase: r.Phase, Reason: r.Reason}
			}
		}
	}
	st = j.status
	return
}

// Info returns what a capture's record names.
func (inst *Service) Info(id string) (info Info, ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	j, ok := inst.jobs[id]
	if ok {
		info = j.info
	}
	return
}

func (inst *Service) finishLocked(id string, j *job, r SourceResult) {
	fail := func(reason string) {
		j.status = opwire.CaptureStatus{Phase: opwire.PhaseFailed, Reason: reason}
	}
	obligations := sortedByPhase(j.info.Decision.Obligations, j.info.Request.Format, inst.registry)
	var out []byte
	var mediaType, ext string
	switch j.info.Request.Format {
	case FormatPng:
		if r.Width <= 0 || r.Height <= 0 || len(r.Rgba) != r.Width*r.Height*4 {
			fail("the render's pixels do not match its size")
			return
		}
		f := &Frame{Img: &image.RGBA{Pix: r.Rgba, Stride: r.Width * 4, Rect: image.Rect(0, 0, r.Width, r.Height)},
			PixelsPerPoint: r.PixelsPerPoint}
		for _, o := range obligations {
			if e := inst.registry.pixel[o.Name].Apply(f, o); e != nil {
				fail(o.Name + ": " + e.Error())
				return
			}
			j.info.Obligations = append(j.info.Obligations, o.String())
		}
		var buf bytes.Buffer
		if e := png.Encode(&buf, f.Img); e != nil {
			fail("png: " + e.Error())
			return
		}
		out, mediaType, ext = buf.Bytes(), "image/png", ".png"
	case FormatSvg:
		svg, e := os.ReadFile(r.SvgPath)
		if e != nil {
			fail("the exported SVG: " + e.Error())
			return
		}
		for _, o := range obligations {
			if svg, e = inst.registry.svg[o.Name].Apply(svg, o); e != nil {
				fail(o.Name + ": " + e.Error())
				return
			}
			j.info.Obligations = append(j.info.Obligations, o.String())
		}
		out, mediaType, ext = svg, "image/svg+xml", ".svg"
	}
	if inst.dir == "" {
		d, e := os.MkdirTemp("", "boxer-captures-")
		if e != nil {
			fail("capture directory: " + e.Error())
			return
		}
		inst.dir = d
	}
	path := filepath.Join(inst.dir, id+ext)
	if e := os.WriteFile(path, out, 0o600); e != nil {
		fail("write: " + e.Error())
		return
	}
	sum := blake3.Sum256(out)
	j.info.Digest = hex.EncodeToString(sum[:])
	j.info.SpansDigest = r.SpansDigest
	j.info.Bytes = int64(len(out))
	j.status = opwire.CaptureStatus{Phase: opwire.PhaseCompleted, Path: path, MediaType: mediaType, Bytes: int64(len(out))}
}

// sortedByPhase orders obligations scope first, then transform, keeping the
// decision's order within a phase.
func sortedByPhase(obligations []Obligation, format FormatE, reg *Registry) (out []Obligation) {
	out = slices.Clone(obligations)
	phase := func(o Obligation) PhaseE {
		if format == FormatPng {
			return reg.pixel[o.Name].Phase()
		}
		return reg.svg[o.Name].Phase()
	}
	slices.SortStableFunc(out, func(a, b Obligation) int { return int(phase(a)) - int(phase(b)) })
	return
}
