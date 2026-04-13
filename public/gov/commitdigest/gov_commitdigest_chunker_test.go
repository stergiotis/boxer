//go:build llm_generated_opus46

package commitdigest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixedCounter struct {
	tokensPerCall int64
}

func (inst *fixedCounter) CountTokens(_ string) (count int64) {
	count = inst.tokensPerCall
	return
}

func makeTestCommits(n int) (commits []CommitEntry) {
	commits = make([]CommitEntry, 0, n)
	for i := range n {
		commits = append(commits, CommitEntry{
			Hash:    "abcdef1234567890abcdef1234567890abcdef12",
			Author:  "Test Author",
			Date:    "2026-04-13 10:00:00 +0200",
			Subject: "commit " + string(rune('A'+i)),
		})
	}
	return
}

func TestChunkAll_SingleChunk(t *testing.T) {
	counter := &fixedCounter{tokensPerCall: 100}
	chunker := &DigestChunker{
		TokenBudget:    10000,
		ReservedTokens: 0,
		Counter:        counter,
	}
	digest := RepoDigest{
		RepoName: "test-repo",
		Commits:  makeTestCommits(3),
	}

	chunks, err := chunker.ChunkAll(digest, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, len(chunks))
	assert.Equal(t, 3, len(chunks[0].Commits))
	assert.Equal(t, int32(0), chunks[0].Index)
}

func TestChunkAll_MultipleChunks(t *testing.T) {
	counter := &fixedCounter{tokensPerCall: 100}
	chunker := &DigestChunker{
		TokenBudget:    250,
		ReservedTokens: 0,
		Counter:        counter,
	}
	digest := RepoDigest{
		RepoName: "test-repo",
		Commits:  makeTestCommits(5),
	}

	chunks, err := chunker.ChunkAll(digest, 0)
	require.NoError(t, err)
	assert.Greater(t, len(chunks), 1)

	// verify all commits accounted for
	total := 0
	for _, c := range chunks {
		total += len(c.Commits)
	}
	assert.Equal(t, 5, total)

	// verify sequential indices
	for i, c := range chunks {
		assert.Equal(t, int32(i), c.Index)
	}
}

func TestChunkAll_EmptyDigest(t *testing.T) {
	counter := &fixedCounter{tokensPerCall: 100}
	chunker := &DigestChunker{
		TokenBudget:    1000,
		ReservedTokens: 0,
		Counter:        counter,
	}
	digest := RepoDigest{
		RepoName: "empty-repo",
		Commits:  nil,
	}

	chunks, err := chunker.ChunkAll(digest, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, len(chunks))
}

func TestChunkNext_OversizedCommit(t *testing.T) {
	counter := &fixedCounter{tokensPerCall: 500}
	chunker := &DigestChunker{
		TokenBudget:    300,
		ReservedTokens: 0,
		Counter:        counter,
	}
	commits := makeTestCommits(2)

	// first chunk: oversized but still consumed (safety: at least 1)
	chunk, consumed := chunker.ChunkNext(commits, 0, 0, "test-repo")
	assert.Equal(t, 1, consumed)
	assert.Equal(t, 1, len(chunk.Commits))

	// second chunk: same
	chunk2, consumed2 := chunker.ChunkNext(commits[consumed:], 1, 0, "test-repo")
	assert.Equal(t, 1, consumed2)
	assert.Equal(t, 1, len(chunk2.Commits))
}

func TestChunkNext_OverheadReducesAvailable(t *testing.T) {
	counter := &fixedCounter{tokensPerCall: 100}
	chunker := &DigestChunker{
		TokenBudget:    500,
		ReservedTokens: 0,
		Counter:        counter,
	}
	commits := makeTestCommits(5)

	// no overhead: should fit more commits
	chunkNoOverhead, consumedNo := chunker.ChunkNext(commits, 0, 0, "test-repo")

	// with overhead: should fit fewer
	chunkWithOverhead, consumedWith := chunker.ChunkNext(commits, 0, 200, "test-repo")

	assert.GreaterOrEqual(t, consumedNo, consumedWith)
	assert.GreaterOrEqual(t, len(chunkNoOverhead.Commits), len(chunkWithOverhead.Commits))
}

func TestChunkNext_GrowingOverhead(t *testing.T) {
	counter := &fixedCounter{tokensPerCall: 100}
	chunker := &DigestChunker{
		TokenBudget:    400,
		ReservedTokens: 0,
		Counter:        counter,
	}
	commits := makeTestCommits(10)

	// simulate growing window: overhead increases each iteration
	var totalConsumed int
	overheads := []int64{0, 100, 200}
	for _, overhead := range overheads {
		remaining := commits[totalConsumed:]
		if len(remaining) == 0 {
			break
		}
		_, consumed := chunker.ChunkNext(remaining, 0, overhead, "test-repo")
		totalConsumed += consumed
	}
	assert.Greater(t, totalConsumed, 0)
	assert.LessOrEqual(t, totalConsumed, len(commits))
}
