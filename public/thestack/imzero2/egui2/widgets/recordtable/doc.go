// Package recordtable draws an Arrow record as an egui_table grid: an
// immediate-mode widget (ADR-0267), [RenderGrid], and the two parts it is
// built from, which a host with a grid of its own uses directly.
//
//   - [SortState] is the header-click sort, held as a view permutation over
//     the record rather than a re-issued query.
//   - [Chrome] is the cell layer egui_table leaves to the caller: an inset on
//     both sides of every cell, headers that line up with their values, and
//     body cells whose whole rect selects the row.
//
// The parts came out of play's Table pane, which still draws its grids with
// them; [RenderGrid] is the same grid without play's paging toggles, gloss
// faces and replayed cells. Widths persist through a [colwidth.Resolver]
// (ADR-0151) the host acquires from its frame context.
//
// A host keeps a [State] and a [SortState], and per frame:
//
//	order := sort.OrderFor(rec, rec.NumRows())
//	res := recordtable.RenderGrid(recordtable.Input{
//		Ids: ids, ScopeKey: "jobs", Record: rec, Columns: cols,
//		Widths: widths, Sort: &sort, Order: order, Selected: sel, State: &st,
//	})
//	if res.SortClicked >= 0 {
//		sort.Clicked(res.SortClicked)
//	}
//	if res.Clicked >= 0 {
//		sel = res.Clicked
//	}
//
// The clicks and the widths are read back from the host, so they describe the
// previous frame: a click lands one frame after it was made.
package recordtable
