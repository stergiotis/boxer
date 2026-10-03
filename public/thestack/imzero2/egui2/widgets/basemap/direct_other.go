//go:build !wasip1

package basemap

// fetchesDirect is off outside the browser tab: tiles go through the HTTP
// egress service (ADR-0262).
const fetchesDirect = false
