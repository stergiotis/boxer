//go:build llm_generated_opus47

package capabilitygrant

import (
	"io"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
)

// sampleGrant mirrors rowmarshall.sampleGrant: identical scalar
// payload so the wire bytes are directly comparable.
func sampleGrant() CapabilityGrant {
	return CapabilityGrant{
		Id:            12345,
		NaturalKey:    []byte{0xa1, 0xb2, 0xc3, 0xd4},
		Ts:            time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		ExpiresAt:     time.Unix(0, 1_900_000_000_000_000_000).UTC(),
		Subject:       "user/alice/repo/foo",
		Capability:    "read",
		ValidityBegin: 1_700_000_000,
		ValidityEnd:   1_800_000_000,
		Active:        true,
		GranterFact:   option.Some(uint64(9876)),
	}
}

// BenchmarkCapabilityGrant_Marshal is the ADR-0042 M4 regression gate —
// the legacy rowmarshall.BenchmarkRowBinaryMarshal hand-coded path runs
// at 51 ns/op + 1 alloc on the same hardware; this generated version
// should land within a comparable envelope.
func BenchmarkCapabilityGrant_Marshal(b *testing.B) {
	cols := &CapabilityGrantColumns{}
	cols.Append(sampleGrant())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := cols.Marshal(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCapabilityGrant_AppendOnly isolates the SoA-build path.
func BenchmarkCapabilityGrant_AppendOnly(b *testing.B) {
	row := sampleGrant()
	cols := &CapabilityGrantColumns{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cols.Append(row)
	}
}
