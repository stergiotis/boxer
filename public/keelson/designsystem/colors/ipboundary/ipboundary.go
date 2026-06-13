// Package ipboundary searches IDS-generated palette hex values against
// the cached published anchors of major design systems (ADR-0029 §SD12,
// ADR-0033 §SD7).
//
// A hex match alone is not a violation; the combination of (matching hex)
// + (matching role) triggers a Tier 3 perturbation per ADR-0033 §SD7.
package ipboundary

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Source is one cached published palette.
type Source struct {
	System  string             // e.g., "tailwind"
	URL     string             // _source field
	Date    string             // _retrieved field
	Anchors map[string]string  // role/token name → "#rrggbb"
}

// Collision records one matched hex.
type Collision struct {
	IDSToken    string // e.g., "semantic.info.default"
	IDSHex      string
	System      string
	SystemToken string
	SystemHex   string
	RoleMatches bool // crude: substring overlap between IDS token and system token
}

// LoadAll reads every <system>.json under refsDir and returns the parsed
// Source list (filename minus .json becomes the System name).
func LoadAll(refsDir string) (sources []Source, err error) {
	entries, err := os.ReadDir(refsDir)
	if err != nil {
		err = fmt.Errorf("read ip-refs dir %s: %w", refsDir, err)
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		var src Source
		src, err = loadOne(filepath.Join(refsDir, name))
		if err != nil {
			return
		}
		src.System = strings.TrimSuffix(name, ".json")
		sources = append(sources, src)
	}
	return
}

func loadOne(path string) (src Source, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		err = fmt.Errorf("read %s: %w", path, err)
		return
	}
	var raw map[string]string
	err = json.Unmarshal(b, &raw)
	if err != nil {
		err = fmt.Errorf("parse %s: %w", path, err)
		return
	}
	src.Anchors = make(map[string]string, len(raw))
	for k, v := range raw {
		switch k {
		case "_source":
			src.URL = v
		case "_retrieved":
			src.Date = v
		case "_note":
			// dropped
		default:
			src.Anchors[k] = strings.ToLower(strings.TrimSpace(v))
		}
	}
	return
}

// Search returns every (idsToken → systemToken) collision across all
// loaded sources. Hex comparison is case-insensitive.
func Search(idsTokens map[string]string, sources []Source) (collisions []Collision) {
	tokenNames := make([]string, 0, len(idsTokens))
	for n := range idsTokens {
		tokenNames = append(tokenNames, n)
	}
	sort.Strings(tokenNames)

	for _, idsName := range tokenNames {
		idsHex := strings.ToLower(idsTokens[idsName])
		for _, src := range sources {
			anchorNames := make([]string, 0, len(src.Anchors))
			for n := range src.Anchors {
				anchorNames = append(anchorNames, n)
			}
			sort.Strings(anchorNames)
			for _, anchor := range anchorNames {
				if src.Anchors[anchor] == idsHex {
					collisions = append(collisions, Collision{
						IDSToken:    idsName,
						IDSHex:      idsHex,
						System:      src.System,
						SystemToken: anchor,
						SystemHex:   src.Anchors[anchor],
						RoleMatches: rolesOverlap(idsName, anchor),
					})
				}
			}
		}
	}
	return
}

// rolesOverlap is a crude semantic-overlap check: do the two token names
// share any role-keyword? (e.g., "info" / "blue" / "primary" / "error").
func rolesOverlap(idsName, anchor string) (overlap bool) {
	roleSyns := map[string][]string{
		"info":    {"info", "blue", "primary", "interactive"},
		"success": {"success", "green", "positive"},
		"warning": {"warning", "yellow", "amber", "orange", "caution"},
		"error":   {"error", "red", "danger", "critical"},
		"accent":  {"accent", "violet", "purple", "pink", "highlight"},
		"neutral": {"neutral", "gray", "grey", "slate", "surface", "border", "text"},
	}
	idsLower := strings.ToLower(idsName)
	anchorLower := strings.ToLower(anchor)
	for _, syns := range roleSyns {
		idsHit := false
		anchorHit := false
		for _, s := range syns {
			if strings.Contains(idsLower, s) {
				idsHit = true
			}
			if strings.Contains(anchorLower, s) {
				anchorHit = true
			}
		}
		if idsHit && anchorHit {
			overlap = true
			return
		}
	}
	return
}
