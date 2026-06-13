package errkind

import (
	"io"
	"testing"
	"time"
)

// sampleError builds a 4-fact error tree pre-shredded into parallel
// arrays. Models a 2-stream chain ("stack-0" / "no-stack") with two
// facts per stream.
func sampleError() Error {
	return Error{
		Id:          0xDEADBEEF,
		NaturalKey:  []byte("trace-1234"),
		CapturedTs:  time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		Messages:    []string{"outer wrap", "", "inner cause", ""},
		Sources:     []string{"", "main.go", "", "lib.go"},
		Funcs:       []string{"", "main.handle", "", "lib.parse"},
		StreamNames: []string{"stack-0", "stack-0", "no-stack", "no-stack"},
		Lines:       []uint32{0, 42, 0, 117},
		FactIds:     []uint64{1, 2, 3, 4},
		ParentIds:   []uint64{0, 1, 0, 3},
		Data:        [][]byte{nil, nil, []byte(`{"k":"v"}`), nil},
	}
}

// BenchmarkError_Marshal measures the per-row cost of marshalling a
// 4-fact Error. With no hand-coded baseline to compare against
// (rowmarshall.Error has no bench), this stands alone as the M5
// performance budget.
func BenchmarkError_Marshal(b *testing.B) {
	cols := &ErrorColumns{}
	cols.Append(sampleError())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := cols.Marshal(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}
