//go:build wasip1

package basemap

// fetchesDirect is on in the browser tab (ADR-0263), which has no runtime
// services and so no HTTP egress service to fetch tiles through: the tab
// fetches them itself, over its host transport (ADR-0262 Update 2026-10-03).
const fetchesDirect = true
