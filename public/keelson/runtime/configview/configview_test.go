//go:build llm_generated_opus47

package configview

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaskValue(t *testing.T) {
	cases := []struct {
		name   string
		spec   env.Spec
		raw    string
		set    bool
		reveal bool
		want   string
	}{
		{
			name: "unset wins over sensitive",
			spec: env.Spec{Sensitive: true},
			set:  false,
			want: unsetMarker,
		},
		{
			name:   "set sensitive masked when reveal=false",
			spec:   env.Spec{Sensitive: true},
			raw:    "hunter2",
			set:    true,
			reveal: false,
			want:   maskedSensitive,
		},
		{
			name:   "set sensitive revealed when reveal=true",
			spec:   env.Spec{Sensitive: true},
			raw:    "hunter2",
			set:    true,
			reveal: true,
			want:   "hunter2",
		},
		{
			name: "set non-sensitive renders raw",
			spec: env.Spec{Sensitive: false},
			raw:  "info",
			set:  true,
			want: "info",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := maskValue(tc.spec, tc.raw, tc.set, tc.reveal)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMaskDefault(t *testing.T) {
	// Empty default stays empty regardless of sensitivity — "no
	// default" is metadata, not a secret.
	empty := env.Spec{Sensitive: true}
	assert.Equal(t, "", maskDefault(empty, false))
	assert.Equal(t, "", maskDefault(empty, true))

	withDefault := env.Spec{Sensitive: true, Default: "topsecret"}
	assert.Equal(t, maskedSensitive, maskDefault(withDefault, false))
	assert.Equal(t, "topsecret", maskDefault(withDefault, true))

	nonSensitive := env.Spec{Sensitive: false, Default: "info"}
	assert.Equal(t, "info", maskDefault(nonSensitive, false))
}

func TestValueTooltip(t *testing.T) {
	// Both parts present.
	full := env.Spec{
		Default: "info",
		Origin:  env.Origin{Package: "github.com/example/owner"},
	}
	got := valueTooltip(full, false)
	assert.Contains(t, got, "default: info")
	assert.Contains(t, got, "declared in: github.com/example/owner")

	// Sensitive default masks under the tooltip too.
	sens := env.Spec{Sensitive: true, Default: "topsecret", Origin: env.Origin{Package: "p"}}
	got = valueTooltip(sens, false)
	assert.Contains(t, got, "default: "+maskedSensitive)

	// Empty default + empty origin → tooltip skipped entirely
	// (caller avoids the empty HoverText scope).
	empty := env.Spec{}
	assert.Equal(t, "", valueTooltip(empty, false))
}

func TestApplyFilterSubstring(t *testing.T) {
	specs := []env.Spec{
		{Name: "BOXER_LOG_LEVEL", Category: env.CategoryObservability, Description: "zerolog level"},
		{Name: "CLICKHOUSE_URL", Category: env.CategoryDatabase, Description: "CH HTTP URL"},
		{Name: "PEBBLE2_RUN_ID", Category: env.CategoryE("runinfo"), Description: "per-process run id"},
	}

	got := applyFilter(specs, Filter{Query: "log"})
	require.Len(t, got, 1)
	assert.Equal(t, "BOXER_LOG_LEVEL", got[0].Name)

	got = applyFilter(specs, Filter{Query: "process run"})
	require.Len(t, got, 1)
	assert.Equal(t, "PEBBLE2_RUN_ID", got[0].Name)

	got = applyFilter(specs, Filter{Query: "RUN"})
	require.Len(t, got, 1)
	assert.Equal(t, "PEBBLE2_RUN_ID", got[0].Name)

	got = applyFilter(specs, Filter{Query: "xyzzy"})
	assert.Empty(t, got)
}

func TestApplyFilterSortsByCategoryThenName(t *testing.T) {
	specs := []env.Spec{
		{Name: "Z_LATE", Category: env.CategoryDatabase},
		{Name: "A_EARLY", Category: env.CategoryDatabase},
		{Name: "M_MID", Category: env.CategoryObservability},
	}
	got := applyFilter(specs, Filter{})
	require.Len(t, got, 3)
	assert.Equal(t, "A_EARLY", got[0].Name)
	assert.Equal(t, "Z_LATE", got[1].Name)
	assert.Equal(t, "M_MID", got[2].Name)
}

func TestGroupByCategoryBucketsAdjacent(t *testing.T) {
	// applyFilter sorts by (Category, Name) so groupByCategory only
	// needs to bucket runs of adjacent same-category specs.
	specs := []env.Spec{
		{Name: "A_ONE", Category: "alpha"},
		{Name: "A_TWO", Category: "alpha"},
		{Name: "B_ONE", Category: "bravo"},
	}
	got := groupByCategory(specs)
	require.Len(t, got, 2)
	assert.Equal(t, env.CategoryE("alpha"), got[0].cat)
	assert.Equal(t, []string{"A_ONE", "A_TWO"}, []string{got[0].specs[0].Name, got[0].specs[1].Name})
	assert.Equal(t, env.CategoryE("bravo"), got[1].cat)
	require.Len(t, got[1].specs, 1)
}

func TestTypeShortLabelAndTone(t *testing.T) {
	// The chip text width drives column alignment; keeping these
	// fixed prevents an unintentional widening of the "name starts
	// here" offset.
	cases := []struct {
		typ   env.TypeE
		label string
		tone  badge.ToneE
	}{
		{env.TypeString, "str", badge.ToneNeutral},
		{env.TypeBool, "bool", badge.ToneSuccess},
		{env.TypeInt64, "int", badge.ToneInfo},
		{env.TypeDuration, "dur", badge.ToneInfo},
		{env.TypePath, "path", badge.ToneNeutral},
		{env.TypeCategorialString, "enum", badge.ToneWarning},
	}
	for _, tc := range cases {
		t.Run(string(tc.typ), func(t *testing.T) {
			assert.Equal(t, tc.label, typeShortLabel(tc.typ))
			assert.Equal(t, tc.tone, typeTone(tc.typ))
		})
	}
}

func TestCategoryIconCoversKnownCategories(t *testing.T) {
	// All boxer-declared categories + the three pebble2impl-local
	// ones land on a stable Phosphor glyph. Unknown categories fall
	// back to PhCircle.
	cases := map[env.CategoryE]string{
		env.CategoryObservability:   icons.PhWaveform,
		env.CategoryDev:             icons.PhCode,
		env.CategoryDatabase:        icons.PhDatabase,
		env.CategorySystem:          icons.PhDesktop,
		env.CategoryE("anchor"):     icons.PhAnchor,
		env.CategoryE("krypto"):     icons.PhKey,
		env.CategoryE("runinfo"):    icons.PhTag,
		env.CategoryE("__unknown"):  icons.PhCircle,
	}
	for cat, want := range cases {
		t.Run(string(cat), func(t *testing.T) {
			assert.Equal(t, want, categoryIcon(cat))
		})
	}
}

func TestTruncateAppendsEllipsisAndCount(t *testing.T) {
	short := strings.Repeat("a", 10)
	assert.Equal(t, short, truncate(short, 96))

	long := strings.Repeat("x", 200)
	got := truncate(long, 96)
	assert.True(t, strings.HasPrefix(got, strings.Repeat("x", 96)))
	assert.Contains(t, got, "(200 chars)")

	// max=0 disables truncation entirely.
	assert.Equal(t, long, truncate(long, 0))
}

func TestTruncateRuneAware(t *testing.T) {
	// "αβγ" is 3 runes / 6 bytes. A byte-based slice at max=2 would
	// cut β in half and produce invalid UTF-8; the rune-aware path
	// must keep "αβ" intact and report the rune count.
	assert.Equal(t, "αβ… (3 chars)", truncate("αβγ", 2))

	// At limit: no truncation, no ellipsis, no count suffix.
	assert.Equal(t, "αβγ", truncate("αβγ", 3))

	// Past limit: result must still be valid UTF-8 — the regression
	// guard for "byte slice landed mid-codepoint".
	all := strings.Repeat("α", 100)
	got := truncate(all, 50)
	assert.True(t, utf8.ValidString(got), "truncate produced invalid UTF-8: %q", got)
	assert.Contains(t, got, "(100 chars)")
}
