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
// # Scope
//
// Batch and univariate. Streaming left-discords (DAMP) and multivariate
// subdimensional aggregation are separate milestones of ADR-0150; see
// doc/adr/0150-timeseries-subsequence-anomaly-detection.md for the decision
// and doc/explanation/timeseries-motif-anomaly-survey.md for why these
// algorithms were chosen over the alternatives.
//
// Nothing here is validated against a labelled benchmark yet — that harness is
// M2 of the same ADR. Until it lands, treat accuracy claims as untested.
package matrixprofile
