package chat

// Screenshots beside the artefact (ADR-0284): a set of PNGs the model
// collects, copies and crops, carried by the artefact's revisions. The
// entries are small and live in the revisions; the bytes live in an
// imageStore, one sealed file per content hash — an unnamed inode under a
// key that exists only in this process, as ad-hoc datasets keep theirs
// (ADR-0240 §SD1) — so no screenshot is plaintext at rest and none outlives
// the process.
//
// The store is bounded per conversation by bytes on disk and by the number
// of names, and every decode is bounded by its pixel count, checked from the
// PNG header before the pixels are touched.

import (
	"bytes"
	"encoding/hex"
	"image"
	"image/draw"
	"image/png"
	"io"
	"regexp"
	"strconv"
	"sync"
	"time"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/sealed"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imagedecode"
)

// The budget's ceilings (ADR-0284 §SD4), per conversation.
var (
	ImageBudgetBytes = env.NewInt(env.Spec{
		Name:        "BOXER_CHAT_IMAGE_BYTES",
		Default:     "268435456",
		Description: "the most a chat conversation's screenshots may hold on disk, in bytes of their sealed files (ADR-0284); a capture, copy or crop past it is refused",
		Category:    env.CategoryE("boxer-chat"),
	})
	ImageBudgetCount = env.NewInt(env.Spec{
		Name:        "BOXER_CHAT_IMAGE_COUNT",
		Default:     "64",
		Description: "the most screenshots a chat conversation's artefact may name (ADR-0284)",
		Category:    env.CategoryE("boxer-chat"),
	})
	ImageMaxPixels = env.NewInt(env.Spec{
		Name:        "BOXER_CHAT_IMAGE_PIXELS",
		Default:     "16000000",
		Description: "the most pixels a chat screenshot may have; a PNG past it is refused from its header, before it is decoded (ADR-0284)",
		Category:    env.CategoryE("boxer-chat"),
	})
)

// imageThumbSide bounds a thumbnail's longer side, in pixels.
const imageThumbSide = 160

// imageRetireGrace is how long a closed conversation's sealed files wait for
// a reader still in flight.
const imageRetireGrace = 5 * time.Second

// imageSourceE is how an image came into the set.
type imageSourceE uint8

const (
	imageSourceCapture imageSourceE = iota
	imageSourceCopy
	imageSourceCrop
)

func (inst imageSourceE) String() string {
	switch inst {
	case imageSourceCopy:
		return "copy"
	case imageSourceCrop:
		return "crop"
	}
	return "capture"
}

// artImage is one name in the set. Its bytes are the store's, by hash.
type artImage struct {
	name   string
	hash   string
	w, h   int
	bytes  int64
	source imageSourceE
	// windows and crop are a capture's: the windows drawn and the crop of
	// the frame, in logical points; nil keeps the frame.
	windows []uint64
	crop    *[4]float32
	// autoCrop says crop is the windows' bounds, not the model's.
	autoCrop bool
	// from is the name a copy or crop was made from; rect a crop's x, y, w,
	// h in the source's pixels.
	from string
	rect [4]int
	// purged marks an entry whose bytes were freed (ADR-0284 §SD2).
	purged bool
	// root is the hash of the capture the entry descends from by copies and
	// crops — its own for a capture — which a disclosure names, so the trail
	// joins a view to the capture's row (ADR-0287 §SD6).
	root string
}

// rootHash is the hash of the capture e descends from.
func (inst artImage) rootHash() string {
	if inst.root == "" {
		return inst.hash
	}
	return inst.root
}

// imageLimits are the budget's ceilings.
type imageLimits struct {
	bytes  int64
	count  int
	pixels int64
}

func imageLimitsNow() imageLimits {
	return imageLimits{bytes: ImageBudgetBytes.Get(), count: int(ImageBudgetCount.Get()), pixels: ImageMaxPixels.Get()}
}

// errImageBudget is a change the budget refuses.
type errImageBudget struct {
	what        string
	used, limit int64
}

func (inst errImageBudget) Error() string {
	return "the screenshots' budget is full: " + inst.what + " " + strconv.FormatInt(inst.used, 10) + " of " + strconv.FormatInt(inst.limit, 10) +
		"; remove screenshots with purge to free it"
}

// errImagePixels is an image past the pixel ceiling, refused from its header.
type errImagePixels struct {
	w, h  int
	limit int64
}

func (inst errImagePixels) Error() string {
	return "the image has " + strconv.Itoa(inst.w) + "×" + strconv.Itoa(inst.h) + " pixels, more than the " +
		strconv.FormatInt(inst.limit, 10) + " a screenshot may have"
}

// errCropOutside is a crop rect not inside its image.
type errCropOutside struct {
	rect [4]int
	w, h int
}

func (inst errCropOutside) Error() string {
	r := inst.rect
	return "the crop " + strconv.Itoa(r[0]) + "," + strconv.Itoa(r[1]) + " " + strconv.Itoa(r[2]) + "×" + strconv.Itoa(r[3]) +
		" is not inside the image's " + strconv.Itoa(inst.w) + "×" + strconv.Itoa(inst.h) + " pixels"
}

// errSharedBytes is a purge of bytes another name still shows.
type errSharedBytes struct{ other string }

func (inst errSharedBytes) Error() string {
	return inst.other + " has the same bytes; remove it first, or remove without purge"
}

// storedImage is one hash's sealed file and what was read from it once.
type storedImage struct {
	file  *sealed.File
	w, h  int
	plain int64
	ct    int64
	thumb imagedecode.Thumbnail
}

// imageStore holds a conversation's screenshot bytes, one sealed file per
// content hash. It is shared by the turn's goroutine, which adds, and the
// render goroutine, which draws thumbnails and purges.
type imageStore struct {
	mu     sync.Mutex
	limits imageLimits
	files  map[string]*storedImage
	used   int64
	closed bool
	// pins keep a hash a tool call is still working with — sealed, not yet
	// committed — from a collection that a revert in the panel runs.
	pins map[string]int
	// create makes a sealed file; tests point it at a directory of theirs.
	create func() (*sealed.File, error)
}

func newImageStore(limits imageLimits) *imageStore {
	return &imageStore{limits: limits, files: make(map[string]*storedImage), pins: make(map[string]int), create: sealed.Create}
}

// pin keeps hash until the matching unpin.
func (inst *imageStore) pin(hash string) (unpin func()) {
	inst.mu.Lock()
	inst.pins[hash]++
	inst.mu.Unlock()
	return func() {
		inst.mu.Lock()
		if inst.pins[hash]--; inst.pins[hash] <= 0 {
			delete(inst.pins, hash)
		}
		inst.mu.Unlock()
	}
}

// imageHash is the content hash an image is addressed by: BLAKE3, as a
// capture's digest is (ADR-0281 §SD6).
func imageHash(data []byte) string {
	sum := blake3.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// pngSize reads a PNG's size from its header and refuses one past the pixel
// ceiling, before anything is decoded.
func pngSize(data []byte, maxPixels int64) (w, h int, err error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, eb.Build().Errorf("not a PNG: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0, eb.Build().Errorf("the PNG is empty")
	}
	if px := int64(cfg.Width) * int64(cfg.Height); px > maxPixels {
		return 0, 0, errImagePixels{w: cfg.Width, h: cfg.Height, limit: maxPixels}
	}
	return cfg.Width, cfg.Height, nil
}

// put seals a PNG into the store and returns its hash and size. Bytes
// already there by hash cost nothing again.
func (inst *imageStore) put(data []byte) (hash string, w, h int, err error) {
	if w, h, err = pngSize(data, inst.limits.pixels); err != nil {
		return
	}
	hash = imageHash(data)
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return "", 0, 0, eb.Build().Errorf("the conversation is closed")
	}
	if _, has := inst.files[hash]; has {
		inst.mu.Unlock()
		return
	}
	// The ciphertext is a little longer than the plaintext; the plaintext
	// is the check before sealing, the ciphertext the one after.
	if inst.used+int64(len(data)) > inst.limits.bytes {
		used := inst.used
		inst.mu.Unlock()
		return "", 0, 0, errImageBudget{what: "bytes", used: used + int64(len(data)), limit: inst.limits.bytes}
	}
	inst.mu.Unlock()
	thumb, err := imagedecode.DecodeThumbnailRGBA8(data, int(inst.limits.pixels), imageThumbSide)
	if err != nil {
		return "", 0, 0, eb.Build().Errorf("decode: %w", err)
	}
	f, err := inst.seal(data)
	if err != nil {
		return "", 0, 0, err
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if prior, has := inst.files[hash]; has || inst.closed {
		// Sealed twice by a race, or the conversation closed meanwhile.
		_ = f.Close()
		if prior == nil {
			return "", 0, 0, eb.Build().Errorf("the conversation is closed")
		}
		return
	}
	if inst.used+f.Size() > inst.limits.bytes {
		_ = f.Close()
		return "", 0, 0, errImageBudget{what: "bytes", used: inst.used + f.Size(), limit: inst.limits.bytes}
	}
	inst.files[hash] = &storedImage{file: f, w: w, h: h, plain: int64(len(data)), ct: f.Size(), thumb: thumb}
	inst.used += f.Size()
	return
}

func (inst *imageStore) seal(data []byte) (f *sealed.File, err error) {
	f, err = inst.create()
	if err != nil {
		return nil, eb.Build().Errorf("seal the screenshot: %w", err)
	}
	wr, err := f.Writer()
	if err == nil {
		if _, err = wr.Write(data); err == nil {
			err = wr.Close()
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, eb.Build().Errorf("seal the screenshot: %w", err)
	}
	return
}

// read is a hash's PNG bytes.
func (inst *imageStore) read(hash string) (data []byte, err error) {
	inst.mu.Lock()
	s, has := inst.files[hash]
	inst.mu.Unlock()
	if !has {
		return nil, eb.Build().Str("hash", hash).Errorf("the screenshot's bytes are gone")
	}
	r, err := s.file.Open()
	if err != nil {
		return nil, eb.Build().Errorf("open the screenshot: %w", err)
	}
	defer func() { _ = r.Close() }()
	return io.ReadAll(r)
}

// thumbnail is a hash's thumbnail, ok false when its bytes are gone.
func (inst *imageStore) thumbnail(hash string) (t imagedecode.Thumbnail, ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	s, ok := inst.files[hash]
	if ok {
		t = s.thumb
	}
	return
}

// has says whether a hash's bytes are held.
func (inst *imageStore) has(hash string) bool {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	_, ok := inst.files[hash]
	return ok
}

// usage is the bytes on disk and the number of files.
func (inst *imageStore) usage() (used int64, files int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.used, len(inst.files)
}

// retain frees every hash keep does not name.
func (inst *imageStore) retain(keep map[string]bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for hash := range inst.files {
		if !keep[hash] && inst.pins[hash] == 0 {
			inst.dropLocked(hash)
		}
	}
}

// purge frees one hash.
func (inst *imageStore) purge(hash string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.dropLocked(hash)
}

func (inst *imageStore) dropLocked(hash string) {
	s, has := inst.files[hash]
	if !has {
		return
	}
	delete(inst.files, hash)
	inst.used -= s.ct
	// A crop may be reading it; the key goes when that reader leaves.
	s.file.Retire(imageRetireGrace)
}

// close frees everything; the conversation is over.
func (inst *imageStore) close() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.closed = true
	for hash := range inst.files {
		inst.dropLocked(hash)
	}
}

// cropPNG cuts rect (x, y, w, h in pixels) out of a PNG, as a PNG. The
// source's size is checked from its header before it is decoded.
func cropPNG(data []byte, rect [4]int, maxPixels int64) (out []byte, err error) {
	w, h, err := pngSize(data, maxPixels)
	if err != nil {
		return
	}
	x, y, cw, ch := rect[0], rect[1], rect[2], rect[3]
	if cw <= 0 || ch <= 0 || x < 0 || y < 0 || x+cw > w || y+ch > h {
		return nil, errCropOutside{rect: rect, w: w, h: h}
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, eb.Build().Errorf("decode: %w", err)
	}
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, cw, ch))
	draw.Draw(dst, dst.Bounds(), src, image.Pt(b.Min.X+x, b.Min.Y+y), draw.Src)
	var buf bytes.Buffer
	if err = png.Encode(&buf, dst); err != nil {
		return nil, eb.Build().Errorf("encode: %w", err)
	}
	return buf.Bytes(), nil
}

// imageNameRe is what a name in the set may be: what a wikilink embed can
// name without escaping, ending in .png.
var imageNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,94}\.png$`)

// imageName checks a name the model gave, or picks the next free
// screenshot-<n>.png.
func imageName(given string, set []artImage) (name string, reason string) {
	taken := func(n string) bool {
		for _, e := range set {
			if e.name == n {
				return true
			}
		}
		return false
	}
	if given != "" {
		if !imageNameRe.MatchString(given) {
			return "", "the name " + strconv.Quote(given) + " is not lower-case letters, digits, '.', '-' and '_' ending in .png"
		}
		if taken(given) {
			return "", "the name " + strconv.Quote(given) + " is taken; artefact_images lists the names"
		}
		return given, ""
	}
	for k := 1; ; k++ {
		if n := "screenshot-" + strconv.Itoa(k) + ".png"; !taken(n) {
			return n, ""
		}
	}
}

// findImage is the entry named name in set, -1 when none is.
func findImage(set []artImage, name string) int {
	for i, e := range set {
		if e.name == name {
			return i
		}
	}
	return -1
}
