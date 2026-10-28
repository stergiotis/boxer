package mdedit

// The images a snapshot document embeds (ADR-0339 §SD6, proposed). A parse
// runs on the render goroutine and must not read the store, so the resolver
// answers from what is already decoded and notes what it was asked for and
// did not have. After the parse, a lane reads those from the same snapshot
// and decodes them; when they land the document is parsed again. The first
// load and an embed typed later take the same path. A snapshot never changes,
// so what was read stays valid while the document is open.

import (
	"context"
	"io/fs"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/fs/lading/ladingview"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/resolver"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imagedecode"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

const (
	// maxSnapshotImageBytes bounds an image file read from a snapshot.
	maxSnapshotImageBytes = int64(32 << 20)
	// snapshotImageSide bounds a decoded image's longer side, in pixels:
	// past the preview's box on a high-density display, and a bound on
	// what one image holds in memory.
	snapshotImageSide = 2048
)

type decodedImage struct {
	pixels []uint32
	w, h   uint32
}

// snapshotImages is one snapshot document's images. The maps are the render
// goroutine's; the lane reads the snapshot through fsys, a Locked view.
type snapshotImages struct {
	fsys fs.FS
	// dir is the document's directory in the snapshot, for a relative path;
	// key names the document, for the lane's key.
	dir string
	key string

	have    map[string]decodedImage
	missing map[string]bool
	wanted  map[string]bool

	job      bgjob.Keyed[map[string]decodedImage]
	jobKey   string
	inflight []string

	// names is base name → the shortest path with it in the snapshot, built
	// once on the lane, for an Obsidian embed found by name.
	namesOnce sync.Once
	names     map[string]string
}

func newSnapshotImages(fsys fs.FS, docPath string, key string) *snapshotImages {
	return &snapshotImages{fsys: fsys, dir: path.Dir(docPath), key: key,
		have: make(map[string]decodedImage), missing: make(map[string]bool), wanted: make(map[string]bool)}
}

// imageResolver answers a parse from the window's snapshot images, if any.
// It is bound to the app, so the blocks widget and the preview, which keep
// their resolver, see the images of whichever document is loaded.
type imageResolver struct {
	resolver.NoopResolver
	inst *App
}

func (r imageResolver) LoadImage(ref string) (pixels []uint32, widthPx uint32, heightPx uint32, ok bool) {
	s := r.inst.snapImages
	if s == nil {
		return
	}
	if d, has := s.have[ref]; has {
		return d.pixels, d.w, d.h, true
	}
	if !s.missing[ref] {
		s.wanted[ref] = true
	}
	return
}

// parseOptions are the options every parse of the buffer uses.
func (inst *App) parseOptions() markdown.ParseOptions {
	return markdown.ParseOptions{Resolver: imageResolver{inst: inst}}
}

// setSnapshotImages makes s the window's images: nil for a document that is
// not from a snapshot. The document is parsed again either way.
func (inst *App) setSnapshotImages(s *snapshotImages) {
	if old := inst.snapImages; old != nil {
		old.job.Close()
	}
	inst.snapImages = s
	inst.reparseForImages()
}

func (inst *App) reparseForImages() {
	inst.docOk = false
	inst.prop.resDoc = nil
	if inst.blocks != nil {
		inst.blocks.Reparse()
	}
}

// pollSnapshotImages starts a read of what the last parse wanted, and lands
// a finished one. Called once per frame; busy says a read is in flight, so
// the caller keeps frames coming.
func (inst *App) pollSnapshotImages() (busy bool) {
	s := inst.snapImages
	if s == nil {
		return
	}
	if s.jobKey == "" && len(s.wanted) > 0 {
		refs := make([]string, 0, len(s.wanted))
		for ref := range s.wanted {
			refs = append(refs, ref)
		}
		slices.Sort(refs)
		clear(s.wanted)
		s.inflight = refs
		s.jobKey = s.key + "\x00" + strings.Join(refs, "\x00")
		s.job.Demand(s.jobKey, func(ctx context.Context) (map[string]decodedImage, error) {
			return s.read(ctx, refs), nil
		})
	}
	if s.jobKey == "" {
		return
	}
	got, done, _, busy := s.job.Demand(s.jobKey, nil)
	if busy || !done {
		return busy
	}
	for _, ref := range s.inflight {
		if d, ok := got[ref]; ok {
			s.have[ref] = d
		} else {
			s.missing[ref] = true
		}
	}
	s.jobKey, s.inflight = "", nil
	if len(got) > 0 {
		inst.reparseForImages()
	}
	return
}

// read decodes each ref it finds; the lane's goroutine.
func (inst *snapshotImages) read(ctx context.Context, refs []string) (out map[string]decodedImage) {
	out = make(map[string]decodedImage, len(refs))
	for _, ref := range refs {
		if ctx.Err() != nil {
			return
		}
		p, ok := inst.locate(ref)
		if !ok {
			continue
		}
		h, err := ladingview.ReadHead(inst.fsys, p, maxSnapshotImageBytes, 0)
		if err != nil || h.IsDir || h.Truncated {
			continue
		}
		t, err := imagedecode.DecodeThumbnailRGBA8(h.Data, imagedecode.DefaultMaxPixels, snapshotImageSide)
		if err != nil {
			continue
		}
		out[ref] = decodedImage{pixels: t.Pixels, w: t.WidthPx, h: t.HeightPx}
	}
	return
}

// locate is the snapshot path ref names: relative to the document's
// directory, as a CommonMark image is; failing that, by name anywhere in the
// snapshot, as an Obsidian embed is. A URL with a scheme names nothing here.
func (inst *snapshotImages) locate(ref string) (p string, ok bool) {
	ref, _, _ = strings.Cut(ref, "#")
	if ref == "" || strings.Contains(ref, ":") {
		return
	}
	if u, err := url.PathUnescape(ref); err == nil {
		ref = u
	}
	rel := path.Join(inst.dir, ref)
	if strings.HasPrefix(ref, "/") {
		rel = path.Clean(strings.TrimPrefix(ref, "/"))
	}
	if fs.ValidPath(rel) {
		if info, err := fs.Stat(inst.fsys, rel); err == nil && !info.IsDir() {
			return rel, true
		}
	}
	inst.namesOnce.Do(inst.indexNames)
	p, ok = inst.names[path.Base(ref)]
	return
}

// indexNames walks the snapshot once for the shortest path of each base
// name, the one Obsidian picks when two files share a name.
func (inst *snapshotImages) indexNames() {
	inst.names = make(map[string]string)
	_ = fs.WalkDir(inst.fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b := path.Base(p)
		if prior, has := inst.names[b]; !has || len(p) < len(prior) {
			inst.names[b] = p
		}
		return nil
	})
}
