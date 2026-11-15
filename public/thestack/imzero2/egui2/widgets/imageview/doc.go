// Package imageview is a semi-retained widget (ADR-0267) that shows an
// [imaging.Image] through a [display.Recipe] (ADR-0373 §SD7): pan, zoom, a
// readout of the pixel under the pointer, and a colour bar for a single
// plane. It owns no navigation of its own — view state, gestures and tile
// life cycle are portolan's, over its flat pixel coordinate system — and no
// samples: it maps the tiles in view to colours on portolan's loader workers
// and never changes a plane.
//
// # Sources
//
// What a viewer shows is a [SourceI]: a description and a tile of samples at
// a level when asked (ADR-0374 §SD1). [Viewer.SetImage] shows an image in
// memory as one, building its reduced levels in the background.
// [Viewer.SetSource] shows any other — a renderer that draws a page or a
// plot at the scale in view, levels above full resolution included, or a
// store a decoder filled — and can keep the view, as a page turn does.
// Every source hands back samples, so the recipe applies to all of them; a
// rendered source's 8-bit colour passes through the default recipe as it was
// drawn, and the viewer claims nothing about it as a measurement.
//
// The tiles of a source that is not resident are kept in a sample cache
// bounded by [Options.SampleCacheBytes], so a recipe change maps them again
// without asking the source; [Viewer.Invalidate] drops them when the
// source's content changes, and the old tiles stand until the new ones land.
// The histogram and percentile limits need every sample at hand and are
// offered for an image in memory only; the readout of a source reads the
// tile at hand and never asks the source from the frame.
//
// # What is shown
//
// An image opens as stored (§SD1): a colour image as its red, green and blue
// planes, a greyscale one through the grey ramp, alpha as transparency over
// a checkerboard. [Viewer.SetPlane] shows one plane by itself through a
// colour map instead; [Viewer.SetAsStored] returns. Each of the two has its
// own default recipe, and switching between them takes it.
//
// # Numbers come from the samples
//
// The readout in [Events] is read from the image's planes at the pixel under
// the pointer (§SD8), not from what was drawn, so it is the same whatever
// the recipe. Like every canvas readback it describes the previous frame.
//
// # Coordinates
//
// Pixel (col, row) covers [col, col+1) × [row, row+1) with row 0 at the top
// (§SD3). On portolan's flat coordinate system that is longitude col and
// latitude −row, which this package converts at its boundary and nowhere
// else.
//
// # Levels
//
// Shown smaller than it is, the image is drawn from reduced levels
// ([levels]): each cell the mean of the block of samples it covers, the
// level chosen so that a screen pixel never has less than one cell behind
// it (§SD6). The levels are built as a background job when an image is set;
// until they are done a tile below full resolution is reduced on the spot,
// to the same cells, so nothing changes on screen when the build lands. The
// readout line says when what is drawn is means, and [Viewer.Level] reports
// it.
//
// # Background work
//
// The level build and each plane's histogram count are background jobs
// ([Viewer.LevelJob], [Viewer.HistogramJob]) that widgets/bgjobrow draws as
// the standard row. Given a keelson task API in [Options.Tasks], each runs as
// a task of it, which the host lists, can cancel, and drains when the window
// closes; without one they run untracked. [Viewer.Close] cancels them and
// does not wait. The tile workers are the map's own and stop with Close.
//
// # What a change costs
//
// A new recipe re-maps the tiles in view and nothing else; they are replaced
// coarser levels first, and no more than a fixed number of bytes ships to
// the host per frame, the previous pixels standing in meanwhile (§SD6,
// §SD11).
//
// # Limits
//
// A magnified image is drawn without interpolation unless [Options.Smooth]
// is set, and with it set a faint seam can show where two tiles meet.
package imageview
