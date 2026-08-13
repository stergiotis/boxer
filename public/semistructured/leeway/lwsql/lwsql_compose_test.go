package lwsql

import (
	"slices"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stretchr/testify/require"
)

func newDefaultComposer(t *testing.T) *Composer {
	t.Helper()
	c, err := NewComposer(DefaultTableSegments())
	require.NoError(t, err)
	return c
}

// TestComposer_HintlessGoldens pins the hint-less physical forms — the same
// shapes the NameConditions goldens pin, minted through the generalized seam.
func TestComposer_HintlessGoldens(t *testing.T) {
	c := newDefaultComposer(t)

	plain, err := c.PlainColumn("mycol", "u64", []string{"item:oq"})
	require.NoError(t, err)
	require.Equal(t, "oq:mycol:u64:::0:", plain)

	tv, err := c.TaggedValueColumn("mysec", "myCol", "s", nil)
	require.NoError(t, err)
	require.Equal(t, "tv:mysec:my-col:val:s::::0::", tv)
}

// TestComposer_AspectSegmentsLand pins that each vocabulary lands in its own
// name segment (segment *encoding* is the aspect codecs' own concern).
func TestComposer_AspectSegmentsLand(t *testing.T) {
	c := newDefaultComposer(t)

	enc := encodingaspects.EncodeAspectsMustValidate(encodingaspects.AspectDeltaEncoding, encodingaspects.AspectLightGeneralCompression)
	sem := valueaspects.EncodeAspectsMustValidate(valueaspects.AspectScaleOfMeasurementNominal)
	plain, err := c.PlainColumn("mycol", "u64", []string{"item:id", "enc:delta-encoding", "enc:light-general-compression", "sem:scale-of-measurement-nominal"})
	require.NoError(t, err)
	require.Equal(t, "id:mycol:u64:"+enc.String()+":"+sem.String()+":0:", plain)

	use := useaspects.EncodeAspectsMustValidate(useaspects.AspectTlpAmber)
	tv, err := c.TaggedValueColumn("mysec", "v", "u64h", []string{"use:tlp-amber"})
	require.NoError(t, err)
	require.Equal(t, "tv:mysec:v:val:u64h::"+use.String()+"::0::", tv)
}

// TestComposer_MatchesGenerator is the anti-drift acceptance criterion: every
// name the Composer mints for a section must be byte-identical to what the
// DDL generator derives for the same authored table. The composer must never
// become a second opinion on naming.
func TestComposer_MatchesGenerator(t *testing.T) {
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	manip.SetTableName("t")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
	hints := encodingaspects.EncodeAspectsMustValidate(encodingaspects.AspectLightGeneralCompression)
	manip.MergeTaggedValueColumn("symbol", "value", ctabb.S, hints, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common.MembershipSpecLowCardRef, "", "")
	manip.MergeTaggedValueColumn("meas", "reading", ctabb.U64h, encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common.MembershipSpecHighCardVerbatim, "", "")
	tbl, err := manip.BuildTableDesc()
	require.NoError(t, err)

	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tbl, tech))
	phys := make([]common.PhysicalColumnDesc, 0, 32)
	for cc, cp := range ir.IterateColumnProps() {
		phys, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, phys, common.TableRowConfigMultiAttributesPerRow)
		require.NoError(t, err)
	}
	generated := make([]string, 0, len(phys))
	for _, p := range phys {
		generated = append(generated, p.String())
	}

	c := newDefaultComposer(t)
	type mint struct {
		name string
		fn   func() (string, error)
	}
	mints := []mint{
		{"plain id", func() (string, error) { return c.PlainColumn("id", "u64", []string{"item:id"}) }},
		{"symbol value", func() (string, error) {
			return c.TaggedValueColumn("symbol", "value", "s", []string{"enc:light-general-compression"})
		}},
		{"symbol membership", func() (string, error) { return c.MembershipColumn("symbol", "low-card-ref") }},
		{"symbol card", func() (string, error) { return c.SupportColumn("symbol", "lrcard") }},
		{"meas value", func() (string, error) { return c.TaggedValueColumn("meas", "reading", "u64h", nil) }},
		{"meas membership", func() (string, error) { return c.MembershipColumn("meas", "high-card-verbatim") }},
		{"meas card", func() (string, error) { return c.SupportColumn("meas", "hvcard") }},
		{"meas len", func() (string, error) { return c.SupportColumn("meas", "len") }},
	}
	for _, m := range mints {
		got, err := m.fn()
		require.NoError(t, err, m.name)
		require.Contains(t, generated, got, "%s: composed name not among generator-derived names", m.name)
	}
}

// TestComposer_ClosureWitness: a composed column set reads back through
// DiscoverTableFromColumnNames as a coherent table — the transform contract's
// closure rule, applied to minted names.
func TestComposer_ClosureWitness(t *testing.T) {
	c := newDefaultComposer(t)
	names := make([]string, 0, 4)
	for _, f := range []func() (string, error){
		func() (string, error) { return c.PlainColumn("id", "u64", []string{"item:id"}) },
		func() (string, error) { return c.TaggedValueColumn("mysec", "v", "s", nil) },
		func() (string, error) { return c.MembershipColumn("mysec", "low-card-ref") },
		func() (string, error) { return c.SupportColumn("mysec", "lrcard") },
	} {
		n, err := f()
		require.NoError(t, err)
		names = append(names, n)
	}
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	table, rowCfg, err := conv.DiscoverTableFromColumnNames(names)
	require.NoError(t, err)
	require.Equal(t, common.TableRowConfigMultiAttributesPerRow, rowCfg)
	require.Len(t, table.TaggedValuesSections, 1)
	sec := table.TaggedValuesSections[0]
	require.Equal(t, "mysec", string(sec.Name))
	require.True(t, sec.MembershipSpec.HasLowCardRefOnly())
}

func TestComposer_UnderscoreSeparator(t *testing.T) {
	seg := DefaultTableSegments()
	seg.Separator = "_"
	c, err := NewComposer(seg)
	require.NoError(t, err)
	tv, err := c.TaggedValueColumn("mysec", "v", "s", nil)
	require.NoError(t, err)
	require.Equal(t, "tv_mysec_v_val_s____0__", tv)
}

// TestComposer_SeparatorCollision: a spinal-cased name under a '-' separator
// would re-split at the wrong position; the composer refuses.
func TestComposer_SeparatorCollision(t *testing.T) {
	seg := DefaultTableSegments()
	seg.Separator = "-"
	c, err := NewComposer(seg)
	require.NoError(t, err)
	_, err = c.TaggedValueColumn("mysec", "myCol", "s", nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "separator")
}

func TestParsePlainSpecTokens_Rejections(t *testing.T) {
	cases := []struct {
		name   string
		tokens []string
		is     error
		msg    string
	}{
		{"missing item", []string{"enc:delta-encoding"}, ErrMissingItemToken, ""},
		{"duplicate item", []string{"item:oq", "item:id"}, nil, "duplicate item:"},
		{"use misroute", []string{"item:oq", "use:tlp-amber"}, ErrUseAspectOnPlainColumn, "tagged section"},
		{"bare token", []string{"item:oq", "privacy"}, nil, "no vocabulary prefix"},
		{"unknown item", []string{"item:zz"}, nil, "unknown plain item type"},
		{"unknown aspect", []string{"item:oq", "enc:nope"}, nil, "unknown aspect name"},
		{"family exclusivity", []string{"item:oq", "enc:light-general-compression", "enc:heavy-general-compression"}, nil, "at most one member"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParsePlainSpecTokens(tc.tokens)
			require.Error(t, err)
			if tc.is != nil {
				require.ErrorIs(t, err, tc.is)
			}
			if tc.msg != "" {
				require.ErrorContains(t, err, tc.msg)
			}
		})
	}
}

func TestParseTaggedSpecTokens_Rejections(t *testing.T) {
	_, err := ParseTaggedSpecTokens([]string{"item:oq"})
	require.ErrorIs(t, err, ErrItemTokenOnTaggedColumn)

	_, err = ParseTaggedSpecTokens([]string{"use:tlp-amber", "use:tlp-red"})
	require.ErrorContains(t, err, "at most one member")
}

func TestParseMembershipChannel(t *testing.T) {
	m, err := ParseMembershipChannel("low-card-ref")
	require.NoError(t, err)
	require.Equal(t, common.MembershipSpecLowCardRef, m)

	_, err = ParseMembershipChannel("low-card-ref-high-card-params")
	require.ErrorContains(t, err, "mixed")

	_, err = ParseMembershipChannel("none")
	require.ErrorContains(t, err, "unknown membership channel")

	_, err = ParseMembershipChannel("nope")
	require.ErrorContains(t, err, "unknown membership channel")
}

func TestSupportColumn_Rejections(t *testing.T) {
	c := newDefaultComposer(t)
	_, err := c.SupportColumn("mysec", "val")
	require.ErrorContains(t, err, "not a machine-derivable support role")
	_, err = c.SupportColumn("mysec", "cusumcard")
	require.ErrorContains(t, err, "not a machine-derivable support role")
	_, err = c.SupportColumn("mysec", "zzz")
	require.ErrorContains(t, err, "unknown support role")
}

// TestComposer_AllChannelsRoundTrip: every non-mixed channel mints a
// membership lane and its card lane, and the pair re-discovers together with
// a value column — the parametrized channels included.
func TestComposer_AllChannelsRoundTrip(t *testing.T) {
	channels := map[string]string{
		"low-card-ref":               "lrcard",
		"low-card-verbatim":          "lvcard",
		"low-card-ref-parametrized":  "lpcard",
		"high-card-ref":              "hrcard",
		"high-card-verbatim":         "hvcard",
		"high-card-ref-parametrized": "hpcard",
	}
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	for channel, cardRole := range channels {
		t.Run(channel, func(t *testing.T) {
			c, err := NewComposer(DefaultTableSegments())
			require.NoError(t, err)
			v, err := c.TaggedValueColumn("s", "v", "s", nil)
			require.NoError(t, err)
			m, err := c.MembershipColumn("s", channel)
			require.NoError(t, err)
			card, err := c.SupportColumn("s", cardRole)
			require.NoError(t, err)
			_, _, err = conv.DiscoverTableFromColumnNames([]string{v, m, card})
			require.NoError(t, err, "channel %s does not round-trip", channel)
		})
	}

	// Mixed value lanes are not per-column authorable (ADR-0181 §SD8), but
	// their shared card lanes are.
	c, err := NewComposer(DefaultTableSegments())
	require.NoError(t, err)
	for _, role := range []string{"lmrcard", "lmvcard"} {
		_, err = c.SupportColumn("s", role)
		require.NoError(t, err, role)
	}
}

// TestComposer_SectionNameFolding: differently-spelled section and column
// names fold to one canonical spelling in the minted physical name.
func TestComposer_SectionNameFolding(t *testing.T) {
	c, err := NewComposer(DefaultTableSegments())
	require.NoError(t, err)
	v, err := c.TaggedValueColumn("geoPoint", "pointLat", "f64", nil)
	require.NoError(t, err)
	require.Contains(t, v, "tv:geo-point:point-lat:")
	m, err := c.MembershipColumn("geo_point", "low-card-ref")
	require.NoError(t, err)
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	_, _, err = conv.DiscoverTableFromColumnNames([]string{v, m})
	require.NoError(t, err, "spellings must fold to one section")
}

func TestResolver_TableSegments(t *testing.T) {
	names := buildKnownNames(t)
	r := newTestResolver(names)
	seg, ok := r.TableSegments("", testTable)
	require.True(t, ok)
	require.Equal(t, ":", seg.Separator)
	require.Equal(t, common.TableRowConfigMultiAttributesPerRow, seg.TableRowConfig)

	// A composer over the adopted segments mints names that re-parse into
	// the same table (the NameConditions property, generalized).
	c, err := NewComposer(seg)
	require.NoError(t, err)
	minted, err := c.TaggedValueColumn("annot", "note", "s", nil)
	require.NoError(t, err)
	conv, err := ddl.NewHumanReadableNamingConvention(seg.Separator)
	require.NoError(t, err)
	_, _, err = conv.DiscoverTableFromColumnNames(append(slices.Clone(names), minted))
	require.NoError(t, err)

	_, ok = r.TableSegments("", "nosuchtable")
	require.False(t, ok)

	rp := NewResolver(passes.NewStaticSchemaProvider(map[string][]string{"plain": {"a", "b"}}))
	_, ok = rp.TableSegments("", "plain")
	require.False(t, ok)
}
