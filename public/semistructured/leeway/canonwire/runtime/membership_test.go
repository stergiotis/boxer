package runtime

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// allChannels lists the eight membership channels in ordinal order. The
// generator side keeps the same list as canonwire.AllMembershipChannels, next
// to the spec mapping that needs it; the runtime only needs it to enumerate
// the wire forms in a test.
var allChannels = []mappingplan.MembershipChannel{
	mappingplan.MembershipChannelLowCardRef,
	mappingplan.MembershipChannelLowCardVerbatim,
	mappingplan.MembershipChannelHighCardRef,
	mappingplan.MembershipChannelHighCardVerbatim,
	mappingplan.MembershipChannelMixedLowCardRef,
	mappingplan.MembershipChannelMixedLowCardVerbatim,
	mappingplan.MembershipChannelLowCardRefParametrized,
	mappingplan.MembershipChannelHighCardRefParametrized,
}

// unknownChannel is the first ordinal past the eight.
var unknownChannel = mappingplan.MembershipChannel(len(allChannels))

// encMembership returns the hex of one membership item.
func encMembership(t require.TestingT, ch mappingplan.MembershipChannel, ref uint64, verbatim []byte, params []byte) string {
	var b bytes.Buffer
	cw, err := NewCborWriter(&b)
	require.NoError(t, err)
	cw.WriteMembership(ch, ref, verbatim, params)
	require.NoError(t, cw.Err())
	return hex.EncodeToString(b.Bytes())
}

// One vector per channel. The outer array is always two elements — the channel
// ordinal and the identity — and the carrier shapes nest inside the second.
func TestWriteMembershipAllChannels(t *testing.T) {
	cases := []struct {
		name     string
		ch       mappingplan.MembershipChannel
		ref      uint64
		verbatim []byte
		params   []byte
		want     string
	}{
		{
			name: "lowCardRef is [0, ref]",
			ch:   mappingplan.MembershipChannelLowCardRef,
			ref:  7,
			want: "820007",
		},
		{
			name:     "lowCardVerbatim is [1, name]",
			ch:       mappingplan.MembershipChannelLowCardVerbatim,
			verbatim: []byte("ab"),
			want:     "8201426162",
		},
		{
			name: "highCardRef is [2, ref] with the shortest argument",
			ch:   mappingplan.MembershipChannelHighCardRef,
			ref:  300,
			want: "820219012c",
		},
		{
			name:     "highCardVerbatim keeps an empty name",
			ch:       mappingplan.MembershipChannelHighCardVerbatim,
			verbatim: []byte{},
			want:     "820340",
		},
		{
			name:   "mixedLowCardRef is [4, [id, params]]",
			ch:     mappingplan.MembershipChannelMixedLowCardRef,
			ref:    5,
			params: []byte{0xde, 0xad},
			want:   "8204820542dead",
		},
		{
			name:     "mixedLowCardVerbatim is [5, [name, params]]",
			ch:       mappingplan.MembershipChannelMixedLowCardVerbatim,
			verbatim: []byte("x"),
			params:   []byte("y"),
			want:     "82058241784179",
		},
		{
			name:   "lowCardRefParametrized is [6, [params]]",
			ch:     mappingplan.MembershipChannelLowCardRefParametrized,
			params: []byte{0x01},
			want:   "8206814101",
		},
		{
			name:   "highCardRefParametrized is [7, [params]]",
			ch:     mappingplan.MembershipChannelHighCardRefParametrized,
			params: []byte{},
			want:   "82078140",
		},
	}
	require.Len(t, cases, len(allChannels))
	for _, tc := range cases {
		require.Equal(t, tc.want, encMembership(t, tc.ch, tc.ref, tc.verbatim, tc.params), tc.name)
	}
}

// The arguments a channel does not use are ignored rather than rejected: a ref
// channel writes no params even when the caller passes some.
func TestWriteMembershipIgnoresUnusedArguments(t *testing.T) {
	require.Equal(t,
		encMembership(t, mappingplan.MembershipChannelLowCardRef, 7, nil, nil),
		encMembership(t, mappingplan.MembershipChannelLowCardRef, 7, []byte("ignored"), []byte("ignored")))
}

func TestWriteMembershipRejectsUnknownChannel(t *testing.T) {
	var b bytes.Buffer
	cw, err := NewCborWriter(&b)
	require.NoError(t, err)
	cw.WriteMembership(unknownChannel, 0, nil, nil)
	require.ErrorIs(t, cw.Err(), ErrUnknownChannel)
	require.Zero(t, b.Len())
}

// The vectors decode as the shapes SD4 names.
func TestWriteMembershipDecodesAsDeclared(t *testing.T) {
	raw, err := hex.DecodeString(encMembership(t, mappingplan.MembershipChannelMixedLowCardVerbatim, 0, []byte("n"), []byte("p")))
	require.NoError(t, err)
	var got any
	require.NoError(t, cbor.Unmarshal(raw, &got))
	require.Equal(t, []any{
		uint64(mappingplan.MembershipChannelMixedLowCardVerbatim),
		[]any{[]byte("n"), []byte("p")},
	}, got)

	raw, err = hex.DecodeString(encMembership(t, mappingplan.MembershipChannelHighCardRef, 300, nil, nil))
	require.NoError(t, err)
	require.NoError(t, cbor.Unmarshal(raw, &got))
	require.Equal(t, []any{uint64(mappingplan.MembershipChannelHighCardRef), uint64(300)}, got)
}
