// Package timeticks generates calendar-aware tick layouts for time axes.
//
// It is the time-axis counterpart to
// github.com/stergiotis/boxer/public/math/numerical/finddivisions, which
// targets numeric (linear/log) axes via Talbot/Heckbert/Wilkinson.
//
// Continuous-scoring algorithms (Talbot, Wilkinson) produce poor results on
// time axes: a tick "every 13 minutes" may score well by simplicity/coverage
// but is hostile to a human reading a clock. Production time-axis renderers
// instead pick from a curated ladder of human-meaningful intervals
// (1s, 5s, 30s, 1m, 5m, 15m, 1h, 6h, 12h, 1d, 1w, 1mo, 1y, ...) and snap
// the first tick to a locale-aware boundary (midnight, hour, month, year).
//
// The ladder, format-by-bucket table, and context-label boundary rules in
// this package follow the design of leeoniya/uPlot (MIT, copyright (c) 2022
// Leon Sorokin) — the rendering layer used by Grafana 7.4+. See
// timeticks_uplot.go for the full attribution and the derivation notes.
package timeticks
