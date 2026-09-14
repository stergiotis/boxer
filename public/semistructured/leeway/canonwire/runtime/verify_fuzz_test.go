package runtime

// Fuzz targets for the canonical-wire gate. VerifyCanonical is what stands
// between untrusted bytes and the readers, so each target encodes the laws its
// verdict has to be consistent with:
//
//	FuzzVerifyCanonical         — VerifyCanonical is total (a verdict, never a
//	                              panic); an accepted item is one entity of a
//	                              sequence, and so is each copy of it doubled;
//	                              Skip consumes exactly the accepted bytes; no
//	                              proper prefix of them is accepted; and
//	                              re-emitting every head through CborWriter —
//	                              shortest heads, shortest floats — reproduces
//	                              them byte for byte, which is what canonical
//	                              means for the head layer.
//	FuzzVerifyCanonicalSequence — VerifyCanonicalSequence is total; the n
//	                              entities it counts before success or failure
//	                              split off with ReadItemBytes, each verifies
//	                              on its own, and on success they concatenate
//	                              back to the input.
//
// Run e.g.:
//
//	go test -run xxx -fuzz FuzzVerifyCanonical -fuzztime 60s ./public/semistructured/leeway/canonwire/runtime/

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// verifyFuzzSeeds returns accepted entities from the verify tests plus
// single-byte corruptions of each, which keep the fuzzer near the boundary
// between accepted and refused.
func verifyFuzzSeeds(f *testing.F) (seeds [][]byte) {
	// The golden of TestEntityGolden.
	golden, err := hex.DecodeString("8301a10181182aa26173818281820001626869637536348182818201416d07")
	if err != nil {
		f.Fatal(err)
	}
	seeds = append(seeds, golden)

	// One value per interior form, framed as in entityWithValue.
	for _, v := range []string{
		"d903e9a20120281a3b8b87c0", "d8348208410a", "6161", "f98000", "f5", "f6",
		"d90102820303", "81d903e9a1011a6553f100", "fa47c35000", "fb3ff199999999999a",
	} {
		seeds = append(seeds, mustHex(f, "8301a0a1637536348182"+"80"+v))
	}

	lo := rawMemb(f, mappingplan.MembershipChannelLowCardRef, 1)
	hi := rawMemb(f, mappingplan.MembershipChannelHighCardRef, 1)
	one := uintVal(f, 1)
	seeds = append(seeds,
		rawEntity(f, Version, nil, nil),
		rawEntity(f, Version, []rawPlain{{key: 1, vals: [][]byte{one}}}, nil),
		rawEntity(f, Version, nil, []rawSlot{{sig: "u64_u64", attrs: [][]byte{
			rawAttrGroups(f, [][][]byte{{lo, hi}, {}}, one),
			rawAttrGroups(f, [][][]byte{{lo}, {lo, hi}}, one),
		}}}),
		rawEntity(f, Version, nil, []rawSlot{{sig: "u64", attrs: [][]byte{
			rawAttr(f, [][]byte{lo, lo}, one),
			rawAttr(f, [][]byte{lo, lo}, one),
		}}}),
	)

	n := len(seeds)
	for _, s := range seeds[:n] {
		for _, i := range []int{0, len(s) / 2, len(s) - 1} {
			m := bytes.Clone(s)
			m[i] ^= 0x01
			seeds = append(seeds, m)
		}
	}
	return
}

func mustHex(f *testing.F, s string) (b []byte) {
	b, err := hex.DecodeString(s)
	if err != nil {
		f.Fatal(err)
	}
	return
}

// reencodeHeads re-emits one data item from r through cw head by head:
// Head for every argument, WriteFloatShortest for floats, payload bytes
// verbatim. For bytes the reader accepts this is the identity exactly when
// every head, float included, is already in its shortest form.
func reencodeHeads(r *CborReader, cw *CborWriter) {
	mt, ai, arg := r.readHead()
	if r.err != nil {
		return
	}
	switch mt {
	case MajorTypeSimple:
		if isFloatAI(ai) {
			cw.WriteFloatShortest(floatFromHead(ai, arg))
		} else {
			cw.Head(mt, arg)
		}
	case MajorTypeBytes, MajorTypeText:
		cw.Head(mt, arg)
		cw.Write(r.take(arg))
	case MajorTypeArray, MajorTypeMap:
		cw.Head(mt, arg)
		n := arg
		if mt == MajorTypeMap {
			n *= 2
		}
		for i := uint64(0); i < n && r.err == nil; i++ {
			reencodeHeads(r, cw)
		}
	case MajorTypeTag:
		cw.Head(mt, arg)
		reencodeHeads(r, cw)
	default:
		cw.Head(mt, arg)
	}
}

func FuzzVerifyCanonical(f *testing.F) {
	for _, s := range verifyFuzzSeeds(f) {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, item []byte) {
		if VerifyCanonical(item) != nil {
			return // refusal is fine; a panic is not
		}

		n, err := VerifyCanonicalSequence(item)
		if err != nil || n != 1 {
			t.Fatalf("accepted item is not a one-entity sequence: n=%d err=%v", n, err)
		}
		doubled := append(bytes.Clone(item), item...)
		if n, err = VerifyCanonicalSequence(doubled); err != nil || n != 2 {
			t.Fatalf("accepted item doubled is not a two-entity sequence: n=%d err=%v", n, err)
		}

		r := NewCborReader(item)
		if got := r.ReadItemBytes(); r.Err() != nil || !bytes.Equal(got, item) || r.Remaining() != 0 {
			t.Fatalf("Skip disagrees with the verifier: err=%v consumed=%d of %d", r.Err(), len(got), len(item))
		}

		for _, k := range []int{len(item) - 1, len(item) / 2} {
			if VerifyCanonical(item[:k]) == nil {
				t.Fatalf("proper prefix of %d bytes (of %d) accepted", k, len(item))
			}
		}

		var buf bytes.Buffer
		cw, err := NewCborWriter(&buf)
		if err != nil {
			t.Fatal(err)
		}
		r = NewCborReader(item)
		reencodeHeads(r, cw)
		if r.Err() != nil || cw.Err() != nil {
			t.Fatalf("re-encoding an accepted item failed: read=%v write=%v", r.Err(), cw.Err())
		}
		if !bytes.Equal(buf.Bytes(), item) {
			t.Fatalf("accepted item is not in shortest form:\n in: %x\nout: %x", item, buf.Bytes())
		}
	})
}

func FuzzVerifyCanonicalSequence(f *testing.F) {
	seeds := verifyFuzzSeeds(f)
	for _, s := range seeds {
		f.Add(s)
	}
	f.Add(append(bytes.Clone(seeds[0]), seeds[1]...))
	f.Add(append(bytes.Clone(seeds[0]), 0x83))

	f.Fuzz(func(t *testing.T, b []byte) {
		n, err := VerifyCanonicalSequence(b)
		r := NewCborReader(b)
		var joined []byte
		for i := 0; i < n; i++ {
			e := r.ReadItemBytes()
			if r.Err() != nil {
				t.Fatalf("entity %d of %d counted but does not skip: %v", i, n, r.Err())
			}
			if verr := VerifyCanonical(e); verr != nil {
				t.Fatalf("entity %d of %d counted but does not verify alone: %v", i, n, verr)
			}
			joined = append(joined, e...)
		}
		if err == nil && !bytes.Equal(joined, b) {
			t.Fatalf("%d entities verified but cover %d of %d bytes", n, len(joined), len(b))
		}
	})
}
