// Package matrixprofile computes the matrix profile of a real-valued time
// series: for every subsequence of a fixed length, the z-normalized Euclidean
// distance to its nearest non-trivial neighbour, and that neighbour's index.
//
// One structure answers both standing questions about a series. The profile's
// minima are motifs — the subsequences that repeat. Its maxima are discords —
// the subsequence least like anything else, which is the standard
// non-parametric definition of an anomaly. See [Profile.Motif] and
// [Profile.Discord].
//
// # Why z-normalization
//
// Subsequences are compared after subtracting their mean and dividing by their
// standard deviation. That makes the measure amplitude- and offset-invariant,
// which is what lets one algorithm work on a heartbeat trace and a power meter
// without retuning. It also means a flat region, whose standard deviation is
// near zero, would amplify rounding noise into apparent structure — hence
// [DefaultStdDevFloorRel], below which a window is treated as constant and
// normalizes to the zero vector. That threshold is relative to the series' own
// standard deviation, so the invariance the measure promises survives a change
// of unit.
//
// # Cost
//
// [Series.Compute] is O(n²) time and O(n) space, independent of the window
// length. Only the first distance profile is computed directly, at
// O(n·Window); every later row follows from the STOMP dot-product recurrence
// at O(1) per cell. [NewSeriesE] is likewise O(n·Window). This package
// therefore needs no FFT and no linear-algebra dependency, which is what keeps
// it inside the WASM-freestanding declaration in package_props.go (ADR-0080).
//
// The published practical ceiling for this algorithm is around 500,000
// samples. Beyond that, the quadratic term dominates.
//
// # Multivariate series
//
// [MultiSeries] and [MultiProfile] carry the same structure across d channels
// at once, as mSTAMP: for every k from 1 to d, the nearest neighbour under the
// best-matching k channels, and which channels those were. A pattern in a wide
// series usually occupies a subset of its channels, and this finds the subset
// rather than requiring it as input. [MultiSeries.SelectDimsMDLE] picks k for a
// motif; for an anomaly, sweeping k and taking the best is what the measurement
// in the tests does, and the sweep peaks at the number of affected channels.
//
// Cost is O(d·n²·log d) time and O(d·n) space — every k is computed whether or
// not it is read, because the profiles at different k do not nest and so cannot
// prune one another.
//
// # Scope
//
// Batch. Streaming left-discords are a separate package,
// [github.com/stergiotis/boxer/public/analytics/timeseries/damp]. See
// doc/adr/0150-timeseries-subsequence-anomaly-detection.md for the decision and
// doc/explanation/timeseries-motif-anomaly-survey.md for why these algorithms
// were chosen over the alternatives.
//
// # What the accuracy numbers say
//
// Both paths are measured against
// [github.com/stergiotis/boxer/public/analytics/timeseries/adscore]'s
// flaw-resistant fixtures, and the results are not uniformly flattering. The
// univariate profile clears the trivial one-liner baselines by a wide margin.
// The subdimensional profile clears them too, but on fixtures whose channels are
// mutually independent it does *not* beat the obvious cheap alternative —
// running d univariate profiles and taking the largest score at each position,
// which costs the same. What it adds over that alternative there is the channel
// subset and the count, not accuracy. Neither path has been measured on a
// multivariate series recorded from anything real.
package matrixprofile
