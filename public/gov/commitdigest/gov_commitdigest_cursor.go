//go:build llm_generated_opus46

package commitdigest

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"encoding/json/v2"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

var ErrCursorHashNotFound = errors.New("cursor hash not found in repository")

const cursorsFileName = "cursors.json"

type CommitCursor struct {
	LastCommitHash string `json:"lastCommitHash"`
	LastCommitDate string `json:"lastCommitDate"`
	LastChunkIndex int32  `json:"lastChunkIndex"`
	UpdatedAt      string `json:"updatedAt"`
}

type CursorMap map[string]CommitCursor

// LoadCursors reads cursors.json from dir. Returns an empty map if dir is empty
// or the file does not exist.
func LoadCursors(dir string) (cursors CursorMap, err error) {
	cursors = make(CursorMap)
	if dir == "" {
		return
	}
	path := filepath.Join(dir, cursorsFileName)
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return
		}
		err = eb.Build().Str("file", path).Errorf("unable to read cursors file: %w", readErr)
		return
	}
	if len(data) == 0 {
		return
	}
	err = json.Unmarshal(data, &cursors)
	if err != nil {
		err = eb.Build().Str("file", path).Errorf("unable to parse cursors file: %w", err)
		return
	}
	if cursors == nil {
		cursors = make(CursorMap)
	}
	return
}

// SaveCursors writes cursors.json atomically via temp file + rename so a crashed
// write never leaves a partial file (resume state would be corrupted otherwise).
func SaveCursors(dir string, cursors CursorMap) (err error) {
	if dir == "" {
		return
	}
	err = os.MkdirAll(dir, 0o755)
	if err != nil {
		err = eb.Build().Str("dir", dir).Errorf("unable to create cursors directory: %w", err)
		return
	}
	path := filepath.Join(dir, cursorsFileName)
	tmp := path + ".tmp"

	f, createErr := os.Create(tmp)
	if createErr != nil {
		err = eb.Build().Str("file", tmp).Errorf("unable to create temp cursors file: %w", createErr)
		return
	}

	err = json.MarshalWrite(f, cursors)
	closeErr := f.Close()
	if err != nil {
		_ = os.Remove(tmp)
		err = eb.Build().Str("file", tmp).Errorf("unable to write cursors file: %w", err)
		return
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		err = eb.Build().Str("file", tmp).Errorf("unable to close cursors file: %w", closeErr)
		return
	}

	err = os.Rename(tmp, path)
	if err != nil {
		_ = os.Remove(tmp)
		err = eb.Build().Str("file", path).Errorf("unable to rename cursors file: %w", err)
		return
	}
	return
}

// ValidateCursorHash confirms the hash still resolves to a commit in repoDir.
// Returns ErrCursorHashNotFound if git rev-parse fails (history was rewritten).
func ValidateCursorHash(ctx context.Context, repoDir string, hash string) (err error) {
	if hash == "" {
		err = eh.Errorf("empty hash: %w", ErrCursorHashNotFound)
		return
	}
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", hash+"^{commit}")
	cmd.Dir = repoDir
	if runErr := cmd.Run(); runErr != nil {
		err = eb.Build().Str("hash", hash).Errorf("hash not found in repo: %w", ErrCursorHashNotFound)
		return
	}
	return
}

// NewCursorForChunk builds a cursor pointing at the newest commit in chunk.
// Commits are assumed oldest-first (as produced by git log --reverse in CollectDigest).
// Returns ok=false for empty chunks.
func NewCursorForChunk(chunkIndex int32, commits []CommitEntry) (cursor CommitCursor, ok bool) {
	if len(commits) == 0 {
		return
	}
	newest := commits[len(commits)-1]
	cursor = CommitCursor{
		LastCommitHash: newest.Hash,
		LastCommitDate: newest.Date,
		LastChunkIndex: chunkIndex,
		UpdatedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	ok = true
	return
}
