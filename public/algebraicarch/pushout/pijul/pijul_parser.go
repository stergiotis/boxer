//go:build llm_generated_opus47

package pijul

import (
	"bufio"
	"encoding/json/v2"
	"fmt"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// parseLogJSON parses the JSON document emitted by `pijul log
// --output-format json`. ParsedTime is filled in for each entry from
// the RFC3339Nano Timestamp; entries whose Timestamp is unparseable
// keep the zero time and will sort as "oldest" under
// [applyCreditToLines]'s before-comparison.
func parseLogJSON(logOut []byte) (entries []LogEntry, err error) {
	if len(logOut) == 0 {
		return
	}
	uerr := json.Unmarshal(logOut, &entries)
	if uerr != nil {
		err = eh.Errorf("parse pijul log JSON: %w", uerr)
		return
	}
	for i := range entries {
		if len(entries[i].Authors) == 0 || entries[i].Authors[0] == "" {
			entries[i].Authors = []string{"System"}
		}
		t, terr := time.Parse(time.RFC3339Nano, entries[i].Timestamp)
		if terr == nil {
			entries[i].ParsedTime = t
		}
	}
	return
}

// FormatLogEntry renders one log entry in the box-style block the demo
// shows in the Pijul-history scroll view.
func FormatLogEntry(entry LogEntry) (s string) {
	author := "System"
	if len(entry.Authors) > 0 {
		author = entry.Authors[0]
	}
	s = fmt.Sprintf("Change %s\nAuthor: %s\nDate: %s\n\n    %s",
		entry.Hash, author,
		entry.ParsedTime.Local().Format("2006-01-02 15:04:05 (MST)"),
		entry.Message)
	return
}

// applyCreditToLines parses the block-based output of `pijul credit`
// and attaches per-line provenance (introducing patch hash + author) to
// each KVLine. When a line is introduced by multiple patches (a
// "context node" on a graph edge), the OLDEST patch wins by graph-age
// resolution — that yields stable authorship across rebases and
// channel mixing.
//
// The implementation builds a `short→entry` index up front rather than
// scanning the entire log per line; Pijul's credit output prints
// exactly 12 hash characters today but the index tolerates any short
// length 8…full.
func applyCreditToLines(creditOut string, lines []KVLine, entries []LogEntry) (out []KVLine) {
	shortToEntry := buildShortHashIndex(entries)

	contentToEntry := make(map[string]LogEntry)
	var currentHashes []string

	scanner := bufio.NewScanner(strings.NewReader(creditOut))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "> ") {
			content := strings.TrimSpace(strings.TrimPrefix(line, "> "))
			oldest, found := oldestEntry(currentHashes, shortToEntry)
			if found {
				contentToEntry[content] = oldest
			}
			continue
		}
		// Header block, e.g. "EYPWGEPXCHFD, JYS6SYSP25AS"
		currentHashes = splitAndTrim(line, ",")
	}

	out = lines
	for i := range out {
		if out[i].Conflict != nil {
			continue
		}
		key := fmt.Sprintf(`%s "%s"`, out[i].Path, out[i].Value)
		entry, ok := contentToEntry[key]
		if !ok {
			continue
		}
		hash := entry.Hash
		if len(hash) > 8 {
			hash = hash[:8]
		}
		out[i].CreditHash = hash
		out[i].CreditAuthor = entry.Authors[0]
	}
	return
}

func buildShortHashIndex(entries []LogEntry) (idx map[string]LogEntry) {
	idx = make(map[string]LogEntry, len(entries)*4)
	for _, e := range entries {
		full := e.Hash
		// Index every prefix from 8 chars up to the full hash.
		// This costs O(L*N) keys but L is fixed (44 for SHA-256
		// b64) and N is the number of patches, both small for
		// the demo.
		for n := 8; n <= len(full); n++ {
			idx[full[:n]] = e
		}
	}
	return
}

func oldestEntry(shortHashes []string, idx map[string]LogEntry) (oldest LogEntry, found bool) {
	for _, sh := range shortHashes {
		sh = strings.TrimSpace(sh)
		if sh == "" {
			continue
		}
		entry, ok := idx[sh]
		if !ok {
			continue
		}
		if !found || entry.ParsedTime.Before(oldest.ParsedTime) {
			oldest = entry
			found = true
		}
	}
	return
}

func splitAndTrim(s string, sep string) (out []string) {
	parts := strings.Split(s, sep)
	out = make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return
}

// ParsePijulFile parses a flat-KV file possibly containing Pijul
// conflict markers. Each non-conflict line is `<path> "<value>"`;
// conflict blocks have the form
//
//	>>>>>>> <label>
//	<path> "<value-A>"
//	=======
//	<path> "<value-B>"
//	<<<<<<< <label>
//
// where <label> is Pijul's internal side number (typically "1" / "2"),
// not a hash. The parser preserves these labels in [ConflictData] so
// that [DemoStore.SaveStateToFile] can re-emit byte-identical markers
// when restoring an unresolved conflict to disk.
//
// Limitations: the parser handles only two-way conflicts spanning a
// single key. Three-way conflicts and conflicts that span multiple
// keys are not modelled by the demo.
func ParsePijulFile(content string) (lines []KVLine, hasConflict bool) {
	scanner := bufio.NewScanner(strings.NewReader(content))

	var (
		inConflict   bool
		cd           *ConflictData
		conflictPath string
	)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, ">>>>>>>"):
			inConflict = true
			hasConflict = true
			cd = &ConflictData{AliceLabel: strings.TrimSpace(strings.TrimLeft(line, ">"))}
		case inConflict && strings.HasPrefix(line, "======="):
			// separator between sides
		case inConflict && strings.HasPrefix(line, "<<<<<<<"):
			cd.BobLabel = strings.TrimSpace(strings.TrimLeft(line, "<"))
			lines = append(lines, KVLine{Path: conflictPath, Conflict: cd})
			inConflict = false
			cd = nil
			conflictPath = ""
		case inConflict:
			path, val, ok := splitKVLine(line)
			if !ok {
				continue
			}
			if conflictPath == "" {
				conflictPath = path
			}
			if cd.AliceValue == "" {
				cd.AliceValue = val
			} else if cd.BobValue == "" {
				cd.BobValue = val
			}
		default:
			path, val, ok := splitKVLine(line)
			if ok {
				lines = append(lines, KVLine{Path: path, Value: val})
			}
		}
	}
	return
}

func splitKVLine(line string) (path string, value string, ok bool) {
	parts := strings.SplitN(line, " ", 2)
	if len(parts) < 2 {
		return
	}
	path = parts[0]
	value = strings.Trim(parts[1], `"`)
	ok = true
	return
}
