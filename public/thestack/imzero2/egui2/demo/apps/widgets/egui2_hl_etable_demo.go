//go:build llm_generated_opus46

package widgets

// =============================================================================
// ETABLE DEMO — deferred block pattern (virtual scrolling via egui_table)
// =============================================================================
//
// This demonstrates the deferred block approach for egui_table:
//   - Column definitions accumulated via EtColumn (pushed to Rust registers)
//   - Header texts accumulated via EtHeaderText
//   - Cell content captured as deferred blocks via BeginCells/EndCells
//   - EndETable drains columns/headers and sends the block map
//   - Rust replays blocks on demand during virtual scroll rendering
//
// Order of operations:
//   1. c.EtColumn().Send()                  — N times (column sizing)
//   2. c.EtHeaderText().Send()              — N times (header labels)
//   3. et := c.EndETable(...)               — create table builder
//   4. et.BeginCells(row, col)              — start capturing cell
//      c.Label(...).Send()                  — arbitrary widgets (captured)
//      et.EndCells()                        — stop capturing cell
//   5. et.Send()                            — send everything
//
// Key advantage over the register-drain Table: cells can contain arbitrary
// widgets (buttons, checkboxes, rich text), not just plain text.
// =============================================================================

import (
	"fmt"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// =============================================================================
// DEMO: Virtual scrolling table with deferred blocks
// =============================================================================

func demoETable(ids *c.WidgetIdStack) {
	employees := sampleEmployees()

	// Step 1: Columns
	c.EtColumn(150.0).Resizable(true).Send()
	c.EtColumn(120.0).Resizable(true).Send()
	c.EtColumn(100.0).Resizable(true).Send()

	// Step 2: Headers
	c.EtHeaderText("Name").Send()
	c.EtHeaderText("Department").Send()
	c.EtHeaderText("Salary").Send()

	// Step 3: Create table builder
	et := c.EndETable(ids.PrepareStr("etable-demo-tbl"),
		uint64(len(employees)), // numRows
		20.0,                   // defaultRowHeight
		1,                      // numStickyHeaders
		0,                      // numStickyCols
	)

	// Step 4: Capture cell content as deferred blocks
	for row, emp := range employees {
		et.BeginCells(uint64(row), 0)
		c.Label(emp.Name).Send()
		et.EndCells()

		et.BeginCells(uint64(row), 1)
		c.Label(emp.Department).Send()
		et.EndCells()

		et.BeginCells(uint64(row), 2)
		c.Label(fmt.Sprintf("$%d", emp.Salary)).Send()
		et.EndCells()
	}

	// Step 5: Send — splices block map into the message
	et.Send()
}

// =============================================================================
// DEMO: Rich headers with deferred blocks
// =============================================================================
//
// Headers use BeginHeaders/EndHeaders with arbitrary widgets, instead of
// plain text via EtHeaderText.

func demoETableRichHeaders(ids *c.WidgetIdStack) {
	employees := sampleEmployees()

	c.EtColumn(150.0).Resizable(true).Send()
	c.EtColumn(120.0).Resizable(true).Send()
	c.EtColumn(100.0).Resizable(true).Send()
	c.EtColumn(60.0).Send()

	// No EtHeaderText — use deferred header blocks instead
	et := c.EndETable(ids.PrepareStr("etable-rich-headers-tbl"),
		uint64(len(employees)), 20.0, 1, 0,
	)

	// Rich header content (header row 0, columns 0-3)
	for range et.Headers(0, 0) {
		for rt := range c.RichTextLabel("Name") {
			rt.Strong()
		}
	}
	for range et.Headers(0, 1) {
		for rt := range c.RichTextLabel("Department") {
			rt.Italics()
		}
	}
	for range et.Headers(0, 2) {
		for rt := range c.RichTextLabel("Salary") {
			rt.Strong()
		}
	}
	for range et.Headers(0, 3) {
		c.Label("Active").Send()
	}

	// Cell content
	for row, emp := range employees {
		for range et.Cells(uint64(row), 0) {
			c.Label(emp.Name).Send()
		}
		for range et.Cells(uint64(row), 1) {
			c.Label(emp.Department).Send()
		}
		for range et.Cells(uint64(row), 2) {
			c.Label(fmt.Sprintf("$%d", emp.Salary)).Send()
		}
		for range et.Cells(uint64(row), 3) {
			if emp.Active {
				c.Label("Yes").Send()
			} else {
				c.Label("No").Send()
			}
		}
	}

	et.Send()
}

// =============================================================================
// DEMO: Large virtual scrolling table (performance test)
// =============================================================================

func demoETableLarge(ids *c.WidgetIdStack) {
	const numRows = 10_000
	const numCols = 3

	c.EtColumn(80.0).Resizable(true).Send()
	c.EtColumn(200.0).Resizable(true).Send()
	c.EtColumn(120.0).Resizable(true).Send()

	c.EtHeaderText("Row").Send()
	c.EtHeaderText("Description").Send()
	c.EtHeaderText("Value").Send()

	et := c.EndETable(ids.PrepareStr("etable-large-tbl"),
		numRows, 18.0, 1, 0,
	)

	for row := uint64(0); row < numRows; row++ {
		et.BeginCells(row, 0)
		c.Label(fmt.Sprintf("%d", row)).Send()
		et.EndCells()

		et.BeginCells(row, 1)
		c.Label(fmt.Sprintf("Item number %d", row)).Send()
		et.EndCells()

		et.BeginCells(row, 2)
		c.Label(fmt.Sprintf("%.2f", float64(row)*3.14)).Send()
		et.EndCells()
	}

	et.Send()
}

// =============================================================================
// DEMO: Dense 10k table (every cell populated — exercises DenseBlockMap)
// =============================================================================

func demoETableDense10k(ids *c.WidgetIdStack) {
	const numRows = 10_000
	const numCols = 5

	c.EtColumn(60.0).Resizable(true).Send()
	c.EtColumn(120.0).Resizable(true).Send()
	c.EtColumn(100.0).Resizable(true).Send()
	c.EtColumn(80.0).Resizable(true).Send()
	c.EtColumn(80.0).Resizable(true).Send()

	c.EtHeaderText("Row").Send()
	c.EtHeaderText("Name").Send()
	c.EtHeaderText("Category").Send()
	c.EtHeaderText("Value").Send()
	c.EtHeaderText("Status").Send()

	categories := [5]string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon"}
	statuses := [3]string{"Active", "Pending", "Closed"}

	et := c.EndETable(ids.PrepareStr("etable-dense-10k-tbl"),
		numRows, 18.0, 1, 0,
	)

	for row := uint64(0); row < numRows; row++ {
		et.BeginCells(row, 0)
		c.Label(fmt.Sprintf("%d", row)).Send()
		et.EndCells()

		et.BeginCells(row, 1)
		c.Label(fmt.Sprintf("Item-%05d", row)).Send()
		et.EndCells()

		et.BeginCells(row, 2)
		c.Label(categories[row%5]).Send()
		et.EndCells()

		et.BeginCells(row, 3)
		c.Label(fmt.Sprintf("%.2f", float64(row)*1.37)).Send()
		et.EndCells()

		et.BeginCells(row, 4)
		c.Label(statuses[row%3]).Send()
		et.EndCells()
	}

	et.Send()
}

// =============================================================================
// DEMO: Sparse 10k table (only ~30% of cells populated)
// =============================================================================
//
// Demonstrates that DenseBlockMap handles missing cells gracefully:
// unpopulated (row, col) slots have zero-length entries and render as empty.
// Column 0 (row number) is always populated. Other columns are populated
// based on simple modular patterns to create a visually obvious sparse grid.

func demoETableSparse10k(ids *c.WidgetIdStack) {
	const numRows = 10_000
	const numCols = 5

	c.EtColumn(60.0).Resizable(true).Send()
	c.EtColumn(120.0).Resizable(true).Send()
	c.EtColumn(100.0).Resizable(true).Send()
	c.EtColumn(80.0).Resizable(true).Send()
	c.EtColumn(80.0).Resizable(true).Send()

	c.EtHeaderText("Row").Send()
	c.EtHeaderText("Name (mod 3)").Send()
	c.EtHeaderText("Category (mod 5)").Send()
	c.EtHeaderText("Value (mod 7)").Send()
	c.EtHeaderText("Note (mod 11)").Send()

	categories := [5]string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon"}

	et := c.EndETable(ids.PrepareStr("etable-sparse-10k-tbl"),
		numRows, 18.0, 1, 0,
	)

	for row := uint64(0); row < numRows; row++ {
		// Column 0: always present
		et.BeginCells(row, 0)
		c.Label(fmt.Sprintf("%d", row)).Send()
		et.EndCells()

		// Column 1: every 3rd row
		if row%3 == 0 {
			et.BeginCells(row, 1)
			c.Label(fmt.Sprintf("Item-%05d", row)).Send()
			et.EndCells()
		}

		// Column 2: every 5th row
		if row%5 == 0 {
			et.BeginCells(row, 2)
			c.Label(categories[row%5]).Send()
			et.EndCells()
		}

		// Column 3: every 7th row
		if row%7 == 0 {
			et.BeginCells(row, 3)
			c.Label(fmt.Sprintf("%.2f", float64(row)*1.37)).Send()
			et.EndCells()
		}

		// Column 4: every 11th row
		if row%11 == 0 {
			et.BeginCells(row, 4)
			c.Label("*").Send()
			et.EndCells()
		}
	}

	et.Send()
}

// =============================================================================
// DEMO: Interactive cells (buttons, checkboxes with response tracking)
// =============================================================================

// etablesDemoState is the per-window state for the "etables" umbrella
// demo (deferred-block etable family). Currently only the interactive
// sub-demo has per-window state; every other sub-demo in the umbrella
// is stateless. The struct lives in egui2_hl_etable_demo.go so the
// etable-specific types stay co-located with the etable code.
//
// The sibling "tables" demo (classic register-drain Table family) is
// fully stateless — it has no equivalent umbrella struct.
type etablesDemoState struct {
	interactive *etableInteractiveState
}

// etableInteractiveState carries the per-window click/checked state
// for demoETableInteractive. Lives on the tables umbrella's state
// struct so two open gallery windows have independent counters and
// row-selection arrays. The checked slice is heap-allocated so the
// &st.checked[row] pointers handed to Checkbox.SendRespVal stay
// stable for the lifetime of the App.
type etableInteractiveState struct {
	checked     []bool
	clickCount  int
	lastClicked string
}

func newEtableInteractiveState() (st *etableInteractiveState) {
	st = &etableInteractiveState{
		checked: make([]bool, 15),
	}
	return
}

func demoETableInteractive(ids *c.WidgetIdStack, st *etableInteractiveState) {
	employees := sampleEmployees()

	c.EtColumn(150.0).Resizable(true).Send()
	c.EtColumn(120.0).Resizable(true).Send()
	c.EtColumn(60.0).Send()
	c.EtColumn(80.0).Send()

	c.EtHeaderText("Name").Send()
	c.EtHeaderText("Department").Send()
	c.EtHeaderText("Selected").Send()
	c.EtHeaderText("Action").Send()

	et := c.EndETable(ids.PrepareStr("etable-interactive-tbl"),
		uint64(len(employees)), 20.0, 1, 0,
	)

	for row, emp := range employees {
		et.BeginCells(uint64(row), 0)
		c.Label(emp.Name).Send()
		et.EndCells()

		et.BeginCells(uint64(row), 1)
		c.Label(emp.Department).Send()
		et.EndCells()

		et.BeginCells(uint64(row), 2)
		c.Checkbox(ids.PrepareSeq(uint64(0xE700+row)), st.checked[row], "").SendRespVal(&st.checked[row])
		et.EndCells()

		et.BeginCells(uint64(row), 3)
		if c.Button(ids.PrepareSeq(uint64(0xE780+row)), c.Atoms().Text("Click").Keep()).SendResp().HasPrimaryClicked() {
			st.clickCount++
			st.lastClicked = emp.Name
		}
		et.EndCells()
	}

	et.Send()

	// Show interaction state below the table
	selectedCount := 0
	for _, v := range st.checked {
		if v {
			selectedCount++
		}
	}
	c.Label(fmt.Sprintf("Selected: %d | Clicks: %d | Last: %s", selectedCount, st.clickCount, st.lastClicked)).Send()
}

// =============================================================================
// DEMO: Variable row heights
// =============================================================================

func demoETableVariableHeights(ids *c.WidgetIdStack) {
	const numRows = 30

	c.EtColumn(80.0).Resizable(true).Send()
	c.EtColumn(200.0).Resizable(true).Send()

	c.EtHeaderText("Row").Send()
	c.EtHeaderText("Content").Send()

	// Send per-row heights: alternating 20px and 40px
	for row := 0; row < numRows; row++ {
		if row%3 == 0 {
			c.EtRowHeight(40.0).Send()
		} else {
			c.EtRowHeight(20.0).Send()
		}
	}

	et := c.EndETable(ids.PrepareStr("etable-varheight-tbl"),
		numRows, 20.0, 1, 0,
	)

	for row := uint64(0); row < numRows; row++ {
		et.BeginCells(row, 0)
		c.Label(fmt.Sprintf("%d", row)).Send()
		et.EndCells()

		et.BeginCells(row, 1)
		if row%3 == 0 {
			c.Label(fmt.Sprintf("Tall row %d (40px)", row)).Send()
		} else {
			c.Label(fmt.Sprintf("Normal row %d (20px)", row)).Send()
		}
		et.EndCells()
	}

	et.Send()
}
