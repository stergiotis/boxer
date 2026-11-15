// Package notebookcells registers renderers of a notebook's presentation
// cells that are drawn small, for a report read among prose: `marks`, the
// instants and intervals of a few things on one time axis, and `points`,
// places on a map. A host that shows notebooks links the package with a
// blank import; nothing is called.
//
// Each reads its relation's columns by name, which a cell's aliases fit a
// record to:
//
//	```sql marks
//	SELECT born AS _tl_time, kind AS _tl_label FROM dates
//	```
//
//	```sql points
//	SELECT latitude AS lat, longitude AS lon, name AS label FROM places
//	```
//
// Both draw what they are handed and keep no view of their own between
// results: a new result is framed afresh. Neither takes the wheel, so a
// report scrolls under the pointer wherever it is.
//
// What they do not do: `points` draws no basemap tiles and no H3 cells.
// Coasts and borders are the vendored country outlines, stroked and not
// filled, which are coarse past a regional zoom.
package notebookcells
