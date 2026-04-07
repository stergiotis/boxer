//go:build llm_generated_opus46

package compression

import (
	"compress/gzip"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var corpus string

func TestMain(m *testing.M) {
	corpus = generateCorpus(1_100_000)
	os.Exit(m.Run())
}

func generateCorpus(minSize int) (text string) {
	phrases := []string{
		"The architecture of modern distributed systems demands careful consideration of trade-offs between consistency and availability.",
		"In practice, most engineering teams optimize for latency at the expense of strict serializability.",
		"Functional programming enables compositional reasoning about complex state transformations.",
		"Immutable data structures prevent whole classes of concurrency bugs in shared-memory environments.",
		"Type systems serve as a form of lightweight formal verification for everyday software engineering.",
		"The CAP theorem places fundamental limits on the guarantees a distributed database can provide.",
		"Event sourcing captures every state change as an immutable sequence of domain events.",
		"Consensus protocols like Raft and Paxos form the backbone of replicated state machines.",
		"Garbage collection trades deterministic latency for programmer productivity and memory safety.",
		"Property-based testing explores the input space far more thoroughly than hand-written examples.",
		"Microservices introduce operational complexity that monoliths avoid but struggle to scale past.",
		"Content-addressable storage enables efficient deduplication and integrity verification.",
		"The normalized compression distance approximates Kolmogorov complexity for practical similarity measurement.",
		"Stylometric analysis leverages statistical patterns in text to attribute authorship with high confidence.",
		"Compression algorithms exploit redundancy in data, making them natural proxies for information content.",
		"Language models capture distributional semantics but lack the causal reasoning of symbolic systems.",
		"Cache coherence protocols ensure that multiple processors observe a consistent view of shared memory.",
		"Write-ahead logging guarantees durability without requiring synchronous writes to the main data file.",
		"Bloom filters provide space-efficient probabilistic set membership queries with controllable false-positive rates.",
		"Lock-free data structures avoid the overhead of mutual exclusion at the cost of algorithmic complexity.",
	}
	rng := rand.New(rand.NewSource(12345))
	var sb strings.Builder
	sb.Grow(minSize + 256)
	for sb.Len() < minSize {
		sb.WriteString(phrases[rng.Intn(len(phrases))])
		sb.WriteString(" ")
		if rng.Intn(8) == 0 {
			sb.WriteString("\n\n")
		}
	}
	text = sb.String()
	return
}

func newGzipSim(t testing.TB, ref string) (inst *Similarity) {
	t.Helper()
	gz := gzip.NewWriter(nil)
	var err error
	inst, err = NewSimilarity(ref, gz)
	require.NoError(t, err)
	return
}

func newZstdSim(t testing.TB, ref string) (inst *Similarity) {
	t.Helper()
	enc, err := zstd.NewWriter(nil)
	require.NoError(t, err)
	inst, err = NewSimilarity(ref, enc)
	require.NoError(t, err)
	return
}

func TestNcdIdenticalTexts(t *testing.T) {
	text := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 100)
	s := newGzipSim(t, text)

	xy, err := s.MeasureCompressedLength(text, text)
	require.NoError(t, err)

	x := s.InputCompressedLen()
	y := x

	ncd := CalculateNormalizedCompressionDistance(xy, x, y)
	t.Logf("NCD(identical): %f, C(x)=%d, C(xy)=%d", ncd, x, xy)

	assert.GreaterOrEqual(t, ncd, 0.0)
	assert.Less(t, ncd, 0.3)
}

func TestNcdDifferentTexts(t *testing.T) {
	text1 := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 100)
	text2 := strings.Repeat("Lorem ipsum dolor sit amet consectetur adipiscing elit. ", 100)

	s := newGzipSim(t, text1)

	xy, err := s.MeasureCompressedLength(text1, text2)
	require.NoError(t, err)
	y, err := s.MeasureCompressedLength(text2, "")
	require.NoError(t, err)
	x := s.InputCompressedLen()

	ncd := CalculateNormalizedCompressionDistance(xy, x, y)
	t.Logf("NCD(different): %f, C(x)=%d, C(y)=%d, C(xy)=%d", ncd, x, y, xy)

	assert.GreaterOrEqual(t, ncd, 0.5)
}

func TestNcdOrdering(t *testing.T) {
	ref := strings.Repeat("I think this is an interesting approach to the problem. ", 100)
	similar := strings.Repeat("I believe this is a fascinating method for solving issues. ", 100)
	different := strings.Repeat("01onal 9xf2 zzq17 ab8 kk plm!! 42 @# $% ^& *() += {}[] ", 100)

	s := newGzipSim(t, ref)
	x := s.InputCompressedLen()

	xy12, err := s.MeasureCompressedLength(ref, similar)
	require.NoError(t, err)
	y2, err := s.MeasureCompressedLength(similar, "")
	require.NoError(t, err)
	ncd12 := CalculateNormalizedCompressionDistance(xy12, x, y2)

	xy13, err := s.MeasureCompressedLength(ref, different)
	require.NoError(t, err)
	y3, err := s.MeasureCompressedLength(different, "")
	require.NoError(t, err)
	ncd13 := CalculateNormalizedCompressionDistance(xy13, x, y3)

	t.Logf("NCD(similar)=%f  NCD(different)=%f", ncd12, ncd13)
	assert.Less(t, ncd12, ncd13)
}

func TestNcdUint64Underflow(t *testing.T) {
	var xy, x, y uint64 = 100, 102, 105
	ncd := CalculateNormalizedCompressionDistance(xy, x, y)
	assert.LessOrEqual(t, ncd, 1.0, "uint64 underflow detected")
}

func TestNcdStabilityAcrossCompressors(t *testing.T) {
	refText := strings.Repeat("the architecture of modern systems requires careful consideration of trade-offs between consistency availability and partition tolerance. ", 50)
	levels := []int{gzip.BestSpeed, gzip.DefaultCompression, gzip.BestCompression}
	sameText := strings.Repeat("the design of contemporary systems demands thoughtful evaluation of trade-offs between reliability performance and fault tolerance. ", 50)
	diffText := strings.Repeat("!! 42 xyz @#$ pqr 999 banana orange!! helicopter zoom vroom 17!! ", 50)

	for _, level := range levels {
		t.Run(fmt.Sprintf("level=%d", level), func(t *testing.T) {
			gz, err := gzip.NewWriterLevel(nil, level)
			require.NoError(t, err)
			sim, err := NewSimilarity(refText, gz)
			require.NoError(t, err)
			x := sim.InputCompressedLen()

			xySame, err := sim.MeasureCompressedLength(refText, sameText)
			require.NoError(t, err)
			ySame, err := sim.MeasureCompressedLength(sameText, "")
			require.NoError(t, err)
			ncdSame := CalculateNormalizedCompressionDistance(xySame, x, ySame)

			xyDiff, err := sim.MeasureCompressedLength(refText, diffText)
			require.NoError(t, err)
			yDiff, err := sim.MeasureCompressedLength(diffText, "")
			require.NoError(t, err)
			ncdDiff := CalculateNormalizedCompressionDistance(xyDiff, x, yDiff)

			t.Logf("level=%d: NCD(same)=%.4f  NCD(diff)=%.4f", level, ncdSame, ncdDiff)
			assert.Less(t, ncdSame, ncdDiff)
		})
	}
}

func TestMeasureJointCompressedLength(t *testing.T) {
	ref := strings.Repeat("The quick brown fox. ", 100)
	cmp := strings.Repeat("Lorem ipsum dolor. ", 100)

	simGzip := newGzipSim(t, ref)
	joint, err := simGzip.MeasureJointCompressedLength(cmp)
	require.NoError(t, err)
	concat, err := simGzip.MeasureCompressedLength(ref, cmp)
	require.NoError(t, err)
	// For gzip (no dict), joint == concat
	assert.Equal(t, concat, joint, "gzip: MeasureJointCompressedLength should equal MeasureCompressedLength(ref, cmp)")

	simZstd := newZstdSim(t, ref)
	require.True(t, simZstd.HasDictOptimization())
	joint, err = simZstd.MeasureJointCompressedLength(cmp)
	require.NoError(t, err)
	assert.Greater(t, joint, uint64(0))
}

var benchSizes = []struct {
	name string
	size int
}{
	{"200B", 200},
	{"1KB", 1_000},
	{"10KB", 10_000},
	{"100KB", 100_000},
	{"1MB", 1_000_000},
}

func benchSlices(b *testing.B, size int) (ref, cmp string) {
	b.Helper()
	need := 2 * size
	src := corpus
	for len(src) < need {
		src += corpus
	}
	ref = src[:size]
	cmp = src[size : 2*size]
	return
}

func BenchmarkMeasureCompressedLength(b *testing.B) {
	b.Run("gzip", func(b *testing.B) {
		for _, sz := range benchSizes {
			b.Run(sz.name, func(b *testing.B) {
				ref, cmp := benchSlices(b, sz.size)
				sim := newGzipSim(b, ref)
				b.SetBytes(int64(len(ref) + len(cmp)))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_, err := sim.MeasureCompressedLength(ref, cmp)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	})
	b.Run("zstd", func(b *testing.B) {
		for _, sz := range benchSizes {
			b.Run(sz.name, func(b *testing.B) {
				ref, cmp := benchSlices(b, sz.size)
				sim := newZstdSim(b, ref)
				b.SetBytes(int64(len(ref) + len(cmp)))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_, err := sim.MeasureCompressedLength(ref, cmp)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	})
}

func BenchmarkCalculateNcd(b *testing.B) {
	b.Run("gzip", func(b *testing.B) {
		for _, sz := range benchSizes {
			b.Run(sz.name, func(b *testing.B) {
				ref, cmp := benchSlices(b, sz.size)
				sim := newGzipSim(b, ref)
				x := sim.InputCompressedLen()
				b.SetBytes(int64(len(ref)+len(cmp)) + int64(len(cmp)))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					xy, err := sim.MeasureCompressedLength(ref, cmp)
					if err != nil {
						b.Fatal(err)
					}
					y, err := sim.MeasureCompressedLength(cmp, "")
					if err != nil {
						b.Fatal(err)
					}
					_ = CalculateNormalizedCompressionDistance(xy, x, y)
				}
			})
		}
	})
	b.Run("zstd_no_dict", func(b *testing.B) {
		for _, sz := range benchSizes {
			b.Run(sz.name, func(b *testing.B) {
				ref, cmp := benchSlices(b, sz.size)
				sim := newZstdSim(b, ref)
				x := sim.InputCompressedLen()
				b.SetBytes(int64(len(ref)+len(cmp)) + int64(len(cmp)))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					xy, err := sim.MeasureCompressedLength(ref, cmp)
					if err != nil {
						b.Fatal(err)
					}
					y, err := sim.MeasureCompressedLength(cmp, "")
					if err != nil {
						b.Fatal(err)
					}
					_ = CalculateNormalizedCompressionDistance(xy, x, y)
				}
			})
		}
	})
	b.Run("zstd_dict", func(b *testing.B) {
		for _, sz := range benchSizes {
			b.Run(sz.name, func(b *testing.B) {
				ref, cmp := benchSlices(b, sz.size)
				sim := newZstdSim(b, ref)
				require.True(b, sim.HasDictOptimization())
				x := sim.InputCompressedLen()
				b.SetBytes(int64(len(cmp)) + int64(len(cmp)))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					dictLen, err := sim.MeasureCompressedLengthWithDict(cmp)
					if err != nil {
						b.Fatal(err)
					}
					xy := x + dictLen
					y, err := sim.MeasureCompressedLength(cmp, "")
					if err != nil {
						b.Fatal(err)
					}
					_ = CalculateNormalizedCompressionDistance(xy, x, y)
				}
			})
		}
	})
}
