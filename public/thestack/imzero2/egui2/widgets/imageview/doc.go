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
// memory as one, building its reduced levels in the background; given as an
// [ImageSource] to [Viewer.SetSource] it replaces the image shown and keeps
// the view. [Viewer.SetSource] shows any other — a renderer that draws a page or a
// plot at the scale in view, levels above full resolution included, or a
// store a decoder filled — and can keep the view, as a page turn does. A
// tile still being asked of the source a new one replaces is not shown for
// the new one; its context is cancelled, as is any tile's the view gives up.
// Every source hands back samples, so the recipe applies to all of them; a
// rendered source's 8-bit colour passes through the default recipe as it was
// drawn, and the viewer claims nothing about it as a measurement.
//
// The tiles of a source that is not resident are kept in a sample cache
// bounded by [Options.SampleCacheBytes], so a recipe change maps them again
// without asking the source; [Viewer.Invalidate] drops them when the
// source's content changes, and the old tiles stand until the new ones land.
// The histogram and percentile limits need every sample at hand: they are
// offered for an image in memory and for a source that counts a histogram
// itself ([HistogramSourceI]), as [StoreSource] does. The readout of a
// source reads the tile at hand and never asks the source from the frame.
//
// # The view and the overlay
//
// A host reads and sets the view in image pixels: [Viewer.View] and
// [Viewer.SetView] (the pixel at the centre and the magnification),
// [Viewer.Reveal] to bring a rectangle into view moving no more than it
// must, and [Events.ViewChanged]. [Options.Overlay] paints over the image
// every frame through a [Projector] between image pixels and the canvas, so
// marks, selections and labelled hit regions land on the pixels they name.
// While a region is dragged, [Events.Drag] says where it stands. Under
// [Options.DragGestures] a drag is the host's gesture instead of a pan: its
// two ends, in continuous pixels and in the order it was made, are in
// [Events.DragFrom] and [Events.DragTo] while it is under way and in
// [Events.DragEndFrom] and [Events.DragEndTo] when it is released, and a
// click's and the pointer's continuous positions in [Events.ClickedAt] and
// [Events.HoverAt] — what a host draws and edits regions with (ADR-0399
// §M2).
// [Options.WheelScrolls] makes the wheel scroll the image as a document
// does, and [Options.Keys] names keys the viewer takes while it has focus
// and hands over in [Events.Keys] — Page Up and Page Down for a document.
// None of this exposes portolan, whose geographic coordinates stay
// behind this package's boundary (ADR-0374 §SD4, §SD5).
//
// # What is shown
//
// An image opens as stored (§SD1): a colour image as its red, green and blue
// planes, a greyscale one through the grey ramp, alpha as transparency over
// a checkerboard. [Viewer.SetPlane] shows one plane by itself through a
// colour map instead; [Viewer.SetAsStored] returns. Each of the two has its
// own default recipe, and switching between them takes it.
//
// A third way is a composite of layers (ADR-0401): [Viewer.SetComposite]
// shows an ordered list of [CompositeLayer] — each a plane of the source by
// index, a recipe of its own, a colour of the fixed set and whether it is
// shown — whose colours are added, mapped from the samples the tiles
// already carry, so a change of layers asks the source nothing it has
// cached. A composite has no alpha, and a pixel where any shown layer is
// invalid is drawn as an invalid sample is (§SD5). It is linked or not
// ([Viewer.SetCompositeLinked]): linked, one recipe is every layer's and a
// recipe set reaches all of them; unlinked, a recipe set reaches the layer
// in hand ([Viewer.SetLayerInHand]) alone, and while two layers are shown the
// viewer states beside the image that their colours are not comparable with
// another view's ([Viewer.CompositeStatement], §SD2). Under a composite a
// legend names each shown layer's colour, plane and limits; the histogram
// strip, its handles and [Viewer.SetRecipe] act on the layer in hand.
//
// # Numbers come from the samples
//
// The readout in [Events] is read from the image's planes at the pixel under
// the pointer (§SD8), not from what was drawn, so it is the same whatever
// the recipe. Like every canvas readback it describes the previous frame.
//
// A source says in its [SourceInfo] which stored value its colour planes
// declare invalid, what its planes are called and where its pixels are
// (ADR-0398 §SD2–§SD4). A declared invalid sample is drawn, read and counted
// as NaN in a float plane is: the checkerboard, *invalid* in the readout,
// left out of the histogram. The readout names a plane by its label — a
// band of a stack — and writes a sample as its kind holds it; of a
// composite it names the planes of the shown layers. A colour image of a
// binned kind (signed 32-bit, float) has no colour mapping and opens as its
// first plane; a composite of its colour planes shows it whole.
//
// # Limits found from the data
//
// Percentile limits are found from the histogram of the plane shown — of a
// composite, its layer in hand's — and zscale limits from a sample of its
// values on a regular grid (ADR-0401 §SD7): at once for an image in memory,
// and otherwise off the frame, zscale reading only the level-0 tiles its
// sample falls in, through the sample cache; [Viewer.LimitsPending] reports
// the wait. [Viewer.ClippedShares] is the share of valid samples the limits
// in force clip, below and above, from the histogram counted (§SD8); the
// strip writes it as [ClippedText] does, "about" when it is read off bins
// rather than counted.
//
// # Coordinates
//
// Pixel (col, row) covers [col, col+1) × [row, row+1) with row 0 at the top
// (§SD3). On portolan's flat coordinate system that is longitude col and
// latitude −row, which this package converts at its boundary and nowhere
// else.
//
// An image with a placement (ADR-0398 §SD4) has its pixels in a frame: the
// readout line adds the pointer's frame coordinates, taken at its continuous
// pixel position, with the frame's axis names, units and code, unless
// [Options.HideFrameCoordinates] leaves them out; [Events.HoverFrame] and
// [Viewer.HoverFrame] carry them as a [FramePosition]. Coordinates on the sky
// are decimal degrees. [FramePositionAt] and [DescribePlacement] give a host
// the same text for a position of its own and for the frame itself.
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
// The level build, each plane's histogram count and a source's zscale
// sample are background jobs ([Viewer.LevelJob], [Viewer.HistogramJob],
// [Viewer.ZScaleJob]) that widgets/bgjobrow draws as
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
