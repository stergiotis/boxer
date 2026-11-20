// Package eb is the builder that attaches structured fields to an error:
//
//	err = eb.Build().Str("path", path).Int("attempt", n).Errorf("open config: %w", err)
//
// [Build] starts a builder; the typed setters (Str, Int, Uint64, Bool,
// Stringer and the slice forms) append one CBOR-encoded field each; Errorf
// closes the payload and returns an error built by
// [github.com/stergiotis/boxer/public/observability/eh.ErrorfWithData], with
// the caller's stack unless [ErrorBuilder.WithoutStack] was called. The
// message stays free of values, so Error() is the message alone and the
// fields are read back by a log sink or by eh.WalkStreams.
//
// A builder is single-use: Errorf closes the field map, and setters called
// after it are ignored. Start a new [Build] for each error. Tests that
// assert on a field rather than on the message read it with the ebtest
// package.
package eb
