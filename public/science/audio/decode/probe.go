package decode

import (
	"context"
	"encoding/json/v2"
	"math"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
)

// probeArgs asks ffprobe for the three numbers a [pcm.SourceI] needs before
// its first read, and nothing else. The format-level duration is requested as
// well because a container can carry it where the stream does not.
func probeArgs(path string) (args []string) {
	return []string{
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=sample_rate,channels,duration:format=duration",
		"-of", "json",
		path,
	}
}

// probeE runs ffprobe over path and returns the format and frame count of its
// first audio stream (ADR-0208 §SD5).
func probeE(ctx context.Context, path string) (format pcm.Format, frames int64, err error) {
	out, err := extbin.Ffprobe.Output(ctx, extbin.Opts{}, probeArgs(path)...)
	if err != nil {
		return format, 0, eb.Build().Str("path", path).Errorf("probe recording: %w", err)
	}
	format, frames, err = parseProbeE(out)
	if err != nil {
		return format, 0, eb.Build().Str("path", path).Errorf("%w", err)
	}
	return format, frames, nil
}

// probeOutput is the subset of ffprobe's JSON the probe asks for. ffprobe
// prints most numbers as strings and a few (channels) bare, so every numeric
// field goes through [probeNumber].
type probeOutput struct {
	Streams []probeStream `json:"streams"`
	Format  probeFormat   `json:"format"`
}

type probeStream struct {
	SampleRate probeNumber `json:"sample_rate"`
	Channels   probeNumber `json:"channels"`
	Duration   probeNumber `json:"duration"`
}

type probeFormat struct {
	Duration probeNumber `json:"duration"`
}

// probeNumber is a number ffprobe may print quoted, bare, or as the "N/A"
// placeholder it uses for a value the demuxer did not supply.
type probeNumber struct {
	Value   float64
	Present bool
}

// UnmarshalJSON accepts a JSON number, a JSON string holding one, null, and
// ffprobe's "N/A"; the last three leave Present false.
func (inst *probeNumber) UnmarshalJSON(data []byte) (err error) {
	text := strings.TrimSpace(string(data))
	if text == "null" {
		return nil
	}
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		text, err = strconv.Unquote(text)
		if err != nil {
			return eh.Errorf("unable to unquote probed number: %w", err)
		}
		text = strings.TrimSpace(text)
	}
	if text == "" || text == "N/A" {
		return nil
	}
	inst.Value, err = strconv.ParseFloat(text, 64)
	if err != nil {
		return eb.Build().Str("text", text).Errorf("probed value is not a number: %w", err)
	}
	inst.Present = true
	return nil
}

// parseProbeE derives the format and frame count from ffprobe's JSON. The
// sample rate and channel count come from the stream; the frame count is
// round(duration × rate) over the stream's duration, falling back to the
// container's. A duration that is absent or zero is an error: the frame count
// is part of the [pcm.SourceI] contract, so a stream of unknown length has no
// representation here.
func parseProbeE(data []byte) (format pcm.Format, frames int64, err error) {
	var out probeOutput
	err = json.Unmarshal(data, &out)
	if err != nil {
		return format, 0, eh.Errorf("unable to parse probe output: %w", err)
	}
	if len(out.Streams) == 0 {
		return format, 0, eh.New("no audio stream")
	}
	stream := out.Streams[0]

	rate := stream.SampleRate
	if !rate.Present || rate.Value < 1 || rate.Value > float64(math.MaxUint32) {
		return format, 0, eb.Build().
			Bool("present", rate.Present).
			Float64("sampleRate", rate.Value).
			Errorf("probed sample rate is unusable")
	}
	channels := stream.Channels
	if !channels.Present || channels.Value < 1 || channels.Value > float64(math.MaxUint16) {
		return format, 0, eb.Build().
			Bool("present", channels.Present).
			Float64("channels", channels.Value).
			Errorf("probed channel count is unusable")
	}
	format = pcm.Format{
		SampleRate: uint32(math.Round(rate.Value)),
		Channels:   uint16(math.Round(channels.Value)),
	}

	duration := stream.Duration
	if !duration.Present || duration.Value <= 0 {
		duration = out.Format.Duration
	}
	if !duration.Present || duration.Value <= 0 {
		return format, 0, eb.Build().
			Bool("streamDurationPresent", stream.Duration.Present).
			Bool("formatDurationPresent", out.Format.Duration.Present).
			Errorf("probed duration is missing or zero")
	}
	exact := math.Round(duration.Value * float64(format.SampleRate))
	if exact < 0 || exact > float64(math.MaxInt64) {
		return format, 0, eb.Build().
			Float64("durationSeconds", duration.Value).
			Uint32("sampleRate", format.SampleRate).
			Errorf("probed duration does not yield a frame count")
	}
	return format, int64(exact), nil
}
