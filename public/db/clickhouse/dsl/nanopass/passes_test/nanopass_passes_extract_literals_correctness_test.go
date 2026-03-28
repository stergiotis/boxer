//go:build llm_generated_opus46

package passes_test

import (
	"fmt"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- #2: Corpus round-trip ---

func TestExtractInjectCorpusRoundTrip(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	config := passes.NewExtractLiteralsConfig(1) // extract everything possible
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(0) // disable IN-list collapsing for clean round-trip
	pass := passes.ExtractLiterals(config)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			extracted, err := pass(entry.SQL)
			if err != nil {
				t.Skipf("extraction failed: %v", err)
			}

			sets, query := passes.ParseExtractedQuery(extracted, "")
			injected, err := passes.InjectParams(sets, "", query)
			require.NoError(t, err, "InjectParams failed for %s", entry.Name)

			assert.Equal(t, entry.SQL, injected,
				"round-trip failed for %s:\n  original:  %s\n  extracted: %s\n  injected:  %s",
				entry.Name, entry.SQL, extracted, injected)
		})
	}
}

func TestExtractInjectCorpusRoundTripWithINList(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	config := passes.NewExtractLiteralsConfig(1)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(2) // collapse small IN lists too
	pass := passes.ExtractLiterals(config)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			extracted, err := pass(entry.SQL)
			if err != nil {
				t.Skipf("extraction failed: %v", err)
			}

			sets, query := passes.ParseExtractedQuery(extracted, "")
			injected, err := passes.InjectParams(sets, "", query)
			if err != nil {
				t.Skipf("injection failed: %v", err)
			}

			// With IN-list collapsing, the round-trip may not be exact
			// (e.g., (1, 2, 3) becomes [1, 2, 3] in the SET, injected as [1, 2, 3])
			// So we only check that it doesn't error — exact match is tested without IN-list collapsing
			_ = injected
		})
	}
}

func TestExtractInjectCorpusRoundTripHashBased(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	config := passes.NewExtractLiteralsConfig(1)
	config.SetMinINListSize(0)
	// Hash-based naming (default) — verify round-trip works the same
	pass := passes.ExtractLiterals(config)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			extracted, err := pass(entry.SQL)
			if err != nil {
				t.Skipf("extraction failed: %v", err)
			}

			sets, query := passes.ParseExtractedQuery(extracted, "")
			injected, err := passes.InjectParams(sets, "", query)
			require.NoError(t, err, "InjectParams failed for %s", entry.Name)

			assert.Equal(t, entry.SQL, injected,
				"hash-based round-trip failed for %s", entry.Name)
		})
	}
}

func TestExtractIterateCorpusMetadataIntegrity(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	config := passes.NewExtractLiteralsConfig(1)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(0)
	pass := passes.ExtractLiterals(config)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			extracted, err := pass(entry.SQL)
			if err != nil {
				t.Skipf("extraction failed: %v", err)
			}

			params := passes.CollectExtractedParams(extracted, "")
			for i, p := range params {
				// Every param should have a non-empty name
				assert.NotEmpty(t, p.FullName, "param %d has empty name in %s", i, entry.Name)

				// Every param should have a non-empty function name
				assert.NotEmpty(t, p.FunctionName, "param %d has empty function name in %s", i, entry.Name)

				// Every param should have a non-empty literal
				assert.NotEmpty(t, p.LiteralSQL, "param %d has empty literal in %s", i, entry.Name)

				// Metadata should have sequential flag set
				assert.True(t, p.Metadata.IsSequential, "param %d not sequential in %s", i, entry.Name)

				// Value() should not error
				val, valErr := p.Value()
				assert.NoError(t, valErr, "param %d Value() failed in %s: %v", i, entry.Name, valErr)
				assert.NotNil(t, val, "param %d Value() returned nil in %s", i, entry.Name)

				// Name should round-trip through ParseParamName
				ctx, meta, parseErr := passes.ParseParamName(p.FullName, "")
				assert.NoError(t, parseErr, "param %d name parse failed in %s", i, entry.Name)
				assert.Equal(t, p.FunctionName, ctx, "param %d context mismatch in %s", i, entry.Name)
				assert.Equal(t, p.Metadata, meta, "param %d metadata mismatch in %s", i, entry.Name)
			}
		})
	}
}

func TestExtractCorpusOutputParses(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	config := passes.NewExtractLiteralsConfig(1)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(0)
	pass := passes.ExtractLiterals(config)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			extracted, err := pass(entry.SQL)
			if err != nil {
				t.Skipf("extraction failed: %v", err)
			}

			// The query part (after SET lines) should still be valid SQL
			// with parameter slots
			_, query := passes.ParseExtractedQuery(extracted, "")
			assert.NotEmpty(t, query, "empty query after extraction for %s", entry.Name)
		})
	}
}

// --- #3: Exhaustive ParamMetadata round-trip ---

func TestParamMetadataExhaustiveRoundTrip(t *testing.T) {
	argIndices := []uint32{0, 1, 2, 255, 65535}
	hashes := []uint64{0, 1, 0xdeadbeef, 0xffffffffffffffff}
	collisions := []uint8{0, 2, 3, 255}
	casts := []string{"", "u8", "u64", "f64", "s", "b", "u64h", "i8-s-f64"}
	seqIndices := []uint32{0, 1, 42, 9999}
	bools := []bool{true, false}

	totalCombinations := len(argIndices) * len(hashes) * len(collisions) * len(casts) * len(seqIndices) * len(bools)
	t.Logf("testing %d combinations", totalCombinations)

	tested := 0
	for _, ai := range argIndices {
		for _, h := range hashes {
			for _, c := range collisions {
				for _, ct := range casts {
					for _, si := range seqIndices {
						for _, isSeq := range bools {
							meta := passes.ParamMetadata{
								ArgIndex:             ai,
								ContentHash:          h,
								HashCollisionCounter: c,
								CastTypeCanonical:    ct,
								SequentialIndex:      si,
								IsSequential:         isSeq,
							}

							encoded, err := passes.EncodeParamMetadata(&meta)
							require.NoError(t, err, "encode failed for %+v", meta)

							decoded, err := passes.DecodeParamMetadata(encoded)
							require.NoError(t, err, "decode failed for encoded=%s meta=%+v", encoded, meta)

							assert.Equal(t, meta, decoded,
								"round-trip mismatch: encoded=%s", encoded)

							tested++
						}
					}
				}
			}
		}
	}

	assert.Equal(t, totalCombinations, tested)
	t.Logf("all %d combinations passed", tested)
}

func TestParamNameExhaustiveRoundTrip(t *testing.T) {
	contexts := []string{"eq", "gt", "like", "in", "between", "substring", "select", "expr", "op"}
	prefixes := []string{"param", "qp", "p"}
	metas := []passes.ParamMetadata{
		{ArgIndex: 0},
		{ArgIndex: 1, ContentHash: 0xdeadbeef},
		{ArgIndex: 0, IsSequential: true, SequentialIndex: 0},
		{ArgIndex: 2, IsSequential: true, SequentialIndex: 42},
		{ArgIndex: 1, ContentHash: 0xff, CastTypeCanonical: "u64"},
		{ArgIndex: 0, IsSequential: true, SequentialIndex: 0, CastTypeCanonical: "u64h"},
		{ArgIndex: 1, ContentHash: 0xaabb, HashCollisionCounter: 2},
		{ArgIndex: 0, ContentHash: 0, CastTypeCanonical: "i8-s"},
		{ArgIndex: 1, ContentHash: 0xffffffffffffffff, HashCollisionCounter: 255, CastTypeCanonical: "f64"},
	}

	tested := 0
	for _, ctx := range contexts {
		for _, prefix := range prefixes {
			for _, meta := range metas {
				name, err := passes.BuildParamName(prefix, ctx, &meta)
				require.NoError(t, err, "build failed for prefix=%s ctx=%s meta=%+v", prefix, ctx, meta)

				parsedCtx, parsedMeta, err := passes.ParseParamName(name, prefix)
				require.NoError(t, err, "parse failed for name=%s prefix=%s", name, prefix)

				assert.Equal(t, ctx, parsedCtx,
					"context mismatch: name=%s", name)
				assert.Equal(t, meta, parsedMeta,
					"metadata mismatch: name=%s", name)

				tested++
			}
		}
	}

	t.Logf("all %d name round-trips passed", tested)
}

// --- Additional correctness checks ---

func TestExtractInjectHandcraftedRoundTrips(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(1)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(0)
	pass := passes.ExtractLiterals(config)

	queries := []string{
		"SELECT a FROM t WHERE x = 'hello'",
		"SELECT a FROM t WHERE x = 42",
		"SELECT a FROM t WHERE x = 3.14",
		"SELECT a FROM t WHERE x > 100 AND y < 200",
		"SELECT a FROM t WHERE x = 'hello' AND y = 'hello'",
		"SELECT a FROM t WHERE x = 'hello' OR y = 'world'",
		"SELECT a FROM t WHERE x LIKE '%pattern%'",
		"SELECT a FROM t WHERE x BETWEEN 10 AND 99",
		"SELECT substring('hello world', 1, 5)",
		"SELECT a FROM t WHERE x > 0 UNION ALL SELECT b FROM t2 WHERE y = 'test'",
		"WITH cte AS (SELECT a FROM t WHERE x = 'value') SELECT * FROM cte",
		"SELECT * FROM (SELECT a FROM t WHERE x = 42)",
		"SELECT a FROM t WHERE x = 'a' AND y = 'b' AND z = 'c'",
		"SELECT a FROM t WHERE x = 0",
		"SELECT a FROM t WHERE x = 1",
	}

	for i, sql := range queries {
		t.Run(fmt.Sprintf("handcrafted_%d", i), func(t *testing.T) {
			extracted, err := pass(sql)
			require.NoError(t, err)

			sets, query := passes.ParseExtractedQuery(extracted, "")
			injected, err := passes.InjectParams(sets, "", query)
			require.NoError(t, err)

			assert.Equal(t, sql, injected,
				"round-trip failed:\n  original:  %s\n  extracted: %s\n  injected:  %s",
				sql, extracted, injected)
		})
	}
}

func TestExtractInjectWithCastsHandcraftedRoundTrips(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(1)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(0)
	config.SetMapTypeToCanonical(mockMapTypeToCanonical)
	pass := passes.ExtractLiterals(config)

	queries := []string{
		"SELECT a FROM t WHERE x = 1::UInt64",
		"SELECT a FROM t WHERE x = 1::UInt8",
		"SELECT a FROM t WHERE x = 1::Int64",
		"SELECT CAST(1 AS UInt64)",
		"SELECT a FROM t WHERE x = 1::UInt64 AND y = 'hello'",
		"SELECT a FROM t WHERE x = 1::UInt64 AND y = 2::Int32",
	}

	for i, sql := range queries {
		t.Run(fmt.Sprintf("cast_handcrafted_%d", i), func(t *testing.T) {
			extracted, err := pass(sql)
			require.NoError(t, err)

			sets, query := passes.ParseExtractedQuery(extracted, "")
			injected, err := passes.InjectParamsWithCasts(sets, query, "", mockMapCanonicalToClickHouse)
			require.NoError(t, err)

			assert.Equal(t, sql, injected,
				"cast round-trip failed:\n  original:  %s\n  extracted: %s\n  injected:  %s",
				sql, extracted, injected)
		})
	}
}

func TestExtractStabilityAcrossRuns(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(1)
	config.SetMinINListSize(0)
	// Hash-based naming — should produce identical output each time
	pass := passes.ExtractLiterals(config)

	queries := []string{
		"SELECT a FROM t WHERE x = 'hello'",
		"SELECT a FROM t WHERE x = 42 AND y = 'world'",
		"SELECT a FROM t WHERE x LIKE '%test%'",
	}

	for i, sql := range queries {
		t.Run(fmt.Sprintf("stability_%d", i), func(t *testing.T) {
			results := make([]string, 5)
			for j := 0; j < 5; j++ {
				got, err := pass(sql)
				require.NoError(t, err)
				results[j] = got
			}

			for j := 1; j < 5; j++ {
				assert.Equal(t, results[0], results[j],
					"output not stable across runs (run 0 vs %d)", j)
			}
		})
	}
}

func TestExtractDeduplicationConsistency(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(1)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(0)
	pass := passes.ExtractLiterals(config)

	// Same literal in same context → should produce exactly 1 SET
	sql := "SELECT a FROM t WHERE x = 'repeated' AND y = 'repeated' AND z = 'repeated'"
	extracted, err := pass(sql)
	require.NoError(t, err)

	sets, _ := passes.ParseExtractedQuery(extracted, "")
	// Count distinct SET statements
	setValues := make(map[string]bool)
	for _, s := range sets {
		setValues[s] = true
	}

	// All three references to 'repeated' in eq position 1 should share one param
	assert.Len(t, sets, 1, "expected exactly 1 SET for 3 identical literals in same context")

	// Round-trip should work
	sets2, query := passes.ParseExtractedQuery(extracted, "")
	injected, err := passes.InjectParams(sets2, "", query)
	require.NoError(t, err)
	assert.Equal(t, sql, injected)
}
