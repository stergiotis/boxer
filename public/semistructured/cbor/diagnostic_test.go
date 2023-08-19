package cbor

import (
	"bytes"
	"encoding/hex"
	"github.com/stergiotis/boxer/public/ea"
	"strings"
	"testing"

	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseAnnotated(str string) []byte {
	lines := strings.Split(str, "\n")
	retr := make([]byte, 0, 256)
	for _, line := range lines {
		before, _, _ := strings.Cut(line, "#")
		b, err := hex.DecodeString(strings.Trim(before, " \t"))
		if err != nil {
			log.Fatal().Err(err).Str("line", line).Msg("unable to decode line")
		}
		retr = append(retr, b...)
	}
	return retr
}

func TestDiagnostic(t *testing.T) {
	d := NewDiagnostics()
	for i, p := range rfc8949Examples {
		log.Debug().Int("index", i).Str("diagnostic", p[0]).Msg("tokenizing example")
		b := hexStr(p[1])
		r := bytes.NewReader(b)
		rr, err := ea.NewByteBlockReaderDiscardReader(r)
		require.NoError(t, err)
		builder := &strings.Builder{}
		err = d.Run(builder, rr)
		require.NoError(t, err)
		s := builder.String()
		s = strings.ReplaceAll(s, "reservedSimple(", "simple(")
		assert.Equal(t, p[0], s)
	}
}

func TestDiagnostic2(t *testing.T) {
	d := NewDiagnostics()
	check := func(diag string, cborHexAnnotated string) {
		cbor := parseAnnotated(cborHexAnnotated)
		r := bytes.NewReader(cbor)
		rr, err := ea.NewByteBlockReaderDiscardReader(r)
		require.NoError(t, err)
		builder := &strings.Builder{}
		log.Info().Str("diag", diag).Msg("aaa")
		err = d.Run(builder, rr)
		require.NoError(t, err)
		s := builder.String()
		s = strings.ReplaceAll(s, "reservedSimple(", "simple(")
		assert.Equal(t, diag, s)
	}
	check(`[1, 2, [_ ]]`, `83       # array(3)
   01    #   unsigned(1)
   02    #   unsigned(2)
   9f    #   array(*)
      ff #     break
`)
	check(`[1, 2, 3]`, `83    # array(3)
   01 #   unsigned(1)
   02 #   unsigned(2)
   03 #   unsigned(3)`)
	check(`{_ "a": [_ ], "b": [_ 1], "c": [_ 1, 2], "d": [_ 1, 2, 3], "e": [], "f": [1], "g": [1, 2], "h": [1, 2, 3]}`,
		`bf       # map(*)
	   61    #   text(1)
	      61 #     "a"
	   9f    #   array(*)
	      ff #     break
	   61    #   text(1)
	      62 #     "b"
	   9f    #   array(*)
	      01 #     unsigned(1)
	      ff #     break
	   61    #   text(1)
	      63 #     "c"
	   9f    #   array(*)
	      01 #     unsigned(1)
	      02 #     unsigned(2)
	      ff #     break
	   61    #   text(1)
	      64 #     "d"
	   9f    #   array(*)
	      01 #     unsigned(1)
	      02 #     unsigned(2)
	      03 #     unsigned(3)
	      ff #     break
	   61    #   text(1)
	      65 #     "e"
	   80    #   array(0)
	   61    #   text(1)
	      66 #     "f"
	   81    #   array(1)
	      01 #     unsigned(1)
	   61    #   text(1)
	      67 #     "g"
	   82    #   array(2)
	      01 #     unsigned(1)
	      02 #     unsigned(2)
	   61    #   text(1)
	      68 #     "h"
	   83    #   array(3)
	      01 #     unsigned(1)
	      02 #     unsigned(2)
	      03 #     unsigned(3)
	   ff    #   break`)
	check(`{_ "a": [], "b": [1], "c": [1, 2], "d": [1, 2, 3], "e": [_ ], "f": [_ 1], "g": [_ 1, 2], "h": [_ 1, 2, 3]}`, `
	bf       # map(*)
	   61    #   text(1)
	      61 #     "a"
	   80    #   array(0)
	   61    #   text(1)
	      62 #     "b"
	   81    #   array(1)
	      01 #     unsigned(1)
	   61    #   text(1)
	      63 #     "c"
	   82    #   array(2)
	      01 #     unsigned(1)
	      02 #     unsigned(2)
	   61    #   text(1)
	      64 #     "d"
	   83    #   array(3)
	      01 #     unsigned(1)
	      02 #     unsigned(2)
	      03 #     unsigned(3)
	   61    #   text(1)
	      65 #     "e"
	   9f    #   array(*)
	      ff #     break
	   61    #   text(1)
	      66 #     "f"
	   9f    #   array(*)
	      01 #     unsigned(1)
	      ff #     break
	   61    #   text(1)
	      67 #     "g"
	   9f    #   array(*)
	      01 #     unsigned(1)
	      02 #     unsigned(2)
	      ff #     break
	   61    #   text(1)
	      68 #     "h"
	   9f    #   array(*)
	      01 #     unsigned(1)
	      02 #     unsigned(2)
	      03 #     unsigned(3)
	      ff #     break
	   ff    #   break`)
	check(`[_ [], [1], [1, 2], [1, 2, 3], [_ ], [_ 1], [_ 1, 2], [_ 1, 2, 3]]`, `9f       # array(*)
	   80    #   array(0)
	   81    #   array(1)
	      01 #     unsigned(1)
	   82    #   array(2)
	      01 #     unsigned(1)
	      02 #     unsigned(2)
	   83    #   array(3)
	      01 #     unsigned(1)
	      02 #     unsigned(2)
	      03 #     unsigned(3)
	   9f    #   array(*)
	      ff #     break
	   9f    #   array(*)
	      01 #     unsigned(1)
	      ff #     break
	   9f    #   array(*)
	      01 #     unsigned(1)
	      02 #     unsigned(2)
	      ff #     break
	   9f    #   array(*)
	      01 #     unsigned(1)
	      02 #     unsigned(2)
	      03 #     unsigned(3)
	      ff #     break
	   ff    #   break`)
	check(`[[], [1], [1, 2], [1, 2, 3], [_ ], [_ 1], [_ 1, 2], [_ 1, 2, 3]]`, `88       # array(8)
   80    #   array(0)
   81    #   array(1)
      01 #     unsigned(1)
   82    #   array(2)
      01 #     unsigned(1)
      02 #     unsigned(2)
   83    #   array(3)
      01 #     unsigned(1)
      02 #     unsigned(2)
      03 #     unsigned(3)
   9f    #   array(*)
      ff #     break
   9f    #   array(*)
      01 #     unsigned(1)
      ff #     break
   9f    #   array(*)
      01 #     unsigned(1)
      02 #     unsigned(2)
      ff #     break
   9f    #   array(*)
      01 #     unsigned(1)
      02 #     unsigned(2)
      03 #     unsigned(3)
      ff #     break`)
	check(`[[_ ], [_ 1], [_ 1, 2], [_ 1, 2, 3], [], [1], [1, 2], [1, 2, 3]]`, `88       # array(8)
   9f    #   array(*)
      ff #     break
   9f    #   array(*)
      01 #     unsigned(1)
      ff #     break
   9f    #   array(*)
      01 #     unsigned(1)
      02 #     unsigned(2)
      ff #     break
   9f    #   array(*)
      01 #     unsigned(1)
      02 #     unsigned(2)
      03 #     unsigned(3)
      ff #     break
   80    #   array(0)
   81    #   array(1)
      01 #     unsigned(1)
   82    #   array(2)
      01 #     unsigned(1)
      02 #     unsigned(2)
   83    #   array(3)
      01 #     unsigned(1)
      02 #     unsigned(2)
      03 #     unsigned(3)`)
	check(`{_ "a": [_ []]}`, `bf       # map(*)
   61    #   text(1)
      61 #     "a"
   9f    #   array(*)
      80 #     array(0)
      ff #     break
   ff    #   break
`)
	check(`{_ "a": [_ [[_ ]]]}`, `bf             # map(*)
   61          #   text(1)
      61       #     "a"
   9f          #   array(*)
      81       #     array(1)
         9f    #       array(*)
            ff #         break
      ff       #     break
   ff          #   break
`)
	check(`[{_ "a": [_ [[_ ], []]]}]`, `81                # array(1)
   bf             #   map(*)
      61          #     text(1)
         61       #       "a"
      9f          #     array(*)
         82       #       array(2)
            9f    #         array(*)
               ff #           break
            80    #         array(0)
         ff       #       break
      ff          #     break`)
}
