package stevedore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/streaming/stevedore/stevedorevocab"
)

func TestClassOfInnermostWins(t *testing.T) {
	base := eh.Errorf("boom")
	require.Equal(t, ClassTransient, ClassOf(base))
	require.Equal(t, ClassPermanent, ClassOf(Permanent(base)))
	require.Equal(t, ClassPermanent, ClassOf(eh.Errorf("outer: %w", Permanent(base))))
	require.Equal(t, ClassPermanent, ClassOf(Transient(eh.Errorf("outer: %w", Permanent(base)))))
	require.Nil(t, Permanent(nil))
	require.Nil(t, Transient(nil))
}

func TestStatusTextRoundTrip(t *testing.T) {
	require.Equal(t, "", StatusText(nil))
	class, msg, ok := ParseStatus("")
	require.True(t, ok)
	require.Equal(t, "", msg)
	require.Equal(t, ClassTransient, class)

	s := StatusText(Permanent(eh.Errorf("bad\nbody")))
	require.Equal(t, "permanent: bad body", s)
	class, msg, ok = ParseStatus(s)
	require.False(t, ok)
	require.Equal(t, ClassPermanent, class)
	require.Equal(t, "bad body", msg)

	class, msg, ok = ParseStatus("something else")
	require.False(t, ok)
	require.Equal(t, ClassTransient, class)
	require.Equal(t, "something else", msg)
}

func TestRequestRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		req := Request{
			Origin:     rapid.String().Draw(rt, "origin"),
			Hint:       rapid.String().Draw(rt, "hint"),
			Attributes: rapid.MapOf(rapid.String(), rapid.String()).Draw(rt, "attrs"),
			Split:      rapid.Bool().Draw(rt, "split"),
			Part:       rapid.Uint32().Draw(rt, "part"),
			Parts:      rapid.Uint32().Draw(rt, "parts"),
			Last:       rapid.Bool().Draw(rt, "last"),
			Body:       rapid.SliceOf(rapid.Byte()).Draw(rt, "body"),
		}
		if len(req.Attributes) == 0 {
			req.Attributes = nil
		}
		frame, err := EncodeRequest(req)
		require.NoError(rt, err)
		got, err := DecodeRequest(frame)
		require.NoError(rt, err)
		if len(got.Body) == 0 {
			got.Body = req.Body
		}
		require.Equal(rt, req, got)
	})
}

func TestRequestRefusals(t *testing.T) {
	_, err := DecodeRequest([]byte("not an archive"))
	require.Equal(t, ClassPermanent, ClassOf(err))
	_, err = DecodeRequest([]byte("\x00\x00\x00\x00"))
	require.ErrorIs(t, err, ErrRequestShape)
	_, err = DecodeRequest([]byte("\x00\x00\x00\x02\x00\x00\x00\x01{\x00\x00\x00\x00"))
	require.Equal(t, ClassPermanent, ClassOf(err))
}

func TestReferenceIsRepeatableAndTagged(t *testing.T) {
	a := ReferenceOf(Request{Origin: "t/0/7", Body: []byte("x")})
	b := ReferenceOf(Request{Origin: "t/0/7", Body: []byte("y")})
	require.Equal(t, a, b, "the origin decides, not the body")
	require.NotEqual(t, a, ReferenceOf(Request{Origin: "t/0/8"}))
	require.True(t, a.IsValid())
	require.True(t, stevedorevocab.TagValueClaim.Tag().SameTag(a))
	c := ReferenceOf(Request{Body: []byte("x")})
	require.NotEqual(t, c, ReferenceOf(Request{Body: []byte("y")}), "without an origin the body decides")
	require.True(t, stevedorevocab.TagValueClaim.Tag().SameTag(c))
}

func TestItemRoundTrip(t *testing.T) {
	item := Item{
		Ref: ReferenceOf(Request{Origin: "o"}), Origin: "o", Ordinal: 3, Line: 12, Offset: 400,
		Split: true, Part: 1, Parts: 2, Last: true, PayloadKind: "demoLine", Payload: []byte("hello"),
	}
	b, err := EncodeItem(item)
	require.NoError(t, err)
	got, err := DecodeItem(b)
	require.NoError(t, err)
	require.Equal(t, item, got)

	_, err = DecodeItem([]byte("garbage"))
	require.Equal(t, ClassPermanent, ClassOf(err))
}

func TestRetry(t *testing.T) {
	pol := RetryPolicy{Attempts: 3, Base: time.Millisecond, Max: time.Millisecond}
	t.Run("transient is retried then exhausted", func(t *testing.T) {
		n := 0
		err := Retry(context.Background(), pol, func(context.Context) error { n++; return eh.Errorf("flaky") })
		require.Error(t, err)
		require.Equal(t, 3, n)
		require.Equal(t, ClassTransient, ClassOf(err))
	})
	t.Run("permanent stops at once", func(t *testing.T) {
		n := 0
		err := Retry(context.Background(), pol, func(context.Context) error { n++; return Permanent(eh.Errorf("no")) })
		require.Equal(t, 1, n)
		require.Equal(t, ClassPermanent, ClassOf(err))
	})
	t.Run("success after a failure", func(t *testing.T) {
		n := 0
		err := Retry(context.Background(), pol, func(context.Context) error {
			n++
			if n < 2 {
				return eh.Errorf("flaky")
			}
			return nil
		})
		require.NoError(t, err)
		require.Equal(t, 2, n)
	})
	t.Run("cancelled context stops the wait", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := Retry(ctx, RetryPolicy{Attempts: 5, Base: time.Hour, Max: time.Hour}, func(context.Context) error { return eh.Errorf("flaky") })
		require.Error(t, err)
		require.True(t, errors.Is(ctx.Err(), context.Canceled))
	})
	t.Run("delay is bounded", func(t *testing.T) {
		p := RetryPolicy{Base: time.Second, Max: 5 * time.Second, Jitter: 0.5}
		for n := uint32(1); n < 10; n++ {
			d := p.Delay(n)
			require.LessOrEqual(t, d, 5*time.Second)
			require.GreaterOrEqual(t, d, 0*time.Second)
		}
	})
}
