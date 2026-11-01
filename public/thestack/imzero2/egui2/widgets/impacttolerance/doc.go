// Package impacttolerance is an immediate-mode widget (ADR-0267) that draws
// important business services against their impact tolerances under named
// scenarios — the operational-resilience chart. It was promoted out of the
// play pane of the same name (ADR-0122, 2026-11-01 update), which builds its
// Model from a query result.
//
// One row per service. Each carries a tolerance — the longest disruption it
// may suffer, in hours — drawn as a line with the region beyond it shaded,
// and one point per scenario at the estimated time to recover, on a
// logarithmic time axis that FormatHours labels in the unit each tick reads
// best in. A row's label says how many of its scenarios breach
// ("2 of 3 breach"), so the reading does not rest on colour. Scenario k takes
// qualitative slot k, named in a legend above the rows.
//
// It works without probabilities: the question is whether a service comes
// back inside its tolerance, not how likely the scenario is.
//
//	res := impacttolerance.Render(impacttolerance.Input{
//	    Ids: ids, ScopeKey: "services", Model: &model, State: &st, Width: 900,
//	})
//	if res.Clicked.Valid() { /* the estimate res.Clicked was clicked */ }
//
// It is painted on the painter substrate into one canvas, every colour from
// the design system. Every estimate and every tolerance line is a hit region
// (ADR-0149's hit regions): the host hit-tests it, shows its tooltip at the
// pointer, and gives it an accessibility node named "service · scenario" for
// an estimate and "service · tolerance" for a line, so a reader or a scene
// finds it by name. The estimates are Tab stops; Enter or Space on a focused
// one is its click. Result.Hovered and Result.Clicked are read back from the
// regions and lag the pointer by one frame.
package impacttolerance
