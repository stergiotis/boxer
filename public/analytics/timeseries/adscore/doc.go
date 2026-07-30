// Package adscore scores time-series anomaly detectors against labelled data,
// and generates labelled fixtures that are worth scoring against.
//
// # Why both halves are here
//
// Wu and Keogh (2021) showed that the datasets the field had standardized on
// carried four pervasive flaws — anomalies a one-liner finds, unrealistic
// anomaly density, mislabelled ground truth, and anomalies bunched at the end
// of the series — and that trivial heuristics reached state-of-the-art scores
// on them. A measure is therefore only half of an evaluation: without data that
// resists triviality, a good score means nothing. [GenerateE] produces series
// designed against those four flaws and [TrivialityE] checks the result by
// running the same one-liners.
//
// # The measures
//
// [EvaluateE] returns the classic point-wise AUC-ROC and AUC-PR alongside
// VUS-ROC and VUS-PR (Paparrizos et al., 2022), the measures TSB-AD settled on.
//
// Point-wise measures punish a detector for flagging an anomaly slightly early
// or late, which is the wrong thing to punish when the label boundaries were
// drawn by hand. Range-based measures fix that with a buffer around each
// labelled range, and then carry the buffer length as a parameter nobody can
// set defensibly. VUS removes it by averaging over every buffer length up to a
// ceiling, which is what makes it worth the extra cost.
//
// Both are reported because seeing them together is informative: a large gap
// between AUC-PR and VUS-PR means the detector is finding the events but not
// their exact extent.
//
// # Cost
//
// [EvaluateE] is O(n log n) for the sort plus O(n) per buffer length, so
// O(n log n + n·maxBuffer) overall. The sort is paid once and reused across
// buffers. The reference implementation re-derives the curve per threshold;
// this one sweeps thresholds incrementally, which is exact rather than
// approximate and considerably cheaper.
//
// # Reading the numbers
//
// VUS does not run from 0 to 1 the way an AUC does, and treating it as if it
// did will overstate a mediocre detector. Two effects, both measured on this
// package's own fixtures:
//
// **Chance is not 0.5 under VUS-ROC.** Positives are counted as the mean of the
// binary and buffered label mass while true positives are credited against the
// full buffered label, so a uniformly random scorer earns recall faster than it
// earns false-positive rate. The gap grows with the buffer: on a fixture with
// 50-sample anomalies, a random scorer reads 0.49 at buffer 0, 0.53 at buffer
// 25, and 0.66 at buffer 100. The existence reward damps this but does not
// remove it.
//
// **A perfect detector does not reach 1.** Firing on exactly the labelled
// extent scores 1.0 point-wise but about 0.92 under VUS, because widening the
// buffer adds positive mass at positions the detector scored 0. VUS rewards
// approximate localisation; exactness is one point on that scale, not the top
// of it.
//
// So on a typical fixture the usable VUS-ROC band is roughly [0.55, 0.92]
// rather than [0.5, 1.0]. VUS-PR is far less distorted — a random scorer lands
// near the prevalence, around 1.4× it — which is a good part of why TSB-AD
// leads with VUS-PR. Prefer comparing detectors on the same fixture over
// reading either value absolutely.
//
// # Conventions worth knowing
//
// Areas are trapezoidal for both ROC and PR, matching the VUS reference
// implementation. Trapezoidal PR area is not the step-wise average-precision
// convention and reads slightly higher; the choice keeps numbers comparable
// with published VUS results.
//
// The VUS average over buffer lengths is a plain mean, again matching the
// reference. The paper states a trapezoidal rule over that axis; the two differ
// only in endpoint weighting.
//
// This package scores detectors; it does not contain one. See
// doc/adr/0150-timeseries-subsequence-anomaly-detection.md for where it sits,
// and doc/explanation/timeseries-motif-anomaly-survey.md for why the benchmark
// history makes it a prerequisite rather than an afterthought.
package adscore
