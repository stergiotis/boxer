//go:build llm_generated_opus46

package commitdigest

import (
	"github.com/rs/zerolog/log"
)

type Chunk struct {
	Index      int32
	Commits    []CommitEntry
	TokenCount int64
	Metrics    DigestMetrics
}

type DigestChunker struct {
	TokenBudget    int64
	ReservedTokens int64
	Counter        TokenCounterI
}

func (inst *DigestChunker) tokenBudget() (b int64) {
	b = inst.TokenBudget
	if b <= 0 {
		b = 4096
	}
	return
}

func (inst *DigestChunker) reservedTokens() (r int64) {
	r = inst.ReservedTokens
	if r < 0 {
		r = 512
	}
	return
}

// ChunkAll splits all commits into chunks using a fixed overhead.
// Use ChunkNext for iterative chunking where overhead changes between chunks.
func (inst *DigestChunker) ChunkAll(digest RepoDigest, overheadTokens int64) (chunks []Chunk, err error) {
	remaining := digest.Commits
	var idx int32
	chunks = make([]Chunk, 0, 4)
	for len(remaining) > 0 {
		var chunk Chunk
		var consumed int
		chunk, consumed = inst.chunkNext(remaining, idx, overheadTokens, digest.RepoName)
		chunks = append(chunks, chunk)
		remaining = remaining[consumed:]
		idx++
	}
	return
}

// ChunkNext returns the next chunk from the front of commits, given the current overhead
// (system prompt + window context + repo header tokens). Returns the chunk and how many
// commits were consumed.
func (inst *DigestChunker) ChunkNext(commits []CommitEntry, index int32, overheadTokens int64, repoName string) (chunk Chunk, consumed int) {
	chunk, consumed = inst.chunkNext(commits, index, overheadTokens, repoName)
	return
}

func (inst *DigestChunker) chunkNext(commits []CommitEntry, index int32, overheadTokens int64, repoName string) (chunk Chunk, consumed int) {
	available := inst.tokenBudget() - inst.reservedTokens() - overheadTokens
	if available <= 0 {
		available = 1
		log.Warn().
			Int64("budget", inst.tokenBudget()).
			Int64("reserved", inst.reservedTokens()).
			Int64("overhead", overheadTokens).
			Msg("overhead exceeds budget, forcing minimum chunk size")
	}

	var currentCommits []CommitEntry
	var currentTokens int64

	for i, c := range commits {
		rendered := RenderCommitEntry(c)
		tokens := inst.Counter.CountTokens(rendered)

		if tokens > available && i == 0 {
			log.Warn().
				Str("hash", c.Hash[:min(12, len(c.Hash))]).
				Int64("commitTokens", tokens).
				Int64("available", available).
				Msg("single commit exceeds chunk budget")
		}

		if currentTokens+tokens > available && len(currentCommits) > 0 {
			break
		}

		currentCommits = append(currentCommits, c)
		currentTokens += tokens
		consumed = i + 1
	}

	// safety: always consume at least one commit to avoid infinite loop
	if consumed == 0 && len(commits) > 0 {
		currentCommits = []CommitEntry{commits[0]}
		currentTokens = inst.Counter.CountTokens(RenderCommitEntry(commits[0]))
		consumed = 1
	}

	chunk = Chunk{
		Index:      index,
		Commits:    currentCommits,
		TokenCount: currentTokens,
	}
	return
}
