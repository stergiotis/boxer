//go:build llm_generated_opus46

package widgets

// =============================================================================
// TABLE DEMO — register-drain pattern (no BlockIterator, no stack overflow)
// =============================================================================
//
// This demonstrates tables using the register-drain approach:
//   - Cell data is pre-collected via tableCellText / tableCellRichText
//   - Column definitions and headers accumulated via tableColumn / tableHeaderText
//   - Table node drains all registers and renders in one shot
//   - No interpret_outer inside closures — no recursion risk
//
// Order of operations:
//   1. c.TableColumn().Send()       — N times (column sizing)
//   2. c.TableHeaderText().Send()   — N times (optional headers)
//   3. c.TableCellText().Send()     — N*M times (row-major cell data)
//   4. c.Table().Send()             — once (drains and renders)
//
// =============================================================================

import (
	"fmt"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// --- Application data ---

type Employee struct {
	Name       string
	Department string
	Salary     int
	Active     bool
}

func sampleEmployees() []Employee {
	return []Employee{
		{"Alice Chen", "Engineering", 145000, true},
		{"Bob Martinez", "Design", 125000, true},
		{"Carol Johnson", "Engineering", 155000, true},
		{"David Kim", "Marketing", 110000, false},
		{"Eve Williams", "Engineering", 160000, true},
		{"Frank Brown", "Sales", 95000, true},
		{"Grace Lee", "Design", 130000, true},
		{"Henry Davis", "Marketing", 105000, true},
		{"Iris Taylor", "Engineering", 150000, true},
		{"Jack Wilson", "Sales", 92000, false},
		{"Karen White", "Engineering", 148000, true},
		{"Leo Garcia", "Design", 128000, true},
		{"Monica Adams", "Marketing", 115000, true},
		{"Nathan Clark", "Sales", 98000, true},
		{"Olivia Scott", "Engineering", 162000, true},
	}
}

// =============================================================================
// DEMO 1: Simple table with plain text cells
// =============================================================================

func demoSimpleTable(ids *c.WidgetIdStack) {
	employees := sampleEmployees()

	// Step 1: Columns
	c.TableColumn().Initial(150.0).Resizable(true).Send()
	c.TableColumn().Initial(120.0).Resizable(true).Send()
	c.TableColumn().Remainder().Send()

	// Step 2: Headers
	c.TableHeaderText("Name").Send()
	c.TableHeaderText("Department").Send()
	c.TableHeaderText("Salary").Send()

	// Step 3: Cells (row-major order: all cols of row 0, then row 1, ...)
	for _, emp := range employees {
		c.TableCellText(emp.Name).Send()
		c.TableCellText(emp.Department).Send()
		c.TableCellText(fmt.Sprintf("$%d", emp.Salary)).Send()
	}

	// Step 4: Render (drains all registers)
	c.Table(ids.PrepareStr("simple-table"), 20.0, uint64(len(employees))).Striped(true).Send()
}

// =============================================================================
// DEMO 2: Table with no header
// =============================================================================
//
// If no tableHeaderText calls are made before the table node, the table
// renders without a header row.

func demoHeaderlessTable(ids *c.WidgetIdStack) {
	data := [][]string{
		{"Alpha", "100", "Yes"},
		{"Beta", "200", "No"},
		{"Gamma", "300", "Yes"},
		{"Delta", "400", "No"},
	}

	c.TableColumn().Auto().Send()
	c.TableColumn().Auto().Send()
	c.TableColumn().Auto().Send()

	// No c.TableHeaderText() calls — no header row
	for _, row := range data {
		for _, cell := range row {
			c.TableCellText(cell).Send()
		}
	}

	c.Table(ids.PrepareStr("headerless"), 18.0, uint64(len(data))).Send()
}

// =============================================================================
// DEMO 3: Table with mixed column sizing
// =============================================================================

func demoMixedColumnsTable(ids *c.WidgetIdStack) {
	employees := sampleEmployees()

	// Fixed + auto + remainder columns
	c.TableColumn().Exact(40.0).Send()
	c.TableColumn().Initial(150.0).Resizable(true).Send()
	c.TableColumn().Initial(120.0).Resizable(true).Send()
	c.TableColumn().Remainder().Send()

	c.TableHeaderText("#").Send()
	c.TableHeaderText("Name").Send()
	c.TableHeaderText("Department").Send()
	c.TableHeaderText("Salary").Send()

	for i, emp := range employees {
		c.TableCellText(fmt.Sprintf("%d", i+1)).Send()
		c.TableCellText(emp.Name).Send()
		c.TableCellText(emp.Department).Send()
		c.TableCellText(fmt.Sprintf("$%d", emp.Salary)).Send()
	}

	c.Table(ids.PrepareStr("mixed-cols"), 20.0, uint64(len(employees))).Striped(true).Vscroll(true).Send()
}

// =============================================================================
// DEMO 4: Table with rich text cells
// =============================================================================
//
// Uses tableCellRichText to push styled text into cells.
// The atoms register is consumed per cell.

func demoRichTextTable(ids *c.WidgetIdStack) {
	employees := sampleEmployees()

	c.TableColumn().Initial(150.0).Resizable(true).Send()
	c.TableColumn().Initial(120.0).Resizable(true).Send()
	c.TableColumn().Remainder().Send()

	c.TableHeaderText("Name").Send()
	c.TableHeaderText("Department").Send()
	c.TableHeaderText("Salary").Send()

	for _, emp := range employees {
		// Name column: bold — plain text for now (register-drain tables
		// don't support inline rich text; use etable deferred blocks instead)
		c.TableCellText(emp.Name).Send()

		// Department column: plain text
		c.TableCellText(emp.Department).Send()

		// Salary column: plain text
		c.TableCellText(fmt.Sprintf("$%d", emp.Salary)).Send()
	}

	c.Table(ids.PrepareStr("richtext-table"), 20.0, uint64(len(employees))).Striped(true).Send()
}

// =============================================================================
// DEMO 5: Table inside a window
// =============================================================================

func demoTableInWindow(ids *c.WidgetIdStack) {
	employees := sampleEmployees()

	for range c.Window(ids.PrepareStr("emp-window"), c.WidgetText().Text("Employee Directory").Keep()).DefaultOpen(true).Resizable(true).KeepIter() {

		c.TableColumn().Initial(150.0).Resizable(true).Send()
		c.TableColumn().Initial(120.0).Resizable(true).Send()
		c.TableColumn().Remainder().Send()

		c.TableHeaderText("Name").Send()
		c.TableHeaderText("Department").Send()
		c.TableHeaderText("Salary").Send()

		for _, emp := range employees {
			c.TableCellText(emp.Name).Send()
			c.TableCellText(emp.Department).Send()
			c.TableCellText(fmt.Sprintf("$%d", emp.Salary)).Send()
		}

		c.Table(ids.PrepareStr("window-table"), 20.0, uint64(len(employees))).Striped(true).Vscroll(true).Send()
	}
}

// =============================================================================
// DEMO 6: Large table (virtualization test)
// =============================================================================
//
// body.rows() with row virtualization: only visible rows call the closure.
// All cell data is pre-collected in Vec<TableCell>, so the closure indexes
// cells[row_idx * col_count + col_idx] — no pipe reads in the closure.

func demoLargeTable(ids *c.WidgetIdStack) {
	const numRows = 10_000

	c.TableColumn().Initial(80.0).Resizable(true).Send()
	c.TableColumn().Initial(200.0).Resizable(true).Send()
	c.TableColumn().Remainder().Send()

	c.TableHeaderText("Row").Send()
	c.TableHeaderText("Description").Send()
	c.TableHeaderText("Value").Send()

	for i := 0; i < numRows; i++ {
		c.TableCellText(fmt.Sprintf("%d", i)).Send()
		c.TableCellText(fmt.Sprintf("Item number %d", i)).Send()
		c.TableCellText(fmt.Sprintf("%.2f", float64(i)*3.14)).Send()
	}

	c.Table(ids.PrepareStr("large-table"), 18.0, uint64(numRows)).Striped(true).Vscroll(true).Send()
}
