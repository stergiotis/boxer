package commitdigest

import (
	"bufio"
	"io"
	"regexp"
	"strings"

	"encoding/json/v2"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// FlatCommitChange is one row in the JSONEachRow facts table consumed by
// trend-mining SQL. One row per file-change per commit; commits that touched
// no files (empty merges, typo-only commits) emit a single row with Path==""
// as a sentinel so commit-level aggregations (countDistinct(commitHash)) still
// see them. Trend-mining queries that want file-level joins should filter
// `path <> ''`.
type FlatCommitChange struct {
	RepoName          string `json:"repoName"`
	ChunkIndex        int32  `json:"chunkIndex"`
	CommitHash        string `json:"commitHash"`
	CommitShortHash   string `json:"commitShortHash"`
	CommitDate        string `json:"commitDate"`
	CommitAuthor      string `json:"commitAuthor"`
	CommitAuthorEmail string `json:"commitAuthorEmail"`
	CommitSubject     string `json:"commitSubject"`
	SubjectType       string `json:"subjectType"`
	Path              string `json:"path"`
	Adds              int32  `json:"adds"`
	Dels              int32  `json:"dels"`
}

// FlatCommitChangeStructure is the ClickHouse --structure argument matching
// the JSON tags on FlatCommitChange. Kept here so mine-trends stays in sync
// with the struct.
const FlatCommitChangeStructure = "repoName String, chunkIndex Int32, " +
	"commitHash String, commitShortHash String, commitDate String, " +
	"commitAuthor String, commitAuthorEmail String, commitSubject String, " +
	"subjectType String, path String, adds Int32, dels Int32"

// conventionalCommitRe matches a Conventional Commits subject prefix:
// `<type>`, `<type>(<scope>)`, with an optional `!` breaking marker, followed
// by `:`. The captured group 1 is the lowercase type.
var conventionalCommitRe = regexp.MustCompile(`^([a-zA-Z]+)(?:\([^)]*\))?!?:`)

// ExtractSubjectType returns a normalized kind for a commit subject. Fixup /
// squash / revert prefixes and WIP-as-word take precedence over Conventional
// Commits `type:` extraction because they are stronger signals of intent.
// Returns "other" when nothing matches.
func ExtractSubjectType(subject string) (kind string) {
	s := strings.TrimSpace(subject)
	switch {
	case fixupSubjectRe.MatchString(s):
		kind = "fixup"
		return
	case squashSubjectRe.MatchString(s):
		kind = "squash"
		return
	case revertSubjectRe.MatchString(s):
		kind = "revert"
		return
	}
	if m := conventionalCommitRe.FindStringSubmatch(s); m != nil {
		kind = strings.ToLower(m[1])
		return
	}
	if wipSubjectRe.MatchString(s) {
		kind = "wip"
		return
	}
	kind = "other"
	return
}

// FlattenRepoChunks materialises all file-changes across all chunks as a flat
// slice. Convenient for tests; production code should prefer the streaming
// WriteFlattenedJSONEachRow.
func FlattenRepoChunks(repos []RepoChunks) (rows []FlatCommitChange) {
	rows = make([]FlatCommitChange, 0, 256)
	for _, r := range repos {
		for _, chunk := range r.Chunks {
			for _, c := range chunk.Commits {
				base := baseRow(r.RepoName, chunk.Index, c)
				if len(c.StatEntries) == 0 {
					rows = append(rows, base)
					continue
				}
				for _, se := range c.StatEntries {
					row := base
					row.Path = se.Path
					row.Adds = se.Adds
					row.Dels = se.Dels
					rows = append(rows, row)
				}
			}
		}
	}
	return
}

// WriteFlattenedJSONEachRow streams one JSON object per line to w. Preferred
// for large inputs — avoids materialising the full flat slice in memory.
func WriteFlattenedJSONEachRow(w io.Writer, repos []RepoChunks) (err error) {
	bw := bufio.NewWriter(w)
	for _, r := range repos {
		for _, chunk := range r.Chunks {
			for _, c := range chunk.Commits {
				base := baseRow(r.RepoName, chunk.Index, c)
				if len(c.StatEntries) == 0 {
					err = writeJSONLine(bw, base)
					if err != nil {
						return
					}
					continue
				}
				for _, se := range c.StatEntries {
					row := base
					row.Path = se.Path
					row.Adds = se.Adds
					row.Dels = se.Dels
					err = writeJSONLine(bw, row)
					if err != nil {
						return
					}
				}
			}
		}
	}
	err = bw.Flush()
	if err != nil {
		err = eh.Errorf("unable to flush flattened output: %w", err)
	}
	return
}

func baseRow(repoName string, chunkIndex int32, c CommitEntry) (row FlatCommitChange) {
	row = FlatCommitChange{
		RepoName:          repoName,
		ChunkIndex:        chunkIndex,
		CommitHash:        c.Hash,
		CommitShortHash:   shortHash(c.Hash),
		CommitDate:        c.Date,
		CommitAuthor:      c.Author,
		CommitAuthorEmail: extractCommitEmail(c.Author),
		CommitSubject:     c.Subject,
		SubjectType:       ExtractSubjectType(c.Subject),
	}
	return
}

func writeJSONLine(w io.Writer, row FlatCommitChange) (err error) {
	err = json.MarshalWrite(w, row)
	if err != nil {
		err = eh.Errorf("unable to marshal flat row: %w", err)
		return
	}
	_, err = w.Write([]byte{'\n'})
	if err != nil {
		err = eh.Errorf("unable to write newline: %w", err)
	}
	return
}
