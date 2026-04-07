//go:build llm_generated_opus46

package stylometry

import (
	"compress/gzip"
	"fmt"
	"iter"
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stergiotis/boxer/public/analytics/stats"
)

func newTestAnalyzer(t *testing.T, referenceText string) (inst *Analyzer) {
	t.Helper()
	conv := stats.NewConvergenceDetector(16, 1.0e-3)
	gz := gzip.NewWriter(nil)
	var err error
	inst, err = NewAnalyzer(referenceText, conv, gz)
	require.NoError(t, err)
	return
}

func TestMeasureNcdInstanceReuse(t *testing.T) {
	ref := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 200)
	a := newTestAnalyzer(t, ref)

	makeTexts := func(base string, n int) func(yield func(string) bool) {
		return func(yield func(string) bool) {
			for i := 0; i < n; i++ {
				if !yield(base) {
					return
				}
			}
		}
	}

	_, count1, _, mean1, _, _, _, err := a.MeasureNcdInstance(makeTexts(ref, 50))
	require.NoError(t, err)

	different := strings.Repeat("Lorem ipsum dolor sit amet consectetur adipiscing elit. ", 200)
	_, count2, _, mean2, _, _, _, err := a.MeasureNcdInstance(makeTexts(different, 50))
	require.NoError(t, err)

	t.Logf("Run1 (identical): mean=%.4f count=%d", mean1, count1)
	t.Logf("Run2 (different): mean=%.4f count=%d", mean2, count2)

	assert.GreaterOrEqual(t, count2, int64(3), "second run was cut short")
	assert.Less(t, mean1, 0.3)
	assert.GreaterOrEqual(t, mean2, 0.5)
}

type syntheticAuthor struct {
	name    string
	phrases []string
	rng     *rand.Rand
}

func newSyntheticAuthor(name string, seed int64, phrases []string) (inst *syntheticAuthor) {
	inst = &syntheticAuthor{name: name, phrases: phrases, rng: rand.New(rand.NewSource(seed))}
	return
}

func (inst *syntheticAuthor) generateComments(count int) (out []string) {
	out = make([]string, count)
	for i := range out {
		var sb strings.Builder
		n := 3 + inst.rng.Intn(4)
		for j := 0; j < n; j++ {
			sb.WriteString(inst.phrases[inst.rng.Intn(len(inst.phrases))])
			sb.WriteString(" ")
		}
		out[i] = sb.String()
	}
	return
}

func TestEndToEndNcdWithSyntheticAuthors(t *testing.T) {
	target := newSyntheticAuthor("alice", 1, []string{
		"I think the key insight here is",
		"what really matters is the underlying architecture",
		"from my experience in distributed systems",
		"the trade-off between consistency and availability",
		"this reminds me of the CAP theorem",
		"in practice you want to optimize for latency",
		"the fundamental problem is state management",
		"if you look at the research literature",
	})
	similar := newSyntheticAuthor("bob", 2, []string{
		"I believe the core challenge is",
		"what really matters is the system design",
		"from my experience in backend systems",
		"the trade-off between throughput and latency",
		"this is related to distributed consensus",
		"in practice you want to minimize overhead",
		"the fundamental issue is data consistency",
		"if you examine the published research",
	})
	different := newSyntheticAuthor("charlie", 3, []string{
		"lol this is hilarious!!",
		"omg I can't even",
		"does anyone know a good pizza place",
		"just got back from vacation it was amazing",
		"my cat did the funniest thing today",
		"who else is watching the game tonight???",
		"honestly I have no idea what's going on",
		"bruh that's wild haha",
	})

	var refBuf strings.Builder
	for _, c := range target.generateComments(200) {
		refBuf.WriteString(c)
	}
	a := newTestAnalyzer(t, refBuf.String())

	measureAuthor := func(author *syntheticAuthor) (meanNcd float64, count int64) {
		comments := author.generateComments(100)
		texts := func(yield func(string) bool) {
			for _, c := range comments {
				if !yield(c) {
					return
				}
			}
		}
		var innerErr error
		_, count, _, meanNcd, _, _, _, innerErr = a.MeasureNcdInstance(texts)
		require.NoError(t, innerErr)
		return
	}

	ncdSelf, countSelf := measureAuthor(target)
	ncdSimilar, countSimilar := measureAuthor(similar)
	ncdDifferent, countDifferent := measureAuthor(different)

	t.Logf("self=%.4f(%d) similar=%.4f(%d) different=%.4f(%d)",
		ncdSelf, countSelf, ncdSimilar, countSimilar, ncdDifferent, countDifferent)

	assert.GreaterOrEqual(t, countSelf, int64(5))
	assert.GreaterOrEqual(t, countSimilar, int64(5))
	assert.GreaterOrEqual(t, countDifferent, int64(5))
	assert.Less(t, ncdSelf, ncdDifferent)
	assert.Less(t, ncdSimilar, ncdDifferent)
}

func TestEndToEndNcdProfile(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	generate := func(phrases []string, n int) (text string) {
		var sb strings.Builder
		for i := 0; i < n; i++ {
			sb.WriteString(phrases[rng.Intn(len(phrases))])
			sb.WriteString(". ")
		}
		text = sb.String()
		return
	}

	refPhrases := []string{"functional programming enables compositional reasoning", "immutable data structures prevent whole classes of bugs", "type systems are a form of lightweight formal verification", "algebraic data types model domain concepts precisely"}
	diffPhrases := []string{"just ship it and iterate", "move fast and break things", "premature optimization is the root of all evil", "good enough is perfect"}

	a := newTestAnalyzer(t, generate(refPhrases, 500))

	makeIter := func(text string, chunkSize int) iter.Seq[string] {
		return func(yield func(string) bool) {
			for i := 0; i < len(text); i += chunkSize {
				end := min(i+chunkSize, len(text))
				if !yield(text[i:end]) {
					return
				}
			}
		}
	}

	_, _, ncdSame, err := a.MeasureNcdProfile(makeIter(generate(refPhrases, 500), 200))
	require.NoError(t, err)
	_, _, ncdDiff, err := a.MeasureNcdProfile(makeIter(generate(diffPhrases, 500), 200))
	require.NoError(t, err)

	t.Logf("NCD-profile same=%.4f diff=%.4f", ncdSame, ncdDiff)
	assert.Less(t, ncdSame, ncdDiff)
}

func TestConvergenceDetectorResetBetweenAuthors(t *testing.T) {
	refText := strings.Repeat("I think the key insight here is the underlying architecture. ", 200)
	a := newTestAnalyzer(t, refText)

	rng := rand.New(rand.NewSource(99))
	for i := 0; i < 5; i++ {
		phrases := make([]string, 8)
		for j := range phrases {
			phrases[j] = fmt.Sprintf("author%d-phrase%d-%d", i, j, rng.Int())
		}
		comments := make([]string, 50)
		for k := range comments {
			var sb strings.Builder
			for p := 0; p < 5; p++ {
				sb.WriteString(phrases[rng.Intn(len(phrases))])
				sb.WriteString(" ")
			}
			comments[k] = sb.String()
		}
		texts := func(yield func(string) bool) {
			for _, c := range comments {
				if !yield(c) {
					return
				}
			}
		}
		_, count, _, _, _, _, _, err := a.MeasureNcdInstance(texts)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, count, int64(5), "author_%d: stale convergence state", i)
	}
}
