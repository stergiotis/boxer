package tally

import (
	"bufio"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/decode"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
	"github.com/stergiotis/boxer/public/science/audio/sink/pulsesink"
	"github.com/stergiotis/boxer/public/science/audio/track"
	"github.com/stergiotis/boxer/public/science/audio/wavfile"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/jobprogress"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/waveform"
)

// The store hands out bytes, and both decoders want a file: the native WAV
// reader an [io.ReaderAt], ffmpeg and ffprobe a seekable input they can name
// on their own command line. A snapshot's recording therefore has to be
// staged out of ClickHouse before it can be played, and staging it as a
// plain file would leave a browsed recording lying on disk after the window
// closed — the exact durability the ad-hoc dataset store exists to refuse
// (ADR-0134). Staging reuses that store: its directory, its quotas, its
// AES-GCM chunk format, its keys-in-memory-only rule, and its startup sweep.
//
// Two shapes, because one boundary cannot be crossed with ciphertext:
//
//   - A WAV is sealed into a BXAD file beside the ad-hoc datasets under a key
//     held only in this process. [adhocdata.SeekableReader] gives each reader
//     random access to the plaintext, so nothing ever leaves the process and
//     a crash strands ciphertext whose key is gone.
//   - Everything else is decoded by ffmpeg, a separate process that can read
//     neither our ciphertext nor a stream (ffprobe seeks for the duration).
//     ADR-0134 met the same wall at ClickHouse and answered it by decrypting
//     on our side of the boundary into a kernel object with no name; here
//     that object is a memfd, anonymous memory the decoders reach only as an
//     inherited descriptor ([decode.FdInputI]). It has no name on any
//     filesystem, so nothing outlives the process holding it.

const (
	// audioMaxBytes is the largest recording tally will stage. It is the
	// ad-hoc store's own per-dataset quota, and for the ffmpeg path it also
	// bounds the anonymous memory a staged recording occupies.
	audioMaxBytes int64 = adhocdata.PerDatasetMaxBytes
	// audioStagePrefix names tally's files in the shared store directory, so
	// they are recognisable in a listing and swept with everything else.
	audioStagePrefix = "tally-audio-"
	// audioCopyChunk is the staging copy's buffer.
	audioCopyChunk = 1 << 20
	// audioPlayerHeight and audioMinimapHeight size the two canvases.
	audioPlayerHeight  float32 = 220
	audioMinimapHeight float32 = 36
	// audioFallbackWidth is the width the player draws at before the host has
	// reported one (the first frame of a painter widget, ADR-0204).
	audioFallbackWidth float32 = 800
)

// audioExts routes a file to the player by name. Sniffing decides which
// decoder handles it (decode.Sniff), but not whether tally offers to play it
// at all: staging costs a full read out of the store, so it happens for
// things named like recordings and not for every binary in a snapshot.
var audioExts = map[string]struct{}{
	".wav": {}, ".wave": {}, ".bwf": {}, ".rf64": {},
	".flac": {}, ".mp3": {}, ".ogg": {}, ".oga": {}, ".opus": {},
	".m4a": {}, ".mp4a": {}, ".aac": {}, ".wma": {},
	".aif": {}, ".aiff": {}, ".aifc": {}, ".caf": {},
	".ape": {}, ".wv": {}, ".mka": {}, ".amr": {}, ".au": {}, ".snd": {},
}

// isAudioName reports whether p is named like a recording.
func isAudioName(p string) (yes bool) {
	_, yes = audioExts[strings.ToLower(path.Ext(p))]
	return
}

// stagedRecording is one recording lifted out of the store and held where a
// decoder can read it. Build with [stageRecording]; every reader it hands out
// is independent, and [stagedRecording.closeE] releases the lot.
type stagedRecording struct {
	name string
	kind decode.KindE

	// sealPath and key are the KindWAV shape: a BXAD file under the ad-hoc
	// store directory, and the key that opens it, which exists nowhere else.
	sealPath string
	key      []byte

	// plain is the KindFfmpeg shape: an anonymous file the decoder processes
	// inherit a handle on.
	plain *os.File
}

var _ decode.FdInputI = (*stagedRecording)(nil)

// stageRecording reads p out of fsys and stages it. It runs off the render
// thread — every read is a query — and the returned recording is the
// caller's to close.
func stageRecording(ctx context.Context, fsys fs.FS, p string, size int64) (inst *stagedRecording, err error) {
	if size > audioMaxBytes {
		return nil, eh.Errorf("%s is %s, over the %s a recording may be staged at",
			path.Base(p), humanSize(size), humanSize(audioMaxBytes))
	}
	f, err := fsys.Open(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	// Peeking the container's head classifies the recording without a second
	// read of the store; the reader then still delivers those bytes.
	br := bufio.NewReaderSize(f, audioCopyChunk)
	head, perr := br.Peek(16)
	if perr != nil && len(head) == 0 {
		return nil, eh.Errorf("unable to read %s: %w", path.Base(p), perr)
	}
	inst = &stagedRecording{name: path.Base(p), kind: decode.Sniff(head)}
	if inst.kind == decode.KindWAV {
		err = inst.sealE(ctx, br)
	} else {
		err = inst.holdPlainE(ctx, br)
	}
	if err != nil {
		_ = inst.closeE()
		return nil, err
	}
	return inst, nil
}

// sealE writes the recording as a BXAD file under a fresh key, through a
// temporary name renamed into place — the ad-hoc store's own write shape.
func (inst *stagedRecording) sealE(ctx context.Context, r io.Reader) (err error) {
	dir := adhocdata.ResolveStoreDir()
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return eh.Errorf("unable to prepare the staging directory: %w", err)
	}
	inst.key = make([]byte, adhocdata.KeySize)
	if _, err = rand.Read(inst.key); err != nil {
		return eh.Errorf("unable to mint a staging key: %w", err)
	}
	var suffix [8]byte
	if _, err = rand.Read(suffix[:]); err != nil {
		return eh.Errorf("unable to mint a staging name: %w", err)
	}
	final := filepath.Join(dir, fmt.Sprintf("%s%x.bxad", audioStagePrefix, suffix))
	tmp := final + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return eh.Errorf("unable to create the staging file: %w", err)
	}
	err = func() (err error) {
		w, err := adhocdata.NewWriter(f, inst.key)
		if err != nil {
			return
		}
		if _, err = copyChunked(ctx, w, r); err != nil {
			return
		}
		return w.Close()
	}()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return eb.Build().Str("name", inst.name).Errorf("unable to seal: %w", err)
	}
	if err = os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return eh.Errorf("unable to place the staging file: %w", err)
	}
	inst.sealPath = final
	return nil
}

// holdPlainE copies the recording into anonymous memory for the external
// decoder. The descriptor is the only way to it: the memfd is unnamed, so
// there is nothing on disk to clean up and nothing to find after a crash.
func (inst *stagedRecording) holdPlainE(ctx context.Context, r io.Reader) (err error) {
	fd, err := unix.MemfdCreate(audioStagePrefix+inst.name, unix.MFD_CLOEXEC)
	if err != nil {
		return eh.Errorf("unable to hold %s for the decoder: %w", inst.name, err)
	}
	inst.plain = os.NewFile(uintptr(fd), "memfd:"+inst.name)
	if _, err = copyChunked(ctx, inst.plain, r); err != nil {
		return eb.Build().Str("name", inst.name).Errorf("unable to stage: %w", err)
	}
	return nil
}

// copyChunked is io.Copy with the context checked between buffers, so a
// selection changed mid-staging abandons the read rather than finishing it.
func copyChunked(ctx context.Context, w io.Writer, r io.Reader) (n int64, err error) {
	buf := make([]byte, audioCopyChunk)
	for {
		if err = ctx.Err(); err != nil {
			return n, err
		}
		read, rerr := r.Read(buf)
		if read > 0 {
			written, werr := w.Write(buf[:read])
			n += int64(written)
			if werr != nil {
				return n, werr
			}
		}
		if rerr == io.EOF {
			return n, nil
		}
		if rerr != nil {
			return n, rerr
		}
	}
}

// Name implements [decode.FdInputI].
func (inst *stagedRecording) Name() (s string) { return inst.name }

// OpenE implements [decode.FdInputI]: a fresh handle on the anonymous file
// for one decoder process. Re-opening through the descriptor's own procfs
// entry is what makes it fresh — handing out duplicates of the one handle
// would have two ffmpeg processes sharing a file offset.
func (inst *stagedRecording) OpenE() (f *os.File, err error) {
	if inst.plain == nil {
		return nil, eh.Errorf("%s is not staged for an external decoder", inst.name)
	}
	f, err = os.Open(fmt.Sprintf("/proc/self/fd/%d", inst.plain.Fd()))
	if err != nil {
		return nil, eh.Errorf("unable to re-open the staged %s: %w", inst.name, err)
	}
	return f, nil
}

// openSourceE opens one independent reader over the staged recording. A track
// takes several — the sink, the peaks build, the window cache — and each gets
// its own decoder here.
func (inst *stagedRecording) openSourceE(ctx context.Context) (src pcm.SourceI, err error) {
	if inst.kind == decode.KindWAV {
		return inst.openSealedWavE()
	}
	return decode.OpenFfmpegFdE(ctx, inst)
}

// openSealedWavE reads the WAV out of the sealed file: one file handle, one
// seekable decryptor over it, and the native reader on top.
func (inst *stagedRecording) openSealedWavE() (src pcm.SourceI, err error) {
	f, err := os.Open(inst.sealPath)
	if err != nil {
		return nil, eh.Errorf("unable to open the staged %s: %w", inst.name, err)
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, eh.Errorf("unable to size the staged %s: %w", inst.name, err)
	}
	sr, err := adhocdata.NewSeekableReader(f, st.Size(), inst.key)
	if err != nil {
		_ = f.Close()
		return nil, eb.Build().Str("name", inst.name).Errorf("unable to unseal: %w", err)
	}
	file, err := wavfile.NewReaderE(&sealedReaderAt{r: sr}, sr.PlaintextSize())
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &sealedWav{File: file, seal: f}, nil
}

// closeE releases everything staging took: the sealed file and its key, or
// the anonymous one. It is idempotent.
func (inst *stagedRecording) closeE() (err error) {
	if inst.sealPath != "" {
		if rerr := os.Remove(inst.sealPath); rerr != nil && !os.IsNotExist(rerr) {
			err = rerr
		}
		inst.sealPath = ""
	}
	if inst.key != nil {
		clear(inst.key)
		inst.key = nil
	}
	if inst.plain != nil {
		if cerr := inst.plain.Close(); cerr != nil && err == nil {
			err = cerr
		}
		inst.plain = nil
	}
	return
}

// sealedReaderAt is the [io.ReaderAt] the WAV reader wants over a
// [adhocdata.SeekableReader], which is a ReadSeeker. The mutex is not for
// concurrency the track creates — each reader is single-goroutine, as
// [pcm.SourceI] says — but so that a positioned read is a seek and a read
// together, never interleaved with another.
type sealedReaderAt struct {
	mu sync.Mutex
	r  *adhocdata.SeekableReader
}

func (inst *sealedReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if _, err = inst.r.Seek(off, io.SeekStart); err != nil {
		return 0, err
	}
	n, err = io.ReadFull(inst.r, p)
	if err == io.ErrUnexpectedEOF {
		// The ReaderAt contract spells a short read at the end io.EOF; a
		// caller reading past the plaintext must see the end, not a distinct
		// error it has no rule for.
		err = io.EOF
	}
	return n, err
}

// sealedWav is a WAV source that also owns the sealed file it reads from.
type sealedWav struct {
	*wavfile.File
	seal *os.File
}

// CloseE closes the reader and then the file under it.
func (inst *sealedWav) CloseE() (err error) {
	err = inst.File.CloseE()
	if cerr := inst.seal.Close(); err == nil {
		err = cerr
	}
	return
}

// audioSession is one staged recording open as a playable track. The preview
// lane produces it off the render thread; the app adopts one at a time and
// closes the one it replaces.
type audioSession struct {
	staged *stagedRecording
	tr     *track.Track
	// deviceErr says why playback is silent when the output device could not
	// be opened; empty means it was.
	deviceErr string

	// player is built on the render thread, which owns the id stack.
	player  *waveform.Player
	buildId string
	// wallClock switches the readout between offsets and instants and volume
	// rides the sink; both are bound to widgets, so they live as long as the
	// session does.
	wallClock bool
	volume    float64
}

// openAudioSession stages p and opens it as a track. The sink is the audio
// device where one opens and the device-less clock otherwise, so a host with
// no sound server still gets a moving playhead and a reason on screen.
func openAudioSession(ctx context.Context, fsys fs.FS, p string, size int64, modTime time.Time) (inst *audioSession, err error) {
	staged, err := stageRecording(ctx, fsys, p, size)
	if err != nil {
		return nil, err
	}
	src, err := staged.openSourceE(ctx)
	if err != nil {
		_ = staged.closeE()
		return nil, err
	}
	inst = &audioSession{staged: staged}
	// The store knows one wall-clock fact about a recording: when it was last
	// written. Frame 0 is that instant less the recording's length, which is
	// the epoch the absolute readout counts from (ADR-0208 §SD9); an entry
	// with no usable mtime leaves the readout relative.
	var epoch time.Time
	if !modTime.IsZero() {
		if d := src.Format().FramesToDuration(src.Frames()); d > 0 {
			epoch = modTime.Add(-d)
		}
	}
	opts := track.Options{
		Epoch: epoch,
		Reopen: func(rctx context.Context) (pcm.SourceI, error) {
			return staged.openSourceE(rctx)
		},
		Background: true,
		// The peaks cache is a plaintext derivative of a recording staged
		// precisely so it leaves nothing behind; it is not written.
		NoCache: true,
		NewSink: func(s pcm.SourceI) sink.SinkI {
			out, oerr := pulsesink.OpenE(s, pulsesink.Options{AppName: "boxer tally"})
			if oerr != nil {
				inst.deviceErr = oerr.Error()
				return sink.NewNull(s, nil)
			}
			return out
		},
	}
	tr, err := track.OpenE(ctx, src, opts)
	if err != nil {
		_ = staged.closeE()
		return nil, err
	}
	inst.tr = tr
	return inst, nil
}

// closeE closes the track and then releases the staged bytes.
func (inst *audioSession) closeE() (err error) {
	if inst == nil {
		return nil
	}
	if inst.tr != nil {
		err = inst.tr.CloseE()
		inst.tr = nil
	}
	if inst.staged != nil {
		if serr := inst.staged.closeE(); err == nil {
			err = serr
		}
		inst.staged = nil
	}
	return
}

// reportBuild lists a background peaks build as a keelson task (ADR-0038), so
// it shows in the task monitor with an ETA and can be cancelled there. A host
// without a bus keeps the inline progress bar and nothing else.
func (inst *audioSession) reportBuild(tasks task.TaskApiI) {
	if tasks == nil || inst.tr == nil || inst.buildId != "" {
		return
	}
	h, err := waveform.SpawnBuildTask(context.Background(), tasks, inst.tr, "audio peaks: "+inst.staged.name)
	if err != nil || h == nil {
		return
	}
	inst.buildId = string(h.Id())
}

// renderAudioPreview draws the player for the session the preview lane holds:
// a transport row, the waveform, the minimap under it, the build's progress
// while the peaks are still filling, and a status line. The player is built
// here rather than in the lane because it needs the id stack, which is the
// render thread's.
func (inst *App) renderAudioPreview(s *audioSession) {
	if s == nil || s.tr == nil {
		return
	}
	if s.player == nil {
		s.player = waveform.New(inst.ids, s.tr, waveform.Options{ScopeKey: "tally-audio"})
		s.player.SetReadout(waveform.ReadoutRelative)
		s.volume = 1
		sm := c.CurrentApplicationState.StateManager
		sm.OverrideDatabindingBPtr(&s.wallClock)
		sm.OverrideDatabindingF64Ptr(&s.volume)
		s.reportBuild(inst.tasks)
	}
	p := s.player
	gap := styletokens.GapInline(inst.density)
	for range c.HorizontalTop().KeepIter() {
		playLabel := icons.PhPlay + " Play"
		if p.IsPlaying() {
			playLabel = icons.PhPause + " Pause"
		}
		if c.Button(inst.ids.PrepareStr("audio-play"), c.Atoms().Text(playLabel).Keep()).SendResp().HasPrimaryClicked() {
			p.TogglePlay()
		}
		if c.Button(inst.ids.PrepareStr("audio-start"), c.Atoms().Text(icons.PhSkipBack+" Start").Keep()).SendResp().HasPrimaryClicked() {
			p.SeekTo(0)
		}
		c.AddSpace(gap)
		if c.Button(inst.ids.PrepareStr("audio-fit"), c.Atoms().Text("Fit").Keep()).SendResp().HasPrimaryClicked() {
			p.FitAll()
		}
		if c.Button(inst.ids.PrepareStr("audio-zoom-in"), c.Atoms().Text(icons.PhMagnifyingGlassPlus).Keep()).SendResp().HasPrimaryClicked() {
			p.ZoomBy(4)
		}
		if c.Button(inst.ids.PrepareStr("audio-zoom-out"), c.Atoms().Text(icons.PhMagnifyingGlassMinus).Keep()).SendResp().HasPrimaryClicked() {
			p.ZoomBy(0.25)
		}
		c.AddSpace(gap)
		if c.SliderF64(inst.ids.PrepareStr("audio-volume"), s.volume, 0, 1).Text("volume").SendRespVal(&s.volume).HasChanged() {
			if err := s.tr.Sink().SetVolumeE(s.volume); err != nil {
				inst.log.Warn().Err(err).Msg("tally: volume rejected")
			}
		}
		c.AddSpace(gap)
		// The store dates the file, not the recording, so the absolute
		// readout is only offered where an epoch was derivable.
		if !s.tr.TimeBase().Epoch.IsZero() {
			if c.Checkbox(inst.ids.PrepareStr("audio-wallclock"), s.wallClock, "Wall clock").SendRespVal(&s.wallClock).HasChanged() {
				mode := waveform.ReadoutRelative
				if s.wallClock {
					mode = waveform.ReadoutAbsolute
				}
				p.SetReadout(mode)
			}
		}
	}
	c.AddSpace(styletokens.GapItems(inst.density))
	p.RenderFillWidth(audioPlayerHeight, audioFallbackWidth)
	w, _ := p.Size()
	c.AddSpace(styletokens.GapItems(inst.density))
	p.RenderMinimap(max(w, 1), audioMinimapHeight)

	bp := s.tr.BuildProgress()
	if !bp.Complete {
		c.RequestRepaint()
		frac := float32(0)
		if bp.TotalFrames > 0 {
			frac = float32(float64(bp.BuiltFrames) / float64(bp.TotalFrames))
		}
		note := "reading the recording to draw it"
		if bp.Err != nil {
			note = "build stopped: " + bp.Err.Error()
		}
		if jobprogress.Render(jobprogress.Input{
			Title: "peaks", Fraction: frac, EtaMs: bp.EtaMs, Note: note,
			CancelId: inst.ids.PrepareStr("audio-cancel-build"),
		}) {
			s.tr.CancelBuild()
		}
	}
	if s.deviceErr != "" {
		c.Label("silent: no output device (" + s.deviceErr + ")").Selectable(false).Send()
	}
	state := "paused"
	switch {
	case p.IsPlaying():
		state = "playing"
		c.RequestRepaint()
	case s.tr.Sink().Ended():
		state = "ended"
	}
	format := s.tr.Format()
	c.Label(fmt.Sprintf("%s · %s · %d ch · %d Hz · %s · %s",
		p.FormatOffset(p.Position()), state, format.Channels, format.SampleRate,
		s.tr.Duration().Round(time.Millisecond), s.staged.kind)).Selectable(false).Send()
	c.Label("Click to seek, drag to pan, wheel to scroll, Ctrl+wheel to zoom; Space plays.").Selectable(false).Send()
}
