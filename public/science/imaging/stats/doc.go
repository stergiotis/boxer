// Package stats computes numbers about a plane from its samples (ADR-0373
// §SD8): a histogram, and from it the range and percentiles of the values.
// Every result is computed over the whole plane it is given and says how
// many samples it counted, so a number can always be read together with
// what it is a number of.
//
// What is counted is the whole plane ([Of]), a rectangle of it ([OfRegion])
// or the row spans a shape covers ([OfSpans], ADR-0399 §SD7); the three
// count under the same definitions, so a rectangle given as spans has the
// rectangle's histogram.
//
// # Exact where the samples allow it
//
// A histogram of a plane whose kind has at most 2^16 codes — unsigned 8- and
// 16-bit, signed 16-bit ([imaging.SampleKindE.Codes]) — has one bin per code,
// so it loses nothing and a percentile of it is a sample that occurs in the
// plane (ADR-0398 §SD1). A float plane has no such finite set of values, and
// a signed 32-bit plane too many: their histogram divides the range of the
// valid samples into [FloatBins] equal bins, and a percentile of it is
// located within one bin by assuming the samples are spread evenly across it
// — right to the width of a bin, which [Histogram.BinWidth] reports.
//
// The mean and the standard deviation come with the count. The standard
// deviation is over the samples as the whole population.
//
// Invalid samples — NaN in a float plane, and the stored value a plane
// declares invalid (ADR-0398 §SD2) — are counted and left out of everything
// else.
package stats
