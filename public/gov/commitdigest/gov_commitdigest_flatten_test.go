//go:build llm_generated_opus46

package commitdigest

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseNumstatLine_Normal(t *testing.T) {
	entry, ok := parseNumstatLine("10\t5\tsrc/foo.go")
	assert.True(t, ok)
	assert.Equal(t, "src/foo.go", entry.Path)
	assert.Equal(t, int32(10), entry.Adds)
	assert.Equal(t, int32(5), entry.Dels)
}

func TestParseNumstatLine_Binary(t *testing.T) {
	entry, ok := parseNumstatLine("-\t-\tbinary/blob.bin")
	assert.True(t, ok)
	assert.Equal(t, "binary/blob.bin", entry.Path)
	assert.Equal(t, int32(0), entry.Adds)
	assert.Equal(t, int32(0), entry.Dels)
}

func TestParseNumstatLine_Rename(t *testing.T) {
	entry, ok := parseNumstatLine("5\t0\t{old => new}.txt")
	assert.True(t, ok)
	assert.Equal(t, "{old => new}.txt", entry.Path)
	assert.Equal(t, int32(5), entry.Adds)
}

func TestParseNumstatLine_Malformed(t *testing.T) {
	_, ok := parseNumstatLine("garbage")
	assert.False(t, ok)
	_, ok = parseNumstatLine("10\t5")
	assert.False(t, ok, "missing path column should not parse")
	_, ok = parseNumstatLine("notnum\t5\tpath")
	assert.False(t, ok)
	_, ok = parseNumstatLine("10\t5\t")
	assert.False(t, ok, "empty path should not parse")
}

func TestExtractSubjectType_ConventionalCommits(t *testing.T) {
	cases := map[string]string{
		"feat: add widget":            "feat",
		"feat(scope): add widget":     "feat",
		"feat!: breaking change":      "feat",
		"feat(scope)!: breaking":      "feat",
		"FIX: case insensitive":       "fix",
		"refactor: rename":            "refactor",
		"chore(deps): bump":           "chore",
		"docs: update README":         "docs",
		"test: add coverage":          "test",
		"perf: faster hashmap":        "perf",
		"build: new target":           "build",
		"ci: update workflow":         "ci",
	}
	for subject, expected := range cases {
		got := ExtractSubjectType(subject)
		assert.Equal(t, expected, got, "subject: %q", subject)
	}
}

func TestExtractSubjectType_SpecialPrefixes(t *testing.T) {
	assert.Equal(t, "revert", ExtractSubjectType("Revert: earlier commit"))
	assert.Equal(t, "revert", ExtractSubjectType(`Revert "feat: add thing"`))
	assert.Equal(t, "fixup", ExtractSubjectType("fixup! refactor parser"))
	assert.Equal(t, "squash", ExtractSubjectType("squash! tighten types"))
}

func TestExtractSubjectType_FixupBeatsConventional(t *testing.T) {
	// "fixup!" prefix wins over embedded "feat:" language downstream.
	assert.Equal(t, "fixup", ExtractSubjectType("fixup! feat: add widget"))
}

func TestExtractSubjectType_WipFallback(t *testing.T) {
	// No Conventional-Commit prefix but WIP as a word.
	assert.Equal(t, "wip", ExtractSubjectType("wip exploring the new API"))
	assert.Equal(t, "other", ExtractSubjectType("random prose without a type"))
}

func TestFlattenRepoChunks_SentinelForEmptyStats(t *testing.T) {
	repos := []RepoChunks{{
		RepoName: "demo",
		Chunks: []ChunkResult{{
			Index: 0,
			Commits: []CommitEntry{{
				Hash:    "aaaa1111",
				Author:  "Alice <a@e.com>",
				Date:    "2026-04-10 10:00:00 +0200",
				Subject: "chore: empty merge",
				// no StatEntries
			}},
		}},
	}}
	rows := FlattenRepoChunks(repos)
	assert.Len(t, rows, 1, "commit with no changes should emit one sentinel row")
	assert.Equal(t, "", rows[0].Path, "sentinel row uses empty path")
	assert.Equal(t, int32(0), rows[0].Adds)
	assert.Equal(t, "chore", rows[0].SubjectType)
}

func TestFlattenRepoChunks_OneRowPerChange(t *testing.T) {
	repos := []RepoChunks{{
		RepoName: "demo",
		Chunks: []ChunkResult{{
			Index: 7,
			Commits: []CommitEntry{{
				Hash:    "bbbb2222",
				Author:  "Alice <a@e.com>",
				Date:    "2026-04-11",
				Subject: "feat: multi-file",
				StatEntries: []StatEntry{
					{Path: "src/one.go", Adds: 10, Dels: 2},
					{Path: "src/two.go", Adds: 3, Dels: 0},
				},
			}},
		}},
	}}
	rows := FlattenRepoChunks(repos)
	assert.Len(t, rows, 2)
	assert.Equal(t, "src/one.go", rows[0].Path)
	assert.Equal(t, int32(10), rows[0].Adds)
	assert.Equal(t, int32(7), rows[0].ChunkIndex, "chunk index propagates")
	assert.Equal(t, "feat", rows[1].SubjectType)
	assert.Equal(t, "a@e.com", rows[1].CommitAuthorEmail)
}

func TestWriteFlattenedJSONEachRow_StreamsOneLinePerRow(t *testing.T) {
	repos := []RepoChunks{{
		RepoName: "demo",
		Chunks: []ChunkResult{{
			Index: 0,
			Commits: []CommitEntry{
				{
					Hash: "aaaa1111", Author: "Alice <a@e.com>", Date: "d", Subject: "feat: a",
					StatEntries: []StatEntry{{Path: "a", Adds: 1, Dels: 0}},
				},
				{
					Hash: "bbbb2222", Author: "Alice <a@e.com>", Date: "d", Subject: "wip: b",
					// sentinel row
				},
			},
		}},
	}}
	var buf bytes.Buffer
	err := WriteFlattenedJSONEachRow(&buf, repos)
	assert.NoError(t, err)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	assert.Len(t, lines, 2, "one line per row (1 change + 1 sentinel)")
	for _, line := range lines {
		assert.True(t, strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}"), "each line is a JSON object: %q", line)
	}
}

func TestParseThreadRegistry_ValidJSON(t *testing.T) {
	raw := `[{"id":"t1","title":"Thread One","span":{"start":"2026-01-01","end":"2026-02-01"},"summary":"s","complexityDirection":"shed","pathPrefixes":["src/go"],"anchorCommits":["abcd1234"]}]`
	threads, err := parseThreadRegistry(raw)
	assert.NoError(t, err)
	assert.Len(t, threads, 1)
	assert.Equal(t, "t1", threads[0].ID)
	assert.Equal(t, "2026-01-01", threads[0].Span.Start)
}

func TestParseThreadRegistry_StripsCodeFences(t *testing.T) {
	raw := "```json\n[{\"id\":\"t1\",\"title\":\"T\",\"span\":{\"start\":\"2026-01-01\",\"end\":\"2026-02-01\"},\"summary\":\"s\",\"complexityDirection\":\"mixed\",\"pathPrefixes\":[],\"anchorCommits\":[]}]\n```"
	threads, err := parseThreadRegistry(raw)
	assert.NoError(t, err)
	assert.Len(t, threads, 1)
}

func TestParseThreadRegistry_EmptyArrayIsError(t *testing.T) {
	_, err := parseThreadRegistry("[]")
	assert.Error(t, err, "registry with no threads violates the prompt rule")
}

func TestStripCodeFences(t *testing.T) {
	assert.Equal(t, "x", stripCodeFences("x"))
	assert.Equal(t, "x", stripCodeFences("```\nx\n```"))
	assert.Equal(t, "x", stripCodeFences("```json\nx\n```"))
	assert.Equal(t, "line1\nline2", stripCodeFences("```\nline1\nline2\n```"))
}

func TestRenderThreadRegistry(t *testing.T) {
	threads := []Thread{
		{
			ID: "stopa-migration", Title: "stopa → boxer",
			Span: ThreadSpan{Start: "2025-11-15", End: "2025-12-05"},
			Summary: "Moved registry into boxer.",
			ComplexityDirection: "shed",
			PathPrefixes: []string{"leeway/stopa", "public/thestack"},
			AnchorCommits: []string{"8bf828bf", "5cc57878"},
		},
	}
	out := RenderThreadRegistry(threads)
	assert.Contains(t, out, "**stopa-migration**")
	assert.Contains(t, out, "2025-11-15 – 2025-12-05")
	assert.Contains(t, out, "shed")
	assert.Contains(t, out, "leeway/stopa, public/thestack")
	assert.Contains(t, out, "Anchors: 8bf828bf, 5cc57878")
}

func TestRenderThreadRegistry_Empty(t *testing.T) {
	assert.Equal(t, "", RenderThreadRegistry(nil))
	assert.Equal(t, "", RenderThreadRegistry([]Thread{}))
}
