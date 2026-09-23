package host

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/streaming/stevedore"
	"github.com/stergiotis/boxer/public/streaming/stevedore/wire"
)

// lineHandler is the contract test's application: one item per line; a
// body starting with "bad" is refused, "panic" panics, "slow" outlives any
// deadline, and "flaky" fails transiently until the counter runs out.
type lineHandler struct {
	flaky atomic.Int32
	calls atomic.Int32
}

func (inst *lineHandler) Handle(ctx context.Context, req stevedore.Request, emit func(stevedore.Emit) error) error {
	inst.calls.Add(1)
	body := string(req.Body)
	switch {
	case strings.HasPrefix(body, "bad"):
		return stevedore.Permanentf("body is bad")
	case strings.HasPrefix(body, "panic"):
		panic("handler bug")
	case strings.HasPrefix(body, "slow"):
		<-ctx.Done()
		return ctx.Err()
	case strings.HasPrefix(body, "flaky"):
		if inst.flaky.Add(-1) >= 0 {
			return eh.Errorf("not yet")
		}
	}
	zerolog.Ctx(ctx).Info().Msg("splitting")
	offset := uint64(0)
	for i, line := range strings.Split(body, "\n") {
		err := emit(stevedore.Emit{PayloadKind: "demoLine", Payload: []byte(line), Line: uint64(i + 1), Offset: offset})
		if err != nil {
			return err
		}
		offset += uint64(len(line) + 1)
	}
	return nil
}

func request(t *testing.T, origin string, body string) []byte {
	t.Helper()
	f, err := stevedore.EncodeRequest(stevedore.Request{Origin: origin, Body: []byte(body)})
	require.NoError(t, err)
	return f
}

func frames(t *testing.T, codec wire.CodecE, reqs ...[]byte) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	w := wire.NewFrameWriter(&buf, codec)
	for _, r := range reqs {
		require.NoError(t, w.Write(r))
	}
	return &buf
}

func readAll(t *testing.T, codec wire.CodecE, b []byte) (out [][]byte) {
	t.Helper()
	r := wire.NewFrameReader(bytes.NewReader(b), codec, 1<<20)
	for {
		f, err := r.Read()
		if err == io.EOF {
			return
		}
		require.NoError(t, err)
		out = append(out, f)
	}
}

func items(t *testing.T, payload []byte) (out []stevedore.Item) {
	t.Helper()
	parts, err := wire.DeserializeParts(payload)
	require.NoError(t, err)
	for _, p := range parts {
		it, err := stevedore.DecodeItem(p)
		require.NoError(t, err)
		out = append(out, it)
	}
	return
}

func TestStdoutModeFanOutAndFailures(t *testing.T) {
	for _, codec := range []wire.CodecE{wire.CodecLengthPrefixedUint32BE, wire.CodecNetstring} {
		t.Run(codec.String(), func(t *testing.T) {
			h := &lineHandler{}
			h.flaky.Store(1)
			in := frames(t, codec,
				request(t, "t/0/1", "a\nb\nc"),
				request(t, "t/0/2", "bad body"),
				request(t, "t/0/3", "flaky once"),
				request(t, "t/0/4", "panic now"),
			)
			var stdout, stderr, logs bytes.Buffer
			cfg := Config{Codec: codec, Reply: ReplyStdout, LogLevel: zerolog.InfoLevel, LogOutput: &logs,
				Retry: stevedore.RetryPolicy{Attempts: 2, Base: time.Millisecond, Max: time.Millisecond}}
			require.NoError(t, Run(context.Background(), cfg, h, in, &stdout, &stderr))

			replies := readAll(t, codec, stdout.Bytes())
			require.Len(t, replies, 2, "two successes reply on stdout")
			got := items(t, replies[0])
			require.Len(t, got, 3)
			require.Equal(t, "t/0/1", got[0].Origin)
			require.Equal(t, uint64(0), got[0].Ordinal)
			require.Equal(t, uint64(2), got[2].Ordinal)
			require.Equal(t, uint64(3), got[2].Line)
			require.Equal(t, "c", string(got[2].Payload))
			require.Equal(t, got[0].Ref, got[2].Ref)
			require.NotEqual(t, got[0].Ref, items(t, replies[1])[0].Ref)

			lines := strings.Split(strings.TrimRight(stderr.String(), "\n"), "\n")
			require.Len(t, lines, 2, "two failures, one line each")
			require.True(t, strings.HasPrefix(lines[0], "permanent: "), lines[0])
			require.Contains(t, lines[0], "body is bad")
			require.True(t, strings.HasPrefix(lines[1], "permanent: "), lines[1])
			require.Contains(t, lines[1], "handler panicked")
			require.Equal(t, int32(5), h.calls.Load(), "the flaky request was retried once")
			require.Contains(t, logs.String(), `"splitting"`)
			require.NotContains(t, stdout.String(), "splitting", "logs never reach stdout in this mode")
		})
	}
}

func TestThreeFrameMode(t *testing.T) {
	codec := wire.CodecNetstring
	h := &lineHandler{}
	in := frames(t, codec, request(t, "o", "x\ny"), request(t, "o", "bad"))
	var stdout, stderr bytes.Buffer
	cfg := Config{Codec: codec, Reply: ReplyThreeFrame, LogLevel: zerolog.InfoLevel}
	require.NoError(t, Run(context.Background(), cfg, h, in, &stdout, &stderr))
	require.Zero(t, stderr.Len(), "stderr stays untouched")
	fr := readAll(t, codec, stdout.Bytes())
	require.Len(t, fr, 6, "three frames per request")
	require.Equal(t, "", string(fr[0]))
	require.Len(t, items(t, fr[1]), 2)
	require.Contains(t, string(fr[2]), `"splitting"`)
	require.True(t, strings.HasPrefix(string(fr[3]), "permanent: "))
	require.Equal(t, "", string(fr[4]))
	require.Contains(t, string(fr[5]), `"level":"error"`)
}

func TestRedeliveryIsByteIdentical(t *testing.T) {
	codec := wire.CodecLengthPrefixedUint32BE
	req := request(t, "t/3/99", "one\ntwo")
	run := func() []byte {
		var stdout, stderr bytes.Buffer
		require.NoError(t, Run(context.Background(), Config{Codec: codec, LogLevel: zerolog.InfoLevel}, &lineHandler{}, frames(t, codec, req), &stdout, &stderr))
		return stdout.Bytes()
	}
	require.Equal(t, run(), run())
}

func TestBounds(t *testing.T) {
	codec := wire.CodecLengthPrefixedUint32BE
	t.Run("oversize body is refused before the handler runs", func(t *testing.T) {
		h := &lineHandler{}
		in := frames(t, codec, request(t, "o", strings.Repeat("x", 100)))
		var stdout, stderr bytes.Buffer
		require.NoError(t, Run(context.Background(), Config{Codec: codec, MaxBody: 16, LogLevel: zerolog.InfoLevel}, h, in, &stdout, &stderr))
		require.Zero(t, h.calls.Load())
		require.Contains(t, stderr.String(), "permanent: ")
		require.Zero(t, stdout.Len())
	})
	t.Run("oversize frame still gets a reply", func(t *testing.T) {
		in := frames(t, codec, request(t, "o", strings.Repeat("x", 2000)), request(t, "o", "ok"))
		var stdout, stderr bytes.Buffer
		require.NoError(t, Run(context.Background(), Config{Codec: codec, MaxFrame: 1024, LogLevel: zerolog.InfoLevel}, &lineHandler{}, in, &stdout, &stderr))
		require.Contains(t, stderr.String(), "permanent: ")
		require.Len(t, readAll(t, codec, stdout.Bytes()), 1, "the second request was served")
	})
	t.Run("oversize reply is refused", func(t *testing.T) {
		in := frames(t, codec, request(t, "o", strings.Repeat("x\n", 40)))
		var stdout, stderr bytes.Buffer
		require.NoError(t, Run(context.Background(), Config{Codec: codec, MaxFrame: 1024, LogLevel: zerolog.InfoLevel}, &lineHandler{}, in, &stdout, &stderr))
		require.Contains(t, stderr.String(), "reply exceeds")
	})
	t.Run("deadline yields a transient status", func(t *testing.T) {
		in := frames(t, codec, request(t, "o", "slow"))
		var stdout, stderr bytes.Buffer
		cfg := Config{Codec: codec, Deadline: 20 * time.Millisecond, LogLevel: zerolog.InfoLevel,
			Retry: stevedore.RetryPolicy{Attempts: 1}}
		start := time.Now()
		require.NoError(t, Run(context.Background(), cfg, &lineHandler{}, in, &stdout, &stderr))
		require.Less(t, time.Since(start), 2*time.Second)
		require.True(t, strings.HasPrefix(stderr.String(), "transient: "), stderr.String())
	})
}

func TestBareBody(t *testing.T) {
	codec := wire.CodecLengthPrefixedUint32BE
	in := frames(t, codec, []byte("p\nq"))
	var stdout, stderr bytes.Buffer
	require.NoError(t, Run(context.Background(), Config{Codec: codec, BareBody: true, LogLevel: zerolog.InfoLevel}, &lineHandler{}, in, &stdout, &stderr))
	got := items(t, readAll(t, codec, stdout.Bytes())[0])
	require.Len(t, got, 2)
	require.Equal(t, "", got[0].Origin)
	require.Equal(t, stevedore.ReferenceOf(stevedore.Request{Body: []byte("p\nq")}), got[0].Ref)
}

func TestLinesCodecIsRefused(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), Config{Codec: wire.CodecLines}, &lineHandler{}, bytes.NewBufferString("x\n"), &stdout, &stderr)
	require.Error(t, err)
	require.Zero(t, stdout.Len())
}

func TestRunEndsWithTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	err := Run(ctx, Config{}, &lineHandler{}, bytes.NewBufferString("\x00\x00\x00\x01x"), &stdout, &stderr)
	require.ErrorIs(t, err, context.Canceled)
}
