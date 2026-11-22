package imagescope

import (
	"context"
	"errors"
	"fmt"
	"image"
	"slices"
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
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imageview"
)

// Handle ops. The broker splits `runtime.fs.handle.{uuid}.{op}` on dots, so these
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

// opened is what an open leaves: the image as a store of tiles, the region
// a seed asked to mark on it, when the image is a selection of a stack the
// stack and that selection, and when it was read from a file the file's
// format and entries.
type opened struct {
	pic    *tiles.Store
	region image.Rectangle
	stk    *stackView
	file   *openedFile
}

// startOpen runs one open whose load yields a store of tiles; see
// startOpening.
func (inst *App) startOpen(region image.Rectangle, load func(ctx context.Context, report bgjob.Reporter, arrive func(*tiles.Store)) (*tiles.Store, error)) (started bool) {
	return inst.startOpening(region, func(ctx context.Context, report bgjob.Reporter, arrive func(*tiles.Store)) (*opened, error) {
		pic, err := load(ctx, report, arrive)
		if err != nil {
			return nil, err
		}
		return &opened{pic: pic}, nil
	})
}

// startOpening runs one open on the runner (W2): at most one in flight, its
// result taken at the top of a later frame. The load reads or makes the
// image off the render goroutine into a store of tiles; a load that hands
// the store over before it is filled, through arrive, has it shown as it
// fills (ADR-0374 §SD7). Decoding costs time proportional to the file's
// pixels; a PNG streamed into the store holds the file's bytes, the store
// and a band of rows, any other file its decoded image as well, for a
// moment, and a stack its decoded samples for as long as it is open — and,
// when its file has more than one entry, the file's bytes too, at most the
// file broker's read cap (ADR-0398 §M5).
func (inst *App) startOpening(region image.Rectangle, load func(ctx context.Context, report bgjob.Reporter, arrive func(*tiles.Store)) (*opened, error)) (started bool) {
	inst.openErr, inst.openNote, inst.openRefusal = "", "", 0
	inst.openGen++
	inst.arriving.Store(nil)
	started = inst.open.StartReporting(inst.tasks, bgjob.Spec{Kind: openKind, Title: openTitle},
		func(ctx context.Context, report bgjob.Reporter) (res *opened, err error) {
			res, err = load(ctx, report, func(s *tiles.Store) { inst.arriving.Store(s) })
			if err != nil {
				return nil, err
			}
			res.region = region
			return res, nil
		})
	if !started {
		inst.openGen--
	}
	return
}

// openSample opens a synthetic sample image, or a sample stack, by name.
func (inst *App) openSample(name string, region image.Rectangle) (started bool) {
	return inst.startOpening(region, func(ctx context.Context, report bgjob.Reporter, _ func(*tiles.Store)) (*opened, error) {
		report(0, 0, "making the sample")
		if slices.Contains(samples.StackNames(), name) {
			st, err := samples.StackByName(name)
			if err != nil {
				return nil, err
			}
			return openedStack(ctx, st, "", report)
		}
		img, err := samples.ByName(name)
		if err != nil {
			return nil, err
		}
		pic, err := tiles.FromImage(ctx, img, tiles.Options{})
		if err != nil {
			return nil, err
		}
		return &opened{pic: pic}, nil
	})
}

// sampleNames are the names open_sample takes: the sample images, then the
// sample stacks.
func sampleNames() []string { return append(samples.Names(), samples.StackNames()...) }

// checkSampleName refuses a name that is no sample, naming the ones that
// are.
func checkSampleName(name string) (err error) {
	if slices.Contains(sampleNames(), name) {
		return nil
	}
	return app.RefuseOperation("no sample named " + strconv.Quote(name) + "; the samples are " + strings.Join(sampleNames(), ", "))
}

// openError is an open that failed on a named file: the window shows the
// sentence as it stands, the file's name first, then what went wrong and,
// when there is one, the reader's own sentence for why.
type openError struct {
	file string
	what string
	err  error
}

func (inst *openError) Error() string {
	if inst.err == nil {
		return inst.file + ": " + inst.what
	}
	return inst.file + " " + inst.what + ": " + inst.err.Error()
}

func (inst *openError) Unwrap() error { return inst.err }

// openFile raises the system file picker and decodes the file the person
// chooses. The app never names a path: the broker returns a handle to the
// one file picked, which is read once, whole, and closed (ADR-0373 §SD10);
// the broker refuses a file over its read cap before the app sees a byte.
// The broker's requests take no context, so a cancel of the task lands
// after the picker closes. The picker takes no filter of name endings, so
// the formats' extensions are not offered to it.
func (inst *App) openFile() {
	if inst.bus == nil {
		inst.openErr = "no bus on this host; a file cannot be opened here — a sample can"
		return
	}
	bus, reg := inst.bus, inst.registry()
	inst.startOpening(image.Rectangle{}, func(ctx context.Context, report bgjob.Reporter, arrive func(*tiles.Store)) (*opened, error) {
		report(0, 0, "choose a file: "+formatTitles(reg))
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
			return nil, &openError{file: dr.DisplayName, what: "could not be read", err: err}
		}
		if _, serr := reg.Sniff(body); decode.Sniff(body) == 0 && serr != nil {
			// What is no format read here may be the broker's refusal — a
			// file over its read cap among them — which arrives in the place
			// of the content.
			if refused, rerr := fsbroker.UnmarshalDialogReply(body); rerr == nil && !refused.Granted {
				reason := refused.Reason
				if reason == "" {
					reason = "the file could not be read"
				}
				return nil, &openError{file: dr.DisplayName, what: reason}
			}
		}
		return openBody(ctx, reg, report, arrive, dr.DisplayName, body)
	})
}

// openBody decodes a file's bytes as an open does, whatever brought them
// (ADR-0398 §M5): a PNG or a JPEG into a store of tiles, streamed where it
// can be (ADR-0374 §SD7); any other file through the reader of reg that
// claims it, into a stack whose default entry's default selection is
// shown. A refusal — no reader claims the file, two do, the reader refuses
// it — is kept whole in the error, so the window and get_view show its own
// sentence, naming the formats this host reads when no reader claims it.
// A stack costs its decoded samples for as long as it is open, besides the
// store of the selection shown.
func openBody(ctx context.Context, reg *decode.Registry, report bgjob.Reporter, arrive func(*tiles.Store), name string, body []byte) (res *opened, err error) {
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	report(0, 0, "decoding "+name)
	format := decode.Sniff(body)
	if format == 0 {
		return readFile(ctx, reg, report, name, body)
	}
	pic, err := decode.DecodeTiled(ctx, body, name, decode.Options{}, tiles.Options{
		Progress: func(done, total int64) { report(uint64(done), uint64(total), "decoding "+name) },
	}, arrive)
	if err != nil {
		return nil, &openError{file: name, what: "is not an image this viewer reads", err: err}
	}
	return &opened{pic: pic, file: streamedFile(ctx, format, name, body)}, nil
}

// openedStack is a stack as an open leaves it: its default selection taken
// into a store of tiles. entry is the file's entry the stack was read
// from, empty for a stack that is no file's.
func openedStack(ctx context.Context, st imaging.Stack, entry string, report bgjob.Reporter) (*opened, error) {
	sel := defaultSelection(st)
	sel.entry = entry
	pic, err := storeOf(ctx, st, sel, report)
	if err != nil {
		return nil, err
	}
	return &opened{pic: pic, stk: newStackView(st, sel)}, nil
}

// takeOpen moves the open's outcome onto the instance, at the top of the
// frame. A store handed over before it is filled is shown at once and
// keeps filling; a failure or a cancel puts back what was shown before.
func (inst *App) takeOpen() {
	if s := inst.arriving.Swap(nil); s != nil && s != inst.pic {
		inst.prev, inst.prevStk, inst.prevShown, inst.prevFile = inst.openedStore(), inst.stk, inst.shownId, inst.file
		inst.setPic(s)
		inst.file = nil
	}
	if res, _, ok := inst.open.TakeResult(); ok {
		if res.pic != inst.pic {
			if res.stk != nil {
				inst.stackGen++
				res.stk.gen = inst.stackGen
			}
			inst.setShown(res.pic, res.stk)
		} else if inst.viewer.Recipe() == inst.openRecipe {
			// Shown while it filled, under the nominal range; now that the
			// data range is known the default follows it, unless the person
			// changed the recipe meanwhile.
			_ = inst.viewer.SetSource(imageview.StoreSource(res.pic), true)
			inst.syncChecks()
		}
		inst.file = res.file
		inst.prev, inst.prevStk, inst.prevShown, inst.prevFile = nil, nil, "", nil
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
			inst.openErr, inst.openRefusal = snap.Err.Error(), decode.RefusalOf(snap.Err)
		}
		if inst.pic != nil && inst.pic.Err() != nil || inst.prev != nil {
			// The store shown while it filled will not finish.
			inst.setShown(inst.prev, inst.prevStk)
			inst.file = inst.prevFile
			if e, ok := entryOf(inst.images, inst.prevShown); ok && inst.prevShown != openedId {
				inst.showEntry(e)
			}
		}
		inst.prev, inst.prevStk, inst.prevShown, inst.prevFile = nil, nil, "", nil
		inst.landedGen = inst.openGen
		inst.open.Invalidate()
	}
}

// setPic shows a store that is no stack's, or nothing for nil.
func (inst *App) setPic(s *tiles.Store) { inst.setShown(s, nil) }

// setShown makes a store the image opened and shows it, or nothing for
// nil, fitted to the view, with the stack it is a selection of, or nil; it
// drops what was derived from the last one, a selection asked for of the
// last stack included. The results of operations stay in the list.
func (inst *App) setShown(s *tiles.Store, stk *stackView) {
	inst.adopt(s, false)
	inst.setOpenedStore(s)
	inst.shownId = ""
	if s != nil {
		inst.shownId = openedId
	}
	inst.stk = stk
	inst.compAsk = nil
	inst.want = selection{}
	if stk != nil {
		inst.want = stk.shown.clone()
	}
	inst.selectJob.Invalidate()
	inst.selBusy, inst.selectNote, inst.selectErr, inst.selectRefusal = false, "", "", 0
	inst.lastTriple = nil
	inst.syncSliders()
}

// adopt makes a store the one shown — keeping the view, or fitting it — and
// drops what was derived from the last one.
func (inst *App) adopt(s *tiles.Store, keepView bool) {
	inst.pic = s
	if s == nil {
		_ = inst.viewer.SetSource(nil, false)
	} else {
		_ = inst.viewer.SetSource(imageview.StoreSource(s), keepView)
	}
	// A colour image of a binned kind has no colour mapping: it opens as the
	// composite of its colour planes (ADR-0401 §SD3).
	inst.showColourComposite()
	inst.openRecipe = inst.viewer.Recipe()
	inst.measure.Invalidate()
	inst.measured, inst.measureErr, inst.measuring = nil, nil, false
	inst.syncChecks()
	inst.clearHandover()
}

// describeImage is an image in one line: its name, the format it was read
// from with the variant beside it, its size, model and sample kind. A
// sample and an operation's result name no format.
func describeImage(s *tiles.Store) string {
	if s == nil {
		return "no image"
	}
	p := s.Provenance()
	name := p.SourceName
	if p.Format != "" && p.Format != resultFormat {
		name += " · " + p.Format
		if p.Variant != "" {
			name += " (" + p.Variant + ")"
		}
	}
	return fmt.Sprintf("%s · %d × %d · %s %s", name, s.Width(), s.Height(), s.Model(), s.Kind())
}

// storeNotes is sourceNotes of the store's provenance, and what its planes
// declare invalid (ADR-0398 §SD2).
func storeNotes(s *tiles.Store) (notes []string) {
	notes = sourceNotes(s.Provenance())
	if inv, ok := s.Invalid(); ok {
		notes = append(notes, "the stored value "+formatStored(inv, s.Kind())+
			" marks no measurement: it is drawn as the checkerboard, read as invalid and left out of every figure")
	}
	return
}

// noteTone is the tone a store's note is drawn in: a caveat about the
// samples in the warning tone, and what an operation records of how it made
// a result — what it was run on, what its plan decided — in the neutral one,
// since it is a statement of where the samples came from and no caveat.
func noteTone(s *tiles.Store, note string) badge.ToneE {
	if p := s.Provenance(); p.Format == resultFormat && slices.Contains(p.Notes, note) {
		return badge.ToneNeutral
	}
	return badge.ToneWarning
}

// resultFormat is the provenance format of an operation's result.
const resultFormat = "operation"

// formatStored writes a stored sample as its kind holds it: an integer
// whole, a float in the fewest digits that read back as it.
func formatStored(v float64, kind imaging.SampleKindE) string {
	if kind.IsInteger() {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// sourceNotes lists what a person should know about where the samples came
// from before reading numbers off them (ADR-0373 §SD5, §SD10; keelson-app
// F3): lossy compression, what the reader changed, a colour declaration
// that was not applied, and the reader's own notes (ADR-0398 §SD5).
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
	notes = append(notes, p.Notes...)
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
	case imaging.DerivationRowsFlipped:
		return "the file stores the bottom row first; the rows were flipped so that row 0 is the top"
	case imaging.DerivationAxesReordered:
		return "the file stores its axes in another order; the samples were reordered so that columns vary fastest"
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
