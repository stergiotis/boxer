// Package recordtable holds the parts an egui_table grid over an Arrow record
// is drawn from. They came out of play's Table pane, which still draws its
// grids with them.
//
//   - [SortState] is the header-click sort, held as a view permutation over
//     the record rather than a re-issued query.
//   - [Chrome] is the cell layer egui_table leaves to the caller: an inset on
//     both sides of every cell, headers that line up with their values, and
//     body cells whose whole rect selects the row.
package recordtable
