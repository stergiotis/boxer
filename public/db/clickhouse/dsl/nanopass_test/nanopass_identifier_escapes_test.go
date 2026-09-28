package nanopass_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stretchr/testify/assert"
)

// Quoted identifiers decode their backslash escapes as the server does.
// Expected names were read back from a live server with
// SELECT 1 AS <spelling> FORMAT JSONEachRow.
func TestDecodeIdentifierServerEscapes(t *testing.T) {
	cases := map[string]string{
		`"a\nb"`:    "a\nb",
		`"a\tb"`:    "a\tb",
		"`a\\x41b`": "aAb",
		`"a\db"`:    `a\db`,
		`"a\Nb"`:    "ab",
		`"a\\b"`:    `a\b`,
		`"a\"b"`:    `a"b`,
		`"a""b"`:    `a"b`,
		"`a``b`":    "a`b",
	}
	for spelling, want := range cases {
		assert.Equal(t, want, nanopass.DecodeIdentifier(spelling), "decode %s", spelling)
		assert.Equal(t, want, nanopass.DecodeIdentifier(nanopass.QuoteIdentifier(want)), "round-trip %q", want)
	}
	// "a\nb" and "anb" are different columns on the server.
	assert.NotEqual(t, nanopass.DecodeIdentifier(`"a\nb"`), nanopass.DecodeIdentifier(`"anb"`))
}
