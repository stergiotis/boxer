package assignments

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/identity/fibonacci"
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The committed claims of tagged id streams (ADR-0319 §SD2). A stream is a
// tag value whose bodies are row identities leased at run time, not names
// registered in version control, so it has no (ordinal, name, id) table to
// pin. What it has is the claim itself — the value, and the cardinality the
// claimant declared — and the claim is the decision: a value changed later
// changes every id minted under it. The stream golden pins that, and the
// union check reads it beside the membership goldens, so a stream and a
// vocabulary can no more share a tag value than two vocabularies can.

// StreamGoldenBaseName is the file a package that claims id streams keeps
// them in, under its testdata directory.
const StreamGoldenBaseName = "idstream-claims.golden"

// StreamClaim is one row of a stream golden: the claim's name, its tag value,
// the code width the value carries, and the ids the claimant declared.
type StreamClaim struct {
	Name           string
	TagValue       uint32
	Width          int
	MaxExpectedIds uint64
}

// SnapshotStreams reads the claims a package declares, ordered by name. A
// claim that did not pass through tagmint is refused, as is a name given
// twice.
func SnapshotStreams(claims []tagmint.ClaimedTagValue) (r []StreamClaim, err error) {
	seen := make(map[string]struct{}, len(claims))
	for i, c := range claims {
		if !c.IsValid() {
			err = eb.Build().Int("index", i).Errorf("an id stream is not a tagmint claim")
			return
		}
		if _, dup := seen[c.Name()]; dup {
			err = eb.Build().Str("name", c.Name()).Errorf("an id stream is listed twice")
			return
		}
		seen[c.Name()] = struct{}{}
		r = append(r, StreamClaim{Name: c.Name(), TagValue: c.Value().Value(), Width: int(c.Tag().GetTagWidth()), MaxExpectedIds: c.MaxExpectedIds()})
	}
	slices.SortFunc(r, func(a, b StreamClaim) int { return strings.Compare(a.Name, b.Name) })
	return
}

// RenderStreams writes a stream golden: a header, then one tab-separated row
// per claim.
func RenderStreams(claims []StreamClaim) string {
	var sb strings.Builder
	sb.WriteString("# name\ttagValue\twidth\tmaxExpectedIds\n")
	sb.WriteString("# The committed id-stream claims (ADR-0319 §SD2). A changed tag value\n")
	sb.WriteString("# changes every id already minted under it; a new line does not.\n")
	sb.WriteString("# Regenerate with " + RegenEnvVar + "=1 go test ./<the claiming package>/...\n")
	for _, c := range claims {
		sb.WriteString(c.Name)
		sb.WriteByte('\t')
		sb.WriteString(strconv.FormatUint(uint64(c.TagValue), 10))
		sb.WriteByte('\t')
		sb.WriteString(strconv.Itoa(c.Width))
		sb.WriteByte('\t')
		sb.WriteString(strconv.FormatUint(c.MaxExpectedIds, 10))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// ParseStreams reads back what RenderStreams wrote.
func ParseStreams(text string) (r []StreamClaim, err error) {
	sc := bufio.NewScanner(strings.NewReader(text))
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 4 {
			err = eb.Build().Int("line", lineNo).Str("text", line).Errorf("expected four tab-separated fields")
			return
		}
		tv, cerr := strconv.ParseUint(parts[1], 10, 32)
		if cerr != nil {
			err = eb.Build().Int("line", lineNo).Errorf("unable to parse tag value: %w", cerr)
			return
		}
		width, cerr := strconv.Atoi(parts[2])
		if cerr != nil {
			err = eb.Build().Int("line", lineNo).Errorf("unable to parse width: %w", cerr)
			return
		}
		n, cerr := strconv.ParseUint(parts[3], 10, 64)
		if cerr != nil {
			err = eb.Build().Int("line", lineNo).Errorf("unable to parse maxExpectedIds: %w", cerr)
			return
		}
		r = append(r, StreamClaim{Name: parts[0], TagValue: uint32(tv), Width: width, MaxExpectedIds: n})
	}
	err = sc.Err()
	return
}

// StreamGoldenPath is the stream golden's location for a package directory.
func StreamGoldenPath(packageDir string) string {
	return filepath.Join(packageDir, "testdata", StreamGoldenBaseName)
}

// WriteStreamGoldenFile renders claims to the package's stream golden.
func WriteStreamGoldenFile(packageDir string, claims []tagmint.ClaimedTagValue) (err error) {
	live, err := SnapshotStreams(claims)
	if err != nil {
		return
	}
	p := StreamGoldenPath(packageDir)
	if err = os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return eb.Build().Str("path", p).Errorf("unable to create the golden's directory: %w", err)
	}
	if err = os.WriteFile(p, []byte(RenderStreams(live)), 0o644); err != nil {
		err = eb.Build().Str("path", p).Errorf("unable to write the golden: %w", err)
	}
	return
}

// CompareToStreamGoldenFile reports how claims differ from the package's
// committed stream golden; an empty result means they agree.
func CompareToStreamGoldenFile(packageDir string, claims []tagmint.ClaimedTagValue) (differences []string, err error) {
	raw, err := os.ReadFile(StreamGoldenPath(packageDir))
	if err != nil {
		err = eb.Build().Str("path", StreamGoldenPath(packageDir)).Errorf("unable to read the stream golden (regenerate with "+RegenEnvVar+"=1): %w", err)
		return
	}
	golden, err := ParseStreams(string(raw))
	if err != nil {
		return
	}
	live, err := SnapshotStreams(claims)
	if err != nil {
		return
	}
	differences = DiffStreams(golden, live)
	return
}

// DiffStreams compares two stream tables by name. A moved tag value is the
// breaking edit; a changed declaration is reported too, since it is what the
// claim's width class was checked against.
func DiffStreams(golden, live []StreamClaim) (differences []string) {
	byName := make(map[string]StreamClaim, len(golden))
	for _, g := range golden {
		byName[g.Name] = g
	}
	seen := make(map[string]struct{}, len(live))
	for _, l := range live {
		seen[l.Name] = struct{}{}
		g, has := byName[l.Name]
		switch {
		case !has:
			differences = append(differences, fmt.Sprintf("+ %s claims tag value %d and is not in the golden — append it", l.Name, l.TagValue))
		case g.TagValue != l.TagValue || g.Width != l.Width:
			differences = append(differences, fmt.Sprintf("! %s moved from tag value %d to %d — every id minted under the old value keeps it", l.Name, g.TagValue, l.TagValue))
		case g.MaxExpectedIds != l.MaxExpectedIds:
			differences = append(differences, fmt.Sprintf("~ %s now declares %d ids, not %d", l.Name, l.MaxExpectedIds, g.MaxExpectedIds))
		}
	}
	for _, g := range golden {
		if _, has := seen[g.Name]; !has {
			differences = append(differences, fmt.Sprintf("- %s (tag value %d) is in the golden and no longer claimed — its value stays spent", g.Name, g.TagValue))
		}
	}
	slices.Sort(differences)
	return
}

// CheckStreamClass reports whether a claim's tag value lies in the width
// class the fibonacci planner advises for its declared cardinality (ADR-0319
// §SD2): the narrowest code that still leaves room for the declared bodies.
func CheckStreamClass(c StreamClaim) (err error) {
	lo, hi, err := fibonacci.SelectFittingTagValueRange(c.MaxExpectedIds)
	if err != nil {
		return
	}
	if c.TagValue < lo.Value() || c.TagValue >= hi.Value() {
		err = eb.Build().Str("name", c.Name).Uint32("tagValue", c.TagValue).Uint32("advisedFromIncl", lo.Value()).Uint32("advisedToExcl", hi.Value()).Errorf("the stream's tag value is not in the class the planner advises for its declared cardinality")
	}
	return
}

// FindStreamGoldens walks root and returns every committed stream golden,
// keyed by the path it was read from, skipping what FindGoldens skips.
func FindStreamGoldens(root string) (r map[string][]StreamClaim, err error) {
	r = make(map[string][]StreamClaim)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path != root && skipDirName(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() != StreamGoldenBaseName {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return eb.Build().Str("path", path).Errorf("unable to read: %w", rerr)
		}
		parsed, perr := ParseStreams(string(raw))
		if perr != nil {
			return eb.Build().Str("path", path).Errorf("unable to parse: %w", perr)
		}
		r[path] = parsed
		return nil
	})
	return
}
