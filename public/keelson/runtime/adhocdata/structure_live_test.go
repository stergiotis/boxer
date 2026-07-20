package adhocdata

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"golang.org/x/sys/unix"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
)

// requireCH skips a test when clickhouse-local is not installed, the
// same gate the chlocalpool/chlocalbroker live tests use.
func requireCH(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(chlocalpool.DefaultBinaryPath); err != nil {
		t.Skipf("clickhouse-local not installed at %s: %v", chlocalpool.DefaultBinaryPath, err)
	}
}

// tsUs / tsNs are fixed instants with sub-second precision.
var (
	tsUs = time.Date(2021, 3, 4, 5, 6, 7, 123456000, time.UTC)
	tsNs = time.Date(2021, 3, 4, 5, 6, 7, 123456789, time.UTC)
)

// TestStructureLiveFIFO is the on-pin re-verification of the FIFO facts
// (ADR-0134 SD3): for every supported type it writes a one-column Arrow
// IPC *stream*, streams it through a named pipe into clickhouse-local
// with the StructureFor structure, and compares the rendered rows. It
// proves both the type mapping and that file('…','ArrowStream',…) reads
// from a pipe with an explicit structure.
func TestStructureLiveFIFO(t *testing.T) {
	requireCH(t)
	cases := []struct {
		name  string
		typ   arrow.DataType
		build func(b array.Builder)
		want  string
	}{
		{"utf8", arrow.BinaryTypes.String, func(b array.Builder) {
			sb := b.(*array.StringBuilder)
			sb.Append("hello")
			sb.Append("world")
		}, "hello\nworld\n"},
		{"binary", arrow.BinaryTypes.Binary, func(b array.Builder) {
			bb := b.(*array.BinaryBuilder)
			bb.Append([]byte("abc"))
			bb.Append([]byte("de"))
		}, "abc\nde\n"},
		{"bool", arrow.FixedWidthTypes.Boolean, func(b array.Builder) {
			bb := b.(*array.BooleanBuilder)
			bb.Append(true)
			bb.Append(false)
		}, "true\nfalse\n"},
		{"int8", arrow.PrimitiveTypes.Int8, func(b array.Builder) {
			ib := b.(*array.Int8Builder)
			ib.Append(1)
			ib.Append(-2)
		}, "1\n-2\n"},
		{"int16", arrow.PrimitiveTypes.Int16, func(b array.Builder) {
			ib := b.(*array.Int16Builder)
			ib.Append(300)
			ib.Append(-300)
		}, "300\n-300\n"},
		{"int32", arrow.PrimitiveTypes.Int32, func(b array.Builder) {
			ib := b.(*array.Int32Builder)
			ib.Append(70000)
			ib.Append(-70000)
		}, "70000\n-70000\n"},
		{"int64", arrow.PrimitiveTypes.Int64, func(b array.Builder) {
			ib := b.(*array.Int64Builder)
			ib.Append(5000000000)
			ib.Append(-5000000000)
		}, "5000000000\n-5000000000\n"},
		{"uint8", arrow.PrimitiveTypes.Uint8, func(b array.Builder) {
			ib := b.(*array.Uint8Builder)
			ib.Append(200)
			ib.Append(0)
		}, "200\n0\n"},
		{"uint16", arrow.PrimitiveTypes.Uint16, func(b array.Builder) {
			ib := b.(*array.Uint16Builder)
			ib.Append(60000)
			ib.Append(1)
		}, "60000\n1\n"},
		{"uint32", arrow.PrimitiveTypes.Uint32, func(b array.Builder) {
			ib := b.(*array.Uint32Builder)
			ib.Append(4000000000)
			ib.Append(2)
		}, "4000000000\n2\n"},
		{"uint64", arrow.PrimitiveTypes.Uint64, func(b array.Builder) {
			ib := b.(*array.Uint64Builder)
			ib.Append(18000000000000000000)
			ib.Append(3)
		}, "18000000000000000000\n3\n"},
		{"float32", arrow.PrimitiveTypes.Float32, func(b array.Builder) {
			fb := b.(*array.Float32Builder)
			fb.Append(1.5)
			fb.Append(-2.25)
		}, "1.5\n-2.25\n"},
		{"float64", arrow.PrimitiveTypes.Float64, func(b array.Builder) {
			fb := b.(*array.Float64Builder)
			fb.Append(3.5)
			fb.Append(-4.25)
		}, "3.5\n-4.25\n"},
		{"date32", arrow.FixedWidthTypes.Date32, func(b array.Builder) {
			db := b.(*array.Date32Builder)
			db.Append(arrow.Date32FromTime(time.Date(2021, 3, 4, 0, 0, 0, 0, time.UTC)))
			db.Append(arrow.Date32(1))
		}, "2021-03-04\n1970-01-02\n"},
		{"ts_us", &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}, func(b array.Builder) {
			tb := b.(*array.TimestampBuilder)
			tb.Append(arrow.Timestamp(tsUs.UnixMicro()))
			tb.Append(arrow.Timestamp(0))
		}, "2021-03-04 05:06:07.123456\n1970-01-01 00:00:00.000000\n"},
		{"ts_ns", &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "UTC"}, func(b array.Builder) {
			tb := b.(*array.TimestampBuilder)
			tb.Append(arrow.Timestamp(tsNs.UnixNano()))
			tb.Append(arrow.Timestamp(0))
		}, "2021-03-04 05:06:07.123456789\n1970-01-01 00:00:00.000000000\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schema := arrow.NewSchema([]arrow.Field{{Name: "a", Type: tc.typ}}, nil)
			structure, err := StructureFor(schema)
			if err != nil {
				t.Fatalf("StructureFor: %v", err)
			}
			stream := buildArrowStream(t, schema, tc.build)
			got := runCHOverFIFO(t, structure, stream)
			if got != tc.want {
				t.Fatalf("structure %q: got %q, want %q", structure, got, tc.want)
			}
		})
	}
}

// buildArrowStream renders a single 2-row record for schema (via build)
// as Arrow IPC *stream* bytes (not the seekable file format).
func buildArrowStream(t *testing.T, schema *arrow.Schema, build func(array.Builder)) []byte {
	t.Helper()
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	build(rb.Field(0))
	rec := rb.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	if err := w.Write(rec); err != nil {
		t.Fatalf("ipc write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("ipc close: %v", err)
	}
	return buf.Bytes()
}

// runCHOverFIFO streams stream through a FIFO into clickhouse-local,
// binding it as a TEMPORARY table with the given structure, and returns
// the TabSeparated rendering of SELECT *.
//
// FIFO discipline (verified against clickhouse-local 26.6): the reader
// blocks until pipe EOF — it does not stop at the Arrow end-of-stream
// marker — so the writer must close the pipe to end the read. The
// writer therefore opens O_WRONLY, which blocks until the reader has
// opened the read end first; this guarantees ordering (no lost data, no
// read-open that hangs forever with no writer present) that a bare
// O_RDWR-then-close would race. The open is polled non-blocking so a
// clickhouse-local that never reaches the file() read cannot hang the
// writer past the context deadline. This is the pattern M3's broker
// materialisation follows.
func runCHOverFIFO(t *testing.T, structure string, stream []byte) string {
	t.Helper()
	fifo := filepath.Join(t.TempDir(), "in.fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	writeErr := make(chan error, 1)
	go func() {
		f, err := openFifoWrite(ctx, fifo)
		if err != nil {
			writeErr <- err
			return
		}
		_, werr := f.Write(stream)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		writeErr <- werr
	}()

	sql := "CREATE TEMPORARY TABLE t AS SELECT * FROM file(" + quoteSQL(fifo) +
		",'ArrowStream'," + quoteSQL(structure) + "); SELECT * FROM t"
	cmd := exec.CommandContext(ctx, chlocalpool.DefaultBinaryPath, "--path", t.TempDir(), "--logger.console", "0")
	cmd.Stdin = strings.NewReader(sql + " FORMAT TabSeparated;\n")
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	runErr := cmd.Run()
	werr := <-writeErr
	if runErr != nil {
		t.Fatalf("clickhouse-local: %v (stderr: %s)", runErr, errb.String())
	}
	if werr != nil {
		t.Fatalf("fifo writer: %v", werr)
	}
	return out.String()
}

// openFifoWrite opens path for writing once a reader is present, polling
// O_WRONLY|O_NONBLOCK — which returns ENXIO while no reader has opened —
// until it succeeds or ctx is done. Opening only after the reader is
// present guarantees the reader opened first, so the subsequent
// stream-then-close delivers a clean EOF with no lost data.
func openFifoWrite(ctx context.Context, path string) (*os.File, error) {
	for {
		f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, syscall.ENXIO) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
}

// quoteSQL single-quotes a ClickHouse string literal, escaping
// backslashes and single quotes (the structure string carries 'UTC').
func quoteSQL(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	return "'" + r.Replace(s) + "'"
}
