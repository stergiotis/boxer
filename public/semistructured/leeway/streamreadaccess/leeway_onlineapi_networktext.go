package streamreadaccess

import (
	"net/netip"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
)

// networkValueText writes out element idx of a network-typed column as the
// address it holds.
//
// The text lane otherwise formats through arrow.Array.ValueStr, which is not
// a rendering of a network value: an IPv4 host rides as a big-endian uint32
// and comes out as a decimal (16909060 for 1.2.3.4), and every other network
// shape rides as a packed FixedSizeBinary, which ValueStr base64-encodes. The
// byte layout is the one the readaccess accessors decode — address bytes,
// then the prefix length in a trailing byte for the CIDR forms.
//
// ok is false for a column whose Arrow array does not carry the layout its
// canonical type claims; the caller then keeps the ValueStr rendering rather
// than inventing an address.
func networkValueText(arr arrow.Array, idx int, ct canonicaltypes.NetworkTypeAstNode) (text string, ok bool) {
	if arr == nil || idx < 0 || idx >= arr.Len() || arr.IsNull(idx) {
		return "", false
	}
	if ct.BaseType == canonicaltypes.BaseTypeNetworkIPv4 && ct.CIDRModifier == canonicaltypes.CIDRModifierNone {
		u, isU32 := arr.(*array.Uint32)
		if !isU32 {
			return "", false
		}
		v := u.Value(idx)
		return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}).String(), true
	}
	fsb, isFSB := arr.(*array.FixedSizeBinary)
	if !isFSB {
		return "", false
	}
	b := fsb.Value(idx)
	if len(b) != ct.ByteWidth() {
		return "", false
	}
	var addr netip.Addr
	switch ct.BaseType {
	case canonicaltypes.BaseTypeNetworkIPv4:
		addr = netip.AddrFrom4([4]byte(b[:4]))
	case canonicaltypes.BaseTypeNetworkIPv6:
		addr = netip.AddrFrom16([16]byte(b[:16]))
	default:
		return "", false
	}
	if ct.CIDRModifier != canonicaltypes.CIDRModifierVariable {
		return addr.String(), true
	}
	p := netip.PrefixFrom(addr, int(b[len(b)-1]))
	if !p.IsValid() {
		return "", false
	}
	return p.String(), true
}

// valueText is the text lane's read of one element: the address for a network
// column, the UTF-8 bytes for a fixed-width text column, arrow's own
// rendering for everything else. The ValueFormatter still sees the result and
// can replace it — what it receives is the value written out, never an
// encoding artefact of how the value is carried.
func valueText(arr arrow.Array, idx int, ct canonicaltypes.PrimitiveAstNodeI) (text string) {
	if nt, isNet := ct.(canonicaltypes.NetworkTypeAstNode); isNet {
		if s, ok := networkValueText(arr, idx, nt); ok {
			return s
		}
	}
	// A fixed-width text column (`sxN`) rides a FixedSizeBinary array, whose
	// ValueStr is base64; the value is text, padding included (ADR-0201 SD3).
	if st, isStr := ct.(canonicaltypes.StringAstNode); isStr &&
		st.BaseType == canonicaltypes.BaseTypeStringUtf8 && st.WidthModifier == canonicaltypes.WidthModifierFixed {
		if fsb, ok := arr.(*array.FixedSizeBinary); ok {
			return string(fsb.Value(idx))
		}
	}
	return arr.ValueStr(idx)
}
