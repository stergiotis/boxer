// Package aec decodes the CCSDS 121.0-B-3 Adaptive Entropy Coder — the
// lossless coding GRIB2's data representation template 5.42 wraps
// (ADR-0292 §R6) — from the standard alone.
//
// The coded stream is a sequence of coded data sets, one per block of J
// preprocessed samples: an option identifier, an uncoded reference sample
// at the start of every reference sample interval when the preprocessor
// is on, and then the block under the option the identifier names —
// fundamental-sequence (unary) codes with k split bits, the second
// extension over sample pairs, a run of all-zero blocks, or the samples
// uncoded. The preprocessor is the unit-delay predictor with the
// prediction-error mapper of §4.4, inverted here sample by sample.
//
// [Decode] returns the samples as unsigned integers of the coded width; a
// stream that ends before its sample count, or a code no option defines,
// is [ErrCorrupt] with the block that failed. Signed samples are decoded
// into two's complement of the coded width.
package aec
