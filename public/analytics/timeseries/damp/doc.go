// Package damp finds left discords over an arriving stream: subsequences whose
// nearest neighbour among everything that came *before* them is far away.
//
// The restriction to the past is the whole point. A bidirectional nearest
// neighbour — what [github.com/stergiotis/boxer/public/analytics/timeseries/matrixprofile]
// computes — lets the future explain the present. That scores well offline and
// cannot be computed online, so it is not a detector.
//
// # Two modes, and they answer different questions
//
// [Config.Exact] computes every subsequence's true left-discord distance. The
// resulting sequence of scores is a usable anomaly score vector: it can be
// plotted, thresholded, or handed to
// [github.com/stergiotis/boxer/public/analytics/timeseries/adscore]. Cost is
// O(retained) per sample, independent of the data.
//
// The default is DAMP (Lu et al., 2022), which abandons a subsequence's search
// the moment a close enough neighbour turns up to prove it is not the discord.
// That is dramatically cheaper on normal data — most subsequences abandon in
// the first block — but it means most scores are upper bounds rather than
// distances. **DAMP answers "where is the anomaly", not "how anomalous is each
// position".** [Reading.Exact] marks which readings carry a real distance.
//
// Choosing wrongly is quiet rather than loud: a DAMP score vector fed to a
// scorer looks like a score vector and produces a number that means nothing.
//
// # What is not implemented, and why
//
// The published algorithm has a second optimization, forward processing, which
// looks ahead from the current subsequence and prunes future positions shown to
// have a close neighbour already. **A genuine stream cannot do this** — those
// positions have not arrived. It is available only when replaying a stored
// series, which is how the paper's throughput figures were produced. This
// package implements the streaming-admissible half.
//
// # Reading positions
//
// A window's score describes its whole span, so [Reading] reports Centre
// alongside Start and consumers should use it. Attributing the score to the
// window's start displaces every peak by half a window; measured against this
// repository's own fixtures that costs more than half the achievable accuracy.
//
// This also means the detector reports with a structural lag of Window/2: the
// subsequence centred at position p is only complete at p + Window/2.
//
// When the window outruns the anomaly, the single highest-scoring reading is a
// bracket rather than a hit. A 50-sample window over a 20-sample anomaly peaks
// at the window that starts just before the novel content enters, so its centre
// lands past the anomaly's trailing edge and neither endpoint is inside the
// event. Centre attribution still wins on average, because a per-position
// scorer integrates the whole plateau of overlapping high-scoring windows — but
// code that wants the anomaly's extent should read that plateau, not the argmax
// alone.
//
// # Throughput, and where the transform actually helps
//
// At Window 50 over an 8000-sample horizon: about 57k samples/s under DAMP
// against 19k in exact mode, so early abandoning is worth roughly 3×.
//
// A block scan can be done two ways, and which is faster is not what the
// asymptotics suggest. Measured over the same benchmark, samples/s:
//
//	Window    direct    transform
//	    16   365,671       88,393   direct 4.1× faster
//	    50    57,405       19,744   direct 2.9× faster
//	   128    12,981        9,446   direct 1.4× faster
//	   256     3,474        5,256   transform 1.5× faster
//	   512     1,097        2,948   transform 2.7× faster
//
// The crossover sits between 128 and 256, which is why [TransformMinWindow] is
// 256 and why [ScanMethodAuto] is the right default. Below it the transform is
// a substantial *loss*: three length-N passes of complex butterflies over a
// power-of-two padded buffer cannot compete with a few hundred fused
// multiply-adds over contiguous memory, and O(N log N) does not care.
//
// The practical consequence is that at the tens-of-samples windows most signals
// want, an FFT-based MASS does not explain the distance to the published DAMP
// throughput figures — it would widen it. What does contribute is forward
// pruning, which those figures use and a stream cannot.
//
// Throughput also falls steeply with Window in both methods, because the first
// backward block is sized at 8×Window: direct block cost grows as Window².
//
// # Numerical conventions
//
// Inherited from
// [github.com/stergiotis/boxer/public/analytics/timeseries/matrixprofile], for
// the reasons documented there: two-pass window statistics, a constant-window
// floor relative to the series' own spread, dot products over centred values
// while statistics come from the originals, and a final distance recomputed
// from materialized z-normalized values rather than read off the
// d = sqrt(2m(1−ρ)) identity.
//
// The centring reference is frozen at the end of the training prefix and never
// moves, because dot products taken at different times must stay comparable. A
// stream whose level drifts far from its training prefix loses the conditioning
// that reference was buying.
//
// See doc/adr/0150-timeseries-subsequence-anomaly-detection.md for where this
// sits.
package damp
