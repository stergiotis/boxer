//go:build llm_generated_opus47

package pijul

import (
	"bufio"
	"encoding/json/v2"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// LogEntry maps the JSON record emitted by `pijul log
// --output-format json`. It is an internal staging type for the text
// backend; the demo sees only [PatchMetadata].
type LogEntry struct {
	Hash       string    `json:"hash"`
	Authors    []string  `json:"authors"`
	Timestamp  string    `json:"timestamp"`
	Message    string    `json:"message"`
	ParsedTime time.Time `json:"-"`
}

// ParseLogJSON parses the JSON document emitted by `pijul log
// --output-format json`. ParsedTime is filled in for each entry from
// the RFC3339Nano Timestamp; entries whose Timestamp is unparseable
// keep the zero time and will sort as "oldest" under
// [ApplyCreditToCells]'s before-comparison.
func ParseLogJSON(logOut []byte) (entries []LogEntry, err error) {
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
func (e LogEntry) toPatchMetadata() (m PatchMetadata) {
	m = PatchMetadata{
		ID:        PatchID{Hex: e.Hash},
		Authors:   append([]string(nil), e.Authors...),
		Timestamp: e.ParsedTime,
		Message:   e.Message,
	}
	return
}

// ApplyCreditToCells parses the block-based output of `pijul credit`
// and attaches per-cell provenance to each [KVLine]. When a cell is
// introduced by multiple patches (a "context node" on a graph edge),
// the OLDEST patch wins by graph-age resolution — that yields stable
// authorship across rebases and channel mixing.
//
// A scanner error (e.g. a line beyond the buffer cap) is returned
// instead of silently truncating the credit data.
func ApplyCreditToCells(creditOut string, cells []KVLine, entries []LogEntry) (out []KVLine, err error) {
	shortToEntry := buildShortHashIndex(entries)

	contentToEntry := make(map[string]LogEntry)
	var currentHashes []string

	scanner := bufio.NewScanner(strings.NewReader(creditOut))
	scanner.Buffer(make([]byte, 64*1024), maxScanLineBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if content, isContent := strings.CutPrefix(line, "> "); isContent {
			content = strings.TrimSpace(content)
			oldest, found := oldestEntry(currentHashes, shortToEntry)
			if found {
				contentToEntry[content] = oldest
			}
			continue
		}
		// Header block, e.g. "EYPWGEPXCHFD, JYS6SYSP25AS"
		currentHashes = splitAndTrim(line, ",")
	}
	if serr := scanner.Err(); serr != nil {
		err = eh.Errorf("scan pijul credit output: %w", serr)
		return
	}

	out = cells
	for i := range out {
		if out[i].Conflict != nil {
			continue
		}
		// The credit output echoes the tracked file's lines, which carry
		// strconv.Quote'd values — the lookup key must match that form.
		key := out[i].Path + " " + strconv.Quote(out[i].Value)
		entry, ok := contentToEntry[key]
		if !ok {
			continue
		}
		meta := entry.toPatchMetadata()
		out[i].Credit = &meta
	}
	return
}

func buildShortHashIndex(entries []LogEntry) (idx map[string]LogEntry) {
	idx = make(map[string]LogEntry, len(entries)*4)
	for _, e := range entries {
		full := e.Hash
		// Index every prefix from 8 chars up to the full hash.
		// Cost is O(L*N) keys; L is the fixed hash-string length
		// (pijul emits 53-char base32) and N the number of patches,
		// both small for the demo.
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

// maxScanLineBytes caps a single scanned line at 1 MiB. The default
// bufio.Scanner limit is 64 KiB and an over-long line used to stop the
// scan silently — the parse returned a truncated cell list with no
// error, and the next record would then delete the unseen cells.
const maxScanLineBytes = 1 << 20

// ParseRecordText parses pijul's textual working-copy format into the
// package's domain [KVLine] slice. Conflict blocks become cells with a
// non-nil Conflict; clean rows become cells with Value populated.
//
// A conflict block left open at EOF (`>>>>>>>` without `<<<<<<<`) is
// flushed as a conflict cell rather than dropped. A scanner error is
// returned instead of a silently truncated cell list.
//
// Limitation: conflict blocks are assumed to span a single key. Values
// are strconv.Quote'd literals (see [splitKVLine]) and round-trip
// byte-exactly.
func ParseRecordText(content string) (cells []KVLine, hasConflict bool, err error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 64*1024), maxScanLineBytes)

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
	if serr := scanner.Err(); serr != nil {
		err = eh.Errorf("scan record text: %w", serr)
		return
	}
	if inConflict && cd != nil {
		// Unterminated conflict block at EOF: keep what was read.
		cells = append(cells, KVLine{Path: conflictPath, Conflict: cd})
	}
	return
}

// splitKVLine parses one `<path> <quoted-value>` cell line. The value is
// a strconv.Quote'd string literal — the exact inverse of
// [formatCellLine] / [SerializeRecordText] — so quotes, backslashes, and
// escaped newlines in values round-trip byte-exactly. A line whose value
// part is not a single valid quoted literal returns ok=false (the
// earlier strings.Trim(`"`) approach silently mangled values with
// leading/trailing quotes instead).
func splitKVLine(line string) (path string, value string, ok bool) {
	parts := strings.SplitN(line, " ", 2)
	if len(parts) < 2 {
		return
	}
	v, uerr := strconv.Unquote(parts[1])
	if uerr != nil {
		return
	}
	path = parts[0]
	value = v
	ok = true
	return
}

// validateCellPaths rejects cell paths that cannot survive the
// `<path> <quoted-value>` line format: the path must be non-empty and
// free of spaces (the separator), quotes, and newlines. Values are
// unrestricted — quoting handles them.
func validateCellPaths(cells []KVLine) (err error) {
	for _, c := range cells {
		if c.Path == "" || strings.ContainsAny(c.Path, " \"\n") {
			err = eh.Errorf("invalid cell path %q: must be non-empty, without spaces, quotes, or newlines", c.Path)
			return
		}
	}
	return
}

// SerializeRecordText is the inverse of [ParseRecordText]: render the
// in-memory cell slice back to pijul's textual flat-KV format, with
// values strconv.Quote'd to match [splitKVLine]. The trailing newline is
// structurally important — without it pijul's patch graph treats the EOF
// context node as overlapping and may promote unrelated edits into
// spurious conflicts.
//
// Conflict blocks use fixed side labels "1" and "2"; pijul does not
// require any specific values in those slots, only that the block is
// well-formed.
func SerializeRecordText(cells []KVLine) (raw []byte) {
	out := make([]string, 0, len(cells)+4)
	for _, c := range cells {
		if c.Conflict != nil {
			values := c.Conflict.AllValues()
			out = append(out, fmt.Sprintf(">>>>>>> %d", 1))
			for i, v := range values {
				if i > 0 {
					out = append(out, "=======")
				}
				out = append(out, c.Path+" "+strconv.Quote(v))
			}
			out = append(out, fmt.Sprintf("<<<<<<< %d", len(values)))
		} else {
			out = append(out, c.Path+" "+strconv.Quote(c.Value))
		}
	}
	raw = []byte(strings.Join(out, "\n") + "\n")
	return
}
