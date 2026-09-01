package track

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/decode"
)

// OpenFileE opens a recording on disk as a track: the format is sniffed
// (ADR-0208 SD5), every reader gets its own decoder through [decode.Reopener],
// the peaks cache is keyed by the file's identity, and the pyramid builds in
// the background so the call returns as soon as the header is read (SD4).
// opts.Reopen, opts.Identity and opts.Background are set by this function;
// the other options are the caller's.
func OpenFileE(ctx context.Context, path string, opts Options) (inst *Track, kind decode.KindE, err error) {
	src, kind, err := decode.OpenE(ctx, path)
	if err != nil {
		return nil, kind, err
	}
	opts.Reopen = decode.Reopener(path)
	opts.Background = true
	if opts.Identity == nil && !opts.NoCache {
		id, idErr := decode.IdentityE(path)
		if idErr != nil {
			// Without an identity the track still opens; it just builds
			// every time.
			log.Debug().Err(idErr).Str("path", path).Msg("audio track: no cache identity for this file")
		} else {
			opts.Identity = &id
		}
	}
	inst, err = OpenE(ctx, src, opts)
	if err != nil {
		return nil, kind, eb.Build().Stringer("kind", kind).Errorf("unable to open the file as a track: %w", err)
	}
	return inst, kind, nil
}
