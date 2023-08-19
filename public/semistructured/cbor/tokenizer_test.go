package cbor

import (
	"bytes"
	"encoding/hex"
	"io"
	"math/rand"
	"strings"
	"testing"

	cbor "github.com/fxamacker/cbor/v2"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

func hexStr(str string) []byte {
	str = strings.TrimPrefix(str, "0x")
	l := len(str) / 2
	d := make([]byte, l, l)
	_, err := hex.Decode(d, []byte(str))
	if err != nil {
		log.Fatal().Str("str", str).Err(err).Msg("invalid hex string")
	}
	return d
}

var rfc8949Examples = [][]string{
	{`false`, "0xf4"},
	{`true`, "0xf5"},
	{`null`, "0xf6"},
	{`undefined`, "0xf7"},
	{`simple(16)`, "0xf0"},
	{`simple(255)`, "0xf8ff"},
	{`0("2013-03-21T20:04:00Z")`, "0xc074323031332d30332d32315432303a30343a30305a"},
	{`1(1363896240)`, "0xc11a514b67b0"},
	//{`1(1363896240.5)`, "0xc1fb41d452d9ec200000"},
	{`23(h'01020304')`, "0xd74401020304"},
	{`24(h'6449455446')`, "0xd818456449455446"},
	{`32("http://www.example.com")`, "0xd82076687474703a2f2f7777772e6578616d706c652e636f6d"},
	{`h''`, "0x40"},
	//{"\"\"", "0x60"},
	//{"\"a\"", "0x6161"},
	//{"\"IETF\"", "0x6449455446"},
	//{"\"\\", "0x62225c"},
	//{"\u00fc", "0x62c3bc"},
	//{"\u6c34", "0x63e6b0b4"},
	//{"\\ud800\\udd51", "0x64f0908591"},
	{`[]`, "0x80"},
	{`[1, 2, 3]`, "0x83010203"},
	{`[1, [2, 3], [4, 5]]`, "0x8301820203820405"},
	{`[1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25]`, "0x98190102030405060708090a0b0c0d0e0f101112131415161718181819"},
	{`{}`, "0xa0"},
	{`{1: 2, 3: 4}`, "0xa201020304"},
	{`{"a": 1, "b": [2, 3]}`, "0xa26161016162820203"},
	{`["a", {"b": "c"}]`, "0x826161a161626163"},
	{`{"a": "A", "b": "B", "c": "C", "d": "D", "e": "E"}`, "0xa56161614161626142616361436164614461656145"},
	{`(_ h'0102', h'030405')`, "0x5f42010243030405ff"},
	{`(_ "strea", "ming")`, "0x7f657374726561646d696e67ff"},
	{`[_ ]`, "0x9fff"},
	{`[_ 1, [2, 3], [_ 4, 5]]`, "0x9f018202039f0405ffff"},
	{`[_ 1, [2, 3], [4, 5]]`, "0x9f01820203820405ff"},
	{`[1, [2, 3], [_ 4, 5]]`, "0x83018202039f0405ff"},
	{`[1, [_ 2, 3], [4, 5]]`, "0x83019f0203ff820405"},
	{`[_ 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25]`, "0x9f0102030405060708090a0b0c0d0e0f101112131415161718181819ff"},
	{`{_ "a": 1, "b": [_ 2, 3]}`, "0xbf61610161629f0203ffff"},
	{`["a", {_ "b": "c"}]`, "0x826161bf61626163ff"},
	{`{_ "Fun": true, "Amt": -2}`, "0xbf6346756ef563416d7421ff"},
}

var rfc8949NotWellFormed = [][]string{
	{"End of input in a head", "0x18191a19011a01021b01020304050607385878989a01ff00b8d8f8f900fa0000fb000000"},
	{"Definite-length strings with short data", "0x415affffffff005bffffffffffffffff0102037affffffff007b7fffffffffffffff010203"},
	{"Definite-length maps and arrays not closed with enough items", "0x818181818181818181818200a1a20102a100a2000000"},
	{"Tag number not followed by tag content", "0xc0"},
	{"Indefinite-length strings not closed by a \"break\" stop code", "0x5f41007f6100"},
	{"Indefinite-length maps and arrays not closed by a \"break\" stop code", "0x9f9f0102bfbf01020102819f9f80009f9f9f9f9fffffffff9f819f819f9fffffff"},
	{"Reserved additional information values", "0x1c1d1e3c3d3e5c5d5e7c7d7e9c9d9ebcbdbedcdddefcfdfe"},
	{"Reserved two-byte encodings of simple values", "0xf800f801f818f81f"},
	{"Indefinite-length string chunks not of the correct type", "0x5f00ff5f21ff5f6100ff5f80ff5fa0ff5fc000ff5fe0ff7f4100ff"},
	{"Indefinite-length string chunks not definite length", "0x5f5f4100ffff7f7f6100ffff"},
	{"Break occurring on its own outside of an indefinite-length item", "0xff"},
	{"Break occurring in a definite-length array or map or a tag", "0x81ff8200ffa1ffa1ff00a100ffa20000ff9f81ff9f829f819f9fffffffff"},
	{"Break in an indefinite-length map that would lead to an odd number of items (break in a value position)", "0xbf00ffbf000000ff"},
	{"Major type 0, 1, 6 with additional information 31", "0x1f3fdf"},
}

func TestTokenizerUint(t *testing.T) {
	buf := &bytes.Buffer{}
	enc := cbor.NewEncoder(buf)
	tk := NewTokenizer(buf)
	for i := 0; i < 10000; i++ {
		u := rand.Uint64()
		err := enc.Encode(u)
		require.NoError(t, err)
		l := buf.Len()
		token, bytesRead, retr, err := tk.Next()
		require.EqualValues(t, l, bytesRead)
		require.NoError(t, err)
		require.EqualValues(t, u, retr)
		switch token {
		case TokenUInt8, TokenUInt16, TokenUInt32, TokenUInt64:
			break
		default:
			require.Failf(t, "unexpected token", "token=%v", token)
		}
		buf.Reset()
	}
}

func TestTokenizerNotWellFormed(t *testing.T) {
	var err error
	for i, p := range rfc8949NotWellFormed {
		b := hexStr(p[1])
		r := bytes.NewReader(b)
		tk := NewTokenizer(r)
		err = nil
		for err == nil {
			_, _, _, err = tk.Next()
			if err != nil {
				log.Debug().Err(err).Str("problem", p[0]).Int("index", i).Msg("tokenizer stopped with error, good")
				break
			}
		}
	}
}

func TestTokenizerManually(t *testing.T) {
	for i, p := range rfc8949Examples {
		log.Debug().Int("index", i).Str("diagnostic", p[0]).Msg("tokenizing example")
		b := hexStr(p[1])
		r := bytes.NewReader(b)
		tk := NewTokenizer(r)
		for {
			token, _, val, err := tk.Next()
			if err != nil {
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
			}
			switch token {
			case TokenByteString, TokenUTF8String:
				tmp := make([]byte, val, val)
				if val > 0 {
					_, err = io.ReadFull(r, tmp)
				}
				if token == TokenUTF8String {
					log.Trace().Str("content", string(tmp)).Msg("content")
				} else {
					log.Trace().Bytes("content", tmp).Msg("content")
				}
				require.NoError(t, err)
				break
			}
		}
	}
}
