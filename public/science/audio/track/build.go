package track

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/peaks"
)

// CacheFileExt is the extension of a peaks sidecar file (ADR-0208 §SD4). A
// derived product other than peaks gets its own extension in the same
// directory (§SD12).
const CacheFileExt = ".bxpk"

// BuildProgress is a snapshot of the peaks build (ADR-0208 §SD4), safe to
// take from any goroutine — typically once per rendered frame, to draw the
// unbuilt remainder as a placeholder.
//
// The pair to watch is BuiltFrames against TotalFrames: the built prefix
// grows monotonically and is readable while it grows. Complete says the
// whole pyramid is readable, including its trailing partial bins, which is
// also the point from which a global-maximum normalisation is meaningful.
type BuildProgress struct {
	BuiltFrames int64
	TotalFrames int64
	Complete    bool
	// FromCache reports that the pyramid was loaded from a peaks file rather
	// than built. It implies Complete.
	FromCache bool
	// Err is a build that failed or was cancelled. The pyramid stays partial
	// and readable up to BuiltFrames, and Complete is false.
	Err error
	// CacheErr is a peaks file that could not be written. It costs the next
	// open its head start and nothing else, so it is reported apart from Err
	// and leaves the build successful.
	CacheErr error
	// Elapsed is the build's running time so far, or its total once done;
	// zero for a pyramid that came from the cache.
	Elapsed time.Duration
	// EtaMs is the rate-based estimate of the milliseconds left, from the
	// frames built over Elapsed; zero when unknown, done, or failed.
	EtaMs int64
}

// EstimateEtaMs is the rate-based remaining time: built frames over elapsed
// time extrapolated to the remainder. Zero until anything is built and once
// everything is.
func EstimateEtaMs(elapsed time.Duration, built, total int64) (etaMs int64) {
	if elapsed <= 0 || built <= 0 || total <= built {
		return 0
	}
	perFrame := float64(elapsed) / float64(built)
	return int64(perFrame * float64(total-built) / float64(time.Millisecond))
}

// buildOutcome is what one build run ended with, published as a whole so a
// reader never sees one of the two errors updated without the other.
type buildOutcome struct {
	err        error
	cacheErr   error
	finishedAt time.Time
}

// BuildProgress returns the state of the peaks build. Safe from any
// goroutine, including while a background build runs.
func (inst *Track) BuildProgress() (bp BuildProgress) {
	bp = BuildProgress{
		BuiltFrames: inst.pyramid.Built(),
		TotalFrames: inst.frames,
		Complete:    inst.pyramid.IsComplete(),
		FromCache:   inst.fromCache,
	}
	outcome := inst.outcome.Load()
	if outcome != nil {
		bp.Err = outcome.err
		bp.CacheErr = outcome.cacheErr
	}
	if !inst.buildStart.IsZero() {
		end := time.Now()
		if outcome != nil && !outcome.finishedAt.IsZero() {
			end = outcome.finishedAt
		}
		bp.Elapsed = end.Sub(inst.buildStart)
		if !bp.Complete && bp.Err == nil {
			bp.EtaMs = EstimateEtaMs(bp.Elapsed, bp.BuiltFrames, bp.TotalFrames)
		}
	}
	return bp
}

// CancelBuild stops a background build where it is; the pyramid keeps the
// prefix it has and draws it, and BuildProgress reports context.Canceled.
// The track itself stays open. A build that is not running is unaffected.
func (inst *Track) CancelBuild() {
	if inst.buildCancel != nil {
		inst.buildCancel()
	}
}

// buildJob is what a build run needs: where to read from, whether that
// source is the run's own (a [Options.Reopen]ed one, closed when the run
// ends) and where to leave the finished pyramid.
type buildJob struct {
	src         pcm.SourceI
	progress    func(builtFrames int64)
	cachePath   string
	identity    *peaks.Identity
	chunkFrames int
	owned       bool
}

// runBuild fills the preallocated pyramid and then writes the cache. It is
// the body of the background build's goroutine (ADR-0208 §SD4) and runs
// [Options.Progress] on that goroutine.
//
// Its context is the track's, not the [OpenE] caller's, so the build outlives
// the open call and is ended by [Track.CloseE]; cancellation is honoured
// between chunks, which is what makes CloseE prompt.
func (inst *Track) runBuild(ctx context.Context, job buildJob) {
	defer close(inst.buildDone)
	err := inst.pyramid.FillFromE(ctx, job.src, job.chunkFrames, job.progress)
	var cacheErr error
	switch {
	case err == nil:
		cacheErr = inst.writePeaksCache(job)
	case ctx.Err() != nil:
		log.Debug().
			Int64("builtFrames", inst.pyramid.Built()).
			Int64("frames", inst.frames).
			Msg("audio peaks build cancelled")
	default:
		log.Warn().Err(err).
			Int64("builtFrames", inst.pyramid.Built()).
			Int64("frames", inst.frames).
			Msg("audio peaks build failed; the pyramid stays partial")
	}
	if job.owned {
		closeErr := job.src.CloseE()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("unable to close the peaks build source")
		}
	}
	inst.outcome.Store(&buildOutcome{err: err, cacheErr: cacheErr, finishedAt: time.Now()})
}

// writePeaksCache writes the finished pyramid to the job's path, or returns
// nil when the job carries no identity to key it by.
func (inst *Track) writePeaksCache(job buildJob) (err error) {
	if job.identity == nil || job.cachePath == "" {
		return nil
	}
	err = writePeaksFileE(job.cachePath, inst.pyramid, *job.identity)
	if err != nil {
		log.Warn().Err(err).Str("path", job.cachePath).Msg("unable to write the audio peaks cache")
		return err
	}
	log.Debug().
		Str("path", job.cachePath).
		Int64("bytes", inst.pyramid.MemoryBytes()).
		Msg("wrote the audio peaks cache")
	return nil
}

// cacheFileName names the peaks file of one recording at one base bin.
// Half the identity hash is plenty to separate recordings and keeps the name
// readable; the base bin is in the name because a pyramid built at another
// one is a different file rather than a mismatch to be discovered on load.
func cacheFileName(id peaks.Identity, baseBin int32) (name string) {
	return hex.EncodeToString(id.Hash[:16]) + "-b" + strconv.FormatInt(int64(baseBin), 10) + CacheFileExt
}

// readPeaksFileE loads a cached pyramid and checks that it describes the
// recording the caller has open. Every failure — no file, another
// recording's identity, a truncated body, a pyramid of another shape — is a
// cache miss for the caller to build over, never a reason to fail an open.
func readPeaksFileE(path string, id peaks.Identity, format pcm.Format, frames int64, baseBin int32) (p *peaks.Pyramid, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, eh.Errorf("unable to open the peaks cache: %w", err)
	}
	defer func() { _ = f.Close() }()
	p, err = peaks.ReadFromE(f, id)
	if err != nil {
		return nil, err
	}
	if p.Format() != format || p.Frames() != frames || p.BaseBin() != baseBin {
		return nil, eb.Build().
			Int64("cachedFrames", p.Frames()).
			Int64("frames", frames).
			Int32("cachedBaseBin", p.BaseBin()).
			Int32("baseBin", baseBin).
			Errorf("cached pyramid describes another recording")
	}
	return p, nil
}

// writePeaksFileE writes the pyramid to a temporary file in the target's own
// directory and renames it into place, so a reader either finds the previous
// file or the whole new one — never the half of one this call had written
// when it was interrupted. The contents are flushed before the rename for
// the same reason: an all-zero body recovered after a crash is a silent
// waveform, which no reader could tell from a real one.
func writePeaksFileE(path string, p *peaks.Pyramid, id peaks.Identity) (err error) {
	dir := filepath.Dir(path)
	err = os.MkdirAll(dir, 0o755)
	if err != nil {
		return eb.Build().Str("dir", dir).Errorf("unable to create the peaks cache directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return eb.Build().Str("dir", dir).Errorf("unable to create a temporary peaks file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	err = p.WriteToE(tmp, id)
	if err != nil {
		return eb.Build().Str("path", tmp.Name()).Errorf("unable to write the peaks file: %w", err)
	}
	err = tmp.Sync()
	if err != nil {
		return eb.Build().Str("path", tmp.Name()).Errorf("unable to flush the peaks file: %w", err)
	}
	err = tmp.Close()
	if err != nil {
		return eb.Build().Str("path", tmp.Name()).Errorf("unable to close the peaks file: %w", err)
	}
	err = os.Rename(tmp.Name(), path)
	if err != nil {
		return eb.Build().Str("path", path).Errorf("unable to rename the peaks file into place: %w", err)
	}
	return nil
}
