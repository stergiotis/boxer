package worldmap

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// Country geometry + identity, parsed once from the vendored Natural Earth
// 110m admin-0 asset (see assets/README.md for provenance; ADR-0114 §SD1).

//go:embed assets/ne_110m_admin_0_countries.geojson.gz
var neCountriesGz []byte

// CountryIdx indexes Atlas.Countries. NoCountry marks "no country" (sea in
// the raster index buffer, resolver miss).
type CountryIdx int32

const NoCountry CountryIdx = -1

// projPt is a ring vertex in normalized projection space (see projectNorm).
type projPt struct{ X, Y float32 }

// Country is one admin-0 feature: identity fields as shipped upstream (the
// `_EH` ISO variants — empty when upstream has none, e.g. Northern Cyprus)
// plus the projected outline rings. Rings concatenate every ring of every
// member polygon; the rasterizer's even-odd rule makes outer/hole/member
// distinctions irrelevant (members are disjoint, holes alternate parity).
type Country struct {
	Admin string
	Name  string
	A2    string // ISO 3166-1 alpha-2 (upstream ISO_A2_EH); "" when absent
	A3    string // ISO 3166-1 alpha-3 (upstream ISO_A3_EH); "" when absent

	rings [][]projPt
	// bbox in normalized projection space: minX, minY, maxX, maxY.
	bbox [4]float32
}

// Label is the human-facing form used in readouts: "Name (A3)" when a code
// exists, plain Name otherwise.
func (inst *Country) Label() string {
	if inst.A3 != "" {
		return inst.Name + " (" + inst.A3 + ")"
	}
	return inst.Name
}

// Atlas is the parsed country set plus the resolver's key table.
type Atlas struct {
	Countries []Country
	byKey     map[string]CountryIdx
}

// aliases maps additional uppercase spellings to the upstream alpha-3 code.
// Deliberately small: ADMIN + NAME already cover both long and short forms
// (e.g. "Democratic Republic of the Congo" and "Dem. Rep. Congo"); this table
// only adds common external forms neither field carries. Fuzzy matching is a
// deferred non-goal (ADR-0114 §SD7).
var aliases = map[string]string{
	"UNITED STATES":     "USA",
	"UK":                "GBR",
	"GREAT BRITAIN":     "GBR",
	"CZECH REPUBLIC":    "CZE",
	"REPUBLIC OF KOREA": "KOR",
	"KOREA":             "KOR",
	"SWAZILAND":         "SWZ",
	"MACEDONIA":         "MKD",
	"BURMA":             "MMR",
	"DRC":               "COD",
	"CAPE VERDE":        "CPV", // absent at 110m scale; kept for a clean miss
}

// geojson decode targets — only the consumed subset (assets/README.md).
type neFeatureCollection struct {
	Features []neFeature `json:"features"`
}
type neFeature struct {
	Properties neProps    `json:"properties"`
	Geometry   neGeometry `json:"geometry"`
}
type neProps struct {
	Admin  string `json:"ADMIN"`
	Name   string `json:"NAME"`
	IsoA2E string `json:"ISO_A2_EH"`
	IsoA3E string `json:"ISO_A3_EH"`
}
type neGeometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

var loadAtlasOnce = sync.OnceValues(loadAtlas)

// LoadAtlas parses the embedded asset once (process-wide) and returns the
// shared Atlas. Concurrency-safe; every caller sees the same instance.
func LoadAtlas() (*Atlas, error) { return loadAtlasOnce() }

func loadAtlas() (*Atlas, error) {
	zr, err := gzip.NewReader(bytes.NewReader(neCountriesGz))
	if err != nil {
		return nil, fmt.Errorf("worldmap: asset gunzip: %w", err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("worldmap: asset read: %w", err)
	}
	var fc neFeatureCollection
	if err = json.Unmarshal(raw, &fc); err != nil {
		return nil, fmt.Errorf("worldmap: asset parse: %w", err)
	}
	a := &Atlas{
		Countries: make([]Country, 0, len(fc.Features)),
		byKey:     make(map[string]CountryIdx, len(fc.Features)*4),
	}
	for _, f := range fc.Features {
		rings, rerr := decodeRings(f.Geometry)
		if rerr != nil {
			return nil, fmt.Errorf("worldmap: %s: %w", f.Properties.Admin, rerr)
		}
		if len(rings) == 0 {
			continue
		}
		ct := Country{
			Admin: f.Properties.Admin,
			Name:  f.Properties.Name,
			A2:    cleanIso(f.Properties.IsoA2E),
			A3:    cleanIso(f.Properties.IsoA3E),
			rings: rings,
			bbox:  ringsBBox(rings),
		}
		idx := CountryIdx(len(a.Countries))
		a.Countries = append(a.Countries, ct)
		a.addKey(ct.A2, idx)
		a.addKey(ct.A3, idx)
		a.addKey(ct.Admin, idx)
		a.addKey(ct.Name, idx)
	}
	for alias, a3 := range aliases {
		if idx, ok := a.byKey[a3]; ok {
			a.addKey(alias, idx)
		}
	}
	return a, nil
}

// cleanIso maps the upstream "-99" no-code sentinel (and empties) to "".
func cleanIso(s string) string {
	if s == "" || s == "-99" {
		return ""
	}
	return s
}

// addKey registers one uppercase-normalized resolver key. First writer wins:
// upstream identity fields are inserted in feature order before aliases, and
// no colliding pair is known in the vendored asset (guarded by a test).
func (inst *Atlas) addKey(key string, idx CountryIdx) {
	key = normalizeKey(key)
	if key == "" {
		return
	}
	if _, exists := inst.byKey[key]; !exists {
		inst.byKey[key] = idx
	}
}

func normalizeKey(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// Resolve maps a free-form value — ISO alpha-2/alpha-3 code, upstream ADMIN /
// NAME spelling, or an alias — to a country (ADR-0114 §SD4). Exact matches
// only, case-insensitive, surrounding whitespace ignored.
func (inst *Atlas) Resolve(s string) (idx CountryIdx, ok bool) {
	idx, ok = inst.byKey[normalizeKey(s)]
	if !ok {
		idx = NoCountry
	}
	return
}

// decodeRings flattens a Polygon or MultiPolygon into projected rings.
func decodeRings(g neGeometry) ([][]projPt, error) {
	switch g.Type {
	case "Polygon":
		var poly [][][2]float64
		if err := json.Unmarshal(g.Coordinates, &poly); err != nil {
			return nil, err
		}
		return projectPoly(nil, poly), nil
	case "MultiPolygon":
		var mp [][][][2]float64
		if err := json.Unmarshal(g.Coordinates, &mp); err != nil {
			return nil, err
		}
		var rings [][]projPt
		for _, poly := range mp {
			rings = projectPoly(rings, poly)
		}
		return rings, nil
	default:
		return nil, fmt.Errorf("unsupported geometry type %q", g.Type)
	}
}

func projectPoly(dst [][]projPt, poly [][][2]float64) [][]projPt {
	for _, ring := range poly {
		if len(ring) < 4 { // degenerate (GeoJSON rings repeat the first point)
			continue
		}
		pr := make([]projPt, len(ring))
		for i, ll := range ring {
			x, y := projectNorm(ll[0], ll[1])
			pr[i] = projPt{X: float32(x), Y: float32(y)}
		}
		dst = append(dst, pr)
	}
	return dst
}

func ringsBBox(rings [][]projPt) (bb [4]float32) {
	bb = [4]float32{1, 1, 0, 0}
	for _, r := range rings {
		for _, p := range r {
			if p.X < bb[0] {
				bb[0] = p.X
			}
			if p.Y < bb[1] {
				bb[1] = p.Y
			}
			if p.X > bb[2] {
				bb[2] = p.X
			}
			if p.Y > bb[3] {
				bb[3] = p.Y
			}
		}
	}
	return
}
