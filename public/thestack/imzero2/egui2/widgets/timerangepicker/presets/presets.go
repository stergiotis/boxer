//go:build llm_generated_opus47

// Copyright 2014-2022 Grafana Labs
// Copyright 2026 Panos Stergiotis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Modifications applied during port (see ../NOTICE):
//   - Re-implemented from React/TypeScript (public/app/core/components/
//     TimePicker/rangeOptions.ts in Grafana v7.5.17) to Go.
//   - Translated each preset's relative-expression body from Grafana
//     datemath (`now-5m`, `now/d`, etc.) to ClickHouse SQL expressions
//     operating against an injected `anchor_now` DateTime64(3, 'UTC')
//     scalar. Labels and semantic intent are Grafana-derived; the
//     concrete SQL bodies are pebble2impl-authored.
//   - Refactored to the boxer coding standards: receiver `inst`,
//     interface suffix `I`, named return values, compile-time
//     interface assertion.

// Package presets provides the time range picker's quick-range
// sidebar entries. PresetI describes a single labelled (from, to)
// pair of ClickHouse SQL expressions; Registry is an ordered
// collection. DefaultGrafana75 returns a registry pre-populated with
// the time range presets shipped by Grafana 7.5.
package presets

// PresetI is one entry in the picker's quick-range sidebar. Label is
// shown to the user; FromSQL and ToSQL are ClickHouse SQL
// expressions referencing the injected `anchor_now` scalar.
type PresetI interface {
	Label() (s string)
	FromSQL() (s string)
	ToSQL() (s string)
}

type preset struct {
	label   string
	fromSQL string
	toSQL   string
}

var _ PresetI = preset{}

func (inst preset) Label() (s string)   { s = inst.label; return }
func (inst preset) FromSQL() (s string) { s = inst.fromSQL; return }
func (inst preset) ToSQL() (s string)   { s = inst.toSQL; return }

// NewPreset returns a PresetI from concrete strings. Useful for
// callers registering site-specific presets.
func NewPreset(label, fromSQL, toSQL string) (p PresetI) {
	p = preset{label: label, fromSQL: fromSQL, toSQL: toSQL}
	return
}

// Registry holds an ordered list of named presets. Duplicate labels
// are not enforced; iteration follows insertion order.
type Registry struct {
	entries []PresetI
}

// NewRegistry returns an empty Registry.
func NewRegistry() (r *Registry) {
	r = &Registry{}
	return
}

// Add appends a preset to the registry.
func (inst *Registry) Add(p PresetI) {
	inst.entries = append(inst.entries, p)
}

// All returns the registered presets in insertion order. Callers
// must not mutate the returned slice.
func (inst *Registry) All() (presets []PresetI) {
	presets = inst.entries
	return
}

// Len returns the number of registered presets.
func (inst *Registry) Len() (n int) {
	n = len(inst.entries)
	return
}

// DefaultGrafana75 returns a registry pre-populated with the time
// range presets shipped by Grafana 7.5. Labels and semantic intent
// are Grafana-derived (Apache-2.0; see ../NOTICE); the concrete SQL
// bodies are pebble2impl-authored, evaluating against an injected
// `anchor_now` DateTime64(3, 'UTC') scalar.
func DefaultGrafana75() (r *Registry) {
	r = NewRegistry()
	r.Add(preset{label: "Last 5 minutes", fromSQL: "anchor_now - INTERVAL 5 MINUTE", toSQL: "anchor_now"})
	r.Add(preset{label: "Last 15 minutes", fromSQL: "anchor_now - INTERVAL 15 MINUTE", toSQL: "anchor_now"})
	r.Add(preset{label: "Last 30 minutes", fromSQL: "anchor_now - INTERVAL 30 MINUTE", toSQL: "anchor_now"})
	r.Add(preset{label: "Last 1 hour", fromSQL: "anchor_now - INTERVAL 1 HOUR", toSQL: "anchor_now"})
	r.Add(preset{label: "Last 3 hours", fromSQL: "anchor_now - INTERVAL 3 HOUR", toSQL: "anchor_now"})
	r.Add(preset{label: "Last 6 hours", fromSQL: "anchor_now - INTERVAL 6 HOUR", toSQL: "anchor_now"})
	r.Add(preset{label: "Last 12 hours", fromSQL: "anchor_now - INTERVAL 12 HOUR", toSQL: "anchor_now"})
	r.Add(preset{label: "Last 24 hours", fromSQL: "anchor_now - INTERVAL 24 HOUR", toSQL: "anchor_now"})
	r.Add(preset{label: "Last 2 days", fromSQL: "anchor_now - INTERVAL 2 DAY", toSQL: "anchor_now"})
	r.Add(preset{label: "Last 7 days", fromSQL: "anchor_now - INTERVAL 7 DAY", toSQL: "anchor_now"})
	r.Add(preset{label: "Last 30 days", fromSQL: "anchor_now - INTERVAL 30 DAY", toSQL: "anchor_now"})
	r.Add(preset{label: "Last 90 days", fromSQL: "anchor_now - INTERVAL 90 DAY", toSQL: "anchor_now"})
	r.Add(preset{label: "Last 6 months", fromSQL: "anchor_now - INTERVAL 6 MONTH", toSQL: "anchor_now"})
	r.Add(preset{label: "Last 1 year", fromSQL: "anchor_now - INTERVAL 1 YEAR", toSQL: "anchor_now"})
	r.Add(preset{label: "Last 2 years", fromSQL: "anchor_now - INTERVAL 2 YEAR", toSQL: "anchor_now"})
	r.Add(preset{label: "Last 5 years", fromSQL: "anchor_now - INTERVAL 5 YEAR", toSQL: "anchor_now"})
	r.Add(preset{label: "Yesterday", fromSQL: "toStartOfDay(anchor_now - INTERVAL 1 DAY)", toSQL: "toStartOfDay(anchor_now)"})
	r.Add(preset{label: "Day before yesterday", fromSQL: "toStartOfDay(anchor_now - INTERVAL 2 DAY)", toSQL: "toStartOfDay(anchor_now - INTERVAL 1 DAY)"})
	r.Add(preset{label: "This day last week", fromSQL: "toStartOfDay(anchor_now - INTERVAL 7 DAY)", toSQL: "toStartOfDay(anchor_now - INTERVAL 6 DAY)"})
	r.Add(preset{label: "Previous week", fromSQL: "toStartOfWeek(anchor_now - INTERVAL 7 DAY)", toSQL: "toStartOfWeek(anchor_now)"})
	r.Add(preset{label: "Previous month", fromSQL: "toStartOfMonth(anchor_now - INTERVAL 1 MONTH)", toSQL: "toStartOfMonth(anchor_now)"})
	r.Add(preset{label: "Previous year", fromSQL: "toStartOfYear(anchor_now - INTERVAL 1 YEAR)", toSQL: "toStartOfYear(anchor_now)"})
	r.Add(preset{label: "Today", fromSQL: "toStartOfDay(anchor_now)", toSQL: "addDays(toStartOfDay(anchor_now), 1)"})
	r.Add(preset{label: "Today so far", fromSQL: "toStartOfDay(anchor_now)", toSQL: "anchor_now"})
	r.Add(preset{label: "This week", fromSQL: "toStartOfWeek(anchor_now)", toSQL: "addDays(toStartOfWeek(anchor_now), 7)"})
	r.Add(preset{label: "This week so far", fromSQL: "toStartOfWeek(anchor_now)", toSQL: "anchor_now"})
	r.Add(preset{label: "This month", fromSQL: "toStartOfMonth(anchor_now)", toSQL: "addMonths(toStartOfMonth(anchor_now), 1)"})
	r.Add(preset{label: "This month so far", fromSQL: "toStartOfMonth(anchor_now)", toSQL: "anchor_now"})
	r.Add(preset{label: "This year", fromSQL: "toStartOfYear(anchor_now)", toSQL: "addYears(toStartOfYear(anchor_now), 1)"})
	r.Add(preset{label: "This year so far", fromSQL: "toStartOfYear(anchor_now)", toSQL: "anchor_now"})
	return
}
