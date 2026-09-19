// Package progressest is boxer's one estimator of how fast a job runs and
// when it will finish: Holt's double exponential smoothing over the job's
// counter, with a damping layer on the displayed ETA, plus the duration,
// ETA, byte and rate spellings every progress readout shares.
//
// It has no I/O, no terminal and no UI dependency, so the CLI bar
// (hmi/progressbar), the keelson task estimator, background-job runners and
// imzero2 widgets all compute the same figures from the same samples.
// EXPLANATION.md has the algorithm and why it replaced a sliding window.
package progressest
