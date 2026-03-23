//go:build llm_generated_opus46

package testdata

import (
	"embed"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

//go:embed corpus/*.sql
var corpusFS embed.FS

// LoadCorpus reads all .sql files from the embedded corpus directory,
// sorted by filename. Each file contains one SQL statement.
func LoadCorpus() (entries []CorpusEntry, err error) {
	dirEntries, err := corpusFS.ReadDir("corpus")
	if err != nil {
		err = eh.Errorf("unable to read corpus directory: %w", err)
		return
	}

	sort.Slice(dirEntries, func(i, j int) bool {
		return dirEntries[i].Name() < dirEntries[j].Name()
	})

	entries = make([]CorpusEntry, 0, len(dirEntries))
	for _, de := range dirEntries {
		if de.IsDir() {
			continue
		}
		if !strings.HasSuffix(de.Name(), ".sql") {
			continue
		}

		var data []byte
		data, err = corpusFS.ReadFile("corpus/" + de.Name())
		if err != nil {
			err = eh.Errorf("unable to read corpus file %s: %w", de.Name(), err)
			return
		}

		sql := strings.TrimSpace(string(data))
		if sql == "" {
			continue
		}

		entries = append(entries, CorpusEntry{
			Name: strings.TrimSuffix(de.Name(), ".sql"),
			SQL:  sql,
		})
	}
	return
}

// CorpusEntry represents a single SQL test case from the corpus.
type CorpusEntry struct {
	Name string
	SQL  string
}
