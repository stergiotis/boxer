package decode

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/wavfile"
)

// OpenE opens the recording at path, sniffing its header to pick the decoder
// (ADR-0208 §SD5): a WAVE container goes to the native wavfile reader, every
// other header — and one too short to classify — to ffmpeg. kind reports which
// decoder was used and is meaningful even when err is non-nil, so a failure
// can be attributed.
//
// The returned source is owned by the caller and must be closed. ctx bounds
// the lifetime of the ffmpeg process for a [KindFfmpeg] source; it is not used
// by a [KindWAV] one.
func OpenE(ctx context.Context, path string) (src pcm.SourceI, kind KindE, err error) {
	var head [sniffBytes]byte
	n, err := readHeadE(path, head[:])
	if err != nil {
		return nil, KindUnknown, err
	}
	kind = Sniff(head[:n])
	if kind == KindWAV {
		var file *wavfile.File
		file, err = wavfile.OpenE(path)
		if err != nil {
			return nil, KindWAV, err
		}
		return file, KindWAV, nil
	}
	kind = KindFfmpeg
	var ff *FfmpegSource
	ff, err = OpenFfmpegE(ctx, path)
	if err != nil {
		return nil, KindFfmpeg, err
	}
	return ff, KindFfmpeg, nil
}

// Reopener returns a function that opens another independent source over the
// same recording. Positioned reads of an ffmpeg-backed source restart its
// process (see [FfmpegSource.Restarts]), so consumers with their own positions
// — the peaks builder, the playback sink, the window cache — take a decoder
// each rather than sharing and thrashing one (ADR-0208 §SD3).
//
// Each call opens a fresh source the caller owns and must close; the path is
// captured, not the file, so a reopen after the file was replaced fails rather
// than reading the old bytes.
func Reopener(path string) (reopen func(ctx context.Context) (src pcm.SourceI, err error)) {
	return func(ctx context.Context) (src pcm.SourceI, err error) {
		src, _, err = OpenE(ctx, path)
		return
	}
}

// readHeadE fills dst with up to len(dst) leading bytes of path. A file
// shorter than dst is not an error — [Sniff] answers KindUnknown for a short
// head and the decoder reports what is actually wrong with the file.
func readHeadE(path string, dst []byte) (n int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, eb.Build().Str("path", path).Errorf("open recording: %w", err)
	}
	defer func() { _ = f.Close() }()
	n, err = io.ReadFull(f, dst)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return n, nil
	}
	if err != nil {
		return n, eb.Build().Str("path", path).Errorf("read recording header: %w", err)
	}
	return n, nil
}
