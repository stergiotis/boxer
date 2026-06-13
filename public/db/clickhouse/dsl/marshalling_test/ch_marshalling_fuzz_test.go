package marshalling_test

// Fuzz targets for the marshalling codecs:
//
//	FuzzEscapeStringRoundTrip — UnescapeString∘EscapeString is identity on
//	                            arbitrary byte strings.
//	FuzzUnescapeStringTotal   — UnescapeString never panics; when it
//	                            succeeds, re-escaping round-trips.
//	FuzzScalarLiteralStable   — MarshalScalarToSQL∘UnmarshalScalarLiteral
//	                            is idempotent on its image: a successful
//	                            unmarshal marshals to a normal form that
//	                            re-unmarshals and re-marshals to itself
//	                            (value- and type-domain stable; NaN safe
//	                            because comparison happens on the text).
//
// Run e.g.:
//
//	go test -run xxx -fuzz FuzzScalarLiteralStable -fuzztime 60s ./public/db/clickhouse/dsl/marshalling_test/

import (
	"testing"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
)

func FuzzEscapeStringRoundTrip(f *testing.F) {
	f.Add("plain")
	f.Add("it's")
	f.Add("a\nb\tc\\d")
	f.Add("nul\x00byte")
	f.Add("héllø")
	f.Add("")

	f.Fuzz(func(t *testing.T, s string) {
		lit := marshalling.EscapeString(s)
		// boxer's parser requires valid UTF-8 — EscapeString must never
		// emit a byte string the parser would reject, even for input that
		// itself carries invalid bytes.
		if !utf8.ValidString(lit) {
			t.Fatalf("EscapeString produced invalid UTF-8 for %q: %q", s, lit)
		}
		back, err := marshalling.UnescapeString(lit)
		if err != nil {
			t.Fatalf("EscapeString produced undecodable literal %q for %q: %v", lit, s, err)
		}
		if back != s {
			t.Fatalf("escape round-trip broken: %q → %q → %q", s, lit, back)
		}
	})
}

func FuzzUnescapeStringTotal(f *testing.F) {
	f.Add("'simple'")
	f.Add(`'\x41A\U00000041'`)
	f.Add("'unterminated")
	f.Add(`'\`)
	f.Add("''")
	f.Add("'''")

	f.Fuzz(func(t *testing.T, raw string) {
		val, err := marshalling.UnescapeString(raw)
		if err != nil {
			return // rejection is fine; panics are not
		}
		relit := marshalling.EscapeString(val)
		back, err := marshalling.UnescapeString(relit)
		if err != nil || back != val {
			t.Fatalf("re-escape unstable: %q → %q → %q → %q (err=%v)", raw, val, relit, back, err)
		}
	})
}

func FuzzScalarLiteralStable(f *testing.F) {
	f.Add("42")
	f.Add("-42")
	f.Add("3.25")
	f.Add("5.0")
	f.Add("0777")
	f.Add("0x10")
	f.Add("1e21")
	f.Add("'str'")
	f.Add("true")
	f.Add("NULL")
	f.Add("inf")
	f.Add("-Inf")
	f.Add("nan")

	f.Fuzz(func(t *testing.T, token string) {
		lit, err := marshalling.UnmarshalScalarLiteral(token)
		if err != nil {
			return // rejection is fine
		}
		sql, err := marshalling.MarshalScalarToSQL(lit)
		if err != nil {
			t.Fatalf("unmarshalled %q but cannot marshal back: %v", token, err)
		}
		lit2, err := marshalling.UnmarshalScalarLiteral(sql)
		if err != nil {
			t.Fatalf("normal form %q (from %q) does not re-unmarshal: %v", sql, token, err)
		}
		sql2, err := marshalling.MarshalScalarToSQL(lit2)
		if err != nil {
			t.Fatalf("normal form %q (from %q) does not re-marshal: %v", sql, token, err)
		}
		if sql2 != sql {
			t.Fatalf("normal form unstable for %q: %q → %q", token, sql, sql2)
		}
		// Type-domain stability: the normal form must preserve the scalar
		// type (a float must not come back as an integer).
		if (lit.ScalarType == nil) != (lit2.ScalarType == nil) {
			t.Fatalf("null-ness changed for %q: %v vs %v", token, lit.ScalarType, lit2.ScalarType)
		}
		if lit.ScalarType != nil && lit.ScalarType.String() != lit2.ScalarType.String() {
			t.Fatalf("scalar type changed for %q: %s → %s (normal form %q)",
				token, lit.ScalarType, lit2.ScalarType, sql)
		}
	})
}
