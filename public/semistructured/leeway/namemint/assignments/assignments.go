// Package assignments reads and writes a name mint's committed assignment
// golden: the (ordinal, name, id) table a version-controlled vocabulary
// promises to keep.
//
// The registry refuses the loud ways an assignment can break — a repeated
// ordinal, a name registered twice from different places, an ordinal too wide
// for the tag. What it cannot see is an ordinal *edited in place*: that
// compiles, vets, writes and reads, and only the rows already stored under the
// old number disagree. A committed table is what makes that edit visible,
// because it shows up in the diff as a changed line rather than as nothing at
// all (ADR-0183 D0/D1).
//
// The goldens are also what make claim uniqueness total. A registry can only
// check the vocabularies its own binary links; the committed files are
// checkable all at once, by anything, which is what the union test in this
// package does.
package assignments

import (
	"bufio"
	"fmt"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// GoldenBaseName is the file every version-controlled vocabulary keeps its
// table in, under the package's testdata directory. One name, so the union
// check can find them all without a registry of registries.
const GoldenBaseName = "membership-assignments.golden"

// RegenEnvVar names the environment variable that rewrites a golden instead of
// comparing against it, following the repo's other fixture-regeneration
// switches.
const RegenEnvVar = "BOXER_VOCAB_GOLDEN_REGEN"

// Assignment is one row of the table: the ordinal a registration declares, the
// name it declares it for, and the tagged id the two compose into.
type Assignment struct {
	Ordinal uint64
	Name    string
	Id      uint64
}

// SourceI is the one method this package needs from a natural-key registry.
// It is stated locally so a vocabulary's test does not have to name a registry
// type it never otherwise mentions.
type SourceI interface {
	IterateAll() iter.Seq2[naming.StylableName, registry.RegisteredNaturalKey]
}

// Snapshot reads every registration out of src, ordered by ordinal.
//
// It refuses what a golden must never record: a zero id (tag value 0 is the
// invalid sentinel, ADR-0106 §SD8) and two names sharing one ordinal or one
// id. A registry refuses those at registration; a Snapshot of something that
// is not a registry — a hand-built source, a future non-Go one — gets the same
// answer here rather than writing a table that cannot be true.
func Snapshot(src SourceI) (r []Assignment, err error) {
	if src == nil {
		err = eb.Build().Errorf("no assignment source")
		return
	}
	byId := make(map[uint64]string)
	byOrdinal := make(map[uint64]string)
	for name, reg := range src.IterateAll() {
		id := reg.GetId()
		if id.Value() == 0 {
			err = eb.Build().Stringer("name", name).Errorf("membership resolves to the zero id")
			return
		}
		ordinal := id.RemoveTag().Value()
		if prev, dup := byId[id.Value()]; dup {
			err = eb.Build().Stringer("name", name).Str("other", prev).Uint64("id", id.Value()).Errorf("two names carry one id")
			return
		}
		if prev, dup := byOrdinal[ordinal]; dup {
			err = eb.Build().Stringer("name", name).Str("other", prev).Uint64("ordinal", ordinal).Errorf("two names carry one ordinal")
			return
		}
		byId[id.Value()] = string(name)
		byOrdinal[ordinal] = string(name)
		r = append(r, Assignment{Ordinal: ordinal, Name: string(name), Id: id.Value()})
	}
	slices.SortFunc(r, func(a, b Assignment) int {
		return int(a.Ordinal) - int(b.Ordinal)
	})
	return
}

// Render writes the table: a header naming the columns, then one tab-separated
// row per assignment, ordinal first so a diff reads as the table it is.
func Render(a []Assignment) string {
	var sb strings.Builder
	sb.WriteString("# ordinal\tname\tid\n")
	sb.WriteString("# The committed assignment table (ADR-0183 D1). A changed line here\n")
	sb.WriteString("# re-points rows already stored under the old id; a new line does not.\n")
	sb.WriteString("# Regenerate with " + RegenEnvVar + "=1 go test ./<the vocabulary package>/...\n")
	for _, x := range a {
		sb.WriteString(strconv.FormatUint(x.Ordinal, 10))
		sb.WriteByte('\t')
		sb.WriteString(x.Name)
		sb.WriteByte('\t')
		sb.WriteString(strconv.FormatUint(x.Id, 10))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// Parse reads back what Render wrote. Comment and blank lines are skipped, so
// the header carries prose without a format flag.
func Parse(text string) (r []Assignment, err error) {
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			err = eb.Build().Int("line", lineNo).Str("text", line).Errorf("expected three tab-separated fields")
			return
		}
		ordinal, cerr := strconv.ParseUint(parts[0], 10, 64)
		if cerr != nil {
			err = eb.Build().Int("line", lineNo).Errorf("unable to parse ordinal: %w", cerr)
			return
		}
		id, cerr := strconv.ParseUint(parts[2], 10, 64)
		if cerr != nil {
			err = eb.Build().Int("line", lineNo).Errorf("unable to parse id: %w", cerr)
			return
		}
		r = append(r, Assignment{Ordinal: ordinal, Name: parts[1], Id: id})
	}
	err = sc.Err()
	return
}

// GoldenPath is the golden's location for a package directory.
func GoldenPath(packageDir string) string {
	return filepath.Join(packageDir, "testdata", GoldenBaseName)
}

// WriteGoldenFile renders src's assignments to the package's golden, creating
// the testdata directory if needed.
func WriteGoldenFile(packageDir string, src SourceI) (err error) {
	live, err := Snapshot(src)
	if err != nil {
		return
	}
	p := GoldenPath(packageDir)
	err = os.MkdirAll(filepath.Dir(p), 0o755)
	if err != nil {
		err = eb.Build().Str("path", p).Errorf("unable to create the golden's directory: %w", err)
		return
	}
	err = os.WriteFile(p, []byte(Render(live)), 0o644)
	if err != nil {
		err = eb.Build().Str("path", p).Errorf("unable to write the golden: %w", err)
	}
	return
}

// CompareToGoldenFile reports how src differs from the package's committed
// golden, as human-readable lines; an empty result means they agree.
//
// The differences are reported per name rather than as a text diff, because
// the three cases are not equally serious and the caller should be told which
// one this is: an added name is ordinary, a removed one is a deletion to think
// about, and a changed id is the breaking edit this whole mechanism exists to
// surface.
func CompareToGoldenFile(packageDir string, src SourceI) (differences []string, err error) {
	raw, err := os.ReadFile(GoldenPath(packageDir))
	if err != nil {
		err = eb.Build().Str("path", GoldenPath(packageDir)).Errorf("unable to read the golden (regenerate with "+RegenEnvVar+"=1): %w", err)
		return
	}
	golden, err := Parse(string(raw))
	if err != nil {
		return
	}
	live, err := Snapshot(src)
	if err != nil {
		return
	}
	differences = Diff(golden, live)
	return
}

// Diff compares two tables by name.
func Diff(golden, live []Assignment) (differences []string) {
	byName := make(map[string]Assignment, len(golden))
	for _, g := range golden {
		byName[g.Name] = g
	}
	seen := make(map[string]struct{}, len(live))
	for _, l := range live {
		seen[l.Name] = struct{}{}
		g, has := byName[l.Name]
		if !has {
			differences = append(differences, fmt.Sprintf("+ %s is registered at ordinal %d (id %d) and is not in the golden — append it", l.Name, l.Ordinal, l.Id))
			continue
		}
		if g.Id != l.Id || g.Ordinal != l.Ordinal {
			differences = append(differences, fmt.Sprintf("! %s moved from ordinal %d (id %d) to ordinal %d (id %d) — rows already stored carry the old id", l.Name, g.Ordinal, g.Id, l.Ordinal, l.Id))
		}
	}
	for _, g := range golden {
		if _, has := seen[g.Name]; !has {
			differences = append(differences, fmt.Sprintf("- %s (ordinal %d, id %d) is in the golden and no longer registered — its ordinal stays spent", g.Name, g.Ordinal, g.Id))
		}
	}
	slices.Sort(differences)
	return
}

// TagValueOf returns the tag value the id was composed under — which
// vocabulary it belongs to, read out of the id itself.
func TagValueOf(id uint64) identifier.TagValue {
	return identifier.TaggedId(id).GetTag().GetValue()
}

// FindGoldens walks root and returns every committed assignment table it
// finds, keyed by the path it was read from.
func FindGoldens(root string) (r map[string][]Assignment, err error) {
	r = make(map[string][]Assignment)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() != GoldenBaseName {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return eb.Build().Str("path", path).Errorf("unable to read: %w", rerr)
		}
		parsed, perr := Parse(string(raw))
		if perr != nil {
			return eb.Build().Str("path", path).Errorf("unable to parse: %w", perr)
		}
		r[path] = parsed
		return nil
	})
	return
}
