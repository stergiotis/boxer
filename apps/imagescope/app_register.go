// Package imagescope is the image viewer app of ADR-0373: it opens a PNG or
// JPEG file, or a synthetic sample, as planes of samples at their own bit
// depth and shows them through a display recipe with
// [github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imageview].
// A TIFF, FITS, NPY or NPZ file opens as a stack, of which the person or an
// agent chooses a position on each selector axis and one band or three, and
// an image whose file places its pixels has its frame coordinates read out
// (ADR-0398 §M6). Regions — points, lines, rectangles, ellipses and
// polygons in a stated frame — are drawn, made by gesture or by operation,
// listed, measured and profiled (ADR-0399 §M2), and operators are applied to
// the window's images, each result an image of the window with the record
// of what was done (ADR-0400 §M2). The numbers it states — the readout, the
// histogram, a region's statistics — are computed from the samples of the
// image shown, never from what was drawn.
package imagescope

import (
	"embed"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowopen"
)

// helpFS embeds the inline help (apps/imagescope/help/*.md), indexed by the
// help library and shown by the HelpHost on F1 (ADR-0047).
//
//go:embed help/*.md
var helpFS embed.FS

// AppId is the app's durable id.
const AppId = "github.com/stergiotis/boxer/apps/imagescope"

// manifest is the per-process AppI descriptor. Static; every newApp()
// returns the same Manifest value.
//
// The caps are the file picker — the one file the person chooses is read
// through the handle its reply carries, and the app sees no other part of
// the filesystem (ADR-0373 §SD10) — its save dialog for the regions
// document and a record, the Send to play handovers, and the task subjects
// the decode, the measurements, the runs and the handovers run under.
// Nothing persists: an image arrives through a grant that ends with the
// window, and the regions and results live in the window; there is no
// draft to keep. Operations is the catalog of
// imagescope_ops.go. A launch configuration waits for the first app that
// has an image to hand over (ADR-0373, Deferred).
var manifest = app.Manifest{
	Id:         AppId,
	Version:    "0.1.0",
	Display:    "Image scope",
	Title:      "Image scope — an image as samples, through a recipe",
	Summary:    "View image samples through limits, a transfer function and a colour map",
	Icon:       icons.IconFileImage,
	Topics:     []app.TopicT{app.TopicData, app.TopicSensing},
	Keywords:   []string{"image", "png", "jpeg", "tiff", "geotiff", "fits", "npy", "stack", "band", "histogram", "pixel", "16-bit", "contrast", "stretch", "colour map"},
	Surface:    app.SurfaceWindowed,
	Maturity:   app.MaturityIncubating,
	Help:       help.MustSub(helpFS, "help"),
	Operations: ops.Catalog(),
	SurfaceHints: app.SurfaceHints{
		// Room for the image with its colour bar and histogram, the
		// statistics of a region beneath, and the regions and operations
		// beside it.
		PreferredWidth:  1400,
		PreferredHeight: 900,
	},
	Caps: append([]app.SubjectFilter{
		fsbroker.DialogReadCap("imagescope: Open — the system file picker chooses one PNG, JPEG, TIFF, FITS, NPY or NPZ file, which is read once through the handle the reply carries; Load regions… reads a regions document the same way"),
		fsbroker.DialogWriteCap("imagescope: Save regions… and Save record… — the system file picker chooses where the regions document or an operation's record is written (ADR-0399 §SD6, ADR-0400 §SD8)"),
		adhocdata.PublishCap("imagescope: publish the pixels of the marked region, the regions' figures or a line's profile as an ephemeral dataset for the play handover (ADR-0134)"),
		windowopen.OpenCap("imagescope: Send to play — a play window seeded with the query behind the statistics table (ADR-0135 §SD7)"),
	}, task.ProducerCaps()...),
}

// sampleSeedName is the seed variable's name, quoted where it is refused.
const sampleSeedName = "BOXER_IMAGESCOPE_SAMPLE"

// SampleSeed opens a new window on a synthetic sample image (ADR-0009 seed
// variable): a scene or a headless run reaches a shown image without the
// file picker, which no script can answer.
var SampleSeed = env.NewString(env.Spec{
	Name:        sampleSeedName,
	Description: "the synthetic sample a new imagescope window opens on: the image field, colour, small, float, scaled, elevation, double or counts, or the stack bands; for scenes and demos",
	Category:    env.CategoryE("boxer-imagescope"),
})

// regionSeedName is the region seed variable's name.
const regionSeedName = "BOXER_IMAGESCOPE_REGION"

// RegionSeed marks a region on the seeded sample, so a scene reaches a
// measurement without a drag.
var RegionSeed = env.NewString(env.Spec{
	Name:        regionSeedName,
	Description: "a region to mark on the image " + sampleSeedName + " opens, as four integers: column, row, width, height in pixels, separated by commas; for scenes and demos",
	Category:    env.CategoryE("boxer-imagescope"),
})

// init registers the app into app.DefaultRegistry. Factory ctor so two open
// windows get independent App state.
func init() {
	app.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	})
}
