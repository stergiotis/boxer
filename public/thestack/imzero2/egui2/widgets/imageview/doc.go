// Package imageview is a semi-retained widget (ADR-0267) that shows an
// [imaging.Image] through a [display.Recipe] (ADR-0373 §SD7): pan, zoom, a
// readout of the pixel under the pointer, and a colour bar for a single
// plane. It owns no navigation of its own — view state, gestures and tile
// life cycle are portolan's, over its flat pixel coordinate system — and no
// samples: it maps the tiles in view to colours on portolan's loader workers
// and never changes a plane.
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
// # Limits of this cut
//
// The image is shown from its full-resolution tiles at every zoom, so a view
// that fits a large image on screen draws all of its tiles; levels are
// ADR-0373 M3. A magnified image is drawn without interpolation unless
// [Options.Smooth] is set, and with it set a faint seam can show where two
// tiles meet.
package imageview
