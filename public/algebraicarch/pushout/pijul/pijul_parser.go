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

// pijulLogEntry maps the JSON record emitted by `pijul log
// --output-format json`. It is an internal staging type for the text
// backend; the demo sees only [PatchMetadata].
type pijulLogEntry struct {
	Hash       string    `json:"hash"`
	Authors    []string  `json:"authors"`
	Timestamp  string    `json:"timestamp"`
	Message    string    `json:"message"`
	ParsedTime time.Time `json:"-"`
}

// parseLogJSON parses the JSON document emitted by `pijul log
// --output-format json`. ParsedTime is filled in for each entry from
// the RFC3339Nano Timestamp; entries whose Timestamp is unparseable
// keep the zero time and will sort as "oldest" under
// [applyCreditToCells]'s before-comparison.
func parseLogJSON(logOut []byte) (entries []pijulLogEntry, err error) {
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

// toPatchMetadata converts a parsed log entry into the public
// [PatchMetadata] shape. The conversion is lossy by design: the demo
// does not need the raw textual timestamp.
func (e pijulLogEntry) toPatchMetadata() (m PatchMetadata) {
	m = PatchMetadata{
		ID:        PatchID{Hex: e.Hash},
		Authors:   append([]string(nil), e.Authors...),
		Timestamp: e.ParsedTime,
		Message:   e.Message,
	}
	return
}

// applyCreditToCells parses the block-based output of `pijul credit`
// and attaches per-cell provenance to each [KVLine]. When a cell is
// introduced by multiple patches (a "context node" on a graph edge),
// the OLDEST patch wins by graph-age resolution — that yields stable
// authorship across rebases and channel mixing.
func applyCreditToCells(creditOut string, cells []KVLine, entries []pijulLogEntry) (out []KVLine) {
	shortToEntry := buildShortHashIndex(entries)

	contentToEntry := make(map[string]pijulLogEntry)
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

	out = cells
	for i := range out {
		if out[i].Conflict != nil {
			continue
		}
		key := fmt.Sprintf(`%s "%s"`, out[i].Path, out[i].Value)
		entry, ok := contentToEntry[key]
		if !ok {
			continue
		}
		meta := entry.toPatchMetadata()
		out[i].Credit = &meta
	}
	return
}

func buildShortHashIndex(entries []pijulLogEntry) (idx map[string]pijulLogEntry) {
	idx = make(map[string]pijulLogEntry, len(entries)*4)
	for _, e := range entries {
		full := e.Hash
		// Index every prefix from 8 chars up to the full hash.
		// Cost is O(L*N) keys; L is fixed (44 for SHA-256 b64)
		// and N is the number of patches, both small for the demo.
		for n := 8; n <= len(full); n++ {
			idx[full[:n]] = e
		}
	}
	return
}

func oldestEntry(shortHashes []string, idx map[string]pijulLogEntry) (oldest pijulLogEntry, found bool) {
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

// parseRecordText parses pijul's textual working-copy format into the
// package's domain [KVLine] slice. Conflict blocks become cells with a
// non-nil Conflict; clean rows become cells with Value populated.
//
// Limitations: handles only two-way conflicts spanning a single key,
// and treats values as untyped trimmed-quote strings (so a value
// containing an embedded `"` round-trips poorly). Both are
// text-format issues and will not exist in the native backend.
func parseRecordText(content string) (cells []KVLine, hasConflict bool) {
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
			cd = &ConflictData{}
		case inConflict && strings.HasPrefix(line, "======="):
			// separator between sides
		case inConflict && strings.HasPrefix(line, "<<<<<<<"):
			cells = append(cells, KVLine{Path: conflictPath, Conflict: cd})
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
			switch {
			case cd.AliceValue == "":
				cd.AliceValue = val
			case cd.BobValue == "":
				cd.BobValue = val
			default:
				cd.OtherValues = append(cd.OtherValues, val)
			}
		default:
			path, val, ok := splitKVLine(line)
			if ok {
				cells = append(cells, KVLine{Path: path, Value: val})
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

// serializeRecordText is the inverse of [parseRecordText]: render the
// in-memory cell slice back to pijul's textual flat-KV format. The
// trailing newline is structurally important — without it pijul's
// patch graph treats the EOF context node as overlapping and may
// promote unrelated edits into spurious conflicts.
//
// Conflict blocks use fixed side labels "1" and "2"; pijul does not
// require any specific values in those slots, only that the block is
// well-formed.
func serializeRecordText(cells []KVLine) (raw []byte) {
	out := make([]string, 0, len(cells)+4)
	for _, c := range cells {
		if c.Conflict != nil {
			values := c.Conflict.AllValues()
			out = append(out, fmt.Sprintf(">>>>>>> %d", 1))
			for i, v := range values {
				if i > 0 {
					out = append(out, "=======")
				}
				out = append(out, fmt.Sprintf(`%s "%s"`, c.Path, v))
			}
			out = append(out, fmt.Sprintf("<<<<<<< %d", len(values)))
		} else {
			out = append(out, fmt.Sprintf(`%s "%s"`, c.Path, c.Value))
		}
	}
	raw = []byte(strings.Join(out, "\n") + "\n")
	return
}
