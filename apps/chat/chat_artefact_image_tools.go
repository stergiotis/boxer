package chat

// The screenshot tools of the artefact (ADR-0284 §SD3): artefact_images
// reads the set; artefact_capture, artefact_copy_image, artefact_crop_image
// and artefact_remove_image change it, as writes — under Edit, naming their
// base revision, and waiting for the person under Ask first. A change of
// the set is a revision whose text is the head's.
//
// Every result is metadata; artefact_view_image, under the Pixels setting,
// is the one way the model sees pixels (chat_pixels.go, ADR-0287).

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonquery"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providersgui"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// imagePrompt is the system message's part for an artefact with screenshots.
const imagePrompt = `- The artefact keeps screenshots beside the text: artefact_images lists them. artefact_capture takes a PNG of windows of your task (with Apps), artefact_crop_image cuts a part out as a new screenshot, artefact_copy_image adds a second name on the same bytes, artefact_remove_image removes a name (purge frees the bytes). The person sees the screenshots in the panel; you see one's pixels only through artefact_view_image, when the person's settings offer it. The screenshots have a budget; a change past it is refused.`

// imageTools are the screenshot tools: the read always, the writes with
// write, and the capture only with Apps too.
func imageTools(write bool, capture bool) (out []openaichat.Tool) {
	out = []openaichat.Tool{
		{Name: "artefact_images", Description: "List the artefact's screenshots: name, size, where each came from, whether its bytes were purged, and the budget's use.",
			Parameters: toolSchema(`{"type":"object","properties":{},"additionalProperties":false}`)},
	}
	if !write {
		return
	}
	base := `"base_revision":{"type":"integer","minimum":0,"description":"the revision you read"}`
	name := `"name":{"type":"string","description":"lower-case letters, digits, '.', '-' and '_', ending in .png; omitted, the chat picks screenshot-<n>.png"}`
	if capture {
		out = append(out, openaichat.Tool{Name: "artefact_capture", Description: "Capture windows of your task as a PNG and add it to the artefact's screenshots. The windows are drawn together, cut to their bounds; crop keeps a rect of the frame instead, in logical points.",
			Parameters: toolSchema(`{"type":"object","properties":{` + base + `,"windows":{"type":"array","items":{"type":"integer"},"minItems":1},"crop":{"type":"object","properties":{"x":{"type":"number"},"y":{"type":"number"},"w":{"type":"number","exclusiveMinimum":0},"h":{"type":"number","exclusiveMinimum":0}},"required":["x","y","w","h"],"additionalProperties":false},` + name + `},"required":["base_revision","windows"],"additionalProperties":false}`)})
	}
	out = append(out,
		openaichat.Tool{Name: "artefact_copy_image", Description: "Add a second name on an existing screenshot's bytes, e.g. to keep it before cropping.",
			Parameters: toolSchema(`{"type":"object","properties":{` + base + `,"from":{"type":"string"},` + name + `},"required":["base_revision","from"],"additionalProperties":false}`)},
		openaichat.Tool{Name: "artefact_crop_image", Description: "Add a new screenshot cut out of an existing one, by a rect in its pixels (artefact_images gives each one's size).",
			Parameters: toolSchema(`{"type":"object","properties":{` + base + `,"from":{"type":"string"},"x":{"type":"integer","minimum":0},"y":{"type":"integer","minimum":0},"w":{"type":"integer","minimum":1},"h":{"type":"integer","minimum":1},` + name + `},"required":["base_revision","from","x","y","w","h"],"additionalProperties":false}`)},
		openaichat.Tool{Name: "artefact_remove_image", Description: "Remove a screenshot's name. Its bytes stay while an earlier revision names them; purge frees them for good, and a revert cannot bring them back.",
			Parameters: toolSchema(`{"type":"object","properties":{` + base + `,"name":{"type":"string"},"purge":{"type":"boolean"}},"required":["base_revision","name"],"additionalProperties":false}`)},
	)
	return
}

// isImageTool says whether a tool is one of the screenshot tools.
func isImageTool(name string) bool {
	switch name {
	case "artefact_images", "artefact_capture", "artefact_copy_image", "artefact_crop_image", "artefact_remove_image", "artefact_view_image":
		return true
	}
	return false
}

type imageView struct {
	Name    string    `json:"name"`
	Width   int       `json:"width"`
	Height  int       `json:"height"`
	Bytes   int64     `json:"bytes"`
	Source  string    `json:"source"`
	Windows []uint64  `json:"windows,omitempty"`
	Crop    []float32 `json:"crop,omitempty"`
	// AutoCrop says the crop is the windows' bounds, not one the model gave.
	AutoCrop bool   `json:"auto_crop,omitempty"`
	From     string `json:"from,omitempty"`
	Rect     []int  `json:"rect,omitempty"`
	Purged   bool   `json:"purged,omitempty"`
}

func viewOfImage(e artImage) (v imageView) {
	v = imageView{Name: e.name, Width: e.w, Height: e.h, Bytes: e.bytes, Source: e.source.String(), Windows: e.windows, AutoCrop: e.autoCrop, From: e.from, Purged: e.purged}
	if e.crop != nil {
		v.Crop = e.crop[:]
	}
	if e.source == imageSourceCrop {
		v.Rect = e.rect[:]
	}
	return
}

type budgetView struct {
	Bytes      int64 `json:"bytes"`
	BytesLimit int64 `json:"bytes_limit"`
	Names      int   `json:"names"`
	NamesLimit int   `json:"names_limit"`
}

func (inst *artefact) budget() (v budgetView) {
	used, _ := inst.store.usage()
	return budgetView{Bytes: used, BytesLimit: inst.store.limits.bytes, Names: len(inst.headImages()), NamesLimit: inst.store.limits.count}
}

type imagesView struct {
	Revision int         `json:"revision"`
	Images   []imageView `json:"images"`
	Budget   budgetView  `json:"budget"`
}

type imageWriteView struct {
	Revision int         `json:"revision"`
	Image    *imageView  `json:"image,omitempty"`
	Removed  string      `json:"removed,omitempty"`
	Purged   bool        `json:"purged,omitempty"`
	Rejected bool        `json:"rejected,omitempty"`
	Then     string      `json:"then,omitempty"`
	Budget   *budgetView `json:"budget,omitempty"`
}

// imageCall runs one screenshot tool call.
func (inst *coordinator) imageCall(ctx context.Context, o toolOrigin, name string, raw map[string]any, art *artefact) (content string, activity string) {
	args := toolArgs(raw)
	n, text := art.head()
	set := art.headImages()
	if name == "artefact_images" {
		v := imagesView{Revision: n, Images: make([]imageView, 0, len(set)), Budget: art.budget()}
		for _, e := range set {
			v.Images = append(v.Images, viewOfImage(e))
		}
		return marshal(v), name + " · " + plural(len(set), "screenshot")
	}
	if name == "artefact_view_image" {
		// A read: the Pixels setting governs it, not At most.
		return inst.viewImage(ctx, o, args, art)
	}
	p := art.policyNow()
	if !p.write {
		reason := "the person's settings let you read the artefact, not change it"
		inst.refuse(reason)
		return "error: " + reason, name + ": refused, read only"
	}
	base, has := args.num("base_revision")
	if !has {
		return "error: base_revision is required: the revision you read", name + ": no base_revision"
	}
	if base != n {
		e := errStale{base: base, head: n}
		return "error: " + e.Error(), name + ": stale, at revision " + strconv.Itoa(n)
	}
	fail := func(reason string) (string, string) { return "error: " + reason, name + ": " + reason }

	var entry artImage
	removes := false
	// unpin lets the collector have bytes this call sealed or relies on;
	// it runs before a release, so a rejected change frees what it sealed.
	var unpin func()
	release := func() {
		if unpin != nil {
			unpin()
			unpin = nil
		}
	}
	defer release()
	next := append([]artImage(nil), set...)
	switch name {
	case "artefact_remove_image":
		i := findImage(set, args.str("name"))
		if i < 0 {
			return fail("no screenshot is named " + strconv.Quote(args.str("name")) + "; artefact_images lists them")
		}
		entry, removes = set[i], true
		if args.flag("purge") {
			for j, e := range set {
				if j != i && e.hash == entry.hash && !e.purged {
					return fail(errSharedBytes{other: e.name}.Error())
				}
			}
		}
		next = append(next[:i:i], set[i+1:]...)
	default:
		if len(set) >= art.store.limits.count {
			return fail(errImageBudget{what: "names", used: int64(len(set) + 1), limit: int64(art.store.limits.count)}.Error())
		}
		nm, reason := imageName(args.str("name"), set)
		if reason != "" {
			return fail(reason)
		}
		entry, unpin, reason = inst.newImage(ctx, o, name, args, set, art)
		if reason != "" {
			return fail(reason)
		}
		entry.name = nm
		next = append(next, entry)
	}

	title := inst.titleNow()
	if p.ask {
		done := inst.awaitPerson()
		shown := entry
		accepted, err := art.propose(ctx, &artProposal{base: base, text: text, tool: name, title: title, images: next, ownImages: true, image: &shown, removes: removes})
		done()
		if err != nil {
			release()
			art.releaseUnreferenced()
			return "error: the proposed change was withdrawn: " + err.Error(), name + ": proposal withdrawn"
		}
		if !accepted {
			release()
			art.releaseUnreferenced()
			return marshal(imageWriteView{Revision: n, Rejected: true, Then: "the person rejected this change; ask what they want instead, do not repeat it"}), name + " · rejected"
		}
	}
	note := "+ " + entry.name
	if removes {
		note = "− " + entry.name
		if args.flag("purge") {
			note += ", purged"
		}
	}
	rev, err := art.commit(base, artRevision{text: text, source: revSourceModel, turn: o.turn, tool: name, title: title, images: next, ownImages: true, imageNote: note})
	if err != nil {
		return "error: " + err.Error(), name + ": " + err.Error()
	}
	v := imageWriteView{Revision: rev}
	if removes {
		v.Removed = entry.name
		if args.flag("purge") {
			art.purge(entry.hash)
			v.Purged = true
		}
		activity = name + " · revision " + strconv.Itoa(rev) + " · " + entry.name
	} else {
		iv := viewOfImage(entry)
		v.Image = &iv
		activity = name + " · revision " + strconv.Itoa(rev) + " · " + entry.name + " " + strconv.Itoa(entry.w) + "×" + strconv.Itoa(entry.h)
	}
	b := art.budget()
	v.Budget = &b
	return marshal(v), activity
}

// newImage makes the entry a capture, copy or crop adds — everything but
// its name — with its bytes sealed and pinned until the caller unpins.
func (inst *coordinator) newImage(ctx context.Context, o toolOrigin, name string, args toolArgs, set []artImage, art *artefact) (e artImage, unpin func(), reason string) {
	from := func() (src artImage, reason string) {
		i := findImage(set, args.str("from"))
		if i < 0 {
			return src, "no screenshot is named " + strconv.Quote(args.str("from")) + "; artefact_images lists them"
		}
		if set[i].purged {
			return src, set[i].name + " was purged: its bytes are gone"
		}
		return set[i], ""
	}
	put := func(data []byte) (reason string) {
		hash, w, h, err := art.store.put(data)
		if err != nil {
			return err.Error()
		}
		unpin = art.store.pin(hash)
		e.hash, e.w, e.h, e.bytes = hash, w, h, int64(len(data))
		return ""
	}
	switch name {
	case "artefact_capture":
		windows, crop, reason := captureArgs(args)
		if reason != "" {
			return e, nil, reason
		}
		auto := false
		if crop == nil {
			// A capture is of the whole frame with only the granted windows
			// drawn (ADR-0281 §SD4); cut to the windows, it holds what was
			// asked for and not the empty rest.
			crop = inst.windowBounds(ctx, windows)
			auto = crop != nil
		}
		data, reason := inst.capturePNG(ctx, o, windows, crop)
		if reason != "" {
			return e, nil, reason
		}
		if reason = put(data); reason != "" {
			return e, nil, reason
		}
		e.source, e.windows, e.crop, e.autoCrop, e.root = imageSourceCapture, windows, crop, auto, e.hash
	case "artefact_copy_image":
		src, reason := from()
		if reason != "" {
			return e, nil, reason
		}
		if !art.store.has(src.hash) {
			return e, nil, src.name + "'s bytes are gone"
		}
		unpin = art.store.pin(src.hash)
		e = artImage{hash: src.hash, w: src.w, h: src.h, bytes: src.bytes, source: imageSourceCopy, from: src.name, root: src.rootHash()}
	case "artefact_crop_image":
		src, reason := from()
		if reason != "" {
			return e, nil, reason
		}
		x, _ := args.num("x")
		y, _ := args.num("y")
		w, _ := args.num("w")
		h, _ := args.num("h")
		data, err := art.store.read(src.hash)
		if err != nil {
			return e, nil, err.Error()
		}
		out, err := cropPNG(data, [4]int{x, y, w, h}, art.store.limits.pixels)
		if err != nil {
			return e, nil, err.Error()
		}
		if reason = put(out); reason != "" {
			return e, nil, reason
		}
		e.source, e.from, e.rect, e.root = imageSourceCrop, src.name, [4]int{x, y, w, h}, src.rootHash()
	default:
		return e, nil, "no tool " + name
	}
	return
}

// captureArgs reads artefact_capture's windows and crop.
func captureArgs(args toolArgs) (windows []uint64, crop *[4]float32, reason string) {
	ws, _ := args["windows"].([]any)
	for _, w := range ws {
		f, ok := w.(float64)
		if !ok || f < 0 {
			return nil, nil, "windows are window numbers, as list_windows gives them"
		}
		windows = append(windows, uint64(f))
	}
	if len(windows) == 0 {
		return nil, nil, "windows is required: the windows to capture"
	}
	if c, ok := args["crop"].(map[string]any); ok {
		var r [4]float32
		for i, k := range []string{"x", "y", "w", "h"} {
			f, ok := c[k].(float64)
			if !ok {
				return nil, nil, "crop needs x, y, w and h"
			}
			r[i] = float32(f)
		}
		if r[2] <= 0 || r[3] <= 0 {
			return nil, nil, "crop's w and h must be positive"
		}
		crop = &r
	}
	return
}

// windowGeom is one window's outer rect as keelson('windows') gives it.
type windowGeom struct {
	Key uint64  `json:"key"`
	X   float64 `json:"x"`
	Y   float64 `json:"y"`
	W   float64 `json:"w"`
	H   float64 `json:"h"`
}

// windowBounds is the union of the windows' outer rects, in logical points,
// from keelson('windows'); nil when it cannot be read or no window has
// drawn, and the capture then keeps the frame. Only geometry is read — no
// title — so the read taints nothing.
func (inst *coordinator) windowBounds(ctx context.Context, windows []uint64) (crop *[4]float32) {
	if inst.kq == nil {
		return nil
	}
	keys := make([]string, 0, len(windows))
	for _, w := range windows {
		keys = append(keys, strconv.FormatUint(w, 10))
	}
	sql := "SELECT toUInt32(key) AS key, x, y, w, h FROM keelson('" + providersgui.TableWindows + "') WHERE shown AND key IN (" + strings.Join(keys, ",") + ")"
	res, err := inst.kq.Query(ctx, providersgui.TableWindows, sql, keelsonquery.FormatJSONEachRow)
	if err != nil {
		return nil
	}
	var geoms []windowGeom
	for _, line := range bytes.Split(bytes.TrimSpace(res.Body), []byte("\n")) {
		var g windowGeom
		if len(line) > 0 && json.Unmarshal(line, &g) == nil {
			geoms = append(geoms, g)
		}
	}
	return unionBounds(geoms)
}

// unionBounds is the smallest rect holding every window's, clipped to the
// viewport's top-left; nil for none.
func unionBounds(geoms []windowGeom) (crop *[4]float32) {
	minX, minY, maxX, maxY := 0.0, 0.0, 0.0, 0.0
	n := 0
	for _, g := range geoms {
		if g.W <= 0 || g.H <= 0 {
			continue
		}
		if n == 0 {
			minX, minY, maxX, maxY = g.X, g.Y, g.X+g.W, g.Y+g.H
		} else {
			minX, minY, maxX, maxY = min(minX, g.X), min(minY, g.Y), max(maxX, g.X+g.W), max(maxY, g.Y+g.H)
		}
		n++
	}
	minX, minY = max(minX, 0), max(minY, 0)
	if n == 0 || maxX <= minX || maxY <= minY {
		return nil
	}
	return &[4]float32{float32(minX), float32(minY), float32(maxX - minX), float32(maxY - minY)}
}

// capturePNG captures windows of the task as a PNG through the host's
// capture service (ADR-0281) and returns its bytes. The host marks every
// capture untrusted and taints the conversation; the chat mirrors that.
func (inst *coordinator) capturePNG(ctx context.Context, o toolOrigin, windows []uint64, crop *[4]float32) (data []byte, reason string) {
	if inst.captureHook != nil {
		return inst.captureHook(ctx, windows, crop)
	}
	if apps, _ := inst.offers(); !apps {
		return nil, "a capture needs Apps, which this conversation was started without"
	}
	h := inst.handle()
	if h == "" {
		return nil, "no task yet; call request_access for the windows first"
	}
	key := o.key()
	out, err := inst.cli.CaptureWith(ctx, agent.CaptureRequest{Handle: h, Instances: windows, Format: agent.CaptureFormatPng, Crop: crop, Key: key})
	if err != nil {
		return nil, err.Error()
	}
	deadline := time.Now().Add(callWait)
	for !out.Final() && time.Now().Before(deadline) && ctx.Err() == nil {
		if out, err = inst.cli.Status(ctx, h, key, agent.MaxStatusWait); err != nil {
			return nil, err.Error()
		}
	}
	if out.Phase != "completed" || out.Job == "" {
		why := strings.TrimSpace(out.Phase + " " + out.Reason)
		if why == "" {
			why = "did not complete"
		}
		if out.Phase == "refused" || out.Phase == "denied" {
			inst.refuse(out.Reason)
		}
		return nil, "the capture " + why
	}
	res, err := inst.cli.Read(ctx, h, out.Job)
	if err != nil {
		return nil, "the capture could not be read: " + err.Error()
	}
	if res.Untrusted {
		inst.markTainted()
	}
	if res.Confined {
		inst.mu.Lock()
		inst.confined = true
		inst.mu.Unlock()
	}
	if len(res.Data) == 0 || res.MediaType != "image/png" {
		return nil, "the capture returned no PNG"
	}
	return res.Data, ""
}
