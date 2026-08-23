package chpack_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/semistructured/leeway/aspectcodec"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/chpack"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	clickhouse "github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stretchr/testify/require"
)

// expectedAspectTransformStatements assembles the golden for the six
// transform-bearing LW_ASPECT_* functions from the enum tables, with string
// assembly independent of the roster builder.
func expectedAspectTransformStatements() (stmts []string) {
	encIdx, encNames, encChars := goldenTables(encodingaspects.AllAspects)
	useIdx, useNames, useChars := goldenTables(useaspects.AllAspects)
	semIdx, semNames, semChars := goldenTables(valueaspects.AllAspects)
	names := func(fn string, idx string, nm string) string {
		return "CREATE OR REPLACE FUNCTION " + fn + " AS (seg) -> arrayMap(i -> transform(i, " + idx + ", " + nm + ", concat('unknown-', toString(i))), LW_ASPECT_DECODE(seg))"
	}
	hasFn := func(fn string, seg string, nm string, ch string) string {
		return "CREATE OR REPLACE FUNCTION " + fn + " AS (name, aspect) -> position(" + seg + "(name), 'z') = 0 AND position(" + seg + "(name), transform(aspect, " + nm + ", " + ch + ", '#')) > 0"
	}
	stmts = []string{
		names("LW_ASPECT_NAMES_ENC", encIdx, encNames),
		names("LW_ASPECT_NAMES_USE", useIdx, useNames),
		names("LW_ASPECT_NAMES_SEM", semIdx, semNames),
		hasFn("LW_ASPECT_HAS_ENC", "LW_ASPECT_SEG_ENC", encNames, encChars),
		hasFn("LW_ASPECT_HAS_USE", "LW_ASPECT_SEG_USE", useNames, useChars),
		hasFn("LW_ASPECT_HAS_SEM", "LW_ASPECT_SEG_SEM", semNames, semChars),
	}
	return
}

func goldenTables[E interface {
	Value() uint8
	String() string
}](all []E) (idx string, names string, chars string) {
	is := make([]string, 0, len(all))
	ns := make([]string, 0, len(all))
	cs := make([]string, 0, len(all))
	for _, a := range all {
		is = append(is, fmt.Sprintf("%d", a.Value()))
		ns = append(ns, "'"+a.String()+"'")
		cs = append(cs, "'"+string(aspectcodec.Alphabet[a.Value()+1])+"'")
	}
	return "[" + strings.Join(is, ", ") + "]", "[" + strings.Join(ns, ", ") + "]", "[" + strings.Join(cs, ", ") + "]"
}

// aspectProbe composes a real table through the manipulator, IR and the
// default naming convention: one plain column and one tagged section with
// known aspect sets. It returns the CREATE TABLE DDL (Memory engine) and the
// quoted physical names.
func aspectProbe(t *testing.T) (ddlSQL string, names []string) {
	t.Helper()
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	manip.SetTableName("aspect_probe")

	manip.PlainValueColumn(common.PlainItemTypeEntityId, "m", ctabb.U64).
		AddColumnValueSemantics(valueaspects.AspectMeasured, valueaspects.AspectHumanReadable).
		AddColumnEncodingHints(encodingaspects.AspectDeltaEncoding)
	sec := manip.TaggedValueSection("sec")
	sec.AddSectionUseAspects(useaspects.AspectTlpRed)
	sec.AddSectionMembership(common.MembershipSpecLowCardVerbatim)
	sec.TaggedValueColumn("value", ctabb.S).
		AddColumnValueSemantics(valueaspects.AspectSecret).
		AddColumnEncodingHints(encodingaspects.AspectSparse)

	tbl, err := manip.BuildTableDesc()
	require.NoError(t, err)
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tbl, tech))
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	ddlSQL, err = clickhouse.ComposeCreateTable("aspect_probe", ir, common.TableRowConfigMultiAttributesPerRow, conv, clickhouse.TableOptions{
		Engine: "Memory",
	})
	require.NoError(t, err)
	for _, m := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(ddlSQL, -1) {
		names = append(names, m[1])
	}
	require.NotEmpty(t, names)
	return
}

// TestAspectSegmentPositionsMatchNamingConvention verifies the part counts
// and segment positions the LW_ASPECT_SEG_* bodies pin against names
// composed by the real pipeline.
func TestAspectSegmentPositionsMatchNamingConvention(t *testing.T) {
	_, names := aspectProbe(t)

	plainSem, err := valueaspects.EncodeAspects(valueaspects.AspectMeasured, valueaspects.AspectHumanReadable)
	require.NoError(t, err)
	plainEnc, err := encodingaspects.EncodeAspects(encodingaspects.AspectDeltaEncoding)
	require.NoError(t, err)
	secUse, err := useaspects.EncodeAspects(useaspects.AspectTlpRed)
	require.NoError(t, err)
	valSem, err := valueaspects.EncodeAspects(valueaspects.AspectSecret)
	require.NoError(t, err)
	valEnc, err := encodingaspects.EncodeAspects(encodingaspects.AspectSparse)
	require.NoError(t, err)

	var sawPlain, sawTagged bool
	for _, n := range names {
		parts := strings.Split(n, ":")
		switch {
		case strings.HasPrefix(n, "id:"):
			require.Len(t, parts, 7, n)
			require.Equal(t, string(plainEnc), parts[3], "plain enc segment of %s", n)
			require.Equal(t, string(plainSem), parts[4], "plain sem segment of %s", n)
			sawPlain = true
		case strings.HasPrefix(n, "tv:sec:value:"):
			require.Len(t, parts, 11, n)
			require.Equal(t, string(valEnc), parts[5], "tagged enc segment of %s", n)
			require.Equal(t, string(secUse), parts[6], "tagged use segment of %s", n)
			require.Equal(t, string(valSem), parts[7], "tagged sem segment of %s", n)
			sawTagged = true
		case strings.HasPrefix(n, "tv:"):
			require.Len(t, parts, 11, n)
			require.Equal(t, string(secUse), parts[6], "support lane use segment of %s", n)
		}
	}
	require.True(t, sawPlain, "probe produced no plain column")
	require.True(t, sawTagged, "probe produced no tagged value column")
}

func aspectStatementsOnly() (stmts []string) {
	for _, f := range chpack.Functions() {
		if strings.HasPrefix(f.Name, "LW_ASPECT_") {
			stmts = append(stmts, chpack.Statement(f))
		}
	}
	return
}

// TestAspectUdfsAgainstClickHouseLocal installs the LW_ASPECT_* family into
// clickhouse-local, creates the probe table, and checks that SQL-side
// decoding agrees with the Go decoder — over literals and over
// system.columns (ADR-0182 verification).
func TestAspectUdfsAgainstClickHouseLocal(t *testing.T) {
	if _, ok := extbin.ClickHouseLocal.Resolve(); !ok {
		t.Skip("clickhouse not on PATH, skipping")
	}
	ddlSQL, names := aspectProbe(t)
	var plainName, taggedValName string
	taggedCols := 0
	for _, n := range names {
		if strings.HasPrefix(n, "id:") {
			plainName = n
		}
		if strings.HasPrefix(n, "tv:sec:value:") {
			taggedValName = n
		}
		if strings.HasPrefix(n, "tv:") {
			taggedCols++
		}
	}
	require.NotEmpty(t, plainName)
	require.NotEmpty(t, taggedValName)

	var script strings.Builder
	for _, s := range aspectStatementsOnly() {
		script.WriteString(s)
		script.WriteString(";\n")
	}
	script.WriteString(ddlSQL)
	script.WriteString(";\n")
	queries := []string{
		fmt.Sprintf("SELECT LW_ASPECT_NAMES_SEM(LW_ASPECT_SEG_SEM('%s'))", plainName),
		fmt.Sprintf("SELECT LW_ASPECT_NAMES_ENC(LW_ASPECT_SEG_ENC('%s'))", plainName),
		fmt.Sprintf("SELECT LW_ASPECT_NAMES_USE(LW_ASPECT_SEG_USE('%s'))", taggedValName),
		fmt.Sprintf("SELECT LW_ASPECT_DECODE(LW_ASPECT_SEG_SEM('%s'))", taggedValName),
		"SELECT count() FROM system.columns WHERE table = 'aspect_probe' AND LW_ASPECT_HAS_USE(name, 'tlp-red')",
		"SELECT count() FROM system.columns WHERE table = 'aspect_probe' AND LW_ASPECT_HAS_SEM(name, 'secret')",
		"SELECT count() FROM system.columns WHERE table = 'aspect_probe' AND LW_ASPECT_HAS_SEM(name, 'measured')",
		fmt.Sprintf("SELECT LW_ASPECT_HAS_SEM('%s', 'not-an-aspect')", plainName),
	}
	script.WriteString(strings.Join(queries, ";\n"))
	script.WriteString(";\n")

	cmd, err := extbin.ClickHouseLocal.Command(t.Context(), extbin.Opts{}, "--multiquery", "--output-format", "TSV")
	require.NoError(t, err)
	cmd.Stdin = strings.NewReader(script.String())
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoErrorf(t, cmd.Run(), "clickhouse local failed, stderr:\n%s", stderr.String())

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	require.Len(t, lines, len(queries), stdout.String())
	require.Equal(t, "['measured','human-readable']", lines[0])
	require.Equal(t, "['delta-encoding']", lines[1])
	require.Equal(t, "['tlp-red']", lines[2])
	require.Equal(t, fmt.Sprintf("[%d]", valueaspects.AspectSecret.Value()), lines[3])
	require.Equal(t, fmt.Sprintf("%d", taggedCols), lines[4], "every column of the section carries its use segment")
	require.Equal(t, "1", lines[5])
	require.Equal(t, "1", lines[6])
	require.Equal(t, "0", lines[7])
}
