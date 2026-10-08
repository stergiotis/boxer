package play

import (
	"encoding/hex"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/cbor/diag"
)

// TestIdentityNotes_NameWhatThePositionsHold pins the notes over the facts
// fixture's sysmetrics row (ADR-0289 SD2): a membership reads as its channel
// and its registry name, a slot as its section and its column types, a plain
// column by its name, bytes that are text as text, a time in ISO 8601 — and
// every canonform leaf digest is paired with the attribute it is the digest
// of.
func TestIdentityNotes_NameWhatThePositionsHold(t *testing.T) {
	rec := factsRecord(t)
	cards := factsCards(t)
	comp, err := newIdentityComputer(cards.TableDesc(), cards.IR(), cards.Driver())
	require.NoError(t, err)
	canonItems, wireItem, err := comp.rowItems(cards.IR(), rec, 0)
	require.NoError(t, err)

	wire, err := diag.String(wireItem, comp.notes.canonwireOptions())
	require.NoError(t, err)
	for _, want := range []string{
		"/ low-card ref · sysm-mem-host /",
		"/ low-card ref · sysm-mem-total-bytes /",
		"/ slot · symbol · string /",
		"/ slot · u64-array · list of u64 /",
		`/ natural-key · "host-a" /`,
		"/ 2023-11-14T22:13:20Z /",
	} {
		assert.Contains(t, wire, want)
	}

	form, err := diag.String(canonItems, comp.notes.canonformOptions())
	require.NoError(t, err)
	assert.Contains(t, form, "/ attribute sysm-mem-host · leaf ")
	assert.Contains(t, form, "/ plains · the entity id is left out under this pin /")
	digests := regexp.MustCompile(`h'[0-9a-f]{64}'(.*)`).FindAllStringSubmatch(form, -1)
	require.Len(t, digests, 13, "one leaf digest per attribute item")
	for _, d := range digests {
		assert.True(t, strings.Contains(d[1], "/ leaf of sysm-"), "every digest is named: %q", d[1])
	}
}

// TestIdentityNotes_CoSectionAndIdentityShapes pins the membership shapes
// both forms share (ADR-0201 SD5) and a time with nanoseconds.
func TestIdentityNotes_CoSectionAndIdentityShapes(t *testing.T) {
	n := newIdentityNotes(nil, nil, "")
	// ref 0x2a, verbatim "ab", [ref, params], [verbatim, params], [params]
	for hexItem, want := range map[string]string{
		"182a":           "0x2a",
		"426162":         "ab",
		"82182a42702d":   "0x2a [p-]",
		"8242616242702d": "ab [p-]",
		"8142702d":       "params [p-]",
	} {
		assert.Equal(t, want, n.identity(mustHex(t, hexItem)), hexItem)
	}
	// [channel 2, ref 0x2a]
	assert.Equal(t, "high-card ref · 0x2a", n.wireMembership(mustHex(t, "8202182a")))
	// 1001({1: 0, -9: 5})
	assert.Equal(t, "1970-01-01T00:00:00.000000005Z", tagNote(1001, mustHex(t, "a2010028 05")))
	assert.Equal(t, "string, list of u64 + no value", signatureWords("s-u64h_"))
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	require.NoError(t, err)
	return b
}
