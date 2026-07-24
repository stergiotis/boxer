package antlr4utils

import (
	"bytes"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	fxcbor "github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/cbor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/grammar"
)

// parseSignature builds a real parse tree rather than a fake, so the
// recogniser's rule and symbolic name tables are the genuine ones the
// converter indexes into.
func parseSignature(t *testing.T, src string) (parser antlr.Recognizer, tree antlr.Tree) {
	t.Helper()
	lexer := grammar.NewCanonicalTypeSignatureLexer(antlr.NewInputStream(src))
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	p := grammar.NewCanonicalTypeSignatureParser(stream)
	p.RemoveErrorListeners()
	tree = p.CanonicalTypeSignature()
	require.NotNil(t, tree)
	return p, tree
}

func convertToCbor(t *testing.T, prefix string, src string) (out []byte) {
	t.Helper()
	parser, tree := parseSignature(t, src)
	buf := &bytes.Buffer{}
	enc := cbor.NewEncoder(buf, nil)
	conv := NewAntlrTreeToCbor(enc, prefix)
	require.NoError(t, conv.Convert(parser, tree))
	out = buf.Bytes()
	require.NoError(t, fxcbor.Wellformed(out), "converter emitted malformed CBOR: % x", out)
	return
}

// Regression, 2026-07-24 review: the prefix branches were inverted — a set
// contextPrefix took the path that dropped it and an empty one took the
// path that concatenated nothing, so the constructor argument never
// reached the output at all.
func TestConvertAppliesContextPrefix(t *testing.T) {
	const src = "u8"
	plain := convertToCbor(t, "", src)
	prefixed := convertToCbor(t, "ct:", src)

	assert.NotEqual(t, plain, prefixed, "contextPrefix made no difference to the output")
	assert.Contains(t, string(prefixed), "ct:", "the prefix must reach the encoded context name")
	assert.NotContains(t, string(plain), "ct:")
}

// The prefix is cosmetic: it must prepend to the context name, not replace
// or reorder anything else.
func TestConvertPrefixOnlyPrependsToContextNames(t *testing.T) {
	const src = "u8"
	_, tree := parseSignature(t, src)
	name := FormatContextTypeName(tree)
	require.NotEmpty(t, name)

	prefixed := string(convertToCbor(t, "P-", src))
	assert.Contains(t, prefixed, "P-"+name,
		"expected the prefixed context name %q in the output", "P-"+name)
}

func TestConvertNilTreeEncodesNil(t *testing.T) {
	buf := &bytes.Buffer{}
	enc := cbor.NewEncoder(buf, nil)
	conv := NewAntlrTreeToCbor(enc, "")
	require.NoError(t, conv.Convert(nil, nil))
	assert.Equal(t, []byte{0xf6}, buf.Bytes(), "a nil tree must encode as CBOR null")
}

func TestFormatContextTypeName(t *testing.T) {
	_, tree := parseSignature(t, "u8")
	name := FormatContextTypeName(tree)
	// Trailing "Context" is stripped, the package qualifier dropped, and
	// the leading rune lowered so the result matches a grammar rule name.
	assert.NotContains(t, name, "Context")
	assert.NotContains(t, name, ".")
	require.NotEmpty(t, name)
	assert.Equal(t, string(name[0]), string(name[0]|0x20), "leading rune must be lowercased")
}
