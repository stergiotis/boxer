package wasmsurvey

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// This file renders a Survey two ways: a compact human report (summary matrix
// + worst-first package list with blame) and the machine-readable JSON DTO.

// worstTierOf returns the most restrictive verdict a package earned across all
// surveyed targets — the key the report sorts and groups by.
func worstTierOf(pr PackageReport) (t Tier) {
	for _, v := range pr.Targets {
		t = worstTier(t, v.Tier())
	}
	return
}

// RenderJSON writes the survey as indented JSON.
func RenderJSON(s Survey, w io.Writer) (err error) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}

// RenderText writes the human report. showGreen controls whether Green
// packages are listed individually (they are always counted in the summary);
// with it false they are summarized as a trailing count to keep the blocker
// list legible.
func RenderText(s Survey, w io.Writer, showGreen bool) (err error) {
	if _, err = fmt.Fprintf(w, "wasmsurvey — %s\n", s.RootModule); err != nil {
		return
	}
	tinygo := s.TinyGoVer
	if tinygo == "" {
		tinygo = "(not run)"
	}
	fmt.Fprintf(w, "mode=%s  tinygo=%s  targets=%s\n", s.Mode, tinygo, strings.Join(s.Targets, ","))
	if len(s.Tags) > 0 {
		fmt.Fprintf(w, "tags=%s\n", strings.Join(s.Tags, ","))
	}
	fmt.Fprintln(w, "note: verdicts cover importable library packages; package main, test-only, and internal/ packages are excluded.")
	for _, warn := range s.Warnings {
		fmt.Fprintf(w, "! %s\n", warn)
	}
	fmt.Fprintln(w)

	renderSummary(s, w)
	fmt.Fprintln(w)
	renderPackages(s, w, showGreen)
	return nil
}

// targetCounts tallies one target's column.
type targetCounts struct {
	green, yellow, red int
	probed, disagree   int
}

func renderSummary(s Survey, w io.Writer) {
	// Index counts by target name (Survey.Targets order).
	counts := make(map[string]*targetCounts, len(s.Targets))
	for _, name := range s.Targets {
		counts[name] = &targetCounts{}
	}
	for _, pr := range s.Packages {
		for _, v := range pr.Targets {
			c := counts[v.Target.String()]
			if c == nil {
				continue
			}
			switch v.Tier() {
			case TierGreen:
				c.green++
			case TierYellow:
				c.yellow++
			case TierRed:
				c.red++
			}
			if v.Probed {
				c.probed++
			}
			if v.Disagrees() {
				c.disagree++
			}
		}
	}

	fmt.Fprintf(w, "summary (%d packages)\n", len(s.Packages))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  target\tgreen\tyellow\tred\tprobed\tdisagree")
	for _, name := range s.Targets {
		c := counts[name]
		fmt.Fprintf(tw, "  %s\t%d\t%d\t%d\t%d\t%d\n", name, c.green, c.yellow, c.red, c.probed, c.disagree)
	}
	_ = tw.Flush()
}

// tierTag is the short per-target cell, e.g. "wasi:red*" (the * marks an
// empirically-probed verdict).
func tierTag(v TargetVerdict) (s string) {
	mark := ""
	if v.Probed {
		mark = "*"
	}
	return fmt.Sprintf("%s:%s%s", v.Target, v.Tier(), mark)
}

func renderPackages(s Survey, w io.Writer, showGreen bool) {
	// Sort worst-first, then by path, so blockers lead.
	pkgs := append([]PackageReport(nil), s.Packages...)
	sort.SliceStable(pkgs, func(i, j int) bool {
		wi, wj := worstTierOf(pkgs[i]), worstTierOf(pkgs[j])
		if wi != wj {
			return wi > wj // Red(3) before Yellow(2) before Green(1)
		}
		return pkgs[i].ImportPath < pkgs[j].ImportPath
	})

	fmt.Fprintln(w, "packages (worst-first; * = empirically probed)")
	greenHidden := 0
	for _, pr := range pkgs {
		worst := worstTierOf(pr)
		if worst == TierGreen && !showGreen {
			greenHidden++
			continue
		}

		cells := make([]string, 0, len(pr.Targets))
		for _, v := range pr.Targets {
			cells = append(cells, tierTag(v))
		}
		fmt.Fprintf(w, "%-6s %-34s %s\n", strings.ToUpper(worst.String()), strings.Join(cells, " "), pr.ImportPath)

		// Blame: distinct reasons across targets (dedup by kind+leaf), so the
		// actionable cause shows once even when several targets share it.
		for _, r := range dedupReasons(pr) {
			via := ""
			if len(r.Path) > 1 {
				via = "  via " + strings.Join(r.Path, "→")
			}
			detail := ""
			if r.Detail != "" {
				detail = "  — " + r.Detail
			}
			fmt.Fprintf(w, "   ↳ %-20s %s%s%s\n", r.Kind, r.Leaf, via, detail)
		}
	}
	if greenHidden > 0 {
		fmt.Fprintf(w, "… and %d green package(s) not shown (pass --show-green to list)\n", greenHidden)
	}
}

// dedupReasons collects the distinct (kind, leaf) reasons across a package's
// targets, preserving the first occurrence (which carries a representative
// blame path/detail).
func dedupReasons(pr PackageReport) (out []Reason) {
	seen := make(map[string]bool)
	for _, v := range pr.Targets {
		for _, r := range v.Reasons {
			key := r.Kind.String() + "\x00" + r.Leaf
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, r)
		}
	}
	return
}
