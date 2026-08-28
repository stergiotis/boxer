package runtime

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
)

// Every channel's identity form survives the round trip, and the arguments the
// channel does not carry come back zero.
func TestReadMembershipRoundTripsEveryChannel(t *testing.T) {
	cases := []struct {
		ch       mappingplan.MembershipChannel
		ref      uint64
		verbatim []byte
		params   []byte
	}{
		{ch: mappingplan.MembershipChannelLowCardRef, ref: 7},
		{ch: mappingplan.MembershipChannelLowCardVerbatim, verbatim: []byte("ab")},
		{ch: mappingplan.MembershipChannelHighCardRef, ref: 300},
		{ch: mappingplan.MembershipChannelHighCardVerbatim, verbatim: []byte{}},
		{ch: mappingplan.MembershipChannelMixedLowCardRef, ref: 5, params: []byte{0xde, 0xad}},
		{ch: mappingplan.MembershipChannelMixedLowCardVerbatim, verbatim: []byte("x"), params: []byte("y")},
		{ch: mappingplan.MembershipChannelLowCardRefParametrized, params: []byte{0x01}},
		{ch: mappingplan.MembershipChannelHighCardRefParametrized, params: []byte{}},
	}
	require.Len(t, cases, len(allChannels))
	for _, tc := range cases {
		var b bytes.Buffer
		cw, err := NewCborWriter(&b)
		require.NoError(t, err)
		cw.WriteMembership(tc.ch, tc.ref, tc.verbatim, tc.params)
		require.NoError(t, cw.Err())

		r := NewCborReader(b.Bytes())
		ch, ref, verbatim, params := r.ReadMembership()
		require.NoError(t, r.Err(), tc.ch.String())
		require.Zero(t, r.Remaining(), tc.ch.String())
		require.Equal(t, tc.ch, ch)

		// Only the fields the channel's identity encoding carries come back.
		switch tc.ch.Identity() {
		case membership.IdentityRef:
			require.Equal(t, tc.ref, ref)
			require.Nil(t, verbatim)
			require.Nil(t, params)
		case membership.IdentityVerbatim:
			require.Equal(t, tc.verbatim, verbatim)
			require.Zero(t, ref)
			require.Nil(t, params)
		case membership.IdentityPerRowId:
			require.Equal(t, tc.ref, ref)
			require.Equal(t, tc.params, params)
			require.Nil(t, verbatim)
		case membership.IdentityPerRowName:
			require.Equal(t, tc.verbatim, verbatim)
			require.Equal(t, tc.params, params)
			require.Zero(t, ref)
		case membership.IdentityPerRowBlob:
			require.Equal(t, tc.params, params)
			require.Zero(t, ref)
			require.Nil(t, verbatim)
		default:
			t.Fatalf("channel %v has no identity encoding", tc.ch)
		}

		// And re-writing what was read reproduces the bytes.
		var b2 bytes.Buffer
		cw2, err := NewCborWriter(&b2)
		require.NoError(t, err)
		cw2.WriteMembership(ch, ref, verbatim, params)
		require.NoError(t, cw2.Err())
		require.Equal(t, hex.EncodeToString(b.Bytes()), hex.EncodeToString(b2.Bytes()))
	}
}

func TestReadMembershipRejectsMalformedItems(t *testing.T) {
	cases := []struct {
		name string
		hex  string
		is   error
	}{
		{name: "not an array", hex: "00", is: ErrUnexpectedMajor},
		{name: "wrong element count", hex: "83000000", is: ErrOutOfRange},
		{name: "unknown channel ordinal", hex: "820800", is: ErrUnknownChannel},
		{name: "channel ordinal past a byte", hex: "82190100 00", is: ErrUnknownChannel},
		// A ref channel with a carrier payload, and a carrier channel with a
		// bare ref: the shape must match the channel's identity encoding.
		{name: "ref channel with a carrier payload", hex: "8200820001", is: ErrUnexpectedMajor},
		{name: "carrier channel with a bare ref", hex: "820401", is: ErrUnexpectedMajor},
		{name: "carrier array of the wrong length", hex: "820481410a", is: ErrOutOfRange},
		{name: "blob channel with two elements", hex: "8206824100 4100", is: ErrOutOfRange},
	}
	for _, tc := range cases {
		raw, err := hex.DecodeString(strip(tc.hex))
		require.NoError(t, err, tc.name)
		r := NewCborReader(raw)
		r.ReadMembership()
		require.ErrorIs(t, r.Err(), tc.is, tc.name)
	}
}

// strip removes the spaces a hex vector uses to show its structure.
func strip(s string) (out string) {
	return string(bytes.ReplaceAll([]byte(s), []byte(" "), nil))
}
