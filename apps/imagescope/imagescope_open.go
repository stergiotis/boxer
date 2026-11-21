package imagescope

import (
	"context"
	"errors"
	"fmt"
	"image"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/decode"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imageview"
)

// Handle ops. The broker splits `fs.handle.{uuid}.{op}` on dots, so these
// are appended to the prefix the dialog reply carried.
const (
	opHandleRead  = ".read"
	opHandleClose = ".close"
)

const (
	openKind  = "imagescope.open"
	openTitle = "open an image"
)

// errOpenCancelled is the open that ended because the person chose no file:
// an outcome, not a failure, and shown as a note.
var errOpenCancelled = errors.New("no file was chosen")

// opened is what an open leaves: the image as a store of tiles, and the
// region a seed asked to mark on it.
type opened struct {
	pic    *tiles.Store
	region image.Rectangle
}

// startOpen runs one open on the runner (W2): at most one in flight, its
// result taken at the top of a later frame. The load reads or makes the
// image off the render goroutine into a store of tiles; a load that hands
// the store over before it is filled, through arrive, has it shown as it
// fills (ADR-0374 §SD7). Decoding costs time proportional to the file's
// pixels; a PNG streamed into the store holds the file's bytes, the store
// and a band of rows, any other file its decoded image as well, for a
// moment.
func (inst *App) startOpen(region image.Rectangle, load func(ctx context.Context, report bgjob.Reporter, arrive func(*tiles.Store)) (*tiles.Store, error)) (started bool) {
	inst.openErr, inst.openNote = "", ""
	inst.openGen++
	inst.arriving.Store(nil)
	started = inst.open.StartReporting(inst.tasks, bgjob.Spec{Kind: openKind, Title: openTitle},
		func(ctx context.Context, report bgjob.Reporter) (res *opened, err error) {
			pic, err := load(ctx, report, func(s *tiles.Store) { inst.arriving.Store(s) })
			if err != nil {
				return nil, err
			}
			return &opened{pic: pic, region: region}, nil
		})
	if !started {
		inst.openGen--
	}
	return
}

// openSample opens a synthetic sample image by name.
func (inst *App) openSample(name string, region image.Rectangle) (started bool) {
	return inst.startOpen(region, func(ctx context.Context, report bgjob.Reporter, _ func(*tiles.Store)) (*tiles.Store, error) {
		report(0, 0, "making the sample")
		img, err := samples.ByName(name)
		if err != nil {
			return nil, err
		}
		return tiles.FromImage(ctx, img, tiles.Options{})
	})
}

// checkSampleName refuses a name that is no sample, naming the ones that
// are.
func checkSampleName(name string) (err error) {
	for _, n := range samples.Names() {
		if n == name {
			return nil
		}
	}
	return app.RefuseOperation("no sample named " + strconv.Quote(name) + "; the samples are " + strings.Join(samples.Names(), ", "))
}

// openFile raises the system file picker and decodes the file the person
// chooses. The app never names a path: the broker returns a handle to the
// one file picked, which is read once and closed (ADR-0373 §SD10). The
// broker's requests take no context, so a cancel of the task lands after the
// picker closes.
func (inst *App) openFile() {
	if inst.bus == nil {
		inst.openErr = "no bus on this host; a file cannot be opened here — a sample can"
		return
	}
	bus := inst.bus
	inst.startOpen(image.Rectangle{}, func(ctx context.Context, report bgjob.Reporter, arrive func(*tiles.Store)) (pic *tiles.Store, err error) {
		report(0, 0, "choose a PNG or JPEG file")
		reply, err := app.RequestWithin(ctx, bus, fsbroker.DialogTimeout, fsbroker.SubjectDialogRead, nil)
		if err != nil {
			return nil, eh.Errorf("the file picker did not answer: %w", err)
		}
		dr, err := fsbroker.UnmarshalDialogReply(reply)
		if err != nil {
			return nil, eh.Errorf("the file picker's reply was not understood: %w", err)
		}
		if !dr.Granted {
			return nil, errOpenCancelled
		}
		report(0, 0, "reading "+dr.DisplayName)
		body, err := app.RequestWithin(ctx, bus, fsbroker.HandleOpTimeout, dr.HandleSubjectPrefix+opHandleRead, nil)
		// The file is read once; the grant goes back whether or not the read
		// worked, so no standing authority outlives the gesture.
		_, _ = app.RequestWithin(ctx, bus, fsbroker.HandleOpTimeout, dr.HandleSubjectPrefix+opHandleClose, nil)
		if err != nil {
			return nil, eh.Errorf("%s could not be read: %w", dr.DisplayName, err)
		}
		if decode.Sniff(body) == 0 {
			// What is neither format may be the broker's refusal, which
			// arrives in the place of the content.
			if refused, rerr := fsbroker.UnmarshalDialogReply(body); rerr == nil && !refused.Granted {
				reason := refused.Reason
				if reason == "" {
					reason = "the file could not be read"
				}
				return nil, eh.Errorf("%s: %s", dr.DisplayName, reason)
			}
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		report(0, 0, "decoding "+dr.DisplayName)
		pic, err = decode.DecodeTiled(ctx, body, dr.DisplayName, decode.Options{}, tiles.Options{
			Progress: func(done, total int64) { report(uint64(done), uint64(total), "decoding "+dr.DisplayName) },
		}, arrive)
		if err != nil {
			return nil, eh.Errorf("%s is not an image this viewer reads: %w", dr.DisplayName, err)
		}
		return pic, nil
	})
}

// takeOpen moves the open's outcome onto the instance, at the top of the
// frame. A store handed over before it is filled is shown at once and
// keeps filling; a failure or a cancel puts back what was shown before.
func (inst *App) takeOpen() {
	if s := inst.arriving.Swap(nil); s != nil && s != inst.pic {
		inst.prev = inst.pic
		inst.setPic(s)
	}
	if res, _, ok := inst.open.TakeResult(); ok {
		if res.pic != inst.pic {
			inst.setPic(res.pic)
		} else if inst.viewer.Recipe() == inst.openRecipe {
			// Shown while it filled, under the nominal range; now that the
			// data range is known the default follows it, unless the person
			// changed the recipe meanwhile.
			_ = inst.viewer.SetSource(imageview.StoreSource(res.pic), true)
			inst.syncChecks()
		}
		inst.prev = nil
		if !res.region.Empty() {
			inst.viewer.SetRegion(res.region)
		}
		// A landed open needs no note: the phase line names the image.
		inst.landedGen = inst.openGen
		return
	}
	if snap := inst.open.Snapshot(); snap.State == bgjob.StateFailed {
		if errors.Is(snap.Err, errOpenCancelled) || errors.Is(snap.Err, context.Canceled) {
			inst.openNote = "open cancelled"
		} else {
			inst.openErr = snap.Err.Error()
		}
		if inst.pic != nil && inst.pic.Err() != nil || inst.prev != nil {
			// The store shown while it filled will not finish.
			inst.setPic(inst.prev)
		}
		inst.prev = nil
		inst.landedGen = inst.openGen
		inst.open.Invalidate()
	}
}

// setPic shows a store, or nothing for nil, and drops what was derived from
// the last one.
func (inst *App) setPic(s *tiles.Store) {
	inst.pic = s
	if s == nil {
		_ = inst.viewer.SetSource(nil, false)
	} else {
		_ = inst.viewer.SetSource(imageview.StoreSource(s), false)
	}
	inst.openRecipe = inst.viewer.Recipe()
	inst.measure.Invalidate()
	inst.measured, inst.measureErr, inst.measuring = nil, nil, false
	inst.syncChecks()
	inst.clearHandover()
}

// describeImage is an image in one line: its name, size, model and sample
// kind.
func describeImage(s *tiles.Store) string {
	if s == nil {
		return "no image"
	}
	return fmt.Sprintf("%s · %d × %d · %s %s", s.Provenance().SourceName, s.Width(), s.Height(), s.Model(), s.Kind())
}

// sourceNotes lists what a person should know about where the samples came
// from before reading numbers off them (ADR-0373 §SD5, §SD10; keelson-app
// F3): lossy compression, what the reader changed, a colour declaration
// that was not applied.
func sourceNotes(p imaging.Provenance) (notes []string) {
	if p.Lossy {
		notes = append(notes, noteLossy)
	}
	for _, d := range imaging.AllDerivations {
		if p.Derivations.Has(d) {
			notes = append(notes, derivationNote(d))
		}
	}
	if p.ColorDeclaration != "" {
		notes = append(notes, "the file declares its colour encoding ("+p.ColorDeclaration+"); it is not applied — samples are shown as sRGB")
	}
	return
}

// derivationNote says, for a person, what a reader did beyond decoding.
func derivationNote(d imaging.DerivationE) (s string) {
	switch d {
	case imaging.DerivationPaletteExpanded:
		return "the file stores palette indices; the planes hold the palette's colours"
	case imaging.DerivationDepthExpanded:
		return "the file stores fewer than 8 bits per sample; they were scaled to the 8-bit range"
	case imaging.DerivationColorKeyAlpha:
		return "the file names one colour as transparent; the alpha plane was made from it"
	case imaging.DerivationYCbCrConverted:
		return "the file stores luma and chroma; the planes hold their conversion to red, green and blue"
	case imaging.DerivationCMYKConverted:
		return "the file stores ink amounts; the planes hold a conversion to red, green and blue made without a profile"
	case imaging.DerivationDefaultFrameOnly:
		return "the file is an animation; only its default image is shown"
	}
	return "the reader changed the stored samples"
}

// noteLossy is the caveat a lossy source carries.
const noteLossy = "lossy source: the samples are what the decoder reconstructs, not what was recorded"

// parseRegionSeed reads the region seed: four integers, column, row, width,
// height. Its errors are refusals, quoted in the status row as they stand.
func parseRegionSeed(s string) (r image.Rectangle, err error) {
	fields := strings.FieldsFunc(s, func(c rune) bool { return c == ',' || c == ' ' || c == ';' })
	if len(fields) != 4 {
		return r, app.RefuseOperation(fmt.Sprintf("%d numbers", len(fields)))
	}
	var v [4]int
	for i, f := range fields {
		if v[i], err = strconv.Atoi(f); err != nil {
			return r, app.RefuseOperation(strconv.Quote(f) + " is not an integer")
		}
	}
	if v[2] <= 0 || v[3] <= 0 {
		return r, app.RefuseOperation("width and height are positive")
	}
	return image.Rect(v[0], v[1], v[0]+v[2], v[1]+v[3]), nil
}
