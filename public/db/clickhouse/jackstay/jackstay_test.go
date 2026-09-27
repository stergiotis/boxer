package jackstay

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

func ref(db string, name string) datacatalog.TableRef {
	return datacatalog.TableRef{Database: db, Name: name}
}

func col(name string, typ string) ColumnInfo {
	return ColumnInfo{Name: name, Type: typ}
}

func mergeTree(r datacatalog.TableRef, sortingKey string, cols ...ColumnInfo) TableInfo {
	for i := range cols {
		cols[i].Position = uint64(i + 1)
	}
	return TableInfo{
		Ref:         r,
		Engine:      "MergeTree",
		SortingKey:  sortingKey,
		CreateQuery: "CREATE TABLE " + r.Database + "." + r.Name + " (`k` UInt64) ENGINE = MergeTree ORDER BY " + sortingKey,
		Columns:     cols,
	}
}

func newOps(t *testing.T) *common.TableOperations {
	t.Helper()
	ops, err := common.NewTableOperations()
	require.NoError(t, err)
	return ops
}

func TestQuoteIdent(t *testing.T) {
	assert.Equal(t, "`plain`", QuoteIdent("plain"))
	assert.Equal(t, "`we.ird`", QuoteIdent("we.ird"))
	assert.Equal(t, "`a\\`b\\\\c`", QuoteIdent("a`b\\c"))
	assert.Equal(t, "`db`.`t`", QuoteRef(ref("db", "t")))
}

func TestRetargetCreateQuery(t *testing.T) {
	target := ref("dst", "t")
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare", "CREATE TABLE src.t (`k` UInt64) ENGINE = MergeTree ORDER BY k",
			"CREATE TABLE IF NOT EXISTS `dst`.`t` (`k` UInt64) ENGINE = MergeTree ORDER BY k"},
		{"quoted name", "CREATE TABLE src.`we.ird` (`k` UInt64) ENGINE = Log",
			"CREATE TABLE IF NOT EXISTS `dst`.`t` (`k` UInt64) ENGINE = Log"},
		{"quoted database with escape", "CREATE TABLE `s\\`rc`.t (`k` UInt64) ENGINE = Log",
			"CREATE TABLE IF NOT EXISTS `dst`.`t` (`k` UInt64) ENGINE = Log"},
		{"uuid dropped", "CREATE TABLE src.t UUID '6f1c0f6e-0000-4000-8000-000000000000' (`k` UInt64) ENGINE = Log",
			"CREATE TABLE IF NOT EXISTS `dst`.`t` (`k` UInt64) ENGINE = Log"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := RetargetCreateQuery(c.in, target)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
	for _, bad := range []string{"", "CREATE VIEW src.v AS SELECT 1", "CREATE TABLE t (k UInt64)", "CREATE TABLE `unterminated.t (k UInt64)"} {
		_, err := RetargetCreateQuery(bad, target)
		assert.Error(t, err, bad)
	}
}

func TestAddColumnDDL(t *testing.T) {
	c := ColumnInfo{Name: "m", Type: "UInt64", DefaultKind: "MATERIALIZED", DefaultExpression: "k * 2", CompressionCodec: "CODEC(ZSTD(1))"}
	assert.Equal(t, "ALTER TABLE `d`.`t` ADD COLUMN IF NOT EXISTS `m` UInt64 MATERIALIZED k * 2 CODEC(ZSTD(1)) AFTER `k`",
		AddColumnDDL(ref("d", "t"), c, "k"))
	assert.Equal(t, "ALTER TABLE `d`.`t` ADD COLUMN IF NOT EXISTS `a` String FIRST",
		AddColumnDDL(ref("d", "t"), col("a", "String"), ""))
}

func TestJudge(t *testing.T) {
	ops := newOps(t)
	target := ref("dst", "t")
	src := mergeTree(ref("src", "t"), "k", col("k", "UInt64"), col("s", "String"), col("n", "UInt32"))

	t.Run("create", func(t *testing.T) {
		v, err := Judge(ops, &src, nil, target)
		require.NoError(t, err)
		assert.Equal(t, VerdictCreate, v.Verdict)
		require.Len(t, v.DDL, 1)
		assert.True(t, strings.HasPrefix(v.DDL[0], "CREATE TABLE IF NOT EXISTS `dst`.`t` "))
		assert.Equal(t, []string{"k", "s", "n"}, v.CopyColumns)
	})
	t.Run("identical", func(t *testing.T) {
		dst := mergeTree(target, "k", col("k", "UInt64"), col("s", "String"), col("n", "UInt32"))
		v, err := Judge(ops, &src, &dst, target)
		require.NoError(t, err)
		assert.Equal(t, VerdictIdentical, v.Verdict)
		assert.Empty(t, v.DDL)
		assert.Equal(t, []string{"k", "s", "n"}, v.CopyColumns)
	})
	t.Run("extend keeps source order", func(t *testing.T) {
		dst := mergeTree(target, "k", col("k", "UInt64"), col("extra", "String"))
		v, err := Judge(ops, &src, &dst, target)
		require.NoError(t, err)
		assert.Equal(t, VerdictExtend, v.Verdict)
		assert.Equal(t, []string{
			"ALTER TABLE `dst`.`t` ADD COLUMN IF NOT EXISTS `s` String AFTER `k`",
			"ALTER TABLE `dst`.`t` ADD COLUMN IF NOT EXISTS `n` UInt32 AFTER `s`",
		}, v.DDL)
		assert.Equal(t, []string{"k", "s", "n"}, v.CopyColumns)
		assert.Contains(t, strings.Join(v.Notes, "\n"), "extra")
	})
	t.Run("narrower", func(t *testing.T) {
		dst := mergeTree(target, "k", col("k", "UInt64"), col("s", "String"), col("n", "UInt32"), col("extra", "String"))
		v, err := Judge(ops, &src, &dst, target)
		require.NoError(t, err)
		assert.Equal(t, VerdictNarrower, v.Verdict)
		assert.Equal(t, []string{"k", "s", "n"}, v.CopyColumns)
	})
	t.Run("type differs", func(t *testing.T) {
		dst := mergeTree(target, "k", col("k", "UInt64"), col("s", "String"), col("n", "UInt64"))
		v, err := Judge(ops, &src, &dst, target)
		require.NoError(t, err)
		assert.Equal(t, VerdictIncompatible, v.Verdict)
		assert.Empty(t, v.CopyColumns)
		assert.Contains(t, strings.Join(v.Reasons, "\n"), "column n type differs")
	})
	t.Run("LowCardinality only is a note", func(t *testing.T) {
		dst := mergeTree(target, "k", col("k", "UInt64"), col("s", "LowCardinality(String)"), col("n", "UInt32"))
		v, err := Judge(ops, &src, &dst, target)
		require.NoError(t, err)
		assert.Equal(t, VerdictIdentical, v.Verdict)
		assert.Contains(t, strings.Join(v.Notes, "\n"), "LowCardinality")
	})
	t.Run("sorting key differs", func(t *testing.T) {
		dst := mergeTree(target, "k, s", col("k", "UInt64"), col("s", "String"), col("n", "UInt32"))
		v, err := Judge(ops, &src, &dst, target)
		require.NoError(t, err)
		assert.Equal(t, VerdictIncompatible, v.Verdict)
	})
	t.Run("sorting key whitespace is not a difference", func(t *testing.T) {
		s2 := mergeTree(ref("src", "t"), "k,  s", col("k", "UInt64"), col("s", "String"))
		dst := mergeTree(target, "k, s", col("k", "UInt64"), col("s", "String"))
		v, err := Judge(ops, &s2, &dst, target)
		require.NoError(t, err)
		assert.Equal(t, VerdictIdentical, v.Verdict)
	})
	t.Run("stored on source, alias on target", func(t *testing.T) {
		dst := mergeTree(target, "k", col("k", "UInt64"), col("s", "String"), ColumnInfo{Name: "n", Type: "UInt32", DefaultKind: "ALIAS", DefaultExpression: "1"})
		v, err := Judge(ops, &src, &dst, target)
		require.NoError(t, err)
		assert.Equal(t, VerdictIncompatible, v.Verdict)
	})
	t.Run("materialized on source is copied into a plain target column", func(t *testing.T) {
		s2 := mergeTree(ref("src", "t"), "k", col("k", "UInt64"), ColumnInfo{Name: "m", Type: "UInt64", DefaultKind: "MATERIALIZED", DefaultExpression: "k * 2"})
		dst := mergeTree(target, "k", col("k", "UInt64"), col("m", "UInt64"))
		v, err := Judge(ops, &s2, &dst, target)
		require.NoError(t, err)
		assert.Equal(t, []string{"k", "m"}, v.CopyColumns)
	})
	t.Run("materialized added is not copied", func(t *testing.T) {
		s2 := mergeTree(ref("src", "t"), "k", col("k", "UInt64"), ColumnInfo{Name: "m", Type: "UInt64", DefaultKind: "MATERIALIZED", DefaultExpression: "k * 2"})
		dst := mergeTree(target, "k", col("k", "UInt64"))
		v, err := Judge(ops, &s2, &dst, target)
		require.NoError(t, err)
		assert.Equal(t, VerdictExtend, v.Verdict)
		assert.Equal(t, []string{"k"}, v.CopyColumns)
	})
	t.Run("unsupported", func(t *testing.T) {
		view := TableInfo{Ref: ref("src", "v"), Engine: "View"}
		v, err := Judge(ops, &view, nil, target)
		require.NoError(t, err)
		assert.Equal(t, VerdictUnsupported, v.Verdict)
		inner := mergeTree(ref("src", ".inner_id.1234"), "k", col("k", "UInt64"))
		v, err = Judge(ops, &inner, nil, target)
		require.NoError(t, err)
		assert.Equal(t, VerdictUnsupported, v.Verdict)
		dst := TableInfo{Ref: target, Engine: "Dictionary"}
		v, err = Judge(ops, &src, &dst, target)
		require.NoError(t, err)
		assert.Equal(t, VerdictUnsupported, v.Verdict)
	})
}

// leewayColumns are the physical columns of a small leeway table (anchor's
// silver table), spelled with sep.
func leewayColumns(sep string) []ColumnInfo {
	names := []struct{ name, typ string }{
		{"id:id:u64:47::0:", "UInt64"},
		{"id:naturalKey:y:4::0:", "String"},
		{"tv:symbol:value:val:s:124::I:0::data", "Array(String)"},
		{"tv:symbol:lr:lr:u64:1247:::0::data", "Array(UInt64)"},
		{"tv:symbol:lrcard:lrcard:u64:4E:::0::data", "Array(UInt64)"},
	}
	cols := make([]ColumnInfo, 0, len(names))
	for _, n := range names {
		cols = append(cols, col(strings.ReplaceAll(n.name, ":", sep), n.typ))
	}
	return cols
}

func TestJudge_Leeway(t *testing.T) {
	ops := newOps(t)
	target := ref("dst", "silver")
	src := mergeTree(ref("anchor", "silver"), "k", leewayColumns(":")...)

	v, err := Judge(ops, &src, nil, target)
	require.NoError(t, err)
	assert.True(t, v.Leeway)

	same := mergeTree(target, "k", leewayColumns(":")...)
	v, err = Judge(ops, &src, &same, target)
	require.NoError(t, err)
	assert.Equal(t, VerdictIdentical, v.Verdict)
	assert.Equal(t, "equal", v.LeewayRelation)

	// The same shape spelled with the dump separator: equal as leeway, but no
	// column matches by name.
	mangled := mergeTree(target, "k", leewayColumns("_")...)
	v, err = Judge(ops, &src, &mangled, target)
	require.NoError(t, err)
	assert.Equal(t, "equal", v.LeewayRelation)
	assert.Equal(t, VerdictIncompatible, v.Verdict)
	assert.Empty(t, v.DDL)
}

func inventory(version string, dbs []string, tables ...TableInfo) *Inventory {
	inv := &Inventory{Server: ServerInfo{Version: version}, Tables: tables}
	for _, d := range dbs {
		inv.Databases = append(inv.Databases, DatabaseInfo{Name: d, Engine: "Atomic"})
	}
	return inv
}

func TestBuildPlan(t *testing.T) {
	ops := newOps(t)
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	srcEp := Endpoint{URL: "http://a:8123/", User: "default"}
	dstEp := Endpoint{URL: "http://b:8123/", User: "default"}
	src := inventory("26.8.1.1", []string{"system", "app", "other"},
		mergeTree(ref("app", "t1"), "k", col("k", "UInt64")),
		mergeTree(ref("app", "t2"), "k", col("k", "UInt64")),
		mergeTree(ref("other", "x"), "k", col("k", "UInt64")),
		mergeTree(ref("other", "silver"), "k", leewayColumns(":")...),
	)
	dst := inventory("26.3.2.1", []string{"system", "app"},
		mergeTree(ref("app", "t1"), "k", col("k", "UInt64")),
	)

	t.Run("all user databases", func(t *testing.T) {
		plan, err := BuildPlan(ops, srcEp, dstEp, src, dst, Selection{}, now)
		require.NoError(t, err)
		assert.Equal(t, []string{"app", "other"}, plan.Selection.Databases)
		require.Len(t, plan.Tables, 4)
		counts := plan.CountVerdicts()
		assert.Equal(t, 1, counts[VerdictIdentical])
		assert.Equal(t, 3, counts[VerdictCreate])
		assert.Equal(t, []string{"CREATE DATABASE IF NOT EXISTS `other`"}, plan.DatabaseDDL)
		require.Len(t, plan.Notes, 1)
		assert.Contains(t, plan.Notes[0], "releases differ")
		assert.True(t, plan.HasPendingDDL())
	})
	t.Run("leeway only", func(t *testing.T) {
		plan, err := BuildPlan(ops, srcEp, dstEp, src, dst, Selection{LeewayOnly: true}, now)
		require.NoError(t, err)
		require.Len(t, plan.Tables, 1)
		assert.Equal(t, ref("other", "silver"), plan.Tables[0].Source)
	})
	t.Run("database map", func(t *testing.T) {
		plan, err := BuildPlan(ops, srcEp, dstEp, src, dst, Selection{Databases: []string{"other"}, DatabaseMap: map[string]string{"other": "app"}}, now)
		require.NoError(t, err)
		require.Len(t, plan.Tables, 2)
		assert.Equal(t, ref("app", "x"), plan.Tables[0].Target)
		assert.Empty(t, plan.DatabaseDDL, "app exists on the target")
	})
	t.Run("refusals", func(t *testing.T) {
		_, err := BuildPlan(ops, srcEp, dstEp, src, dst, Selection{Databases: []string{"missing"}}, now)
		assert.Error(t, err)
		_, err = BuildPlan(ops, srcEp, dstEp, src, dst, Selection{Databases: []string{"system"}}, now)
		assert.Error(t, err)
		_, err = BuildPlan(ops, srcEp, dstEp, src, dst, Selection{Databases: []string{"app"}, DatabaseMap: map[string]string{"other": "z"}}, now)
		assert.Error(t, err, "mapping an unselected database")
		_, err = BuildPlan(ops, srcEp, srcEp, src, src, Selection{Databases: []string{"app"}}, now)
		assert.Error(t, err, "same server, same table")
		aliased := *src
		aliased.Server.UUID = "7c3a1f2e-0000-4000-8000-000000000001"
		_, err = BuildPlan(ops, srcEp, dstEp, &aliased, &aliased, Selection{Databases: []string{"app"}}, now)
		assert.Error(t, err, "one server behind two URLs")
		_, err = BuildPlan(ops, srcEp, dstEp, src, dst, Selection{DatabaseMap: map[string]string{"other": "app"}}, now)
		assert.NoError(t, err, "no name collides")
		collide := inventory("26.8.1.1", []string{"a", "b"},
			mergeTree(ref("a", "t"), "k", col("k", "UInt64")),
			mergeTree(ref("b", "t"), "k", col("k", "UInt64")))
		_, err = BuildPlan(ops, srcEp, dstEp, collide, dst, Selection{DatabaseMap: map[string]string{"a": "z", "b": "z"}}, now)
		assert.Error(t, err, "two sources onto one target")
	})
	t.Run("same server with a map", func(t *testing.T) {
		plan, err := BuildPlan(ops, srcEp, srcEp, src, src, Selection{Databases: []string{"app"}, DatabaseMap: map[string]string{"app": "app_copy"}}, now)
		require.NoError(t, err)
		assert.Equal(t, []string{"CREATE DATABASE IF NOT EXISTS `app_copy`"}, plan.DatabaseDDL)
	})
}

func TestPlan_SaveLoadRestate(t *testing.T) {
	ops := newOps(t)
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	srcEp := Endpoint{URL: "http://a:8123/", User: "default"}
	dstEp := Endpoint{URL: "http://b:8123/", User: "reader"}
	src := inventory("26.8.1.1", []string{"app"},
		mergeTree(ref("app", "t1"), "k", col("k", "UInt64"), col("s", "String")),
		mergeTree(ref("app", "silver"), "k", leewayColumns(":")...))
	dst := inventory("26.8.1.1", []string{"app"}, mergeTree(ref("app", "t1"), "k", col("k", "UInt64")))

	plan, err := BuildPlan(ops, srcEp, dstEp, src, dst, Selection{}, now)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "plan.json")
	require.NoError(t, plan.Save(path))
	loaded, err := LoadPlan(path)
	require.NoError(t, err)
	assert.Equal(t, plan, loaded)

	_, stale, err := Restate(ops, &loaded, src, dst, now.Add(time.Hour))
	require.NoError(t, err)
	assert.Empty(t, stale)

	// The plan's own DDL ran part-way: silver was created, t1 not yet
	// extended. That is progress, and only t1's statement remains.
	created := mergeTree(ref("app", "silver"), "k", leewayColumns(":")...)
	dstPartial := inventory("26.8.1.1", []string{"app"}, mergeTree(ref("app", "t1"), "k", col("k", "UInt64")), created)
	fresh, stale, err := Restate(ops, &loaded, src, dstPartial, now.Add(time.Hour))
	require.NoError(t, err)
	assert.Empty(t, stale)
	assert.Equal(t, loaded.Tables[0].DDL, fresh.Tables[0].DDL)
	assert.Empty(t, fresh.Tables[1].DDL)

	// Someone else added the column with another type: the plan's DDL is no
	// longer what the operator would be confirming.
	dst2 := inventory("26.8.1.1", []string{"app"}, mergeTree(ref("app", "t1"), "k", col("k", "UInt64"), col("s", "LowCardinality(UInt64)")))
	_, stale, err = Restate(ops, &loaded, src, dst2, now.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, []string{"app.t1: verdict extend is now incompatible"}, stale)

	loaded.FormatVersion = 99
	require.NoError(t, loaded.Save(path))
	_, err = LoadPlan(path)
	assert.Error(t, err)
}

func TestVerdict_Text(t *testing.T) {
	for _, v := range AllVerdicts {
		text, err := v.MarshalText()
		require.NoError(t, err)
		var back VerdictE
		require.NoError(t, back.UnmarshalText(text))
		assert.Equal(t, v, back)
	}
	var v VerdictE
	assert.Error(t, v.UnmarshalText([]byte("nope")))
}

func TestDDLClientConfig(t *testing.T) {
	cfg := DDLClientConfig(chclient.Config{URL: "http://h:8123/", User: "u"}, ddlGuardSettings)
	assert.True(t, strings.HasPrefix(cfg.URL, "http://h:8123/?allow_suspicious_low_cardinality_types=1&"))
	assert.Equal(t, "u", cfg.User)
	cfg = DDLClientConfig(chclient.Config{URL: "http://h:8123/?database=x"}, ddlGuardSettings)
	assert.True(t, strings.HasPrefix(cfg.URL, "http://h:8123/?database=x&allow_suspicious_low_cardinality_types=1&"))
	// Only the settings the target knows travel; an unknown one fails every request.
	cfg = DDLClientConfig(chclient.Config{URL: "http://h:8123/"}, []string{"allow_suspicious_codecs"})
	assert.Equal(t, "http://h:8123/?allow_suspicious_codecs=1", cfg.URL)
	cfg = DDLClientConfig(chclient.Config{URL: "http://h:8123/"}, nil)
	assert.Equal(t, "http://h:8123/", cfg.URL)
}

func TestNormalizeEndpointURL(t *testing.T) {
	assert.Equal(t, "http://host:8123/", NormalizeEndpointURL("host:8123"))
	assert.Equal(t, "https://host:8443/", NormalizeEndpointURL(" https://host:8443 "))
	assert.Equal(t, "", NormalizeEndpointURL(""))
	// The slash ends the path; a query string stays where it is.
	assert.Equal(t, "http://h:8123/?database=x", NormalizeEndpointURL("http://h:8123?database=x"))
	assert.Equal(t, "http://h:8123/?database=x", NormalizeEndpointURL("h:8123/?database=x"))
	assert.Equal(t, "http://h:8123/base/", NormalizeEndpointURL("http://h:8123/base"))
}
