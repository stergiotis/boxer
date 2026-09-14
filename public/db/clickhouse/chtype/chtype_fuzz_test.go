package chtype_test

// Fuzz targets for the type-string parser:
//
//	FuzzParseRoundTrip — Parse never panics; whenever it accepts s,
//	                     Parse(t.String()) succeeds and yields an equal Type,
//	                     and String is a fixed point after one round.
//	FuzzUnescapeTotal  — Unescape never panics and never lengthens its input
//	                     (every escape it resolves is at least as long as the
//	                     byte it produces).
//
// Run e.g.:
//
//	go test -run xxx -fuzz FuzzParseRoundTrip -fuzztime 60s ./public/db/clickhouse/chtype/
//
// In plain `go test` runs the seeds, including the system.columns corpus,
// double as a regression table.

import (
	"bufio"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/chtype"
)

func addSystemColumnsSeeds(f *testing.F) {
	file, err := os.Open("testdata/system_columns_types.txt")
	if err != nil {
		f.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			f.Add(line)
		}
	}
	if err = sc.Err(); err != nil {
		f.Fatal(err)
	}
}

func FuzzParseRoundTrip(f *testing.F) {
	addSystemColumnsSeeds(f)
	f.Add("Tuple(\n    a UInt8,\n    b String)")
	f.Add("  LowCardinality( String )  ")
	f.Add("Tuple(`a b` UInt8, `c\\`d` String)")
	f.Add("Enum8('SHOW COLUMNS' = -3, 'it''s' = 0, 'x\\'y' = 1)")
	f.Add("AggregateFunction(quantiles(0.5, 0.9), UInt64)")
	f.Add("Decimal(38,10)")
	f.Add("DateTime64(9, 'UTC)")
	f.Add("Tuple(a UInt8,)")
	f.Add("Object('json')")
	f.Add("X(1e-5, +3, -2.5E+10)")

	f.Fuzz(func(t *testing.T, s string) {
		ty, err := chtype.Parse(s)
		if err != nil {
			return // rejection is fine; panics are not
		}
		canonical := ty.String()
		again, err := chtype.Parse(canonical)
		if err != nil {
			t.Fatalf("canonical spelling does not re-parse:\n in: %q\nout: %q\nerr: %v", s, canonical, err)
		}
		if !reflect.DeepEqual(ty, again) {
			t.Fatalf("round-trip changed the tree:\n in: %q\nout: %q\n  1: %#v\n  2: %#v", s, canonical, ty, again)
		}
		if again.String() != canonical {
			t.Fatalf("canonical spelling not a fixed point: %q → %q", canonical, again.String())
		}
	})
}

func FuzzUnescapeTotal(f *testing.F) {
	f.Add(`Tuple(Ts DateTime64(9,\'UTC\'))`)
	f.Add(`a''b`)
	f.Add(`plain`)
	f.Add(`a\nb\t\r\0`)
	f.Add(`a\\b`)
	f.Add(`trailing\`)
	f.Add(`'`)

	f.Fuzz(func(t *testing.T, body string) {
		if v := chtype.Unescape(body); len(v) > len(body) {
			t.Fatalf("Unescape lengthened its input: %q → %q", body, v)
		}
	})
}
