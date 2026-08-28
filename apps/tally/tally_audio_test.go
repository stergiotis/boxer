package tally

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/science/audio/decode"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/wavfile"
)

// audioFixtureFormat and audioFixtureFrames are a short stereo tone: long
// enough to span several BXAD chunks, short enough to compare whole.
var audioFixtureFormat = pcm.Format{SampleRate: 8000, Channels: 2}

const audioFixtureFrames int64 = 24000

// writeWavFixture writes a 16-bit PCM WAV into dir and returns its bytes.
func writeWavFixture(t *testing.T, dir string, name string) (path string, want []float32) {
	t.Helper()
	src, err := pcm.NewSynthSourceE(audioFixtureFormat, audioFixtureFrames,
		pcm.Sine(audioFixtureFormat, 440, 0.7))
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, wavfile.WriteE(context.Background(), &buf, audioFixtureFormat, wavfile.EncodingPCMInt, 16, src))
	path = filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))

	file, err := wavfile.OpenE(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, file.CloseE()) }()
	want = make([]float32, audioFixtureFrames*int64(audioFixtureFormat.Channels))
	n, err := file.ReadFramesAtE(context.Background(), 0, want)
	require.NoError(t, err)
	require.Equal(t, int(audioFixtureFrames), n)
	return path, want
}

func TestIsAudioName(t *testing.T) {
	assert.True(t, isAudioName("takes/interview.WAV"))
	assert.True(t, isAudioName("a.flac"))
	assert.True(t, isAudioName("a.opus"))
	assert.False(t, isAudioName("a.wavelet"))
	assert.False(t, isAudioName("notes.md"))
	assert.Equal(t, previewKindAudio, classifyByName("takes/interview.wav"))
}

// TestStagedWavIsSealedAndReadsBack is the sealed half of staging: the file
// tally leaves on disk is ciphertext, the source it opens over it delivers
// the recording's own samples, and closing takes the file with it.
func TestStagedWavIsSealedAndReadsBack(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(adhocdata.StoreDir.Spec().Name, filepath.Join(dir, "store"))
	path, want := writeWavFixture(t, dir, "tone.wav")
	info, err := os.Stat(path)
	require.NoError(t, err)

	staged, err := stageRecording(context.Background(), os.DirFS(dir), "tone.wav", info.Size())
	require.NoError(t, err)
	require.Equal(t, decode.KindWAV, staged.kind, "a WAV is decoded in-process, so it is sealed")

	sealed, err := os.ReadFile(staged.sealPath)
	require.NoError(t, err)
	assert.False(t, bytes.Contains(sealed, []byte("RIFF")), "the staged file is ciphertext, not the recording")
	assert.False(t, bytes.Contains(sealed, staged.key), "a key is never written beside what it opens")
	assert.Equal(t, "BXAD", string(sealed[:4]), "the ad-hoc store's own format")

	src, err := staged.openSourceE(context.Background())
	require.NoError(t, err)
	require.Equal(t, audioFixtureFormat, src.Format())
	require.Equal(t, audioFixtureFrames, src.Frames())
	got := make([]float32, len(want))
	n, err := src.ReadFramesAtE(context.Background(), 0, got)
	require.NoError(t, err)
	require.Equal(t, int(audioFixtureFrames), n)
	assert.Equal(t, want, got)

	// A track opens several readers over one staged recording; a second must
	// not disturb the first, and a positioned read must land where it says.
	other, err := staged.openSourceE(context.Background())
	require.NoError(t, err)
	const at, span = 10000, 512
	channels := int64(audioFixtureFormat.Channels)
	buf := make([]float32, span*channels)
	n, err = other.ReadFramesAtE(context.Background(), at, buf)
	require.NoError(t, err)
	require.Equal(t, span, n)
	assert.Equal(t, want[at*channels:(at+span)*channels], buf)
	require.NoError(t, other.CloseE())
	require.NoError(t, src.CloseE())

	sealPath := staged.sealPath
	require.NoError(t, staged.closeE())
	_, err = os.Stat(sealPath)
	assert.True(t, os.IsNotExist(err), "closing a staged recording removes it")
	assert.Nil(t, staged.key, "and forgets its key")
	assert.NoError(t, staged.closeE(), "closing twice is not an error")
}

// TestStagedRecordingRefusesAnOversizeFile keeps the store's quota where the
// ad-hoc datasets put it: refused at staging with the sizes named, never
// discovered part-way through a read.
func TestStagedRecordingRefusesAnOversizeFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(adhocdata.StoreDir.Spec().Name, filepath.Join(dir, "store"))
	_, err := stageRecording(context.Background(), os.DirFS(dir), "huge.wav", audioMaxBytes+1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "over the")
	entries, rerr := os.ReadDir(filepath.Join(dir, "store"))
	assert.True(t, os.IsNotExist(rerr) || len(entries) == 0, "a refused recording stages nothing")
}

// TestStagedCompressedRecordingIsAnonymous is the ffmpeg half: the plaintext
// an external decoder must be able to seek is held in an unnamed object, and
// the staging directory stays empty.
func TestStagedCompressedRecordingIsAnonymous(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "store")
	t.Setenv(adhocdata.StoreDir.Spec().Name, store)
	// Any non-WAV header routes to the external decoder; opening it as a
	// track would need ffmpeg, staging it does not.
	body := append([]byte("fLaC\x00\x00\x00\x22"), bytes.Repeat([]byte{0x5a}, 4096)...)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tone.flac"), body, 0o600))

	staged, err := stageRecording(context.Background(), os.DirFS(dir), "tone.flac", int64(len(body)))
	require.NoError(t, err)
	defer func() { require.NoError(t, staged.closeE()) }()
	require.Equal(t, decode.KindFfmpeg, staged.kind)
	assert.Empty(t, staged.sealPath, "nothing is written for the external decoder")
	if entries, rerr := os.ReadDir(store); rerr == nil {
		assert.Empty(t, entries, "the staging directory stays empty")
	}

	// The decoder's view of it: a fresh handle each time, holding the whole
	// recording, each with an offset of its own.
	first, err := staged.OpenE()
	require.NoError(t, err)
	defer func() { _ = first.Close() }()
	head := make([]byte, 4)
	_, err = first.Read(head)
	require.NoError(t, err)
	assert.Equal(t, "fLaC", string(head))
	second, err := staged.OpenE()
	require.NoError(t, err)
	defer func() { _ = second.Close() }()
	_, err = second.Read(head)
	require.NoError(t, err)
	assert.Equal(t, "fLaC", string(head), "a second handle starts at the beginning, not where the first stopped")
	st, err := second.Stat()
	require.NoError(t, err)
	assert.Equal(t, int64(len(body)), st.Size())
}

// TestPreviewLaneClosesTheRecordingItReplaces pins the ownership rule: the
// lane is the only holder of an open recording, so a new selection releases
// the previous one rather than leaking its decoders and its device.
func TestPreviewLaneClosesTheRecordingItReplaces(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(adhocdata.StoreDir.Spec().Name, filepath.Join(dir, "store"))
	path, _ := writeWavFixture(t, dir, "tone.wav")
	info, err := os.Stat(path)
	require.NoError(t, err)

	app := newApp()
	stage := func(key string) (sealPath string) {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			content, done, lerr, busy := app.preview.demand(key, func(ctx context.Context) (previewContent, error) {
				staged, serr := stageRecording(ctx, os.DirFS(dir), "tone.wav", info.Size())
				if serr != nil {
					return previewContent{}, serr
				}
				return previewContent{kind: previewKindAudio, audio: &audioSession{staged: staged}}, nil
			})
			if busy || !done {
				time.Sleep(time.Millisecond)
				continue
			}
			require.NoError(t, lerr)
			require.NotNil(t, content.audio)
			return content.audio.staged.sealPath
		}
		t.Fatalf("the lane never settled on %s", key)
		return ""
	}
	first := stage("a")
	require.FileExists(t, first)
	second := stage("b")
	require.NotEqual(t, first, second)
	assert.NoFileExists(t, first, "the replaced recording is released")

	app.preview.close()
	assert.NoFileExists(t, second, "and so is the one held at unmount")
}
