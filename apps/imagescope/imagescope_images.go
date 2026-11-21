package imagescope

// The window's images (ADR-0400 §M2): the image opened — for a stack, the
// store of the selection it shows — and the results of operations run on
// it, of which one is shown at a time. A result is an image like the one
// opened, a store of tiles with its levels, and carries the record of the
// operation that made it. Results are held in memory under a cap and
// dropped only when asked; nothing of them outlives the window.

import (
	"fmt"
	"image"
	"slices"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	imgops "github.com/stergiotis/boxer/public/science/imaging/ops"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
)

const (
	// openedId is the id of the image opened. Opening another replaces it
	// under the same id; a stack's selection changing replaces its store.
	openedId = "opened"
	// resultPrefix starts a result's id: r1, r2, … in the order they were
	// made, never reused within a window.
	resultPrefix = "r"
	// maxResultBytes caps what the results held take in memory, their
	// levels included: a run whose result would pass it is refused, naming
	// the remedy, drop_image.
	maxResultBytes = 1 << 30
)

// imageEntry is one image of the window. It is never modified once made:
// a change makes a new entry, so a snapshot holding the list reads a
// consistent one.
type imageEntry struct {
	id    string
	name  string
	store *tiles.Store
	// record is the record of the operation whose result the image is; nil
	// for the image opened.
	record *imgops.Record
}

// isResult reports whether the entry is an operation's result.
func (inst imageEntry) isResult() bool { return inst.id != openedId }

// entryIndex is the index of the image of that id in the list, -1 when
// there is none.
func entryIndex(images []imageEntry, id string) int {
	return slices.IndexFunc(images, func(e imageEntry) bool { return e.id == id })
}

// entryOf is the image of that id.
func entryOf(images []imageEntry, id string) (e imageEntry, ok bool) {
	i := entryIndex(images, id)
	if i < 0 {
		return e, false
	}
	return images[i], true
}

// heldResultBytes is what the results held take in memory.
func heldResultBytes(images []imageEntry) (n int64) {
	for _, e := range images {
		if e.isResult() && e.store != nil {
			n += e.store.Bytes()
		}
	}
	return
}

// resultCap is what the results held may take: maxResultBytes, unless a
// test set a smaller cap to reach it.
func (inst *App) resultCap() int64 {
	if inst.resultsCap > 0 {
		return inst.resultsCap
	}
	return maxResultBytes
}

// openedStore is the store of the image opened, nil when none is.
func (inst *App) openedStore() *tiles.Store {
	if e, ok := entryOf(inst.images, openedId); ok {
		return e.store
	}
	return nil
}

// setOpenedStore makes s the store of the image opened — a new open, or
// another selection of the stack — or removes the entry for nil. The
// results stay.
func (inst *App) setOpenedStore(s *tiles.Store) {
	i := entryIndex(inst.images, openedId)
	if s == nil {
		if i >= 0 {
			inst.images = slices.Delete(slices.Clone(inst.images), i, i+1)
		}
		return
	}
	e := imageEntry{id: openedId, name: s.Provenance().SourceName, store: s}
	images := slices.Clone(inst.images)
	if i >= 0 {
		images[i] = e
	} else {
		images = slices.Insert(images, 0, e)
	}
	inst.images = images
}

// stackShown is the stack whose selection the image shown is, nil when
// the image shown is no stack's: a plain image, or a result.
func (inst *App) stackShown() *stackView {
	if inst.shownId != openedId {
		return nil
	}
	return inst.stk
}

// addResult puts a finished operation's result into the list under the
// next id, and returns the entry.
func (inst *App) addResult(res imgops.Result, name string) imageEntry {
	inst.nextResult++
	id := resultPrefix + strconv.Itoa(inst.nextResult)
	if name == "" {
		name = res.Store.Provenance().SourceName
	}
	rec := res.Record
	e := imageEntry{id: id, name: name, store: res.Store, record: &rec}
	inst.images = append(slices.Clone(inst.images), e)
	return e
}

// showImage shows the image of that id, keeping the view, the marked
// region and the recipe's kind of view when it is the size of the one
// shown, fitting it otherwise.
func (inst *App) showImage(id string) error {
	e, ok := entryOf(inst.images, id)
	if !ok {
		return app.RefuseOperation("no image " + strconv.Quote(id) + "; list_images lists them")
	}
	if id == inst.shownId && e.store == inst.pic {
		return nil
	}
	if inst.runJob.Running() {
		return app.RefuseOperation("a run is under way and its result is shown as it fills; wait for it, or cancel it with cancel_operation")
	}
	inst.showEntry(e)
	return nil
}

// showEntry makes an entry the one shown. A store of the size of the one
// shown keeps the view and the marked region.
func (inst *App) showEntry(e imageEntry) {
	keep := inst.pic != nil && e.store != nil && inst.pic.Width() == e.store.Width() && inst.pic.Height() == e.store.Height()
	var region image.Rectangle
	if keep {
		region, _ = inst.viewer.Region()
	}
	inst.adopt(e.store, keep)
	inst.shownId = e.id
	if keep && !region.Empty() {
		inst.viewer.SetRegion(region)
	}
}

// dropImage drops a result from the list and frees what it holds once
// nothing else does. The image opened is not dropped: another is opened in
// its place. A result shown is replaced by the image opened.
func (inst *App) dropImage(id string) error {
	if id == openedId {
		return app.RefuseOperation("the image opened is not dropped; open another in its place, or drop the results with drop_image")
	}
	i := entryIndex(inst.images, id)
	if i < 0 {
		return app.RefuseOperation("no image " + strconv.Quote(id) + "; list_images lists them")
	}
	if inst.runJob.Running() && inst.shownId == id {
		return app.RefuseOperation("the image is shown while a run is under way; wait for it, or cancel it with cancel_operation")
	}
	inst.images = slices.Delete(slices.Clone(inst.images), i, i+1)
	if inst.shownId == id {
		if e, ok := entryOf(inst.images, openedId); ok {
			inst.showEntry(e)
		} else {
			inst.adopt(nil, false)
			inst.shownId = ""
		}
	}
	return nil
}

// ImageListEntry is one image of the window, as list_images gives it.
type ImageListEntry struct {
	Id       string   `desc:"the image's id: opened for the image opened, r1, r2, … for results; what show_image, drop_image, get_record and an input of run_operation take"`
	Name     string   `desc:"the image's name: the file or sample opened, or what the run that made it was named"`
	Shown    bool     `json:",omitzero" desc:"this is the image shown"`
	Result   bool     `json:",omitzero" desc:"the image is an operation's result, and get_record returns its record"`
	Width    int      `desc:"columns"`
	Height   int      `desc:"rows"`
	Model    string   `desc:"colour model"`
	Kind     string   `desc:"sample kind"`
	Planes   []string `desc:"plane names"`
	Bytes    int64    `desc:"what its store takes in memory, levels included"`
	Operator string   `json:",omitzero" desc:"for a result: the operator and its version, as operator@version"`
	Digest   string   `json:",omitzero" desc:"BLAKE3 digest of its samples, hex; empty while they are being made"`
}

// ImageList is list_images's result.
type ImageList struct {
	Images     []ImageListEntry `desc:"the images, the one opened first, then the results in the order they were made"`
	Shown      string           `desc:"the id of the image shown; empty when none is"`
	ResultsCap int64            `desc:"bytes the results held may take; a run past it is refused"`
	HeldBytes  int64            `desc:"bytes the results held take"`
}

func imageListOf(images []imageEntry, shown string) ImageList {
	out := ImageList{Shown: shown, ResultsCap: maxResultBytes, HeldBytes: heldResultBytes(images)}
	for _, e := range images {
		info := imageInfo(e.store)
		le := ImageListEntry{Id: e.id, Name: e.name, Shown: e.id == shown, Result: e.isResult(), Width: info.Width, Height: info.Height,
			Model: info.Model, Kind: info.Kind, Planes: info.Planes, Digest: info.SampleHash}
		if e.store != nil {
			le.Bytes = e.store.Bytes()
		}
		if e.record != nil {
			le.Operator = e.record.Operator + "@" + strconv.FormatUint(uint64(e.record.Version), 10)
		}
		out.Images = append(out.Images, le)
	}
	return out
}

// describeEntry is an image of the list in one line.
func describeEntry(e imageEntry) string {
	if e.store == nil {
		return e.id + " · no image"
	}
	return fmt.Sprintf("%s · %s · %d × %d · %s %s", e.id, e.name, e.store.Width(), e.store.Height(), e.store.Model(), e.store.Kind())
}

// formatBytes writes a size in bytes for a person.
func formatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return strconv.FormatFloat(float64(n)/(1<<30), 'f', 2, 64) + " GiB"
	case n >= 1<<20:
		return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MiB"
	case n >= 1<<10:
		return strconv.FormatFloat(float64(n)/(1<<10), 'f', 1, 64) + " KiB"
	}
	return strconv.FormatInt(n, 10) + " B"
}
