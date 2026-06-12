package nanopass_test

// Fuzz targets for the nanopass core. Each encodes a small set of laws:
//
//	FuzzParse              — Parse is total (guards reject pathology, never
//	                         a panic); successful parses reconstruct the
//	                         input byte-for-byte; scope building terminates
//	                         and FlattenScopes yields no duplicates.
//	FuzzIdentifierCodec    — QuoteIdentifier∘DecodeIdentifier is identity
//	                         on arbitrary names; decoding never panics.
//	FuzzIsDiscardOutput    — quote-aware marker scan: no marker substring ⇒
//	                         false; marker prefix ⇒ true; never panics.
//	FuzzCanonicalizeFull   — differential oracle: every Grammar1-parseable
//	                         input either canonicalises to Grammar2-parseable
//	                         output or fails with a real error (never a
//	                         recovered panic).
//
// Run e.g.:
//
//	go test -run xxx -fuzz FuzzParse -fuzztime 60s ./public/db/clickhouse/dsl/nanopass_test/
//
// In plain `go test` runs the seed corpus doubles as a regression table.

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
)

// fuzzMaxInput keeps per-exec cost low; the input guards already bound
// pathological cost, but fuzz throughput wants small inputs.
const fuzzMaxInput = 4 << 10

func addCorpusSeeds(f *testing.F) {
	entries, err := testdata.LoadCorpus()
	if err != nil {
		f.Fatal(err)
	}
	for _, e := range entries {
		f.Add(e.SQL)
	}
}

func FuzzParse(f *testing.F) {
	addCorpusSeeds(f)
	f.Add("SELECT \x01 1")
	f.Add("SELECT ((((1))))")
	f.Add("SELECT 'unterminated")
	f.Add("SELECT 'héllø', `bt`, \"dq\" FROM t -- c")

	f.Fuzz(func(t *testing.T, sql string) {
		if len(sql) > fuzzMaxInput {
			t.Skip()
		}
		pr, err := nanopass.Parse(sql)
		if err != nil {
			return // rejected input is fine; panics are not
		}

		// Exact reconstruction: every byte was tokenised (lexer errors
		// reject), so an edit-free rewriter must reproduce the input.
		if got := nanopass.NewRewriter(pr).GetTextDefault(); got != sql {
			t.Fatalf("reconstruction mismatch:\n in: %q\nout: %q", sql, got)
		}

		// Scope building must terminate and deduplicate.
		scopes, err := nanopass.BuildScopes(pr, "db")
		if err != nil {
			return // structural error is acceptable; panic is not
		}
		seen := make(map[*nanopass.SelectScope]struct{})
		for _, s := range nanopass.FlattenScopes(scopes) {
			if _, dup := seen[s]; dup {
				t.Fatal("FlattenScopes yielded a duplicate scope")
			}
			seen[s] = struct{}{}
		}
	})
}

func FuzzIdentifierCodec(f *testing.F) {
	f.Add("simple")
	f.Add(`with"quote`)
	f.Add("with`tick")
	f.Add(`back\slash`)
	f.Add("héllø wörld")
	f.Add("")
	f.Add(`"`)
	f.Add("`x`")

	f.Fuzz(func(t *testing.T, name string) {
		quoted := nanopass.QuoteIdentifier(name)
		if len(quoted) < 2 || quoted[0] != '"' || quoted[len(quoted)-1] != '"' {
			t.Fatalf("QuoteIdentifier(%q) = %q — not double-quoted", name, quoted)
		}
		if back := nanopass.DecodeIdentifier(quoted); back != name {
			t.Fatalf("codec round-trip broken: %q → %q → %q", name, quoted, back)
		}
		// Decoding arbitrary input must be total (no panic) and stable.
		once := nanopass.DecodeIdentifier(name)
		_ = nanopass.DecodeIdentifier(once)
	})
}

func FuzzIsDiscardOutput(f *testing.F) {
	f.Add("SELECT 1")
	f.Add("SELECT '" + nanopass.PassDiscardOutputMarker + "'")
	f.Add(nanopass.PassDiscardOutputMarker)
	f.Add("'unterminated " + nanopass.PassDiscardOutputMarker)

	f.Fuzz(func(t *testing.T, s string) {
		got := nanopass.IsDiscardOutput(s)
		if !strings.Contains(s, nanopass.PassDiscardOutputMarker) && got {
			t.Fatalf("marker reported without marker substring in %q", s)
		}
		// A marker at position 0 is outside any quoting context.
		if !nanopass.IsDiscardOutput(nanopass.PassDiscardOutputMarker + s) {
			t.Fatalf("marker prefix not detected for suffix %q", s)
		}
	})
}

func FuzzCanonicalizeFull(f *testing.F) {
	addCorpusSeeds(f)

	pipeline := passes.CanonicalizeFull(16)
	f.Fuzz(func(t *testing.T, sql string) {
		if len(sql) > fuzzMaxInput {
			t.Skip()
		}
		if _, err := nanopass.Parse(sql); err != nil {
			t.Skip() // only Grammar1-parseable inputs are in-contract
		}
		out, err := pipeline.Run(sql)
		if err != nil {
			if strings.Contains(err.Error(), "panicked") {
				t.Fatalf("canonicalisation panicked on %q: %v", sql, err)
			}
			return // loud, typed failure is in-contract
		}
		if _, err := nanopass.ParseCanonical(out); err != nil {
			t.Fatalf("canonical output not Grammar2-parseable:\n in: %q\nout: %q\nerr: %v", sql, out, err)
		}
	})
}
