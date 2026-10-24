package logging

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/jsontext"
	"maps"
	"math"
	"math/big"
	"net"
	"slices"
	"strconv"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The CBOR tags zerolog's binary_log encoder writes beside tag 1 (an epoch
// timestamp); zerolog v1.35.1 internal/cbor/cbor.go.
const (
	zerologTagEmbeddedCbor  = 63
	zerologTagNetworkAddr   = 260
	zerologTagNetworkPrefix = 261
	zerologTagEmbeddedJson  = 262
	zerologTagHexString     = 263
)

// consoleCborDecMode decodes zerolog's CBOR one data item at a time. Strings
// are taken as zerolog wrote them, valid UTF-8 or not, as zerolog's own
// decoder takes them.
var consoleCborDecMode = func() cbor.DecMode {
	dm, err := cbor.DecOptions{
		UTF8: cbor.UTF8DecodeInvalid,
	}.DecMode()
	if err != nil {
		panic(err)
	}
	return dm
}()

// zerologCborToJson turns the first event of a binary_log write into the JSON
// zerolog.ConsoleWriter reads. It stands in for zerolog's own decoder, which
// ConsoleWriter otherwise applies and which reads every integer into an
// int64, so an unsigned value at or above 2⁶³ printed negative (zerolog pull
// requests 796 and 807, unmerged as of v1.35.1). The JSON matches that
// decoder's case by case — tag 1 to an RFC 3339 string, tag 63 to a CBOR data
// URL, tag 262 inserted raw, tags 260, 261 and 263 to text, NaN and the
// infinities to strings, a float32 at float32 precision — except that integers
// keep their full range and a byte string is escaped rather than copied
// between quotes.
//
// deferred: drop this once a zerolog release decodes major type 0 over its
// full range; ConsoleWriter then needs no help.
func zerologCborToJson(p []byte) (j []byte, err error) {
	var raw cbor.RawMessage
	_, err = consoleCborDecMode.UnmarshalFirst(p, &raw)
	if err != nil {
		err = eh.Errorf("unable to read cbor event: %w", err)
		return
	}
	j, err = appendCborAsJson(make([]byte, 0, 2*len(raw)), raw)
	return
}

func appendCborAsJson(dst []byte, raw cbor.RawMessage) (r []byte, err error) {
	if len(raw) == 0 {
		err = eh.Errorf("empty cbor data item")
		return
	}
	head := raw[0]
	switch head >> 5 {
	case 0:
		var u uint64
		if err = consoleCborDecMode.Unmarshal(raw, &u); err != nil {
			return
		}
		r = strconv.AppendUint(dst, u, 10)
	case 1:
		// -2⁶⁴ ≤ value ≤ -1: an int64 holds only the upper half.
		var i big.Int
		if err = consoleCborDecMode.Unmarshal(raw, &i); err != nil {
			return
		}
		r = i.Append(dst, 10)
	case 2:
		var b []byte
		if err = consoleCborDecMode.Unmarshal(raw, &b); err != nil {
			return
		}
		r = appendJsonString(dst, b)
	case 3:
		var s string
		if err = consoleCborDecMode.Unmarshal(raw, &s); err != nil {
			return
		}
		r = appendJsonString(dst, s)
	case 4:
		var items []cbor.RawMessage
		if err = consoleCborDecMode.Unmarshal(raw, &items); err != nil {
			return
		}
		dst = append(dst, '[')
		for i, it := range items {
			if i > 0 {
				dst = append(dst, ',')
			}
			if dst, err = appendCborAsJson(dst, it); err != nil {
				return
			}
		}
		r = append(dst, ']')
	case 5:
		// ConsoleWriter reads the event into a Go map, so the order of the
		// keys is lost either way; sorting them keeps the JSON deterministic.
		var m map[string]cbor.RawMessage
		if err = consoleCborDecMode.Unmarshal(raw, &m); err != nil {
			return
		}
		dst = append(dst, '{')
		for i, k := range slices.Sorted(maps.Keys(m)) {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendJsonString(dst, k)
			dst = append(dst, ':')
			if dst, err = appendCborAsJson(dst, m[k]); err != nil {
				return
			}
		}
		r = append(dst, '}')
	case 6:
		r, err = appendCborTagAsJson(dst, raw)
	case 7:
		r, err = appendCborSimpleAsJson(dst, raw)
	}
	return
}

func appendCborTagAsJson(dst []byte, raw cbor.RawMessage) (r []byte, err error) {
	var tag cbor.RawTag
	if err = consoleCborDecMode.Unmarshal(raw, &tag); err != nil {
		return
	}
	switch tag.Number {
	case 1:
		if len(tag.Content) > 0 && tag.Content[0]>>5 == 7 {
			var f float64
			if err = consoleCborDecMode.Unmarshal(tag.Content, &f); err != nil {
				return
			}
			secs := int64(f)
			t := time.Unix(secs, int64((f-float64(secs))*1e9)).UTC()
			r = appendJsonString(dst, t.Format(time.RFC3339Nano))
			return
		}
		var secs int64
		if err = consoleCborDecMode.Unmarshal(tag.Content, &secs); err != nil {
			return
		}
		r = appendJsonString(dst, time.Unix(secs, 0).UTC().Format(time.RFC3339))
	case zerologTagEmbeddedCbor:
		var b []byte
		if err = consoleCborDecMode.Unmarshal(tag.Content, &b); err != nil {
			return
		}
		r = appendJsonString(dst, "data:application/cbor;base64,"+base64.StdEncoding.EncodeToString(b))
	case zerologTagEmbeddedJson:
		var b []byte
		if err = consoleCborDecMode.Unmarshal(tag.Content, &b); err != nil {
			return
		}
		r = append(dst, b...)
	case zerologTagNetworkAddr:
		var b []byte
		if err = consoleCborDecMode.Unmarshal(tag.Content, &b); err != nil {
			return
		}
		switch len(b) {
		case 6:
			r = appendJsonString(dst, net.HardwareAddr(b).String())
		case 4, 16:
			r = appendJsonString(dst, net.IP(b).String())
		default:
			err = eb.Build().Int("len", len(b)).Errorf("network address of unexpected length")
		}
	case zerologTagNetworkPrefix:
		// A map of one pair: the address's octets to the prefix length.
		if len(tag.Content) == 0 || tag.Content[0] != 0xa1 {
			err = eh.Errorf("network prefix is not a map of one pair")
			return
		}
		dec := consoleCborDecMode.NewDecoder(bytes.NewReader(tag.Content[1:]))
		var ip []byte
		var ones int
		if err = dec.Decode(&ip); err != nil {
			return
		}
		if err = dec.Decode(&ones); err != nil {
			return
		}
		ipn := net.IPNet{IP: net.IP(ip), Mask: net.CIDRMask(ones, 8*len(ip))}
		r = appendJsonString(dst, ipn.String())
	case zerologTagHexString:
		var b []byte
		if err = consoleCborDecMode.Unmarshal(tag.Content, &b); err != nil {
			return
		}
		r = appendJsonString(dst, hex.EncodeToString(b))
	default:
		r, err = appendCborAsJson(dst, tag.Content)
	}
	return
}

func appendCborSimpleAsJson(dst []byte, raw cbor.RawMessage) (r []byte, err error) {
	switch raw[0] {
	case 0xf4:
		r = append(dst, "false"...)
	case 0xf5:
		r = append(dst, "true"...)
	case 0xf6, 0xf7:
		r = append(dst, "null"...)
	case 0xf9, 0xfa, 0xfb:
		bitSize := 64
		var f float64
		if raw[0] == 0xfb {
			err = consoleCborDecMode.Unmarshal(raw, &f)
		} else {
			bitSize = 32
			var f32 float32
			err = consoleCborDecMode.Unmarshal(raw, &f32)
			f = float64(f32)
		}
		if err != nil {
			return
		}
		switch {
		case math.IsNaN(f):
			r = append(dst, `"NaN"`...)
		case math.IsInf(f, 1):
			r = append(dst, `"+Inf"`...)
		case math.IsInf(f, -1):
			r = append(dst, `"-Inf"`...)
		default:
			r = strconv.AppendFloat(dst, f, 'f', -1, bitSize)
		}
	default:
		err = eb.Build().Uint8("head", raw[0]).Errorf("unsupported cbor simple value")
	}
	return
}

// appendJsonString quotes s as a JSON string. Invalid UTF-8 becomes U+FFFD;
// the error jsontext reports for it is dropped, since the literal is valid.
func appendJsonString[S ~[]byte | ~string](dst []byte, s S) []byte {
	dst, _ = jsontext.AppendQuote(dst, s)
	return dst
}
