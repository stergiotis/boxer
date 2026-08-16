package gloss

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func instFor(t *testing.T, name string) InstanceI {
	t.Helper()
	d, declared := Default().ParseColumn(name)
	require.True(t, declared, name)
	require.Empty(t, d.Reason, name)
	require.NotNil(t, d.Instance)
	return d.Instance
}

func num(s string) TextCell { return TextCell{S: s, K: ValueKindNumeric} }
func txt(s string) TextCell { return TextCell{S: s, K: ValueKindText} }

// One golden per inline face (ADR-0186 §Verification).
func TestTemperatureFace(t *testing.T) {
	assert.Equal(t, Inline{Text: "21.5 °C"}, instFor(t, "t@gloss/temperature;unit=C").Inline(num("21.5")))
	assert.Equal(t, Inline{Text: "293.7 K"}, instFor(t, "t@gloss/temperature;unit=K").Inline(num("293.7")))
	assert.Equal(t, Inline{Text: "70.7 °F"}, instFor(t, "t@gloss/temperature; unit=F").Inline(num("70.7")))
	assert.Equal(t, Inline{Text: "-40.0 °C"}, instFor(t, "t@gloss/temperature;unit=C").Inline(num("-40")))

	// The unit is the stored unit and is required; its spelling is
	// case-sensitive.
	d, declared := Default().ParseColumn("t@gloss/temperature")
	require.True(t, declared)
	assert.Nil(t, d.Instance)
	assert.Contains(t, d.Reason, "requires unit=")
	d, _ = Default().ParseColumn("t@gloss/temperature;unit=k")
	assert.Nil(t, d.Instance)
	assert.Contains(t, d.Reason, `unit="k" is not allowed`)
	d, _ = Default().ParseColumn("t@gloss/temperature;unti=K")
	assert.Nil(t, d.Instance)
	assert.Contains(t, d.Reason, "unknown parameter")

	// Kind discipline: a temperature is a number.
	ok, reason := instFor(t, "t@gloss/temperature;unit=C").Accepts(ValueKindText)
	assert.False(t, ok)
	assert.Contains(t, reason, "expects numeric, got text")
	// A cell that will not parse still shows something.
	assert.Equal(t, Inline{Text: "n/a"}, instFor(t, "t@gloss/temperature;unit=C").Inline(txt("n/a")))
}

func TestLengthFace(t *testing.T) {
	m := instFor(t, "h@gloss/length;unit=m")
	assert.Equal(t, "1.83 m", m.Inline(num("1.83")).Text)
	assert.Equal(t, "1.234 km", m.Inline(num("1234")).Text)
	assert.Equal(t, "2.5 cm", m.Inline(num("0.025")).Text)
	assert.Equal(t, "0.4 mm", m.Inline(num("0.0004")).Text)
	assert.Equal(t, "-1.50 m", m.Inline(num("-1.5")).Text, "sign kept, scale by magnitude")
	assert.Equal(t, "1.50 m", instFor(t, "h@gloss/length;unit=cm").Inline(num("150")).Text)
	assert.Equal(t, "3.05 m", instFor(t, "h@gloss/length;unit=ft").Inline(num("10")).Text, "feet convert to SI")
	assert.Equal(t, "12.000 km", instFor(t, "h@gloss/length;unit=km").Inline(num("12")).Text)
	d, _ := Default().ParseColumn("h@gloss/length")
	assert.Contains(t, d.Reason, "requires unit=")
}

func TestBytesFace(t *testing.T) {
	b := instFor(t, "sz@gloss/bytes")
	assert.Equal(t, Inline{Text: "40 KiB"}, b.Inline(num("40858")))
	assert.Equal(t, Inline{Text: "0 B"}, b.Inline(num("0")))
	assert.Equal(t, Inline{Text: "-1", Tone: ToneError}, b.Inline(num("-1")), "a negative size is shown as-is, in the error tone")
}

func TestLuhnFace(t *testing.T) {
	l := instFor(t, "pan@gloss/luhn")
	assert.Equal(t, Inline{Text: "4111 •••• •••• 1111 ✓", Tone: ToneSuccess}, l.Inline(txt("4111111111111111")))
	assert.Equal(t, Inline{Text: "4111 •••• •••• 1112 ✗", Tone: ToneError}, l.Inline(txt("4111 1111 1111 1112")))
	assert.Equal(t, Inline{Text: "3782 •••• •••• 005 ✓", Tone: ToneSuccess}, l.Inline(txt("3782-822463-10005")), "a 15-digit Amex groups from the left")
	assert.Equal(t, Inline{Text: "7992 •••• 713 ✓", Tone: ToneSuccess}, l.Inline(num("79927398713")), "the canonical Luhn example, digits from a numeric cell")
	assert.Equal(t, Inline{Text: "12ab", Tone: ToneWarning}, l.Inline(txt("12ab")), "not a number: shown as-is, warning")
	assert.Equal(t, Inline{Text: "7", Tone: ToneWarning}, l.Inline(txt("7")), "too short to check")
	assert.True(t, LuhnValid("79927398713"))
	assert.False(t, LuhnValid("79927398710"))
}

func TestMaskedFace(t *testing.T) {
	s := instFor(t, "pw@gloss/masked")
	short := s.Inline(txt("a"))
	long := s.Inline(txt("correct horse battery staple"))
	assert.Equal(t, MaskedFace, short.Text)
	assert.Equal(t, short, long, "the mask never reveals the length")
	ok, _ := s.Accepts(ValueKindOther)
	assert.True(t, ok, "anything can be masked")
	assert.Equal(t, []string{`\bsem:secret\b`}, s.Gloss().Affinities(), "the affinity keeps the vocabulary's own aspect name")
}

// gloss/epoch: seconds by default, the resolution by unit=, and the s/ms
// mix-up made loud by the warning tone.
func TestEpochFace(t *testing.T) {
	e := instFor(t, "ts@gloss/epoch")
	assert.Equal(t, Inline{Text: "2026-08-15T12:00:00Z"}, e.Inline(num("1786795200")))
	assert.Equal(t, Inline{Text: "1969-12-31T23:59:59Z"}, e.Inline(num("-1")))
	assert.Equal(t, Inline{Text: "2026-08-15T12:00:00.250Z"}, instFor(t, "ts@gloss/epoch;unit=ms").Inline(num("1786795200250")))
	assert.Equal(t, Inline{Text: "2026-08-15T12:00:00.000250Z"}, instFor(t, "ts@gloss/epoch;unit=us").Inline(num("1786795200000250")))
	assert.Equal(t, Inline{Text: "2026-08-15T12:00:00.000000250Z"}, instFor(t, "ts@gloss/epoch;unit=ns").Inline(num("1786795200000000250")))
	assert.Equal(t, Inline{Text: "2026-08-15T12:00:00Z"}, e.Inline(num("1786795200.5")), "a seconds column shows whole seconds")

	// Milliseconds read as seconds: year 58,000-odd — shown raw, in warning,
	// with the year that gave it away. (The other direction, seconds read
	// as milliseconds, is January 1970: a real moment, shown as such.)
	face := e.Inline(num("1786795200250"))
	assert.Equal(t, ToneWarning, face.Tone)
	assert.Contains(t, face.Text, "1786795200250")
	assert.Equal(t, "1970-01-21T16:19:55.200Z", instFor(t, "ts@gloss/epoch;unit=ms").Inline(num("1786795200")).Text)

	d, _ := Default().ParseColumn("ts@gloss/epoch;unit=sec")
	assert.Contains(t, d.Reason, "not allowed")
	ok, reason := e.Accepts(ValueKindText)
	assert.False(t, ok)
	assert.Contains(t, reason, "expects numeric")
}

// gloss/duration: the stored unit is required; the face picks the two
// largest units that apply.
func TestDurationFace(t *testing.T) {
	ms := instFor(t, "took@gloss/duration;unit=ms")
	assert.Equal(t, "12.3 ms", ms.Inline(num("12.34")).Text)
	assert.Equal(t, "1.50 s", ms.Inline(num("1500")).Text)
	assert.Equal(t, "1m 05s", ms.Inline(num("65000")).Text)
	assert.Equal(t, "1h 02m", ms.Inline(num("3720000")).Text)
	assert.Equal(t, "3d 4h 05m", ms.Inline(num("273900000")).Text)
	assert.Equal(t, "0 s", ms.Inline(num("0")).Text)
	assert.Equal(t, "-1.50 s", ms.Inline(num("-1500")).Text)
	assert.Equal(t, "123 ns", instFor(t, "d@gloss/duration;unit=ns").Inline(num("123")).Text)
	assert.Equal(t, "12.3 µs", instFor(t, "d@gloss/duration;unit=us").Inline(num("12.3")).Text)
	assert.Equal(t, "2h 30m", instFor(t, "d@gloss/duration;unit=min").Inline(num("150")).Text)
	assert.Equal(t, "1d 0h 00m", instFor(t, "d@gloss/duration;unit=h").Inline(num("24")).Text)
	assert.Equal(t, ToneWarning, instFor(t, "d@gloss/duration;unit=h").Inline(num("1e12")).Tone, "past what a Duration holds: raw, warning")

	d, _ := Default().ParseColumn("took@gloss/duration")
	assert.Contains(t, d.Reason, "requires unit=")
}

func TestURLFace(t *testing.T) {
	u := instFor(t, "link@gloss/url")
	assert.Equal(t, Inline{Text: "https://example.com/a", Tone: ToneAccent}, u.Inline(txt("https://example.com/a\nsecond line dropped")))
	ok, _ := u.Accepts(ValueKindNumeric)
	assert.False(t, ok)
	assert.Equal(t, []string{`\bsem:url\b`}, u.Gloss().Affinities())
}

// The catalog order is pinned end to end: content family, then gloss/* with
// raw last (raw's affinity-override role does not depend on it, but the
// reject message lists this order).
func TestDefaultOrderPresentation(t *testing.T) {
	var order []string
	for g := range Default().All() {
		order = append(order, g.MediaType())
	}
	assert.Equal(t, []string{
		MediaTypeTemperature, MediaTypeLength, MediaTypeEpoch, MediaTypeDuration,
		MediaTypeBytes, MediaTypeTaggedId, MediaTypeLuhn, MediaTypeMasked, MediaTypeURL, MediaTypeRaw,
	}, order[8:])
}

// AcceptedKinds is the catalog listing's "accepts:" line: probed once per
// gloss, "any" for a gloss that refuses nothing.
func TestAcceptedKinds(t *testing.T) {
	kinds, all := AcceptedKinds(instFor(t, "t@gloss/temperature;unit=K"))
	assert.Equal(t, []ValueKindE{ValueKindNumeric}, kinds)
	assert.False(t, all)

	kinds, all = AcceptedKinds(instFor(t, "n@text/markdown"))
	assert.Equal(t, []ValueKindE{ValueKindText, ValueKindBytes}, kinds, "the content family: text and bytes, in enum order")
	assert.False(t, all)

	kinds, all = AcceptedKinds(instFor(t, "p@gloss/luhn"))
	assert.Equal(t, []ValueKindE{ValueKindNumeric, ValueKindText}, kinds)
	assert.False(t, all)

	kinds, all = AcceptedKinds(instFor(t, "s@gloss/masked"))
	assert.True(t, all, "masked accepts any kind")
	assert.Len(t, kinds, len(AllValueKinds))
	_, all = AcceptedKinds(instFor(t, "r@gloss/raw"))
	assert.True(t, all)
}
