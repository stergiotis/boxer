package gloss

import (
	"math"
	"net/netip"
)

// MediaTypeIPAddr shows an IP address as an address. Nothing in an Arrow
// result says a column holds one: ClickHouse's IPv4 rides as a big-endian
// uint32 (so `1.2.3.4` reads as `16909060`) and its IPv6 as a packed
// 16-byte FixedSizeBinary (so the plain rendering is a hex blob, and any
// text lane that formats through arrow's ValueStr shows base64). Leeway's
// network columns use the same two representations, plus the CIDR forms
// that carry the prefix length in a trailing byte — which is what the
// affinity keys on: a `ct:v` / `ct:w` column, and their `c` (CIDR) and
// `h`/`m` (array / set) spellings.
const MediaTypeIPAddr = "gloss/ipaddr"

func ipAddrGloss() GlossI {
	return &simpleGloss{
		mediaType:  MediaTypeIPAddr,
		doc:        "an IP address or CIDR prefix — packed bytes, a big-endian uint32, or address text",
		affinities: []string{`\bct:[vw]c?[hm]?\b`},
		accepts:    []ValueKindE{ValueKindNumeric, ValueKindText, ValueKindBytes},
		inline:     ipAddrFace,
	}
}

// ipAddrFace reads the three shapes an address arrives in, by the kind of
// the column it came from. A value that is none of them keeps its plain
// rendering in the error tone: the column said it holds an address and this
// one is not, which is worth seeing rather than hiding behind a blank.
func ipAddrFace(cell CellI) Inline {
	switch cell.Kind() {
	case ValueKindBytes:
		raw, ok := cell.Raw()
		if !ok {
			break
		}
		if raw == "" {
			return Inline{}
		}
		// Address text before packed bytes. Both lanes reach this face — the
		// grids hand it the Arrow bytes, the leeway card and the
		// per-attribute grid hand it text a driver already wrote out — and
		// only one order is safe for real data: an IPv6 written out is
		// routinely 16 characters long, the width of a packed one, so
		// packed-first would re-read `2001:db8:1::abcd` as bytes.
		if s := ipAddrText(raw); s != "" {
			return Inline{Text: s}
		}
		if s := ipAddrPacked(raw); s != "" {
			return Inline{Text: s}
		}
	case ValueKindNumeric:
		// The numeric shape is the IPv4 host address: a big-endian uint32,
		// the Arrow type ClickHouse's IPv4 column round-trips.
		if v, ok := cell.Uint64(); ok && v <= math.MaxUint32 {
			return Inline{Text: netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}).String()}
		}
		if s := ipAddrText(cell.Text()); s != "" {
			return Inline{Text: s}
		}
	default:
		t := cell.Text()
		if t == "" {
			return Inline{}
		}
		if s := ipAddrText(t); s != "" {
			return Inline{Text: s}
		}
	}
	return Inline{Text: cell.Text(), Tone: ToneError}
}

// ipAddrText reads a value that is already written out, canonicalising it;
// empty when it is not address text.
func ipAddrText(raw string) string {
	if a, err := netip.ParseAddr(raw); err == nil {
		return a.String()
	}
	if p, err := netip.ParsePrefix(raw); err == nil {
		return p.String()
	}
	return ""
}

// ipAddrPacked reads the packed wire forms by width: the address bytes, plus
// a trailing prefix-length byte for a CIDR. Empty when the width is not one
// of them, or when the prefix length does not fit the address.
func ipAddrPacked(raw string) string {
	switch len(raw) {
	case 4:
		var b [4]byte
		copy(b[:], raw)
		return netip.AddrFrom4(b).String()
	case 5:
		var b [4]byte
		copy(b[:], raw)
		return ipAddrPrefix(netip.AddrFrom4(b), int(raw[4]))
	case 16:
		var b [16]byte
		copy(b[:], raw)
		return netip.AddrFrom16(b).String()
	case 17:
		var b [16]byte
		copy(b[:], raw)
		return ipAddrPrefix(netip.AddrFrom16(b), int(raw[16]))
	}
	return ""
}

func ipAddrPrefix(addr netip.Addr, bits int) string {
	p := netip.PrefixFrom(addr, bits)
	if !p.IsValid() {
		return ""
	}
	return p.String()
}
