// Package levels reduces an image by halving, for showing it smaller than it
// is (ADR-0373 §SD6). Level k has one cell for every 2^k × 2^k block of the
// image's samples, and for each cell three things: the mean of the samples
// the cell covers, and the smallest and the largest of them.
//
// # What a level promises
//
// The mean is the mean of the level-0 samples in the cell's block, not a
// mean of means: an integer mean is that sum over that count, rounded to
// nearest, and a block cut short by the image's edge is averaged over the
// samples it has. The minimum and maximum are exact, so a cell's extremes
// bound every sample it summarises — a bright pixel that the mean averages
// away is still in the maximum. In a float plane invalid samples are left
// out of all three, and a cell with no valid sample is itself invalid.
//
// [Build] makes every level in one pass per level, each from the sums of the
// one below. [Reduce] computes one region of one level directly from the
// image's samples; it is slower, needs nothing built, and is what a caller
// shows before a build has finished and what the tests hold [Build] to.
// While it works on a plane a build holds that plane's sums for the first
// level, about four bytes for every sample of the plane; the levels it keeps
// add up to about the size of the image again.
//
// Alpha is reduced like any other plane. Reducing colour and alpha
// separately lets a transparent pixel's colour into the mean of its
// neighbours; a file that keeps meaningful colour under transparent pixels
// is shown faithfully at full resolution and approximately below it.
package levels
