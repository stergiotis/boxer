// Package cargolicenses harvests the license files of a Rust dependency tree
// into a directory a binary can ship beside itself.
//
// It reads `cargo metadata --locked` output rather than the crate registry, so
// what is collected is the exact locked source the artifact was built from —
// the same reason the build sources scripts/dev/rust-repro-env.sh (ADR-0215).
// Cargo's `license` field is a declaration; the files beside each manifest are
// the notice, and several licenses require shipping them.
package cargolicenses

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	cli "github.com/urfave/cli/v2"
)

// licenseFilePrefixes is what counts as a notice file beside a crate's
// manifest, lowercased. Crates spell these with and without an extension and
// with a license name appended (LICENSE-APACHE, LICENSE-MIT.txt).
var licenseFilePrefixes = []string{"license", "copying", "notice", "unlicense"}

// undeclared stands in for a crate with no SPDX expression in its manifest,
// where the supplied files are the only statement of terms.
const undeclared = "see supplied license files"

// indexName is the manifest written beside the collected files: one line per
// crate, so a reader can see what was in the tree without walking it.
const indexName = "INDEX.txt"

// metadata is the subset of `cargo metadata --format-version 1` this reads.
type metadata struct {
	Packages []pkg `json:"packages"`
}

type pkg struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	License      string `json:"license"`
	ManifestPath string `json:"manifest_path"`
}

func (p pkg) dirName() string { return p.Name + "-" + p.Version }

// NewCliCommand returns the `cargo-licenses` subcommand. Mounted under boxer's
// `gov` parent by public/gov/gov.go.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "cargo-licenses",
		Usage: "collect the license files of a locked Rust dependency tree (ADR-0215)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "metadata",
				Usage:    "path to `cargo metadata --locked --format-version 1` output",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "out",
				Usage:    "directory to write the per-crate files and " + indexName + " into",
				Required: true,
			},
		},
		Action: func(ctx *cli.Context) (err error) {
			n, err := Run(ctx.String("metadata"), ctx.String("out"))
			if err != nil {
				return
			}
			fmt.Printf("collected licenses for %d crates into %s\n", n, ctx.String("out"))
			return
		},
	}
}

// Run collects the license files of every crate in a metadata document and
// returns how many crates were indexed.
func Run(metadataPath string, outDir string) (crates int, err error) {
	raw, err := os.ReadFile(metadataPath)
	if err != nil {
		err = eb.Build().Str("path", metadataPath).Errorf("read the cargo metadata: %w", err)
		return
	}
	var md metadata
	if err = json.Unmarshal(raw, &md); err != nil {
		err = eb.Build().Str("path", metadataPath).Errorf("decode the cargo metadata: %w", err)
		return
	}
	if err = os.MkdirAll(outDir, 0o755); err != nil {
		err = eb.Build().Str("path", outDir).Errorf("create the output directory: %w", err)
		return
	}

	// Sorted so two runs over one lockfile write the same INDEX.txt, whatever
	// order cargo happened to emit its packages in.
	packages := slices.Clone(md.Packages)
	slices.SortFunc(packages, func(a, b pkg) int { return strings.Compare(a.dirName(), b.dirName()) })

	index := make([]string, 0, len(packages))
	for _, p := range packages {
		license := p.License
		if license == "" {
			license = undeclared
		}
		index = append(index, p.dirName()+": "+license)
		if err = copyNotices(p, outDir); err != nil {
			return
		}
	}
	crates = len(index)

	indexPath := filepath.Join(outDir, indexName)
	if err = os.WriteFile(indexPath, []byte(strings.Join(index, "\n")+"\n"), 0o644); err != nil {
		err = eb.Build().Str("path", indexPath).Errorf("write the index: %w", err)
	}
	return
}

// copyNotices copies the notice files beside a crate's manifest into its own
// directory under outDir. A crate with none gets no directory: the index line
// still records what it declared.
func copyNotices(p pkg, outDir string) (err error) {
	src := filepath.Dir(p.ManifestPath)
	entries, err := os.ReadDir(src)
	if err != nil {
		// A crate whose source is not on this machine — a workspace member
		// resolved elsewhere — has nothing to copy and is not a failure.
		if os.IsNotExist(err) {
			err = nil
			return
		}
		err = eb.Build().Str("crate", p.dirName()).Str("path", src).Errorf("read the crate source: %w", err)
		return
	}
	dst := filepath.Join(outDir, p.dirName())
	made := false
	for _, e := range entries {
		if e.IsDir() || !isNoticeFile(e.Name()) {
			continue
		}
		if !made {
			if err = os.MkdirAll(dst, 0o755); err != nil {
				err = eb.Build().Str("path", dst).Errorf("create the crate directory: %w", err)
				return
			}
			made = true
		}
		if err = copyFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			err = eb.Build().Str("crate", p.dirName()).Str("file", e.Name()).Errorf("copy the notice: %w", err)
			return
		}
	}
	return
}

func isNoticeFile(name string) bool {
	lower := strings.ToLower(name)
	return slices.ContainsFunc(licenseFilePrefixes, func(p string) bool { return strings.HasPrefix(lower, p) })
}

func copyFile(src string, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return
	}
	defer func() {
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
	}()
	_, err = io.Copy(out, in)
	return
}
