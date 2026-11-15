// Package stats computes numbers about a plane from its samples (ADR-0373
// §SD8): a histogram, and from it the range and percentiles of the values.
// Every result is computed over the whole plane it is given and says how
// many samples it counted, so a number can always be read together with
// what it is a number of.
//
// # Exact where the samples allow it
//
// A histogram of an integer plane has one bin per value the sample kind can
// hold, so it loses nothing and a percentile of it is a sample that occurs
// in the plane. A float plane has no such finite set of values: its
// histogram divides the range of its valid samples into [FloatBins] equal
// bins, and a percentile of it is located within one bin by assuming the
// samples are spread evenly across it — right to the width of a bin, which
// [Histogram.BinWidth] reports.
//
// The mean and the standard deviation come with the count. The standard
// deviation is over the samples as the whole population.
//
// Invalid samples of a float plane are counted and left out of everything
// else.
package stats
