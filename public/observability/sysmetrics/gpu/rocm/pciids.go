//go:build linux && gpu_rocm && llm_generated_opus47

package rocm

import "strings"

// pciIDNames is a small subset of mainline-shipping AMD GPU codenames.
// Unknown ids fall back to "AMD Graphics".
//
// Maintenance: the upstream is amdgpu/include/asic_reg/gc/gc_*.h plus
// drivers/gpu/drm/amd/include/atomfirmware.h; thousands of revisions
// exist. We mirror only desktop and mobile parts a contemporary user
// is likely to hit. Extend on demand.
var pciIDNames = map[string]string{
	// RDNA 3 / Navi 31, 32, 33 — Radeon RX 7000-series
	"0x744c": "Radeon RX 7900 XTX",
	"0x7448": "Radeon Pro W7900",
	"0x7449": "Radeon RX 7900 XT",
	"0x7480": "Radeon RX 7700/7800 XT",
	"0x7470": "Radeon RX 7600",
	"0x7461": "Radeon RX 7600",

	// RDNA 2 / Navi 21, 22, 23, 24 — Radeon RX 6000-series
	"0x73a0": "Radeon RX 6900 XT",
	"0x73a1": "Radeon Pro V620",
	"0x73a3": "Radeon Pro W6800",
	"0x73af": "Radeon RX 6900 XT",
	"0x73bf": "Radeon RX 6800/6800 XT",
	"0x73df": "Radeon RX 6700/6700 XT",
	"0x73ef": "Radeon RX 6650 XT",
	"0x73ff": "Radeon RX 6600/6600 XT",
	"0x743f": "Radeon RX 6400/6500 XT",

	// CDNA / Instinct — datacenter
	"0x740c": "Instinct MI250X/MI250",
	"0x740f": "Instinct MI210",
	"0x738c": "Instinct MI100",
	"0x7408": "Instinct MI250X",

	// RDNA / Navi 10, 14 — Radeon RX 5000-series
	"0x731f": "Radeon RX 5700 XT",
	"0x7340": "Radeon RX 5500 XT",
	"0x7341": "Radeon Pro W5500",

	// Vega / Polaris — Radeon RX 500/Vega
	"0x66af": "Radeon VII",
	"0x687f": "Radeon RX Vega 64",
	"0x67df": "Radeon RX 480/580",
	"0x67ef": "Radeon RX 460/560",

	// Integrated (APU) — RDNA 3+ Phoenix / Strix variants
	"0x15bf": "Phoenix1 (Radeon 780M)",
	"0x15c8": "Phoenix2 (Radeon 740M)",
	"0x150e": "Vega 8 (Cezanne)",
	"0x1638": "Vega 8 (Renoir/Lucienne)",
	"0x164e": "Raphael (Radeon Graphics)",
	"0x1681": "Rembrandt (Radeon 680M)",
	"0x1506": "Rembrandt-X (Radeon 660M)",
	"0x1900": "Mendocino (Radeon 610M)",
	"0x1586": "Strix Halo (Radeon 8060S)",
}

// pciIDName returns the codename for a /sys/class/drm/card*/device/device
// reading. Lookups are case-insensitive — callers commonly hand us the
// raw "0x1586" form, the lowercase "0x1586" form, or just "1586".
func pciIDName(rawID string) (name string) {
	id := strings.ToLower(strings.TrimSpace(rawID))
	if !strings.HasPrefix(id, "0x") {
		id = "0x" + id
	}
	if n, ok := pciIDNames[id]; ok {
		return n
	}
	return "AMD Graphics"
}
