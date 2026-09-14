package diag_test

// Fuzz targets for the diagnostic-notation printer:
//
//	FuzzPrint         — Print and String are total on arbitrary bytes under
//	                    every option combination: they agree on the text
//	                    and the error, the spans cover the text contiguously
//	                    from 0, and exactly one Error span is present iff an
//	                    error is returned, which wraps exactly one sentinel.
//	FuzzCompactOracle — wherever the fxamacker library's Diagnose accepts
//	                    the bytes, the compact rendering is byte-for-byte
//	                    its output and carries no error (the claim in
//	                    doc.go). Inputs nested past the walk's own bound are
//	                    outside that claim.
//
// Run e.g.:
//
//	go test -run xxx -fuzz FuzzCompactOracle -fuzztime 60s ./public/semistructured/cbor/diag/

import (
	"encoding/hex"
	"errors"
	"math"
	"strconv"
	"testing"

	fx "github.com/fxamacker/cbor/v2"

	. "github.com/stergiotis/boxer/public/semistructured/cbor/diag"
)

// fuzzMaxInput keeps per-exec cost low; pretty mode measures each
// container's compact width, so cost grows faster than the input.
const fuzzMaxInput = 4 << 10

var sentinels = []error{
	ErrTruncated, ErrReservedInfo, ErrUnexpectedBreak, ErrIndefiniteHead,
	ErrChunkType, ErrInvalidUTF8, ErrTrailingBytes, ErrNesting, ErrEmpty,
}

func addRFCSeeds(f *testing.F) {
	for i, v := range rfcVectors {
		b, err := hex.DecodeString(v.hex)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b, uint8(i))
	}
	// Malformed shapes from TestDegradation.
	for _, h := range []string{"830102", "1b0000", "1c", "8201ff", "1f", "5f6161ff", "62c328", "0102", "", "8301021c"} {
		b, err := hex.DecodeString(h)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b, uint8(len(h)))
	}
}

// fuzzOptions spends the flag bits on the options; the Annotate hook is pure
// and depends only on the path, as the Options contract requires.
func fuzzOptions(flags uint8) (opts Options) {
	opts.Compact = flags&1 != 0
	opts.FloatPrecision = flags&2 != 0
	opts.TagComments = flags&4 != 0
	opts.Sequence = flags&8 != 0
	if flags&16 != 0 {
		opts.BytesFold = 3
	}
	if flags&32 != 0 {
		opts.Width = 8
		opts.Indent = "\t"
	}
	if flags&64 != 0 {
		opts.Annotate = func(path []PathElem) string {
			if len(path)%2 == 1 {
				return "p" + strconv.Itoa(len(path))
			}
			return ""
		}
	}
	return
}

func FuzzPrint(f *testing.F) {
	addRFCSeeds(f)

	f.Fuzz(func(t *testing.T, data []byte, flags uint8) {
		if len(data) > fuzzMaxInput {
			t.Skip()
		}
		opts := fuzzOptions(flags)
		s, errS := String(data, opts)
		spans, errP := Print(data, opts)

		if (errS == nil) != (errP == nil) || (errS != nil && errS.Error() != errP.Error()) {
			t.Fatalf("String and Print disagree on the error for %x: %v vs %v", data, errS, errP)
		}
		if got := Text(spans); got != s {
			t.Fatalf("Text(Print) != String for %x:\nprint:  %q\nstring: %q", data, got, s)
		}
		pos := 0
		nErr := 0
		for i, sp := range spans {
			if sp.Start != pos || sp.Stop != sp.Start+len(sp.Text) || s[sp.Start:sp.Stop] != sp.Text {
				t.Fatalf("span %d %+v is not contiguous at %d for %x", i, sp, pos, data)
			}
			if sp.Category == CategoryError {
				nErr++
			}
			pos = sp.Stop
		}
		if pos != len(s) {
			t.Fatalf("spans end at %d, text is %d bytes, for %x", pos, len(s), data)
		}
		if errS == nil {
			if nErr != 0 {
				t.Fatalf("%d Error spans without an error for %x: %q", nErr, data, s)
			}
			return
		}
		if nErr != 1 {
			t.Fatalf("%d Error spans for error %v on %x: %q", nErr, errS, data, s)
		}
		matched := 0
		for _, sentinel := range sentinels {
			if errors.Is(errS, sentinel) {
				matched++
			}
		}
		if matched != 1 {
			t.Fatalf("error %v wraps %d sentinels for %x", errS, matched, data)
		}
	})
}

func fuzzDiagMode(f *testing.F, precision, sequence bool) fx.DiagMode {
	dm, err := fx.DiagOptions{
		FloatPrecisionIndicator: precision,
		CBORSequence:            sequence,
		MaxNestedLevels:         65535,
		MaxArrayElements:        math.MaxInt32,
		MaxMapPairs:             math.MaxInt32,
	}.DiagMode()
	if err != nil {
		f.Fatal(err)
	}
	return dm
}

func FuzzCompactOracle(f *testing.F) {
	addRFCSeeds(f)

	modes := [2][2]fx.DiagMode{}
	for p := range 2 {
		for q := range 2 {
			modes[p][q] = fuzzDiagMode(f, p == 1, q == 1)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte, flags uint8) {
		if len(data) > fuzzMaxInput {
			t.Skip()
		}
		precision := flags&2 != 0
		sequence := flags&8 != 0
		dm := modes[b2i(precision)][b2i(sequence)]
		want, err := dm.Diagnose(data)
		if err != nil {
			return // the oracle refuses; the claim does not reach here
		}
		opts := Options{Compact: true, FloatPrecision: precision, Sequence: sequence}
		got, err := String(data, opts)
		if errors.Is(err, ErrNesting) {
			t.Skip() // deeper than the walk's bound, which the oracle does not share
		}
		if err != nil {
			t.Fatalf("oracle accepts %x (%q) but the printer fails: %v\ngot: %q", data, want, err, got)
		}
		if got != want {
			t.Fatalf("compact rendering differs from the oracle for %x:\nwant: %q\ngot:  %q", data, want, got)
		}
	})
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
