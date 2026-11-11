// Package changepoint finds the positions at which a series changes regime:
// its level, or its level and spread, is one thing up to a sample and another
// from the next.
//
// A change point is not an anomaly. An anomaly is a stretch unlike the rest
// that then ends; a change is a boundary after which the new behaviour is the
// rest. The sibling packages matrixprofile and damp look for the former and
// are deliberately blind to a level shift that persists.
//
// # Methods
//
// Offline, over a whole series, through a [Series]:
//
//   - [Series.Pelt] — the penalised optimum, exactly (Killick, Fearnhead and
//     Eckley, 2012). The default.
//   - [Series.BinSeg] — binary segmentation: greedy, approximate, and able to
//     stop at a fixed number of changes.
//   - [Series.PenaltyPath] — every segmentation that is optimal for some
//     penalty in a range (CROPS; Haynes, Eckley and Fearnhead, 2017). The
//     check the choice of penalty is read against.
//   - [Mosum] — a moving-sum test with an asymptotic critical value
//     (Eichinger and Kirch, 2018). A function of its own, since it scans with
//     a window and fits no segments.
//
// Online, one sample at a time, each reading using nothing after it:
//
//   - [Cusum] — Page's two-sided cumulative-sum chart (1954), with a decision
//     threshold whose false-alarm rate is tabulated.
//   - [Bocpd] — the Bayesian run-length posterior of Adams and MacKay (2007),
//     which reports a probability that the regime is recent.
//
// The three segment costs ([CostMean], [CostMeanVar], [CostRank]) decide what
// kind of change the first three can see; each cost's comment says what it is
// blind to.
//
// # Reading the results
//
// Every method here assumes the samples within a regime are independent. That
// assumption is what most real series violate and it fails in one direction:
// positively autocorrelated noise wanders, a wander looks like a short-lived
// level, and all six methods then report changes that are not there. A trend
// is reported as a staircase and a seasonal cycle as a change per half
// period. Remove what is known — difference, detrend, aggregate past the
// correlation length — before the call, and say so beside the result.
//
// The offline methods return a segmentation, not a list of discoveries with
// error rates. PELT's "exact" is a statement about an optimisation; nothing
// attaches a confidence to an individual change, and a position is typically
// good to a few samples for a shift of a few noise standard deviations and
// worse for a smaller one. The penalty sets how many changes are reported,
// and [Series.PenaltyBIC] is a convention. Where the answer matters, read the
// [Path]: changes that persist across a wide range of penalties are the ones
// the data insists on.
//
// The thresholds the methods do supply hold under their own conditions.
// [MosumResult.Critical] bounds the chance of any report in a series without
// change and says nothing once there is one. [Cusum]'s published run lengths
// assume the in-control mean and scale are known rather than estimated from a
// warm-up. [BocpdReading.RecentChange] is a posterior probability under a
// Gaussian model with a hazard the caller chose.
//
// Offline and online answers to the same series are not interchangeable. An
// offline segmentation sees what came after each change and places it with
// that hindsight; an online reading is what could have been known at the
// time, and it arrives some samples late. Drawing the first as if it were the
// second overstates what a monitor would have caught.
//
// # Conventions
//
// A change is reported as the index of the first sample of the new regime
// ([Segmentation.Starts]). Input must be finite; a NaN or an infinity is
// refused with its index, because dropping it would join two samples that
// were not neighbours. Sampling is assumed regular in whatever sense the
// caller cares about: the methods see an ordered sequence and no timestamps.
//
// The package has no dependency beyond the standard library and the error
// helpers, and compiles for freestanding WASM.
//
// # Not here
//
// Wild binary segmentation, kernel and energy-distance (E-divisive) costs,
// classifier-based detection (changeforest), Bayesian offline segmentation
// with trend and seasonality (BEAST), regression structural breaks
// (Bai–Perron) and multivariate input are outside this package. Each would
// arrive with the series that needs it.
package changepoint
